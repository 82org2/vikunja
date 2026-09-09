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
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/config"

	"github.com/ganigeorgiev/fexpr"
	"github.com/iancoleman/strcase"
	"github.com/jszwedko/go-datemath"
	"xorm.io/xorm/schemas"
)

type taskFilterComparator string

const (
	taskFilterComparatorInvalid taskFilterComparator = "invalid"

	taskFilterComparatorEquals       taskFilterComparator = "="
	taskFilterComparatorGreater      taskFilterComparator = ">"
	taskFilterComparatorGreateEquals taskFilterComparator = ">="
	taskFilterComparatorLess         taskFilterComparator = "<"
	taskFilterComparatorLessEquals   taskFilterComparator = "<="
	taskFilterComparatorNotEquals    taskFilterComparator = "!="
	taskFilterComparatorLike         taskFilterComparator = "like"
	taskFilterComparatorIn           taskFilterComparator = "in"
	taskFilterComparatorNotIn        taskFilterComparator = "not in"
	taskFilterComparatorIsNull       taskFilterComparator = "is null"
	taskFilterComparatorIsNotNull    taskFilterComparator = "is not null"
)

// Guess what you get back if you ask Safari for a rfc 3339 formatted date?
const safariDateAndTime = "2006-01-02 15:04"
const safariDate = "2006-01-02"

type taskFilter struct {
	field      string
	value      interface{} // Needs to be an interface to be able to hold the field's native value
	comparator taskFilterComparator
	isNumeric  bool
	join       taskFilterConcatinator

	// customFieldKey is the machine key of a custom_fields.<key> filter. The
	// value stays a raw string until the definition is resolved in the search
	// step, where customFieldDefs is populated per project.
	customFieldKey  string
	customFieldDefs map[int64]*CustomFieldDefinition
}

// customFieldFilterNamespace prefixes the filter and sort field names that
// address a custom field. Machine keys match ^[a-z][a-z0-9_]{0,63}$, so the
// dot unambiguously separates the namespace from the key.
const customFieldFilterNamespace = "custom_fields."

// clampDateToDriverRange lifts boundaries with year < 1 back into year 1:
// converting the zero-date sentinel to UTC from a timezone east of Greenwich
// underflows into year 0, which the MySQL driver refuses to encode ("year is
// not in the range [1, 9999]"). The extra day for January 1st keeps the
// boundary strictly after the sentinel, so `due_date > 0001-01-01` keeps
// meaning "has a due date".
func clampDateToDriverRange(t time.Time) time.Time {
	if t.Year() >= 1 {
		return t
	}
	t = t.AddDate(1-t.Year(), 0, 0)
	if t.Month() == 1 && t.Day() == 1 {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func parseTimeFromUserInput(timeString string, loc *time.Location) (value time.Time, err error) {
	value, err = time.ParseInLocation(time.RFC3339, timeString, loc)
	if err != nil {
		value, err = time.ParseInLocation(safariDateAndTime, timeString, loc)
	}
	if err != nil {
		value, err = time.ParseInLocation(safariDate, timeString, loc)
	}
	if err != nil {
		// Here we assume a date like 2022-11-1 and try to parse it manually
		parts := strings.Split(timeString, "-")
		if len(parts) < 3 {
			return
		}
		// Assign to the named err return (not :=) so a successful manual parse
		// clears the error from the failed layout attempts above.
		var year, month, day int
		year, err = strconv.Atoi(parts[0])
		if err != nil {
			return value, err
		}
		month, err = strconv.Atoi(parts[1])
		if err != nil {
			return value, err
		}
		day, err = strconv.Atoi(parts[2])
		if err != nil {
			return value, err
		}
		value = time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
	}
	// UTC, not service timezone — see getValueForField.
	value = value.UTC()
	value = clampDateToDriverRange(value)
	return value, err
}

func parseFilterFromExpression(f fexpr.ExprGroup, loc *time.Location, allowCustomFields bool) (filter *taskFilter, err error) {
	filter = &taskFilter{
		join: filterConcatAnd,
	}
	if f.Join == fexpr.JoinOr {
		filter.join = filterConcatOr
	}

	var value string
	var rightType fexpr.TokenType
	switch v := f.Item.(type) {
	case fexpr.Expr:
		filter.field = v.Left.Literal
		value = v.Right.Literal
		rightType = v.Right.Type
		filter.comparator, err = getFilterComparatorFromOp(v.Op)
		if err != nil {
			return
		}
	case []fexpr.ExprGroup:
		values := make([]*taskFilter, 0, len(v))
		for _, expression := range v {
			subfilter, err := parseFilterFromExpression(expression, loc, allowCustomFields)
			if err != nil {
				return nil, err
			}
			values = append(values, subfilter)
		}
		filter.value = values
		return
	}

	// The preprocess step rewrites the explicit `is null` / `is not null`
	// operators to `= <sentinel>` / `!= <sentinel>`. Only that rewrite produces
	// the sentinel as an unquoted identifier; a user value that literally
	// contains it is quoted by the preprocess step and parses as a text token,
	// so it never reaches this branch.
	if rightType == fexpr.TokenIdentifier && value == nullFilterSentinel {
		switch filter.comparator {
		case taskFilterComparatorEquals:
			filter.comparator = taskFilterComparatorIsNull
		case taskFilterComparatorNotEquals:
			filter.comparator = taskFilterComparatorIsNotNull
		default:
			return nil, ErrInvalidTaskFilterValue{Field: filter.field, Value: value}
		}
		value = ""
	}

	err = validateTaskFieldComparator(filter.comparator)
	if err != nil {
		return
	}

	// Cast the field value to its native type
	var reflectValue *reflect.StructField
	if filter.field == "project" {
		filter.field = "project_id"
	}

	err = validateTaskField(filter.field)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(filter.field, customFieldFilterNamespace) {
		if !allowCustomFields {
			return nil, ErrInvalidTaskField{TaskField: filter.field}
		}
		filter.customFieldKey = strings.TrimPrefix(filter.field, customFieldFilterNamespace)
		// The definition's type is unknown here (no DB access), so the value
		// stays a raw string and is cast in the search step after resolution.
		if filter.comparator != taskFilterComparatorIsNull && filter.comparator != taskFilterComparatorIsNotNull {
			filter.value = value
		}
		return filter, nil
	}

	// The null comparators carry no value; casting an empty string would fail.
	if filter.comparator == taskFilterComparatorIsNull || filter.comparator == taskFilterComparatorIsNotNull {
		return filter, nil
	}

	reflectValue, filter.value, err = getNativeValueForTaskField(filter.field, filter.comparator, value, loc)
	if err != nil {
		return nil, ErrInvalidTaskFilterValue{
			Field: filter.field,
			Value: value,
		}
	}
	if reflectValue != nil {
		filter.isNumeric = reflectValue.Type.Kind() == reflect.Int64
	}

	return filter, nil
}

// filterOperatorSigils maps the human filter operators to their fexpr sigil.
// Order matters: " not in " must be matched before " in " so the longer
// operator wins.
var filterOperatorSigils = []struct {
	operator string
	sigil    string
}{
	{" not in ", " " + string(fexpr.SignAnyNeq) + " "},
	{" in ", " " + string(fexpr.SignAnyEq) + " "},
	{" like ", " " + string(fexpr.SignLike) + " "},
}

// quotedRunEnd returns the index just past the quoted string opening at start,
// or -1 if it is never closed. Quoting mirrors fexpr's scanner: both ' and "
// quote, and a backslash escapes whatever follows it.
func quotedRunEnd(filter string, start int) int {
	quote := filter[start]
	for i := start + 1; i < len(filter); i++ {
		switch filter[i] {
		case '\\':
			i++
		case quote:
			return i + 1
		}
	}
	return -1
}

// replaceFilterOperators rewrites the human filter operators to fexpr sigils,
// skipping quoted values so `title like 'stuff in progress'` keeps its text.
// An unclosed quote is treated as an ordinary character, because bare values
// may legitimately contain an apostrophe (`title = it's cool && done = false`).
func replaceFilterOperators(filter string) string {
	var out strings.Builder
	out.Grow(len(filter))

	for i := 0; i < len(filter); {
		if c := filter[i]; c == '\'' || c == '"' {
			if end := quotedRunEnd(filter, i); end > 0 {
				out.WriteString(filter[i:end])
				i = end
				continue
			}
		}

		matched := false
		for _, op := range filterOperatorSigils {
			if strings.HasPrefix(filter[i:], op.operator) {
				out.WriteString(op.sigil)
				i += len(op.operator)
				matched = true
				break
			}
		}
		if !matched {
			out.WriteByte(filter[i])
			i++
		}
	}

	return out.String()
}

// preprocessFilterString rewrites the human filter syntax (in / not in / like /
// is null / is not null) into fexpr sigils and quotes bare values so
// fexpr.Parse accepts them. Shared by every entity that filters with the task
// grammar.
func preprocessFilterString(filter string) string {
	filter = replaceFilterOperators(filter)

	// The field group allows dots so custom_fields.<machine_key> is captured as
	// one field name; fexpr treats '.' as an identifier combine rune.
	re := regexp.MustCompile(`([\w.]+)\s*(>=|<=|!=|~|\?=|\?!=|=|>|<)\s*([^&|()]+)`)
	filter = re.ReplaceAllStringFunc(filter, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}

		field := parts[1]
		comparator := parts[2]
		value := strings.TrimSpace(parts[3])

		// Already quoted — leave as-is
		if (strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) ||
			(strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) {
			return field + " " + comparator + " " + value
		}

		quotedValue := "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
		return field + " " + comparator + " " + quotedValue
	})

	// The is null / is not null rewrite runs after bare values are quoted so
	// the sentinel is always unquoted and user text containing it stays a
	// quoted string.
	return rewriteNullOperators(filter)
}

// nullFilterSentinel is the internal marker produced only by the explicit
// `is null` / `is not null` rewrite. It is never quoted, so fexpr parses it as
// an identifier and parseFilterFromExpression converts it to the null
// comparators. A user value that literally contains the sentinel is quoted by
// the preprocess step and therefore never matches.
const nullFilterSentinel = "__VIKUNJA_NULL__"

// rewriteNullOperators rewrites the explicit `is null` / `is not null`
// operators to `= <sentinel>` / `!= <sentinel>` outside quoted regions, with
// word boundaries so `this is null` inside a value is untouched. The rewrite
// runs after bare values are quoted, so the sentinel is always unquoted.
func rewriteNullOperators(filter string) string {
	var out strings.Builder
	out.Grow(len(filter))

	for i := 0; i < len(filter); {
		if c := filter[i]; c == '\'' || c == '"' {
			if end := quotedRunEnd(filter, i); end > 0 {
				out.WriteString(filter[i:end])
				i = end
				continue
			}
		}

		// Match "is not null" before "is null" so the longer operator wins.
		matched := false
		for _, op := range []struct {
			literal string
			repl    string
		}{
			{"is not null", "!= " + nullFilterSentinel},
			{"is null", "= " + nullFilterSentinel},
		} {
			if !strings.HasPrefix(filter[i:], op.literal) {
				continue
			}
			beforeOK := i == 0 || !isFilterWordRune(filter[i-1])
			after := i + len(op.literal)
			afterOK := after >= len(filter) || !isFilterWordRune(filter[after])
			if beforeOK && afterOK {
				out.WriteString(op.repl)
				i = after
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		out.WriteByte(filter[i])
		i++
	}

	return out.String()
}

// isFilterWordRune reports whether c can continue a filter identifier or value
// word, used to bound the is null / is not null rewrite to whole words.
func isFilterWordRune(c byte) bool {
	return c == '_' || c == '.' || c == ':' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// These caps block GHSA-xxc3-xpmc-vmvr before fexpr allocates while leaving
// realistic hand-written filters unaffected.
const (
	maxFilterBytes = 16 * 1024
	maxFilterDepth = 100
)

func validateFilterComplexity(filter string) error {
	if len(filter) > maxFilterBytes {
		return &ErrFilterTooComplex{Reason: fmt.Sprintf("it exceeds the %d-byte limit", maxFilterBytes)}
	}

	depth := 0
	for i := 0; i < len(filter); {
		switch c := filter[i]; c {
		case '\'', '"':
			if end := quotedRunEnd(filter, i); end > 0 {
				i = end
				continue
			}
			i++
		case '(':
			depth++
			if depth > maxFilterDepth {
				return &ErrFilterTooComplex{Reason: fmt.Sprintf("it exceeds the %d-level nesting limit", maxFilterDepth)}
			}
			i++
		case ')':
			depth--
			if depth < 0 {
				return &ErrInvalidFilterExpression{
					Expression:      filter,
					ExpressionError: errors.New("unexpected closing parenthesis"),
				}
			}
			i++
		default:
			i++
		}
	}

	return nil
}

func prepareFilterForParsing(filter string) (string, error) {
	// Preprocessing may shrink or expand input, so enforce the cap on both forms.
	if err := validateFilterComplexity(filter); err != nil {
		return "", err
	}

	filter = preprocessFilterString(filter)
	if err := validateFilterComplexity(filter); err != nil {
		return "", err
	}

	return filter, nil
}

// getTaskFiltersFromFilterString parses the filter grammar into taskFilter
// structs. allowCustomFields gates the custom_fields.<key> namespace: v1 task
// collections pass false so the shared parser does not enable custom-field
// filtering on frozen v1 routes; saved filters, project views, and re-parses
// pass true.
func getTaskFiltersFromFilterString(filter string, filterTimezone string, allowCustomFields bool) (filters []*taskFilter, err error) {

	if filter == "" {
		return
	}

	filter, err = prepareFilterForParsing(filter)
	if err != nil {
		return nil, err
	}

	parsedFilter, err := fexpr.Parse(filter)
	if err != nil {
		return nil, &ErrInvalidFilterExpression{
			Expression:      filter,
			ExpressionError: err,
		}
	}

	var loc *time.Location
	if filterTimezone != "" {
		loc, err = time.LoadLocation(filterTimezone)
		if err != nil {
			return nil, &ErrInvalidTimezone{
				Name:      filterTimezone,
				LoadError: err,
			}
		}
	}

	filters = make([]*taskFilter, 0, len(parsedFilter))
	for _, f := range parsedFilter {
		parsedFilter, err := parseFilterFromExpression(f, loc, allowCustomFields)
		if err != nil {
			return nil, err
		}
		filters = append(filters, parsedFilter)
	}

	return
}

func isErrInvalidFilter(err error) bool {
	return IsErrInvalidFilterExpression(err) ||
		IsErrFilterTooComplex(err) ||
		IsErrInvalidTaskFilterValue(err) ||
		IsErrInvalidTaskFilterConcatinator(err) ||
		IsErrInvalidTaskFilterComparator(err) ||
		IsErrInvalidTaskField(err) ||
		IsErrInvalidTimezone(err)
}

func validateTaskFieldComparator(comparator taskFilterComparator) error {
	switch comparator {
	case
		taskFilterComparatorEquals,
		taskFilterComparatorGreater,
		taskFilterComparatorGreateEquals,
		taskFilterComparatorLess,
		taskFilterComparatorLessEquals,
		taskFilterComparatorNotEquals,
		taskFilterComparatorLike,
		taskFilterComparatorIn,
		taskFilterComparatorNotIn,
		taskFilterComparatorIsNull,
		taskFilterComparatorIsNotNull:
		return nil
	case taskFilterComparatorInvalid:
		fallthrough
	default:
		return ErrInvalidTaskFilterComparator{Comparator: comparator}
	}
}

func getFilterComparatorFromOp(op fexpr.SignOp) (taskFilterComparator, error) {
	switch op {
	case fexpr.SignEq:
		return taskFilterComparatorEquals, nil
	case fexpr.SignGt:
		return taskFilterComparatorGreater, nil
	case fexpr.SignGte:
		return taskFilterComparatorGreateEquals, nil
	case fexpr.SignLt:
		return taskFilterComparatorLess, nil
	case fexpr.SignLte:
		return taskFilterComparatorLessEquals, nil
	case fexpr.SignNeq:
		return taskFilterComparatorNotEquals, nil
	case fexpr.SignLike:
		return taskFilterComparatorLike, nil
	case fexpr.SignAnyEq:
		fallthrough
	case "in":
		return taskFilterComparatorIn, nil
	case fexpr.SignAnyNeq:
		fallthrough
	case "not in":
		return taskFilterComparatorNotIn, nil
	default:
		return taskFilterComparatorInvalid, ErrInvalidTaskFilterComparator{Comparator: taskFilterComparator(op)}
	}
}

// safeDatemathParse wraps datemath.Parse with a recover to catch panics from
// the datemath lexer when given malformed input (e.g. "no" triggers a
// "scanner internal error" panic in the generated lexer).
func safeDatemathParse(s string) (expr datemath.Expression, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("datemath parse error: %v", r)
		}
	}()
	return datemath.Parse(s)
}

func getValueForField(field reflect.StructField, rawValue string, loc *time.Location) (value interface{}, err error) {

	if loc == nil {
		loc = config.GetTimeZone()
	}

	rawValue = strings.TrimSpace(rawValue)

	switch field.Type.Kind() {
	case reflect.Int64:
		value, err = strconv.ParseInt(rawValue, 10, 64)
	case reflect.Float64:
		value, err = strconv.ParseFloat(rawValue, 64)
	case reflect.String:
		value = rawValue
	case reflect.Bool:
		value, err = strconv.ParseBool(rawValue)
	case reflect.Struct:
		if field.Type == schemas.TimeType {
			var t datemath.Expression
			var tt time.Time
			t, err = safeDatemathParse(rawValue)
			if err == nil {
				// UTC, not service timezone: due dates live in a naive UTC column and
				// the driver drops a bound parameter's offset, so a non-UTC wall clock
				// shifts the boundary. loc still controls how the datemath rounds.
				tt = t.Time(datemath.WithLocation(loc)).UTC()
				tt = clampDateToDriverRange(tt)
			} else {
				tt, err = parseTimeFromUserInput(rawValue, loc)
			}
			if err != nil {
				return
			}
			value = tt
		}
	case reflect.Slice:
		// If this is a slice of pointers we're dealing with some property which is a relation
		// In that case we don't really care about what the actual type is, we just cast the value to an
		// int64 since we need the id - yes, this assumes we only ever have int64 IDs, but this is fine.
		if field.Type.Elem().Kind() == reflect.Pointer {
			value, err = strconv.ParseInt(strings.TrimSpace(rawValue), 10, 64)
			return
		}

		// There are probably better ways to do this - please let me know if you have one.
		if field.Type.Elem().String() == "time.Time" {
			value, err = time.Parse(time.RFC3339, rawValue)
			value = value.(time.Time).In(config.GetTimeZone())
			return
		}
		fallthrough
	default:
		panic(fmt.Errorf("unrecognized filter type %s for field %s, value %s", field.Type.String(), field.Name, value))
	}

	return
}

func getNativeValueForTaskField(fieldName string, comparator taskFilterComparator, value string, loc *time.Location) (reflectField *reflect.StructField, nativeValue interface{}, err error) {

	realFieldName := strings.ReplaceAll(strcase.ToCamel(fieldName), "Id", "ID")

	if realFieldName == "Assignees" || realFieldName == "CreatedBy" {
		vals := strings.Split(value, ",")
		valueSlice := make([]string, 0, len(vals))
		for _, val := range vals {
			val = strings.TrimSpace(val)
			if val == "" {
				continue
			}
			valueSlice = append(valueSlice, val)
		}
		return nil, valueSlice, nil
	}
	if realFieldName == "ParentProject" || realFieldName == "ParentProjectID" {
		if comparator == taskFilterComparatorIn || comparator == taskFilterComparatorNotIn {
			vals := strings.Split(value, ",")
			valueSlice := make([]interface{}, 0, len(vals))
			for _, val := range vals {
				parsed, parseErr := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
				if parseErr != nil {
					return nil, nil, parseErr
				}
				valueSlice = append(valueSlice, parsed)
			}
			return nil, valueSlice, nil
		}
		parsed, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return nil, parsed, parseErr
	}

	field, ok := reflect.TypeOf(&Task{}).Elem().FieldByName(realFieldName)
	if !ok {
		return nil, nil, ErrInvalidTaskField{TaskField: fieldName}
	}

	if realFieldName == "Reminders" {
		field, ok = reflect.TypeOf(&TaskReminder{}).Elem().FieldByName("Reminder")
		if !ok {
			return nil, nil, ErrInvalidTaskField{TaskField: fieldName}
		}
	}

	if comparator == taskFilterComparatorIn || comparator == taskFilterComparatorNotIn {
		vals := strings.Split(value, ",")
		valueSlice := []interface{}{}
		for _, val := range vals {
			v, err := getValueForField(field, val, loc)
			if err != nil {
				return nil, nil, err
			}
			valueSlice = append(valueSlice, v)
		}
		return nil, valueSlice, nil
	}

	val, err := getValueForField(field, value, loc)
	return &field, val, err
}
