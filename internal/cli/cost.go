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
	"github.com/KorhanOzturk90/obscost/internal/report"
)

// newCostCmd wires `cost`: config.Load -> inventory -> mimirdrivers (pool
// driver values from Mimir's own metrics) -> cost.AllocateAll -> cost
// reporter. It is ADR 0004 build steps 1 and 3, tenant showback, for
// pools 1 (ingester memory), 6 (query path) and 7 (ruler CPU).
//
// It is a separate command from `report` rather than a mode of it because
// the two share almost no inputs: `report` joins rule executions against
// rule definitions, `cost` needs neither — only a Mimir URL, the metrics
// tenant, and the operator's inventory.
//
// As with `report`, stdout carries only the rendered report, so
// `--format json | jq` always sees exactly one document.
func newCostCmd(stdout io.Writer) *cobra.Command {
	var opts costOptions

	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Split each cost pool (ingester memory, query path, ruler CPU) across tenants by its driver, in replicas and (if priced) currency",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCost(cmd.Context(), stdout, opts)
		},
	}

	cmd.Flags().StringVar(&opts.metricsTenant, "metrics-tenant", "", "tenant whose TSDB holds Mimir's own scraped metrics (required). NOT a tenant being costed — see internal/telemetry/mimirmetrics")
	cmd.Flags().StringVar(&opts.tenants, "tenant", "", "comma-separated tenants you expect to exist; any Mimir did not measure is reported as \"not measured\" instead of silently missing")
	cmd.Flags().StringVar(&opts.configPath, "config", "", "path to promcost.yaml (backend.url, and the inventory block for replica counts and prices)")
	cmd.Flags().StringVar(&opts.format, "format", "md", "output format: md|json")
	cmd.Flags().StringVar(&opts.since, "since", "", "window to average or total over (e.g. 1h, 24h, 7d); defaults to 1h")
	cmd.Flags().StringVar(&opts.pools, "pool", "", "comma-separated pools to cost: "+poolList()+" (default: all)")
	return cmd
}

type costOptions struct {
	metricsTenant string
	tenants       string
	configPath    string
	format        string
	since         string
	pools         string
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

func runCost(ctx context.Context, stdout io.Writer, opts costOptions) error {
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
	return rep.Render(stdout, report.CostResult{
		Pools:       rpt.Pools,
		CrossPool:   rpt.CrossPool,
		GeneratedAt: time.Now(),
	})
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
