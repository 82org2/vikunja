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
	"strconv"
	"strings"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// resolveCustomFieldFilters resolves every custom_fields.<key> filter and sort
// against the queried projects' definitions, validates the comparator against
// the type, casts the value, and stores the per-project definitions on the
// filter/sort params. It must run before convertFiltersToDBFilterCond and
// getOrderByDBStatement, where the session and project ids are available.
//
// The same machine key can resolve to different definition ids in different
// projects, so definitions are stored per project and the SQL resolves the
// definition per task's project. Every definition for a key must share its
// type (and, for number fields, its precision) or the query fails validation
// instead of returning partial results.
func resolveCustomFieldFilters(s *xorm.Session, projectIDs []int64, filters []*taskFilter, sortby []*sortParam) error {
	keys := collectCustomFieldKeys(filters, sortby)
	if len(keys) == 0 || len(projectIDs) == 0 {
		return nil
	}

	defs, err := getCustomFieldDefinitionsByKeys(s, projectIDs, keys)
	if err != nil {
		return err
	}

	byKey := make(map[string][]*CustomFieldDefinition, len(keys))
	for _, def := range defs {
		byKey[def.MachineKey] = append(byKey[def.MachineKey], def)
	}

	for _, key := range keys {
		keyDefs := byKey[key]
		if len(keyDefs) == 0 {
			return ErrInvalidTaskField{TaskField: customFieldFilterNamespace + key}
		}
		if err := checkCustomFieldDefinitionConsistency(keyDefs); err != nil {
			return err
		}
	}

	for _, f := range filters {
		if err := resolveCustomFieldFilter(f, byKey); err != nil {
			return err
		}
	}

	alias := 0
	for _, sp := range sortby {
		if !strings.HasPrefix(sp.sortBy, customFieldFilterNamespace) {
			continue
		}
		key := strings.TrimPrefix(sp.sortBy, customFieldFilterNamespace)
		keyDefs := byKey[key]
		if len(keyDefs) == 0 {
			return ErrInvalidTaskField{TaskField: sp.sortBy}
		}
		def := keyDefs[0]
		sp.customFieldDefs = defsByProject(keyDefs)
		if !isCustomFieldSortable(def.FieldType) {
			return ErrInvalidTaskField{TaskField: sp.sortBy}
		}
		alias++
		sp.customFieldAlias = fmt.Sprintf("cfv%d", alias)
	}

	return nil
}

// resolveCustomFieldFilter resolves one custom-field filter, recursing into
// parenthesised groups so a custom_fields.<key> filter nested inside a group is
// resolved and cast like a top-level one.
func resolveCustomFieldFilter(f *taskFilter, byKey map[string][]*CustomFieldDefinition) error {
	if nested, is := f.value.([]*taskFilter); is {
		for _, sub := range nested {
			if err := resolveCustomFieldFilter(sub, byKey); err != nil {
				return err
			}
		}
		return nil
	}

	if f.customFieldKey == "" {
		return nil
	}
	keyDefs := byKey[f.customFieldKey]
	def := keyDefs[0]
	f.customFieldDefs = defsByProject(keyDefs)
	if err := validateCustomFieldComparator(def, f.comparator); err != nil {
		return err
	}
	if f.comparator != taskFilterComparatorIsNull && f.comparator != taskFilterComparatorIsNotNull {
		raw, is := f.value.(string)
		if !is {
			return ErrInvalidTaskFilterValue{Field: customFieldFilterNamespace + f.customFieldKey, Value: f.value}
		}
		cast, err := castCustomFieldFilterValue(def, f.comparator, raw)
		if err != nil {
			return err
		}
		f.value = cast
	}
	return nil
}

func collectCustomFieldKeys(filters []*taskFilter, sortby []*sortParam) []string {
	seen := map[string]struct{}{}
	keys := []string{}
	add := func(key string) {
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	var walk func(filters []*taskFilter)
	walk = func(filters []*taskFilter) {
		for _, f := range filters {
			if nested, is := f.value.([]*taskFilter); is {
				walk(nested)
				continue
			}
			add(f.customFieldKey)
		}
	}
	walk(filters)
	for _, sp := range sortby {
		if strings.HasPrefix(sp.sortBy, customFieldFilterNamespace) {
			add(strings.TrimPrefix(sp.sortBy, customFieldFilterNamespace))
		}
	}
	return keys
}

func getCustomFieldDefinitionsByKeys(s *xorm.Session, projectIDs []int64, keys []string) ([]*CustomFieldDefinition, error) {
	defs := []*CustomFieldDefinition{}
	const batchSize = 500
	for chunk := range slices.Chunk(keys, batchSize) {
		batch := []*CustomFieldDefinition{}
		if err := s.In("project_id", projectIDs).In("machine_key", chunk).Find(&batch); err != nil {
			return nil, err
		}
		defs = append(defs, batch...)
	}
	return defs, nil
}

// checkCustomFieldDefinitionConsistency enforces the wiki's cross-project rule:
// every definition for a key must share its type, and number definitions must
// share their precision (differently scaled integers are not comparable).
func checkCustomFieldDefinitionConsistency(defs []*CustomFieldDefinition) error {
	first := defs[0]
	for _, def := range defs[1:] {
		if def.FieldType != first.FieldType {
			return ErrInvalidTaskField{TaskField: customFieldFilterNamespace + first.MachineKey}
		}
		if def.FieldType == CustomFieldTypeNumber && numberPrecision(def) != numberPrecision(first) {
			return ErrInvalidTaskField{TaskField: customFieldFilterNamespace + first.MachineKey}
		}
	}
	return nil
}

func defsByProject(defs []*CustomFieldDefinition) map[int64]*CustomFieldDefinition {
	m := make(map[int64]*CustomFieldDefinition, len(defs))
	for _, def := range defs {
		m[def.ProjectID] = def
	}
	return m
}

func firstCustomFieldDef(defs map[int64]*CustomFieldDefinition) *CustomFieldDefinition {
	for _, def := range defs {
		return def
	}
	return nil
}

// validateCustomFieldComparator enforces the wiki's comparator matrix: equality
// and inequality on every type, ordering on number/date/datetime, pattern
// matching on short/long text, membership on select fields, and the null
// operators on every type.
func validateCustomFieldComparator(def *CustomFieldDefinition, comparator taskFilterComparator) error {
	switch comparator {
	case taskFilterComparatorEquals, taskFilterComparatorNotEquals,
		taskFilterComparatorIsNull, taskFilterComparatorIsNotNull:
		return nil
	case taskFilterComparatorGreater, taskFilterComparatorGreateEquals,
		taskFilterComparatorLess, taskFilterComparatorLessEquals:
		switch def.FieldType {
		case CustomFieldTypeNumber, CustomFieldTypeDate, CustomFieldTypeDateTime:
			return nil
		case CustomFieldTypeShortText, CustomFieldTypeLongText, CustomFieldTypeBoolean,
			CustomFieldTypeURL, CustomFieldTypeSingleSelect, CustomFieldTypeMultiSelect, CustomFieldTypeUser:
			// Ordering comparisons are invalid for these types.
		}
	case taskFilterComparatorLike:
		switch def.FieldType {
		case CustomFieldTypeShortText, CustomFieldTypeLongText:
			return nil
		case CustomFieldTypeNumber, CustomFieldTypeBoolean, CustomFieldTypeDate,
			CustomFieldTypeDateTime, CustomFieldTypeURL, CustomFieldTypeSingleSelect,
			CustomFieldTypeMultiSelect, CustomFieldTypeUser:
			// Pattern matching is invalid for these types.
		}
	case taskFilterComparatorIn, taskFilterComparatorNotIn:
		switch def.FieldType {
		case CustomFieldTypeSingleSelect, CustomFieldTypeMultiSelect:
			return nil
		case CustomFieldTypeShortText, CustomFieldTypeLongText, CustomFieldTypeNumber,
			CustomFieldTypeBoolean, CustomFieldTypeDate, CustomFieldTypeDateTime,
			CustomFieldTypeURL, CustomFieldTypeUser:
			// Membership is invalid for these types.
		}
	case taskFilterComparatorInvalid:
		// Falls through to the error below.
	}
	return ErrInvalidTaskFilterValue{Field: customFieldFilterNamespace + def.MachineKey, Value: string(comparator)}
}

// castCustomFieldFilterValue casts a raw filter value to the stored column
// type. in/not in split the comma-separated value and cast each element.
func castCustomFieldFilterValue(def *CustomFieldDefinition, comparator taskFilterComparator, raw string) (interface{}, error) {
	if comparator == taskFilterComparatorIn || comparator == taskFilterComparatorNotIn {
		vals := strings.Split(raw, ",")
		out := make([]interface{}, 0, len(vals))
		for _, v := range vals {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			cast, err := castSingleCustomFieldFilterValue(def, v)
			if err != nil {
				return nil, err
			}
			out = append(out, cast)
		}
		return out, nil
	}
	return castSingleCustomFieldFilterValue(def, raw)
}

func castSingleCustomFieldFilterValue(def *CustomFieldDefinition, raw string) (interface{}, error) {
	field := customFieldFilterNamespace + def.MachineKey
	switch def.FieldType {
	case CustomFieldTypeShortText, CustomFieldTypeLongText, CustomFieldTypeURL:
		return raw, nil
	case CustomFieldTypeNumber:
		v, err := parseFixedPoint(raw, numberPrecision(def))
		if err != nil {
			return nil, ErrInvalidTaskFilterValue{Field: field, Value: raw}
		}
		return v, nil
	case CustomFieldTypeBoolean:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, ErrInvalidTaskFilterValue{Field: field, Value: raw}
		}
		return v, nil
	case CustomFieldTypeDate:
		d := &CustomFieldDate{}
		if err := d.parse(raw); err != nil {
			return nil, ErrInvalidTaskFilterValue{Field: field, Value: raw}
		}
		return d.DayCount, nil
	case CustomFieldTypeDateTime:
		t, err := parseTimeFromUserInput(raw, nil)
		if err != nil {
			return nil, ErrInvalidTaskFilterValue{Field: field, Value: raw}
		}
		return t, nil
	case CustomFieldTypeUser, CustomFieldTypeSingleSelect, CustomFieldTypeMultiSelect:
		v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, ErrInvalidTaskFilterValue{Field: field, Value: raw}
		}
		return v, nil
	}
	return nil, ErrInvalidTaskFilterValue{Field: field, Value: raw}
}

// customFieldValueColumn maps a definition type to its typed column in
// custom_field_values. Multi-select has no typed column and is handled
// separately.
func customFieldValueColumn(def *CustomFieldDefinition) string {
	switch def.FieldType {
	case CustomFieldTypeShortText:
		return "value_short_text"
	case CustomFieldTypeLongText:
		return "value_long_text"
	case CustomFieldTypeNumber:
		return "value_number"
	case CustomFieldTypeBoolean:
		return "value_boolean"
	case CustomFieldTypeDate:
		return "value_date"
	case CustomFieldTypeDateTime:
		return "value_datetime"
	case CustomFieldTypeURL:
		return "value_url"
	case CustomFieldTypeUser:
		return "value_user_id"
	case CustomFieldTypeSingleSelect:
		return "value_single_option_id"
	case CustomFieldTypeMultiSelect:
		// Multi-select stores its selection in custom_field_value_options.
		return ""
	}
	return ""
}

// isCustomFieldSortable reports whether a type may be sorted in the first
// release. Long text, URL, and multi select are not sortable.
func isCustomFieldSortable(t CustomFieldType) bool {
	switch t {
	case CustomFieldTypeShortText, CustomFieldTypeNumber, CustomFieldTypeBoolean,
		CustomFieldTypeDate, CustomFieldTypeDateTime, CustomFieldTypeUser, CustomFieldTypeSingleSelect:
		return true
	case CustomFieldTypeLongText, CustomFieldTypeURL, CustomFieldTypeMultiSelect:
		return false
	}
	return false
}

func customFieldDefinitionIDs(defs map[int64]*CustomFieldDefinition) []int64 {
	ids := make([]int64, 0, len(defs))
	for _, def := range defs {
		ids = append(ids, def.ID)
	}
	return ids
}

// Task IDs are globally unique and value writes enforce that the task and
// definition belong to the same project, so resolved definition IDs avoid a
// correlated raw-SQL expression here.
func customFieldValueTaskIDsSubQuery(f *taskFilter) *builder.Builder {
	return builder.Select("task_id").
		From("custom_field_values").
		Where(builder.In("definition_id", customFieldDefinitionIDs(f.customFieldDefs)))
}

func customFieldMissingValueCond(f *taskFilter, taskAlias string) builder.Cond {
	return builder.NotIn(taskAlias+".id", customFieldValueTaskIDsSubQuery(f))
}

// buildCustomFieldFilterCond builds the EXISTS/NOT EXISTS condition for one
// custom-field filter. Negative predicates (!=, not in) require an existing
// value row plus a non-matching value, so they use EXISTS with a negated value
// condition rather than NOT EXISTS — a task with no value row must not match
// `impact != 5`. filter_include_nulls ORs the missing-row condition so unset
// tasks are included only when explicitly requested.
func buildCustomFieldFilterCond(f *taskFilter, taskAlias string, includeNulls bool) (builder.Cond, error) {
	key := f.customFieldKey
	def := firstCustomFieldDef(f.customFieldDefs)

	baseSub := customFieldValueTaskIDsSubQuery(f)

	switch f.comparator {
	case taskFilterComparatorIsNull:
		return builder.NotIn(taskAlias+".id", baseSub), nil
	case taskFilterComparatorIsNotNull:
		return builder.In(taskAlias+".id", baseSub), nil
	case taskFilterComparatorEquals, taskFilterComparatorNotEquals,
		taskFilterComparatorGreater, taskFilterComparatorGreateEquals,
		taskFilterComparatorLess, taskFilterComparatorLessEquals,
		taskFilterComparatorLike, taskFilterComparatorIn, taskFilterComparatorNotIn:
		// Falls through to the value-condition building below.
	case taskFilterComparatorInvalid:
		return nil, ErrInvalidTaskFilterValue{Field: customFieldFilterNamespace + key, Value: string(f.comparator)}
	}

	if def.FieldType == CustomFieldTypeMultiSelect {
		return buildMultiSelectCustomFieldCond(f, taskAlias, key, includeNulls)
	}

	col := "custom_field_values." + customFieldValueColumn(def)
	valueCond, err := buildCustomFieldValueCond(col, f.comparator, f.value)
	if err != nil {
		return nil, err
	}

	filter := builder.In(taskAlias+".id", baseSub.And(valueCond))
	if includeNulls {
		filter = builder.Or(filter, customFieldMissingValueCond(f, taskAlias))
	}
	return filter, nil
}

// buildMultiSelectCustomFieldCond builds the condition for a multi-select
// filter. Positive predicates (in, =) match a value row with a matching
// membership; negative predicates (not in, !=) match a value row without one.
// The membership condition always uses the positive form and negative
// predicates wrap it in NOT EXISTS, so `tags != 3` means "has a value row and
// option 3 is not among its memberships".
func buildMultiSelectCustomFieldCond(f *taskFilter, taskAlias, key string, includeNulls bool) (builder.Cond, error) {
	valueRowSub := customFieldValueTaskIDsSubQuery(f)

	membershipCond, err := buildCustomFieldMembershipCond(f.comparator, f.value)
	if err != nil {
		return nil, err
	}
	membershipSub := builder.Select("value_id").
		From("custom_field_value_options").
		Where(membershipCond)

	var filter builder.Cond
	switch f.comparator {
	case taskFilterComparatorEquals, taskFilterComparatorIn:
		filter = builder.In(taskAlias+".id", valueRowSub.And(builder.In("id", membershipSub)))
	case taskFilterComparatorNotEquals, taskFilterComparatorNotIn:
		filter = builder.In(taskAlias+".id", valueRowSub.And(builder.NotIn("id", membershipSub)))
	case taskFilterComparatorGreater, taskFilterComparatorGreateEquals,
		taskFilterComparatorLess, taskFilterComparatorLessEquals,
		taskFilterComparatorLike, taskFilterComparatorIsNull, taskFilterComparatorIsNotNull,
		taskFilterComparatorInvalid:
		return nil, ErrInvalidTaskFilterValue{Field: customFieldFilterNamespace + key, Value: string(f.comparator)}
	}
	if includeNulls {
		filter = builder.Or(filter, customFieldMissingValueCond(f, taskAlias))
	}
	return filter, nil
}

// buildCustomFieldMembershipCond builds the option condition for a multi-select
// filter. It always uses the positive form (option_id = X or IN (...)); the
// caller wraps it in NOT EXISTS for negative predicates.
func buildCustomFieldMembershipCond(comparator taskFilterComparator, value interface{}) (builder.Cond, error) {
	col := "custom_field_value_options.option_id"
	switch comparator {
	case taskFilterComparatorEquals, taskFilterComparatorNotEquals:
		return &builder.Eq{col: value}, nil
	case taskFilterComparatorIn, taskFilterComparatorNotIn:
		return builder.In(col, value), nil
	case taskFilterComparatorGreater, taskFilterComparatorGreateEquals,
		taskFilterComparatorLess, taskFilterComparatorLessEquals,
		taskFilterComparatorLike, taskFilterComparatorIsNull, taskFilterComparatorIsNotNull,
		taskFilterComparatorInvalid:
		return nil, ErrInvalidTaskFilterValue{Field: col, Value: string(comparator)}
	}
	return nil, nil
}

// buildCustomFieldValueCond maps a comparator to a condition on a typed column.
// The like comparator escapes wildcard characters as data, per the wiki.
func buildCustomFieldValueCond(col string, comparator taskFilterComparator, value interface{}) (builder.Cond, error) {
	switch comparator {
	case taskFilterComparatorEquals:
		return &builder.Eq{col: value}, nil
	case taskFilterComparatorNotEquals:
		return &builder.Neq{col: value}, nil
	case taskFilterComparatorGreater:
		return &builder.Gt{col: value}, nil
	case taskFilterComparatorGreateEquals:
		return &builder.Gte{col: value}, nil
	case taskFilterComparatorLess:
		return &builder.Lt{col: value}, nil
	case taskFilterComparatorLessEquals:
		return &builder.Lte{col: value}, nil
	case taskFilterComparatorLike:
		s, is := value.(string)
		if !is {
			return nil, ErrInvalidTaskFilterValue{Field: col, Value: value}
		}
		return escapedLikeCond{column: col, value: "%" + escapeLikeWildcards(s) + "%"}, nil
	case taskFilterComparatorIn:
		return builder.In(col, value), nil
	case taskFilterComparatorNotIn:
		return builder.NotIn(col, value), nil
	case taskFilterComparatorInvalid, taskFilterComparatorIsNull, taskFilterComparatorIsNotNull:
		return nil, ErrInvalidTaskFilterValue{Field: col, Value: string(comparator)}
	}
	return nil, nil
}

type escapedLikeCond struct {
	column string
	value  string
}

type columnEqualsCond struct {
	left  string
	right string
}

func (c columnEqualsCond) WriteTo(w builder.Writer) error {
	_, err := fmt.Fprintf(w, "%s = %s", c.left, c.right)
	return err
}

func (c columnEqualsCond) And(conds ...builder.Cond) builder.Cond {
	return builder.And(c, builder.And(conds...))
}

func (c columnEqualsCond) Or(conds ...builder.Cond) builder.Cond {
	return builder.Or(c, builder.Or(conds...))
}

func (c columnEqualsCond) IsValid() bool {
	return c.left != "" && c.right != ""
}

func (c escapedLikeCond) WriteTo(w builder.Writer) error {
	if _, err := fmt.Fprintf(w, "%s LIKE ? ESCAPE '!'", c.column); err != nil {
		return err
	}
	w.Append(c.value)
	return nil
}

func (c escapedLikeCond) And(conds ...builder.Cond) builder.Cond {
	return builder.And(c, builder.And(conds...))
}

func (c escapedLikeCond) Or(conds ...builder.Cond) builder.Cond {
	return builder.Or(c, builder.Or(conds...))
}

func (c escapedLikeCond) IsValid() bool {
	return c.column != ""
}

// escapeLikeWildcards escapes the SQL LIKE wildcards so a user's % and _ are
// matched as literal data. The escape character is ! rather than backslash:
// backslash is the default escape in MySQL and needs dialect-specific handling,
// while ! is not special in any of the supported databases and works uniformly
// with an explicit ESCAPE '!' clause.
func escapeLikeWildcards(s string) string {
	s = strings.ReplaceAll(s, "!", "!!")
	s = strings.ReplaceAll(s, "%", "!%")
	s = strings.ReplaceAll(s, "_", "!_")
	return s
}
