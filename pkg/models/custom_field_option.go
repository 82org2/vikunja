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
	"unicode/utf8"

	"code.vikunja.io/api/pkg/utils"
	"code.vikunja.io/api/pkg/web"
	"xorm.io/xorm"
)

// CustomFieldOption is one selectable option of a single_select or multi_select
// definition. Options have stable numeric IDs for the API and immutable
// project-local machine keys so label changes, reordering, project duplication,
// and compatible task moves never change their meaning.
type CustomFieldOption struct {
	ID int64 `xorm:"bigint autoincr not null unique pk" json:"id" param:"option" readOnly:"true" doc:"The unique, numeric id of this option."`

	DefinitionID int64 `xorm:"bigint not null unique(definition_machine_key) index(definition_is_archived_position)" json:"definition_id" doc:"The definition this option belongs to. Only select definitions have options."`

	MachineKey string `xorm:"varchar(64) not null unique(definition_machine_key)" json:"machine_key" doc:"The immutable, definition-local machine key, using the same syntax as definition keys. It is the stable identity for duplication and moves; it never changes."`

	Label      string  `xorm:"varchar(250) not null" json:"label" valid:"runelength(1|250)" minLength:"1" maxLength:"250" doc:"The display label of this option."`
	HexColor   string  `xorm:"varchar(6) null" json:"hex_color" doc:"The color of this option in hex format, without the leading #."`
	IsArchived bool    `xorm:"not null default false index(definition_is_archived_position)" json:"is_archived" doc:"Whether this option is archived. Archived options can no longer be selected but continue to describe retained values."`
	Position   float64 `xorm:"double null index(definition_is_archived_position)" json:"position" doc:"The position of this option, controlling display and multi-select response order."`

	Created time.Time `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this option was created. You cannot change this value."`
	Updated time.Time `xorm:"updated not null" json:"updated" readOnly:"true" doc:"A timestamp when this option was last updated. You cannot change this value."`
}

// TableName makes a pretty table name
func (*CustomFieldOption) TableName() string {
	return "custom_field_options"
}

func getCustomFieldOptionByID(s *xorm.Session, id int64) (*CustomFieldOption, error) {
	opt := &CustomFieldOption{}
	exists, err := s.Where("id = ?", id).Get(opt)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomFieldOptionDoesNotExist{OptionID: id}
	}
	return opt, nil
}

func (opt *CustomFieldOption) validate() error {
	if err := validateCustomFieldKey(opt.MachineKey); err != nil {
		return err
	}
	if labelLen := utf8.RuneCountInString(opt.Label); labelLen < 1 || labelLen > 250 {
		return ErrInvalidCustomFieldOption{Message: "The label must be between 1 and 250 characters."}
	}
	opt.HexColor = utils.NormalizeHex(opt.HexColor)
	return nil
}

// Create adds an option to a definition, enforcing the per-definition limit
// and backfilling the position. Options are only meaningful for select types;
// that check lives in the write path so an option can be created before its
// definition's type is fully wired in tests.
func (opt *CustomFieldOption) Create(s *xorm.Session, _ web.Auth) (err error) {
	if err = opt.validate(); err != nil {
		return err
	}

	def, err := getCustomFieldDefinitionByID(s, opt.DefinitionID)
	if err != nil {
		return err
	}
	if def.FieldType != CustomFieldTypeSingleSelect && def.FieldType != CustomFieldTypeMultiSelect {
		return ErrInvalidCustomFieldOption{Message: "Only single-select and multi-select definitions may have options."}
	}
	if err = updateProjectLastUpdated(s, &Project{ID: def.ProjectID}); err != nil {
		return err
	}

	count, err := s.Where("definition_id = ?", opt.DefinitionID).Count(&CustomFieldOption{})
	if err != nil {
		return err
	}
	if count >= 200 {
		return ErrCustomFieldOptionLimitReached{}
	}

	opt.ID = 0
	if _, err = s.Insert(opt); err != nil {
		return err
	}

	opt.Position = calculateDefaultPosition(opt.ID, opt.Position)
	if _, err = s.Where("id = ?", opt.ID).Update(opt); err != nil {
		return err
	}

	return nil
}

// Update edits the editable fields of an option. The machine key is immutable.
func (opt *CustomFieldOption) Update(s *xorm.Session, _ web.Auth) (err error) {
	existing, err := getCustomFieldOptionByID(s, opt.ID)
	if err != nil {
		return err
	}

	if existing.MachineKey != opt.MachineKey {
		return ErrInvalidCustomFieldOption{Message: "The machine key of an option is immutable."}
	}
	if existing.DefinitionID != opt.DefinitionID {
		return ErrInvalidCustomFieldOption{Message: "An option cannot be moved to another definition."}
	}

	if err = opt.validate(); err != nil {
		return err
	}

	_, err = s.
		Where("id = ?", opt.ID).
		Cols("label", "hex_color", "position", "is_archived", "updated").
		Update(opt)
	if err != nil {
		return err
	}

	def, err := getCustomFieldDefinitionByID(s, opt.DefinitionID)
	if err != nil {
		return err
	}
	return updateProjectLastUpdated(s, &Project{ID: def.ProjectID})
}
