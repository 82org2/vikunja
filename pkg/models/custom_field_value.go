// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package models

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/web"
	"github.com/google/uuid"
	"xorm.io/xorm"
)

// TaskCustomFieldValue is the stored row for one task and definition. No row
// means the field is unset; an empty string or a false value are real values.
// Exactly one of the value_* columns is populated — except for multi-select,
// which stores its selection in custom_field_value_options and leaves every
// typed column NULL.
type TaskCustomFieldValue struct {
	ID int64 `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this value row."`

	TaskID int64 `xorm:"bigint not null index unique(task_definition)" json:"task_id" doc:"The task this value belongs to."`

	DefinitionID int64 `xorm:"bigint not null index unique(task_definition) index(definition_short_text) index(definition_number) index(definition_boolean) index(definition_date) index(definition_datetime) index(definition_user) index(definition_single_option)" json:"definition_id" doc:"The definition this value belongs to. The leading column of the typed query indexes."`

	// The typed columns: exactly one is populated (multi-select uses none,
	// storing its selection in custom_field_value_options instead).
	ValueShortText      *string    `xorm:"value_short_text varchar(255) null index(definition_short_text)" json:"short_text,omitempty" doc:"Short text value, at most 255 characters."`
	ValueLongText       *string    `xorm:"value_long_text longtext null" json:"long_text,omitempty" doc:"Long text value, at most 65535 bytes. Not indexed or sortable."`
	ValueNumber         *int64     `xorm:"value_number bigint null index(definition_number)" json:"number,omitempty" doc:"Fixed-point number value scaled by the definition's precision."`
	ValueBoolean        *bool      `xorm:"value_boolean boolean null index(definition_boolean)" json:"boolean,omitempty" doc:"Boolean value."`
	ValueDate           *int64     `xorm:"value_date bigint null index(definition_date)" json:"date,omitempty" doc:"Date value as a signed day count from 1970-01-01."`
	ValueDateTime       *time.Time `xorm:"value_datetime datetime null index(definition_datetime)" json:"datetime,omitempty" doc:"Date-time value, a UTC instant exchanged as RFC 3339."`
	ValueURL            *string    `xorm:"value_url text null" json:"url,omitempty" doc:"URL value, a validated absolute HTTP(S) URL of at most 2048 characters. Not indexed or sortable."`
	ValueUserID         *int64     `xorm:"value_user_id bigint null index(definition_user)" json:"user_id,omitempty" doc:"User value, an active user visible in the definition's project."`
	ValueSingleOptionID *int64     `xorm:"value_single_option_id bigint null index(definition_single_option)" json:"single_option_id,omitempty" doc:"Single-select value, an option of the definition."`

	Created time.Time `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this value was created. You cannot change this value."`
	Updated time.Time `xorm:"updated not null" json:"updated" readOnly:"true" doc:"A timestamp when this value was last updated. You cannot change this value."`
}

// TableName makes a pretty table name
func (*TaskCustomFieldValue) TableName() string {
	return "custom_field_values"
}

// TaskCustomFieldValueWithDefinition is the wire shape of one populated custom
// field value on a task: the definition context plus the discriminated value,
// nested so CustomFieldValue's own schema and marshaler are reused unchanged.
type TaskCustomFieldValueWithDefinition struct {
	DefinitionID int64             `json:"definition_id" doc:"The numeric id of the custom field definition this value belongs to."`
	MachineKey   string            `json:"machine_key" doc:"The immutable machine key of the definition. The stable identity for filtering, export, duplication, and moves."`
	Value        *CustomFieldValue `json:"value" doc:"The typed value, self-describing via its type discriminant."`
}

// --- reads ---

func GetCustomFieldValue(s *xorm.Session, taskID, definitionID int64) (*TaskCustomFieldValue, error) {
	v := &TaskCustomFieldValue{}
	exists, err := s.Where("task_id = ? AND definition_id = ?", taskID, definitionID).Get(v)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomFieldValueDoesNotExist{ValueID: definitionID}
	}
	return v, nil
}

// CanWriteCustomFieldValue reports whether the auth may set or unset a value on
// the task. It delegates to Task.CanWrite, which resolves the task's project and
// checks project write permission, covering users and write-admin link shares.
func CanWriteCustomFieldValue(s *xorm.Session, taskID int64, a web.Auth) (bool, error) {
	return (&Task{ID: taskID}).CanWrite(s, a)
}

// addCustomFieldsToTasks batch-loads the custom field values for the given task
// ids, ordered per task by definition position. Values and their definitions are
// loaded in chunked queries, and multi-select memberships in one batched pair of
// queries regardless of how many values there are, so bulk reads stay bounded.
// Every task in the map gets a non-nil slice (empty when it has no values) so
// the expand-active response serializes [] rather than omitting the field.
//
// Malformed stored rows fail loudly rather than being silently discarded: a
// value row whose definition is missing or lives in a different project means
// the stored data is corrupt.
func addCustomFieldsToTasks(s *xorm.Session, taskIDs []int64, taskMap map[int64]*Task) error {
	rows := []*TaskCustomFieldValue{}
	const batchSize = 500
	for chunk := range slices.Chunk(taskIDs, batchSize) {
		batch := []*TaskCustomFieldValue{}
		if err := s.In("task_id", chunk).Find(&batch); err != nil {
			return err
		}
		rows = append(rows, batch...)
	}

	for _, task := range taskMap {
		task.CustomFields = []*TaskCustomFieldValueWithDefinition{}
	}
	if len(rows) == 0 {
		return nil
	}

	defIDs := make([]int64, 0, len(rows))
	seen := map[int64]struct{}{}
	for _, row := range rows {
		if _, ok := seen[row.DefinitionID]; ok {
			continue
		}
		seen[row.DefinitionID] = struct{}{}
		defIDs = append(defIDs, row.DefinitionID)
	}
	defs, err := getCustomFieldDefinitionsByIDs(s, defIDs)
	if err != nil {
		return err
	}
	defMap := make(map[int64]*CustomFieldDefinition, len(defs))
	for _, def := range defs {
		defMap[def.ID] = def
	}

	// Load every multi-select value's memberships in one batched pair of queries
	// instead of two queries per value inside FromRow.
	multiSelectValueIDs := make([]int64, 0, len(rows))
	valueDefinitions := make(map[int64]int64, len(rows))
	for _, row := range rows {
		def, ok := defMap[row.DefinitionID]
		if !ok {
			return missingCustomFieldDefinitionError(row)
		}
		if def.FieldType == CustomFieldTypeMultiSelect {
			multiSelectValueIDs = append(multiSelectValueIDs, row.ID)
			valueDefinitions[row.ID] = row.DefinitionID
		}
	}
	optionIDsByValue, err := getOptionIDsForValues(s, multiSelectValueIDs, valueDefinitions)
	if err != nil {
		return err
	}

	byTask := make(map[int64][]*TaskCustomFieldValue, len(rows))
	for _, row := range rows {
		byTask[row.TaskID] = append(byTask[row.TaskID], row)
	}
	for taskID, taskRows := range byTask {
		task, ok := taskMap[taskID]
		if !ok {
			continue
		}
		values := make([]*TaskCustomFieldValueWithDefinition, 0, len(taskRows))
		for _, row := range taskRows {
			def, ok := defMap[row.DefinitionID]
			if !ok {
				return missingCustomFieldDefinitionError(row)
			}
			if def.ProjectID != task.ProjectID {
				return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The custom field value row %d references a definition from a different project.", row.ID)}
			}
			var value *CustomFieldValue
			if def.FieldType == CustomFieldTypeMultiSelect {
				if row.populatedStoredFields() != 0 {
					return ErrInvalidCustomFieldValue{Message: "The stored value row for a multi-select field must not have a typed column."}
				}
				optionIDs := optionIDsByValue[row.ID]
				if len(optionIDs) == 0 {
					return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The multi-select value row %d has no memberships; an empty selection must be unset.", row.ID)}
				}
				value = &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: optionIDs}
			} else {
				value, err = row.FromRow(s, def)
				if err != nil {
					return err
				}
			}
			values = append(values, &TaskCustomFieldValueWithDefinition{
				DefinitionID: def.ID,
				MachineKey:   def.MachineKey,
				Value:        value,
			})
		}
		sort.SliceStable(values, func(i, j int) bool {
			di := defMap[values[i].DefinitionID]
			dj := defMap[values[j].DefinitionID]
			if di.Position != dj.Position {
				return di.Position < dj.Position
			}
			return di.ID < dj.ID
		})
		task.CustomFields = values
	}
	return nil
}

func missingCustomFieldDefinitionError(row *TaskCustomFieldValue) error {
	return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The custom field value row %d references a definition that does not exist.", row.ID)}
}

// --- write path ---

// Set stores the given value for the task and definition, replacing any
// existing row and, for multi-select, its memberships. The session is expected
// to be transactional so the upsert and membership replace commit together.
// A nil auth suppresses lifecycle events (internal callers such as default
// materialisation); a non-nil auth queues a value event and one task.updated
// event, both dispatched after the transaction commits.
func SetCustomFieldValue(s *xorm.Session, taskID int64, def *CustomFieldDefinition, value *CustomFieldValue, a web.Auth) error {
	if value == nil {
		return ErrInvalidCustomFieldValue{Message: "A custom field value is required."}
	}
	if def == nil {
		return ErrInvalidCustomFieldValue{Message: "A custom field definition is required."}
	}

	// Lock the task row first, then the definition row: value writes and task
	// moves both take the task lock before any definition lock, so the order is
	// consistent and no AB-BA deadlock is possible. The task lock also
	// serializes concurrent writes to any custom field on the task.
	if err := lockTaskRow(s, taskID); err != nil {
		return err
	}
	// Lock the definition row so a concurrent permanent-delete or definition
	// update cannot interleave: whichever side holds the lock first wins, and the
	// loser sees the definition gone (or the new value included in the deletion
	// and validated against the new constraints).
	if err := lockCustomFieldDefinition(s, def.ID); err != nil {
		return err
	}
	persistedDef, err := GetCustomFieldDefinitionByID(s, def.ID)
	if err != nil {
		return err
	}
	task, err := GetTaskByIDSimple(s, taskID)
	if err != nil {
		return err
	}
	if task.ProjectID != persistedDef.ProjectID {
		return ErrInvalidCustomFieldValue{Message: "The task and custom field definition must belong to the same project."}
	}
	if persistedDef.IsArchived {
		return ErrInvalidCustomFieldValue{Message: "Archived custom field definitions cannot receive new values."}
	}

	if err := value.validate(); err != nil {
		return err
	}
	if err := validateValueAgainstDefinition(s, value, persistedDef); err != nil {
		return err
	}

	// An empty multi-select value means "unset": delete the row instead of
	// storing it. This runs after the definition checks so a number, foreign, or
	// nonexistent definition fails the normal validation first. The task and
	// definition locks are already held, so the locked unset helper is used.
	if value.Type == CustomFieldTypeMultiSelect && value.OptionIDs != nil && len(value.OptionIDs) == 0 && value.populatedFields() == 0 {
		return unsetCustomFieldValueLocked(s, taskID, persistedDef, a)
	}

	// An idempotent set — the stored value already equals the new one — must not
	// write or emit anything: no task timestamp advance, no upsert, no event.
	oldRow := &TaskCustomFieldValue{}
	has, err := s.Where("task_id = ? AND definition_id = ?", taskID, persistedDef.ID).Get(oldRow)
	if err != nil {
		return err
	}
	var oldValue *CustomFieldValue
	if has {
		oldValue, err = oldRow.FromRow(s, persistedDef)
		if err != nil {
			return err
		}
		if oldValue.Equals(value) {
			return nil
		}
	}

	row, err := value.toRow(taskID, persistedDef.ID)
	if err != nil {
		return err
	}
	// Besides advancing the task timestamp, this serializes concurrent writes
	// to any custom field on the task so the XORM update-or-insert below cannot
	// race on its unique key.
	if err = updateTaskLastUpdated(s, &Task{ID: taskID}); err != nil {
		return err
	}
	if err := row.upsert(s); err != nil {
		return err
	}

	// Memberships are always rewritten: switching a value away from multi-select
	// must not leave orphaned selections behind.
	optionIDs := []int64{}
	if value.Type == CustomFieldTypeMultiSelect {
		optionIDs = value.OptionIDs
	}
	if err := replaceValueOptions(s, row.ID, optionIDs); err != nil {
		return err
	}

	if a == nil {
		return nil
	}

	semantic := CustomFieldValueSet
	if has {
		semantic = CustomFieldValueChanged
	}
	events.DispatchOnCommit(s, &CustomFieldValueEvent{
		EventID:      uuid.NewString(),
		Version:      1,
		Semantic:     semantic,
		ProjectID:    task.ProjectID,
		TaskID:       taskID,
		DefinitionID: persistedDef.ID,
		MachineKey:   persistedDef.MachineKey,
		Type:         persistedDef.FieldType,
		OldValue:     oldValue,
		NewValue:     value,
		Doer:         doerFromAuth(s, a),
		Timestamp:    time.Now(),
	})
	return triggerTaskUpdatedEventForTaskID(s, a, taskID)
}

// materialiseCustomFieldDefaults inserts one value row per task for every
// definition of the project that carries a default. Archived definitions are
// skipped — they cannot receive new values. A default that has gone stale since
// it was saved — its option archived or user deactivated — is skipped for that
// task rather than failing the whole task creation.
func materialiseCustomFieldDefaults(s *xorm.Session, projectID int64, tasks []*Task) error {
	defs, err := getCustomFieldDefinitionsForProject(s, projectID)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		for _, def := range defs {
			if def.IsArchived || def.DefaultValue == nil {
				continue
			}
			if err := validateValueAgainstDefinition(s, def.DefaultValue, def); err != nil {
				if isStaleCustomFieldDefaultError(err, def.FieldType) {
					continue
				}
				return err
			}
			value := *def.DefaultValue
			// Defaults are internal: no lifecycle events, and no task.updated
			// during task creation.
			if err := SetCustomFieldValue(s, task.ID, def, &value, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func isStaleCustomFieldDefaultError(err error, fieldType CustomFieldType) bool {
	var optionMissing ErrCustomFieldOptionDoesNotExist
	var userMissing ErrCustomFieldUserNotVisible
	if errors.As(err, &optionMissing) || errors.As(err, &userMissing) {
		return true
	}
	var invalidValue ErrInvalidCustomFieldValue
	return errors.As(err, &invalidValue) && (fieldType == CustomFieldTypeSingleSelect || fieldType == CustomFieldTypeMultiSelect)
}

// UnsetCustomFieldValue removes the value row and its memberships. Unsetting a
// field that has no row is a no-op, matching the idempotent unset contract.
// A nil auth suppresses lifecycle events; a non-nil auth queues an unset event
// and one task.updated event, both dispatched after the transaction commits.
func UnsetCustomFieldValue(s *xorm.Session, taskID, definitionID int64, a web.Auth) error {
	// Take the task row lock first, then the definition row, matching
	// SetCustomFieldValue's lock order.
	if err := lockTaskRow(s, taskID); err != nil {
		return err
	}
	if err := lockCustomFieldDefinition(s, definitionID); err != nil {
		return err
	}
	def, err := GetCustomFieldDefinitionByID(s, definitionID)
	if err != nil {
		return err
	}
	return unsetCustomFieldValueLocked(s, taskID, def, a)
}

// unsetCustomFieldValueLocked removes the value row and its memberships,
// assuming the task and definition locks are already held. It is shared by
// UnsetCustomFieldValue and the empty multi-select path of SetCustomFieldValue,
// which must not re-acquire the locks it already holds.
func unsetCustomFieldValueLocked(s *xorm.Session, taskID int64, def *CustomFieldDefinition, a web.Auth) error {
	row, err := GetCustomFieldValue(s, taskID, def.ID)
	if err != nil {
		if IsErrCustomFieldValueDoesNotExist(err) {
			return nil
		}
		return err
	}

	oldValue, err := row.FromRow(s, def)
	if err != nil {
		return err
	}

	if _, err = s.Where("value_id = ?", row.ID).Delete(&CustomFieldValueOption{}); err != nil {
		return err
	}
	if _, err = s.Where("id = ?", row.ID).Delete(&TaskCustomFieldValue{}); err != nil {
		return err
	}

	if err = updateTaskLastUpdated(s, &Task{ID: taskID}); err != nil {
		return err
	}

	if a == nil {
		return nil
	}

	task, err := GetTaskByIDSimple(s, taskID)
	if err != nil {
		return err
	}
	events.DispatchOnCommit(s, &CustomFieldValueEvent{
		EventID:      uuid.NewString(),
		Version:      1,
		Semantic:     CustomFieldValueUnset,
		ProjectID:    task.ProjectID,
		TaskID:       taskID,
		DefinitionID: def.ID,
		MachineKey:   def.MachineKey,
		Type:         def.FieldType,
		OldValue:     oldValue,
		NewValue:     nil,
		Doer:         doerFromAuth(s, a),
		Timestamp:    time.Now(),
	})
	return triggerTaskUpdatedEventForTaskID(s, a, taskID)
}

// toRow maps the discriminated value onto exactly one typed column of the row.
func (v *CustomFieldValue) toRow(taskID, definitionID int64) (*TaskCustomFieldValue, error) {
	row := &TaskCustomFieldValue{
		TaskID:       taskID,
		DefinitionID: definitionID,
	}

	var err error
	switch v.Type {
	case CustomFieldTypeShortText:
		row.ValueShortText = v.ShortText
	case CustomFieldTypeLongText:
		row.ValueLongText = v.LongText
	case CustomFieldTypeNumber:
		number := v.Number.Value
		row.ValueNumber = &number
	case CustomFieldTypeBoolean:
		row.ValueBoolean = v.Boolean
	case CustomFieldTypeDate:
		days := v.Date.DayCount
		row.ValueDate = &days
	case CustomFieldTypeDateTime:
		row.ValueDateTime = v.DateTime
	case CustomFieldTypeURL:
		row.ValueURL = v.URL
	case CustomFieldTypeUser:
		row.ValueUserID = v.UserID
	case CustomFieldTypeSingleSelect:
		row.ValueSingleOptionID = v.SingleOptionID
	case CustomFieldTypeMultiSelect:
		// No typed column; memberships carry the selection.
	default:
		err = ErrInvalidCustomFieldType{Type: string(v.Type)}
	}

	return row, err
}

// FromRow reconstructs the discriminated value from a stored row, loading
// memberships for multi-select values. Rows are trusted only after checking the
// exactly-one-typed-column invariant: a malformed row must fail loudly here
// instead of panicking on a nil dereference or decoding into the wrong shape.
func (v *TaskCustomFieldValue) FromRow(s *xorm.Session, def *CustomFieldDefinition) (*CustomFieldValue, error) {
	populated := v.populatedStoredFields()
	if populated > 1 {
		return nil, ErrInvalidCustomFieldValue{Message: "The stored custom field value row has more than one populated typed column."}
	}

	value := &CustomFieldValue{Type: def.FieldType}

	switch def.FieldType {
	case CustomFieldTypeShortText:
		if v.ValueShortText == nil {
			return nil, invalidStoredCustomFieldRow(def.FieldType)
		}
		value.ShortText = v.ValueShortText
	case CustomFieldTypeLongText:
		if v.ValueLongText == nil {
			return nil, invalidStoredCustomFieldRow(def.FieldType)
		}
		value.LongText = v.ValueLongText
	case CustomFieldTypeNumber:
		if v.ValueNumber == nil {
			return nil, invalidStoredCustomFieldRow(def.FieldType)
		}
		number := &CustomFieldNumber{Value: *v.ValueNumber, Precision: numberPrecision(def)}
		number.raw = formatFixedPoint(number.Value, number.Precision)
		value.Number = number
	case CustomFieldTypeBoolean:
		if v.ValueBoolean == nil {
			return nil, invalidStoredCustomFieldRow(def.FieldType)
		}
		value.Boolean = v.ValueBoolean
	case CustomFieldTypeDate:
		if v.ValueDate == nil {
			return nil, invalidStoredCustomFieldRow(def.FieldType)
		}
		value.Date = &CustomFieldDate{DayCount: *v.ValueDate}
	case CustomFieldTypeDateTime:
		if v.ValueDateTime == nil {
			return nil, invalidStoredCustomFieldRow(def.FieldType)
		}
		utc := v.ValueDateTime.UTC()
		value.DateTime = &utc
	case CustomFieldTypeURL:
		if v.ValueURL == nil {
			return nil, invalidStoredCustomFieldRow(def.FieldType)
		}
		value.URL = v.ValueURL
	case CustomFieldTypeUser:
		if v.ValueUserID == nil {
			return nil, invalidStoredCustomFieldRow(def.FieldType)
		}
		value.UserID = v.ValueUserID
	case CustomFieldTypeSingleSelect:
		if v.ValueSingleOptionID == nil {
			return nil, invalidStoredCustomFieldRow(def.FieldType)
		}
		value.SingleOptionID = v.ValueSingleOptionID
	case CustomFieldTypeMultiSelect:
		if populated != 0 {
			return nil, ErrInvalidCustomFieldValue{Message: "The stored value row for a multi-select field must not have a typed column."}
		}
		optionIDs, err := getOptionIDsForValue(s, v.ID)
		if err != nil {
			return nil, err
		}
		value.OptionIDs = optionIDs
	default:
		return nil, ErrInvalidCustomFieldType{Type: string(def.FieldType)}
	}

	return value, nil
}

func invalidStoredCustomFieldRow(t CustomFieldType) error {
	return ErrInvalidCustomFieldValue{
		Message: fmt.Sprintf("The stored value row for a %s field is missing its typed column.", string(t)),
	}
}

func (v *TaskCustomFieldValue) populatedStoredFields() int {
	populated := 0
	for _, set := range []bool{
		v.ValueShortText != nil,
		v.ValueLongText != nil,
		v.ValueNumber != nil,
		v.ValueBoolean != nil,
		v.ValueDate != nil,
		v.ValueDateTime != nil,
		v.ValueURL != nil,
		v.ValueUserID != nil,
		v.ValueSingleOptionID != nil,
	} {
		if set {
			populated++
		}
	}
	return populated
}

// upsert replaces all typed columns or inserts the first value row. The caller
// holds a write lock on the task row, serializing competing writes to this
// unique (task_id, definition_id) key without dialect-specific SQL.
func (v *TaskCustomFieldValue) upsert(s *xorm.Session) error {
	existing := &TaskCustomFieldValue{}
	found, err := s.Where("task_id = ? AND definition_id = ?", v.TaskID, v.DefinitionID).Get(existing)
	if err != nil {
		return err
	}
	if !found {
		_, err = s.Insert(v)
		return err
	}

	v.ID = existing.ID
	_, err = s.ID(v.ID).
		Cols("value_short_text", "value_long_text", "value_number", "value_boolean", "value_date", "value_datetime", "value_url", "value_user_id", "value_single_option_id", "updated").
		Update(v)
	return err
}

// --- lifecycle cleanup and duplication ---

// deleteTaskCustomFieldValues removes the value rows and memberships of a task
// in bounded batches. Called by hardDeleteTask so a purged task leaves no
// custom-field rows behind.
func deleteTaskCustomFieldValues(s *xorm.Session, taskID int64) error {
	const batchSize = 500
	for {
		rows := []*TaskCustomFieldValue{}
		if err := s.Where("task_id = ?", taskID).Limit(batchSize).Cols("id").Find(&rows); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if _, err := s.In("value_id", ids).Delete(&CustomFieldValueOption{}); err != nil {
			return err
		}
		if _, err := s.In("id", ids).Delete(&TaskCustomFieldValue{}); err != nil {
			return err
		}
	}
}

// unsetCustomFieldUserValues deletes every user-type value row referencing the
// deleted user and advances each affected task's updated timestamp in bounded
// batches, so task ETags stay consistent. User-type values have no memberships,
// so a plain row delete suffices; the referenced user must not be left as a
// dangling identity.
func unsetCustomFieldUserValues(s *xorm.Session, userID int64) error {
	const batchSize = 500
	taskIDs := make([]int64, 0)
	seen := map[int64]struct{}{}
	for {
		rows := []*TaskCustomFieldValue{}
		if err := s.Where("value_user_id = ?", userID).Limit(batchSize).Cols("id", "task_id").Find(&rows); err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
			if _, ok := seen[row.TaskID]; ok {
				continue
			}
			seen[row.TaskID] = struct{}{}
			taskIDs = append(taskIDs, row.TaskID)
		}
		if _, err := s.In("id", ids).Delete(&TaskCustomFieldValue{}); err != nil {
			return err
		}
	}
	for chunk := range slices.Chunk(taskIDs, batchSize) {
		if _, err := s.In("id", chunk).Cols("updated").Update(&Task{}); err != nil {
			return err
		}
	}
	return nil
}

// copyTaskCustomFieldValues copies every value row and multi-select membership
// of the source task onto the destination task, remapping definition and
// option ids through the given maps (nil maps mean identity). It first clears
// all destination values so defaults materialized by task creation are removed
// and unset fields stay unset: the copy is exact. It bypasses
// SetCustomFieldValue so archived definitions are copied and no per-value
// validation or timestamp churn happens; the source values were already valid
// against their definitions.
func copyTaskCustomFieldValues(s *xorm.Session, sourceTaskID, destTaskID int64, defRemap, optionRemap map[int64]int64) error {
	if err := deleteTaskCustomFieldValues(s, destTaskID); err != nil {
		return err
	}

	rows := []*TaskCustomFieldValue{}
	if err := s.Where("task_id = ?", sourceTaskID).Find(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	// Load the source definitions to detect multi-select rows and fail loudly
	// on corrupt data.
	defIDs := make([]int64, 0, len(rows))
	seen := map[int64]struct{}{}
	for _, row := range rows {
		if _, ok := seen[row.DefinitionID]; ok {
			continue
		}
		seen[row.DefinitionID] = struct{}{}
		defIDs = append(defIDs, row.DefinitionID)
	}
	defs, err := getCustomFieldDefinitionsByIDs(s, defIDs)
	if err != nil {
		return err
	}
	defMap := make(map[int64]*CustomFieldDefinition, len(defs))
	for _, def := range defs {
		defMap[def.ID] = def
	}

	// Load every multi-select value's memberships in one batched pair of
	// queries instead of one query per value.
	multiSelectValueIDs := make([]int64, 0, len(rows))
	valueDefinitions := make(map[int64]int64, len(rows))
	for _, row := range rows {
		def, ok := defMap[row.DefinitionID]
		if !ok {
			return missingCustomFieldDefinitionError(row)
		}
		if def.FieldType == CustomFieldTypeMultiSelect {
			multiSelectValueIDs = append(multiSelectValueIDs, row.ID)
			valueDefinitions[row.ID] = row.DefinitionID
		}
	}
	optionIDsByValue, err := getOptionIDsForValues(s, multiSelectValueIDs, valueDefinitions)
	if err != nil {
		return err
	}

	// Load the destination definitions so number values can be rescaled to the
	// destination precision (a no-op when the precision is unchanged).
	destDefMap, err := destinationDefMapForCopy(s, rows, defRemap)
	if err != nil {
		return err
	}

	for _, row := range rows {
		def, ok := defMap[row.DefinitionID]
		if !ok {
			return missingCustomFieldDefinitionError(row)
		}
		newDefID, err := remapDefinitionID(row.DefinitionID, defRemap)
		if err != nil {
			return err
		}

		// Zero the identity and timestamps so XORM fills fresh values; the
		// source timestamps must not be preserved through the copy.
		newRow := *row
		newRow.ID = 0
		newRow.TaskID = destTaskID
		newRow.DefinitionID = newDefID
		newRow.Created = time.Time{}
		newRow.Updated = time.Time{}

		if def.FieldType == CustomFieldTypeSingleSelect && newRow.ValueSingleOptionID != nil {
			// A nil optionRemap means identity (same-project duplication).
			if optionRemap != nil {
				mapped, ok := optionRemap[*newRow.ValueSingleOptionID]
				if !ok {
					return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("No destination option for source option %d.", *newRow.ValueSingleOptionID)}
				}
				newRow.ValueSingleOptionID = &mapped
			}
		}

		// Rescale number values to the destination precision: the preflight
		// guaranteed representability, so the scaled integer changes (12.3 @ p1
		// = 123 becomes 1230 @ p2), never the value itself.
		if def.FieldType == CustomFieldTypeNumber && newRow.ValueNumber != nil {
			destDef, ok := destDefMap[newDefID]
			if !ok {
				return missingCustomFieldDefinitionError(row)
			}
			scaled, err := rescaleNumberValueForCopy(s, row, def, destDef)
			if err != nil {
				return err
			}
			newRow.ValueNumber = scaled
		}

		if _, err := s.Insert(&newRow); err != nil {
			return err
		}

		if def.FieldType == CustomFieldTypeMultiSelect {
			remapped, err := remapOptionIDs(optionIDsByValue[row.ID], optionRemap)
			if err != nil {
				return err
			}
			if err := replaceValueOptions(s, newRow.ID, remapped); err != nil {
				return err
			}
		}
	}
	return nil
}

// rescaleNumberValueForCopy re-scales a stored number value to the destination
// definition's precision. The preflight guaranteed representability, so the
// scaled integer changes, never the value itself.
func rescaleNumberValueForCopy(s *xorm.Session, row *TaskCustomFieldValue, srcDef, destDef *CustomFieldDefinition) (*int64, error) {
	value, err := row.FromRow(s, srcDef)
	if err != nil {
		return nil, err
	}
	if err := validateNumberValue(value, destDef); err != nil {
		return nil, err
	}
	scaled := value.Number.Value
	return &scaled, nil
}

// remapOptionIDs maps a slice of option ids through the remap table (nil means
// identity).
func remapOptionIDs(optionIDs []int64, optionRemap map[int64]int64) ([]int64, error) {
	remapped := make([]int64, 0, len(optionIDs))
	for _, optID := range optionIDs {
		mapped := optID
		if optionRemap != nil {
			m, ok := optionRemap[optID]
			if !ok {
				return nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("No destination option for source option %d.", optID)}
			}
			mapped = m
		}
		remapped = append(remapped, mapped)
	}
	return remapped, nil
}

// remapDefinitionID maps a source definition id through the remap table (nil
// means identity).
func remapDefinitionID(defID int64, defRemap map[int64]int64) (int64, error) {
	if defRemap == nil {
		return defID, nil
	}
	mapped, ok := defRemap[defID]
	if !ok {
		return 0, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("No destination definition for source definition %d.", defID)}
	}
	return mapped, nil
}

// destinationDefMapForCopy loads the destination definitions of the rows being
// copied, so number values can be rescaled to the destination precision.
func destinationDefMapForCopy(s *xorm.Session, rows []*TaskCustomFieldValue, defRemap map[int64]int64) (map[int64]*CustomFieldDefinition, error) {
	destDefIDs := make([]int64, 0, len(rows))
	seen := map[int64]struct{}{}
	for _, row := range rows {
		newDefID, err := remapDefinitionID(row.DefinitionID, defRemap)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[newDefID]; ok {
			continue
		}
		seen[newDefID] = struct{}{}
		destDefIDs = append(destDefIDs, newDefID)
	}
	destDefs, err := getCustomFieldDefinitionsByIDs(s, destDefIDs)
	if err != nil {
		return nil, err
	}
	destDefMap := make(map[int64]*CustomFieldDefinition, len(destDefs))
	for _, def := range destDefs {
		destDefMap[def.ID] = def
	}
	return destDefMap, nil
}
