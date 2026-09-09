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
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func TestSetCustomFieldValue(t *testing.T) {
	t.Run("set number", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def, err := GetCustomFieldDefinitionByID(s, 1)
		require.NoError(t, err)

		number := &CustomFieldNumber{}
		require.NoError(t, number.SetRaw("7.5"))
		err = SetCustomFieldValue(s, 2, def, &CustomFieldValue{Type: CustomFieldTypeNumber, Number: number})
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		db.AssertExists(t, "custom_field_values", map[string]interface{}{
			"task_id":       2,
			"definition_id": 1,
			"value_number":  int64(75),
		}, false)
	})

	t.Run("upsert replaces existing value", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def, err := GetCustomFieldDefinitionByID(s, 1)
		require.NoError(t, err)

		// task 1 already has value_number 75 for definition 1; writing -25 must replace it.
		number := &CustomFieldNumber{}
		require.NoError(t, number.SetRaw("-2.5"))
		err = SetCustomFieldValue(s, 1, def, &CustomFieldValue{Type: CustomFieldTypeNumber, Number: number})
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		db.AssertCount(t, "custom_field_values", builder.Eq{"task_id": 1, "definition_id": 1}, 1)
		db.AssertExists(t, "custom_field_values", map[string]interface{}{
			"task_id":       1,
			"definition_id": 1,
			"value_number":  int64(-25),
		}, false)
	})

	t.Run("multi select replaces memberships", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def, err := GetCustomFieldDefinitionByID(s, 4)
		require.NoError(t, err)

		// task 2 already has tags [3, 4] through value id 7; replacing drops option 4.
		err = SetCustomFieldValue(s, 2, def, &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: []int64{3}})
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		db.AssertCount(t, "custom_field_value_options", builder.Eq{"value_id": 7}, 1)
		db.AssertExists(t, "custom_field_value_options", map[string]interface{}{
			"value_id":  int64(7),
			"option_id": int64(3),
		}, false)
		db.AssertMissing(t, "custom_field_value_options", map[string]interface{}{
			"value_id":  int64(7),
			"option_id": int64(4),
		})
	})

	t.Run("rejects value not matching definition type", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def, err := GetCustomFieldDefinitionByID(s, 1) // number
		require.NoError(t, err)

		err = SetCustomFieldValue(s, 1, def, &CustomFieldValue{Type: CustomFieldTypeBoolean, Boolean: boolPtr(true)})
		require.Error(t, err)
	})

	t.Run("rejects a definition from another project", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def, err := GetCustomFieldDefinitionByID(s, 7) // project 2
		require.NoError(t, err)
		err = SetCustomFieldValue(s, 1, def, &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: strPtr("nope")})
		require.Error(t, err)
	})

	t.Run("rejects an archived definition", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def, err := GetCustomFieldDefinitionByID(s, 5)
		require.NoError(t, err)
		err = SetCustomFieldValue(s, 1, def, &CustomFieldValue{Type: CustomFieldTypeBoolean, Boolean: boolPtr(true)})
		require.Error(t, err)
	})

	t.Run("empty multi select unsets", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def, err := GetCustomFieldDefinitionByID(s, 4)
		require.NoError(t, err)
		err = SetCustomFieldValue(s, 2, def, &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: []int64{}})
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		db.AssertMissing(t, "custom_field_values", map[string]interface{}{
			"task_id":       int64(2),
			"definition_id": int64(4),
		})
		db.AssertMissing(t, "custom_field_value_options", map[string]interface{}{"value_id": int64(7)})
	})
}

func TestUnsetCustomFieldValue(t *testing.T) {
	t.Run("removes value row and memberships", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// task 2 definition 4 has a multi-select value with two memberships.
		require.NoError(t, UnsetCustomFieldValue(s, 2, 4))
		require.NoError(t, s.Commit())

		db.AssertMissing(t, "custom_field_values", map[string]interface{}{
			"task_id":       2,
			"definition_id": 4,
		})
		db.AssertMissing(t, "custom_field_value_options", map[string]interface{}{
			"value_id": int64(7),
		})
	})

	t.Run("unsetting an unset field is a no-op", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		require.NoError(t, UnsetCustomFieldValue(s, 1, 3)) // no value row for task 1, definition 3
		require.NoError(t, s.Commit())
	})
}

func TestMaterialiseCustomFieldDefaults(t *testing.T) {
	usr := &user.User{ID: 1}

	t.Run("defaults materialised on task create", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// A definition with a default on project 2.
		def := &CustomFieldDefinition{
			ProjectID:    2,
			MachineKey:   "category",
			Title:        "Category",
			FieldType:    CustomFieldTypeShortText,
			DefaultValue: &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: strPtr("General")},
		}
		require.NoError(t, def.Create(s, usr))

		task := &Task{Title: "Defaulted", ProjectID: 2}
		require.NoError(t, task.Create(s, usr))
		require.NoError(t, s.Commit())

		db.AssertExists(t, "custom_field_values", map[string]interface{}{
			"task_id":          task.ID,
			"definition_id":    def.ID,
			"value_short_text": "General",
		}, false)
	})

	t.Run("archived definitions materialise nothing", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := &CustomFieldDefinition{
			ProjectID:    2,
			MachineKey:   "legacy",
			Title:        "Legacy",
			FieldType:    CustomFieldTypeShortText,
			DefaultValue: &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: strPtr("Old")},
		}
		require.NoError(t, def.Create(s, usr))
		// xorm only updates bools when they are named explicitly via Cols.
		_, err := s.Where("id = ?", def.ID).Cols("is_archived").Update(&CustomFieldDefinition{IsArchived: true})
		require.NoError(t, err)

		task := &Task{Title: "No default", ProjectID: 2}
		require.NoError(t, task.Create(s, usr))
		require.NoError(t, s.Commit())

		db.AssertMissing(t, "custom_field_values", map[string]interface{}{
			"task_id":       task.ID,
			"definition_id": def.ID,
		})
	})

	t.Run("changing a default is not retroactive", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := &CustomFieldDefinition{
			ProjectID:    2,
			MachineKey:   "region",
			Title:        "Region",
			FieldType:    CustomFieldTypeShortText,
			DefaultValue: &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: strPtr("EU")},
		}
		require.NoError(t, def.Create(s, usr))

		first := &Task{Title: "First", ProjectID: 2}
		require.NoError(t, first.Create(s, usr))

		// Change the default, then create a second task.
		require.NoError(t, def.Update(s, usr))
		def.DefaultValue = &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: strPtr("US")}
		_, err := s.Where("id = ?", def.ID).Cols("default_value").Update(def)
		require.NoError(t, err)

		second := &Task{Title: "Second", ProjectID: 2}
		require.NoError(t, second.Create(s, usr))
		require.NoError(t, s.Commit())

		db.AssertExists(t, "custom_field_values", map[string]interface{}{
			"task_id":          first.ID,
			"definition_id":    def.ID,
			"value_short_text": "EU",
		}, false)
		db.AssertExists(t, "custom_field_values", map[string]interface{}{
			"task_id":          second.ID,
			"definition_id":    def.ID,
			"value_short_text": "US",
		}, false)
	})

	t.Run("unset does not reapply the default", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := &CustomFieldDefinition{
			ProjectID:    2,
			MachineKey:   "channel",
			Title:        "Channel",
			FieldType:    CustomFieldTypeShortText,
			DefaultValue: &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: strPtr("Web")},
		}
		require.NoError(t, def.Create(s, usr))

		task := &Task{Title: "Unset me", ProjectID: 2}
		require.NoError(t, task.Create(s, usr))

		require.NoError(t, UnsetCustomFieldValue(s, task.ID, def.ID))
		require.NoError(t, s.Commit())

		db.AssertMissing(t, "custom_field_values", map[string]interface{}{
			"task_id":       task.ID,
			"definition_id": def.ID,
		})
	})
}

func TestTaskCustomFieldValueFromRow(t *testing.T) {
	t.Run("number", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def, err := GetCustomFieldDefinitionByID(s, 1)
		require.NoError(t, err)
		row, err := GetCustomFieldValue(s, 1, 1)
		require.NoError(t, err)

		val, err := row.FromRow(s, def)
		require.NoError(t, err)
		require.Equal(t, CustomFieldTypeNumber, val.Type)
		require.NotNil(t, val.Number)
		require.Equal(t, int64(75), val.Number.Value)
		require.Equal(t, 1, val.Number.Precision)
	})

	t.Run("multi select loads memberships in option position order", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// The fixture inserts option 3 before option 4. Flip their positions so
		// the response order can only come from the option position, not the
		// membership insertion order.
		_, err := s.ID(3).Cols("position").Update(&CustomFieldOption{Position: 2})
		require.NoError(t, err)
		_, err = s.ID(4).Cols("position").Update(&CustomFieldOption{Position: 1})
		require.NoError(t, err)

		def, err := GetCustomFieldDefinitionByID(s, 4)
		require.NoError(t, err)
		row, err := GetCustomFieldValue(s, 2, 4)
		require.NoError(t, err)

		val, err := row.FromRow(s, def)
		require.NoError(t, err)
		require.Equal(t, CustomFieldTypeMultiSelect, val.Type)
		require.Equal(t, []int64{4, 3}, val.OptionIDs)
	})
}
