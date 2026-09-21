// Package cost is ADR 0003's cost model: it splits each resource pool of a
// shared Mimir cluster across tenants by the driver that physically makes
// that pool grow, and turns a share into "N of your M replicas" — and into
// currency only when the operator supplied a price.
//
// It does no I/O. Driver values arrive as a Measurement (from
// internal/cost/mimirdrivers, or a test), prices arrive as an Inventory
// (from promcost.yaml), and Allocate combines the two. Keeping the
// arithmetic here, away from PromQL, is what lets the rules below be
// tested exhaustively without a Mimir:
//
//   - shares are exhaustive over every measured tenant and sum to 1, never
//     a top-N (ADR 0003 decision 1)
//   - resources first; currency only from an operator-supplied price, and
//     an unpriced pool is "not costed", never 0 (decisions 2 and 3)
//   - a tenant the caller expected but Mimir did not measure is reported as
//     unmeasured and kept out of the denominator (decision 5)
//   - every figure carries the assumptions it rests on (decision 4)
//
// Three pools exist: 1 (ingester memory), 6 (query path) and 7 (ruler CPU),
// ADR 0004's build order. AllocateAll combines them, and ADR 0003 [A]'s rule
// is enforced by the shape of its result: a cross-pool total lives inside
// CrossPool, next to the coverage that qualifies it, so there is no way to
// read one without the other — and with no priced pool there is no total at
// all, only per-pool shares.
package cost

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// PoolID names a resource pool. Its string form is the key operators use
// under inventory.pools in promcost.yaml.
type PoolID string

const (
	PoolIngesterMemory PoolID = "ingester_memory"
	PoolQueryPath      PoolID = "query_path"
	PoolRulerCPU       PoolID = "ruler_cpu"
)

// knownPools is every pool Allocate can price. An inventory key outside it
// is a typo or a pool not built yet, and either way would otherwise be
// silently ignored — leaving a pool the operator thinks they priced
// reported as not costed.
var knownPools = map[PoolID]Pool{
	PoolIngesterMemory: IngesterMemory,
	PoolQueryPath:      QueryPath,
	PoolRulerCPU:       RulerCPU,
}

// PoolOrder is the order pools are read and rendered in: ADR 0003's pool
// numbers (1, 6, 7), which is also ADR 0004's confidence order.
var PoolOrder = []PoolID{PoolIngesterMemory, PoolQueryPath, PoolRulerCPU}

func knownPoolList() string {
	ids := make([]string, len(PoolOrder))
	for i, id := range PoolOrder {
		ids[i] = string(id)
	}
	return strings.Join(ids, ", ")
}

// Pool describes one resource pool for rendering and for the assumption
// trail. ReplicaNoun is what one unit of the pool is called ("ingester"),
// so a report can say "5.0 of your 8 ingesters" without a per-pool switch.
type Pool struct {
	ID          PoolID `json:"id"`
	Name        string `json:"name"`
	ReplicaNoun string `json:"replica_noun"`
	Driver      string `json:"driver"`
	DriverUnit  string `json:"driver_unit"`
	// Cumulative is true when the driver is a counter's increase over the
	// window (a total), false when it is a gauge averaged over it. It
	// decides the wording — "total over 24h" vs "average over 24h" — and
	// nothing else; the reader that built the Measurement did the
	// arithmetic.
	Cumulative bool `json:"cumulative,omitempty"`
	// Components names the processes a pool spans, in the order they are
	// rendered, for pools priced per component (ComponentInventory). Empty
	// for a single-component pool, which is priced with Replicas and
	// CostPerReplicaMonth directly.
	Components []string `json:"components,omitempty"`
}

// IngesterMemory is ADR 0003's pool 1: ingester memory, driven by
// in-memory active series.
var IngesterMemory = Pool{
	ID:          PoolIngesterMemory,
	Name:        "ingester memory",
	ReplicaNoun: "ingester",
	Driver:      "cortex_ingester_active_series",
	DriverUnit:  "active series",
}

// QueryPath is ADR 0003's pool 6: the query-frontend and queriers, driven by
// the data queries fetch. The driver is chosen at read time from a chain —
// see mimirdrivers.ReadQueryPath — so Driver names the preferred one and
// Measurement.DriverMetric names the one that actually answered.
var QueryPath = Pool{
	ID:          PoolQueryPath,
	Name:        "query path",
	ReplicaNoun: "query-path replica",
	Driver:      "cortex_query_fetched_chunk_bytes_total",
	DriverUnit:  "fetched volume",
	Cumulative:  true,
	// ADR 0003 defines pool 6 as the frontend and the queriers. The
	// query-scheduler is small and not part of that definition; an operator
	// who wants it counted folds its cost into one of these components' prices.
	Components: []string{"query-frontend", "querier"},
}

// RulerCPU is ADR 0003's pool 7: the ruler, driven by the time it spends
// evaluating rules. Under remote evaluation the queries are pool 6's; see
// mimirdrivers.ReadRulerCPU for what is and is not left here.
var RulerCPU = Pool{
	ID:          PoolRulerCPU,
	Name:        "ruler CPU",
	ReplicaNoun: "ruler",
	Driver:      "cortex_prometheus_rule_evaluation_duration_seconds_sum",
	DriverUnit:  "rule-evaluation seconds",
	Cumulative:  true,
}

// Inventory is the operator's side of the model (ADR 0003 decision 3).
// Every field is optional. See config.InventoryConfig, which this mirrors
// minus the YAML, and Validate, which checks it.
type Inventory struct {
	Currency          string
	ReplicationFactor *int
	// PlatformCostMonth is what the whole Mimir platform costs per month,
	// if the operator knows it. It is the only thing that lets a report say
	// what fraction of platform cost the priced pools cover (ADR 0003 [A]):
	// the fraction is not derivable from the pools alone, because an
	// unpriced pool's cost is unknown, not zero.
	PlatformCostMonth *float64
	Pools             map[PoolID]PoolInventory
}

// PoolInventory prices one pool. A single-component pool (1 and 7) uses
// Replicas and CostPerReplicaMonth; a pool that spans components (6) may
// use Components instead. Setting both forms on one pool is rejected by
// Validate rather than guessing which one the operator meant.
type PoolInventory struct {
	Replicas            *int
	CostPerReplicaMonth *float64
	Components          map[string]ComponentInventory
}

// ComponentInventory prices one component of a multi-component pool. A
// pool's cost is the sum over the components supplied: a partial list
// prices what it lists, and the allocation records which were priced.
type ComponentInventory struct {
	Replicas            *int
	CostPerReplicaMonth *float64
}

// Validate rejects inventories that would produce a figure nobody could
// defend: a price with no currency to print it in, or a non-positive
// replica count or replication factor. A negative price is rejected for
// the same reason. None of these are defaulted — the whole point of the
// inventory is that promcost never invents a coefficient.
func (inv Inventory) Validate() error {
	if inv.ReplicationFactor != nil && *inv.ReplicationFactor < 1 {
		return fmt.Errorf("inventory.replication_factor must be at least 1, got %d", *inv.ReplicationFactor)
	}
	if inv.PlatformCostMonth != nil {
		if *inv.PlatformCostMonth <= 0 {
			return fmt.Errorf("inventory.platform_cost_month must be positive, got %g", *inv.PlatformCostMonth)
		}
		if inv.Currency == "" {
			return fmt.Errorf("inventory.platform_cost_month is set but inventory.currency is empty: a cost figure is never rendered without its unit")
		}
	}
	ids := make([]string, 0, len(inv.Pools))
	for id := range inv.Pools {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	for _, id := range ids {
		pool, ok := knownPools[PoolID(id)]
		if !ok {
			return fmt.Errorf("inventory.pools.%s: unknown pool (known: %s)", id, knownPoolList())
		}
		p := inv.Pools[PoolID(id)]
		if err := validatePrice(fmt.Sprintf("inventory.pools.%s", id), p.Replicas, p.CostPerReplicaMonth, inv.Currency); err != nil {
			return err
		}
		if len(p.Components) == 0 {
			continue
		}
		if len(pool.Components) == 0 {
			return fmt.Errorf("inventory.pools.%s.components: %s is a single component; set replicas and cost_per_replica_month on the pool instead", id, id)
		}
		if p.Replicas != nil || p.CostPerReplicaMonth != nil {
			return fmt.Errorf("inventory.pools.%s sets both pool-level replicas/cost_per_replica_month and components: use one form, not both", id)
		}
		names := make([]string, 0, len(p.Components))
		for name := range p.Components {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !slices.Contains(pool.Components, name) {
				return fmt.Errorf("inventory.pools.%s.components.%s: unknown component (known: %s)", id, name, strings.Join(pool.Components, ", "))
			}
			c := p.Components[name]
			if err := validatePrice(fmt.Sprintf("inventory.pools.%s.components.%s", id, name), c.Replicas, c.CostPerReplicaMonth, inv.Currency); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePrice(path string, replicas *int, price *float64, currency string) error {
	if replicas != nil && *replicas < 1 {
		return fmt.Errorf("%s.replicas must be at least 1, got %d", path, *replicas)
	}
	if price != nil {
		if *price < 0 {
			return fmt.Errorf("%s.cost_per_replica_month must not be negative, got %g", path, *price)
		}
		if currency == "" {
			return fmt.Errorf("%s has a price but inventory.currency is empty: a cost figure is never rendered without its unit", path)
		}
	}
	return nil
}

// Unit says how a driver value should be shown. It is a property of the
// measurement, not the pool: pool 6's driver is bytes when Mimir reports
// fetched volume and seconds when it does not.
type Unit string

const (
	UnitCount   Unit = "count"
	UnitBytes   Unit = "bytes"
	UnitSeconds Unit = "seconds"
)

// Measurement is one pool's driver values over one window, as read from
// Mimir. Drivers holds each tenant's value summed across every replica
// that reported it — i.e. *before* dividing by the replication factor,
// which is Allocate's job, so that the division shows up in the
// assumption trail rather than being buried in a query.
type Measurement struct {
	Pool   Pool
	Window time.Duration
	End    time.Time
	// Source names where the values came from, e.g. "Mimir metrics
	// (queried as tenant infra)".
	Source string
	// DriverQuery is the PromQL that produced Drivers, verbatim.
	DriverQuery string
	Drivers     map[string]float64
	// DriverMetric names the metric that actually answered, for a pool
	// whose driver is chosen at read time (pool 6). Empty means
	// Pool.Driver.
	DriverMetric string
	// DriverKind says what the driver measures — "chunk bytes fetched",
	// "query seconds" — and heads the tenant column. Empty means
	// Pool.DriverUnit. Unit is how a value is formatted; zero means a count.
	DriverKind string
	Unit       Unit
	// Fallback is true when the reader used a weaker driver because a
	// stronger one returned nothing; FallbackNote says why that matters.
	// A report must show it: a fallback to time changes what the shares
	// mean (ADR 0002 decision 8).
	Fallback     bool
	FallbackNote string
	// Unreplicated is true for a per-tenant counter that is already
	// counted once (pools 6 and 7): no replication factor is read, applied
	// or shown, unlike the ingester gauge whose replicas each count a
	// series.
	Unreplicated bool
	// Assumptions and Notes are the reader's own additions to the trail and
	// to the "read before quoting" list — what it tried, what it detected.
	Assumptions []Assumption
	Notes       []string
	// ReplicationFactor is the value of
	// cortex_distributor_replication_factor, nil if Mimir did not return
	// one. RFQuery is the PromQL that was asked.
	ReplicationFactor *float64
	RFQuery           string
	// Deduplicated is true when Drivers already count each series once,
	// so no replication divisor applies. That is the case under Mimir's
	// ingest-storage architecture, where each ingester owns a Kafka
	// partition and the driver query takes the max across the zone
	// replicas of a partition instead of summing replicas.
	Deduplicated bool
	// Architecture names the write path the driver query found, e.g.
	// "classic" or "ingest storage", for the assumption trail.
	Architecture string
}

// Assumption is one line of a figure's assumption trail: what was assumed,
// the value used, and where that value came from — so an operator who
// disputes a number knows which input to change.
type Assumption struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

// TenantShare is one tenant's slice of a pool.
//
// DriverRaw is always present. Driver is DriverRaw divided by the
// replication factor, and is nil when no replication factor is known.
// Share is nil when the pool total is zero — a 0/0 share is undefined,
// and rendering it as 0% would be exactly the fabricated zero ADR 0001
// decision 3 forbids. EquivalentReplicas needs the operator's replica
// count and Cost additionally needs a price; each is nil without its
// input. A pool priced per component has no single replica count, so its
// tenants carry a Cost and no EquivalentReplicas.
type TenantShare struct {
	Tenant             string   `json:"tenant"`
	DriverRaw          float64  `json:"driver_raw"`
	Driver             *float64 `json:"driver,omitempty"`
	Share              *float64 `json:"share,omitempty"`
	EquivalentReplicas *float64 `json:"equivalent_replicas,omitempty"`
	Cost               *float64 `json:"cost_per_month,omitempty"`
}

// ComponentAllocation is one component of a component-priced pool. CostMonth
// is nil when the operator gave the component a replica count or a price
// but not both — it is then not priced, and says so.
type ComponentAllocation struct {
	Name                string   `json:"name"`
	Replicas            *int     `json:"replicas,omitempty"`
	CostPerReplicaMonth *float64 `json:"cost_per_replica_month,omitempty"`
	CostMonth           *float64 `json:"cost_per_month,omitempty"`
}

// PoolAllocation is one pool split across tenants.
type PoolAllocation struct {
	Pool   Pool          `json:"pool"`
	Window time.Duration `json:"-"`
	// WindowText is Window as an operator would write it ("24h", "7d").
	WindowText string    `json:"window"`
	End        time.Time `json:"end"`
	// DriverKind and Unit are what the driver actually measured, which for
	// pool 6 is only known after the read. Fallback is set when it is a
	// weaker driver than the pool prefers.
	DriverKind   string `json:"driver_kind"`
	Unit         Unit   `json:"unit"`
	Fallback     bool   `json:"fallback,omitempty"`
	FallbackNote string `json:"fallback_note,omitempty"`
	// Tenants is every measured tenant, sorted by DriverRaw descending,
	// ties broken by name. It is never truncated.
	Tenants []TenantShare `json:"tenants"`
	// Unmeasured is every expected tenant with no driver value, sorted.
	// They are not in the denominator (ADR 0003 decision 5).
	Unmeasured []string `json:"unmeasured,omitempty"`

	TotalRaw          float64  `json:"total_raw"`
	Total             *float64 `json:"total,omitempty"`
	ReplicationFactor *float64 `json:"replication_factor,omitempty"`

	Replicas            *int     `json:"replicas,omitempty"`
	CostPerReplicaMonth *float64 `json:"cost_per_replica_month,omitempty"`
	// Components is the priced breakdown of a component-priced pool, in the
	// pool's own order, and UnpricedComponents the components the pool
	// spans that the operator did not price. Neither is set for a pool
	// priced with Replicas and CostPerReplicaMonth.
	Components         []ComponentAllocation `json:"components,omitempty"`
	UnpricedComponents []string              `json:"unpriced_components,omitempty"`
	PoolCostMonth      *float64              `json:"pool_cost_per_month,omitempty"`
	Currency           string                `json:"currency,omitempty"`
	// Costed is true only when every input to a currency figure was
	// supplied. When false, the pool is "not costed" — not free. A
	// component-priced pool is costed as soon as one component is priced;
	// UnpricedComponents says how much of the pool that leaves out.
	Costed bool `json:"costed"`

	Assumptions []Assumption `json:"assumptions"`
	// Notes are caveats about this particular allocation that an operator
	// should read before quoting it, e.g. a replication-factor override
	// that disagrees with what Mimir reports.
	Notes []string `json:"notes,omitempty"`
}

const inventorySource = "promcost.yaml inventory"

// Allocate splits m's pool across its measured tenants and prices it from
// inv. expected lists tenants the caller knows exist; any of them absent
// from m.Drivers is reported as unmeasured rather than as zero. It may be
// nil — a tenant nobody expected and nobody measured is invisible to every
// source there is.
func Allocate(m Measurement, inv Inventory, expected []string) PoolAllocation {
	kind := m.DriverKind
	if kind == "" {
		kind = m.Pool.DriverUnit
	}
	metric := m.DriverMetric
	if metric == "" {
		metric = m.Pool.Driver
	}
	unit := m.Unit
	if unit == "" {
		unit = UnitCount
	}
	a := PoolAllocation{
		Pool:         m.Pool,
		Window:       m.Window,
		WindowText:   windowString(m.Window),
		End:          m.End,
		Currency:     inv.Currency,
		DriverKind:   kind,
		Unit:         unit,
		Fallback:     m.Fallback,
		FallbackNote: m.FallbackNote,
	}

	aggregation := "averaged"
	if m.Pool.Cumulative {
		aggregation = "total increase"
	}
	a.Assumptions = append(a.Assumptions,
		Assumption{
			Name:   "driver",
			Value:  fmt.Sprintf("%s (%s), %s over %s", metric, kind, aggregation, windowString(m.Window)),
			Source: m.Source,
		},
		Assumption{Name: "driver query", Value: m.DriverQuery, Source: m.Source},
	)
	a.Assumptions = append(a.Assumptions, m.Assumptions...)
	a.Notes = append(a.Notes, m.Notes...)
	if m.Fallback && m.FallbackNote != "" {
		a.Notes = append(a.Notes, m.FallbackNote)
	}
	if m.Architecture != "" {
		a.Assumptions = append(a.Assumptions, Assumption{
			Name:   "write path",
			Value:  m.Architecture,
			Source: "detected from which driver query returned data",
		})
	}

	var rf rfResolution
	if m.Unreplicated {
		// A counter already summed once: nothing to divide by, and saying
		// "replication factor: 1" would imply one was read.
		one := 1.0
		rf = rfResolution{value: &one}
	} else {
		var rfNote string
		rf, rfNote = resolveReplicationFactor(m, inv)
		a.ReplicationFactor = rf.value
		if rfNote != "" {
			a.Notes = append(a.Notes, rfNote)
		}
		a.Assumptions = append(a.Assumptions, Assumption{Name: "replication factor", Value: rf.display(), Source: rf.source})
	}

	for tenant, v := range m.Drivers {
		a.TotalRaw += v
		a.Tenants = append(a.Tenants, TenantShare{Tenant: tenant, DriverRaw: v})
	}
	sort.Slice(a.Tenants, func(i, j int) bool {
		if a.Tenants[i].DriverRaw != a.Tenants[j].DriverRaw {
			return a.Tenants[i].DriverRaw > a.Tenants[j].DriverRaw
		}
		return a.Tenants[i].Tenant < a.Tenants[j].Tenant
	})

	seen := map[string]bool{}
	for _, t := range expected {
		if _, ok := m.Drivers[t]; !ok && !seen[t] {
			a.Unmeasured = append(a.Unmeasured, t)
		}
		seen[t] = true
	}
	sort.Strings(a.Unmeasured)

	if rf.value != nil {
		total := a.TotalRaw / *rf.value
		a.Total = &total
	}

	pinv := inv.Pools[m.Pool.ID]
	if len(pinv.Components) > 0 {
		priceComponents(&a, m.Pool, inv, pinv)
	} else {
		priceFlat(&a, m.Pool, inv, pinv)
	}

	if a.TotalRaw <= 0 {
		if len(a.Tenants) > 0 {
			a.Notes = append(a.Notes, fmt.Sprintf(
				"every measured tenant reported 0 %s, so shares are undefined rather than 0%%", kind))
		}
		for i := range a.Tenants {
			a.Tenants[i].Driver = divideBy(a.Tenants[i].DriverRaw, rf.value)
		}
		return a
	}

	for i := range a.Tenants {
		t := &a.Tenants[i]
		t.Driver = divideBy(t.DriverRaw, rf.value)
		// The replication factor is deliberately absent here: a uniform
		// divisor cancels in a ratio, which is why shares survive an
		// unknown replication factor and absolute figures do not.
		share := t.DriverRaw / a.TotalRaw
		t.Share = &share
		if a.Replicas != nil {
			eq := share * float64(*a.Replicas)
			t.EquivalentReplicas = &eq
		}
		if a.PoolCostMonth != nil {
			c := share * *a.PoolCostMonth
			t.Cost = &c
		}
	}
	return a
}

// priceFlat prices a pool from its own Replicas and CostPerReplicaMonth —
// pools 1 and 7, or a component pool whose operator gave one figure for the
// whole tier.
func priceFlat(a *PoolAllocation, pool Pool, inv Inventory, pinv PoolInventory) {
	a.Replicas = pinv.Replicas
	a.CostPerReplicaMonth = pinv.CostPerReplicaMonth
	if pinv.Replicas != nil {
		a.Assumptions = append(a.Assumptions, Assumption{
			Name:   pool.ReplicaNoun + " replicas",
			Value:  fmt.Sprintf("%d", *pinv.Replicas),
			Source: inventorySource,
		})
	} else {
		a.Assumptions = append(a.Assumptions, Assumption{
			Name:   pool.ReplicaNoun + " replicas",
			Value:  "not supplied — shares only, no replica equivalents",
			Source: inventorySource,
		})
	}
	switch {
	case pinv.CostPerReplicaMonth == nil:
		a.Assumptions = append(a.Assumptions, Assumption{
			Name:   "price",
			Value:  "not supplied — pool not costed",
			Source: inventorySource,
		})
	case pinv.Replicas == nil:
		a.Assumptions = append(a.Assumptions, Assumption{
			Name:   "price",
			Value:  fmt.Sprintf("%s %s per %s-month", formatMoney(*pinv.CostPerReplicaMonth), inv.Currency, pool.ReplicaNoun),
			Source: inventorySource,
		})
		a.Notes = append(a.Notes, fmt.Sprintf(
			"a price per %s was supplied without a replica count, so the pool's total cost is unknown and it is not costed",
			pool.ReplicaNoun))
	default:
		poolCost := float64(*pinv.Replicas) * *pinv.CostPerReplicaMonth
		a.PoolCostMonth = &poolCost
		a.Costed = true
		a.Assumptions = append(a.Assumptions, Assumption{
			Name: "price",
			Value: fmt.Sprintf("%s %s per %s-month × %d = %s %s/month",
				formatMoney(*pinv.CostPerReplicaMonth), inv.Currency, pool.ReplicaNoun,
				*pinv.Replicas, formatMoney(poolCost), inv.Currency),
			Source: inventorySource,
		})
	}
}

// priceComponents prices a multi-component pool as the sum of the
// components the operator supplied (ADR 0003 decision 3: never invent a
// coefficient). A partial list prices what it lists — the pool is costed,
// its cost is a lower bound, and the allocation names the components left
// out so no figure claims a completeness it lacks.
func priceComponents(a *PoolAllocation, pool Pool, inv Inventory, pinv PoolInventory) {
	var total float64
	for _, name := range pool.Components {
		c, supplied := pinv.Components[name]
		if !supplied {
			a.UnpricedComponents = append(a.UnpricedComponents, name)
			a.Assumptions = append(a.Assumptions, Assumption{
				Name: name + " price", Value: "not supplied — component not priced", Source: inventorySource,
			})
			continue
		}
		ca := ComponentAllocation{Name: name, Replicas: c.Replicas, CostPerReplicaMonth: c.CostPerReplicaMonth}
		switch {
		case c.Replicas != nil && c.CostPerReplicaMonth != nil:
			cost := float64(*c.Replicas) * *c.CostPerReplicaMonth
			ca.CostMonth = &cost
			total += cost
			a.Assumptions = append(a.Assumptions, Assumption{
				Name: name + " price",
				Value: fmt.Sprintf("%s %s per %s-month × %d = %s %s/month",
					formatMoney(*c.CostPerReplicaMonth), inv.Currency, name, *c.Replicas, formatMoney(cost), inv.Currency),
				Source: inventorySource,
			})
		default:
			a.UnpricedComponents = append(a.UnpricedComponents, name)
			missing := "replica count"
			if c.Replicas != nil {
				missing = "price"
			}
			a.Assumptions = append(a.Assumptions, Assumption{
				Name: name + " price", Value: "no " + missing + " supplied — component not priced", Source: inventorySource,
			})
		}
		a.Components = append(a.Components, ca)
	}
	priced := 0
	for _, c := range a.Components {
		if c.CostMonth != nil {
			priced++
		}
	}
	if priced == 0 {
		return
	}
	a.PoolCostMonth = &total
	a.Costed = true
	if len(a.UnpricedComponents) > 0 {
		a.Notes = append(a.Notes, fmt.Sprintf(
			"the pool is priced from %d of its %d components; %s not priced, so the pool cost and every tenant's cost from it are lower bounds. Shares are unaffected",
			priced, len(pool.Components), strings.Join(a.UnpricedComponents, ", ")))
	}
}

type rfResolution struct {
	value  *float64
	source string
}

func (r rfResolution) display() string {
	if r.value == nil {
		return "unknown — absolute figures withheld, shares unaffected"
	}
	return formatNumber(*r.value)
}

// resolveReplicationFactor picks the divisor for absolute figures. An
// operator-supplied value wins, because the only reason to supply one is
// knowing better than what the metrics tenant can see; a disagreement with
// the measured value is surfaced as a note rather than silently resolved.
func resolveReplicationFactor(m Measurement, inv Inventory) (rfResolution, string) {
	if m.Deduplicated {
		one := 1.0
		var note string
		if inv.ReplicationFactor != nil && *inv.ReplicationFactor != 1 {
			note = fmt.Sprintf(
				"inventory.replication_factor (%d) is ignored: under %s the driver query already counts each series once",
				*inv.ReplicationFactor, m.Architecture)
		}
		return rfResolution{
			value:  &one,
			source: m.Architecture + ": one owner per partition, zone replicas collapsed with max, so there is nothing to divide",
		}, note
	}
	if inv.ReplicationFactor != nil {
		v := float64(*inv.ReplicationFactor)
		var note string
		if m.ReplicationFactor != nil && *m.ReplicationFactor != v {
			note = fmt.Sprintf(
				"inventory.replication_factor (%d) overrides the %s reported by cortex_distributor_replication_factor",
				*inv.ReplicationFactor, formatNumber(*m.ReplicationFactor))
		}
		return rfResolution{value: &v, source: inventorySource}, note
	}
	if m.ReplicationFactor != nil && *m.ReplicationFactor >= 1 {
		v := *m.ReplicationFactor
		return rfResolution{value: &v, source: m.RFQuery}, ""
	}
	return rfResolution{source: "not reported by Mimir and not in the inventory"},
		fmt.Sprintf("no replication factor is known, so absolute %s figures are withheld; shares are unaffected because a uniform replication factor cancels in a ratio", m.Pool.DriverUnit)
}

func divideBy(v float64, by *float64) *float64 {
	if by == nil {
		return nil
	}
	out := v / *by
	return &out
}

// windowString spells a window the way an operator would: whole days
// from 2d up, then the largest whole unit ("24h", "90m", "5m"), and Go's
// own form only for something no single unit divides.
func windowString(d time.Duration) string {
	if d >= 48*time.Hour && d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	for _, u := range []struct {
		size   time.Duration
		suffix string
	}{{time.Hour, "h"}, {time.Minute, "m"}, {time.Second, "s"}} {
		if d > 0 && d%u.size == 0 {
			return fmt.Sprintf("%d%s", d/u.size, u.suffix)
		}
	}
	return d.String()
}

func formatNumber(v float64) string {
	return fmt.Sprintf("%g", v)
}

func formatMoney(v float64) string {
	return fmt.Sprintf("%.2f", v)
}
