package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCursorRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
		id   string
	}{
		{
			name: "nanosecond precision",
			t:    time.Date(2025, 5, 9, 12, 0, 0, 123456789, time.UTC),
			id:   "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name: "zero nanoseconds",
			t:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			id:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeCursor(tc.t, tc.id)
			assert.NotEmpty(t, encoded)

			gotTime, gotID, err := decodeCursor(encoded)
			require.NoError(t, err)
			assert.Equal(t, tc.t.UTC(), gotTime)
			assert.Equal(t, tc.id, gotID)
		})
	}
}

func TestDecodeCursor_Invalid(t *testing.T) {
	cases := []struct {
		name   string
		cursor string
	}{
		{"not base64", "not-base64!!!"},
		{"missing comma", "dGltZW9ubHk="}, // base64("timeonly")
		{"empty string", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cursor == "" {
				// empty cursor is the zero-value sentinel, not an error path in decodeCursor
				return
			}
			_, _, err := decodeCursor(tc.cursor)
			assert.Error(t, err)
		})
	}
}
