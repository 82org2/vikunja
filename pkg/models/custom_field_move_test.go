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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// grantWriteOnProject12 gives user 1 write access on project 12 so the move and
// cross-project duplicate integration tests can act on it. The grant lives in
// the test transaction, not the shared fixtures, so the project-access tests
// that assert user 1's inherited read on project 12 stay unchanged.
func grantWriteOnProject12(t *testing.T, s *xorm.Session) {
	t.Helper()
	_, err := s.Insert(&ProjectUser{ProjectID: 12, UserID: 1, Permission: PermissionWrite})
	require.NoError(t, err)
}

func TestPreflightTaskMoveCustomFields(t *testing.T) {
	t.Run("maps all value types", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// task 2 has impact (number), reviewed (boolean), tags (multi-select).
		defRemap, optionRemap, err := preflightTaskMoveCustomFields(s, 2, 12)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int64{1: 12, 4: 14, 5: 15}, defRemap)
		assert.Equal(t, map[int64]int64{3: 10, 4: 11}, optionRemap)
	})

	t.Run("maps single select and user", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// task 3 has status (single-select) and owner (user).
		defRemap, optionRemap, err := preflightTaskMoveCustomFields(s, 3, 12)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int64{3: 13, 6: 16}, defRemap)
		assert.Equal(t, map[int64]int64{2: 9}, optionRemap)
	})

	t.Run("no values", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		defRemap, optionRemap, err := preflightTaskMoveCustomFields(s, 4, 12)
		require.NoError(t, err)
		assert.Empty(t, defRemap)
		assert.Empty(t, optionRemap)
	})

	t.Run("type mismatch rejects", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// task 1 has target_date (date); project 12 defines target_date as
		// short_text, so the move must be rejected.
		_, _, err := preflightTaskMoveCustomFields(s, 1, 12)
		require.Error(t, err)
	})

	t.Run("missing destination definition rejects", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := &CustomFieldDefinition{
			ProjectID:  1,
			MachineKey: "extra",
			Title:      "Extra",
			FieldType:  CustomFieldTypeShortText,
		}
		require.NoError(t, def.Create(s, nil))
		value := "x"
		require.NoError(t, SetCustomFieldValue(s, 2, def, &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: &value}, nil))

		_, _, err := preflightTaskMoveCustomFields(s, 2, 12)
		require.Error(t, err)
	})

	t.Run("missing option key rejects", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		opt := &CustomFieldOption{
			DefinitionID: 4,
			MachineKey:   "mobile",
			Label:        "Mobile",
		}
		require.NoError(t, opt.Create(s, nil))
		require.NoError(t, SetCustomFieldValue(s, 2, &CustomFieldDefinition{ID: 4}, &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: []int64{opt.ID}}, nil))

		_, _, err := preflightTaskMoveCustomFields(s, 2, 12)
		require.Error(t, err)
	})

	t.Run("archived destination option is allowed", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// A retained value referencing the archived docs option (5) on task 3
		// maps to project 12's archived docs option (12): a move relocates a
		// retained value, so archived is accepted. The row is inserted directly
		// because set-time validation rejects archived options.
		row := &TaskCustomFieldValue{TaskID: 3, DefinitionID: 4}
		_, err := s.Insert(row)
		require.NoError(t, err)
		require.NoError(t, replaceValueOptions(s, row.ID, []int64{5}))

		defRemap, optionRemap, err := preflightTaskMoveCustomFields(s, 3, 12)
		require.NoError(t, err)
		assert.Equal(t, int64(14), defRemap[4])
		assert.Equal(t, int64(12), optionRemap[5])
	})
}

func TestBulkUpdateIgnoresUnmaskedProjectID(t *testing.T) {
	t.Run("does not preflight a move for an ignored project_id", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		u := &user.User{ID: 1}
		// Task 1 has custom-field values; project 2 has no matching definitions.
		// A bulk update that names only "title" must ignore the project_id in
		// the payload and not run the move preflight.
		task := &Task{ID: 1, Title: "renamed", ProjectID: 2}
		_, err := updateTasks(s, u, task, []int64{1}, []string{"title"})
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		// The task stayed in project 1 with the renamed title.
		db.AssertExists(t, "tasks", map[string]interface{}{"id": 1, "project_id": 1, "title": "renamed"}, false)
	})

	t.Run("permission check ignores a masked-out project_id", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		u := &user.User{ID: 1}
		// The full pipeline: CanUpdate must not require write access to the
		// masked-out destination project, and Update must not move the task.
		bt := &BulkTask{
			TaskIDs: []int64{1},
			Fields:  []string{"title"},
			Values:  &Task{Title: "renamed", ProjectID: 2},
		}
		can, err := bt.CanUpdate(s, u)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, bt.Update(s, u))
		require.NoError(t, s.Commit())

		db.AssertExists(t, "tasks", map[string]interface{}{"id": 1, "project_id": 1, "title": "renamed"}, false)
	})
}

func TestTaskMoveRewritesCustomFields(t *testing.T) {
	u := &user.User{ID: 1}

	t.Run("remaps values to destination definitions", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		grantWriteOnProject12(t, s)

		// Give the destination impact definition a different precision so the
		// move must rescale the stored number. The fixture keeps precision 1 so
		// cross-project filter consistency tests stay valid.
		precision := 2
		_, err := s.ID(12).Cols("configuration").Update(&CustomFieldDefinition{
			Configuration: &CustomFieldConfiguration{Precision: &precision},
		})
		require.NoError(t, err)

		task := &Task{ID: 2, ProjectID: 12}
		require.NoError(t, task.Update(s, u))
		require.NoError(t, s.Commit())

		// impact rescaled to precision 2: -25 @ p1 becomes -250 @ p2.
		db.AssertExists(t, "custom_field_values", map[string]interface{}{
			"task_id":       2,
			"definition_id": 12,
			"value_number":  int64(-250),
		}, false)
		// reviewed moved onto the archived destination definition.
		db.AssertExists(t, "custom_field_values", map[string]interface{}{
			"task_id":       2,
			"definition_id": 15,
			"value_boolean": true,
		}, false)
		// tags moved onto the destination definition with remapped memberships.
		row := &TaskCustomFieldValue{}
		exists, err := s.Where("task_id = ? AND definition_id = ?", 2, 14).Get(row)
		require.NoError(t, err)
		require.True(t, exists)
		optionIDs, err := getOptionIDsForValue(s, row.ID)
		require.NoError(t, err)
		assert.Equal(t, []int64{10, 11}, optionIDs)
		// The source-project value rows are gone.
		db.AssertMissing(t, "custom_field_values", map[string]interface{}{"task_id": 2, "definition_id": 1})
		db.AssertMissing(t, "custom_field_values", map[string]interface{}{"task_id": 2, "definition_id": 4})
		db.AssertMissing(t, "custom_field_values", map[string]interface{}{"task_id": 2, "definition_id": 5})
	})

	t.Run("rejects incompatible move and rolls back", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		grantWriteOnProject12(t, s)

		// task 1 has a target_date (date) value; project 12 defines target_date
		// as short_text, so the whole move is rejected before any mutation.
		task := &Task{ID: 1, ProjectID: 12}
		err := task.Update(s, u)
		require.Error(t, err)
		require.NoError(t, s.Rollback())

		db.AssertExists(t, "tasks", map[string]interface{}{"id": 1, "project_id": 1}, false)
		db.AssertExists(t, "custom_field_values", map[string]interface{}{
			"task_id":       1,
			"definition_id": 1,
			"value_number":  int64(75),
		}, false)
	})

	t.Run("move with no values is a no-op", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		grantWriteOnProject12(t, s)

		task := &Task{ID: 4, ProjectID: 12}
		require.NoError(t, task.Update(s, u))
		require.NoError(t, s.Commit())

		db.AssertExists(t, "tasks", map[string]interface{}{"id": 4, "project_id": 12}, false)
	})
}
