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
	"sort"
	"time"

	"xorm.io/xorm"
)

// CustomFieldValueOption is one selected option of a multi-select value. It is
// only used for multi-select; single-select stores its option on the value row.
type CustomFieldValueOption struct {
	ID int64 `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this membership row."`

	ValueID int64 `xorm:"bigint not null index unique(value_option)" json:"value_id" doc:"The value row this option belongs to."`

	OptionID int64 `xorm:"bigint not null index unique(value_option)" json:"option_id" doc:"The selected option."`

	Created time.Time `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this membership was created. You cannot change this value."`
}

// TableName makes a pretty table name
func (*CustomFieldValueOption) TableName() string {
	return "custom_field_value_options"
}

// --- reads ---

func getOptionIDsForValue(s *xorm.Session, valueID int64) ([]int64, error) {
	rows := []*CustomFieldValueOption{}
	err := s.Where("value_id = ?", valueID).Find(&rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []int64{}, nil
	}

	optionIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		optionIDs = append(optionIDs, row.OptionID)
	}

	// Response order follows option position, not insertion order, per the
	// CustomFieldOption.Position contract; id breaks ties deterministically.
	options := []*CustomFieldOption{}
	if err = s.In("id", optionIDs).OrderBy("position asc, id asc").Find(&options); err != nil {
		return nil, err
	}

	ordered := make([]int64, 0, len(options))
	for _, opt := range options {
		ordered = append(ordered, opt.ID)
	}
	return ordered, nil
}

// getOptionIDsForValues is the batched form of getOptionIDsForValue: it returns
// every value's option ids ordered by option position (id as tie-break) in two
// chunked queries total, regardless of how many values are requested, so task
// reads never issue a query per multi-select value.
//
// valueDefinitions maps each value row to its definition id. Every membership
// must resolve to an existing option of that definition; anything else is
// corrupt stored data and fails loudly. Archived options remain valid.
func getOptionIDsForValues(s *xorm.Session, valueIDs []int64, valueDefinitions map[int64]int64) (map[int64][]int64, error) {
	ordered := make(map[int64][]int64, len(valueIDs))
	for _, id := range valueIDs {
		ordered[id] = []int64{}
	}
	if len(valueIDs) == 0 {
		return ordered, nil
	}

	memberships := []*CustomFieldValueOption{}
	const batchSize = 500
	for chunk := range slices.Chunk(valueIDs, batchSize) {
		batch := []*CustomFieldValueOption{}
		if err := s.In("value_id", chunk).Find(&batch); err != nil {
			return nil, err
		}
		memberships = append(memberships, batch...)
	}
	if len(memberships) == 0 {
		return ordered, nil
	}

	optionIDs := make([]int64, 0, len(memberships))
	seen := map[int64]struct{}{}
	for _, m := range memberships {
		if _, ok := seen[m.OptionID]; ok {
			continue
		}
		seen[m.OptionID] = struct{}{}
		optionIDs = append(optionIDs, m.OptionID)
	}

	options := []*CustomFieldOption{}
	for chunk := range slices.Chunk(optionIDs, batchSize) {
		batch := []*CustomFieldOption{}
		if err := s.In("id", chunk).Find(&batch); err != nil {
			return nil, err
		}
		options = append(options, batch...)
	}
	optionByID := make(map[int64]*CustomFieldOption, len(options))
	for _, opt := range options {
		optionByID[opt.ID] = opt
	}

	for _, m := range memberships {
		opt, ok := optionByID[m.OptionID]
		if !ok {
			return nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The multi-select membership %d references an option that does not exist.", m.ID)}
		}
		if opt.DefinitionID != valueDefinitions[m.ValueID] {
			return nil, ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The multi-select membership %d references an option from a different definition.", m.ID)}
		}
		ordered[m.ValueID] = append(ordered[m.ValueID], m.OptionID)
	}
	for _, ids := range ordered {
		sort.SliceStable(ids, func(i, j int) bool {
			if optionByID[ids[i]].Position != optionByID[ids[j]].Position {
				return optionByID[ids[i]].Position < optionByID[ids[j]].Position
			}
			return ids[i] < ids[j]
		})
	}
	return ordered, nil
}

// --- write path ---

// replaceValueOptions rewrites the memberships of a value in one transaction,
// deleting old selections before inserting the new ones. Empty optionIDs leaves
// no memberships behind; UnsetCustomFieldValue deletes the value row itself.
func replaceValueOptions(s *xorm.Session, valueID int64, optionIDs []int64) error {
	if _, err := s.Where("value_id = ?", valueID).Delete(&CustomFieldValueOption{}); err != nil {
		return err
	}

	for _, optionID := range optionIDs {
		if _, err := s.Insert(&CustomFieldValueOption{ValueID: valueID, OptionID: optionID}); err != nil {
			return err
		}
	}
	return nil
}
