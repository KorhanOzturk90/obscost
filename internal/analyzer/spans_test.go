package analyzer_test

import (
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/analyzer"
)

func TestRangeSpansMatrixSelector(t *testing.T) {
	expr := mustParse(t, "rate(http_requests_total[5m])")
	spans := analyzer.RangeSpans(expr)
	if len(spans) != 1 {
		t.Fatalf("len(spans) = %d, want 1: %+v", len(spans), spans)
	}
	if spans[0].Range != 5*time.Minute {
		t.Errorf("Range = %v, want 5m", spans[0].Range)
	}
	if spans[0].IsSubquery() {
		t.Error("plain matrix selector reported as subquery")
	}
}

func TestRangeSpansSubquery(t *testing.T) {
	expr := mustParse(t, "avg_over_time((sum(rate(m[5m])))[30d:5m])")
	spans := analyzer.RangeSpans(expr)

	var subquery, matrix *analyzer.Span
	for i := range spans {
		s := spans[i]
		if s.IsSubquery() {
			subquery = &s
		} else {
			matrix = &s
		}
	}
	if subquery == nil {
		t.Fatal("no subquery span found")
	}
	if subquery.Range != 30*24*time.Hour {
		t.Errorf("subquery Range = %v, want 720h", subquery.Range)
	}
	if subquery.Step != 5*time.Minute {
		t.Errorf("subquery Step = %v, want 5m", subquery.Step)
	}
	if matrix == nil {
		t.Fatal("no inner matrix selector span found")
	}
	if matrix.Range != 5*time.Minute {
		t.Errorf("inner matrix Range = %v, want 5m", matrix.Range)
	}
}

func TestRangeSpansOmittedStep(t *testing.T) {
	expr := mustParse(t, "avg_over_time(m[1h:])")
	spans := analyzer.RangeSpans(expr)
	if len(spans) != 1 || !spans[0].IsSubquery() {
		t.Fatalf("spans = %+v, want one subquery span", spans)
	}
	if spans[0].Step != 0 {
		t.Errorf("Step = %v, want 0 (omitted)", spans[0].Step)
	}
}
