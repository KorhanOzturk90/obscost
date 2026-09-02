package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func TestJSONReportRoundTrip(t *testing.T) {
	result := sampleResult()

	var buf bytes.Buffer
	if err := (jsonReporter{}).Render(&buf, result); err != nil {
		t.Fatalf("Render: %v", err)
	}

	var decoded Result
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.RulesScanned != result.RulesScanned {
		t.Errorf("RulesScanned = %d, want %d", decoded.RulesScanned, result.RulesScanned)
	}
	if len(decoded.Findings) != len(result.Findings) {
		t.Fatalf("len(Findings) = %d, want %d", len(decoded.Findings), len(result.Findings))
	}
	if decoded.Findings[0].Severity != rule.SeverityError {
		t.Errorf("Findings[0].Severity = %v, want error", decoded.Findings[0].Severity)
	}
	if !decoded.GeneratedAt.Equal(result.GeneratedAt) {
		t.Errorf("GeneratedAt = %v, want %v", decoded.GeneratedAt, result.GeneratedAt)
	}
}
