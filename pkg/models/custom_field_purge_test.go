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
	"code.vikunja.io/api/pkg/notifications"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHardDeleteTaskPurgesCustomFields(t *testing.T) {
	t.Run("removes value rows and memberships", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		require.NoError(t, hardDeleteTask(s, &Task{ID: 2}))
		require.NoError(t, s.Commit())

		// No value rows reference the task, and no memberships reference the
		// deleted value rows (value row 7 held the multi-select memberships).
		db.AssertMissing(t, "custom_field_values", map[string]interface{}{"task_id": 2})
		db.AssertMissing(t, "custom_field_value_options", map[string]interface{}{"value_id": 7})
	})
}

func TestProjectDeleteRemovesDefinitionsAndOptions(t *testing.T) {
	t.Run("removes definitions, options, and values", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// Insert an inconsistent value row: a task in project 1 referencing
		// project 3's definition. Project deletion must still clean it up.
		row := &TaskCustomFieldValue{TaskID: 1, DefinitionID: 11}
		_, err := s.Insert(row)
		require.NoError(t, err)

		p := &Project{ID: 3}
		require.NoError(t, p.Delete(s, &user.User{ID: 1}))
		require.NoError(t, s.Commit())

		db.AssertMissing(t, "custom_field_definitions", map[string]interface{}{"project_id": 3})
		db.AssertMissing(t, "custom_field_options", map[string]interface{}{"definition_id": 11})
		// The inconsistent value row is gone too.
		db.AssertMissing(t, "custom_field_values", map[string]interface{}{"definition_id": 11})
	})
}

func TestDeleteUserUnsetsCustomFieldUserValues(t *testing.T) {
	t.Run("unsets user values and bumps task timestamps", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		notifications.Fake()
		t.Cleanup(notifications.Unfake)

		// A user-type value referencing user 4 on task 2 (project 1).
		uid := int64(4)
		row := &TaskCustomFieldValue{TaskID: 2, DefinitionID: 6, ValueUserID: &uid}
		_, err := s.Insert(row)
		require.NoError(t, err)

		task := &Task{}
		_, err = s.Where("id = ?", 2).Get(task)
		require.NoError(t, err)
		before := task.Updated

		require.NoError(t, DeleteUser(s, &user.User{ID: 4}))
		require.NoError(t, s.Commit())

		// The user-type value referencing the deleted user is gone.
		db.AssertMissing(t, "custom_field_values", map[string]interface{}{"task_id": 2, "definition_id": 6})
		// The affected task's updated timestamp advanced for ETag consistency.
		// A fresh struct avoids XORM's auto-condition filtering on the old value.
		taskAfter := &Task{}
		_, err = s.Where("id = ?", 2).Get(taskAfter)
		require.NoError(t, err)
		assert.True(t, taskAfter.Updated.After(before))
	})
}
