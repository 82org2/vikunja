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

package dump

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func TestConvertFieldValue(t *testing.T) {
	t.Run("Float field conversions", func(t *testing.T) {
		t.Run("should return float64 as-is", func(t *testing.T) {
			result, err := convertFieldValue("position", 123.45, true)
			require.NoError(t, err)
			assert.InEpsilon(t, 123.45, result, 0.0001)
		})

		t.Run("should convert int to float64", func(t *testing.T) {
			result, err := convertFieldValue("position", 42, true)
			require.NoError(t, err)
			assert.InEpsilon(t, 42.0, result, 0.0001)
		})

		t.Run("should decode base64 string and convert to float", func(t *testing.T) {
			encoded := base64.StdEncoding.EncodeToString([]byte("123.45"))
			result, err := convertFieldValue("position", encoded, true)
			require.NoError(t, err)
			assert.InEpsilon(t, 123.45, result, 0.0001)
		})

		t.Run("should handle non-base64 string and convert to float", func(t *testing.T) {
			result, err := convertFieldValue("position", "67.89", true)
			require.NoError(t, err)
			assert.InEpsilon(t, 67.89, result, 0.0001)
		})

		t.Run("should return error for invalid float string", func(t *testing.T) {
			_, err := convertFieldValue("position", "not-a-number", true)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "could not parse double value")
		})

		t.Run("should return error for unexpected type", func(t *testing.T) {
			_, err := convertFieldValue("position", []int{1, 2, 3}, true)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unexpected type for float field")
		})
	})

	t.Run("JSON field conversions", func(t *testing.T) {
		t.Run("should decode base64 string", func(t *testing.T) {
			jsonData := `{"key": "value"}`
			encoded := base64.StdEncoding.EncodeToString([]byte(jsonData))
			result, err := convertFieldValue("permissions", encoded, false)
			require.NoError(t, err)
			assert.JSONEq(t, jsonData, result.(string))
		})

		t.Run("should handle non-base64 string", func(t *testing.T) {
			jsonData := `{"key": "value"}`
			result, err := convertFieldValue("permissions", jsonData, false)
			require.NoError(t, err)
			assert.JSONEq(t, jsonData, result.(string))
		})

		t.Run("should return nil for 'null' string", func(t *testing.T) {
			result, err := convertFieldValue("bucket_configuration", "null", false)
			require.NoError(t, err)
			assert.Nil(t, result)
		})

		t.Run("should return nil for 'NULL' string", func(t *testing.T) {
			result, err := convertFieldValue("bucket_configuration", "NULL", false)
			require.NoError(t, err)
			assert.Nil(t, result)
		})

		t.Run("should return nil for 'Null' string", func(t *testing.T) {
			result, err := convertFieldValue("bucket_configuration", "Null", false)
			require.NoError(t, err)
			assert.Nil(t, result)
		})

		t.Run("should return error for non-string type", func(t *testing.T) {
			_, err := convertFieldValue("permissions", 123, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "expected string for JSON field")
		})
	})

	t.Run("Base64 error handling", func(t *testing.T) {
		t.Run("should handle base64 decode error for float field", func(t *testing.T) {
			invalidBase64 := "invalid base64 with spaces and special chars!!!"
			result, err := convertFieldValue("position", invalidBase64, true)
			// Should not error on decode, but should error on parse float
			require.Error(t, err)
			assert.Contains(t, err.Error(), "could not parse double value")
			assert.Nil(t, result)
		})

		t.Run("should handle base64 decode error for JSON field", func(t *testing.T) {
			// For JSON fields, CorruptInputError just returns the raw string
			invalidBase64 := "invalid base64 with spaces and special chars!!!"
			result, err := convertFieldValue("permissions", invalidBase64, false)
			require.NoError(t, err)
			assert.Equal(t, invalidBase64, result)
		})
	})

	t.Run("Edge cases", func(t *testing.T) {
		t.Run("should handle empty string for JSON field", func(t *testing.T) {
			result, err := convertFieldValue("permissions", "", false)
			require.NoError(t, err)
			assert.Empty(t, result)
		})

		t.Run("should handle empty string for float field", func(t *testing.T) {
			_, err := convertFieldValue("position", "", true)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "could not parse double value")
		})

		t.Run("should handle zero float value", func(t *testing.T) {
			result, err := convertFieldValue("position", 0.0, true)
			require.NoError(t, err)
			assert.InDelta(t, 0.0, result, 0.0001)
		})

		t.Run("should handle negative float value", func(t *testing.T) {
			result, err := convertFieldValue("position", -123.45, true)
			require.NoError(t, err)
			assert.InEpsilon(t, -123.45, result, 0.0001)
		})
	})
}

func TestRestoreCustomFieldColumns(t *testing.T) {
	t.Run("round-trips JSON and position columns", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// Build the dump payload for the four custom-field tables the same way
		// db.Dump does, so the restore path is exercised without dumping every
		// registered table (some are not synced in this test engine). Without
		// the jsonFields/floatFields maps, the configuration and default_value
		// JSON columns would come back as base64 text and the position columns
		// as strings.
		data := make(map[string][]byte)
		for _, table := range []string{"custom_field_definitions", "custom_field_options", "custom_field_values", "custom_field_value_options"} {
			rows := []map[string]interface{}{}
			require.NoError(t, s.Table(table).Find(&rows))
			content, err := json.Marshal(rows)
			require.NoError(t, err)
			data[table] = content
		}

		files := make(map[string]*zip.File)
		{
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for table, content := range data {
				w, err := zw.Create(table)
				require.NoError(t, err)
				_, err = w.Write(content)
				require.NoError(t, err)
			}
			require.NoError(t, zw.Close())
			zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			require.NoError(t, err)
			for _, f := range zr.File {
				files[f.Name] = f
			}
		}

		// Clear the tables and restore from the dump.
		_, err := s.Where(builder.Gt{"id": 0}).Delete(&models.CustomFieldValueOption{})
		require.NoError(t, err)
		_, err = s.Where(builder.Gt{"id": 0}).Delete(&models.TaskCustomFieldValue{})
		require.NoError(t, err)
		_, err = s.Where(builder.Gt{"id": 0}).Delete(&models.CustomFieldOption{})
		require.NoError(t, err)
		_, err = s.Where(builder.Gt{"id": 0}).Delete(&models.CustomFieldDefinition{})
		require.NoError(t, err)
		require.NoError(t, s.Commit())

		require.NoError(t, restoreTableData(files))

		// The definition's configuration JSON and position double survive.
		def := &models.CustomFieldDefinition{}
		exists, err := s.Where("id = ?", 1).Get(def)
		require.NoError(t, err)
		require.True(t, exists)
		require.NotNil(t, def.Configuration)
		assert.Equal(t, 1, *def.Configuration.Precision)
		assert.InEpsilon(t, 1.0, def.Position, 0.0001)

		// The option's position double survives.
		opt := &models.CustomFieldOption{}
		exists, err = s.Where("id = ?", 1).Get(opt)
		require.NoError(t, err)
		require.True(t, exists)
		assert.InEpsilon(t, 1.0, opt.Position, 0.0001)
	})
}
