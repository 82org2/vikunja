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

package migration

import (
	"time"

	"src.techknowlogick.com/xormigrate"
	"xorm.io/xorm"
)

// The four mirrors repeat the index and unique tags of the models in
// pkg/models/ exactly, so Sync2 on a fresh install and this migration create
// identical table structures — including the composite (project_id,
// machine_key) and (task_id, definition_id) unique keys on sqlite and mysql,
// where a later hand-written index would diverge.

type customFieldDefinition20260908204428 struct {
	ID            int64     `xorm:"bigint autoincr not null unique pk"`
	ProjectID     int64     `xorm:"bigint not null unique(project_machine_key) index(project_is_archived_position)"`
	MachineKey    string    `xorm:"varchar(64) not null unique(project_machine_key)"`
	Title         string    `xorm:"varchar(250) not null"`
	Description   string    `xorm:"text null"`
	FieldType     string    `xorm:"varchar(32) not null"`
	IsArchived    bool      `xorm:"not null default false index(project_is_archived_position)"`
	Position      float64   `xorm:"double null index(project_is_archived_position)"`
	ShowOnCard    bool      `xorm:"not null default false"`
	ShowInTable   bool      `xorm:"not null default false"`
	Configuration string    `xorm:"json null default null"`
	DefaultValue  string    `xorm:"json null default null"`
	Created       time.Time `xorm:"created not null"`
	Updated       time.Time `xorm:"updated not null"`
}

func (customFieldDefinition20260908204428) TableName() string {
	return "custom_field_definitions"
}

type customFieldOption20260908204428 struct {
	ID           int64     `xorm:"bigint autoincr not null unique pk"`
	DefinitionID int64     `xorm:"bigint not null unique(definition_machine_key) index(definition_is_archived_position)"`
	MachineKey   string    `xorm:"varchar(64) not null unique(definition_machine_key)"`
	Label        string    `xorm:"varchar(250) not null"`
	HexColor     string    `xorm:"varchar(6) null"`
	IsArchived   bool      `xorm:"not null default false index(definition_is_archived_position)"`
	Position     float64   `xorm:"double null index(definition_is_archived_position)"`
	Created      time.Time `xorm:"created not null"`
	Updated      time.Time `xorm:"updated not null"`
}

func (customFieldOption20260908204428) TableName() string {
	return "custom_field_options"
}

type taskCustomFieldValue20260908204428 struct {
	ID                  int64      `xorm:"bigint autoincr not null unique pk"`
	TaskID              int64      `xorm:"bigint not null index unique(task_definition)"`
	DefinitionID        int64      `xorm:"bigint not null index unique(task_definition) index(definition_short_text) index(definition_number) index(definition_boolean) index(definition_date) index(definition_datetime) index(definition_user) index(definition_single_option)"`
	ValueShortText      *string    `xorm:"value_short_text varchar(255) null index(definition_short_text)"`
	ValueLongText       *string    `xorm:"value_long_text longtext null"`
	ValueNumber         *int64     `xorm:"value_number bigint null index(definition_number)"`
	ValueBoolean        *bool      `xorm:"value_boolean boolean null index(definition_boolean)"`
	ValueDate           *int64     `xorm:"value_date bigint null index(definition_date)"`
	ValueDateTime       *time.Time `xorm:"value_datetime datetime null index(definition_datetime)"`
	ValueURL            *string    `xorm:"value_url text null"`
	ValueUserID         *int64     `xorm:"value_user_id bigint null index(definition_user)"`
	ValueSingleOptionID *int64     `xorm:"value_single_option_id bigint null index(definition_single_option)"`
	Created             time.Time  `xorm:"created not null"`
	Updated             time.Time  `xorm:"updated not null"`
}

func (taskCustomFieldValue20260908204428) TableName() string {
	return "custom_field_values"
}

type customFieldValueOption20260908204428 struct {
	ID       int64     `xorm:"bigint autoincr not null unique pk"`
	ValueID  int64     `xorm:"bigint not null index unique(value_option)"`
	OptionID int64     `xorm:"bigint not null index unique(value_option)"`
	Created  time.Time `xorm:"created not null"`
}

func (customFieldValueOption20260908204428) TableName() string {
	return "custom_field_value_options"
}

// addCustomFields20260908204428 creates the four brand-new custom field tables.
func addCustomFields20260908204428(tx *xorm.Engine) error {
	if err := tx.Sync2(customFieldDefinition20260908204428{}); err != nil { //nolint:forbidigo // brand-new table, nothing to drop
		return err
	}
	if err := tx.Sync2(customFieldOption20260908204428{}); err != nil { //nolint:forbidigo // brand-new table, nothing to drop
		return err
	}
	if err := tx.Sync2(taskCustomFieldValue20260908204428{}); err != nil { //nolint:forbidigo // brand-new table, nothing to drop
		return err
	}
	if err := tx.Sync2(customFieldValueOption20260908204428{}); err != nil { //nolint:forbidigo // brand-new table, nothing to drop
		return err
	}
	return nil
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20260908204428",
		Description: "Add the custom field tables (definitions, options, value rows, multi-select memberships)",
		Migrate:     addCustomFields20260908204428,
		Rollback: func(tx *xorm.Engine) error {
			if err := tx.DropTables(customFieldDefinition20260908204428{}); err != nil {
				return err
			}
			if err := tx.DropTables(customFieldOption20260908204428{}); err != nil {
				return err
			}
			if err := tx.DropTables(taskCustomFieldValue20260908204428{}); err != nil {
				return err
			}
			return tx.DropTables(customFieldValueOption20260908204428{})
		},
	})
}
