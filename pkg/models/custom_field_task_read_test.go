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
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm/log"
)

func TestAddCustomFieldsToTasks(t *testing.T) {
	t.Run("orders values by definition position", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		taskMap := map[int64]*Task{1: {ID: 1, ProjectID: 1}}
		require.NoError(t, addCustomFieldsToTasks(s, []int64{1}, taskMap))

		require.Len(t, taskMap[1].CustomFields, 2)
		assert.Equal(t, int64(1), taskMap[1].CustomFields[0].DefinitionID)
		assert.Equal(t, "impact", taskMap[1].CustomFields[0].MachineKey)
		require.NotNil(t, taskMap[1].CustomFields[0].Value.Number)
		assert.Equal(t, int64(75), taskMap[1].CustomFields[0].Value.Number.Value)

		assert.Equal(t, int64(2), taskMap[1].CustomFields[1].DefinitionID)
		assert.Equal(t, "target_date", taskMap[1].CustomFields[1].MachineKey)
		require.NotNil(t, taskMap[1].CustomFields[1].Value.Date)
		assert.Equal(t, int64(17862), taskMap[1].CustomFields[1].Value.Date.DayCount)
	})

	t.Run("archived definitions still describe retained values", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		taskMap := map[int64]*Task{2: {ID: 2, ProjectID: 1}}
		require.NoError(t, addCustomFieldsToTasks(s, []int64{2}, taskMap))

		found := false
		for _, entry := range taskMap[2].CustomFields {
			if entry.DefinitionID == 5 {
				found = true
				require.Equal(t, CustomFieldTypeBoolean, entry.Value.Type)
				require.NotNil(t, entry.Value.Boolean)
				assert.True(t, *entry.Value.Boolean)
			}
		}
		assert.True(t, found, "the archived definition's value must still be loaded")
	})

	t.Run("multi select follows option position", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		taskMap := map[int64]*Task{2: {ID: 2, ProjectID: 1}}
		require.NoError(t, addCustomFieldsToTasks(s, []int64{2}, taskMap))

		for _, entry := range taskMap[2].CustomFields {
			if entry.DefinitionID == 4 {
				require.Equal(t, CustomFieldTypeMultiSelect, entry.Value.Type)
				assert.Equal(t, []int64{3, 4}, entry.Value.OptionIDs)
			}
		}
	})

	t.Run("tasks without values get an empty non-nil slice", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		taskMap := map[int64]*Task{4: {ID: 4, ProjectID: 1}}
		require.NoError(t, addCustomFieldsToTasks(s, []int64{4}, taskMap))
		require.NotNil(t, taskMap[4].CustomFields, "a non-nil empty slice lets the v2 wrapper serialize custom_fields: []")
		assert.Empty(t, taskMap[4].CustomFields)
	})

	t.Run("archived options still describe retained memberships", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// Option 5 (docs) of definition 4 is archived; a retained membership to
		// it must still load, ordered after the active options by position.
		_, err := s.Insert(&CustomFieldValueOption{ValueID: 7, OptionID: 5})
		require.NoError(t, err)

		taskMap := map[int64]*Task{2: {ID: 2, ProjectID: 1}}
		require.NoError(t, addCustomFieldsToTasks(s, []int64{2}, taskMap))

		for _, entry := range taskMap[2].CustomFields {
			if entry.DefinitionID == 4 {
				require.Equal(t, CustomFieldTypeMultiSelect, entry.Value.Type)
				assert.Equal(t, []int64{3, 4, 5}, entry.Value.OptionIDs)
			}
		}
	})
}

func TestAddCustomFieldsToTasks_QueryCountIsBounded(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	// Give many tasks in project 1 a multi-select value so a per-value
	// membership query would scale linearly with the input. Task 2 is skipped:
	// it already carries three fixture values.
	multiSelectDef, err := GetCustomFieldDefinitionByID(s, 4)
	require.NoError(t, err)
	taskIDs := []int64{}
	for _, id := range []int64{5, 6, 7, 8, 9, 10, 11, 12} {
		err := SetCustomFieldValue(s, id, multiSelectDef, &CustomFieldValue{
			Type:      CustomFieldTypeMultiSelect,
			OptionIDs: []int64{3, 4},
		}, nil)
		require.NoError(t, err)
		taskIDs = append(taskIDs, id)
	}

	taskMap := map[int64]*Task{}
	for _, id := range taskIDs {
		task, err := GetTaskByIDSimple(s, id)
		require.NoError(t, err)
		taskMap[id] = &task
	}

	// Count SQL statements for the loader only.
	engine := s.Engine()
	prevLogger := engine.Logger()
	defer engine.SetLogger(prevLogger)
	logger := &sqlCountingLogger{}
	engine.SetLogger(logger)
	s.MustLogSQL(true)

	require.NoError(t, addCustomFieldsToTasks(s, taskIDs, taskMap))

	for _, id := range taskIDs {
		require.Len(t, taskMap[id].CustomFields, 1)
		require.Equal(t, CustomFieldTypeMultiSelect, taskMap[id].CustomFields[0].Value.Type)
		assert.Equal(t, []int64{3, 4}, taskMap[id].CustomFields[0].Value.OptionIDs)
	}

	// 8 multi-select values, but the loader issues a constant number of queries:
	// values, definitions, memberships, options. A per-value pair of queries
	// would land in the high teens here.
	require.LessOrEqual(t, logger.count, 6, "multi-select expansion must stay bounded regardless of value count")
	require.Positive(t, logger.count, "the loader must actually issue queries")
}

func TestAddCustomFieldsToTasks_MalformedRowsFailLoudly(t *testing.T) {
	t.Run("missing definition", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		_, err := s.Insert(&TaskCustomFieldValue{TaskID: 1, DefinitionID: 99999, ValueShortText: strPtr("orphan")})
		require.NoError(t, err)

		taskMap := map[int64]*Task{1: {ID: 1, ProjectID: 1}}
		err = addCustomFieldsToTasks(s, []int64{1}, taskMap)
		require.Error(t, err, "a value row whose definition is gone must fail loudly, not be discarded")
	})

	t.Run("definition from another project", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// Definition 7 belongs to project 2; task 1 lives in project 1. A value
		// row linking them is corrupt regardless of the definition existing.
		_, err := s.Insert(&TaskCustomFieldValue{TaskID: 1, DefinitionID: 7, ValueShortText: strPtr("cross-project")})
		require.NoError(t, err)

		taskMap := map[int64]*Task{1: {ID: 1, ProjectID: 1}}
		err = addCustomFieldsToTasks(s, []int64{1}, taskMap)
		require.Error(t, err, "a value row referencing another project's definition must fail loudly")
	})

	t.Run("multi-select membership references a missing option", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		_, err := s.Insert(&CustomFieldValueOption{ValueID: 7, OptionID: 99999})
		require.NoError(t, err)

		taskMap := map[int64]*Task{2: {ID: 2, ProjectID: 1}}
		err = addCustomFieldsToTasks(s, []int64{2}, taskMap)
		require.Error(t, err, "a membership referencing a nonexistent option must fail loudly")
	})

	t.Run("multi-select membership references an option from another definition", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// Option 1 belongs to definition 3, not definition 4.
		_, err := s.Insert(&CustomFieldValueOption{ValueID: 7, OptionID: 1})
		require.NoError(t, err)

		taskMap := map[int64]*Task{2: {ID: 2, ProjectID: 1}}
		err = addCustomFieldsToTasks(s, []int64{2}, taskMap)
		require.Error(t, err, "a membership referencing another definition's option must fail loudly")
	})

	t.Run("multi-select value row with no memberships", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// A multi-select value row with no memberships is corrupt: an empty
		// selection must be unset, which deletes the row.
		_, err := s.Insert(&TaskCustomFieldValue{TaskID: 5, DefinitionID: 4})
		require.NoError(t, err)

		taskMap := map[int64]*Task{5: {ID: 5, ProjectID: 1}}
		err = addCustomFieldsToTasks(s, []int64{5}, taskMap)
		require.Error(t, err, "a multi-select value row without memberships must fail loudly")
	})
}

// TestGetTasksInBucketsForView_OverlappingFilterBucketsKeepCustomFields is a
// regression test for overlapping filter buckets: the same task can land in two
// buckets as separate pointers, and every copy must carry the expanded custom
// fields, not just the last one the task map kept.
func TestGetTasksInBucketsForView_OverlappingFilterBucketsKeepCustomFields(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	// Two filter buckets whose filters both match task 1 (not done, has custom
	// field values), so the same task lands in both buckets as distinct pointers.
	view := &ProjectView{
		Title:                   "Overlap",
		ProjectID:               1,
		ViewKind:                ProjectViewKindKanban,
		BucketConfigurationMode: BucketConfigurationModeFilter,
		BucketConfiguration: []*ProjectViewBucketConfiguration{
			{Title: "Not done A", Filter: &TaskCollection{Filter: "done = false"}},
			{Title: "Not done B", Filter: &TaskCollection{Filter: "done = false"}},
		},
	}
	require.NoError(t, view.Create(s, &user.User{ID: 1}))
	require.NoError(t, s.Commit())

	view, err := GetProjectViewByIDAndProject(s, view.ID, 1)
	require.NoError(t, err)

	project, err := GetProjectSimpleByID(s, 1)
	require.NoError(t, err)

	opts := &taskSearchOptions{expand: []TaskCollectionExpandable{TaskCollectionExpandCustomFields}}
	buckets, err := GetTasksInBucketsForView(s, view, []*Project{project}, opts, &user.User{ID: 1})
	require.NoError(t, err)

	var task1Copies []*Task
	for _, b := range buckets {
		task1InBucket := 0
		for _, tsk := range b.Tasks {
			// Every copy must carry the id of the bucket it is actually placed
			// in; a struct-copy that overwrites BucketID would land both copies
			// in one bucket and leave the other without the task.
			require.Equal(t, b.ID, tsk.BucketID, "task %d in bucket %d must carry that bucket's id", tsk.ID, b.ID)
			require.NotNil(t, tsk.CustomFields, "every bucket copy must carry a custom fields slice when expanded")
			if tsk.ID == 1 {
				task1InBucket++
				task1Copies = append(task1Copies, tsk)
				require.Len(t, tsk.CustomFields, 2, "the overlapping task 1 copy must carry its custom field values")
			}
		}
		require.Equal(t, 1, task1InBucket, "each overlapping bucket must contain task 1 exactly once", b.ID)
	}
	require.Len(t, task1Copies, 2, "task 1 must appear in both overlapping buckets")
}

// sqlCountingLogger implements xorm's log.Logger to count the SQL statements a
// session runs, enabled per-session via Session.MustLogSQL.
type sqlCountingLogger struct {
	count int
}

func (l *sqlCountingLogger) Infof(format string, _ ...any) {
	if strings.Contains(format, "[SQL]") {
		l.count++
	}
}
func (l *sqlCountingLogger) Debug(...any)          {}
func (l *sqlCountingLogger) Debugf(string, ...any) {}
func (l *sqlCountingLogger) Error(...any)          {}
func (l *sqlCountingLogger) Errorf(string, ...any) {}
func (l *sqlCountingLogger) Info(...any)           {}
func (l *sqlCountingLogger) Warn(...any)           {}
func (l *sqlCountingLogger) Warnf(string, ...any)  {}
func (l *sqlCountingLogger) Level() log.LogLevel   { return log.LOG_INFO }
func (l *sqlCountingLogger) SetLevel(log.LogLevel) {}
func (l *sqlCountingLogger) ShowSQL(_ ...bool)     {}
func (l *sqlCountingLogger) IsShowSQL() bool       { return true }

func TestTaskReadOneIncludesCustomFields(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	task := &Task{ID: 1, ExpandCustomFields: true}
	require.NoError(t, task.ReadOne(s, &user.User{ID: 1}))

	require.Len(t, task.CustomFields, 2)
	assert.Equal(t, int64(1), task.CustomFields[0].DefinitionID)
	assert.Equal(t, "impact", task.CustomFields[0].MachineKey)
	assert.False(t, task.ProjectUpdated.IsZero(), "ProjectUpdated must be captured for the v2 single-task ETag")
}
