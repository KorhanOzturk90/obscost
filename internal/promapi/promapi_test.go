package promapi

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

func TestDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{15 * time.Minute, "15m"},
		{time.Hour, "1h"},
		{7 * 24 * time.Hour, "7d"},
		{24 * time.Hour, "1d"}, // an exact day renders as a day, not 24h
		{90 * time.Second, "90s"},
		{30 * 24 * time.Hour, "30d"},
		{36 * time.Hour, "36h"}, // not a whole day, so it drops one rung
		{5 * time.Minute, "5m"},
		{1500 * time.Millisecond, "1500ms"},
		{time.Millisecond, "1ms"},
		{1_500_500 * time.Nanosecond, "1ms"}, // truncated to PromQL's finest unit
	}

	p := parser.NewParser(parser.Options{})
	for _, tt := range tests {
		t.Run(tt.in.String(), func(t *testing.T) {
			got, err := Duration(tt.in)
			if err != nil {
				t.Fatalf("Duration(%v): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("Duration(%v) = %q, want %q", tt.in, got, tt.want)
			}
			// The point of this formatter is producing something Mimir
			// will accept, so check that with the real PromQL parser
			// rather than trusting the string.
			if _, err := p.ParseExpr(fmt.Sprintf("up[%s]", got)); err != nil {
				t.Errorf("up[%s] is not valid PromQL: %v", got, err)
			}
		})
	}
}

func TestDuration_RejectsNonPositive(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Hour, 500 * time.Nanosecond} {
		if _, err := Duration(d); err == nil {
			t.Errorf("Duration(%v) succeeded, want an error", d)
		}
	}
}

// TestDurationStringIsNotUsable records why Duration exists at all:
// time.Duration.String() renders a week without ever mentioning a week,
// and pads an exact hour with zero terms. Both are the strings an operator
// would have to read back out of a failing query.
func TestDurationStringIsNotUsable(t *testing.T) {
	if got := (7 * 24 * time.Hour).String(); got != "168h0m0s" {
		t.Fatalf("(7d).String() = %q; this test's premise has changed", got)
	}
	if got := time.Hour.String(); got != "1h0m0s" {
		t.Fatalf("(1h).String() = %q; this test's premise has changed", got)
	}

	// Checked rather than assumed, because it decides what the formatter
	// is actually for: PromQL does accept the padded form, so this is a
	// legibility problem, not a correctness one...
	p := parser.NewParser(parser.Options{})
	if _, err := p.ParseExpr("up[168h0m0s]"); err != nil {
		t.Errorf("up[168h0m0s] unexpectedly failed to parse (%v); Duration's doc comment says it parses but reads badly", err)
	}
	// ...except for fractions, where Duration.String() is genuinely
	// invalid PromQL — which is why Duration truncates to whole
	// milliseconds instead of ever emitting a decimal point.
	if _, err := p.ParseExpr("up[1.5s]"); err == nil {
		t.Error("up[1.5s] parsed; Duration truncates on the premise that fractional units are rejected")
	}
}
