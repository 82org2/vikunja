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
	"sort"

	"xorm.io/xorm"
)

// preflightTaskMoveCustomFields validates that every populated custom field
// value of the task can be carried into the destination project: the
// destination must have a definition with the same machine key and type, the
// value must validate against it, and every selected option must exist in the
// destination definition by machine key. It locks every involved definition
// row (source and destination, in one ascending id order) so a concurrent value
// write or definition change cannot interleave between validation and rewrite,
// and returns the remap tables for the rewrite. It performs no writes, so a
// rejection happens before any mutation of the move.
func preflightTaskMoveCustomFields(s *xorm.Session, taskID, destProjectID int64) (defRemap, optionRemap map[int64]int64, err error) {
	rows := []*TaskCustomFieldValue{}
	if err := s.Where("task_id = ?", taskID).Find(&rows); err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return map[int64]int64{}, map[int64]int64{}, nil
	}

	// Load the source definitions and resolve the destination definitions by
	// machine key.
	srcDefIDs := make([]int64, 0, len(rows))
	seen := map[int64]struct{}{}
	for _, row := range rows {
		if _, ok := seen[row.DefinitionID]; ok {
			continue
		}
		seen[row.DefinitionID] = struct{}{}
		srcDefIDs = append(srcDefIDs, row.DefinitionID)
	}
	srcDefs, err := getCustomFieldDefinitionsByIDs(s, srcDefIDs)
	if err != nil {
		return nil, nil, err
	}
	keys := make([]string, 0, len(srcDefs))
	for _, def := range srcDefs {
		keys = append(keys, def.MachineKey)
	}
	destDefs, err := getCustomFieldDefinitionsByKeys(s, []int64{destProjectID}, keys)
	if err != nil {
		return nil, nil, err
	}

	// Lock every involved definition (source and destination) in one
	// deterministic ascending id order, so a concurrent definition update or
	// permanent-delete cannot interleave between validation and rewrite.
	allDefIDs := make([]int64, 0, len(srcDefs)+len(destDefs))
	allDefIDs = append(allDefIDs, srcDefIDs...)
	for _, def := range destDefs {
		allDefIDs = append(allDefIDs, def.ID)
	}
	sort.Slice(allDefIDs, func(i, j int) bool { return allDefIDs[i] < allDefIDs[j] })
	for _, defID := range allDefIDs {
		if err := lockCustomFieldDefinition(s, defID); err != nil {
			return nil, nil, err
		}
	}

	// Re-read the definitions after locking so validation sees the latest
	// committed state.
	srcDefs, err = getCustomFieldDefinitionsByIDs(s, srcDefIDs)
	if err != nil {
		return nil, nil, err
	}
	destDefs, err = getCustomFieldDefinitionsByKeys(s, []int64{destProjectID}, keys)
	if err != nil {
		return nil, nil, err
	}
	srcDefByID := make(map[int64]*CustomFieldDefinition, len(srcDefs))
	for _, def := range srcDefs {
		srcDefByID[def.ID] = def
	}
	destDefByKey := make(map[string]*CustomFieldDefinition, len(destDefs))
	for _, def := range destDefs {
		destDefByKey[def.MachineKey] = def
	}

	defRemap = make(map[int64]int64, len(srcDefs))
	optionRemap = make(map[int64]int64)

	for _, row := range rows {
		srcDef, ok := srcDefByID[row.DefinitionID]
		if !ok {
			return nil, nil, missingCustomFieldDefinitionError(row)
		}
		destDef, ok := destDefByKey[srcDef.MachineKey]
		if !ok {
			return nil, nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The destination project has no custom field definition with machine key %q.", srcDef.MachineKey)}
		}
		if destDef.FieldType != srcDef.FieldType {
			return nil, nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The destination definition %q has a different type than the source.", srcDef.MachineKey)}
		}
		defRemap[srcDef.ID] = destDef.ID

		value, err := row.FromRow(s, srcDef)
		if err != nil {
			return nil, nil, err
		}
		if value.Type == CustomFieldTypeSingleSelect || value.Type == CustomFieldTypeMultiSelect {
			if err := preflightSelectOptions(s, value, srcDef, destDef, optionRemap); err != nil {
				return nil, nil, err
			}
		} else if err := validateValueAgainstDefinition(s, value, destDef); err != nil {
			return nil, nil, err
		}
	}
	return defRemap, optionRemap, nil
}

// preflightSelectOptions resolves each selected option of a select value by
// machine key into the destination definition and records the remap. Archived
// destination options are allowed: a move relocates a retained value rather
// than selecting a new one, and archived options continue to describe retained
// values. A key absent from the destination definition rejects the move.
func preflightSelectOptions(s *xorm.Session, v *CustomFieldValue, srcDef, destDef *CustomFieldDefinition, optionRemap map[int64]int64) error {
	optionIDs := []int64{}
	if v.Type == CustomFieldTypeSingleSelect {
		if v.SingleOptionID == nil {
			return valueTypeMismatch(v.Type)
		}
		optionIDs = append(optionIDs, *v.SingleOptionID)
	} else {
		optionIDs = v.OptionIDs
	}

	for _, srcOptID := range optionIDs {
		srcOpt, err := GetCustomFieldOptionByID(s, srcOptID)
		if err != nil {
			return err
		}
		if srcOpt.DefinitionID != srcDef.ID {
			return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The option %d does not belong to this definition.", srcOptID)}
		}
		destOpt, err := GetCustomFieldOptionByKeyAndDefinition(s, destDef.ID, srcOpt.MachineKey)
		if err != nil {
			if IsErrCustomFieldOptionDoesNotExist(err) {
				return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The destination definition %q has no option with machine key %q.", destDef.MachineKey, srcOpt.MachineKey)}
			}
			return err
		}
		optionRemap[srcOptID] = destOpt.ID
	}
	return nil
}

// rewriteTaskCustomFieldsForMove rewrites the value rows of a moved task onto
// the destination definitions and options, remapping single-select option ids
// and multi-select memberships and rescaling number values to the destination
// precision. It must run after a successful preflight, inside the same
// transaction; the task row lock held by the move guarantees no value can be
// written between the two.
func rewriteTaskCustomFieldsForMove(s *xorm.Session, taskID int64, defRemap, optionRemap map[int64]int64) error {
	rows := []*TaskCustomFieldValue{}
	if err := s.Where("task_id = ?", taskID).Find(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	// Load the source definitions to detect multi-select rows and to know the
	// source number precision for rescaling.
	defIDs := make([]int64, 0, len(rows))
	seen := map[int64]struct{}{}
	for _, row := range rows {
		if _, ok := seen[row.DefinitionID]; ok {
			continue
		}
		seen[row.DefinitionID] = struct{}{}
		defIDs = append(defIDs, row.DefinitionID)
	}
	defs, err := getCustomFieldDefinitionsByIDs(s, defIDs)
	if err != nil {
		return err
	}
	srcDefByID := make(map[int64]*CustomFieldDefinition, len(defs))
	for _, def := range defs {
		srcDefByID[def.ID] = def
	}

	// Load every multi-select value's memberships in one batched pair of
	// queries instead of one query per value.
	multiSelectValueIDs := make([]int64, 0, len(rows))
	valueDefinitions := make(map[int64]int64, len(rows))
	for _, row := range rows {
		def, ok := srcDefByID[row.DefinitionID]
		if !ok {
			return missingCustomFieldDefinitionError(row)
		}
		if def.FieldType == CustomFieldTypeMultiSelect {
			multiSelectValueIDs = append(multiSelectValueIDs, row.ID)
			valueDefinitions[row.ID] = row.DefinitionID
		}
	}
	optionIDsByValue, err := getOptionIDsForValues(s, multiSelectValueIDs, valueDefinitions)
	if err != nil {
		return err
	}

	for _, row := range rows {
		srcDef, ok := srcDefByID[row.DefinitionID]
		if !ok {
			return missingCustomFieldDefinitionError(row)
		}
		newDefID, ok := defRemap[row.DefinitionID]
		if !ok {
			return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("No destination definition for source definition %d.", row.DefinitionID)}
		}

		// Rescale number values to the destination precision: the preflight
		// guaranteed the value is representable, so the scaled integer changes
		// (12.3 @ p1 = 123 becomes 1230 @ p2), never the value itself.
		cols := []string{"definition_id", "updated"}
		update := &TaskCustomFieldValue{DefinitionID: newDefID}
		if srcDef.FieldType == CustomFieldTypeNumber && row.ValueNumber != nil {
			value, err := row.FromRow(s, srcDef)
			if err != nil {
				return err
			}
			destDef, err := GetCustomFieldDefinitionByID(s, newDefID)
			if err != nil {
				return err
			}
			if err := validateNumberValue(value, destDef); err != nil {
				return err
			}
			scaled := value.Number.Value
			update.ValueNumber = &scaled
			cols = append(cols, "value_number")
		}
		if srcDef.FieldType == CustomFieldTypeSingleSelect && row.ValueSingleOptionID != nil {
			mapped, ok := optionRemap[*row.ValueSingleOptionID]
			if !ok {
				return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("No destination option for source option %d.", *row.ValueSingleOptionID)}
			}
			update.ValueSingleOptionID = &mapped
			cols = append(cols, "value_single_option_id")
		}
		if _, err := s.ID(row.ID).Cols(cols...).Update(update); err != nil {
			return err
		}

		if srcDef.FieldType == CustomFieldTypeMultiSelect {
			optionIDs := optionIDsByValue[row.ID]
			remapped := make([]int64, 0, len(optionIDs))
			for _, optID := range optionIDs {
				mapped, ok := optionRemap[optID]
				if !ok {
					return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("No destination option for source option %d.", optID)}
				}
				remapped = append(remapped, mapped)
			}
			if err := replaceValueOptions(s, row.ID, remapped); err != nil {
				return err
			}
		}
	}
	return nil
}
