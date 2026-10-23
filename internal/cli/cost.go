package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/cost"
	"github.com/KorhanOzturk90/obscost/internal/cost/mimirdrivers"
	"github.com/KorhanOzturk90/obscost/internal/promapi"
	"github.com/KorhanOzturk90/obscost/internal/report"
	"github.com/KorhanOzturk90/obscost/internal/ruleoutput"
)

// newCostCmd wires `cost`: config.Load -> inventory -> mimirdrivers (pool
// driver values from Mimir's own metrics) -> cost.AllocateAll -> cost
// reporter. It is ADR 0004 build step 1, tenant showback, for ADR 0003's
// pools 1 (ingester memory), 6 (query path) and 7 (ruler CPU).
//
// It is a separate command from `report` rather than a mode of it because
// the two share almost no inputs: `report` joins rule executions against
// rule definitions, `cost` needs neither — only a Mimir URL, the metrics
// tenant, and the operator's inventory.
//
// As with `report`, stdout carries only the rendered report, so
// `--format json | jq` always sees exactly one document.
//
// --rule-outputs adds the one step that does need rule definitions: each
// recording rule joined to the series its record: name writes, counted
// per tenant (issue #37, internal/ruleoutput). It is a drill-down under
// pool 1, not a pool of its own.
func newCostCmd(stdout, stderr io.Writer) *cobra.Command {
	var opts costOptions

	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Split each cost pool (ingester memory, query path, ruler CPU) across tenants by its driver, in replicas and (if priced) currency",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCost(cmd.Context(), stdout, stderr, opts)
		},
	}

	cmd.Flags().StringVar(&opts.metricsTenant, "metrics-tenant", "", "tenant whose TSDB holds Mimir's own scraped metrics (required). NOT a tenant being costed — see internal/telemetry/mimirmetrics")
	cmd.Flags().StringVar(&opts.tenants, "tenant", "", "comma-separated tenants you expect to exist; any Mimir did not measure is reported as \"not measured\" instead of silently missing")
	cmd.Flags().StringVar(&opts.configPath, "config", "", "path to promcost.yaml (backend.url, and the inventory block for replica counts and prices)")
	cmd.Flags().StringVar(&opts.format, "format", "md", "output format: md|json")
	cmd.Flags().StringVar(&opts.since, "since", "", "window to average or total over (e.g. 1h, 24h, 7d); defaults to 1h")
	cmd.Flags().StringVar(&opts.pools, "pool", "", "comma-separated pools to cost: "+poolList()+" (default: all)")
	cmd.Flags().BoolVar(&opts.ruleOutputs, "rule-outputs", false, "also count, per tenant, the series each recording rule writes (its record: name), as a point-in-time figure and a share of the tenant's active series. Rule definitions come from --dir, or from the ruler API for --tenant")
	cmd.Flags().StringVar(&opts.dir, "dir", "", "directory of rule files for --rule-outputs. If omitted, rule definitions are fetched from Mimir's ruler API for --tenant's tenants")
	cmd.Flags().IntVar(&opts.ruleOutputConcurrency, "rule-output-concurrency", 4, "maximum --rule-outputs count queries in flight at once")
	return cmd
}

type costOptions struct {
	metricsTenant string
	tenants       string
	configPath    string
	format        string
	since         string
	pools         string

	ruleOutputs           bool
	dir                   string
	ruleOutputConcurrency int
}

func poolList() string {
	ids := make([]string, len(cost.PoolOrder))
	for i, id := range cost.PoolOrder {
		ids[i] = string(id)
	}
	return strings.Join(ids, ",")
}

// selectPools resolves --pool to pool IDs in cost.PoolOrder, whatever order
// the operator typed them in, so the report reads the same either way.
func selectPools(flag string) ([]cost.PoolID, error) {
	if strings.TrimSpace(flag) == "" {
		return cost.PoolOrder, nil
	}
	want := map[cost.PoolID]bool{}
	for _, raw := range strings.Split(flag, ",") {
		id := cost.PoolID(strings.TrimSpace(raw))
		if id == "" {
			continue
		}
		if !slices.Contains(cost.PoolOrder, id) {
			return nil, fmt.Errorf("unknown --pool %q (known: %s)", id, poolList())
		}
		want[id] = true
	}
	var out []cost.PoolID
	for _, id := range cost.PoolOrder {
		if want[id] {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--pool %q names no pool (known: %s)", flag, poolList())
	}
	return out, nil
}

func runCost(ctx context.Context, stdout, stderr io.Writer, opts costOptions) error {
	window := defaultMetricsWindow
	if opts.since != "" {
		d, err := parseSinceDuration(opts.since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", opts.since, err)
		}
		window = d
	}

	// Resolve the reporter before any network call, so a bad --format
	// fails fast instead of after a potentially heavy 30d query.
	rep, err := report.NewCost(report.Format(opts.format))
	if err != nil {
		return err
	}

	pools, err := selectPools(opts.pools)
	if err != nil {
		return err
	}

	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	if cfg.Backend.URL == "" {
		return fmt.Errorf("config's backend.url is empty, so there is no Mimir to read active series from")
	}
	if opts.metricsTenant == "" {
		return fmt.Errorf("--metrics-tenant is required: Mimir's own metrics live in whichever tenant scrapes them, which is usually not a tenant being costed")
	}

	if opts.dir != "" && !opts.ruleOutputs {
		return fmt.Errorf("--dir only supplies rule definitions for --rule-outputs, which was not given")
	}
	if opts.ruleOutputs {
		if opts.dir == "" && opts.tenants == "" {
			return fmt.Errorf("--rule-outputs needs rule definitions: give --dir, or --tenant to fetch them from the ruler API")
		}
		if opts.ruleOutputConcurrency < 1 {
			return fmt.Errorf("--rule-output-concurrency must be at least 1, got %d", opts.ruleOutputConcurrency)
		}
		// The drill-down divides by pool 1's per-tenant active series, so
		// it cannot run without pool 1. Adding the pool silently would put
		// a section in the report the operator excluded.
		if !slices.Contains(pools, cost.PoolIngesterMemory) {
			return fmt.Errorf("--rule-outputs is a drill-down under pool %s (its shares divide by that pool's active series): add %s to --pool, or drop --rule-outputs", cost.PoolIngesterMemory, cost.PoolIngesterMemory)
		}
	}

	inv := inventoryFromConfig(cfg.Inventory)
	if err := inv.Validate(); err != nil {
		return err
	}

	header, bearerToken := backendAuth(cfg)
	src := mimirdrivers.New(mimirdrivers.Config{
		BaseURL:       cfg.Backend.URL,
		Header:        header,
		MetricsTenant: opts.metricsTenant,
		BearerToken:   bearerToken,
		Timeout:       cfg.Backend.Timeout.Duration(),
	})
	readers := map[cost.PoolID]func(context.Context, time.Duration) (cost.Measurement, error){
		cost.PoolIngesterMemory: src.ReadIngesterMemory,
		cost.PoolQueryPath:      src.ReadQueryPath,
		cost.PoolRulerCPU:       src.ReadRulerCPU,
	}
	measurements := make([]cost.Measurement, 0, len(pools))
	for _, id := range pools {
		m, err := readers[id](ctx, window)
		if err != nil {
			return fmt.Errorf("pool %s: %w", id, err)
		}
		measurements = append(measurements, m)
	}

	rpt := cost.AllocateAll(measurements, inv, splitTenants(opts.tenants))
	result := report.CostResult{
		Pools:       rpt.Pools,
		CrossPool:   rpt.CrossPool,
		GeneratedAt: time.Now(),
	}
	if opts.ruleOutputs {
		pool, ok := rpt.Pool(cost.PoolIngesterMemory)
		if !ok {
			// Unreachable: --rule-outputs without pool 1 is rejected above.
			return fmt.Errorf("--rule-outputs needs pool %s", cost.PoolIngesterMemory)
		}
		ro, err := measureRuleOutputs(ctx, stderr, opts, cfg, pool)
		if err != nil {
			return err
		}
		result.RuleOutputs = &ro
	}
	return rep.Render(stdout, result)
}

// measureRuleOutputs loads rule definitions exactly as `report` does, then
// counts each recording rule's output series as the rule's own tenant, and
// joins the counts against pool 1's per-tenant active series. A rule file
// that fails to load is fatal, as in `report`: silently dropping a tenant's
// rules would understate its rule output. A count query that fails is not:
// its rules render as not measured, with the reason, and a warning goes to
// stderr.
func measureRuleOutputs(ctx context.Context, stderr io.Writer, opts costOptions, cfg config.Config, pool cost.PoolAllocation) (ruleoutput.Report, error) {
	defs, err := newDefinitionsSource(reportOptions{dir: opts.dir, tenants: opts.tenants}, cfg)
	if err != nil {
		return ruleoutput.Report{}, err
	}
	rules, loadErrs, err := defs.Load(ctx)
	if err != nil {
		return ruleoutput.Report{}, err
	}
	if len(loadErrs) > 0 {
		for _, le := range loadErrs {
			_, _ = fmt.Fprintln(stderr, "load error:", le.Error())
		}
		return ruleoutput.Report{}, fmt.Errorf("%d rule source(s) failed to load", len(loadErrs))
	}

	header, bearerToken := backendAuth(cfg)
	counts := ruleoutput.Count(ctx, ruleoutput.CountConfig{
		NewQuerier: func(tenant string) ruleoutput.Querier {
			return promapi.New(promapi.Config{
				BaseURL:     cfg.Backend.URL,
				Header:      header,
				Tenant:      tenant,
				BearerToken: bearerToken,
				Timeout:     cfg.Backend.Timeout.Duration(),
			})
		},
		Concurrency: opts.ruleOutputConcurrency,
	}, ruleoutput.Wanted(rules))

	failed := 0
	for _, metrics := range counts.Tenants {
		for _, c := range metrics {
			if c.Series == nil {
				failed++
			}
		}
	}
	if failed > 0 {
		_, _ = fmt.Fprintf(stderr, "warning: %d output metric(s) could not be counted; they are reported as not measured\n", failed)
	}

	denom := ruleoutput.DenominatorFromPool(pool)
	return ruleoutput.Join(rules, counts, &denom), nil
}

func inventoryFromConfig(c config.InventoryConfig) cost.Inventory {
	inv := cost.Inventory{
		Currency:          c.Currency,
		ReplicationFactor: c.ReplicationFactor,
		PlatformCostMonth: c.PlatformCostMonth,
		Pools:             make(map[cost.PoolID]cost.PoolInventory, len(c.Pools)),
	}
	for id, p := range c.Pools {
		pinv := cost.PoolInventory{
			Replicas:            p.Replicas,
			CostPerReplicaMonth: p.CostPerReplicaMonth,
		}
		if len(p.Components) > 0 {
			pinv.Components = make(map[string]cost.ComponentInventory, len(p.Components))
			for name, ci := range p.Components {
				pinv.Components[name] = cost.ComponentInventory{Replicas: ci.Replicas, CostPerReplicaMonth: ci.CostPerReplicaMonth}
			}
		}
		inv.Pools[cost.PoolID(id)] = pinv
	}
	return inv
}
