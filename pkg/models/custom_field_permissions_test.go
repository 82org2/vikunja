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
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/license"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/require"
)

// Fixture topology: user1 owns project 1 and has read on project 3, read on 9,
// write on 10, and admin on 11 (users_projects.yml). user3 owns projects 2 and
// 3. Definitions 1-6 live in project 1, 7 in project 2, 8 in 9, 9 in 10, 10 in
// 11, 11 in project 3.

func TestCustomFieldDefinitionPermissions(t *testing.T) {
	owner := &user.User{ID: 1}
	adminOfProjectTwo := &user.User{ID: 3}
	readShare := &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionRead}
	writeShare := &LinkSharing{ID: 9, ProjectID: 1, Permission: PermissionWrite}
	adminShare := &LinkSharing{ID: 3, ProjectID: 3, Permission: PermissionAdmin}

	t.Run("CanRead", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()

		can, _, err := (&CustomFieldDefinition{ID: 1, ProjectID: 1}).CanRead(s, owner)
		require.NoError(t, err)
		require.True(t, can)

		can, _, err = (&CustomFieldDefinition{ID: 1, ProjectID: 1}).CanRead(s, readShare)
		require.NoError(t, err)
		require.True(t, can)

		// user1 holds a read share on projects 3 and 9, so its definitions are readable.
		can, _, err = (&CustomFieldDefinition{ID: 11, ProjectID: 3}).CanRead(s, owner)
		require.NoError(t, err)
		require.True(t, can)
		can, _, err = (&CustomFieldDefinition{ID: 8, ProjectID: 9}).CanRead(s, owner)
		require.NoError(t, err)
		require.True(t, can)

		// user1 has no access to project 2.
		can, _, err = (&CustomFieldDefinition{ID: 7, ProjectID: 2}).CanRead(s, owner)
		require.NoError(t, err)
		require.False(t, can)

		// Reading a definition that does not belong to the path project is not found.
		_, _, err = (&CustomFieldDefinition{ID: 1, ProjectID: 2}).CanRead(s, owner)
		require.Error(t, err)
		require.True(t, IsErrCustomFieldDefinitionDoesNotExist(err))
	})

	t.Run("CanCreate", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()

		can, err := (&CustomFieldDefinition{ProjectID: 1}).CanCreate(s, owner)
		require.NoError(t, err)
		require.True(t, can)

		// A read link share cannot manage definitions.
		can, err = (&CustomFieldDefinition{ProjectID: 1}).CanCreate(s, readShare)
		require.NoError(t, err)
		require.False(t, can)

		// Admin of a different project cannot create in project 1.
		can, err = (&CustomFieldDefinition{ProjectID: 1}).CanCreate(s, adminOfProjectTwo)
		require.NoError(t, err)
		require.False(t, can)

		// The same user is admin of their own project 2.
		can, err = (&CustomFieldDefinition{ProjectID: 2}).CanCreate(s, adminOfProjectTwo)
		require.NoError(t, err)
		require.True(t, can)
	})

	t.Run("CanUpdate", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()

		can, err := (&CustomFieldDefinition{ID: 1, ProjectID: 1}).CanUpdate(s, owner)
		require.NoError(t, err)
		require.True(t, can)

		// Write shares stay below the admin bar.
		can, err = (&CustomFieldDefinition{ID: 1, ProjectID: 1}).CanUpdate(s, writeShare)
		require.NoError(t, err)
		require.False(t, can)

		// Definition 9 is in project 10, where user1 only has write.
		can, err = (&CustomFieldDefinition{ID: 9, ProjectID: 10}).CanUpdate(s, owner)
		require.NoError(t, err)
		require.False(t, can)

		// Definition 10 is in project 11, where user1 has admin.
		can, err = (&CustomFieldDefinition{ID: 10, ProjectID: 11}).CanUpdate(s, owner)
		require.NoError(t, err)
		require.True(t, can)

		// An admin link share on the definition's project may manage it.
		can, err = (&CustomFieldDefinition{ID: 11, ProjectID: 3}).CanUpdate(s, adminShare)
		require.NoError(t, err)
		require.True(t, can)

		// Admin of the path project must not mutate another project's definition.
		_, err = (&CustomFieldDefinition{ID: 5, ProjectID: 2}).CanUpdate(s, owner)
		require.Error(t, err)
		require.True(t, IsErrCustomFieldDefinitionDoesNotExist(err))
	})
}

func TestCustomFieldOptionPermissions(t *testing.T) {
	owner := &user.User{ID: 1}
	readShare := &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionRead}
	writeShare := &LinkSharing{ID: 9, ProjectID: 1, Permission: PermissionWrite}
	adminShare := &LinkSharing{ID: 3, ProjectID: 3, Permission: PermissionAdmin}

	t.Run("CanRead", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()

		can, _, err := (&CustomFieldOption{ID: 1, DefinitionID: 3, ProjectID: 1}).CanRead(s, owner)
		require.NoError(t, err)
		require.True(t, can)

		can, _, err = (&CustomFieldOption{ID: 1, DefinitionID: 3, ProjectID: 1}).CanRead(s, readShare)
		require.NoError(t, err)
		require.True(t, can)

		// Option 6 belongs to definition 8 in project 9 (user1 read share).
		can, _, err = (&CustomFieldOption{ID: 6, DefinitionID: 8, ProjectID: 9}).CanRead(s, owner)
		require.NoError(t, err)
		require.True(t, can)

		// Option under the wrong definition is not found.
		_, _, err = (&CustomFieldOption{ID: 1, DefinitionID: 4, ProjectID: 1}).CanRead(s, owner)
		require.Error(t, err)
		require.True(t, IsErrCustomFieldOptionDoesNotExist(err))

		// Definition in the path project but option elsewhere is not found too.
		_, _, err = (&CustomFieldOption{ID: 6, DefinitionID: 10, ProjectID: 1}).CanRead(s, owner)
		require.Error(t, err)
	})

	t.Run("CanUpdate", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()

		can, err := (&CustomFieldOption{ID: 1, DefinitionID: 3, ProjectID: 1}).CanUpdate(s, owner)
		require.NoError(t, err)
		require.True(t, can)

		can, err = (&CustomFieldOption{ID: 1, DefinitionID: 3, ProjectID: 1}).CanUpdate(s, writeShare)
		require.NoError(t, err)
		require.False(t, can)

		// Option 7 belongs to definition 10 in project 11 (user1 admin share).
		can, err = (&CustomFieldOption{ID: 7, DefinitionID: 10, ProjectID: 11}).CanUpdate(s, owner)
		require.NoError(t, err)
		require.True(t, can)

		// An admin link share on the definition's project may manage its options.
		can, err = (&CustomFieldOption{ID: 6, DefinitionID: 8, ProjectID: 9}).CanUpdate(s, adminShare)
		require.NoError(t, err)
		require.False(t, can) // adminShare is scoped to project 3, not 9
	})
}

func TestCanWriteCustomFieldValue(t *testing.T) {
	user1 := &user.User{ID: 1}
	user3 := &user.User{ID: 3}
	readShare := &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionRead}
	writeShare := &LinkSharing{ID: 9, ProjectID: 1, Permission: PermissionWrite}

	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	// Task 1 is in project 1, owned by user1.
	can, err := CanWriteCustomFieldValue(s, 1, user1)
	require.NoError(t, err)
	require.True(t, can)

	// Task 21 is in project 3, where user1 only has a read share.
	can, err = CanWriteCustomFieldValue(s, 21, user1)
	require.NoError(t, err)
	require.False(t, can)

	// Task 13 is in project 2, to which user1 has no access.
	can, err = CanWriteCustomFieldValue(s, 13, user1)
	require.NoError(t, err)
	require.False(t, can)

	// user3 owns project 2, so task 13 is writable for them.
	can, err = CanWriteCustomFieldValue(s, 13, user3)
	require.NoError(t, err)
	require.True(t, can)

	// Write link shares on the task's project may set values; read shares may not.
	can, err = CanWriteCustomFieldValue(s, 1, writeShare)
	require.NoError(t, err)
	require.True(t, can)

	can, err = CanWriteCustomFieldValue(s, 1, readShare)
	require.NoError(t, err)
	require.False(t, can)
}

func TestCanWriteCustomFieldValueTaskDoesNotExist(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	can, err := CanWriteCustomFieldValue(s, 9999, &user.User{ID: 1})
	require.Error(t, err)
	require.False(t, can)
}

func TestCustomFieldDefinitionArchiveKeepsValues(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	def := &CustomFieldDefinition{ID: 1, ProjectID: 1}
	require.NoError(t, def.Delete(s, &user.User{ID: 1}))

	// The row is kept and flagged archived; values are retained.
	archived, err := GetCustomFieldDefinitionByID(s, 1)
	require.NoError(t, err)
	require.True(t, archived.IsArchived)

	_, err = GetCustomFieldValue(s, 1, 1)
	require.NoError(t, err)
}

// An empty multi-select value means "unset", but only on a multi-select
// definition: the setter must still reject it for a number, foreign, or
// nonexistent definition instead of silently deleting a row.
func TestSetCustomFieldValueEmptyMultiSelectValidatesDefinition(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	// Definition 1 is a number field: an empty multi-select must fail type validation.
	def, err := GetCustomFieldDefinitionByID(s, 1)
	require.NoError(t, err)
	err = SetCustomFieldValue(s, 1, def, &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: []int64{}}, nil)
	require.Error(t, err)
	require.True(t, IsErrInvalidCustomFieldValue(err))

	// Definition 4 is a multi-select field: an empty selection unsets the row.
	def4, err := GetCustomFieldDefinitionByID(s, 4)
	require.NoError(t, err)
	require.NoError(t, SetCustomFieldValue(s, 2, def4, &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: []int64{}}, nil))
	_, err = GetCustomFieldValue(s, 2, 4)
	require.Error(t, err)
	require.True(t, IsErrCustomFieldValueDoesNotExist(err))
}

// The nested-parent guard must run before the instance-admin bypass: an admin
// cannot create or mutate an option or definition through a wrong-project path.
func TestCustomFieldParentGuardAppliesToInstanceAdmin(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	license.SetForTests([]license.Feature{license.FeatureAdminPanel})
	defer license.ResetForTests()
	s := db.NewSession()
	defer s.Close()

	_, err := s.ID(int64(2)).Cols("is_admin").Update(&user.User{IsAdmin: true})
	require.NoError(t, err)
	admin := &user.User{ID: 2, IsAdmin: true}

	// Option 1 belongs to definition 3 in project 1; a wrong-project path is a 404.
	can, err := (&CustomFieldOption{ID: 1, DefinitionID: 3, ProjectID: 2}).CanUpdate(s, admin)
	require.Error(t, err)
	require.True(t, IsErrCustomFieldOptionDoesNotExist(err))
	require.False(t, can)

	// The same admin through the correct path is allowed.
	can, err = (&CustomFieldOption{ID: 1, DefinitionID: 3, ProjectID: 1}).CanUpdate(s, admin)
	require.NoError(t, err)
	require.True(t, can)

	// Definition 1 is in project 1; a wrong-project read is a 404 for the admin too.
	_, _, err = (&CustomFieldDefinition{ID: 1, ProjectID: 2}).CanRead(s, admin)
	require.Error(t, err)
	require.True(t, IsErrCustomFieldDefinitionDoesNotExist(err))
}
