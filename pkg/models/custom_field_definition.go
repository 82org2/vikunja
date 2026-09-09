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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"
	"unicode/utf8"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"
	"xorm.io/builder"
	"xorm.io/xorm"
	"xorm.io/xorm/schemas"
)

// CustomFieldDefinition describes one project-scoped custom field: its
// immutable machine key and type, the validated per-type configuration, and an
// optional default value materialised when a new task is created.
type CustomFieldDefinition struct {
	ID int64 `xorm:"bigint autoincr not null unique pk" json:"id" param:"definition" readOnly:"true" doc:"The unique, numeric id of this definition."`

	ProjectID int64 `xorm:"bigint not null unique(project_machine_key) index(project_is_archived_position)" json:"project_id" doc:"The project this definition belongs to."`

	MachineKey string `xorm:"varchar(64) not null unique(project_machine_key)" json:"machine_key" doc:"The immutable, project-local machine key. This is the stable identity used by the API, filters, exports, duplication, and moves; it never changes."`

	Title       string          `xorm:"varchar(250) not null" json:"title" valid:"runelength(1|250)" minLength:"1" maxLength:"250" doc:"The display title of this definition."`
	Description string          `xorm:"text null" json:"description" doc:"A longer description of this definition, at most 4096 bytes."`
	FieldType   CustomFieldType `xorm:"varchar(32) not null" json:"field_type" doc:"The immutable type of the field. It selects which column of custom_field_values holds values and which editor the frontend uses; it never changes."`

	IsArchived  bool    `xorm:"not null default false index(project_is_archived_position)" json:"is_archived" doc:"Whether this definition is archived. Archived definitions cannot receive new values but continue to describe retained ones."`
	Position    float64 `xorm:"double null index(project_is_archived_position)" json:"position" doc:"The position of this definition for ordering, following the usual Vikunja position convention."`
	ShowOnCard  bool    `xorm:"not null default false" json:"show_on_card" doc:"Whether values of this field are shown on task cards."`
	ShowInTable bool    `xorm:"not null default false" json:"show_in_table" doc:"Whether this field is offered as a table column in views."`

	// IncludeArchived asks a ReadAll to include archived definitions in the
	// result. It is a query-carried flag, never stored on the row.
	IncludeArchived bool `xorm:"-" json:"-"`

	Configuration *CustomFieldConfiguration `xorm:"json null default null" json:"configuration,omitempty" doc:"The validated, canonicalised per-type configuration, at most 16 KiB. Only number fields carry settings (precision plus optional min, max, step, and display unit)."`

	DefaultValue *CustomFieldValue `xorm:"json null default null" json:"default_value,omitempty" doc:"A validated default value, materialised into a regular value row when a new task is created. Changing it is not retroactive and unsetting a task value does not reapply it."`

	Created time.Time `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this definition was created. You cannot change this value."`
	Updated time.Time `xorm:"updated not null" json:"updated" readOnly:"true" doc:"A timestamp when this definition was last updated. You cannot change this value."`
}

// TableName makes a pretty table name
func (*CustomFieldDefinition) TableName() string {
	return "custom_field_definitions"
}

// CustomFieldConfiguration is the per-type configuration of a definition. It is
// validated and canonicalised against the field type before storage; unknown
// JSON properties are rejected.
type CustomFieldConfiguration struct {
	// Number fields: precision 0-6 plus optional minimum, maximum, and step as
	// exact decimal literals (compared at the definition's precision) and a
	// display unit.
	Precision *int    `json:"precision,omitempty" doc:"Number precision, 0-6 fractional digits and the scaling applied to stored values."`
	Min       *string `json:"min,omitempty" doc:"Optional fixed-point minimum for number values, exact at the definition's precision."`
	Max       *string `json:"max,omitempty" doc:"Optional fixed-point maximum for number values, exact at the definition's precision."`
	Step      *string `json:"step,omitempty" doc:"Optional positive fixed-point step for number values, exact at the definition's precision."`
	Unit      string  `json:"unit,omitempty" doc:"Optional display unit for number values."`
}

func (c *CustomFieldConfiguration) UnmarshalJSON(data []byte) error {
	// Reject unknown properties: a config with a typo should fail loudly
	// instead of being silently stored.
	type wire CustomFieldConfiguration
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var w wire
	if err := dec.Decode(&w); err != nil {
		return ErrInvalidCustomFieldConfiguration{Message: err.Error()}
	}
	*c = CustomFieldConfiguration(w)
	return nil
}

func (c *CustomFieldConfiguration) isEmpty() bool {
	return c == nil || (c.Precision == nil && c.Min == nil && c.Max == nil && c.Step == nil && c.Unit == "")
}

// validateAndCanonicalise validates the configuration against the definition's
// field type and returns the canonical form to store. Only number fields carry
// settings; for every other type an empty configuration canonicalises to nil.
// An empty configuration on a number field canonicalises to nil too, so a
// plain number field is stored without a config row rather than an explicit
// precision 0.
func (c *CustomFieldConfiguration) validateAndCanonicalise(t CustomFieldType) (*CustomFieldConfiguration, error) {
	if t != CustomFieldTypeNumber {
		if !c.isEmpty() {
			return nil, ErrInvalidCustomFieldConfiguration{Message: fmt.Sprintf("Field type %q does not support a configuration.", string(t))}
		}
		return nil, nil
	}

	if c == nil || c.isEmpty() {
		return nil, nil
	}

	precision := 0
	if c.Precision != nil {
		precision = *c.Precision
	}
	if precision < 0 || precision > 6 {
		return nil, ErrInvalidCustomFieldConfiguration{Message: "Number precision must be between 0 and 6."}
	}

	// Min, max, and step are exact decimal literals scaled at the precision.
	for _, limit := range []struct {
		value *string
		name  string
	}{{c.Min, "minimum"}, {c.Max, "maximum"}, {c.Step, "step"}} {
		if limit.value == nil {
			continue
		}
		if _, err := parseFixedPoint(*limit.value, precision); err != nil {
			return nil, ErrInvalidCustomFieldConfiguration{
				Message: fmt.Sprintf("The number %s is invalid: %s.", limit.name, customFieldNumberErrorMessage(err)),
			}
		}
	}
	if c.Step != nil {
		stepScaled, _ := parseFixedPoint(*c.Step, precision)
		if stepScaled <= 0 {
			return nil, ErrInvalidCustomFieldConfiguration{Message: "The number step must be greater than zero."}
		}
	}
	if c.Min != nil && c.Max != nil {
		minScaled, _ := parseFixedPoint(*c.Min, precision)
		maxScaled, _ := parseFixedPoint(*c.Max, precision)
		if minScaled > maxScaled {
			return nil, ErrInvalidCustomFieldConfiguration{Message: "The number minimum may not be greater than the maximum."}
		}
	}

	canonical := *c
	canonical.Precision = &precision
	return &canonical, nil
}

// customFieldNumberErrorMessage extracts the human message from a
// ErrInvalidCustomFieldNumber so it can be embedded in a configuration error.
func customFieldNumberErrorMessage(err error) string {
	var numErr ErrInvalidCustomFieldNumber
	if errors.As(err, &numErr) {
		return numErr.Message
	}
	return err.Error()
}

// --- reads ---

func GetCustomFieldDefinitionByID(s *xorm.Session, id int64) (*CustomFieldDefinition, error) {
	def := &CustomFieldDefinition{}
	exists, err := s.Where("id = ?", id).Get(def)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomFieldDefinitionDoesNotExist{DefinitionID: id}
	}
	return def, nil
}

// GetCustomFieldDefinitionByIDAndProject loads a definition only when it belongs
// to the given project; any other id resolves to ErrCustomFieldDefinitionDoesNotExist
// so a path scoped to the wrong parent project fails as not found, not forbidden.
func GetCustomFieldDefinitionByIDAndProject(s *xorm.Session, defID, projectID int64) (def *CustomFieldDefinition, err error) {
	def = &CustomFieldDefinition{}
	exists, err := s.
		Where("id = ? AND project_id = ?", defID, projectID).
		NoAutoCondition().
		Get(def)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomFieldDefinitionDoesNotExist{DefinitionID: defID}
	}
	return def, nil
}

func getCustomFieldDefinitionsForProject(s *xorm.Session, projectID int64) ([]*CustomFieldDefinition, error) {
	defs := []*CustomFieldDefinition{}
	err := s.Where("project_id = ?", projectID).Find(&defs)
	return defs, err
}

// lockCustomFieldDefinition serializes value-affecting operations on the
// definition row without touching any timestamp: value writes, definition
// updates, archiving, and permanent deletion all take this lock before
// inspecting or changing values. PostgreSQL/MySQL use SELECT ... FOR UPDATE;
// SQLite has no FOR UPDATE, so a no-op write takes the database write lock as
// the first statement (avoiding a stale-snapshot read→write upgrade).
func lockCustomFieldDefinition(s *xorm.Session, defID int64) error {
	if db.Type() == schemas.SQLITE {
		// NoAutoTime keeps the no-op write from auto-filling the updated column.
		_, err := s.NoAutoTime().ID(defID).Cols("id").Update(&CustomFieldDefinition{ID: defID})
		return err
	}
	_, err := s.ForUpdate().Where("id = ?", defID).Get(&CustomFieldDefinition{})
	return err
}

// --- permissions ---

// CanRead lets any project reader see the active definitions of the project.
// Project.CanRead already resolves link shares and instance-admin bypass, so the
// wiki's link-share behaviour follows for free.
func (d *CustomFieldDefinition) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	// Resolve the parent first so a wrong-project path is a 404 even for an
	// instance administrator.
	existing, err := GetCustomFieldDefinitionByIDAndProject(s, d.ID, d.ProjectID)
	if err != nil {
		return false, 0, err
	}

	if isInstanceAdmin(s, a) {
		return true, int(PermissionAdmin), nil
	}

	return (&Project{ID: existing.ProjectID}).CanRead(s, a)
}

// CanCreate gates definition management behind project admin, matching the wiki:
// "Project administrators manage definitions and options."
func (d *CustomFieldDefinition) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	if isInstanceAdmin(s, a) {
		return true, nil
	}
	return (&Project{ID: d.ProjectID}).IsAdmin(s, a)
}

func (d *CustomFieldDefinition) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	// Reject a definition that is not in the path project before authorizing
	// against it: an admin of the path project must not mutate another project's
	// definition under that path, and the parent check applies to instance
	// administrators too.
	existing, err := GetCustomFieldDefinitionByIDAndProject(s, d.ID, d.ProjectID)
	if err != nil {
		return false, err
	}

	if isInstanceAdmin(s, a) {
		return true, nil
	}
	return (&Project{ID: existing.ProjectID}).IsAdmin(s, a)
}

func (d *CustomFieldDefinition) CanDelete(s *xorm.Session, a web.Auth) (bool, error) {
	return d.CanUpdate(s, a)
}

// --- CRUD ---

func (d *CustomFieldDefinition) ReadOne(s *xorm.Session, _ web.Auth) (err error) {
	def, err := GetCustomFieldDefinitionByIDAndProject(s, d.ID, d.ProjectID)
	if err != nil {
		return err
	}
	*d = *def
	return
}

// ReadAll lists the definitions of one project. Archived definitions are hidden
// unless d.IncludeArchived is set; the route feeds that flag from a query param.
func (d *CustomFieldDefinition) ReadAll(s *xorm.Session, a web.Auth, search string, page, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	can, _, err := (&Project{ID: d.ProjectID}).CanRead(s, a)
	if err != nil {
		return nil, 0, 0, err
	}
	if !can {
		return nil, 0, 0, ErrGenericForbidden{}
	}

	cond := builder.NewCond()
	cond = cond.And(builder.Eq{"project_id": d.ProjectID})
	if !d.IncludeArchived {
		cond = cond.And(builder.Eq{"is_archived": false})
	}
	if search != "" {
		cond = cond.And(builder.Or(
			builder.Like{"title", search},
			builder.Like{"machine_key", search},
		))
	}

	totalCount, err := s.Where(cond).Count(&CustomFieldDefinition{})
	if err != nil {
		return nil, 0, 0, err
	}

	limit, start := getLimitFromPageIndex(page, perPage)
	query := s.Where(cond).OrderBy("position asc")
	if limit > 0 {
		query = query.Limit(limit, start)
	}
	defs := []*CustomFieldDefinition{}
	if err = query.Find(&defs); err != nil {
		return nil, 0, 0, err
	}

	return defs, len(defs), totalCount, nil
}

// Delete archives the definition rather than removing the row, so retained
// values and options keep being describable. Idempotent.
func (d *CustomFieldDefinition) Delete(s *xorm.Session, _ web.Auth) (err error) {
	if err = lockCustomFieldDefinition(s, d.ID); err != nil {
		return err
	}
	existing, err := GetCustomFieldDefinitionByIDAndProject(s, d.ID, d.ProjectID)
	if err != nil {
		return err
	}
	if existing.IsArchived {
		return nil
	}

	if _, err = s.ID(d.ID).Cols("is_archived", "updated").Update(&CustomFieldDefinition{IsArchived: true}); err != nil {
		return err
	}
	return updateProjectLastUpdated(s, &Project{ID: existing.ProjectID})
}

// DeletePermanently removes the definition together with its values, options,
// and multi-select memberships. Populated definitions return a conflict unless
// deleteValues is set, which is how the API surfaces the confirmed destructive
// action. Cleanup order follows the wiki: memberships, values, options, definition.
func (d *CustomFieldDefinition) DeletePermanently(s *xorm.Session, deleteValues bool) (err error) {
	existing, err := GetCustomFieldDefinitionByIDAndProject(s, d.ID, d.ProjectID)
	if err != nil {
		return err
	}

	// Lock the definition row so a concurrent value write (which takes the same
	// lock) cannot insert a value for this definition while it is being deleted.
	if err = lockCustomFieldDefinition(s, d.ID); err != nil {
		return err
	}

	count, err := s.Where("definition_id = ?", d.ID).Count(&TaskCustomFieldValue{})
	if err != nil {
		return err
	}
	if count > 0 && !deleteValues {
		return ErrCustomFieldDefinitionHasValues{DefinitionID: d.ID}
	}

	// Delete memberships and values in bounded batches so a project at the
	// documented limits never builds an oversized IN list.
	const batchSize = 500
	for {
		rows := []*TaskCustomFieldValue{}
		if err = s.Where("definition_id = ?", d.ID).Limit(batchSize).Cols("id").Find(&rows); err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if _, err = s.In("value_id", ids).Delete(&CustomFieldValueOption{}); err != nil {
			return err
		}
		if _, err = s.In("id", ids).Delete(&TaskCustomFieldValue{}); err != nil {
			return err
		}
	}

	if _, err = s.Where("definition_id = ?", d.ID).Delete(&CustomFieldOption{}); err != nil {
		return err
	}
	if _, err = s.Where("id = ?", d.ID).Delete(&CustomFieldDefinition{}); err != nil {
		return err
	}

	return updateProjectLastUpdated(s, &Project{ID: existing.ProjectID})
}

// --- write path ---

func (d *CustomFieldDefinition) validate(s *xorm.Session) error {
	if err := validateCustomFieldKey(d.MachineKey); err != nil {
		return err
	}
	if err := d.FieldType.Validate(); err != nil {
		return err
	}
	if titleLen := utf8.RuneCountInString(d.Title); titleLen < 1 || titleLen > 250 {
		return ErrInvalidCustomFieldDefinition{Message: "The title must be between 1 and 250 characters."}
	}
	if len([]byte(d.Description)) > 4096 {
		return ErrInvalidCustomFieldDefinition{Message: "The description may not exceed 4096 bytes."}
	}

	canonical, err := d.Configuration.validateAndCanonicalise(d.FieldType)
	if err != nil {
		return err
	}
	d.Configuration = canonical
	if canonical != nil {
		encoded, err := json.Marshal(canonical)
		if err != nil {
			return fmt.Errorf("could not encode custom field configuration: %w", err)
		}
		if len(encoded) > 16*1024 {
			return ErrInvalidCustomFieldConfiguration{Message: "The encoded configuration may not exceed 16 KiB."}
		}
	}

	if d.DefaultValue == nil {
		return nil
	}
	if err := d.DefaultValue.validate(); err != nil {
		return err
	}
	return validateValueAgainstDefinition(s, d.DefaultValue, d)
}

// Create adds a definition to a project, enforcing the per-project limit and
// backfilling the position.
func (d *CustomFieldDefinition) Create(s *xorm.Session, _ web.Auth) (err error) {
	if err = d.validate(s); err != nil {
		return err
	}
	if _, err = GetProjectSimpleByID(s, d.ProjectID); err != nil {
		return err
	}

	// Updating the parent serializes limit checks for definitions and options
	// in this project. The transaction rolls this timestamp change back when a
	// later validation or insert fails.
	if err = updateProjectLastUpdated(s, &Project{ID: d.ProjectID}); err != nil {
		return err
	}

	exists, err := s.Where("project_id = ? AND machine_key = ?", d.ProjectID, d.MachineKey).Exist(&CustomFieldDefinition{})
	if err != nil {
		return err
	}
	if exists {
		return ErrInvalidCustomFieldDefinition{Message: "A definition with this machine key already exists in this project."}
	}

	count, err := s.Where("project_id = ?", d.ProjectID).Count(&CustomFieldDefinition{})
	if err != nil {
		return err
	}
	if count >= 100 {
		return ErrCustomFieldDefinitionLimitReached{}
	}

	d.ID = 0
	if _, err = s.Insert(d); err != nil {
		return err
	}

	d.Position = calculateDefaultPosition(d.ID, d.Position)
	if _, err = s.Where("id = ?", d.ID).Update(d); err != nil {
		return err
	}

	return nil
}

// Update edits the editable fields of a definition. Its project, machine key,
// and field type are immutable, and new numeric constraints must remain valid
// for every stored value.
func (d *CustomFieldDefinition) Update(s *xorm.Session, _ web.Auth) (err error) {
	// Lock the definition row before validating the new configuration against
	// stored values, so a concurrent value write cannot commit a value that
	// violates the constraints being applied.
	if err = lockCustomFieldDefinition(s, d.ID); err != nil {
		return err
	}
	existing, err := GetCustomFieldDefinitionByID(s, d.ID)
	if err != nil {
		return err
	}

	if existing.MachineKey != d.MachineKey {
		return ErrInvalidCustomFieldDefinition{Message: "The machine key of a definition is immutable."}
	}
	if existing.FieldType != d.FieldType {
		return ErrInvalidCustomFieldDefinition{Message: "The field type of a definition is immutable."}
	}
	if existing.ProjectID != d.ProjectID {
		return ErrInvalidCustomFieldDefinition{Message: "A definition cannot be moved to another project."}
	}

	if err = d.validate(s); err != nil {
		return err
	}
	if err = validateConfigurationAgainstStoredValues(s, existing, d); err != nil {
		return err
	}

	_, err = s.
		Where("id = ?", d.ID).
		Cols("title", "description", "position", "is_archived", "show_on_card", "show_in_table", "configuration", "default_value", "updated").
		Update(d)
	if err != nil {
		return err
	}

	return updateProjectLastUpdated(s, &Project{ID: existing.ProjectID})
}

func validateConfigurationAgainstStoredValues(s *xorm.Session, existing, updated *CustomFieldDefinition) error {
	if existing.FieldType != CustomFieldTypeNumber {
		return nil
	}

	rows := []*TaskCustomFieldValue{}
	err := s.Where("definition_id = ?", existing.ID).
		And("value_number IS NOT NULL").
		Find(&rows)
	if err != nil {
		return err
	}

	oldPrecision := numberPrecision(existing)
	for _, row := range rows {
		stored := *row.ValueNumber
		number := &CustomFieldNumber{
			Value:     stored,
			Precision: oldPrecision,
			raw:       formatFixedPoint(stored, oldPrecision),
		}
		value := &CustomFieldValue{Type: CustomFieldTypeNumber, Number: number}
		if err = validateNumberValue(value, updated); err != nil {
			return ErrCustomFieldConfigurationChangeInvalidatesValues{Message: fmt.Sprintf("The new configuration invalidates the value on task %d.", row.TaskID)}
		}
		if number.Value != stored {
			return ErrCustomFieldConfigurationChangeInvalidatesValues{Message: "Changing precision would require rescaling existing values."}
		}
	}
	return nil
}

// validateValueAgainstDefinition checks a value against everything that
// depends on the definition it is set for: matching type, per-type length and
// format limits, the number configuration, option ownership, and user
// visibility. It is shared by defaults and by value set operations.
func validateValueAgainstDefinition(s *xorm.Session, v *CustomFieldValue, def *CustomFieldDefinition) error {
	if v == nil {
		return nil
	}
	if v.Type != def.FieldType {
		return ErrInvalidCustomFieldValue{
			Message: fmt.Sprintf("The value type %q does not match the definition type %q.", string(v.Type), string(def.FieldType)),
		}
	}

	switch v.Type {
	case CustomFieldTypeShortText:
		if utf8.RuneCountInString(*v.ShortText) > 255 {
			return ErrInvalidCustomFieldValue{Message: "Short text values may not exceed 255 characters."}
		}
	case CustomFieldTypeLongText:
		if len([]byte(*v.LongText)) > 65535 {
			return ErrInvalidCustomFieldValue{Message: "Long text values may not exceed 65535 bytes."}
		}
	case CustomFieldTypeNumber:
		return validateNumberValue(v, def)
	case CustomFieldTypeBoolean, CustomFieldTypeDate, CustomFieldTypeDateTime:
		// Shape was checked at parse time; no definition-dependent rules apply.
	case CustomFieldTypeURL:
		return validateURLValue(*v.URL)
	case CustomFieldTypeSingleSelect:
		return validateOptionValue(s, *v.SingleOptionID, def)
	case CustomFieldTypeMultiSelect:
		for _, id := range v.OptionIDs {
			if err := validateOptionValue(s, id, def); err != nil {
				return err
			}
		}
	case CustomFieldTypeUser:
		return validateUserValue(s, *v.UserID, def)
	}

	return nil
}

func validateNumberValue(v *CustomFieldValue, def *CustomFieldDefinition) error {
	precision := numberPrecision(def)
	if err := v.Number.Scale(precision); err != nil {
		return err
	}

	cfg := def.Configuration
	if cfg == nil {
		return nil
	}
	if cfg.Min != nil {
		minScaled, _ := parseFixedPoint(*cfg.Min, precision)
		if v.Number.Value < minScaled {
			return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The value is below the configured minimum of %s.", formatFixedPoint(minScaled, precision))}
		}
	}
	if cfg.Max != nil {
		maxScaled, _ := parseFixedPoint(*cfg.Max, precision)
		if v.Number.Value > maxScaled {
			return ErrInvalidCustomFieldValue{Message: "The value is above the configured maximum."}
		}
	}
	if cfg.Step != nil {
		stepScaled, _ := parseFixedPoint(*cfg.Step, precision)
		if stepScaled > 0 && v.Number.Value%stepScaled != 0 {
			return ErrInvalidCustomFieldValue{Message: "The value is not a multiple of the configured step."}
		}
	}
	return nil
}

func numberPrecision(d *CustomFieldDefinition) int {
	if d == nil || d.Configuration == nil || d.Configuration.Precision == nil {
		return 0
	}
	return *d.Configuration.Precision
}

func validateURLValue(raw string) error {
	if len([]byte(raw)) > 2048 {
		return ErrInvalidCustomFieldURL{Value: raw}
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrInvalidCustomFieldURL{Value: raw}
	}
	return nil
}

// validateOptionValue rejects unknown, foreign, and archived options. Archived
// options can no longer be selected but continue to describe retained values,
// which is why this check runs at set time and not when loading values.
func validateOptionValue(s *xorm.Session, optionID int64, def *CustomFieldDefinition) error {
	opt, err := GetCustomFieldOptionByID(s, optionID)
	if err != nil {
		return err
	}
	if opt.DefinitionID != def.ID {
		return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The option %d does not belong to this definition.", optionID)}
	}
	if opt.IsArchived {
		return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The option %d is archived and can no longer be selected.", optionID)}
	}
	return nil
}

// validateUserValue rejects missing, inactive, or project-invisible users. A
// disabled or locked user is already rejected by GetUserByID.
func validateUserValue(s *xorm.Session, userID int64, def *CustomFieldDefinition) error {
	u, err := user.GetUserByID(s, userID)
	if err != nil {
		return ErrCustomFieldUserNotVisible{UserID: userID}
	}

	project, _, err := getProjectSimple(s, builder.Eq{"id": def.ProjectID})
	if err != nil {
		return err
	}
	can, _, err := project.CanRead(s, u)
	if err != nil {
		return err
	}
	if !can {
		return ErrCustomFieldUserNotVisible{UserID: userID}
	}
	return nil
}
