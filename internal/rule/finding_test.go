package rule

import (
	"encoding/json"
	"testing"
)

func TestSeverityOrdering(t *testing.T) {
	if SeverityInfo >= SeverityWarn || SeverityWarn >= SeverityError {
		t.Fatalf("severity ordering broken: info=%d warn=%d error=%d", SeverityInfo, SeverityWarn, SeverityError)
	}
}

func TestParseSeverity(t *testing.T) {
	cases := map[string]Severity{
		"info":  SeverityInfo,
		"warn":  SeverityWarn,
		"error": SeverityError,
	}
	for in, want := range cases {
		got, err := ParseSeverity(in)
		if err != nil {
			t.Fatalf("ParseSeverity(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ParseSeverity(%q) = %v, want %v", in, got, want)
		}
	}

	if _, err := ParseSeverity("bogus"); err == nil {
		t.Fatal("ParseSeverity(\"bogus\") expected error, got nil")
	}
}

func TestSeverityJSONRoundTrip(t *testing.T) {
	for _, sev := range []Severity{SeverityInfo, SeverityWarn, SeverityError} {
		b, err := json.Marshal(sev)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", sev, err)
		}
		var got Severity
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("Unmarshal(%s): %v", b, err)
		}
		if got != sev {
			t.Errorf("round trip: got %v, want %v", got, sev)
		}
	}
}

func TestFindingJSONHasStringSeverity(t *testing.T) {
	f := Finding{CheckID: "PC-S01", Severity: SeverityWarn, Message: "test"}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m["severity"] != "warn" {
		t.Errorf("severity field = %v, want \"warn\"", m["severity"])
	}
}
