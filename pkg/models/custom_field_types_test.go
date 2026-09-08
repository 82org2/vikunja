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
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFixedPoint(t *testing.T) {
	type test struct {
		text      string
		precision int
		want      int64
		wantErr   bool
	}
	tests := []test{
		{text: "0", precision: 0, want: 0},
		{text: "42", precision: 0, want: 42},
		{text: "-7", precision: 0, want: -7},
		{text: "1.5", precision: 1, want: 15},
		{text: "-1.5", precision: 1, want: -15},
		{text: "1.50", precision: 2, want: 150},
		{text: "0.05", precision: 2, want: 5},
		{text: "123", precision: 2, want: 12300},
		{text: "1e2", precision: 0, wantErr: true},
		{text: "1,5", precision: 1, wantErr: true},
		{text: "1..5", precision: 2, wantErr: true},
		{text: "", precision: 0, wantErr: true},
		{text: "abc", precision: 0, wantErr: true},
		{text: "1.500", precision: 2, wantErr: true}, // more fraction digits than precision: rejected, not rounded
		{text: "9223372036854775807", precision: 0, want: 9223372036854775807},
		{text: "922337203685477581", precision: 1, wantErr: true}, // fits unscaled, scaled by 10^1 overflows int64
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got, err := parseFixedPoint(tt.text, tt.precision)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatFixedPoint(t *testing.T) {
	tests := []struct {
		v         int64
		precision int
		want      string
	}{
		{v: 42, precision: 0, want: "42"},
		{v: 15, precision: 1, want: "1.5"},
		{v: -15, precision: 1, want: "-1.5"},
		{v: 5, precision: 2, want: "0.05"},
		{v: 12300, precision: 2, want: "123.00"}, // rendered at full field precision
		{v: 150, precision: 2, want: "1.50"},
		{v: math.MinInt64, precision: 0, want: "-9223372036854775808"},
		{v: math.MinInt64, precision: 2, want: "-92233720368547758.08"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, formatFixedPoint(tt.v, tt.precision))
		})
	}
}

func TestCustomFieldNumberJSON(t *testing.T) {
	t.Run("roundtrip keeps literal", func(t *testing.T) {
		var n CustomFieldNumber
		require.NoError(t, json.Unmarshal([]byte(`1.50`), &n))
		require.NoError(t, n.Scale(2))
		out, err := json.Marshal(&n)
		require.NoError(t, err)
		assert.Equal(t, `1.50`, string(out)) // trailing zero survives, not normalised to 1.5
	})

	t.Run("rejects non-number", func(t *testing.T) {
		var n CustomFieldNumber
		require.Error(t, json.Unmarshal([]byte(`"abc"`), &n))
	})

	t.Run("rejects NaN and Infinity", func(t *testing.T) {
		for _, raw := range []string{`NaN`, `Infinity`, `-Infinity`} {
			var n CustomFieldNumber
			require.Error(t, json.Unmarshal([]byte(raw), &n), raw)
		}
	})

	t.Run("too precise for definition", func(t *testing.T) {
		var n CustomFieldNumber
		require.NoError(t, json.Unmarshal([]byte(`1.234`), &n))
		require.Error(t, n.Scale(2))
	})
}

func TestCustomFieldDate(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		var d CustomFieldDate
		require.NoError(t, json.Unmarshal([]byte(`"2020-02-29"`), &d))
		out, err := json.Marshal(&d)
		require.NoError(t, err)
		assert.Equal(t, `"2020-02-29"`, string(out))
	})

	t.Run("rejects malformed", func(t *testing.T) {
		for _, raw := range []string{`"2020-2-1"`, `"2020-13-01"`, `"2020-00-10"`, `"garbage"`, `123`} {
			var d CustomFieldDate
			require.Error(t, json.Unmarshal([]byte(raw), &d), raw)
		}
	})

	t.Run("day count is timezone free", func(t *testing.T) {
		assert.Equal(t, int64(0), dateToDayCount(dayCountToTime(0)))
		assert.Equal(t, int64(18262), dateToDayCount(dayCountToTime(18262)))
	})
}

func TestValidateCustomFieldKey(t *testing.T) {
	valid := []string{"a", "field", "field_1", "a0b1c2", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"}
	for _, key := range valid {
		t.Run("valid "+key, func(t *testing.T) {
			require.NoError(t, validateCustomFieldKey(key))
		})
	}

	invalid := []string{"", "1leading", "A", "a-b", "a.b", "a b", "über", "with space"}
	for _, key := range invalid {
		t.Run("invalid "+key, func(t *testing.T) {
			require.Error(t, validateCustomFieldKey(key))
		})
	}
}

func boolPtr(b bool) *bool    { return &b }
func int64Ptr(i int64) *int64 { return &i }
func strPtr(s string) *string { return &s }

func TestCustomFieldValueValidate(t *testing.T) {
	t.Run("empty has no value", func(t *testing.T) {
		v := &CustomFieldValue{Type: CustomFieldTypeBoolean}
		require.Error(t, v.validate())
	})

	t.Run("two populated fields", func(t *testing.T) {
		v := &CustomFieldValue{
			Type:      CustomFieldTypeBoolean,
			Boolean:   boolPtr(false),
			ShortText: strPtr("x"),
		}
		require.Error(t, v.validate())
	})

	t.Run("typed value matches type", func(t *testing.T) {
		v := &CustomFieldValue{Type: CustomFieldTypeBoolean, ShortText: strPtr("x")}
		require.Error(t, v.validate())

		v = &CustomFieldValue{Type: CustomFieldTypeNumber, Boolean: boolPtr(true)}
		require.Error(t, v.validate())

		v = &CustomFieldValue{Type: CustomFieldTypeShortText, ShortText: strPtr("")}
		require.NoError(t, v.validate())
	})

	t.Run("unknown type", func(t *testing.T) {
		v := &CustomFieldValue{Type: "nope"}
		require.Error(t, v.validate())
	})

	t.Run("multi select", func(t *testing.T) {
		v := &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: []int64{}}
		require.Error(t, v.validate()) // empty is unset, not a value

		v = &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: []int64{1, 2}}
		require.NoError(t, v.validate())

		v = &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: []int64{1, 1}}
		require.Error(t, v.validate()) // duplicates

		tooMany := make([]int64, 101)
		for i := range tooMany {
			tooMany[i] = int64(i)
		}
		v = &CustomFieldValue{Type: CustomFieldTypeMultiSelect, OptionIDs: tooMany}
		require.Error(t, v.validate())
	})
}
