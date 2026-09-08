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

package migration

import (
	"testing"

	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
	"xorm.io/xorm/schemas"
)

func customFieldIndex20260908204428(t *testing.T, x *xorm.Engine, table, name string) *schemas.Index {
	t.Helper()
	tables, err := x.DBMetas()
	require.NoError(t, err)
	for _, meta := range tables {
		if meta.Name != table {
			continue
		}
		for _, index := range meta.Indexes {
			// Dialects strip the "UQE_<table>_" prefix off model-style index names; XName puts it back.
			if index.XName(table) == name {
				return index
			}
		}
		return nil
	}
	t.Fatalf("%s table not found", table)
	return nil
}

func TestCustomFieldTables20260908204428(t *testing.T) {
	x, err := db.CreateTestEngine()
	require.NoError(t, err)

	tables := []interface{}{
		customFieldDefinition20260908204428{},
		customFieldOption20260908204428{},
		taskCustomFieldValue20260908204428{},
		customFieldValueOption20260908204428{},
	}
	// x is the process-global test engine; the four tables may exist from a prior run.
	t.Cleanup(func() {
		require.NoError(t, x.DropTables(tables...))
	})
	require.NoError(t, x.DropTables(tables...))

	require.NoError(t, addCustomFields20260908204428(x))

	// Every composite unique key must exist; a missing one would silently weaken the table.
	uniqueIndexes := map[string]map[string]string{
		"custom_field_definitions": {
			"UQE_custom_field_definitions_project_machine_key": "project_machine_key",
		},
		"custom_field_options": {
			"UQE_custom_field_options_definition_machine_key": "definition_machine_key",
		},
		"custom_field_values": {
			"UQE_custom_field_values_task_definition": "task_definition",
		},
		"custom_field_value_options": {
			"UQE_custom_field_value_options_value_option": "value_option",
		},
	}
	for table, indexes := range uniqueIndexes {
		for name := range indexes {
			index := customFieldIndex20260908204428(t, x, table, name)
			require.NotNil(t, index, "%s on %s", name, table)
			require.Equal(t, schemas.UniqueType, index.Type, "%s on %s", name, table)
		}
	}

	regularIndexes := map[string]map[string][]string{
		"custom_field_definitions": {
			"IDX_custom_field_definitions_project_is_archived_position": {"project_id", "is_archived", "position"},
		},
		"custom_field_options": {
			"IDX_custom_field_options_definition_is_archived_position": {"definition_id", "is_archived", "position"},
		},
		"custom_field_values": {
			"IDX_custom_field_values_definition_short_text":    {"definition_id", "value_short_text"},
			"IDX_custom_field_values_definition_number":        {"definition_id", "value_number"},
			"IDX_custom_field_values_definition_boolean":       {"definition_id", "value_boolean"},
			"IDX_custom_field_values_definition_date":          {"definition_id", "value_date"},
			"IDX_custom_field_values_definition_datetime":      {"definition_id", "value_datetime"},
			"IDX_custom_field_values_definition_user":          {"definition_id", "value_user_id"},
			"IDX_custom_field_values_definition_single_option": {"definition_id", "value_single_option_id"},
		},
	}
	for table, indexes := range regularIndexes {
		for name, columns := range indexes {
			index := customFieldIndex20260908204428(t, x, table, name)
			require.NotNil(t, index, "%s on %s", name, table)
			require.Equal(t, schemas.IndexType, index.Type, "%s on %s", name, table)
			require.Equal(t, columns, index.Cols, "%s on %s", name, table)
		}
	}

	// Idempotent: running the migration again keeps the tables and their constraints.
	require.NoError(t, addCustomFields20260908204428(x))

	// The materialised unique keys actually reject duplicates.
	_, err = x.Insert(&customFieldDefinition20260908204428{
		ID:         1,
		ProjectID:  1,
		MachineKey: "impact",
		FieldType:  "number",
	})
	require.NoError(t, err)
	_, err = x.Insert(&customFieldDefinition20260908204428{
		ID:         2,
		ProjectID:  1,
		MachineKey: "impact",
		FieldType:  "number",
	})
	require.Error(t, err)
}
