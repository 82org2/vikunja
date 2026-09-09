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
	"code.vikunja.io/api/pkg/files"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskDuplicateCopiesCustomFields(t *testing.T) {
	t.Run("copies values and memberships", func(t *testing.T) {
		files.InitTestFileFixtures(t)
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		u := &user.User{ID: 1}
		td := &TaskDuplicate{TaskID: 2}
		require.NoError(t, td.Create(s, u))
		require.NoError(t, s.Commit())

		newID := td.Task.ID
		// Same definitions, same option ids, including the archived-def value.
		db.AssertExists(t, "custom_field_values", map[string]interface{}{"task_id": newID, "definition_id": 1, "value_number": int64(-25)}, false)
		db.AssertExists(t, "custom_field_values", map[string]interface{}{"task_id": newID, "definition_id": 5, "value_boolean": true}, false)
		row := &TaskCustomFieldValue{}
		exists, err := s.Where("task_id = ? AND definition_id = ?", newID, 4).Get(row)
		require.NoError(t, err)
		require.True(t, exists)
		optionIDs, err := getOptionIDsForValue(s, row.ID)
		require.NoError(t, err)
		assert.Equal(t, []int64{3, 4}, optionIDs)
		// Timestamps are regenerated, not copied from the source.
		assert.False(t, row.Created.IsZero())
	})

	t.Run("copies single-select values", func(t *testing.T) {
		files.InitTestFileFixtures(t)
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		u := &user.User{ID: 1}
		td := &TaskDuplicate{TaskID: 3}
		require.NoError(t, td.Create(s, u))
		require.NoError(t, s.Commit())

		newID := td.Task.ID
		// The single-select value keeps its option id (identity mapping).
		db.AssertExists(t, "custom_field_values", map[string]interface{}{
			"task_id":                newID,
			"definition_id":          3,
			"value_single_option_id": int64(2),
		}, false)
	})

	t.Run("unset fields stay unset", func(t *testing.T) {
		files.InitTestFileFixtures(t)
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		u := &user.User{ID: 1}

		// A definition with a default that task 2 has no value on: the duplicate
		// must not keep the default materialized by task creation.
		def := &CustomFieldDefinition{
			ProjectID:  1,
			MachineKey: "with_default",
			Title:      "With Default",
			FieldType:  CustomFieldTypeShortText,
		}
		require.NoError(t, def.Create(s, nil))
		value := "dflt"
		def.DefaultValue = &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: &value}
		require.NoError(t, validateValueAgainstDefinition(s, def.DefaultValue, def))
		_, err := s.ID(def.ID).Cols("default_value").Update(&CustomFieldDefinition{DefaultValue: def.DefaultValue})
		require.NoError(t, err)

		td := &TaskDuplicate{TaskID: 2}
		require.NoError(t, td.Create(s, u))
		require.NoError(t, s.Commit())

		db.AssertMissing(t, "custom_field_values", map[string]interface{}{"task_id": td.Task.ID, "definition_id": def.ID})
	})
}

func TestProjectDuplicateCopiesCustomFields(t *testing.T) {
	t.Run("clones definitions and options", func(t *testing.T) {
		files.InitTestFileFixtures(t)
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		u := &user.User{ID: 1}
		l := &ProjectDuplicate{ProjectID: 1, DuplicateShares: true}
		_, err := l.CanCreate(s, u)
		require.NoError(t, err)
		require.NoError(t, l.Create(s, u))
		require.NoError(t, s.Commit())

		newProjectID := l.Project.ID
		defs := []*CustomFieldDefinition{}
		require.NoError(t, s.Where("project_id = ?", newProjectID).Find(&defs))
		require.Len(t, defs, 6)
		byKey := map[string]*CustomFieldDefinition{}
		for _, def := range defs {
			byKey[def.MachineKey] = def
		}
		// Same keys, types, configuration, and archived state; new ids.
		assert.NotEqual(t, int64(1), byKey["impact"].ID)
		assert.Equal(t, CustomFieldTypeNumber, byKey["impact"].FieldType)
		require.NotNil(t, byKey["impact"].Configuration)
		assert.Equal(t, 1, *byKey["impact"].Configuration.Precision)
		assert.True(t, byKey["reviewed"].IsArchived)

		// Options cloned with remapped definition ids and the same machine keys.
		options := []*CustomFieldOption{}
		require.NoError(t, s.Where("definition_id = ?", byKey["tags"].ID).Find(&options))
		require.Len(t, options, 3)
		optByKey := map[string]*CustomFieldOption{}
		for _, opt := range options {
			optByKey[opt.MachineKey] = opt
		}
		assert.Equal(t, "Backend", optByKey["backend"].Label)
		assert.True(t, optByKey["docs"].IsArchived)
	})

	t.Run("remaps task values onto cloned definitions", func(t *testing.T) {
		files.InitTestFileFixtures(t)
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		u := &user.User{ID: 1}
		l := &ProjectDuplicate{ProjectID: 1, DuplicateShares: true}
		_, err := l.CanCreate(s, u)
		require.NoError(t, err)
		require.NoError(t, l.Create(s, u))
		require.NoError(t, s.Commit())

		newProjectID := l.Project.ID

		// Find the duplicated task that corresponds to old task 2 (same title).
		oldTask := &Task{}
		_, err = s.Where("id = ?", 2).Get(oldTask)
		require.NoError(t, err)
		newTask := &Task{}
		exists, err := s.Where("project_id = ? AND title = ?", newProjectID, oldTask.Title).Get(newTask)
		require.NoError(t, err)
		require.True(t, exists)

		// The new task's value rows all reference definitions of the new project.
		rows := []*TaskCustomFieldValue{}
		require.NoError(t, s.Where("task_id = ?", newTask.ID).Find(&rows))
		require.Len(t, rows, 3) // impact, reviewed, tags
		for _, row := range rows {
			def := &CustomFieldDefinition{}
			exists, err := s.Where("id = ? AND project_id = ?", row.DefinitionID, newProjectID).Get(def)
			require.NoError(t, err)
			require.True(t, exists, "value row %d references a definition outside the new project", row.ID)
		}

		// The tags memberships reference options of the new project's tags def.
		tagsDef := &CustomFieldDefinition{}
		_, err = s.Where("project_id = ? AND machine_key = ?", newProjectID, "tags").Get(tagsDef)
		require.NoError(t, err)
		tagsRow := &TaskCustomFieldValue{}
		exists, err = s.Where("task_id = ? AND definition_id = ?", newTask.ID, tagsDef.ID).Get(tagsRow)
		require.NoError(t, err)
		require.True(t, exists)
		optionIDs, err := getOptionIDsForValue(s, tagsRow.ID)
		require.NoError(t, err)
		require.Len(t, optionIDs, 2)
		for _, optID := range optionIDs {
			opt := &CustomFieldOption{}
			exists, err := s.Where("id = ? AND definition_id = ?", optID, tagsDef.ID).Get(opt)
			require.NoError(t, err)
			require.True(t, exists, "membership %d references an option outside the new project", optID)
		}
	})
}

func TestTaskDuplicateCrossProject(t *testing.T) {
	t.Run("remaps values into the destination project", func(t *testing.T) {
		files.InitTestFileFixtures(t)
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		grantWriteOnProject12(t, s)

		// Give the destination impact definition a different precision so the
		// copy must rescale the stored number.
		precision := 2
		_, err := s.ID(12).Cols("configuration").Update(&CustomFieldDefinition{
			Configuration: &CustomFieldConfiguration{Precision: &precision},
		})
		require.NoError(t, err)

		u := &user.User{ID: 1}
		td := &TaskDuplicate{TaskID: 2, ProjectID: 12}
		require.NoError(t, td.Create(s, u))
		require.NoError(t, s.Commit())

		newID := td.Task.ID
		// impact rescaled to precision 2.
		db.AssertExists(t, "custom_field_values", map[string]interface{}{"task_id": newID, "definition_id": 12, "value_number": int64(-250)}, false)
		// reviewed moved onto the archived destination definition.
		db.AssertExists(t, "custom_field_values", map[string]interface{}{"task_id": newID, "definition_id": 15, "value_boolean": true}, false)
		// tags memberships remapped onto the destination options.
		row := &TaskCustomFieldValue{}
		exists, err := s.Where("task_id = ? AND definition_id = ?", newID, 14).Get(row)
		require.NoError(t, err)
		require.True(t, exists)
		optionIDs, err := getOptionIDsForValue(s, row.ID)
		require.NoError(t, err)
		assert.Equal(t, []int64{10, 11}, optionIDs)
	})

	t.Run("rejects an incompatible destination", func(t *testing.T) {
		files.InitTestFileFixtures(t)
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		grantWriteOnProject12(t, s)

		u := &user.User{ID: 1}
		// task 1 has a target_date (date) value; project 12 defines it as
		// short_text, so the duplication is rejected before any mutation.
		td := &TaskDuplicate{TaskID: 1, ProjectID: 12}
		err := td.Create(s, u)
		require.Error(t, err)
		require.NoError(t, s.Rollback())
	})
}

func TestValidateProjectCustomFieldUserValues(t *testing.T) {
	t.Run("pages through more than 500 rows", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// Project 1 has def 6 (owner, user type). Insert 600 user-type value
		// rows referencing user 1 (visible in project 1) with distinct task
		// ids, so the ID-cursor pagination must walk more than one page.
		rows := make([]*TaskCustomFieldValue, 0, 600)
		for i := 0; i < 600; i++ {
			uid := int64(1)
			rows = append(rows, &TaskCustomFieldValue{TaskID: int64(1000 + i), DefinitionID: 6, ValueUserID: &uid})
		}
		_, err := s.Insert(&rows)
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		require.NoError(t, validateProjectCustomFieldUserValues(s, 1))
	})

	t.Run("catches an invisible user across pages", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// 599 rows reference user 1; the last one references user 2, who is not
		// visible in project 1. The pagination must not skip it.
		rows := make([]*TaskCustomFieldValue, 0, 600)
		for i := 0; i < 599; i++ {
			uid := int64(1)
			rows = append(rows, &TaskCustomFieldValue{TaskID: int64(1000 + i), DefinitionID: 6, ValueUserID: &uid})
		}
		uid := int64(2)
		rows = append(rows, &TaskCustomFieldValue{TaskID: int64(2000), DefinitionID: 6, ValueUserID: &uid})
		_, err := s.Insert(&rows)
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		err = validateProjectCustomFieldUserValues(s, 1)
		require.Error(t, err)
	})
}
