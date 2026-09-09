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
	"sync"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/require"
	"xorm.io/xorm/schemas"
)

func intPtr(i int) *int { return &i }

// A value write must not advance the project or definition timestamp: the wiki
// contract reserves those for definition and option changes, and bumping them
// would invalidate every task's custom-field ETag.
func TestSetCustomFieldValueDoesNotTouchDefinitionOrProjectTimestamp(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	projectBefore, err := GetProjectSimpleByID(s, 1)
	require.NoError(t, err)
	defBefore, err := GetCustomFieldDefinitionByID(s, 1)
	require.NoError(t, err)

	number := &CustomFieldNumber{}
	require.NoError(t, number.SetRaw("7.5"))
	require.NoError(t, SetCustomFieldValue(s, 1, defBefore, &CustomFieldValue{Type: CustomFieldTypeNumber, Number: number}))
	require.NoError(t, s.Commit())

	projectAfter, err := GetProjectSimpleByID(s, 1)
	require.NoError(t, err)
	require.Equal(t, projectBefore.Updated, projectAfter.Updated, "a value write must not advance the project timestamp")

	defAfter, err := GetCustomFieldDefinitionByID(s, 1)
	require.NoError(t, err)
	require.Equal(t, defBefore.Updated, defAfter.Updated, "a value write must not advance the definition timestamp")
}

// The definition-row lock serializes value writes with permanent deletion and
// definition updates. These races are only meaningful where row locks exist:
// the shared in-memory SQLite test engine has no busy timeout, so concurrent
// writers fail at the database layer there. PostgreSQL/MySQL in CI exercise the
// real lock.
func skipWithoutRowLocks(t *testing.T) {
	t.Helper()
	if db.Type() == schemas.SQLITE {
		t.Skip("the shared in-memory SQLite test database has no busy timeout; the definition-row lock is exercised on PostgreSQL/MySQL in CI")
	}
}

// Permanent-delete racing with a value set: either the set wins and its value is
// deleted with the definition, or the delete wins and the set reports the
// definition gone. No value row may reference a deleted definition, and the
// winning transaction must commit cleanly (a deadlock or database failure is not
// a successful serialization).
func TestCustomFieldValuePermanentDeleteRace(t *testing.T) {
	skipWithoutRowLocks(t)
	db.LoadAndAssertFixtures(t)

	var wg sync.WaitGroup
	var delErr, delCommitErr, setErr, setCommitErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		s := db.NewSession()
		defer s.Close()
		delErr = (&CustomFieldDefinition{ID: 1, ProjectID: 1}).DeletePermanently(s, true)
		if delErr != nil {
			_ = s.Rollback()
			return
		}
		delCommitErr = s.Commit()
	}()
	go func() {
		defer wg.Done()
		s := db.NewSession()
		defer s.Close()
		number := &CustomFieldNumber{}
		if err := number.SetRaw("7.5"); err != nil {
			setErr = err
			return
		}
		setErr = SetCustomFieldValue(s, 1, &CustomFieldDefinition{ID: 1}, &CustomFieldValue{Type: CustomFieldTypeNumber, Number: number})
		if setErr != nil {
			_ = s.Rollback()
			return
		}
		setCommitErr = s.Commit()
	}()
	wg.Wait()

	require.NoError(t, delErr)
	require.NoError(t, delCommitErr, "the winning permanent delete must commit cleanly")

	s := db.NewSession()
	defer s.Close()
	exists, err := s.Where("id = ?", 1).Exist(&CustomFieldDefinition{})
	require.NoError(t, err)
	require.False(t, exists, "permanent delete must always remove the definition")

	count, err := s.Where("definition_id = ?", 1).Count(&TaskCustomFieldValue{})
	require.NoError(t, err)
	require.Zero(t, count, "no value row may reference a deleted definition")

	if setErr != nil {
		require.True(t, IsErrCustomFieldDefinitionDoesNotExist(setErr), "the set must either win or report the definition gone, got: %v", setErr)
	} else {
		require.NoError(t, setCommitErr, "a successful set must commit cleanly")
	}
}

// A definition update tightening a number constraint racing with a value set:
// no committed value may violate the final configuration. Definition 1 ships
// with stored values 7.5 and -2.5, which would make the min=100 update fail
// against existing data no matter what the setter does — so those values are
// removed first to make the race real. Exactly one operation wins and commits
// cleanly: the set wins and the update is rejected as invalidating stored
// values, or the update wins and the set is rejected as out of range.
func TestCustomFieldValueConstraintUpdateRace(t *testing.T) {
	skipWithoutRowLocks(t)
	db.LoadAndAssertFixtures(t)

	setupS := db.NewSession()
	_, err := setupS.Where("definition_id = ?", 1).Delete(&TaskCustomFieldValue{})
	require.NoError(t, err)
	require.NoError(t, setupS.Commit())
	setupS.Close()

	var wg sync.WaitGroup
	var updateErr, updateCommitErr, setErr, setCommitErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		s := db.NewSession()
		defer s.Close()
		updateErr = (&CustomFieldDefinition{
			ID: 1, ProjectID: 1, MachineKey: "impact", FieldType: CustomFieldTypeNumber, Title: "Impact",
			Configuration: &CustomFieldConfiguration{Precision: intPtr(1), Min: strPtr("100")},
		}).Update(s, &user.User{ID: 1})
		if updateErr != nil {
			_ = s.Rollback()
			return
		}
		updateCommitErr = s.Commit()
	}()
	go func() {
		defer wg.Done()
		s := db.NewSession()
		defer s.Close()
		number := &CustomFieldNumber{}
		if err := number.SetRaw("7.5"); err != nil {
			setErr = err
			return
		}
		setErr = SetCustomFieldValue(s, 1, &CustomFieldDefinition{ID: 1}, &CustomFieldValue{Type: CustomFieldTypeNumber, Number: number})
		if setErr != nil {
			_ = s.Rollback()
			return
		}
		setCommitErr = s.Commit()
	}()
	wg.Wait()

	// Exactly one operation wins and commits cleanly; the loser is rejected.
	switch {
	case updateErr == nil && setErr == nil:
		t.Fatal("exactly one of the update and the set must succeed, both did")
	case updateErr != nil && setErr != nil:
		t.Fatalf("exactly one of the update and the set must succeed, both failed (update: %v, set: %v)", updateErr, setErr)
	case updateErr == nil:
		require.NoError(t, updateCommitErr, "the winning update must commit cleanly")
		require.True(t, IsErrInvalidCustomFieldValue(setErr), "the set must be rejected as out of range, got: %v", setErr)
	default:
		require.NoError(t, setCommitErr, "the winning set must commit cleanly")
		require.True(t, IsErrCustomFieldConfigurationChangeInvalidatesValues(updateErr), "the update must be rejected as invalidating stored values, got: %v", updateErr)
	}

	// The final state must be internally consistent.
	s := db.NewSession()
	defer s.Close()
	final, err := GetCustomFieldDefinitionByID(s, 1)
	require.NoError(t, err)
	require.NoError(t, validateConfigurationAgainstStoredValues(s, final, final), "a committed value violates the final configuration")
}
