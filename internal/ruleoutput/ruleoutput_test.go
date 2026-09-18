package ruleoutput

import (
	"strings"
	"testing"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/cost"
	"github.com/KorhanOzturk90/obscost/internal/rule"
)

func rec(tenant, file, group, record string) rule.AnnotatedRule {
	return rule.AnnotatedRule{
		Rule:     rule.Rule{Kind: rule.KindRecording, Record: record, Expr: "vector(1)"},
		Group:    rule.RuleGroupMeta{Name: group},
		Tenant:   tenant,
		Location: rule.SourceLocation{File: file, Group: group, Rule: record},
	}
}

func alert(tenant, file, group, name string) rule.AnnotatedRule {
	return rule.AnnotatedRule{
		Rule:     rule.Rule{Kind: rule.KindAlerting, Alert: name, Expr: "up == 0"},
		Group:    rule.RuleGroupMeta{Name: group},
		Tenant:   tenant,
		Location: rule.SourceLocation{File: file, Group: group, Rule: name},
	}
}

func n(v int64) *int64 { return &v }

var at = time.Date(2026, 9, 19, 3, 0, 0, 0, time.UTC)

// rf3Pool is pool 1 on an RF=3 cluster: raw active series are three times
// the real, deduplicated counts (infra 6,000; payments 1,000).
func rf3Pool(t *testing.T) cost.PoolAllocation {
	t.Helper()
	rf := 3.0
	return cost.Allocate(cost.Measurement{
		Pool:              cost.IngesterMemory,
		Window:            time.Hour,
		End:               at,
		Source:            "test",
		DriverQuery:       "q",
		Drivers:           map[string]float64{"infra": 18000, "payments": 3000},
		ReplicationFactor: &rf,
		RFQuery:           "max(max_over_time(cortex_distributor_replication_factor[1h]))",
	}, cost.Inventory{}, nil)
}

func TestJoin_StatusesAndRFDividedShare(t *testing.T) {
	rules := []rule.AnnotatedRule{
		rec("infra", "ns", "g1", "instance:cpu:sum"),
		rec("infra", "ns", "g1", "never:sum"),
		rec("infra", "ns", "g2", "broken:sum"),
		alert("infra", "ns", "alerts", "InstanceDown"),
		rec("payments", "pay", "g", "job:up:sum"),
	}
	counts := Counts{
		At:         at,
		QueryShape: CountQueryShape,
		Queries:    2,
		Tenants: map[string]map[string]MetricCount{
			"infra": {
				"instance:cpu:sum": {Series: n(600)},
				"never:sum":        {Series: n(0)},
				"broken:sum":       {Err: "unexpected status 422: too many series"},
			},
			"payments": {"job:up:sum": {Series: n(50)}},
		},
	}
	denom := DenominatorFromPool(rf3Pool(t))
	r := Join(rules, counts, &denom)

	if len(r.Tenants) != 2 || r.Tenants[0].Tenant != "infra" {
		t.Fatalf("tenants = %+v, want infra first (more output series)", r.Tenants)
	}
	infra := r.Tenants[0]
	if infra.RecordingRules != 3 || infra.AlertingRules != 1 || infra.OutputMetrics != 3 {
		t.Errorf("infra counts = %+v", infra)
	}
	byRule := map[string]RuleOutput{}
	for _, ro := range infra.Rules {
		byRule[ro.Rule] = ro
	}

	cpu := byRule["instance:cpu:sum"]
	if cpu.Status != StatusSeries || cpu.Series == nil || *cpu.Series != 600 {
		t.Errorf("cpu = %+v", cpu)
	}
	// 600 / (18000 raw / RF 3) = 10%. Against the raw sum it would be 3.3%.
	if cpu.ShareOfTenantActiveSeries == nil || *cpu.ShareOfTenantActiveSeries != 0.1 {
		t.Errorf("cpu share = %v, want 0.1 (divided by RF 3)", cpu.ShareOfTenantActiveSeries)
	}

	never := byRule["never:sum"]
	if never.Status != StatusNoSeries || never.Series == nil || *never.Series != 0 {
		t.Errorf("never = %+v, want a measured zero", never)
	}

	broken := byRule["broken:sum"]
	if broken.Status != StatusNotMeasured || broken.Series != nil || broken.ShareOfTenantActiveSeries != nil {
		t.Errorf("broken = %+v, want not measured with nil series and share", broken)
	}
	if !strings.Contains(broken.Reason, "422") {
		t.Errorf("broken reason = %q", broken.Reason)
	}

	down := byRule["InstanceDown"]
	if down.Status != StatusNoOutput || down.Series != nil || down.ShareOfTenantActiveSeries != nil || down.OutputMetric != "" {
		t.Errorf("alerting rule = %+v, want no_output with no series figure at all (never zero)", down)
	}

	// Tenant total: 600 + 0, with broken:sum unmeasured, so a lower bound.
	if infra.OutputSeries == nil || *infra.OutputSeries != 600 || infra.OutputSeriesComplete {
		t.Errorf("infra total = %v complete=%v, want 600 and incomplete", infra.OutputSeries, infra.OutputSeriesComplete)
	}
	if infra.ActiveSeries == nil || *infra.ActiveSeries != 6000 {
		t.Errorf("infra denominator = %v, want 6000", infra.ActiveSeries)
	}

	// Rows are ordered series, no_series, not_measured, no_output.
	var order []Status
	for _, ro := range infra.Rules {
		order = append(order, ro.Status)
	}
	want := []Status{StatusSeries, StatusNoSeries, StatusNotMeasured, StatusNoOutput}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("row order = %v, want %v", order, want)
			break
		}
	}

	// The trail names the replication factor actually used and its source.
	var rfLine *cost.Assumption
	for i := range r.Assumptions {
		if r.Assumptions[i].Name == "replication factor" {
			rfLine = &r.Assumptions[i]
		}
	}
	if rfLine == nil || rfLine.Value != "3" || !strings.Contains(rfLine.Source, "cortex_distributor_replication_factor") {
		t.Errorf("replication factor assumption = %+v", rfLine)
	}
	joined := strings.Join(r.Notes, "\n")
	if !strings.Contains(joined, "lower bounds") {
		t.Errorf("notes should flag the incomplete total: %v", r.Notes)
	}
}

func TestJoin_InventoryRFOverrideIsUsed(t *testing.T) {
	rf := 1.0 // Mimir says 1, the operator knows better
	override := 3
	pool := cost.Allocate(cost.Measurement{
		Pool: cost.IngesterMemory, Window: time.Hour, End: at, Source: "test",
		Drivers:           map[string]float64{"infra": 18000},
		ReplicationFactor: &rf, RFQuery: "rfq",
	}, cost.Inventory{ReplicationFactor: &override}, nil)
	denom := DenominatorFromPool(pool)
	if denom.ReplicationFactor != "3" || denom.RFSource != "promcost.yaml inventory" {
		t.Errorf("denominator RF = %q from %q, want 3 from the inventory", denom.ReplicationFactor, denom.RFSource)
	}
	if denom.PerTenant["infra"] != 6000 {
		t.Errorf("denominator = %v, want 6000", denom.PerTenant["infra"])
	}
}

func TestJoin_UnknownRFWithholdsShareNotCount(t *testing.T) {
	pool := cost.Allocate(cost.Measurement{
		Pool: cost.IngesterMemory, Window: time.Hour, End: at, Source: "test",
		Drivers: map[string]float64{"infra": 18000},
	}, cost.Inventory{}, nil)
	denom := DenominatorFromPool(pool)
	r := Join([]rule.AnnotatedRule{rec("infra", "ns", "g", "a:sum")}, Counts{
		At: at, Tenants: map[string]map[string]MetricCount{"infra": {"a:sum": {Series: n(10)}}},
	}, &denom)
	ro := r.Tenants[0].Rules[0]
	if ro.Series == nil || *ro.Series != 10 {
		t.Errorf("raw count must still be reported, got %+v", ro)
	}
	if ro.ShareOfTenantActiveSeries != nil || r.Tenants[0].ShareOfActiveSeries != nil {
		t.Errorf("share must be withheld without a replication factor: a deduplicated count over a raw replica sum is wrong by the RF")
	}
	if !strings.Contains(strings.Join(r.Notes, " "), "no replication factor") {
		t.Errorf("notes should say why the share is withheld: %v", r.Notes)
	}
}

func TestJoin_SharedRecordNameCountedOnce(t *testing.T) {
	rules := []rule.AnnotatedRule{
		rec("infra", "a.yaml", "g1", "job:up:sum"),
		rec("infra", "b.yaml", "g2", "job:up:sum"),
	}
	r := Join(rules, Counts{At: at, Tenants: map[string]map[string]MetricCount{
		"infra": {"job:up:sum": {Series: n(40)}},
	}}, nil)
	infra := r.Tenants[0]
	if infra.OutputMetrics != 1 || *infra.OutputSeries != 40 || !infra.OutputSeriesComplete {
		t.Errorf("shared metric must be counted once: %+v", infra)
	}
	for _, ro := range infra.Rules {
		if len(ro.SharedWith) != 1 {
			t.Errorf("%s/%s SharedWith = %v, want the other writer", ro.Namespace, ro.Group, ro.SharedWith)
		}
	}
	if infra.ActiveSeries != nil || infra.ShareOfActiveSeries != nil {
		t.Errorf("no denominator was given, so no share: %+v", infra)
	}
}

func TestJoin_RuleWithoutTenantIsNotMeasured(t *testing.T) {
	r := Join([]rule.AnnotatedRule{rec("", "x.yaml", "g", "a:sum")}, Counts{At: at}, nil)
	ro := r.Tenants[0].Rules[0]
	if ro.Status != StatusNotMeasured || ro.Series != nil {
		t.Errorf("got %+v", ro)
	}
	if w := Wanted([]rule.AnnotatedRule{rec("", "x.yaml", "g", "a:sum"), alert("infra", "x", "g", "A")}); len(w) != 0 {
		t.Errorf("Wanted should skip tenantless and alerting rules, got %v", w)
	}
}
