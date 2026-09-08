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
	err := s.Where("value_id = ?", valueID).OrderBy("id ASC").Find(&rows)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.OptionID)
	}
	return ids, nil
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
