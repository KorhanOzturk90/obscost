package main

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// decodeFields splits one protobuf message into (field, wire type, payload)
// triples — enough to check the hand-rolled encoder against the wire
// format without depending on a protobuf library.
func decodeFields(t *testing.T, b []byte) [][3]any {
	t.Helper()
	var out [][3]any
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		b = b[n:]
		field, wire := int(tag>>3), int(tag&7)
		switch wire {
		case 0:
			v, n := binary.Uvarint(b)
			b = b[n:]
			out = append(out, [3]any{field, wire, v})
		case 1:
			out = append(out, [3]any{field, wire, binary.LittleEndian.Uint64(b[:8])})
			b = b[8:]
		case 2:
			l, n := binary.Uvarint(b)
			b = b[n:]
			out = append(out, [3]any{field, wire, b[:l]})
			b = b[l:]
		default:
			t.Fatalf("unexpected wire type %d", wire)
		}
	}
	return out
}

func TestEncodeWriteRequest(t *testing.T) {
	ss := []series{{labels: []label{{"__name__", "m"}, {"id", "7"}}, value: 2.5}}
	req := decodeFields(t, encodeWriteRequest(ss, 1234))
	if len(req) != 1 || req[0][0] != 1 {
		t.Fatalf("want one timeseries (field 1), got %v", req)
	}
	ts := decodeFields(t, req[0][2].([]byte))
	if len(ts) != 3 {
		t.Fatalf("want 2 labels + 1 sample, got %d fields", len(ts))
	}
	lbl := decodeFields(t, ts[0][2].([]byte))
	if string(lbl[0][2].([]byte)) != "__name__" || string(lbl[1][2].([]byte)) != "m" {
		t.Errorf("first label = %q=%q", lbl[0][2], lbl[1][2])
	}
	smp := decodeFields(t, ts[2][2].([]byte))
	if ts[2][0] != 2 || math.Float64frombits(smp[0][2].(uint64)) != 2.5 || smp[1][2].(uint64) != 1234 {
		t.Errorf("sample = %v", smp)
	}
}

func TestGenerateRulesOutputCounts(t *testing.T) {
	tn := Tenant{
		Name:   "rule-heavy",
		Series: SeriesSpec{Metrics: 3, PerMetric: 30, Shards: 10, Interval: Duration{15 * time.Second}},
		Rules:  &RuleSpec{Groups: 2, Recording: 3, Alerting: 2, Interval: Duration{30 * time.Second}, Range: Duration{10 * time.Minute}},
	}
	groups, truth := generateRules(tn)
	if len(groups) != 2 || truth.Recording != 6 || truth.Alerting != 4 {
		t.Fatalf("groups %d, truth %+v", len(groups), truth)
	}
	// Recording rules alternate: by-shard (10 series), sum (1).
	if truth.RecordSeriesTotal != 3*10+3*1 {
		t.Errorf("record series total = %d, want 33", truth.RecordSeriesTotal)
	}
	if got := groups[0].Rules[0].Expr; got != "sum by (shard) (rate(rule_heavy_load_m0_total[10m]))" {
		t.Errorf("expr = %q", got)
	}
	if groups[0].Interval != "30s" {
		t.Errorf("interval = %q", groups[0].Interval)
	}
	if truth.EvaluationsPerMinute != 20 {
		t.Errorf("evaluations/min = %v, want 20", truth.EvaluationsPerMinute)
	}
}

func TestBuildSeriesLabelsSortedAndChurnGeneration(t *testing.T) {
	tn := Tenant{Name: "c", Series: SeriesSpec{Metrics: 1, PerMetric: 2, Shards: 2, ExtraLabels: 1}, Churn: &ChurnSpec{}}
	ss := buildSeries(tn, 42)
	for _, s := range ss {
		for i := 1; i < len(s.labels); i++ {
			if s.labels[i-1].name >= s.labels[i].name {
				t.Fatalf("labels not sorted: %v", s.labels)
			}
		}
	}
	found := false
	for _, l := range ss[0].labels {
		found = found || (l.name == "generation" && l.value == "42")
	}
	if !found {
		t.Errorf("no generation label in %v", ss[0].labels)
	}
}
