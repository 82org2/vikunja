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

package e2etests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"
	"code.vikunja.io/api/pkg/user"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// humaTokenE2E issues a real JWT for a test user so a v2 request can be driven
// through the Echo+Huma stack.
func humaTokenE2E(t *testing.T, u *user.User) string {
	t.Helper()
	tok, err := auth.NewUserJWTAuthtoken(u, "test-session-id")
	require.NoError(t, err)
	return tok
}

// humaRequestE2E dispatches one v2 request against the pre-built echo.Echo.
func humaRequestE2E(t *testing.T, e *echo.Echo, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestCustomFieldValueWebhookE2E exercises the full pipeline for a custom field
// value mutation: v2 route → SetCustomFieldValue → DispatchOnCommit →
// s.Commit() → DispatchPending → watermill → WebhookListener.Handle →
// HTTP POST to the test target.
func TestCustomFieldValueWebhookE2E(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e, err := setupE2ETestEnv(ctx)
	require.NoError(t, err)

	// Start a test HTTP server to capture webhook payloads.
	webhookReceived := make(chan []byte, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case webhookReceived <- body:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Reload fixtures, drop the example.com fixture webhook (it would fire a
	// noisy failing delivery for the task.updated compatibility event), and
	// insert a webhook for project 1 on the value event.
	require.NoError(t, db.LoadFixtures())
	s := db.NewSession()
	defer s.Close()
	_, err = s.Where("id = ?", 1).Delete(&models.Webhook{})
	require.NoError(t, err)
	_, err = s.Insert(&models.Webhook{
		TargetURL:   ts.URL,
		Events:      []string{"custom_field.value.changed.v1"},
		ProjectID:   1,
		CreatedByID: 1,
	})
	require.NoError(t, err)
	require.NoError(t, s.Commit())

	// Set a date value on task 2 / definition 2 (no prior value → "set").
	token := humaTokenE2E(t, &testuser1)
	rec := humaRequestE2E(t, e, http.MethodPut, "/api/v2/tasks/2/custom-field-values/2", `{"type":"date","date":"2019-01-01"}`, token)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	// Wait for the webhook payload via the real async pipeline.
	select {
	case body := <-webhookReceived:
		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &payload))
		assert.Equal(t, "custom_field.value.changed.v1", payload["event_name"])

		data, ok := payload["data"].(map[string]interface{})
		require.True(t, ok, "payload.data should be a map")
		assert.Equal(t, "set", data["semantic"])
		assert.InDelta(t, 1, data["project_id"], 0)
		assert.InDelta(t, 2, data["task_id"], 0)
		assert.InDelta(t, 2, data["definition_id"], 0)
		assert.Equal(t, "target_date", data["machine_key"])
		assert.Equal(t, "date", data["type"])
		assert.Nil(t, data["old_value"])
		require.NotNil(t, data["new_value"])
		assert.NotEmpty(t, data["event_id"])
		assert.NotNil(t, data["doer"])
		assert.NotNil(t, data["timestamp"])
		// The payload stays flat: no injected project object.
		_, hasProject := data["project"]
		assert.False(t, hasProject, "flat-ID events must not get an injected project object")

	case <-time.After(10 * time.Second):
		t.Fatal("Webhook payload not received within 10s timeout")
	}
}
