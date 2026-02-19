package eff

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// UUID wraps uuid.UUID for GraphQL scalar marshaling.
type UUID = uuid.UUID

// Date represents a Twisp Date scalar (YYYY-MM-DD).
type Date struct{ time.Time }

func (d *Date) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Time.Format("2006-01-02"))
}

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return fmt.Errorf("invalid Date %q: %w", s, err)
	}
	d.Time = t
	return nil
}

func NewDate(year int, month time.Month, day int) Date {
	return Date{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// Decimal represents a Twisp Decimal scalar as a string to preserve precision.
type Decimal string

func (d Decimal) String() string { return string(d) }

func (d Decimal) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(d))
}

func (d *Decimal) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		// Try as a number
		var n json.Number
		if err2 := json.Unmarshal(b, &n); err2 != nil {
			return fmt.Errorf("invalid Decimal: %w", err)
		}
		*d = Decimal(n.String())
		return nil
	}
	*d = Decimal(s)
	return nil
}

// Timestamp represents a Twisp Timestamp scalar (RFC3339).
type Timestamp struct{ time.Time }

func (t *Timestamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Time.Format(time.RFC3339Nano))
}

func (t *Timestamp) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("invalid Timestamp %q: %w", s, err)
	}
	t.Time = parsed
	return nil
}

// Simple string-based scalars.
type CurrencyCode = string
type EntryType = string
type Expression = string
type InterpolatedExpression = string
type Uint8Array = string

// Map-based scalars.
type ExpressionMap = map[string]string
type ExpressionNestedMap = map[string]interface{}
type JSON = map[string]interface{}
type Value = interface{}
