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
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures: definition 3 (project 1, single_select) has options 1-2; definition
// 8 (project 9, read share) has option 6; definition 10 (project 11, admin
// share) has option 7. Definition 7 lives in the inaccessible project 2.
func TestHumaCustomFieldOption(t *testing.T) {
	owned := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/projects/1/custom-field-definitions/3/options",
		idParam:  "option",
		t:        t,
	}
	require.NoError(t, owned.ensureEnv())
	readShared := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/projects/9/custom-field-definitions/8/options",
		idParam:  "option",
		t:        t,
		e:        owned.e,
	}
	writeShared := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/projects/10/custom-field-definitions/9/options",
		idParam:  "option",
		t:        t,
		e:        owned.e,
	}
	adminShared := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/projects/11/custom-field-definitions/10/options",
		idParam:  "option",
		t:        t,
		e:        owned.e,
	}
	forbidden := webHandlerTestV2{
		user:     &testuser1,
		basePath: "/api/v2/projects/2/custom-field-definitions/7/options",
		idParam:  "option",
		t:        t,
		e:        owned.e,
	}

	t.Run("ReadAll", func(t *testing.T) {
		t.Run("Normal", func(t *testing.T) {
			rec, err := owned.testReadAllWithUser(nil, nil)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"label":"To Do"`)
			assert.Contains(t, rec.Body.String(), `"label":"Done"`)

			var env struct {
				Items []struct {
					ID         int64  `json:"id"`
					MachineKey string `json:"machine_key"`
				} `json:"items"`
				Total int64 `json:"total"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			require.Len(t, env.Items, 2)
			require.Equal(t, int64(2), env.Total)
		})
		t.Run("Read share can list", func(t *testing.T) {
			rec, err := readShared.testReadAllWithUser(nil, nil)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"machine_key":"cat1"`)
		})
		t.Run("Pagination", func(t *testing.T) {
			// Definition 3 has 2 active options; page 2 with per_page=1 must return
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
			require.Equal(t, int64(2), env.Total)
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
			rec, err := owned.testReadOneWithUser(nil, map[string]string{"option": "1"})
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"label":"To Do"`)
			assert.Contains(t, rec.Body.String(), `"max_permission":`)
			assert.NotEmpty(t, rec.Result().Header.Get("ETag"))
		})
		t.Run("Option from another definition", func(t *testing.T) {
			// Option 6 belongs to definition 8, not definition 3.
			_, err := owned.testReadOneWithUser(nil, map[string]string{"option": "6"})
			require.Error(t, err)
			assert.Equal(t, http.StatusNotFound, getHTTPErrorCode(err))
		})
		t.Run("Nonexisting", func(t *testing.T) {
			_, err := owned.testReadOneWithUser(nil, map[string]string{"option": "9999"})
			require.Error(t, err)
			assert.Equal(t, http.StatusNotFound, getHTTPErrorCode(err))
		})
	})

	t.Run("Create", func(t *testing.T) {
		t.Run("Normal", func(t *testing.T) {
			rec, err := owned.testCreateWithUser(nil, nil, `{"machine_key":"wip","label":"WIP"}`)
			require.NoError(t, err)
			assert.Equal(t, http.StatusCreated, rec.Code)
			assert.Contains(t, rec.Body.String(), `"label":"WIP"`)
		})
		t.Run("Admin share can create", func(t *testing.T) {
			rec, err := adminShared.testCreateWithUser(nil, nil, `{"machine_key":"lead2","label":"Lead Two"}`)
			require.NoError(t, err)
			assert.Equal(t, http.StatusCreated, rec.Code)
			assert.Contains(t, rec.Body.String(), `"definition_id":10`)
		})
		t.Run("Read share cannot create", func(t *testing.T) {
			_, err := readShared.testCreateWithUser(nil, nil, `{"machine_key":"nope","label":"Nope"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Write share cannot create", func(t *testing.T) {
			_, err := writeShared.testCreateWithUser(nil, nil, `{"machine_key":"nope","label":"Nope"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Forbidden", func(t *testing.T) {
			_, err := forbidden.testCreateWithUser(nil, nil, `{"machine_key":"nope","label":"Nope"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Non-select definition rejects options", func(t *testing.T) {
			// Definition 1 is a number field; options are only meaningful for selects.
			_, err := owned.serve(http.MethodPost, "/api/v2/projects/1/custom-field-definitions/1/options", `{"machine_key":"nope","label":"Nope"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusBadRequest, getHTTPErrorCode(err))
		})
		t.Run("Duplicate machine key", func(t *testing.T) {
			_, err := owned.testCreateWithUser(nil, nil, `{"machine_key":"todo","label":"Again"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusBadRequest, getHTTPErrorCode(err))
		})
		t.Run("Empty label", func(t *testing.T) {
			_, err := owned.testCreateWithUser(nil, nil, `{"machine_key":"nope","label":""}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusUnprocessableEntity, getHTTPErrorCode(err))
		})
	})

	t.Run("Update", func(t *testing.T) {
		t.Run("Normal", func(t *testing.T) {
			// PUT is a full replace; the immutable machine key must be echoed.
			rec, err := owned.testUpdateWithUser(nil, map[string]string{"option": "1"}, `{"machine_key":"todo","label":"Renamed To Do"}`)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"label":"Renamed To Do"`)
		})
		t.Run("Admin share can update", func(t *testing.T) {
			rec, err := adminShared.testUpdateWithUser(nil, map[string]string{"option": "7"}, `{"machine_key":"lead1","label":"Renamed Lead"}`)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"label":"Renamed Lead"`)
		})
		t.Run("Read share cannot update", func(t *testing.T) {
			_, err := readShared.testUpdateWithUser(nil, map[string]string{"option": "6"}, `{"label":"x"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
		t.Run("Option from another definition", func(t *testing.T) {
			_, err := owned.testUpdateWithUser(nil, map[string]string{"option": "6"}, `{"label":"x"}`)
			require.Error(t, err)
			assert.Equal(t, http.StatusNotFound, getHTTPErrorCode(err))
		})
	})

	t.Run("Archive", func(t *testing.T) {
		t.Run("Normal", func(t *testing.T) {
			rec, err := owned.testDeleteWithUser(nil, map[string]string{"option": "2"})
			require.NoError(t, err)
			assert.Equal(t, http.StatusNoContent, rec.Code)
			assert.Empty(t, rec.Body.String())

			rec, err = owned.testReadOneWithUser(nil, map[string]string{"option": "2"})
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"is_archived":true`)
		})
		t.Run("Archived options hidden by default", func(t *testing.T) {
			// Options 1 and 2 are now the only ones; 2 was archived above.
			rec, err := owned.testReadAllWithUser(nil, nil)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"label":"WIP"`)
			assert.NotContains(t, rec.Body.String(), `"label":"Done"`)
		})
		t.Run("Include archived", func(t *testing.T) {
			rec, err := owned.testReadAllWithUser(url.Values{"include_archived": {"true"}}, nil)
			require.NoError(t, err)
			assert.Contains(t, rec.Body.String(), `"label":"Done"`)
			assert.Contains(t, rec.Body.String(), `"is_archived":true`)
		})
		t.Run("Admin share can archive", func(t *testing.T) {
			rec, err := adminShared.testDeleteWithUser(nil, map[string]string{"option": "7"})
			require.NoError(t, err)
			assert.Equal(t, http.StatusNoContent, rec.Code)
		})
		t.Run("Read share cannot archive", func(t *testing.T) {
			_, err := readShared.testDeleteWithUser(nil, map[string]string{"option": "6"})
			require.Error(t, err)
			assert.Equal(t, http.StatusForbidden, getHTTPErrorCode(err))
		})
	})
}

func TestHumaCustomFieldOption_APITokens(t *testing.T) {
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

	t.Run("token with the option read permission can list and read", func(t *testing.T) {
		token := createToken(`{"projects_custom_field_definitions_options":["read_all","read_one"]}`)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-field-definitions/3/options", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-field-definitions/3/options/1", "", token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("token without the option permission is rejected", func(t *testing.T) {
		// Fixture token 1 grants tasks.read_all/update only.
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-field-definitions/3/options", "", "tk_2eef46f40ebab3304919ab2e7e39993f75f29d2e", "")
		require.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
	})
}
