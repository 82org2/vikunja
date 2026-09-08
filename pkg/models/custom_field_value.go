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
	"time"

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

// --- reads ---

func getCustomFieldValue(s *xorm.Session, taskID, definitionID int64) (*TaskCustomFieldValue, error) {
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

	persistedDef, err := getCustomFieldDefinitionByID(s, def.ID)
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

	if value.Type == CustomFieldTypeMultiSelect && value.OptionIDs != nil && len(value.OptionIDs) == 0 && value.populatedFields() == 0 {
		return UnsetCustomFieldValue(s, taskID, persistedDef.ID)
	}
	if err := value.validate(); err != nil {
		return err
	}
	if err := validateValueAgainstDefinition(s, value, persistedDef); err != nil {
		return err
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
	row, err := getCustomFieldValue(s, taskID, definitionID)
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

// fromRow reconstructs the discriminated value from the row, loading memberships
// for multi-select values.
func (v *TaskCustomFieldValue) fromRow(s *xorm.Session, def *CustomFieldDefinition) (*CustomFieldValue, error) {
	value := &CustomFieldValue{Type: def.FieldType}

	switch def.FieldType {
	case CustomFieldTypeShortText:
		value.ShortText = v.ValueShortText
	case CustomFieldTypeLongText:
		value.LongText = v.ValueLongText
	case CustomFieldTypeNumber:
		number := &CustomFieldNumber{Value: *v.ValueNumber, Precision: numberPrecision(def)}
		number.raw = formatFixedPoint(number.Value, number.Precision)
		value.Number = number
	case CustomFieldTypeBoolean:
		value.Boolean = v.ValueBoolean
	case CustomFieldTypeDate:
		value.Date = &CustomFieldDate{DayCount: *v.ValueDate}
	case CustomFieldTypeDateTime:
		value.DateTime = v.ValueDateTime
	case CustomFieldTypeURL:
		value.URL = v.ValueURL
	case CustomFieldTypeUser:
		value.UserID = v.ValueUserID
	case CustomFieldTypeSingleSelect:
		value.SingleOptionID = v.ValueSingleOptionID
	case CustomFieldTypeMultiSelect:
		optionIDs, err := getOptionIDsForValue(s, v.ID)
		if err != nil {
			return nil, err
		}
		value.OptionIDs = optionIDs
	}

	return value, nil
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
