package money

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseRejectsNonDecimals(t *testing.T) {
	for _, literal := range []string{"", "abc", "1,00", "NaN", "Infinity", "null", "1e", "0x10"} {
		if a, err := Parse(literal); err == nil {
			t.Errorf("want %q rejected, got %s", literal, a)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		literal string
		want    error
	}{
		{"100", nil},
		{"0.0001", nil},
		{"1.00000", nil},
		{"99999999999999.9999", nil},
		{"0", ErrNotPositive},
		{"-0.5", ErrNotPositive},
		{"0.00006", ErrScale},
		{"0.00004", ErrScale},
		{"100000000000000", ErrTooLarge},
	}

	for _, tt := range tests {
		if err := MustParse(tt.literal).Validate(); !errors.Is(err, tt.want) {
			t.Errorf("Validate(%s) = %v, want %v", tt.literal, err, tt.want)
		}
	}
}

func TestJSONKeepsTheLiteralTheClientSent(t *testing.T) {
	tests := []struct {
		payload string
		want    string
	}{
		{`{"amount": 12345678901234.5678}`, "12345678901234.5678"},
		{`{"amount": 0.1}`, "0.1000"},
		{`{"amount": 150}`, "150.0000"},
		{`{"amount": "0.10"}`, "0.1000"},
		{`{"amount": 99999999999999.9999}`, "99999999999999.9999"},
	}

	for _, tt := range tests {
		var req struct {
			Amount Amount `json:"amount"`
		}
		if err := json.Unmarshal([]byte(tt.payload), &req); err != nil {
			t.Fatalf("unmarshal %s: %v", tt.payload, err)
		}
		if got := req.Amount.String(); got != tt.want {
			t.Errorf("%s decoded to %s, want %s", tt.payload, got, tt.want)
		}
	}
}

func TestMarshalStaysAJSONNumber(t *testing.T) {
	data, err := json.Marshal(struct {
		Balance Amount `json:"balance"`
	}{MustParse("150.5")})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"balance":150.5000}`; string(data) != want {
		t.Errorf("want %s, got %s", want, data)
	}
}

func TestValueAndScanRoundTrip(t *testing.T) {
	sent, err := MustParse("12345678901234.5678").Value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	if want := "12345678901234.5678"; sent != want {
		t.Fatalf("want the ledger to receive %s, got %v", want, sent)
	}

	// NUMERIC comes back from lib/pq as text.
	var read Amount
	if err := read.Scan([]byte(sent.(string))); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !read.Equal(MustParse("12345678901234.5678")) {
		t.Errorf("read back %s", read)
	}
}
