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
	"xorm.io/builder"
)

func TestParseCustomFieldFilter(t *testing.T) {
	t.Run("number greater than", func(t *testing.T) {
		result, err := getTaskFiltersFromFilterString("custom_fields.impact > 5", "UTC", true)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "custom_fields.impact", result[0].field)
		assert.Equal(t, "impact", result[0].customFieldKey)
		assert.Equal(t, taskFilterComparatorGreater, result[0].comparator)
		assert.Equal(t, "5", result[0].value)
	})
	t.Run("in", func(t *testing.T) {
		result, err := getTaskFiltersFromFilterString("custom_fields.status in 1,2", "UTC", true)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "status", result[0].customFieldKey)
		assert.Equal(t, taskFilterComparatorIn, result[0].comparator)
	})
	t.Run("like", func(t *testing.T) {
		result, err := getTaskFiltersFromFilterString("custom_fields.vendor like 'acme'", "UTC", true)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "vendor", result[0].customFieldKey)
		assert.Equal(t, taskFilterComparatorLike, result[0].comparator)
	})
	t.Run("is null", func(t *testing.T) {
		result, err := getTaskFiltersFromFilterString("custom_fields.foo is null", "UTC", true)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "foo", result[0].customFieldKey)
		assert.Equal(t, taskFilterComparatorIsNull, result[0].comparator)
	})
	t.Run("is not null", func(t *testing.T) {
		result, err := getTaskFiltersFromFilterString("custom_fields.foo is not null", "UTC", true)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "foo", result[0].customFieldKey)
		assert.Equal(t, taskFilterComparatorIsNotNull, result[0].comparator)
	})
	t.Run("combined with regular fields", func(t *testing.T) {
		result, err := getTaskFiltersFromFilterString("done = false && custom_fields.impact > 5", "UTC", true)
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, "done", result[0].field)
		assert.Equal(t, "impact", result[1].customFieldKey)
	})
	t.Run("invalid machine key", func(t *testing.T) {
		_, err := getTaskFiltersFromFilterString("custom_fields.Impact > 5", "UTC", true)
		require.Error(t, err)
	})
	t.Run("v1 rejects custom fields", func(t *testing.T) {
		_, err := getTaskFiltersFromFilterString("custom_fields.impact > 5", "UTC", false)
		require.Error(t, err)
		assert.True(t, IsErrInvalidTaskField(err))
	})
}

func TestNullRewriteSafety(t *testing.T) {
	t.Run("quoted is null stays a string", func(t *testing.T) {
		result, err := getTaskFiltersFromFilterString("title = 'is null'", "UTC", true)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, taskFilterComparatorEquals, result[0].comparator)
		assert.Equal(t, "is null", result[0].value)
	})
	t.Run("bare null stays a string", func(t *testing.T) {
		result, err := getTaskFiltersFromFilterString("title = null", "UTC", true)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, taskFilterComparatorEquals, result[0].comparator)
		assert.Equal(t, "null", result[0].value)
	})
	t.Run("regular field is null", func(t *testing.T) {
		result, err := getTaskFiltersFromFilterString("due_date is null", "UTC", true)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, taskFilterComparatorIsNull, result[0].comparator)
	})
}

func TestCustomFieldLikeEscapesWildcards(t *testing.T) {
	cond, err := buildCustomFieldValueCond("value_short_text", taskFilterComparatorLike, "50%_!")
	require.NoError(t, err)
	sql, args, err := builder.ToSQL(cond)
	require.NoError(t, err)
	assert.Equal(t, "value_short_text LIKE ? ESCAPE '!'", sql)
	assert.Equal(t, []interface{}{"%50!%!_!!%"}, args)
}

func readTaskIDs(t *testing.T, tc *TaskCollection, userID int64) []int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	// A user session is always allowed to filter/sort by custom fields.
	tc.CustomFieldFilterScopeSatisfied = true
	result, _, _, err := tc.ReadAll(s, &user.User{ID: userID}, "", 1, 50)
	require.NoError(t, err)
	tasks, ok := result.([]*Task)
	require.True(t, ok, "expected []*Task, got %T", result)
	ids := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func TestCustomFieldFilterSQL(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	t.Run("number greater than", func(t *testing.T) {
		// Definition 1 (impact) has precision 1: value_number 75 = 7.5.
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "custom_fields.impact > 5", AllowCustomFieldFilters: true}, 1)
		assert.Contains(t, ids, int64(1), "task 1 has impact 7.5")
		assert.NotContains(t, ids, int64(2), "task 2 has impact -2.5")
	})
	t.Run("not equals excludes missing values", func(t *testing.T) {
		// != 7.5 scales to value_number != 75. Task 1 (75) excluded; task 2 (-25) matches.
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "custom_fields.impact != 7.5", AllowCustomFieldFilters: true}, 1)
		assert.NotContains(t, ids, int64(1), "task 1 has impact 7.5")
		assert.Contains(t, ids, int64(2), "task 2 has impact -2.5")
	})
	t.Run("is null excludes tasks with values", func(t *testing.T) {
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "custom_fields.impact is null", AllowCustomFieldFilters: true}, 1)
		assert.NotContains(t, ids, int64(1), "task 1 has impact 7.5")
		assert.NotContains(t, ids, int64(2), "task 2 has impact -2.5")
	})
	t.Run("is not null includes tasks with values", func(t *testing.T) {
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "custom_fields.impact is not null", AllowCustomFieldFilters: true}, 1)
		assert.Contains(t, ids, int64(1))
		assert.Contains(t, ids, int64(2))
	})
	t.Run("single select in", func(t *testing.T) {
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "custom_fields.status in 1,2", AllowCustomFieldFilters: true}, 1)
		assert.Contains(t, ids, int64(3), "task 3 has status option 2")
	})
	t.Run("multi select in", func(t *testing.T) {
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "custom_fields.tags in 3,4", AllowCustomFieldFilters: true}, 1)
		assert.Contains(t, ids, int64(2), "task 2 has tags backend(3) and frontend(4)")
	})
	t.Run("multi select not in excludes a matching task", func(t *testing.T) {
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "custom_fields.tags not in 3", AllowCustomFieldFilters: true}, 1)
		assert.NotContains(t, ids, int64(2), "task 2 has tag backend(3)")
	})
	t.Run("short text like", func(t *testing.T) {
		// Project 2 is owned by user 3.
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 2, Filter: "custom_fields.vendor like 'Nope'", AllowCustomFieldFilters: true}, 3)
		assert.Contains(t, ids, int64(13), "task 13 has vendor 'Nope'")
	})
	t.Run("filter include nulls adds missing rows", func(t *testing.T) {
		// Without include_nulls, only task 1 (impact 7.5) matches > 5.
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "custom_fields.impact > 5", FilterIncludeNulls: true, AllowCustomFieldFilters: true}, 1)
		assert.Contains(t, ids, int64(1))
		assert.NotContains(t, ids, int64(2), "task 2 has impact -2.5, not null")
		// At least one task without an impact value must now be included.
		assert.Greater(t, len(ids), 1, "filter_include_nulls must add tasks without a value row")
	})
	t.Run("zero is not null", func(t *testing.T) {
		s := db.NewSession()
		// Give task 4 an impact value of 0 (a real value, not null). Commit and
		// close so the write lock is released before the read below.
		zero := int64(0)
		_, err := s.Insert(&TaskCustomFieldValue{TaskID: 4, DefinitionID: 1, ValueNumber: &zero})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		require.NoError(t, s.Close())

		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "custom_fields.impact is null", AllowCustomFieldFilters: true}, 1)
		assert.NotContains(t, ids, int64(4), "task 4 has impact 0, which is a real value")
	})
}

func TestCustomFieldFilterDefinitionResolution(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	t.Run("mixed types across projects fail", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		// Project 1 has `impact` (number). Add a short_text definition with the
		// same key in project 2 to force the mixed-type path.
		_, err := s.Insert(&CustomFieldDefinition{
			ProjectID:  2,
			MachineKey: "impact",
			Title:      "Impact 2",
			FieldType:  CustomFieldTypeShortText,
		})
		require.NoError(t, err)

		filters, err := getTaskFiltersFromFilterString("custom_fields.impact > 5", "UTC", true)
		require.NoError(t, err)
		err = resolveCustomFieldFilters(s, []int64{1, 2}, filters, nil)
		require.Error(t, err, "mixed types across projects must fail validation")
		assert.True(t, IsErrInvalidTaskField(err))
	})
	t.Run("mixed number precisions fail", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		// Project 1 `impact` has precision 1. Add a precision-2 definition with
		// the same key in project 2.
		precision := 2
		_, err := s.Insert(&CustomFieldDefinition{
			ProjectID:     2,
			MachineKey:    "impact",
			Title:         "Impact 2",
			FieldType:     CustomFieldTypeNumber,
			Configuration: &CustomFieldConfiguration{Precision: &precision},
		})
		require.NoError(t, err)

		filters, err := getTaskFiltersFromFilterString("custom_fields.impact > 5", "UTC", true)
		require.NoError(t, err)
		err = resolveCustomFieldFilters(s, []int64{1, 2}, filters, nil)
		require.Error(t, err, "mixed number precisions must fail validation")
		assert.True(t, IsErrInvalidTaskField(err))
	})
	t.Run("archived definitions still filterable", func(t *testing.T) {
		// Definition 5 (reviewed, boolean) is archived but task 2 has a value.
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "custom_fields.reviewed = true", AllowCustomFieldFilters: true}, 1)
		assert.Contains(t, ids, int64(2), "task 2 has reviewed=true on an archived definition")
	})
}

func TestCustomFieldSort(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	t.Run("sort by number desc with nulls last", func(t *testing.T) {
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, SortBy: []string{"custom_fields.impact"}, OrderBy: []string{"desc"}, AllowCustomFieldFilters: true}, 1)
		require.NotEmpty(t, ids)
		assert.Equal(t, int64(1), ids[0], "task 1 has the highest impact (7.5)")
		// Tasks without an impact value sort last.
		last := ids[len(ids)-1]
		assert.NotEqual(t, int64(1), last)
		assert.NotEqual(t, int64(2), last, "task 2 has impact -2.5, so it sorts before nulls")
	})
	t.Run("non-sortable type fails", func(t *testing.T) {
		// Definition 4 (tags) is multi_select, not sortable.
		_, err := getTaskFiltersFromFilterString("", "UTC", true)
		require.NoError(t, err)
		s := db.NewSession()
		defer s.Close()
		sp := &sortParam{sortBy: "custom_fields.tags", orderBy: orderAscending}
		require.NoError(t, sp.validate())
		err = resolveCustomFieldFilters(s, []int64{1}, nil, []*sortParam{sp})
		require.Error(t, err, "multi_select is not sortable")
		assert.True(t, IsErrInvalidTaskField(err))
	})
	t.Run("id tie-breaker preserved", func(t *testing.T) {
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, SortBy: []string{"custom_fields.impact"}, OrderBy: []string{"asc"}, AllowCustomFieldFilters: true}, 1)
		require.NotEmpty(t, ids)
		// Task 2 (impact -2.5) sorts first ascending.
		assert.Equal(t, int64(2), ids[0])
	})
}

func TestCustomFieldRelationNullSemantics(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	t.Run("labels is null", func(t *testing.T) {
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "labels is null", AllowCustomFieldFilters: true}, 1)
		assert.NotContains(t, ids, int64(1), "task 1 has labels")
	})
	t.Run("assignees is not null", func(t *testing.T) {
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "assignees is not null", AllowCustomFieldFilters: true}, 1)
		assert.Contains(t, ids, int64(30), "task 30 has assignees")
	})
	t.Run("parent project null checks the nullable parent id", func(t *testing.T) {
		ids := readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "parent_project is null", AllowCustomFieldFilters: true}, 1)
		assert.Contains(t, ids, int64(1), "project 1 is a root project")

		s := db.NewSession()
		parentID := int64(2)
		_, err := s.ID(1).Cols("parent_project_id").Update(&Project{ParentProjectID: &parentID})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		require.NoError(t, s.Close())

		ids = readTaskIDs(t, &TaskCollection{ProjectID: 1, Filter: "parent_project_id is not null", AllowCustomFieldFilters: true}, 1)
		assert.Contains(t, ids, int64(1), "project 1 now has a parent")
	})
}

func TestCustomFieldFilterV1Isolation(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	t.Run("saved filter write rejects custom fields", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		sf := &SavedFilter{
			Title:   "v1 custom field filter",
			Filters: &TaskCollection{Filter: "custom_fields.impact > 5"},
		}
		err := sf.Create(s, &user.User{ID: 1})
		require.Error(t, err)
		assert.True(t, IsErrInvalidTaskField(err))
	})

	t.Run("project view write rejects custom fields", func(t *testing.T) {
		view := &ProjectView{
			Filter: &TaskCollection{Filter: "custom_fields.impact > 5"},
		}
		err := validateProjectViewFilters(view)
		require.Error(t, err)
		assert.True(t, IsErrInvalidTaskField(err))

		view.AllowCustomFieldFilters = true
		require.NoError(t, validateProjectViewFilters(view))
	})

	t.Run("filter bucket execution rejects custom fields", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		view := &ProjectView{
			ID:                      1,
			BucketConfigurationMode: BucketConfigurationModeFilter,
			BucketConfiguration: []*ProjectViewBucketConfiguration{{
				Title:  "High impact",
				Filter: &TaskCollection{Filter: "custom_fields.impact > 5"},
			}},
		}
		_, err := GetTasksInBucketsForView(
			s,
			view,
			[]*Project{{ID: 1}},
			&taskSearchOptions{},
			&user.User{ID: 1},
		)
		require.Error(t, err)
		assert.True(t, IsErrInvalidTaskField(err))
	})
}

func TestCustomFieldFilterNested(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	// A custom-field filter nested inside parentheses must be resolved and cast
	// like a top-level one.
	filters, err := getTaskFiltersFromFilterString("(custom_fields.impact > 5 && done = false) || custom_fields.status in 1", "UTC", true)
	require.NoError(t, err)
	require.NoError(t, resolveCustomFieldFilters(s, []int64{1}, filters, nil))

	nested, is := filters[0].value.([]*taskFilter)
	require.True(t, is, "the parenthesised group must be a nested filter list")
	require.Len(t, nested, 2)
	require.NotNil(t, nested[0].customFieldDefs, "nested custom-field filter must be resolved")
	assert.Equal(t, int64(50), nested[0].value, "impact > 5 scales to value_number > 50 at precision 1")
	require.NotNil(t, filters[1].customFieldDefs, "top-level custom-field filter must be resolved")
}

func TestNullSentinelLiteralStaysString(t *testing.T) {
	// A quoted value equal to the sentinel text must stay a string comparison,
	// not become a null predicate.
	result, err := getTaskFiltersFromFilterString("title = '__VIKUNJA_NULL__'", "UTC", true)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, taskFilterComparatorEquals, result[0].comparator)
	assert.Equal(t, "__VIKUNJA_NULL__", result[0].value)
}

func TestCustomFieldFilterRepeatedQuery(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	// Resolution mutates the parsed filters (casts values, attaches
	// definitions), so a benchmark-style loop must clone a pristine parse per
	// iteration. Two iterations must both return results.
	pristine, err := getTaskFiltersFromFilterString("custom_fields.impact > 5", "UTC", true)
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		opts := &taskSearchOptions{
			page:           1,
			perPage:        50,
			filter:         "custom_fields.impact > 5",
			filterTimezone: "UTC",
			parsedFilters:  cloneTaskFilters(pristine),
		}
		tasks, _, _, err := getRawTasksForProjects(s, []*Project{{ID: 1}}, &user.User{ID: 1}, opts)
		require.NoError(t, err, "iteration %d", i)
		assert.NotEmpty(t, tasks, "iteration %d must return results", i)
	}
}
