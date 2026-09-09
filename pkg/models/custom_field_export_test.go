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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func TestAddCustomFieldsToExport(t *testing.T) {
	t.Run("attaches definitions, options, and values", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		projects := []*ProjectWithTasksAndBuckets{
			{
				Project: Project{ID: 1},
				Tasks: []*TaskWithComments{
					{Task: Task{ID: 2}},
					{Task: Task{ID: 3}},
				},
			},
		}
		require.NoError(t, addCustomFieldsToExport(s, projects, []int64{1}, []int64{2, 3}))

		p := projects[0]
		require.Len(t, p.CustomFieldDefinitions, 6)
		byKey := map[string]*CustomFieldDefinitionExport{}
		for _, def := range p.CustomFieldDefinitions {
			byKey[def.MachineKey] = def
		}
		require.NotNil(t, byKey["impact"].Configuration)
		assert.Equal(t, 1, *byKey["impact"].Configuration.Precision)

		// Options carry the authoritative definition machine key.
		optByKey := map[string]*CustomFieldOptionExport{}
		for _, opt := range p.CustomFieldOptions {
			optByKey[opt.MachineKey] = opt
		}
		assert.Equal(t, "tags", optByKey["backend"].DefinitionMachineKey)

		// Task 2's values carry option machine keys.
		task2 := p.Tasks[0]
		require.Len(t, task2.CustomFieldValues, 3)
		valByKey := map[string]*CustomFieldTaskValueExport{}
		for _, v := range task2.CustomFieldValues {
			valByKey[v.MachineKey] = v
		}
		assert.Equal(t, []string{"backend", "frontend"}, valByKey["tags"].Value.OptionKeys)

		// Task 3's user value carries a stable identity.
		task3 := p.Tasks[1]
		valByKey3 := map[string]*CustomFieldTaskValueExport{}
		for _, v := range task3.CustomFieldValues {
			valByKey3[v.MachineKey] = v
		}
		// The user value carries a stable username, never an email.
		require.NotNil(t, valByKey3["owner"].Value.UserUsername)
		assert.Equal(t, "user1", *valByKey3["owner"].Value.UserUsername)
		assert.Nil(t, valByKey3["owner"].Value.UserEmail)
	})
}

func TestImportCustomFieldsForProject(t *testing.T) {
	numberValue := func(raw string) *CustomFieldValueExport {
		number := &CustomFieldNumber{}
		require.NoError(t, number.SetRaw(raw))
		return &CustomFieldValueExport{Value: &CustomFieldValue{Type: CustomFieldTypeNumber, Number: number}}
	}

	t.Run("creates definitions, options, and values", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		defs := []*CustomFieldDefinitionExport{
			{
				MachineKey:    "impact",
				FieldType:     CustomFieldTypeNumber,
				Title:         "Impact",
				Configuration: &CustomFieldConfiguration{Precision: intPtr(1)},
			},
			{
				MachineKey: "status",
				FieldType:  CustomFieldTypeSingleSelect,
				Title:      "Status",
			},
		}
		options := []*CustomFieldOptionExport{
			{DefinitionMachineKey: "status", MachineKey: "done", Label: "Done"},
		}
		taskValues := map[int64][]*CustomFieldTaskValueExport{
			1: {
				{MachineKey: "impact", Value: numberValue("7.5")},
				{MachineKey: "status", Value: &CustomFieldValueExport{Value: &CustomFieldValue{Type: CustomFieldTypeSingleSelect}, SingleOptionKey: strPtr("done")}},
			},
		}

		require.NoError(t, ImportCustomFieldsForProject(s, 4, defs, options, taskValues))
		require.NoError(t, s.Commit())

		db.AssertExists(t, "custom_field_definitions", map[string]interface{}{"project_id": 4, "machine_key": "impact"}, false)
		statusDef := &CustomFieldDefinition{}
		_, err := s.Where("project_id = ? AND machine_key = ?", 4, "status").Get(statusDef)
		require.NoError(t, err)
		db.AssertExists(t, "custom_field_options", map[string]interface{}{"definition_id": statusDef.ID, "machine_key": "done"}, false)
		// The number value is stored scaled at the definition's precision.
		db.AssertExists(t, "custom_field_values", map[string]interface{}{"task_id": 1, "definition_id": statusDef.ID}, false)
		impactDef := &CustomFieldDefinition{}
		_, err = s.Where("project_id = ? AND machine_key = ?", 4, "impact").Get(impactDef)
		require.NoError(t, err)
		db.AssertExists(t, "custom_field_values", map[string]interface{}{"task_id": 1, "definition_id": impactDef.ID, "value_number": int64(75)}, false)
	})

	t.Run("reuses an existing definition by key", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := &CustomFieldDefinition{
			ProjectID:  4,
			MachineKey: "impact",
			Title:      "Impact",
			FieldType:  CustomFieldTypeNumber,
		}
		require.NoError(t, def.Create(s, nil))

		defs := []*CustomFieldDefinitionExport{
			{MachineKey: "impact", FieldType: CustomFieldTypeNumber, Title: "Impact"},
		}
		require.NoError(t, ImportCustomFieldsForProject(s, 4, defs, nil, nil))
		require.NoError(t, s.Commit())

		db.AssertCount(t, "custom_field_definitions", builder.Eq{"project_id": 4, "machine_key": "impact"}, 1)
	})

	t.Run("rejects a definition with a different type", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := &CustomFieldDefinition{
			ProjectID:  4,
			MachineKey: "impact",
			Title:      "Impact",
			FieldType:  CustomFieldTypeShortText,
		}
		require.NoError(t, def.Create(s, nil))

		defs := []*CustomFieldDefinitionExport{
			{MachineKey: "impact", FieldType: CustomFieldTypeNumber, Title: "Impact"},
		}
		err := ImportCustomFieldsForProject(s, 4, defs, nil, nil)
		require.Error(t, err)
	})

	t.Run("rejects an unresolvable user value", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		defs := []*CustomFieldDefinitionExport{
			{MachineKey: "owner", FieldType: CustomFieldTypeUser, Title: "Owner"},
		}
		email := "nobody@example.com"
		taskValues := map[int64][]*CustomFieldTaskValueExport{
			1: {
				{MachineKey: "owner", Value: &CustomFieldValueExport{Value: &CustomFieldValue{Type: CustomFieldTypeUser}, UserEmail: &email}},
			},
		}
		// An unresolvable user value must reject the import rather than be
		// silently dropped, and the rollback leaves no partial definitions.
		err := ImportCustomFieldsForProject(s, 4, defs, nil, taskValues)
		require.Error(t, err)
		require.NoError(t, s.Rollback())
		db.AssertMissing(t, "custom_field_definitions", map[string]interface{}{"project_id": 4})
	})

	t.Run("rejects a nil value", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		defs := []*CustomFieldDefinitionExport{
			{MachineKey: "impact", FieldType: CustomFieldTypeNumber, Title: "Impact"},
		}
		taskValues := map[int64][]*CustomFieldTaskValueExport{
			1: {{MachineKey: "impact", Value: nil}},
		}
		err := ImportCustomFieldsForProject(s, 4, defs, nil, taskValues)
		require.Error(t, err)
	})

	t.Run("failed import leaves no partial definitions", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// A valid definition followed by an option referencing an unknown
		// definition: the import fails after creating the definition, and the
		// transaction rollback leaves nothing behind.
		defs := []*CustomFieldDefinitionExport{
			{MachineKey: "impact", FieldType: CustomFieldTypeNumber, Title: "Impact"},
		}
		options := []*CustomFieldOptionExport{
			{DefinitionMachineKey: "unknown", MachineKey: "x", Label: "X"},
		}
		err := ImportCustomFieldsForProject(s, 4, defs, options, nil)
		require.Error(t, err)
		require.NoError(t, s.Rollback())

		db.AssertMissing(t, "custom_field_definitions", map[string]interface{}{"project_id": 4})
	})
}
