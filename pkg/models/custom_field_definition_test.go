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
	"encoding/json"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func numPtr(i int) *int { return &i }

func TestCustomFieldConfigurationUnmarshal(t *testing.T) {
	t.Run("unknown property rejected", func(t *testing.T) {
		var cfg CustomFieldConfiguration
		require.Error(t, json.Unmarshal([]byte(`{"precision":2,"nope":1}`), &cfg))
	})

	t.Run("known properties accepted", func(t *testing.T) {
		var cfg CustomFieldConfiguration
		require.NoError(t, json.Unmarshal([]byte(`{"precision":2,"min":"0.5","unit":"kg"}`), &cfg))
		assert.Equal(t, 2, *cfg.Precision)
		assert.Equal(t, "0.5", *cfg.Min)
		assert.Equal(t, "kg", cfg.Unit)
	})
}

func TestCustomFieldDefinitionValidateAndCanonicalise(t *testing.T) {
	t.Run("non-number rejects any config", func(t *testing.T) {
		cfg := &CustomFieldConfiguration{Precision: numPtr(2)}
		_, err := cfg.validateAndCanonicalise(CustomFieldTypeShortText)
		require.Error(t, err)
	})

	t.Run("number config canonicalised with precision", func(t *testing.T) {
		cfg := &CustomFieldConfiguration{Precision: numPtr(1), Min: strPtr("1.5")}
		canonical, err := cfg.validateAndCanonicalise(CustomFieldTypeNumber)
		require.NoError(t, err)
		require.NotNil(t, canonical)
		assert.Equal(t, 1, *canonical.Precision)
		assert.Equal(t, "1.5", *canonical.Min)
	})

	t.Run("empty config on number becomes nil", func(t *testing.T) {
		cfg := &CustomFieldConfiguration{}
		canonical, err := cfg.validateAndCanonicalise(CustomFieldTypeNumber)
		require.NoError(t, err)
		assert.Nil(t, canonical)
	})

	t.Run("encoded config over 16 KiB rejected", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := &CustomFieldDefinition{
			ProjectID:     2,
			MachineKey:    "oversized_config",
			Title:         "Oversized config",
			FieldType:     CustomFieldTypeNumber,
			Configuration: &CustomFieldConfiguration{Unit: strings.Repeat("x", 16*1024)},
		}
		require.Error(t, def.Create(s, &user.User{ID: 1}))
	})
}

func TestCustomFieldDefinition_Create(t *testing.T) {
	usr := &user.User{ID: 1}

	newDef := func() *CustomFieldDefinition {
		return &CustomFieldDefinition{
			ProjectID:   2,
			MachineKey:  "estimate",
			Title:       "Estimate",
			Description: "A plain number",
			FieldType:   CustomFieldTypeNumber,
		}
	}

	t.Run("normal", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := newDef()
		err := def.Create(s, usr)
		require.NoError(t, err)

		// Position backfilled as entity_id * 2^16
		assert.InDelta(t, float64(def.ID)*float64(65536), def.Position, 0.0001)
		err = s.Commit()
		require.NoError(t, err)

		db.AssertExists(t, "custom_field_definitions", map[string]interface{}{
			"id":          def.ID,
			"project_id":  2,
			"machine_key": "estimate",
			"field_type":  "number",
		}, false)
	})

	t.Run("configuration validated and stored", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := newDef()
		def.Configuration = &CustomFieldConfiguration{Precision: numPtr(2), Unit: "h"}
		err := def.Create(s, usr)
		require.NoError(t, err)
		err = s.Commit()
		require.NoError(t, err)

		got := &CustomFieldDefinition{}
		_, err = s.ID(def.ID).Get(got)
		require.NoError(t, err)
		require.NotNil(t, got.Configuration)
		assert.Equal(t, 2, *got.Configuration.Precision)
		assert.Equal(t, "h", got.Configuration.Unit)
	})

	t.Run("invalid machine key", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := newDef()
		def.MachineKey = "1invalid"
		require.Error(t, def.Create(s, usr))
	})

	t.Run("duplicate machine key in project", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := newDef()
		def.ProjectID = 1
		def.MachineKey = "impact" // fixture already has machine_key impact in project 1
		require.Error(t, def.Create(s, usr))
	})

	t.Run("same machine key allowed in another project", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := newDef()
		def.MachineKey = "impact"
		require.NoError(t, def.Create(s, usr))
	})

	t.Run("invalid field type", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := newDef()
		def.FieldType = "nope"
		require.Error(t, def.Create(s, usr))
	})

	t.Run("empty title", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := newDef()
		def.Title = ""
		require.Error(t, def.Create(s, usr))
	})

	t.Run("description too long", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := newDef()
		def.Description = strings.Repeat("a", 4097)
		require.Error(t, def.Create(s, usr))
	})

	t.Run("default value must match definition", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		def := newDef()
		def.DefaultValue = &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: strPtr("nope")}
		require.Error(t, def.Create(s, usr))
	})

	t.Run("reaching the definition limit", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// Project 2 already has one fixture definition (vendor). Fill to 100.
		for i := 0; i < 99; i++ {
			def := &CustomFieldDefinition{
				ProjectID:  2,
				MachineKey: "auto_" + strings.Repeat("x", i/26) + string(rune('a'+i%26)),
				Title:      "Auto",
				FieldType:  CustomFieldTypeShortText,
			}
			require.NoError(t, def.Create(s, usr))
		}

		over := newDef()
		over.MachineKey = "overlimit"
		err := over.Create(s, usr)
		require.Error(t, err)
		require.True(t, IsErrCustomFieldDefinitionLimitReached(err))
	})
}

func TestCustomFieldDefinition_Update(t *testing.T) {
	usr := &user.User{ID: 1}
	s := db.NewSession()
	defer s.Close()

	t.Run("normal", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		def := &CustomFieldDefinition{
			ID:            1,
			ProjectID:     1,
			MachineKey:    "impact",
			Title:         "New Title",
			FieldType:     CustomFieldTypeNumber,
			Configuration: &CustomFieldConfiguration{Precision: numPtr(1), Unit: "points"},
		}
		require.NoError(t, def.Update(s, usr))
		require.NoError(t, s.Commit())

		got := &CustomFieldDefinition{}
		_, err := s.ID(1).Get(got)
		require.NoError(t, err)
		assert.Equal(t, "New Title", got.Title)
	})

	t.Run("machine key immutable", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		def := &CustomFieldDefinition{
			ID:         1,
			ProjectID:  1,
			MachineKey: "other",
			Title:      "New Title",
			FieldType:  CustomFieldTypeNumber,
		}
		require.Error(t, def.Update(s, usr))
	})

	t.Run("field type immutable", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		def := &CustomFieldDefinition{
			ID:         1,
			ProjectID:  1,
			MachineKey: "impact",
			Title:      "New Title",
			FieldType:  CustomFieldTypeShortText,
		}
		require.Error(t, def.Update(s, usr))
	})

	t.Run("project immutable", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		def := &CustomFieldDefinition{
			ID:         1,
			ProjectID:  2,
			MachineKey: "impact",
			Title:      "Impact",
			FieldType:  CustomFieldTypeNumber,
		}
		require.Error(t, def.Update(s, usr))
	})

	t.Run("precision change with stored values rejected", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		def := &CustomFieldDefinition{
			ID:            1,
			ProjectID:     1,
			MachineKey:    "impact",
			Title:         "Impact",
			FieldType:     CustomFieldTypeNumber,
			Configuration: &CustomFieldConfiguration{Precision: numPtr(2)},
		}
		err := def.Update(s, usr)
		require.Error(t, err)
		require.True(t, IsErrCustomFieldConfigurationChangeInvalidatesValues(err))
	})

	t.Run("new constraint invalidating stored values rejected", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		def := &CustomFieldDefinition{
			ID:            1,
			ProjectID:     1,
			MachineKey:    "impact",
			Title:         "Impact",
			FieldType:     CustomFieldTypeNumber,
			Configuration: &CustomFieldConfiguration{Precision: numPtr(1), Min: strPtr("8.0")},
		}
		err := def.Update(s, usr)
		require.Error(t, err)
		require.True(t, IsErrCustomFieldConfigurationChangeInvalidatesValues(err))
	})
}

func TestValidateValueAgainstDefinition(t *testing.T) {
	t.Run("short text length", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		ses := db.NewSession()
		defer ses.Close()

		def := &CustomFieldDefinition{FieldType: CustomFieldTypeShortText}
		v := &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: strPtr(strings.Repeat("a", 256))}
		require.Error(t, validateValueAgainstDefinition(ses, v, def))
		v.ShortText = strPtr("ok")
		require.NoError(t, validateValueAgainstDefinition(ses, v, def))
	})

	t.Run("long text byte length", func(t *testing.T) {
		ses := db.NewSession()
		defer ses.Close()

		def := &CustomFieldDefinition{FieldType: CustomFieldTypeLongText}
		v := &CustomFieldValue{Type: CustomFieldTypeLongText, LongText: strPtr(strings.Repeat("a", 65536))}
		require.Error(t, validateValueAgainstDefinition(ses, v, def))
	})

	t.Run("url scheme and length", func(t *testing.T) {
		ses := db.NewSession()
		defer ses.Close()

		def := &CustomFieldDefinition{FieldType: CustomFieldTypeURL}
		valid := &CustomFieldValue{Type: CustomFieldTypeURL, URL: strPtr("https://example.com/x")}
		require.NoError(t, validateValueAgainstDefinition(ses, valid, def))
		for _, raw := range []string{"ftp://example.com", "not a url", "http://", "javascript:alert(1)"} {
			v := &CustomFieldValue{Type: CustomFieldTypeURL, URL: strPtr(raw)}
			require.Error(t, validateValueAgainstDefinition(ses, v, def), raw)
		}
	})

	t.Run("number against precision and min max step", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		ses := db.NewSession()
		defer ses.Close()

		n := &CustomFieldNumber{}
		require.NoError(t, n.SetRaw("7.5"))

		def := &CustomFieldDefinition{
			ID:            1,
			FieldType:     CustomFieldTypeNumber,
			Configuration: &CustomFieldConfiguration{Precision: numPtr(1), Min: strPtr("5"), Max: strPtr("10"), Step: strPtr("0.5")},
		}
		require.NoError(t, validateValueAgainstDefinition(ses, &CustomFieldValue{Type: CustomFieldTypeNumber, Number: n}, def))

		below := &CustomFieldNumber{}
		require.NoError(t, below.SetRaw("4"))
		require.Error(t, validateValueAgainstDefinition(ses, &CustomFieldValue{Type: CustomFieldTypeNumber, Number: below}, def))

		offStep := &CustomFieldNumber{}
		require.NoError(t, offStep.SetRaw("7.6"))
		require.Error(t, validateValueAgainstDefinition(ses, &CustomFieldValue{Type: CustomFieldTypeNumber, Number: offStep}, def))
	})

	t.Run("option ownership and archiving", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		ses := db.NewSession()
		defer ses.Close()

		def := &CustomFieldDefinition{ID: 3, FieldType: CustomFieldTypeSingleSelect}
		v := &CustomFieldValue{Type: CustomFieldTypeSingleSelect, SingleOptionID: int64Ptr(1)}
		require.NoError(t, validateValueAgainstDefinition(ses, v, def))

		// option 5 belongs to definition 4
		v.SingleOptionID = int64Ptr(5)
		require.Error(t, validateValueAgainstDefinition(ses, v, def))

		// unknown option
		v.SingleOptionID = int64Ptr(99)
		require.Error(t, validateValueAgainstDefinition(ses, v, def))
	})

	t.Run("multi select validates each option", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		ses := db.NewSession()
		defer ses.Close()

		def := &CustomFieldDefinition{ID: 4, FieldType: CustomFieldTypeMultiSelect}
		v := &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: []int64{3, 4}}
		require.NoError(t, validateValueAgainstDefinition(ses, v, def))

		// archived option can no longer be selected
		v.OptionIDs = []int64{5}
		require.Error(t, validateValueAgainstDefinition(ses, v, def))

		// option of a different definition
		v.OptionIDs = []int64{1}
		require.Error(t, validateValueAgainstDefinition(ses, v, def))
	})

	t.Run("user must be visible in project", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		ses := db.NewSession()
		defer ses.Close()

		def := &CustomFieldDefinition{ID: 6, ProjectID: 1, FieldType: CustomFieldTypeUser}
		// user 1 owns project 1 -> visible
		require.NoError(t, validateValueAgainstDefinition(ses, &CustomFieldValue{Type: CustomFieldTypeUser, UserID: int64Ptr(1)}, def))
		// user 4 has no membership in project 1 -> not visible
		require.Error(t, validateValueAgainstDefinition(ses, &CustomFieldValue{Type: CustomFieldTypeUser, UserID: int64Ptr(4)}, def))
		// unknown user
		require.Error(t, validateValueAgainstDefinition(ses, &CustomFieldValue{Type: CustomFieldTypeUser, UserID: int64Ptr(99)}, def))
	})
}
