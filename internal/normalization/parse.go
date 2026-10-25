// Package normalization converts raw DocuSign API response structs into
// normalized structs with Parquet and JSON struct tags.
package normalization

import (
	"time"
)

// docuSignTimeFormats lists the time layouts DocuSign uses in its API responses.
// We try each in order.
var docuSignTimeFormats = []string{
	time.RFC3339,
	"2006-01-02T15:04:05.9999999Z07:00",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
}

// parseTime parses a DocuSign ISO-8601 date-time string into a *time.Time.
// Returns nil if the string is empty or cannot be parsed.
func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range docuSignTimeFormats {
		if t, err := time.Parse(layout, s); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

// strPtr returns a *string pointing to s, or nil if s is empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
