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

package webtests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// taskIDsFromItems extracts the task ids from a paginated task-list response.
func taskIDsFromItems(t *testing.T, rec *httptest.ResponseRecorder) []int64 {
	t.Helper()
	items := decodePaginatedTaskItems(t, rec)
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		var task struct {
			ID int64 `json:"id"`
		}
		require.NoError(t, json.Unmarshal(item, &task))
		ids = append(ids, task.ID)
	}
	return ids
}

// Custom-field filters and sorts flow through the v2 task-list query params.
// Fixtures: task 1 in project 1 has impact 7.5 (definition 1, precision 1,
// value_number 75); task 2 has impact -2.5 (value_number -25).
func TestHumaCustomFieldFilter(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	t.Run("filter by number", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?filter=custom_fields.impact%20%3E%205", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		ids := taskIDsFromItems(t, rec)
		assert.Contains(t, ids, int64(1))
		assert.NotContains(t, ids, int64(2))
	})
	t.Run("filter is null", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?filter=custom_fields.impact%20is%20null", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		ids := taskIDsFromItems(t, rec)
		assert.NotContains(t, ids, int64(1))
		assert.NotContains(t, ids, int64(2))
	})
	t.Run("sort by custom field", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?sort_by=custom_fields.impact&order_by=desc", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		ids := taskIDsFromItems(t, rec)
		require.NotEmpty(t, ids)
		// Task 1 (impact 7.5) sorts first descending.
		assert.Equal(t, int64(1), ids[0])
	})
	t.Run("invalid custom field filter", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?filter=custom_fields.nonexistent%20%3E%205", "", token, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	})
	t.Run("non-sortable custom field", func(t *testing.T) {
		// Definition 4 (tags) is multi_select, not sortable.
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?sort_by=custom_fields.tags", "", token, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	})
	t.Run("v1 rejects custom-field sort", func(t *testing.T) {
		// v1 never sets AllowCustomFieldFilters, so sort_by=custom_fields.<key>
		// is rejected on frozen v1 routes.
		rec := humaRequest(t, e, http.MethodGet, "/api/v1/projects/1/tasks?sort_by=custom_fields.impact", "", token, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	})
}

// API tokens need the custom_fields.read_all scope to filter or sort by custom
// fields, not just to expand them.
func TestHumaCustomFieldFilterTokenScope(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	createToken := func(perms string) string {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tokens",
			`{"title":"cf-filter","permissions":`+perms+`,"expires_at":"2099-01-01T00:00:00Z"}`,
			token, "")
		require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
		var created struct {
			Token string `json:"token"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		require.NotEmpty(t, created.Token)
		return created.Token
	}

	t.Run("filter without scope is forbidden", func(t *testing.T) {
		// tasks.read_all alone does not unlock custom-field filtering. A valid
		// token that lacks a required scope is forbidden (403), not unauthenticated.
		tok := createToken(`{"tasks":["read_all"]}`)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?filter=custom_fields.impact%20%3E%205", "", tok, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	})
	t.Run("filter with scope succeeds", func(t *testing.T) {
		tok := createToken(`{"tasks":["read_all"],"custom_fields":["read_all"]}`)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?filter=custom_fields.impact%20%3E%205", "", tok, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	})
	t.Run("sort without scope is forbidden", func(t *testing.T) {
		tok := createToken(`{"tasks":["read_all"]}`)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?sort_by=custom_fields.impact", "", tok, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	})
	t.Run("value containing the namespace does not trigger the scope", func(t *testing.T) {
		// A filter value that literally contains "custom_fields." must not be
		// mistaken for a custom-field reference.
		tok := createToken(`{"tasks":["read_all"]}`)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?filter=title%20%3D%20%27custom_fields.foo%27", "", tok, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	})
	t.Run("saved filter with custom-field filter requires the scope", func(t *testing.T) {
		// The effective filter merged from a saved filter is not visible to the
		// URL-param scope check, so ReadAll must enforce it.
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/filters",
			`{"title":"cf-filter","filters":{"filter":"custom_fields.impact > 5"}}`,
			token, "")
		require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
		var created struct {
			ID int64 `json:"id"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

		tok := createToken(`{"tasks":["read_all"]}`)
		projectID := -created.ID - 1
		rec = humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/projects/%d/tasks", projectID), "", tok, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	})
}
