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
	"context"
	"encoding/json"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// flushPendingEvents mirrors the route handlers: after the model call and
// commit, pending events are dispatched into the events.Fake() recorder.
func flushPendingEvents(t *testing.T, s *xorm.Session) {
	t.Helper()
	events.DispatchPending(context.Background(), s)
}

func TestCustomFieldDefinitionEvents(t *testing.T) {
	usr := &user.User{ID: 1}

	t.Run("created", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def := &CustomFieldDefinition{
			ProjectID:  2,
			MachineKey: "estimate",
			Title:      "Estimate",
			FieldType:  CustomFieldTypeNumber,
		}
		require.NoError(t, def.Create(s, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		dispatched := events.GetDispatchedEvents((&CustomFieldDefinitionEvent{}).Name())
		require.Len(t, dispatched, 1)
		evt := dispatched[0].(*CustomFieldDefinitionEvent)
		assert.Equal(t, CustomFieldDefinitionCreated, evt.Semantic)
		assert.Equal(t, def.ID, evt.DefinitionID)
		assert.Equal(t, int64(2), evt.ProjectID)
		assert.Equal(t, "estimate", evt.MachineKey)
		assert.Equal(t, CustomFieldTypeNumber, evt.Type)
		assert.Nil(t, evt.OldDefinition)
		require.NotNil(t, evt.NewDefinition)
		assert.Equal(t, def.ID, evt.NewDefinition.ID)
		assert.Equal(t, int64(1), evt.Doer.ID)
		assert.Equal(t, 1, evt.Version)
		assert.NotEmpty(t, evt.EventID)
		assert.False(t, evt.Timestamp.IsZero())
	})

	t.Run("updated", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def, err := GetCustomFieldDefinitionByID(s, 1)
		require.NoError(t, err)
		def.Title = "Impact updated"
		require.NoError(t, def.Update(s, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		dispatched := events.GetDispatchedEvents((&CustomFieldDefinitionEvent{}).Name())
		require.Len(t, dispatched, 1)
		evt := dispatched[0].(*CustomFieldDefinitionEvent)
		assert.Equal(t, CustomFieldDefinitionUpdated, evt.Semantic)
		require.NotNil(t, evt.OldDefinition)
		require.NotNil(t, evt.NewDefinition)
		assert.Equal(t, "Impact", evt.OldDefinition.Title)
		assert.Equal(t, "Impact updated", evt.NewDefinition.Title)
	})

	t.Run("archived via delete", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def := &CustomFieldDefinition{ID: 1, ProjectID: 1}
		require.NoError(t, def.Delete(s, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		dispatched := events.GetDispatchedEvents((&CustomFieldDefinitionEvent{}).Name())
		require.Len(t, dispatched, 1)
		evt := dispatched[0].(*CustomFieldDefinitionEvent)
		assert.Equal(t, CustomFieldDefinitionArchived, evt.Semantic)
		require.NotNil(t, evt.OldDefinition)
		require.NotNil(t, evt.NewDefinition)
		assert.False(t, evt.OldDefinition.IsArchived)
		assert.True(t, evt.NewDefinition.IsArchived)
		// Old and new snapshots must be distinct structs, not aliases.
		assert.NotSame(t, evt.OldDefinition, evt.NewDefinition)
	})

	t.Run("unarchived via update", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def, err := GetCustomFieldDefinitionByID(s, 5) // archived boolean
		require.NoError(t, err)
		def.IsArchived = false
		require.NoError(t, def.Update(s, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		dispatched := events.GetDispatchedEvents((&CustomFieldDefinitionEvent{}).Name())
		require.Len(t, dispatched, 1)
		evt := dispatched[0].(*CustomFieldDefinitionEvent)
		assert.Equal(t, CustomFieldDefinitionUnarchived, evt.Semantic)
		require.NotNil(t, evt.OldDefinition)
		require.NotNil(t, evt.NewDefinition)
		assert.True(t, evt.OldDefinition.IsArchived)
		assert.False(t, evt.NewDefinition.IsArchived)
	})

	t.Run("permanently deleted", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def := &CustomFieldDefinition{ID: 8, ProjectID: 9} // no values
		require.NoError(t, def.DeletePermanently(s, false, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		dispatched := events.GetDispatchedEvents((&CustomFieldDefinitionEvent{}).Name())
		require.Len(t, dispatched, 1)
		evt := dispatched[0].(*CustomFieldDefinitionEvent)
		assert.Equal(t, CustomFieldDefinitionDeleted, evt.Semantic)
		require.NotNil(t, evt.OldDefinition)
		assert.Nil(t, evt.NewDefinition)
		assert.Equal(t, "category", evt.MachineKey)
	})

	t.Run("no-op update emits nothing", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def, err := GetCustomFieldDefinitionByID(s, 1)
		require.NoError(t, err)
		require.NoError(t, def.Update(s, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		assert.Equal(t, 0, events.CountDispatchedEvents((&CustomFieldDefinitionEvent{}).Name()))
	})

	t.Run("no-op update with unchanged numeric default emits nothing", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		precision := 1
		number := &CustomFieldNumber{}
		require.NoError(t, number.SetRaw("7.5"))
		def := &CustomFieldDefinition{
			ProjectID:     2,
			MachineKey:    "estimate",
			Title:         "Estimate",
			FieldType:     CustomFieldTypeNumber,
			Configuration: &CustomFieldConfiguration{Precision: &precision},
			DefaultValue:  &CustomFieldValue{Type: CustomFieldTypeNumber, Number: number},
		}
		require.NoError(t, def.Create(s, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		events.ClearDispatchedEvents()
		loaded, err := GetCustomFieldDefinitionByID(s, def.ID)
		require.NoError(t, err)
		require.NoError(t, loaded.Update(s, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		assert.Equal(t, 0, events.CountDispatchedEvents((&CustomFieldDefinitionEvent{}).Name()))
	})
}

func TestCustomFieldValueEvents(t *testing.T) {
	usr := &user.User{ID: 1}

	t.Run("set", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def, err := GetCustomFieldDefinitionByID(s, 2) // date, no value for task 2
		require.NoError(t, err)
		value := &CustomFieldValue{Type: CustomFieldTypeDate, Date: &CustomFieldDate{DayCount: 18000}}
		require.NoError(t, SetCustomFieldValue(s, 2, def, value, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		dispatched := events.GetDispatchedEvents((&CustomFieldValueEvent{}).Name())
		require.Len(t, dispatched, 1)
		evt := dispatched[0].(*CustomFieldValueEvent)
		assert.Equal(t, CustomFieldValueSet, evt.Semantic)
		assert.Nil(t, evt.OldValue)
		require.NotNil(t, evt.NewValue)
		assert.Equal(t, int64(2), evt.TaskID)
		assert.Equal(t, int64(1), evt.ProjectID)
		assert.Equal(t, int64(2), evt.DefinitionID)
		assert.Equal(t, "target_date", evt.MachineKey)
		assert.Equal(t, CustomFieldTypeDate, evt.Type)
		assert.Equal(t, int64(1), evt.Doer.ID)
		assert.Equal(t, 1, evt.Version)
		assert.NotEmpty(t, evt.EventID)
		assert.False(t, evt.Timestamp.IsZero())
		// Exactly one compatibility task.updated event.
		assert.Equal(t, 1, events.CountDispatchedEvents((&TaskUpdatedEvent{}).Name()))
	})

	t.Run("changed", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def, err := GetCustomFieldDefinitionByID(s, 1) // number, task 1 has 75
		require.NoError(t, err)
		number := &CustomFieldNumber{}
		require.NoError(t, number.SetRaw("8.0"))
		require.NoError(t, SetCustomFieldValue(s, 1, def, &CustomFieldValue{Type: CustomFieldTypeNumber, Number: number}, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		dispatched := events.GetDispatchedEvents((&CustomFieldValueEvent{}).Name())
		require.Len(t, dispatched, 1)
		evt := dispatched[0].(*CustomFieldValueEvent)
		assert.Equal(t, CustomFieldValueChanged, evt.Semantic)
		require.NotNil(t, evt.OldValue)
		require.NotNil(t, evt.NewValue)
		assert.Equal(t, int64(75), evt.OldValue.Number.Value)
		assert.Equal(t, int64(80), evt.NewValue.Number.Value)
		assert.Equal(t, 1, events.CountDispatchedEvents((&TaskUpdatedEvent{}).Name()))
	})

	t.Run("idempotent set writes nothing and emits nothing", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def, err := GetCustomFieldDefinitionByID(s, 1) // number, task 1 has 75
		require.NoError(t, err)
		number := &CustomFieldNumber{}
		require.NoError(t, number.SetRaw("7.5")) // same value as the fixture
		require.NoError(t, SetCustomFieldValue(s, 1, def, &CustomFieldValue{Type: CustomFieldTypeNumber, Number: number}, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		assert.Equal(t, 0, events.CountDispatchedEvents((&CustomFieldValueEvent{}).Name()))
		assert.Equal(t, 0, events.CountDispatchedEvents((&TaskUpdatedEvent{}).Name()))
		// The task timestamp must not have advanced.
		task, err := GetTaskByIDSimple(s, 1)
		require.NoError(t, err)
		assert.True(t, task.Updated.Equal(time.Date(2018, 12, 1, 1, 12, 4, 0, time.UTC)), "task updated must not advance on an idempotent set")
	})

	t.Run("unset", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		require.NoError(t, UnsetCustomFieldValue(s, 1, 1, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		dispatched := events.GetDispatchedEvents((&CustomFieldValueEvent{}).Name())
		require.Len(t, dispatched, 1)
		evt := dispatched[0].(*CustomFieldValueEvent)
		assert.Equal(t, CustomFieldValueUnset, evt.Semantic)
		require.NotNil(t, evt.OldValue)
		assert.Nil(t, evt.NewValue)
		assert.Equal(t, int64(75), evt.OldValue.Number.Value)
		assert.Equal(t, int64(1), evt.ProjectID)
		assert.Equal(t, "impact", evt.MachineKey)
		assert.Equal(t, 1, events.CountDispatchedEvents((&TaskUpdatedEvent{}).Name()))
	})

	t.Run("empty multi select unsets", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def, err := GetCustomFieldDefinitionByID(s, 4) // multi-select, task 2 has [3,4]
		require.NoError(t, err)
		require.NoError(t, SetCustomFieldValue(s, 2, def, &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: []int64{}}, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		dispatched := events.GetDispatchedEvents((&CustomFieldValueEvent{}).Name())
		require.Len(t, dispatched, 1)
		evt := dispatched[0].(*CustomFieldValueEvent)
		assert.Equal(t, CustomFieldValueUnset, evt.Semantic)
		require.NotNil(t, evt.OldValue)
		assert.Nil(t, evt.NewValue)
		assert.ElementsMatch(t, []int64{3, 4}, evt.OldValue.OptionIDs)
		assert.Equal(t, 1, events.CountDispatchedEvents((&TaskUpdatedEvent{}).Name()))
	})

	t.Run("unset of an unset field emits nothing", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		require.NoError(t, UnsetCustomFieldValue(s, 1, 3, usr)) // no value row for task 1, def 3
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		assert.Equal(t, 0, events.CountDispatchedEvents((&CustomFieldValueEvent{}).Name()))
		assert.Equal(t, 0, events.CountDispatchedEvents((&TaskUpdatedEvent{}).Name()))
	})

	t.Run("defaults materialisation emits nothing", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		events.ClearDispatchedEvents()
		def := &CustomFieldDefinition{
			ProjectID:    2,
			MachineKey:   "region",
			Title:        "Region",
			FieldType:    CustomFieldTypeShortText,
			DefaultValue: &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: strPtr("EU")},
		}
		require.NoError(t, def.Create(s, usr))

		task := &Task{Title: "First", ProjectID: 2}
		require.NoError(t, task.Create(s, usr))
		require.NoError(t, s.Commit())
		flushPendingEvents(t, s)

		assert.Equal(t, 0, events.CountDispatchedEvents((&CustomFieldValueEvent{}).Name()))
		assert.Equal(t, 0, events.CountDispatchedEvents((&TaskUpdatedEvent{}).Name()))
	})
}

func TestCustomFieldEventPayloadShape(t *testing.T) {
	t.Run("value event is flat", func(t *testing.T) {
		evt := &CustomFieldValueEvent{
			EventID:      "evt-1",
			Version:      1,
			Semantic:     CustomFieldValueSet,
			ProjectID:    1,
			TaskID:       2,
			DefinitionID: 3,
			MachineKey:   "impact",
			Type:         CustomFieldTypeNumber,
			OldValue:     nil,
			NewValue:     &CustomFieldValue{Type: CustomFieldTypeNumber, Number: &CustomFieldNumber{Value: 75, Precision: 1}},
			Doer:         &user.User{ID: 1},
			Timestamp:    time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC),
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)

		var m map[string]interface{}
		require.NoError(t, json.Unmarshal(data, &m))
		assert.Equal(t, "evt-1", m["event_id"])
		assert.InDelta(t, 1, m["version"], 0)
		assert.Equal(t, "set", m["semantic"])
		assert.InDelta(t, 1, m["project_id"], 0)
		assert.InDelta(t, 2, m["task_id"], 0)
		assert.InDelta(t, 3, m["definition_id"], 0)
		assert.Equal(t, "impact", m["machine_key"])
		assert.Equal(t, "number", m["type"])
		assert.Nil(t, m["old_value"])
		assert.NotNil(t, m["new_value"])
		assert.NotNil(t, m["doer"])
		assert.NotNil(t, m["timestamp"])
	})

	t.Run("definition event is flat", func(t *testing.T) {
		evt := &CustomFieldDefinitionEvent{
			EventID:       "evt-2",
			Version:       1,
			Semantic:      CustomFieldDefinitionDeleted,
			ProjectID:     9,
			DefinitionID:  8,
			MachineKey:    "category",
			Type:          CustomFieldTypeSingleSelect,
			OldDefinition: &CustomFieldDefinition{ID: 8, ProjectID: 9, MachineKey: "category", FieldType: CustomFieldTypeSingleSelect},
			NewDefinition: nil,
			Doer:          &user.User{ID: 1},
			Timestamp:     time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC),
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)

		var m map[string]interface{}
		require.NoError(t, json.Unmarshal(data, &m))
		assert.Equal(t, "evt-2", m["event_id"])
		assert.Equal(t, "deleted", m["semantic"])
		assert.InDelta(t, 9, m["project_id"], 0)
		assert.InDelta(t, 8, m["definition_id"], 0)
		assert.Equal(t, "category", m["machine_key"])
		assert.Equal(t, "single_select", m["type"])
		assert.NotNil(t, m["old_definition"])
		assert.Nil(t, m["new_definition"])
	})
}

func TestCustomFieldWebhookListenerRouting(t *testing.T) {
	t.Run("value event routes to project webhook with flat payload", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		wh := &Webhook{
			TargetURL:   "https://example.com/custom-field-webhook",
			Events:      []string{"custom_field.value.changed.v1"},
			ProjectID:   1,
			CreatedByID: 1,
		}
		_, err := s.Insert(wh)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		require.NoError(t, s.Close())

		events.ClearDispatchedEvents()
		events.TestListener(t, &CustomFieldValueEvent{
			EventID:      "evt-1",
			Version:      1,
			Semantic:     CustomFieldValueSet,
			ProjectID:    1,
			TaskID:       1,
			DefinitionID: 1,
			MachineKey:   "impact",
			Type:         CustomFieldTypeNumber,
			NewValue:     &CustomFieldValue{Type: CustomFieldTypeNumber, Number: &CustomFieldNumber{Value: 75, Precision: 1}},
			Doer:         &user.User{ID: 1},
			Timestamp:    time.Now(),
		}, &WebhookListener{EventName: "custom_field.value.changed.v1"})

		deliveries := events.GetDispatchedEvents((&WebhookDeliveryEvent{}).Name())
		require.Len(t, deliveries, 1)
		delivery := deliveries[0].(*WebhookDeliveryEvent)
		assert.Equal(t, wh.ID, delivery.WebhookID)
		assert.Equal(t, "custom_field.value.changed.v1", delivery.Payload.EventName)
		data, ok := delivery.Payload.Data.(map[string]interface{})
		require.True(t, ok)
		assert.InDelta(t, 1, data["project_id"], 0)
		_, hasProject := data["project"]
		assert.False(t, hasProject, "flat-ID events must not get an injected project object")
	})

	t.Run("ancestor webhook receives flat payload", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		wh := &Webhook{
			TargetURL:   "https://example.com/ancestor-webhook",
			Events:      []string{"custom_field.value.changed.v1"},
			ProjectID:   27, // parent of project 12
			CreatedByID: 1,
		}
		_, err := s.Insert(wh)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		require.NoError(t, s.Close())

		events.ClearDispatchedEvents()
		events.TestListener(t, &CustomFieldValueEvent{
			EventID:      "evt-ancestor",
			Version:      1,
			Semantic:     CustomFieldValueSet,
			ProjectID:    12,
			TaskID:       1,
			DefinitionID: 12,
			MachineKey:   "impact",
			Type:         CustomFieldTypeNumber,
			NewValue:     &CustomFieldValue{Type: CustomFieldTypeNumber, Number: &CustomFieldNumber{Value: 75, Precision: 1}},
			Doer:         &user.User{ID: 1},
			Timestamp:    time.Now(),
		}, &WebhookListener{EventName: "custom_field.value.changed.v1"})

		deliveries := events.GetDispatchedEvents((&WebhookDeliveryEvent{}).Name())
		require.Len(t, deliveries, 1)
		delivery := deliveries[0].(*WebhookDeliveryEvent)
		assert.Equal(t, wh.ID, delivery.WebhookID)
		data, ok := delivery.Payload.Data.(map[string]interface{})
		require.True(t, ok)
		assert.InDelta(t, 12, data["project_id"], 0)
		_, hasProject := data["project"]
		assert.False(t, hasProject, "flat-ID events must not get an injected project object")
	})

	t.Run("definition event routes to project webhook", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		wh := &Webhook{
			TargetURL:   "https://example.com/custom-field-def-webhook",
			Events:      []string{"custom_field.definition.changed.v1"},
			ProjectID:   1,
			CreatedByID: 1,
		}
		_, err := s.Insert(wh)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		require.NoError(t, s.Close())

		events.ClearDispatchedEvents()
		events.TestListener(t, &CustomFieldDefinitionEvent{
			EventID:       "evt-def",
			Version:       1,
			Semantic:      CustomFieldDefinitionCreated,
			ProjectID:     1,
			DefinitionID:  1,
			MachineKey:    "impact",
			Type:          CustomFieldTypeNumber,
			NewDefinition: &CustomFieldDefinition{ID: 1, ProjectID: 1, MachineKey: "impact", FieldType: CustomFieldTypeNumber},
			Doer:          &user.User{ID: 1},
			Timestamp:     time.Now(),
		}, &WebhookListener{EventName: "custom_field.definition.changed.v1"})

		deliveries := events.GetDispatchedEvents((&WebhookDeliveryEvent{}).Name())
		require.Len(t, deliveries, 1)
		delivery := deliveries[0].(*WebhookDeliveryEvent)
		assert.Equal(t, wh.ID, delivery.WebhookID)
		assert.Equal(t, "custom_field.definition.changed.v1", delivery.Payload.EventName)
	})
}
