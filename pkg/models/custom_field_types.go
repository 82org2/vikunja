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
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// CustomFieldType is the immutable type of a custom field definition and the
// value stored under it. It selects which typed column of custom_field_values
// is populated.
type CustomFieldType string

const (
	CustomFieldTypeShortText    CustomFieldType = "short_text"
	CustomFieldTypeLongText     CustomFieldType = "long_text"
	CustomFieldTypeNumber       CustomFieldType = "number"
	CustomFieldTypeBoolean      CustomFieldType = "boolean"
	CustomFieldTypeDate         CustomFieldType = "date"
	CustomFieldTypeDateTime     CustomFieldType = "datetime"
	CustomFieldTypeURL          CustomFieldType = "url"
	CustomFieldTypeSingleSelect CustomFieldType = "single_select"
	CustomFieldTypeMultiSelect  CustomFieldType = "multi_select"
	CustomFieldTypeUser         CustomFieldType = "user"
)

var validCustomFieldTypes = map[CustomFieldType]struct{}{
	CustomFieldTypeShortText:    {},
	CustomFieldTypeLongText:     {},
	CustomFieldTypeNumber:       {},
	CustomFieldTypeBoolean:      {},
	CustomFieldTypeDate:         {},
	CustomFieldTypeDateTime:     {},
	CustomFieldTypeURL:          {},
	CustomFieldTypeSingleSelect: {},
	CustomFieldTypeMultiSelect:  {},
	CustomFieldTypeUser:         {},
}

func (t CustomFieldType) isValid() bool {
	_, ok := validCustomFieldTypes[t]
	return ok
}

// Validate returns ErrInvalidCustomFieldType for types outside the supported enum.
func (t CustomFieldType) Validate() error {
	if !t.isValid() {
		return ErrInvalidCustomFieldType{Type: string(t)}
	}
	return nil
}

// customFieldKeyRegex is the documented machine-key expression: 64 lower-case
// ASCII characters (a-z, 0-9, underscore) starting with a letter. Shared by
// definition and option keys.
var customFieldKeyRegex = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func validateCustomFieldKey(key string) error {
	if !customFieldKeyRegex.MatchString(key) {
		return ErrInvalidCustomFieldKey{Key: key}
	}
	return nil
}

// CustomFieldNumber holds a fixed-point numeric field value. Floats are never
// involved: the definition's precision (0-6) scales a signed integer and the
// JSON form is the exact decimal number (123.45), which keeps SQLite,
// PostgreSQL, and MySQL behaving identically.
type CustomFieldNumber struct {
	// The scaled integer (value * 10^precision) and the definition's precision;
	// authoritative once the value has been parsed against a definition.
	Value     int64
	Precision int

	// raw keeps the exact literal from decoding, because a decoded value does
	// not know its definition's precision yet. Once present it is the source of
	// truth for output, so input like 1.50 round-trips instead of becoming 1.5.
	raw string
}

// SetRaw stores a decimal literal without scaling it. Call Scale once the
// definition's precision is known.
func (n *CustomFieldNumber) SetRaw(raw string) error {
	if raw == "" {
		return ErrInvalidCustomFieldNumber{Message: "The number value is empty."}
	}
	// Validate syntax with the widest allowed precision so an ill-formed literal
	// fails here rather than at marshal time.
	if _, err := parseFixedPoint(raw, 6); err != nil {
		return err
	}
	n.raw = raw
	n.Value = 0
	n.Precision = 0
	return nil
}

// Scale parses the stored literal at the definition's precision, caching the
// scaled integer. It is the same check applied before persisting a value.
func (n *CustomFieldNumber) Scale(precision int) error {
	v, err := parseFixedPoint(n.raw, precision)
	if err != nil {
		return err
	}
	n.Value = v
	n.Precision = precision
	return nil
}

// Schema lets Huma (/api/v2) reflect CustomFieldNumber as a JSON number. The
// custom Marshal/UnmarshalJSON exchange a bare decimal literal, but the Go type
// is a struct — without this Huma would generate an object schema and reject
// the number form clients actually send.
func (*CustomFieldNumber) Schema(_ huma.Registry) *huma.Schema {
	return &huma.Schema{Type: huma.TypeNumber}
}

func (n *CustomFieldNumber) MarshalJSON() ([]byte, error) {
	if n.raw != "" {
		return []byte(n.raw), nil
	}
	return []byte(formatFixedPoint(n.Value, n.Precision)), nil
}

func (n *CustomFieldNumber) UnmarshalJSON(data []byte) error {
	// UseNumber keeps the literal verbatim; json.Number also enforces the JSON
	// number grammar, so NaN/Inf and malformed numbers can never reach raw.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var num json.Number
	if err := dec.Decode(&num); err != nil {
		return ErrInvalidCustomFieldNumber{Message: "The number value is not a valid JSON number."}
	}
	return n.SetRaw(num.String())
}

// parseFixedPoint converts a decimal literal into the integer obtained by
// scaling it by 10^precision. Values with more fractional digits than the
// precision allow are rejected instead of rounded (rounding would silently
// change the stored value), and the scaled result must fit in a signed 64-bit
// integer.
func parseFixedPoint(text string, precision int) (int64, error) {
	if text == "" {
		return 0, ErrInvalidCustomFieldNumber{Message: "The number value is empty."}
	}

	negative := text[0] == '-'
	if negative {
		text = text[1:]
	}
	if text == "" {
		return 0, ErrInvalidCustomFieldNumber{Message: "The number value is invalid."}
	}

	parts := strings.Split(text, ".")
	if len(parts) > 2 {
		return 0, ErrInvalidCustomFieldNumber{Message: "The number value may contain only one decimal point."}
	}
	whole := parts[0]
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if whole == "" && frac == "" {
		return 0, ErrInvalidCustomFieldNumber{Message: "The number value is invalid."}
	}
	for _, r := range whole {
		if r < '0' || r > '9' {
			return 0, ErrInvalidCustomFieldNumber{Message: "The number value may only contain digits and a decimal point."}
		}
	}
	for _, r := range frac {
		if r < '0' || r > '9' {
			return 0, ErrInvalidCustomFieldNumber{Message: "The number value may only contain digits and a decimal point."}
		}
	}

	if len(frac) > precision {
		return 0, ErrInvalidCustomFieldNumber{
			Message: fmt.Sprintf("The number value may have at most %d decimal place(s).", precision),
		}
	}

	// Scale by padding the fraction to the full precision; ParseInt's range
	// check rejects scaled values that no longer fit a signed 64-bit integer.
	padded := frac
	for len(padded) < precision {
		padded += "0"
	}
	scaledStr := whole + padded
	if negative {
		scaledStr = "-" + scaledStr
	}

	v, err := strconv.ParseInt(scaledStr, 10, 64)
	if err != nil {
		return 0, ErrInvalidCustomFieldNumber{Message: "The number value is too large for a 64-bit scaled integer."}
	}
	return v, nil
}

// formatFixedPoint renders a scaled integer back to its decimal literal at the
// given precision, e.g. 12345 with precision 2 becomes "123.45".
func formatFixedPoint(v int64, precision int) string {
	negative := v < 0
	magnitude := uint64(v)
	if negative {
		// Negating math.MinInt64 overflows an int64. Build its magnitude without
		// ever representing the positive value as a signed integer.
		magnitude = uint64(-(v + 1)) + 1
	}
	if precision == 0 {
		s := strconv.FormatUint(magnitude, 10)
		if negative {
			return "-" + s
		}
		return s
	}

	scale := uint64(1)
	for i := 0; i < precision; i++ {
		scale *= 10
	}
	whole := magnitude / scale
	frac := magnitude % scale
	fracStr := strconv.FormatUint(frac, 10)
	for len(fracStr) < precision {
		fracStr = "0" + fracStr
	}
	wholeStr := strconv.FormatUint(whole, 10)
	if negative {
		return "-" + wholeStr + "." + fracStr
	}
	return wholeStr + "." + fracStr
}

// CustomFieldDate holds a date-only field value. It is exchanged as an ISO
// date (YYYY-MM-DD) and stored as a signed day count from 1970-01-01 so no
// timezone arithmetic is involved.
type CustomFieldDate struct {
	// DayCount is the number of days since 1970-01-01 UTC.
	DayCount int64
}

func (d *CustomFieldDate) MarshalJSON() ([]byte, error) {
	return json.Marshal(dayCountToTime(d.DayCount).Format("2006-01-02"))
}

func (d *CustomFieldDate) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return ErrInvalidCustomFieldDate{Value: string(data)}
	}
	return d.parse(s)
}

// Schema lets Huma (/api/v2) reflect CustomFieldDate as a date string: the wire
// form is ISO 2006-01-02 while the Go type holds a day count.
func (*CustomFieldDate) Schema(_ huma.Registry) *huma.Schema {
	return &huma.Schema{Type: huma.TypeString, Format: "date"}
}

func (d *CustomFieldDate) parse(s string) error {
	// Strict ISO shape before time.Parse, which would otherwise accept
	// single-digit months and days.
	if !isoDateRegex.MatchString(s) {
		return ErrInvalidCustomFieldDate{Value: s}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return ErrInvalidCustomFieldDate{Value: s}
	}
	d.DayCount = dateToDayCount(t)
	return nil
}

var isoDateRegex = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// dateToDayCount normalises the time to its UTC calendar day before dividing,
// so any time on a given day maps to the same count.
func dateToDayCount(t time.Time) int64 {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// dayCountToTime returns midnight UTC of the given day; exact because midnight
// UTC is a multiple of a day.
func dayCountToTime(days int64) time.Time {
	return time.Unix(days*86400, 0).UTC()
}

// CustomFieldValue is the discriminated, type-aware shape of a task value and
// of a definition's default value. Exactly one typed field is populated and it
// must match the Type discriminant. It maps onto exactly one typed column of
// custom_field_values (multi-select uses the membership table).
type CustomFieldValue struct {
	Type CustomFieldType `json:"type" doc:"The type of this value. It must match the definition's immutable field type."`

	ShortText      *string            `json:"short_text,omitempty" doc:"Short text value, at most 255 characters."`
	LongText       *string            `json:"long_text,omitempty" doc:"Long text value, at most 65535 bytes."`
	Number         *CustomFieldNumber `json:"number,omitempty" doc:"Fixed-point number value, exact at the definition's precision."`
	Boolean        *bool              `json:"boolean,omitempty" doc:"Boolean value."`
	Date           *CustomFieldDate   `json:"date,omitempty" doc:"Date value in ISO 2006-01-02 form."`
	DateTime       *time.Time         `json:"datetime,omitempty" doc:"Date-time value, a UTC instant exchanged as RFC 3339."`
	URL            *string            `json:"url,omitempty" doc:"URL value, a validated absolute HTTP(S) URL of at most 2048 characters."`
	UserID         *int64             `json:"user_id,omitempty" doc:"User value, an active user visible in the definition's project."`
	SingleOptionID *int64             `json:"single_option_id,omitempty" doc:"Single-select value, an option of the definition."`
	OptionIDs      []int64            `json:"option_ids,omitempty" doc:"Multi-select value, options of the definition. An empty array unsets the field."`
}

// validate checks the discriminated shape: a valid type, exactly one populated
// field, and that field matching the type. Content rules against a definition
// (length limits, URL scheme, number precision, option ownership) are checked
// when the value is set against that definition.
func (v *CustomFieldValue) validate() error {
	if err := v.Type.Validate(); err != nil {
		return err
	}

	// An empty multi-select value represents "unset" on the wire; the setter
	// deletes the row rather than storing it. Everything else must be exactly
	// one typed field.
	if v.Type == CustomFieldTypeMultiSelect && v.OptionIDs != nil && v.populatedFields() == 0 {
		return nil
	}

	if v.populatedFields() != 1 {
		return ErrInvalidCustomFieldValue{Message: "A custom field value must contain exactly one typed value."}
	}

	if err := v.validateTypedField(); err != nil {
		return err
	}

	// Date-time values are exchanged as RFC 3339 and stored as UTC instants;
	// normalising here keeps the stored column and every response in UTC even
	// when a client sends an offset.
	if v.Type == CustomFieldTypeDateTime && v.DateTime != nil {
		utc := v.DateTime.UTC()
		v.DateTime = &utc
	}

	if v.Type == CustomFieldTypeMultiSelect {
		return validateMultiSelectOptionIDs(v.OptionIDs)
	}
	return nil
}

// populatedFields counts how many typed value fields are set. Exactly one must
// be populated; an empty multi-select counts as unset, and a nil pointer is
// distinct from a zero value (e.g. an empty string or false).
func (v *CustomFieldValue) populatedFields() int {
	populated := 0
	if v.ShortText != nil {
		populated++
	}
	if v.LongText != nil {
		populated++
	}
	if v.Number != nil {
		populated++
	}
	if v.Boolean != nil {
		populated++
	}
	if v.Date != nil {
		populated++
	}
	if v.DateTime != nil {
		populated++
	}
	if v.URL != nil {
		populated++
	}
	if v.UserID != nil {
		populated++
	}
	if v.SingleOptionID != nil {
		populated++
	}
	if len(v.OptionIDs) > 0 {
		populated++
	}
	return populated
}

// validateTypedField checks the populated field matches the type discriminant.
// Content rules against a definition (lengths, URL scheme, number precision,
// option ownership) are applied separately in validateValueAgainstDefinition.
func (v *CustomFieldValue) validateTypedField() error {
	switch v.Type {
	case CustomFieldTypeShortText:
		if v.ShortText == nil {
			return valueTypeMismatch(v.Type)
		}
	case CustomFieldTypeLongText:
		if v.LongText == nil {
			return valueTypeMismatch(v.Type)
		}
	case CustomFieldTypeNumber:
		if v.Number == nil {
			return valueTypeMismatch(v.Type)
		}
	case CustomFieldTypeBoolean:
		if v.Boolean == nil {
			return valueTypeMismatch(v.Type)
		}
	case CustomFieldTypeDate:
		if v.Date == nil {
			return valueTypeMismatch(v.Type)
		}
	case CustomFieldTypeDateTime:
		if v.DateTime == nil {
			return valueTypeMismatch(v.Type)
		}
	case CustomFieldTypeURL:
		if v.URL == nil {
			return valueTypeMismatch(v.Type)
		}
	case CustomFieldTypeUser:
		if v.UserID == nil {
			return valueTypeMismatch(v.Type)
		}
	case CustomFieldTypeSingleSelect:
		if v.SingleOptionID == nil {
			return valueTypeMismatch(v.Type)
		}
	case CustomFieldTypeMultiSelect:
		if v.OptionIDs == nil {
			return valueTypeMismatch(v.Type)
		}
	}
	return nil
}

func valueTypeMismatch(t CustomFieldType) error {
	return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The value does not match the field type %q.", string(t))}
}

// validateMultiSelectOptionIDs rejects duplicate and empty option selections;
// an empty array means the field is unset, which deletes the row instead.
func validateMultiSelectOptionIDs(ids []int64) error {
	if len(ids) == 0 {
		return ErrInvalidCustomFieldValue{Message: "A multi-select value must contain at least one option."}
	}
	if len(ids) > 100 {
		return ErrInvalidCustomFieldValue{Message: "A multi-select value may contain at most 100 options."}
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			return ErrInvalidCustomFieldValue{Message: fmt.Sprintf("The option %d is selected more than once.", id)}
		}
		seen[id] = struct{}{}
	}
	return nil
}

// Validate exposes the discriminated-shape check for callers outside the type.
func (v *CustomFieldValue) Validate() error {
	return v.validate()
}

func (v *CustomFieldValue) MarshalJSON() ([]byte, error) {
	if err := v.validate(); err != nil {
		return nil, err
	}
	type wire CustomFieldValue
	return json.Marshal(wire(*v))
}

func (v *CustomFieldValue) UnmarshalJSON(data []byte) error {
	type wire CustomFieldValue
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	val := CustomFieldValue(w)
	if err := val.validate(); err != nil {
		return err
	}
	*v = val
	return nil
}
