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
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures (pkg/db/fixtures): project 1 (owned by testuser1) has definitions
// 1-6, with 5 archived. Project 2 (owned by user3, no share to testuser1) has
// definition 7. Projects 9/10/11 are shared to testuser1 read/write/admin and
// hold definitions 8/9/10. testuser1 has read share on project 3 (definition 11).
//
// Permission gradient mirrors TestProjectView: reads delegate to Project.CanRead;
// create/update/delete (archive) to Project.IsAdmin, so read/write shares stay
// below the admin bar and the admin share clears it.
func TestHumaCustomFieldDefinition(t *testing.T) {
	owned := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/projects/1/custom-field-definitions",
		idParam:  "definition",
		t:        t,
	}
	require.NoError(t, owned.ensureEnv())
	forbidden := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/projects/2/custom-field-definitions",
		idParam:  "definition",
		t:        t,
		e:        owned.e,
	}
	readShared := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/projects/9/custom-field-definitions",
		idParam:  "definition",
		t:        t,
		e:        owned.e,
	}
	writeShared := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/projects/10/custom-field-definitions",
		idParam:  "definition",
		t:        t,
		e:        owned.e,
	}
	adminShared := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/projects/11/custom-field-definitions",
		idParam:  "definition",
		t:        t,
		e:        owned.e,
	}

	t.Run("ReadAll", func(t *testing.T) {
		t.Run("Normal", func(t *testing.T) {
			rec, err := owned.testReadAllWithUser(nil, nil)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"machine_key":"impact"`)
			assert.NotContains(t, rec.Body.String(), `"machine_key":"reviewed"`)

			var env struct {
				Items []struct {
					ID         int64  `json:"id"`
					MachineKey string `json:"machine_key"`
					IsArchived bool   `json:"is_archived"`
				} `json:"items"`
				Total int64 `json:"total"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			// Definitions 1-6 live in project 1; def 5 is archived and hidden by default.
			require.Len(t, env.Items, 5)
			require.Equal(t, int64(5), env.Total)
			for _, item := range env.Items {
				assert.False(t, item.IsArchived, "the default list must not contain archived definitions")
			}
		})
		t.Run("IncludeArchived", func(t *testing.T) {
			rec, err := owned.testReadAllWithUser(url.Values{"include_archived": {"true"}}, nil)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"machine_key":"reviewed"`)
			assert.Contains(t, rec.Body.String(), `"is_archived":true`)
		})
		t.Run("Read share can list", func(t *testing.T) {
			rec, err := readShared.testReadAllWithUser(nil, nil)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"machine_key":"category"`)
		})
		t.Run("Pagination", func(t *testing.T) {
			// Project 1 has 5 active definitions; page 2 with per_page=1 must return
			// exactly one item while reporting the unpaginated total.
			rec, err := owned.testReadAllWithUser(url.Values{"per_page": {"1"}, "page": {"2"}}, nil)
			require.NoError(t, err)
			var env struct {
				Items []struct {
					ID int64 `json:"id"`
				} `json:"items"`
				Total int64 `json:"total"`
				Page  int   `json:"page"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			require.Len(t, env.Items, 1)
			require.Equal(t, int64(5), env.Total)
			require.Equal(t, 2, env.Page)
		})
		t.Run("Forbidden", func(t *testing.T) {
			_, err := forbidden.testReadAllWithUser(nil, nil)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
	})

	t.Run("ReadOne", func(t *testing.T) {
		t.Run("Normal", func(t *testing.T) {
			rec, err := owned.testReadOneWithUser(nil, map[string]string{"definition": "1"})
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"machine_key":"impact"`)
			assert.Contains(t, rec.Body.String(), `"max_permission":`)
			assert.NotEmpty(t, rec.Result().Header.Get("ETag"))
		})
		t.Run("Archived definition can be read", func(t *testing.T) {
			rec, err := owned.testReadOneWithUser(nil, map[string]string{"definition": "5"})
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"is_archived":true`)
		})
		t.Run("Read share can read", func(t *testing.T) {
			rec, err := readShared.testReadOneWithUser(nil, map[string]string{"definition": "8"})
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"id":8`)
		})
		t.Run("Nonexisting", func(t *testing.T) {
			_, err := owned.testReadOneWithUser(nil, map[string]string{"definition": "9999"})
			require.Error(t, err)
			assert.Equal(t, http.StatusNotFound, getHTTPErrorCode(err))
		})
		t.Run("Definition from another project", func(t *testing.T) {
			_, err := owned.testReadOneWithUser(nil, map[string]string{"definition": "7"})
			require.Error(t, err)
			assert.Equal(t, http.StatusNotFound, getHTTPErrorCode(err))
		})
		t.Run("Forbidden", func(t *testing.T) {
			_, err := forbidden.testReadOneWithUser(nil, map[string]string{"definition": "7"})
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
	})

	t.Run("Create", func(t *testing.T) {
		t.Run("Normal", func(t *testing.T) {
			rec, err := owned.testCreateWithUser(nil, nil, `{"machine_key":"new_field","title":"New Field","field_type":"short_text"}`)
			require.NoError(t, err)
			assert.Equal(t, http.StatusCreated, rec.Code)
			assert.Contains(t, rec.Body.String(), `"machine_key":"new_field"`)
			assert.Contains(t, rec.Body.String(), `"project_id":1`)
		})
		t.Run("Admin share can create", func(t *testing.T) {
			rec, err := adminShared.testCreateWithUser(nil, nil, `{"machine_key":"admin_field","title":"Admin Field","field_type":"short_text"}`)
			require.NoError(t, err)
			assert.Equal(t, http.StatusCreated, rec.Code)
			assert.Contains(t, rec.Body.String(), `"project_id":11`)
		})
		t.Run("Read share cannot create", func(t *testing.T) {
			_, err := readShared.testCreateWithUser(nil, nil, `{"machine_key":"nope","title":"Nope","field_type":"short_text"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Write share cannot create", func(t *testing.T) {
			_, err := writeShared.testCreateWithUser(nil, nil, `{"machine_key":"nope","title":"Nope","field_type":"short_text"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Forbidden", func(t *testing.T) {
			_, err := forbidden.testCreateWithUser(nil, nil, `{"machine_key":"nope","title":"Nope","field_type":"short_text"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Empty title", func(t *testing.T) {
			_, err := owned.testCreateWithUser(nil, nil, `{"machine_key":"t1","title":"","field_type":"short_text"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusUnprocessableEntity, getHTTPErrorCode(err))
		})
		t.Run("Invalid machine key", func(t *testing.T) {
			_, err := owned.testCreateWithUser(nil, nil, `{"machine_key":"Bad Key","title":"x","field_type":"short_text"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusBadRequest, getHTTPErrorCode(err))
		})
		t.Run("Invalid field type", func(t *testing.T) {
			_, err := owned.testCreateWithUser(nil, nil, `{"machine_key":"t2","title":"x","field_type":"nope"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusBadRequest, getHTTPErrorCode(err))
		})
		t.Run("Duplicate machine key", func(t *testing.T) {
			_, err := owned.testCreateWithUser(nil, nil, `{"machine_key":"impact","title":"x","field_type":"short_text"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusBadRequest, getHTTPErrorCode(err))
		})
	})

	t.Run("Update", func(t *testing.T) {
		t.Run("Normal", func(t *testing.T) {
			// PUT is a full replace: the immutable machine key, field type, and the
			// number configuration must all be echoed, because dropping the precision
			// would invalidate stored values.
			rec, err := owned.testUpdateWithUser(nil, map[string]string{"definition": "1"}, `{"machine_key":"impact","field_type":"number","title":"Renamed impact","configuration":{"precision":1,"unit":"points"}}`)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"title":"Renamed impact"`)
			assert.Contains(t, rec.Body.String(), `"id":1`)
		})
		t.Run("Admin share can update", func(t *testing.T) {
			// Definition 10 is in project 11 (admin share).
			rec, err := adminShared.testUpdateWithUser(nil, map[string]string{"definition": "10"}, `{"machine_key":"lead","field_type":"single_select","title":"Renamed lead"}`)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"title":"Renamed lead"`)
		})
		t.Run("Read share cannot update", func(t *testing.T) {
			_, err := readShared.testUpdateWithUser(nil, map[string]string{"definition": "8"}, `{"title":"x"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Write share cannot update", func(t *testing.T) {
			_, err := writeShared.testUpdateWithUser(nil, map[string]string{"definition": "9"}, `{"title":"x"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Definition from another project", func(t *testing.T) {
			_, err := owned.testUpdateWithUser(nil, map[string]string{"definition": "7"}, `{"title":"x"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusNotFound, getHTTPErrorCode(err))
		})
		t.Run("Forbidden", func(t *testing.T) {
			_, err := forbidden.testUpdateWithUser(nil, map[string]string{"definition": "7"}, `{"title":"x"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Machine key is immutable", func(t *testing.T) {
			_, err := owned.testUpdateWithUser(nil, map[string]string{"definition": "1"}, `{"machine_key":"other","title":"x","field_type":"number"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusBadRequest, getHTTPErrorCode(err))
		})
		t.Run("Field type is immutable", func(t *testing.T) {
			_, err := owned.testUpdateWithUser(nil, map[string]string{"definition": "1"}, `{"machine_key":"impact","title":"x","field_type":"short_text"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusBadRequest, getHTTPErrorCode(err))
		})
		t.Run("Configuration change invalidating values", func(t *testing.T) {
			// Def 1 is a number field (precision 1) with stored values 7.5 and -2.5;
			// a minimum above them would invalidate both.
			_, err := owned.testUpdateWithUser(nil, map[string]string{"definition": "1"}, `{"machine_key":"impact","title":"Impact","field_type":"number","configuration":{"precision":1,"min":"100"}}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusBadRequest, getHTTPErrorCode(err))
		})
	})

	t.Run("Archive", func(t *testing.T) {
		t.Run("Normal", func(t *testing.T) {
			rec, err := owned.testDeleteWithUser(nil, map[string]string{"definition": "2"})
			require.NoError(t, err)
			assert.Equal(t, http.StatusNoContent, rec.Code)
			assert.Empty(t, rec.Body.String())

			// The row survives, flagged archived: read-one still returns it.
			rec, err = owned.testReadOneWithUser(nil, map[string]string{"definition": "2"})
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"is_archived":true`)
		})
		t.Run("Admin share can archive", func(t *testing.T) {
			rec, err := adminShared.testDeleteWithUser(nil, map[string]string{"definition": "10"})
			require.NoError(t, err)
			assert.Equal(t, http.StatusNoContent, rec.Code)
		})
		t.Run("Read share cannot archive", func(t *testing.T) {
			_, err := readShared.testDeleteWithUser(nil, map[string]string{"definition": "8"})
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Write share cannot archive", func(t *testing.T) {
			_, err := writeShared.testDeleteWithUser(nil, map[string]string{"definition": "9"})
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Definition from another project", func(t *testing.T) {
			_, err := owned.testDeleteWithUser(nil, map[string]string{"definition": "7"})
			require.Error(t, err)
			assert.Equal(t, http.StatusNotFound, getHTTPErrorCode(err))
		})
		t.Run("Forbidden", func(t *testing.T) {
			_, err := forbidden.testDeleteWithUser(nil, map[string]string{"definition": "7"})
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
	})
}

// TestHumaCustomFieldDefinition_PermanentDelete uses its own env because the
// confirmations destroy fixture rows.
func TestHumaCustomFieldDefinition_PermanentDelete(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	t.Run("conflict while values exist without delete_values", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/custom-field-definitions/1/permanent-delete", `{"delete_values":false}`, token, "")
		require.Equal(t, http.StatusConflict, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("confirmed delete removes definition, values, options, memberships", func(t *testing.T) {
		// Definition 3 has options 1-2 and a single-select value on task 3.
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/custom-field-definitions/3/permanent-delete", `{"delete_values":true}`, token, "")
		require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-field-definitions/3", "", token, "")
		require.Equal(t, http.StatusNotFound, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("empty definition can be deleted without delete_values", func(t *testing.T) {
		// Definition 10 lives in project 11, where testuser1 has admin.
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/custom-field-definitions/10/permanent-delete", `{"delete_values":false}`, token, "")
		require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("read share cannot permanently delete", func(t *testing.T) {
		// Definition 8 is in project 9 (read share only).
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/9/custom-field-definitions/8/permanent-delete", `{"delete_values":true}`, token, "")
		require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	})
}

func TestHumaCustomFieldDefinition_ETagReturns304(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-field-definitions/1", "", token, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	etag := rec.Header().Get("ETag")
	require.NotEmpty(t, etag)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/projects/1/custom-field-definitions/1", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotModified, rec.Code, "body: %s", rec.Body.String())
}

func TestHumaCustomFieldDefinition_PATCHMergePatch(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/custom-field-definitions",
		`{"machine_key":"patchme","title":"before","field_type":"short_text"}`, token, "")
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
	var created struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	rec = humaRequest(t, e, http.MethodPatch, fmt.Sprintf("/api/v2/projects/1/custom-field-definitions/%d", created.ID),
		`{"title":"after"}`, token, "application/merge-patch+json")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	rec = humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/projects/1/custom-field-definitions/%d", created.ID), "", token, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"title":"after"`)
	assert.Contains(t, rec.Body.String(), `"machine_key":"patchme"`)
}

func TestHumaCustomFieldDefinition_APITokens(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	userToken := humaTokenFor(t, &testuser1)

	createToken := func(perms string) string {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tokens",
			fmt.Sprintf(`{"title":"cf","permissions":%s,"expires_at":"2099-01-01T00:00:00Z"}`, perms),
			userToken, "")
		require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
		var created struct {
			Token string `json:"token"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		require.NotEmpty(t, created.Token)
		return created.Token
	}

	t.Run("token with the definition read permission can list and read", func(t *testing.T) {
		token := createToken(`{"projects_custom_field_definitions":["read_all","read_one"]}`)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-field-definitions", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-field-definitions/1", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("token without the definition permission is rejected", func(t *testing.T) {
		// Fixture token 1 grants tasks.read_all/update only.
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-field-definitions", "", "tk_2eef46f40ebab3304919ab2e7e39993f75f29d2e", "")
		require.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("token without the permanent-delete permission is rejected", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/custom-field-definitions/10/permanent-delete", `{"delete_values":false}`, "tk_2eef46f40ebab3304919ab2e7e39993f75f29d2e", "")
		require.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("token with the permanent-delete permission can delete", func(t *testing.T) {
		// Definition 10 lives in project 11, where the token owner (user1) is admin.
		token := createToken(`{"projects":["custom_field_definitions_permanent_delete"]}`)
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/custom-field-definitions/10/permanent-delete", `{"delete_values":false}`, token, "")
		require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())
	})
}
