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

package vikunjafile

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVikunjaFileMigrator_CustomFields(t *testing.T) {
	t.Run("round-trips custom fields", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		number := &models.CustomFieldNumber{}
		require.NoError(t, number.SetRaw("7.5"))
		precision := 1

		projects := []*models.ProjectWithTasksAndBuckets{
			{
				Project: models.Project{Title: "CF Project"},
				Tasks: []*models.TaskWithComments{
					{
						Task: models.Task{Title: "CF Task"},
						CustomFieldValues: []*models.CustomFieldTaskValueExport{
							{
								MachineKey: "impact",
								Value: &models.CustomFieldValueExport{
									Value: &models.CustomFieldValue{Type: models.CustomFieldTypeNumber, Number: number},
								},
							},
						},
					},
				},
				CustomFieldDefinitions: []*models.CustomFieldDefinitionExport{
					{
						MachineKey:    "impact",
						FieldType:     models.CustomFieldTypeNumber,
						Title:         "Impact",
						Configuration: &models.CustomFieldConfiguration{Precision: &precision},
					},
				},
			},
		}

		data, err := json.Marshal(projects)
		require.NoError(t, err)

		var zipBuf bytes.Buffer
		zw := zip.NewWriter(&zipBuf)
		vf, err := zw.Create("VERSION")
		require.NoError(t, err)
		_, err = vf.Write([]byte("dev"))
		require.NoError(t, err)
		df, err := zw.Create("data.json")
		require.NoError(t, err)
		_, err = df.Write(data)
		require.NoError(t, err)
		require.NoError(t, zw.Close())

		m := &FileMigrator{}
		u := &user.User{ID: 1}
		reader := bytes.NewReader(zipBuf.Bytes())
		require.NoError(t, m.Migrate(u, reader, int64(reader.Len())))

		s := db.NewSession()
		defer s.Close()

		project := &models.Project{}
		exists, err := s.Where("title = ?", "CF Project").Get(project)
		require.NoError(t, err)
		require.True(t, exists)

		def := &models.CustomFieldDefinition{}
		exists, err = s.Where("project_id = ? AND machine_key = ?", project.ID, "impact").Get(def)
		require.NoError(t, err)
		require.True(t, exists)
		assert.Equal(t, models.CustomFieldTypeNumber, def.FieldType)

		task := &models.Task{}
		exists, err = s.Where("project_id = ? AND title = ?", project.ID, "CF Task").Get(task)
		require.NoError(t, err)
		require.True(t, exists)

		// The number value is stored scaled at the definition's precision.
		db.AssertExists(t, "custom_field_values", map[string]interface{}{
			"task_id":       task.ID,
			"definition_id": def.ID,
			"value_number":  int64(75),
		}, false)
	})
}
