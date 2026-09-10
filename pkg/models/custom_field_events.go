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
	"slices"
	"time"

	"code.vikunja.io/api/pkg/user"
)

// CustomFieldDefinitionEventSemantic enumerates the lifecycle transitions of a
// custom field definition. The semantic discriminates one versioned event name
// so consumers subscribe once and filter by it.
type CustomFieldDefinitionEventSemantic string

const (
	CustomFieldDefinitionCreated    CustomFieldDefinitionEventSemantic = "created"
	CustomFieldDefinitionUpdated    CustomFieldDefinitionEventSemantic = "updated"
	CustomFieldDefinitionArchived   CustomFieldDefinitionEventSemantic = "archived"
	CustomFieldDefinitionUnarchived CustomFieldDefinitionEventSemantic = "unarchived"
	CustomFieldDefinitionDeleted    CustomFieldDefinitionEventSemantic = "deleted"
)

// CustomFieldDefinitionEvent is fired when a definition is created, updated,
// archived, unarchived, or permanently deleted. OldDefinition is null for
// created, NewDefinition is null for deleted; both are populated otherwise.
// The flat project_id identifies the source project for webhook routing.
type CustomFieldDefinitionEvent struct {
	EventID       string                             `json:"event_id" doc:"A stable identifier for this event, generated once at dispatch. Consumers use it to deduplicate retries."`
	Version       int                                `json:"version" doc:"The event schema version. Bump the event name (custom_field.definition.changed.v2) when the payload changes."`
	Semantic      CustomFieldDefinitionEventSemantic `json:"semantic" doc:"The lifecycle transition: created, updated, archived, unarchived, or deleted."`
	ProjectID     int64                              `json:"project_id" doc:"The id of the project the definition belongs to."`
	DefinitionID  int64                              `json:"definition_id" doc:"The id of the definition."`
	MachineKey    string                             `json:"machine_key" doc:"The immutable machine key of the definition."`
	Type          CustomFieldType                    `json:"type" doc:"The immutable field type of the definition."`
	OldDefinition *CustomFieldDefinition             `json:"old_definition" doc:"The definition before the transition, or null for created."`
	NewDefinition *CustomFieldDefinition             `json:"new_definition" doc:"The definition after the transition, or null for deleted."`
	Doer          *user.User                         `json:"doer" doc:"The user who performed the transition."`
	Timestamp     time.Time                          `json:"timestamp" doc:"When the transition happened."`
}

// Name defines the name for CustomFieldDefinitionEvent
func (e *CustomFieldDefinitionEvent) Name() string {
	return "custom_field.definition.changed.v1"
}

// CustomFieldValueEventSemantic enumerates the value transitions of a task's
// custom field value.
type CustomFieldValueEventSemantic string

const (
	CustomFieldValueSet     CustomFieldValueEventSemantic = "set"
	CustomFieldValueChanged CustomFieldValueEventSemantic = "changed"
	CustomFieldValueUnset   CustomFieldValueEventSemantic = "unset"
)

// CustomFieldValueEvent is fired when a task's custom field value is set,
// changed, or unset. OldValue is null for set, NewValue is null for unset; both
// are populated for changed. The flat project_id and task_id identify the
// source task for webhook routing.
type CustomFieldValueEvent struct {
	EventID      string                        `json:"event_id" doc:"A stable identifier for this event, generated once at dispatch. Consumers use it to deduplicate retries."`
	Version      int                           `json:"version" doc:"The event schema version. Bump the event name (custom_field.value.changed.v2) when the payload changes."`
	Semantic     CustomFieldValueEventSemantic `json:"semantic" doc:"The value transition: set, changed, or unset."`
	ProjectID    int64                         `json:"project_id" doc:"The id of the project the task belongs to."`
	TaskID       int64                         `json:"task_id" doc:"The id of the task the value belongs to."`
	DefinitionID int64                         `json:"definition_id" doc:"The id of the definition the value belongs to."`
	MachineKey   string                        `json:"machine_key" doc:"The immutable machine key of the definition."`
	Type         CustomFieldType               `json:"type" doc:"The immutable field type of the definition."`
	OldValue     *CustomFieldValue             `json:"old_value" doc:"The value before the transition, or null for set."`
	NewValue     *CustomFieldValue             `json:"new_value" doc:"The value after the transition, or null for unset."`
	Doer         *user.User                    `json:"doer" doc:"The user who performed the transition."`
	Timestamp    time.Time                     `json:"timestamp" doc:"When the transition happened."`
}

// Name defines the name for CustomFieldValueEvent
func (e *CustomFieldValueEvent) Name() string {
	return "custom_field.value.changed.v1"
}

// Equals reports whether two values are semantically equal: the same type and
// the same typed value. Multi-select compares option id sets
// order-independently. It is used to detect an idempotent set, which must not
// write or emit an event.
func (v *CustomFieldValue) Equals(other *CustomFieldValue) bool {
	if v == nil || other == nil {
		return v == other
	}
	if v.Type != other.Type {
		return false
	}
	switch v.Type {
	case CustomFieldTypeShortText:
		return ptrEqual(v.ShortText, other.ShortText)
	case CustomFieldTypeLongText:
		return ptrEqual(v.LongText, other.LongText)
	case CustomFieldTypeNumber:
		return v.Number != nil && other.Number != nil &&
			v.Number.Value == other.Number.Value && v.Number.Precision == other.Number.Precision
	case CustomFieldTypeBoolean:
		return ptrEqual(v.Boolean, other.Boolean)
	case CustomFieldTypeDate:
		return v.Date != nil && other.Date != nil && v.Date.DayCount == other.Date.DayCount
	case CustomFieldTypeDateTime:
		return v.DateTime != nil && other.DateTime != nil && v.DateTime.Equal(*other.DateTime)
	case CustomFieldTypeURL:
		return ptrEqual(v.URL, other.URL)
	case CustomFieldTypeUser:
		return ptrEqual(v.UserID, other.UserID)
	case CustomFieldTypeSingleSelect:
		return ptrEqual(v.SingleOptionID, other.SingleOptionID)
	case CustomFieldTypeMultiSelect:
		return optionIDSetEqual(v.OptionIDs, other.OptionIDs)
	}
	return false
}

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// optionIDSetEqual compares two multi-select option id slices as sets: order
// is not significant because memberships are stored in insertion order while a
// client may send any order.
func optionIDSetEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	aa := slices.Clone(a)
	bb := slices.Clone(b)
	slices.Sort(aa)
	slices.Sort(bb)
	return slices.Equal(aa, bb)
}
