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
	"sync"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm/schemas"
)

// Value endpoints take two path params, so the tests drive humaRequest with
// explicit paths instead of the single-id webHandlerTestV2 harness.
//
// Fixtures: tasks 1-3 live in project 1 (owned by testuser1), task 13 in
// project 2, task 21 in project 3 (testuser1 has read-only there). Definitions:
// 1 (number) in project 1, 5 (boolean, archived) in project 1, 4 (multi-select)
// in project 1, 7 (short text) in project 2, 11 (short text) in project 3.
func TestHumaCustomFieldValue(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	t.Run("set number", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/1", `{"type":"number","number":7.5}`, token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"type":"number"`)
		assert.Contains(t, rec.Body.String(), `"number":7.5`)
	})

	t.Run("set replaces the previous value", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/1", `{"type":"number","number":9.5}`, token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"number":9.5`)
	})

	t.Run("set multi select returns options in position order", func(t *testing.T) {
		// Options 3 and 4 of definition 4 are positioned 1 and 2.
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/2/custom-field-values/4", `{"type":"multi_select","option_ids":[4,3]}`, token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"option_ids":[3,4]`)
	})

	t.Run("empty multi select unsets", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/2/custom-field-values/4", `{"type":"multi_select","option_ids":[]}`, token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("unset", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodDelete, "/api/v2/tasks/1/custom-field-values/2", "", token, "")
		require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("unset is idempotent", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodDelete, "/api/v2/tasks/1/custom-field-values/2", "", token, "")
		require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("value type must match the definition", func(t *testing.T) {
		// Definition 1 is a number field.
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/1", `{"type":"short_text","short_text":"hi"}`, token, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("archived definition cannot receive values", func(t *testing.T) {
		// Definition 5 is archived.
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/5", `{"type":"boolean","boolean":true}`, token, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("write on a read-only project is forbidden", func(t *testing.T) {
		// Task 21 is in project 3, where testuser1 only has a read share.
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/21/custom-field-values/11", `{"type":"short_text","short_text":"x"}`, token, "")
		require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("write on an inaccessible project is forbidden", func(t *testing.T) {
		// Task 13 is in project 2, to which testuser1 has no access.
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/13/custom-field-values/7", `{"type":"short_text","short_text":"x"}`, token, "")
		require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	})
}

func TestHumaCustomFieldValue_LinkShares(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	linkShareToken := func(share *models.LinkSharing) string {
		tok, err := auth.NewLinkShareJWTAuthtoken(share)
		require.NoError(t, err)
		return tok
	}

	t.Run("read share cannot set values", func(t *testing.T) {
		// Link share "test" is read-only on project 1.
		token := linkShareToken(&models.LinkSharing{ID: 1, Hash: "test", ProjectID: 1, Permission: models.PermissionRead})
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/1", `{"type":"number","number":3.5}`, token, "")
		require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("write share can set values", func(t *testing.T) {
		// Link share "test2" is write on project 2, where task 13 lives.
		token := linkShareToken(&models.LinkSharing{ID: 2, Hash: "test2", ProjectID: 2, Permission: models.PermissionWrite})
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/13/custom-field-values/7", `{"type":"short_text","short_text":"from share"}`, token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"short_text":"from share"`)
	})

	t.Run("read share can read definitions", func(t *testing.T) {
		token := linkShareToken(&models.LinkSharing{ID: 1, Hash: "test", ProjectID: 1, Permission: models.PermissionRead})
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-field-definitions/1", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"machine_key":"impact"`)
	})
}

func TestHumaCustomFieldValue_APITokens(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	userToken := humaTokenFor(t, &testuser1)

	t.Run("token without the value route permission is rejected", func(t *testing.T) {
		// Fixture token 1 grants tasks.read_all/update but no custom-field routes;
		// the token middleware denies the route before it reaches the handler.
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/1", `{"type":"number","number":4.5}`, "tk_2eef46f40ebab3304919ab2e7e39993f75f29d2e", "")
		require.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("token with the value route permission can set values", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tokens",
			`{"title":"custom fields","permissions":{"tasks_custom_field_values":["update"]},"expires_at":"2099-01-01T00:00:00Z"}`,
			userToken, "")
		require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
		var created struct {
			Token string `json:"token"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		require.NotEmpty(t, created.Token)

		rec = humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/1", `{"type":"number","number":5.5}`, created.Token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	})
}

func TestHumaCustomFieldValue_ConcurrentSet(t *testing.T) {
	// The value write path locks the task row (updateTaskLastUpdated) before the
	// upsert, so competing writers serialize and both succeed (last write wins)
	// instead of tripping the unique key. The shared in-memory SQLite test engine
	// has no busy timeout, so its whole-table write lock surfaces as "database
	// table is locked"; production SQLite enables busy_timeout+WAL, and the CI
	// matrix runs PostgreSQL/MySQL where the row lock is actually exercised.
	if db.Type() == schemas.SQLITE {
		t.Skip("the shared in-memory SQLite test database has no busy timeout; concurrent-writer serialisation is covered on PostgreSQL/MySQL in CI")
	}

	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	// Concurrent writes to the same task and definition must both succeed: the
	// task-row lock serialises the upsert (last write wins), never a 409.
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	values := []string{"1.5", "2.5"}
	for _, n := range values {
		wg.Add(1)
		go func(num string) {
			defer wg.Done()
			rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/1",
				fmt.Sprintf(`{"type":"number","number":%s}`, num), token, "")
			codes <- rec.Code
		}(n)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		require.Equal(t, http.StatusOK, code)
	}

	// Exactly one row must remain, holding one of the two submitted values
	// (1.5 or 2.5 at precision 1 → scaled 15 or 25): last write wins, never a
	// duplicate or a torn value.
	s := db.NewSession()
	defer s.Close()
	count, err := s.Where("task_id = ? AND definition_id = ?", 1, 1).Count(&models.TaskCustomFieldValue{})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	row, err := models.GetCustomFieldValue(s, 1, 1)
	require.NoError(t, err)
	def, err := models.GetCustomFieldDefinitionByID(s, 1)
	require.NoError(t, err)
	val, err := row.FromRow(s, def)
	require.NoError(t, err)
	require.Equal(t, models.CustomFieldTypeNumber, val.Type)
	require.NotNil(t, val.Number)
	require.Contains(t, []int64{15, 25}, val.Number.Value)
}
