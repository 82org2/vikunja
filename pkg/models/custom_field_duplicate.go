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
	"fmt"
	"slices"

	"code.vikunja.io/api/pkg/user"
	"xorm.io/builder"
	"xorm.io/xorm"
)

// duplicateCustomFieldDefinitions clones the definitions and options of a
// project, remapping DefaultValue option ids through the option map, and
// returns the old->new id maps. Definitions and options are inserted directly
// (bypassing Create) so the source position, archived state, and show flags
// are preserved verbatim and the per-project limits are not re-checked (the
// source already satisfied them).
func duplicateCustomFieldDefinitions(s *xorm.Session, sourceProjectID, destProjectID int64) (defRemap, optionRemap map[int64]int64, err error) {
	srcDefs, err := getCustomFieldDefinitionsForProject(s, sourceProjectID)
	if err != nil {
		return nil, nil, err
	}
	defRemap = make(map[int64]int64, len(srcDefs))
	optionRemap = make(map[int64]int64)

	// Clone the definitions first so the option clones can reference their new
	// definition ids. DefaultValue is set after options are cloned so its
	// option ids can be remapped.
	clonedDefs := make([]*CustomFieldDefinition, 0, len(srcDefs))
	for _, src := range srcDefs {
		clone := &CustomFieldDefinition{
			ProjectID:     destProjectID,
			MachineKey:    src.MachineKey,
			Title:         src.Title,
			Description:   src.Description,
			FieldType:     src.FieldType,
			IsArchived:    src.IsArchived,
			Position:      src.Position,
			ShowOnCard:    src.ShowOnCard,
			ShowInTable:   src.ShowInTable,
			Configuration: src.Configuration,
		}
		if _, err := s.Insert(clone); err != nil {
			return nil, nil, err
		}
		defRemap[src.ID] = clone.ID
		clonedDefs = append(clonedDefs, clone)
	}

	// Clone the options, remapping each to its cloned definition.
	srcDefIDs := make([]int64, 0, len(srcDefs))
	for _, def := range srcDefs {
		srcDefIDs = append(srcDefIDs, def.ID)
	}
	srcOptions := []*CustomFieldOption{}
	if len(srcDefIDs) > 0 {
		if err := s.In("definition_id", srcDefIDs).Find(&srcOptions); err != nil {
			return nil, nil, err
		}
	}
	for _, src := range srcOptions {
		clone := &CustomFieldOption{
			DefinitionID: defRemap[src.DefinitionID],
			MachineKey:   src.MachineKey,
			Label:        src.Label,
			HexColor:     src.HexColor,
			IsArchived:   src.IsArchived,
			Position:     src.Position,
		}
		if _, err := s.Insert(clone); err != nil {
			return nil, nil, err
		}
		optionRemap[src.ID] = clone.ID
	}

	// Set each cloned definition's default value with its option ids remapped.
	for i, src := range srcDefs {
		if src.DefaultValue == nil {
			continue
		}
		value := *src.DefaultValue
		if err := remapDefaultValueOptionIDs(&value, optionRemap); err != nil {
			return nil, nil, err
		}
		if _, err := s.ID(clonedDefs[i].ID).Cols("default_value").Update(&CustomFieldDefinition{DefaultValue: &value}); err != nil {
			return nil, nil, err
		}
	}

	// The direct inserts bypass Create, so advance the destination project
	// timestamp explicitly: definition and option mutations must invalidate
	// cached task representations.
	if err := updateProjectLastUpdated(s, &Project{ID: destProjectID}); err != nil {
		return nil, nil, err
	}
	return defRemap, optionRemap, nil
}

// validateProjectCustomFieldUserValues rejects a project whose user-type custom
// field values or defaults reference users not visible in the project. Project
// duplication runs this after shares are copied, so the destination's
// visibility is final; silently dropping such a value would lose data. Value
// rows are paged by id and each distinct user is validated once, so a project
// at the documented limits never accumulates every value in memory or issues a
// visibility query per row.
func validateProjectCustomFieldUserValues(s *xorm.Session, projectID int64) error {
	defs, err := getCustomFieldDefinitionsForProject(s, projectID)
	if err != nil {
		return err
	}
	defIDs := make([]int64, 0, len(defs))
	for _, def := range defs {
		defIDs = append(defIDs, def.ID)
	}

	// User-type defaults are few per project; validate them directly.
	for _, def := range defs {
		if def.FieldType == CustomFieldTypeUser && def.DefaultValue != nil && def.DefaultValue.UserID != nil {
			if err := validateUserValue(s, *def.DefaultValue.UserID, def); err != nil {
				return err
			}
		}
	}

	if len(defIDs) == 0 {
		return nil
	}

	// Page the user-type value rows by id and collect the distinct users.
	userIDs := make([]int64, 0)
	seen := map[int64]struct{}{}
	for chunk := range slices.Chunk(defIDs, 500) {
		lastID := int64(0)
		for {
			rows := []*TaskCustomFieldValue{}
			if err := s.In("definition_id", chunk).
				Where(builder.NotNull{"value_user_id"}).
				And(builder.Gt{"id": lastID}).
				OrderBy("id asc").
				Limit(500).
				Cols("id", "value_user_id").
				Find(&rows); err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				if row.ID > lastID {
					lastID = row.ID
				}
				if _, ok := seen[*row.ValueUserID]; ok {
					continue
				}
				seen[*row.ValueUserID] = struct{}{}
				userIDs = append(userIDs, *row.ValueUserID)
			}
		}
	}

	// Load the referenced users in bounded batches and validate each distinct
	// user once: active and visible in the project.
	users := make(map[int64]*user.User)
	for chunk := range slices.Chunk(userIDs, 500) {
		batch, err := user.GetUsersByIDs(s, chunk)
		if err != nil {
			return err
		}
		for id, u := range batch {
			users[id] = u
		}
	}
	project, _, err := getProjectSimple(s, builder.Eq{"id": projectID})
	if err != nil {
		return err
	}
	for _, uid := range userIDs {
		u, ok := users[uid]
		if !ok {
			return ErrCustomFieldUserNotVisible{UserID: uid}
		}
		if u.Status == user.StatusDisabled || u.Status == user.StatusAccountLocked {
			return ErrCustomFieldUserNotVisible{UserID: uid}
		}
		can, _, err := project.CanRead(s, u)
		if err != nil {
			return err
		}
		if !can {
			return ErrCustomFieldUserNotVisible{UserID: uid}
		}
	}
	return nil
}

// remapDefaultValueOptionIDs rewrites the option references of a select
// default value through the option map.
func remapDefaultValueOptionIDs(v *CustomFieldValue, optionRemap map[int64]int64) error {
	switch v.Type {
	case CustomFieldTypeSingleSelect:
		if v.SingleOptionID != nil {
			mapped, ok := optionRemap[*v.SingleOptionID]
			if !ok {
				return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("No destination option for source option %d.", *v.SingleOptionID)}
			}
			v.SingleOptionID = &mapped
		}
	case CustomFieldTypeMultiSelect:
		for i, optID := range v.OptionIDs {
			mapped, ok := optionRemap[optID]
			if !ok {
				return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("No destination option for source option %d.", optID)}
			}
			v.OptionIDs[i] = mapped
		}
	case CustomFieldTypeShortText, CustomFieldTypeLongText, CustomFieldTypeNumber,
		CustomFieldTypeBoolean, CustomFieldTypeDate, CustomFieldTypeDateTime,
		CustomFieldTypeURL, CustomFieldTypeUser:
		// Only select defaults reference options.
	}
	return nil
}
