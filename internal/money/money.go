// Package money carries wallet amounts exactly, from the wire to the ledger and
// back. The ledger stores NUMERIC(18,4); an Amount is a decimal all the way
// through, and values that column cannot hold exactly are rejected rather than
// rounded.
package money

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

const Scale = 4

var Max = decimal.RequireFromString("99999999999999.9999")

var (
	ErrNotPositive = errors.New("amount must be greater than zero")
	ErrScale       = fmt.Errorf("amount must not be finer than %d decimal places", Scale)
	ErrTooLarge    = fmt.Errorf("amount must not exceed %s", Max)
)

type Amount struct {
	d decimal.Decimal
}

func Parse(s string) (Amount, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Amount{}, fmt.Errorf("amount %q is not a decimal number", s)
	}
	return Amount{d}, nil
}

func MustParse(s string) Amount {
	a, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return a
}

func (a Amount) Validate() error {
	switch {
	case a.d.Sign() <= 0:
		return ErrNotPositive
	case !a.d.Truncate(Scale).Equal(a.d):
		return ErrScale
	case a.d.GreaterThan(Max):
		return ErrTooLarge
	}
	return nil
}

func (a Amount) String() string {
	return a.d.StringFixed(Scale)
}

func (a Amount) Equal(b Amount) bool {
	return a.d.Equal(b.d)
}

func (a Amount) Add(b Amount) Amount {
	return Amount{a.d.Add(b.d)}
}

func (a Amount) IsZero() bool {
	return a.d.IsZero()
}

// Text, which PostgreSQL parses into NUMERIC exactly: no amount reaches the
// database as a float.
func (a Amount) Value() (driver.Value, error) {
	return a.String(), nil
}

func (a *Amount) Scan(src any) error {
	return a.d.Scan(src)
}

func (a Amount) MarshalJSON() ([]byte, error) {
	return []byte(a.String()), nil
}

// Read the literal the client sent, not the nearest float to it.
func (a *Amount) UnmarshalJSON(data []byte) error {
	parsed, err := Parse(strings.Trim(string(data), `"`))
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}
