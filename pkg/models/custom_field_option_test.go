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
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomFieldOption_Create(t *testing.T) {
	usr := &user.User{ID: 1}

	newOpt := func() *CustomFieldOption {
		return &CustomFieldOption{
			DefinitionID: 4,
			MachineKey:   "design",
			Label:        "Design",
		}
	}

	t.Run("normal", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		opt := newOpt()
		require.NoError(t, opt.Create(s, usr))
		require.NoError(t, s.Commit())

		assert.InDelta(t, float64(opt.ID)*float64(65536), opt.Position, 0.0001)
		db.AssertExists(t, "custom_field_options", map[string]interface{}{
			"id":            opt.ID,
			"definition_id": 4,
			"machine_key":   "design",
			"label":         "Design",
		}, false)
	})

	t.Run("hex colour normalised", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		opt := newOpt()
		opt.MachineKey = "design2"
		opt.HexColor = "#FFF000"
		require.NoError(t, opt.Create(s, usr))
		require.NoError(t, s.Commit())

		db.AssertExists(t, "custom_field_options", map[string]interface{}{
			"id":          opt.ID,
			"machine_key": "design2",
			"hex_color":   "FFF000",
		}, false)
	})

	t.Run("only select definitions may have options", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		opt := newOpt()
		opt.DefinitionID = 1 // number
		require.Error(t, opt.Create(s, usr))
	})

	t.Run("invalid machine key", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		opt := newOpt()
		opt.MachineKey = "design-1"
		require.Error(t, opt.Create(s, usr))
	})

	t.Run("duplicate machine key in definition", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		opt := newOpt()
		opt.MachineKey = "backend" // fixture already has it on definition 4
		require.Error(t, opt.Create(s, usr))
	})

	t.Run("same machine key allowed on another definition", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		opt := newOpt()
		opt.DefinitionID = 3
		opt.MachineKey = "backend"
		require.NoError(t, opt.Create(s, usr))
	})

	t.Run("label empty", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		opt := newOpt()
		opt.Label = ""
		require.Error(t, opt.Create(s, usr))
	})

	t.Run("reaching the option limit", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// Definition 4 already has three fixture options (backend, frontend, docs). Fill to 200.
		for i := 0; i < 197; i++ {
			opt := &CustomFieldOption{
				DefinitionID: 4,
				MachineKey:   "auto_" + strings.Repeat("x", i/26) + string(rune('a'+i%26)),
				Label:        "Auto",
			}
			require.NoError(t, opt.Create(s, usr))
		}

		over := newOpt()
		over.MachineKey = "overlimit"
		err := over.Create(s, usr)
		require.Error(t, err)
		require.True(t, IsErrCustomFieldOptionLimitReached(err))
	})
}

func TestCustomFieldOption_Update(t *testing.T) {
	usr := &user.User{ID: 1}
	s := db.NewSession()
	defer s.Close()

	t.Run("normal", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		opt := &CustomFieldOption{
			ID:           1,
			DefinitionID: 3,
			MachineKey:   "todo",
			Label:        "To Do Renamed",
		}
		require.NoError(t, opt.Update(s, usr))
		require.NoError(t, s.Commit())

		got := &CustomFieldOption{}
		_, err := s.ID(1).Get(got)
		require.NoError(t, err)
		assert.Equal(t, "To Do Renamed", got.Label)
	})

	t.Run("machine key immutable", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		opt := &CustomFieldOption{
			ID:           1,
			DefinitionID: 3,
			MachineKey:   "other",
			Label:        "To Do",
		}
		require.Error(t, opt.Update(s, usr))
	})

	t.Run("cannot be moved to another definition", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		opt := &CustomFieldOption{
			ID:           1,
			DefinitionID: 4,
			MachineKey:   "todo",
			Label:        "To Do",
		}
		require.Error(t, opt.Update(s, usr))
	})

	t.Run("archiving", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		opt := &CustomFieldOption{
			ID:           1,
			DefinitionID: 3,
			MachineKey:   "todo",
			Label:        "To Do",
			IsArchived:   true,
		}
		require.NoError(t, opt.Update(s, usr))
		require.NoError(t, s.Commit())

		db.AssertExists(t, "custom_field_options", map[string]interface{}{
			"id":          int64(1),
			"is_archived": true,
		}, false)
	})
}
