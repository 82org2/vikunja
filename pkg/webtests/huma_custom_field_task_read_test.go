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
	"net/http"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Custom field values surface through task reads: always for single-task reads
// (user sessions and link shares), only via expand=custom_fields for
// collections, and for API tokens only when the token has the
// custom_fields.read_all expansion scope.
//
// Fixtures: task 1 in project 1 has a number value (definition 1, "impact",
// scaled 75 at precision 1 → 7.5) and a date value (definition 2, "target_date");
// definitions 1 and 2 sit at positions 1 and 2.
func TestHumaCustomFieldTaskRead(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	createToken := func(perms string) string {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tokens",
			`{"title":"cf-read","permissions":`+perms+`,"expires_at":"2099-01-01T00:00:00Z"}`,
			token, "")
		require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
		var created struct {
			Token string `json:"token"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		require.NotEmpty(t, created.Token)
		return created.Token
	}

	t.Run("single read includes values for a user session", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"custom_fields":`)
		assert.Contains(t, rec.Body.String(), `"machine_key":"impact"`)
		assert.Contains(t, rec.Body.String(), `"number":7.5`)
		// Values are ordered by definition position: impact (pos 1) before
		// target_date (pos 2).
		assert.Less(t, strings.Index(rec.Body.String(), `"machine_key":"impact"`),
			strings.Index(rec.Body.String(), `"machine_key":"target_date"`))
	})

	t.Run("single read of a value-less task serializes an empty array", func(t *testing.T) {
		// Task 4 is in project 1 with no custom field values; an expand-active
		// response must show [] rather than omitting the field.
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/4", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"custom_fields":[]`)
	})

	t.Run("single read includes values for a link share", func(t *testing.T) {
		shareToken := linkShareToken(t, &models.LinkSharing{ID: 1, Hash: "test", ProjectID: 1, Permission: models.PermissionRead})
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1", "", shareToken, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"custom_fields":`)
	})

	t.Run("single read omits values for a token without the scope", func(t *testing.T) {
		tok := createToken(`{"tasks":["read_all","read_one"]}`)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1", "", tok, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.NotContains(t, rec.Body.String(), `"custom_fields"`)
	})

	t.Run("single read includes values for a token with the scope", func(t *testing.T) {
		tok := createToken(`{"tasks":["read_all","read_one"],"custom_fields":["read_all"]}`)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1", "", tok, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"custom_fields":`)
	})

	t.Run("collection expand loads values for a user", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?expand=custom_fields", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"custom_fields":`)
		// The value-less tasks in project 1 must serialize [], not drop the field.
		assert.Contains(t, rec.Body.String(), `"custom_fields":[]`)
	})

	t.Run("collection without expand omits values", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.NotContains(t, rec.Body.String(), `"custom_fields"`)
	})

	t.Run("collection expand requires the scope for tokens", func(t *testing.T) {
		// Fixture token 1 has tasks.read_all but no custom_fields scope. A valid
		// token that lacks a required scope is forbidden (403), not unauthenticated.
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks?expand=custom_fields", "", "tk_2eef46f40ebab3304919ab2e7e39993f75f29d2e", "")
		require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("collection expand works for a scoped token", func(t *testing.T) {
		tok := createToken(`{"tasks":["read_all"],"custom_fields":["read_all"]}`)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks?expand=custom_fields", "", tok, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"custom_fields":`)
	})
}

// TestHumaCustomFieldTaskRead_V1Isolation proves v1 keeps rejecting the
// custom_fields expansion as an unknown expand value: it must not execute it,
// and API tokens must not be asked for the custom_fields.read_all scope on v1.
func TestHumaCustomFieldTaskRead_V1Isolation(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	t.Run("v1 rejects the custom_fields expansion as unknown", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v1/tasks/1?expand=custom_fields", "", token, "")
		require.Equal(t, http.StatusPreconditionFailed, rec.Code, "body: %s", rec.Body.String())
		assert.NotContains(t, rec.Body.String(), `"custom_fields"`)
	})

	t.Run("v1 without the expansion returns no custom fields", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v1/tasks/1", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.NotContains(t, rec.Body.String(), `"custom_fields"`)
	})

	t.Run("v1 token with the expansion is not asked for the scope", func(t *testing.T) {
		// Fixture token 1 has tasks.read_all/update but no custom_fields scope;
		// on v1 the request must fail as an unknown expansion, not as a missing
		// scope (401).
		rec := humaRequest(t, e, http.MethodGet, "/api/v1/tasks?expand=custom_fields", "", "tk_2eef46f40ebab3304919ab2e7e39993f75f29d2e", "")
		require.Equal(t, http.StatusPreconditionFailed, rec.Code, "body: %s", rec.Body.String())
	})
}

func TestHumaCustomFieldTaskRead_ETagTracksDefinitionEdits(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	before := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1", "", token, "")
	require.Equal(t, http.StatusOK, before.Code, "body: %s", before.Body.String())
	etagBefore := before.Header().Get("ETag")
	require.NotEmpty(t, etagBefore)

	// Editing a definition bumps the parent project's updated timestamp, so the
	// task response's ETag must change even though the task row did not.
	rec := humaRequest(t, e, http.MethodPut, "/api/v2/projects/1/custom-field-definitions/1",
		`{"machine_key":"impact","field_type":"number","title":"Renamed impact","configuration":{"precision":1,"unit":"points"}}`, token, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	after := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1", "", token, "")
	require.Equal(t, http.StatusOK, after.Code, "body: %s", after.Body.String())
	etagAfter := after.Header().Get("ETag")
	require.NotEmpty(t, etagAfter)
	assert.NotEqual(t, etagBefore, etagAfter, "a definition edit must invalidate the cached task representation")
}

func linkShareToken(t *testing.T, share *models.LinkSharing) string {
	tok, err := auth.NewLinkShareJWTAuthtoken(share)
	require.NoError(t, err)
	return tok
}
