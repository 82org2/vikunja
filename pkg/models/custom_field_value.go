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

	"code.vikunja.io/api/pkg/web"
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
func SetCustomFieldValue(s *xorm.Session, taskID int64, def *CustomFieldDefinition, value *CustomFieldValue) error {
	if value == nil {
		return ErrInvalidCustomFieldValue{Message: "A custom field value is required."}
	}
	if def == nil {
		return ErrInvalidCustomFieldValue{Message: "A custom field definition is required."}
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
	// nonexistent definition fails the normal validation first.
	if value.Type == CustomFieldTypeMultiSelect && value.OptionIDs != nil && len(value.OptionIDs) == 0 && value.populatedFields() == 0 {
		return UnsetCustomFieldValue(s, taskID, persistedDef.ID)
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

	return nil
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
			if err := SetCustomFieldValue(s, task.ID, def, &value); err != nil {
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
func UnsetCustomFieldValue(s *xorm.Session, taskID, definitionID int64) error {
	row, err := GetCustomFieldValue(s, taskID, definitionID)
	if err != nil {
		if IsErrCustomFieldValueDoesNotExist(err) {
			return nil
		}
		return err
	}

	if _, err = s.Where("value_id = ?", row.ID).Delete(&CustomFieldValueOption{}); err != nil {
		return err
	}
	if _, err = s.Where("id = ?", row.ID).Delete(&TaskCustomFieldValue{}); err != nil {
		return err
	}

	return updateTaskLastUpdated(s, &Task{ID: taskID})
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
