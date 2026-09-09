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

// CustomFieldValueExport is the export/import wire shape of one value (a task
// value or a definition default). The typed value is the regular
// CustomFieldValue; select options are additionally referenced by machine key
// and user values by email/username, so a round-trip never trusts foreign
// numeric ids.
type CustomFieldValueExport struct {
	Value           *CustomFieldValue `json:"value"`
	SingleOptionKey *string           `json:"single_option_key,omitempty"`
	OptionKeys      []string          `json:"option_keys,omitempty"`
	UserEmail       *string           `json:"user_email,omitempty"`
	UserUsername    *string           `json:"user_username,omitempty"`
}

// CustomFieldDefinitionExport is the export/import wire shape of a definition.
// The numeric id is informational only; the machine key is authoritative.
type CustomFieldDefinitionExport struct {
	ID            int64                     `json:"id"`
	MachineKey    string                    `json:"machine_key"`
	FieldType     CustomFieldType           `json:"field_type"`
	Title         string                    `json:"title"`
	Description   string                    `json:"description"`
	Configuration *CustomFieldConfiguration `json:"configuration,omitempty"`
	DefaultValue  *CustomFieldValueExport   `json:"default_value,omitempty"`
	IsArchived    bool                      `json:"is_archived"`
	Position      float64                   `json:"position"`
	ShowOnCard    bool                      `json:"show_on_card"`
	ShowInTable   bool                      `json:"show_in_table"`
}

// CustomFieldOptionExport is the export/import wire shape of an option. The
// parent definition is resolved by DefinitionMachineKey, never by the
// informational numeric DefinitionID.
type CustomFieldOptionExport struct {
	ID                   int64   `json:"id"`
	DefinitionMachineKey string  `json:"definition_machine_key"`
	DefinitionID         int64   `json:"definition_id"`
	MachineKey           string  `json:"machine_key"`
	Label                string  `json:"label"`
	HexColor             string  `json:"hex_color"`
	IsArchived           bool    `json:"is_archived"`
	Position             float64 `json:"position"`
}

// CustomFieldTaskValueExport is the export/import wire shape of one task value.
// The definition is resolved by MachineKey, never by the informational
// numeric DefinitionID.
type CustomFieldTaskValueExport struct {
	DefinitionID int64                   `json:"definition_id"`
	MachineKey   string                  `json:"machine_key"`
	Value        *CustomFieldValueExport `json:"value"`
}

// --- export ---

// addCustomFieldsToExport loads the definitions, options, and task values of
// the given projects and attaches them to the export DTOs. All loads are
// bounded and batched.
func addCustomFieldsToExport(s *xorm.Session, projects []*ProjectWithTasksAndBuckets, projectIDs, taskIDs []int64) error {
	if len(projectIDs) == 0 {
		return nil
	}

	defs, err := getCustomFieldDefinitionsByProjectIDs(s, projectIDs)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		return nil
	}

	defIDs := make([]int64, 0, len(defs))
	for _, def := range defs {
		defIDs = append(defIDs, def.ID)
	}

	options := []*CustomFieldOption{}
	for chunk := range slices.Chunk(defIDs, 500) {
		batch := []*CustomFieldOption{}
		if err := s.In("definition_id", chunk).Find(&batch); err != nil {
			return err
		}
		options = append(options, batch...)
	}
	optionByID := make(map[int64]*CustomFieldOption, len(options))
	optionsByDef := make(map[int64][]*CustomFieldOption, len(defs))
	for _, opt := range options {
		optionByID[opt.ID] = opt
		optionsByDef[opt.DefinitionID] = append(optionsByDef[opt.DefinitionID], opt)
	}

	rows := []*TaskCustomFieldValue{}
	for chunk := range slices.Chunk(taskIDs, 500) {
		batch := []*TaskCustomFieldValue{}
		if err := s.In("task_id", chunk).Find(&batch); err != nil {
			return err
		}
		rows = append(rows, batch...)
	}

	defByID := make(map[int64]*CustomFieldDefinition, len(defs))
	for _, def := range defs {
		defByID[def.ID] = def
	}

	// Load every multi-select value's memberships in one batched pair of
	// queries instead of one query per value.
	multiSelectValueIDs := make([]int64, 0, len(rows))
	valueDefinitions := make(map[int64]int64, len(rows))
	for _, row := range rows {
		def, ok := defByID[row.DefinitionID]
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

	// Load every referenced user in bounded batches so user-type values and
	// defaults carry a stable username without one lookup per value or an
	// oversized IN list. GetUsersByIDs obfuscates emails, so the export never
	// leaks them.
	userByID := make(map[int64]*user.User)
	for chunk := range slices.Chunk(collectCustomFieldUserIDs(defs, rows, defByID), 500) {
		batch, err := user.GetUsersByIDs(s, chunk)
		if err != nil {
			return err
		}
		for id, u := range batch {
			userByID[id] = u
		}
	}

	// Convert definitions and options per project.
	defsByProject := make(map[int64][]*CustomFieldDefinitionExport, len(projects))
	optionsByProject := make(map[int64][]*CustomFieldOptionExport, len(projects))
	for _, def := range defs {
		exp, err := def.toExport(optionByID, userByID)
		if err != nil {
			return err
		}
		defsByProject[def.ProjectID] = append(defsByProject[def.ProjectID], exp)
		for _, opt := range optionsByDef[def.ID] {
			optionsByProject[def.ProjectID] = append(optionsByProject[def.ProjectID], &CustomFieldOptionExport{
				ID:                   opt.ID,
				DefinitionMachineKey: def.MachineKey,
				DefinitionID:         opt.DefinitionID,
				MachineKey:           opt.MachineKey,
				Label:                opt.Label,
				HexColor:             opt.HexColor,
				IsArchived:           opt.IsArchived,
				Position:             opt.Position,
			})
		}
	}

	// Convert value rows per task.
	valuesByTask := make(map[int64][]*CustomFieldTaskValueExport, len(rows))
	for _, row := range rows {
		def, ok := defByID[row.DefinitionID]
		if !ok {
			return missingCustomFieldDefinitionError(row)
		}
		var value *CustomFieldValue
		if def.FieldType == CustomFieldTypeMultiSelect {
			optionIDs := optionIDsByValue[row.ID]
			if len(optionIDs) == 0 {
				return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The multi-select value row %d has no memberships; an empty selection must be unset.", row.ID)}
			}
			value = &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: optionIDs}
		} else {
			value, err = row.FromRow(s, def)
			if err != nil {
				return err
			}
		}
		valueExp, err := valueToExport(value, optionByID, userByID)
		if err != nil {
			return err
		}
		valuesByTask[row.TaskID] = append(valuesByTask[row.TaskID], &CustomFieldTaskValueExport{
			DefinitionID: def.ID,
			MachineKey:   def.MachineKey,
			Value:        valueExp,
		})
	}

	// Attach to the projects and their tasks.
	for _, p := range projects {
		p.CustomFieldDefinitions = defsByProject[p.ID]
		p.CustomFieldOptions = optionsByProject[p.ID]
		for _, t := range p.Tasks {
			t.CustomFieldValues = valuesByTask[t.ID]
		}
	}
	return nil
}

func (d *CustomFieldDefinition) toExport(optionByID map[int64]*CustomFieldOption, userByID map[int64]*user.User) (*CustomFieldDefinitionExport, error) {
	exp := &CustomFieldDefinitionExport{
		ID:            d.ID,
		MachineKey:    d.MachineKey,
		FieldType:     d.FieldType,
		Title:         d.Title,
		Description:   d.Description,
		Configuration: d.Configuration,
		IsArchived:    d.IsArchived,
		Position:      d.Position,
		ShowOnCard:    d.ShowOnCard,
		ShowInTable:   d.ShowInTable,
	}
	if d.DefaultValue != nil {
		valueExp, err := valueToExport(d.DefaultValue, optionByID, userByID)
		if err != nil {
			return nil, err
		}
		exp.DefaultValue = valueExp
	}
	return exp, nil
}

// collectCustomFieldUserIDs gathers the distinct user ids referenced by
// user-type values and defaults, so the export can load them in one query.
func collectCustomFieldUserIDs(defs []*CustomFieldDefinition, rows []*TaskCustomFieldValue, defByID map[int64]*CustomFieldDefinition) []int64 {
	userIDs := make([]int64, 0)
	seen := map[int64]struct{}{}
	collect := func(id *int64) {
		if id == nil {
			return
		}
		if _, ok := seen[*id]; ok {
			return
		}
		seen[*id] = struct{}{}
		userIDs = append(userIDs, *id)
	}
	for _, def := range defs {
		if def.FieldType == CustomFieldTypeUser && def.DefaultValue != nil {
			collect(def.DefaultValue.UserID)
		}
	}
	for _, row := range rows {
		def := defByID[row.DefinitionID]
		if def.FieldType == CustomFieldTypeUser {
			collect(row.ValueUserID)
		}
	}
	return userIDs
}

// valueToExport decorates a typed value with the machine keys of its selected
// options and the stable username of a referenced user. Emails are never
// exported: GetUsersByIDs obfuscates them, and the import resolves by username.
func valueToExport(v *CustomFieldValue, optionByID map[int64]*CustomFieldOption, userByID map[int64]*user.User) (*CustomFieldValueExport, error) {
	exp := &CustomFieldValueExport{Value: v}
	switch v.Type {
	case CustomFieldTypeSingleSelect:
		if v.SingleOptionID != nil {
			opt, ok := optionByID[*v.SingleOptionID]
			if !ok {
				return nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The option %d does not exist.", *v.SingleOptionID)}
			}
			exp.SingleOptionKey = &opt.MachineKey
		}
	case CustomFieldTypeMultiSelect:
		for _, optID := range v.OptionIDs {
			opt, ok := optionByID[optID]
			if !ok {
				return nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The option %d does not exist.", optID)}
			}
			exp.OptionKeys = append(exp.OptionKeys, opt.MachineKey)
		}
	case CustomFieldTypeUser:
		if v.UserID != nil {
			if u, ok := userByID[*v.UserID]; ok {
				exp.UserUsername = &u.Username
			}
		}
	case CustomFieldTypeShortText, CustomFieldTypeLongText, CustomFieldTypeNumber,
		CustomFieldTypeBoolean, CustomFieldTypeDate, CustomFieldTypeDateTime,
		CustomFieldTypeURL:
		// Scalar and date types carry no extra identity.
	}
	return exp, nil
}

// --- import ---

// ImportCustomFieldsForProject resolves or creates the definitions and options
// of an imported project and remaps the exported task values onto the newly
// created tasks. Definitions and options are matched by machine key; a present
// definition with a different type is rejected. Foreign numeric ids are never
// trusted. The caller runs inside a transaction, so a rejection leaves no
// partial definitions behind.
func ImportCustomFieldsForProject(s *xorm.Session, projectID int64, defs []*CustomFieldDefinitionExport, options []*CustomFieldOptionExport, taskValues map[int64][]*CustomFieldTaskValueExport) error {
	// Resolve or create definitions by machine key.
	defByKey := make(map[string]*CustomFieldDefinition, len(defs))
	for _, exp := range defs {
		existing := &CustomFieldDefinition{}
		exists, err := s.Where("project_id = ? AND machine_key = ?", projectID, exp.MachineKey).Get(existing)
		if err != nil {
			return err
		}
		if exists {
			if existing.FieldType != exp.FieldType {
				return ErrInvalidCustomFieldDefinition{Message: fmt.Sprintf("A definition with machine key %q already exists in this project with a different type.", exp.MachineKey)}
			}
			defByKey[exp.MachineKey] = existing
			continue
		}
		def := &CustomFieldDefinition{
			ProjectID:     projectID,
			MachineKey:    exp.MachineKey,
			Title:         exp.Title,
			Description:   exp.Description,
			FieldType:     exp.FieldType,
			IsArchived:    exp.IsArchived,
			Position:      exp.Position,
			ShowOnCard:    exp.ShowOnCard,
			ShowInTable:   exp.ShowInTable,
			Configuration: exp.Configuration,
		}
		if err := def.Create(s, nil); err != nil {
			return err
		}
		defByKey[exp.MachineKey] = def
	}

	// Resolve or create options by machine key within their definition.
	optionByKey := make(map[string]map[string]*CustomFieldOption, len(defs))
	for _, exp := range options {
		def, ok := defByKey[exp.DefinitionMachineKey]
		if !ok {
			return ErrInvalidCustomFieldOption{Message: fmt.Sprintf("The option %q references an unknown definition %q.", exp.MachineKey, exp.DefinitionMachineKey)}
		}
		existing := &CustomFieldOption{}
		exists, err := s.Where("definition_id = ? AND machine_key = ?", def.ID, exp.MachineKey).Get(existing)
		if err != nil {
			return err
		}
		if exists {
			if optionByKey[exp.DefinitionMachineKey] == nil {
				optionByKey[exp.DefinitionMachineKey] = map[string]*CustomFieldOption{}
			}
			optionByKey[exp.DefinitionMachineKey][exp.MachineKey] = existing
			continue
		}
		opt := &CustomFieldOption{
			DefinitionID: def.ID,
			MachineKey:   exp.MachineKey,
			Label:        exp.Label,
			HexColor:     exp.HexColor,
			IsArchived:   exp.IsArchived,
			Position:     exp.Position,
		}
		if err := opt.Create(s, nil); err != nil {
			return err
		}
		if optionByKey[exp.DefinitionMachineKey] == nil {
			optionByKey[exp.DefinitionMachineKey] = map[string]*CustomFieldOption{}
		}
		optionByKey[exp.DefinitionMachineKey][exp.MachineKey] = opt
	}

	if err := importCustomFieldDefaults(s, defs, defByKey, optionByKey); err != nil {
		return err
	}
	return importCustomFieldTaskValues(s, taskValues, defByKey, optionByKey)
}

// importCustomFieldDefaults sets each definition's default value with option
// ids resolved by key. User defaults must resolve by email/username, never the
// foreign id.
func importCustomFieldDefaults(s *xorm.Session, defs []*CustomFieldDefinitionExport, defByKey map[string]*CustomFieldDefinition, optionByKey map[string]map[string]*CustomFieldOption) error {
	for _, exp := range defs {
		if exp.DefaultValue == nil {
			continue
		}
		def := defByKey[exp.MachineKey]
		value, err := exportValueToValue(exp.DefaultValue, def, optionByKey[exp.MachineKey])
		if err != nil {
			return err
		}
		if value.Type == CustomFieldTypeUser {
			resolved, err := resolveUserValue(s, exp.DefaultValue, def)
			if err != nil {
				return err
			}
			if resolved == nil {
				return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The default value for definition %q references a user that could not be resolved in this installation.", def.MachineKey)}
			}
			value = resolved
		}
		if err := validateValueAgainstDefinitionAllowArchivedOptions(s, value, def); err != nil {
			return err
		}
		if _, err := s.ID(def.ID).Cols("default_value").Update(&CustomFieldDefinition{DefaultValue: value}); err != nil {
			return err
		}
	}
	return nil
}

// importCustomFieldTaskValues inserts the exported task values. An unresolvable
// user value rejects the import rather than being silently dropped: the wiki
// requires values to be preserved without silent loss, and a user value that
// cannot be resolved here must not bind to an unrelated user.
func importCustomFieldTaskValues(s *xorm.Session, taskValues map[int64][]*CustomFieldTaskValueExport, defByKey map[string]*CustomFieldDefinition, optionByKey map[string]map[string]*CustomFieldOption) error {
	for taskID, values := range taskValues {
		for _, exp := range values {
			def, ok := defByKey[exp.MachineKey]
			if !ok {
				return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The value references an unknown definition %q.", exp.MachineKey)}
			}
			value, err := exportValueToValue(exp.Value, def, optionByKey[exp.MachineKey])
			if err != nil {
				return err
			}
			if value.Type == CustomFieldTypeUser {
				resolved, err := resolveUserValue(s, exp.Value, def)
				if err != nil {
					return err
				}
				if resolved == nil {
					return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The user value for definition %q references a user that could not be resolved in this installation.", def.MachineKey)}
				}
				value = resolved
			}
			if err := validateValueAgainstDefinitionAllowArchivedOptions(s, value, def); err != nil {
				return err
			}
			row, err := value.toRow(taskID, def.ID)
			if err != nil {
				return err
			}
			if _, err := s.Insert(row); err != nil {
				return err
			}
			if def.FieldType == CustomFieldTypeMultiSelect {
				if err := replaceValueOptions(s, row.ID, value.OptionIDs); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// exportValueToValue rebuilds a typed value from its export form, replacing
// foreign select option ids with ids resolved by machine key. User values are
// left for resolveUserValue, which never trusts the foreign numeric id.
func exportValueToValue(exp *CustomFieldValueExport, def *CustomFieldDefinition, optionsByKey map[string]*CustomFieldOption) (*CustomFieldValue, error) {
	if exp == nil || exp.Value == nil {
		return nil, ErrInvalidCustomFieldValue{Message: "A custom field value is required."}
	}
	value := *exp.Value
	switch value.Type {
	case CustomFieldTypeSingleSelect:
		if exp.SingleOptionKey == nil {
			return nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The single-select value for %q is missing its option key.", def.MachineKey)}
		}
		opt, ok := optionsByKey[*exp.SingleOptionKey]
		if !ok {
			return nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The option %q does not exist in definition %q.", *exp.SingleOptionKey, def.MachineKey)}
		}
		value.SingleOptionID = &opt.ID
	case CustomFieldTypeMultiSelect:
		ids := make([]int64, 0, len(exp.OptionKeys))
		for _, key := range exp.OptionKeys {
			opt, ok := optionsByKey[key]
			if !ok {
				return nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The option %q does not exist in definition %q.", key, def.MachineKey)}
			}
			ids = append(ids, opt.ID)
		}
		value.OptionIDs = ids
	case CustomFieldTypeShortText, CustomFieldTypeLongText, CustomFieldTypeNumber,
		CustomFieldTypeBoolean, CustomFieldTypeDate, CustomFieldTypeDateTime,
		CustomFieldTypeURL, CustomFieldTypeUser:
		// Scalar, date, and user types carry no option references.
	}
	return &value, nil
}

// resolveUserValue resolves a user-type value by email or username and checks
// visibility in the target project. It returns nil when the user cannot be
// resolved or is not visible, so the caller surfaces a warning and skips the
// value instead of trusting the foreign numeric id.
func resolveUserValue(s *xorm.Session, exp *CustomFieldValueExport, def *CustomFieldDefinition) (*CustomFieldValue, error) {
	if exp.UserEmail == nil && exp.UserUsername == nil {
		return nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The user value for %q has no email or username.", def.MachineKey)}
	}
	var u *user.User
	var err error
	if exp.UserEmail != nil {
		u, err = user.GetUserByEmail(s, *exp.UserEmail)
	} else {
		u, err = user.GetUserByUsername(s, *exp.UserUsername)
	}
	if err != nil {
		if user.IsErrUserDoesNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	project, _, err := getProjectSimple(s, builder.Eq{"id": def.ProjectID})
	if err != nil {
		return nil, err
	}
	can, _, err := project.CanRead(s, u)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, nil
	}
	uid := u.ID
	return &CustomFieldValue{Type: CustomFieldTypeUser, UserID: &uid}, nil
}
