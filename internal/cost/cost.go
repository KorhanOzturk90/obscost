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
// Only pool 1, ingester memory, exists so far — ADR 0003's "ship pool 1
// alone". The types are shaped for more pools, but no multi-pool total is
// computed yet: ADR 0003 [A] requires any cross-pool total to name the
// fraction of platform cost it covers, and with one pool there is nothing
// to total.
package cost

import (
	"fmt"
	"sort"
	"time"
)

// PoolID names a resource pool. Its string form is the key operators use
// under inventory.pools in promcost.yaml.
type PoolID string

const PoolIngesterMemory PoolID = "ingester_memory"

// knownPools is every pool Allocate can price. An inventory key outside it
// is a typo or a pool not built yet, and either way would otherwise be
// silently ignored — leaving a pool the operator thinks they priced
// reported as not costed.
var knownPools = map[PoolID]bool{PoolIngesterMemory: true}

// Pool describes one resource pool for rendering and for the assumption
// trail. ReplicaNoun is what one unit of the pool is called ("ingester"),
// so a report can say "5.0 of your 8 ingesters" without a per-pool switch.
type Pool struct {
	ID          PoolID `json:"id"`
	Name        string `json:"name"`
	ReplicaNoun string `json:"replica_noun"`
	Driver      string `json:"driver"`
	DriverUnit  string `json:"driver_unit"`
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

// Inventory is the operator's side of the model (ADR 0003 decision 3).
// Every field is optional. See config.InventoryConfig, which this mirrors
// minus the YAML, and Validate, which checks it.
type Inventory struct {
	Currency          string
	ReplicationFactor *int
	Pools             map[PoolID]PoolInventory
}

type PoolInventory struct {
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
	ids := make([]string, 0, len(inv.Pools))
	for id := range inv.Pools {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !knownPools[PoolID(id)] {
			return fmt.Errorf("inventory.pools.%s: unknown pool (known: %s)", id, PoolIngesterMemory)
		}
		p := inv.Pools[PoolID(id)]
		if p.Replicas != nil && *p.Replicas < 1 {
			return fmt.Errorf("inventory.pools.%s.replicas must be at least 1, got %d", id, *p.Replicas)
		}
		if p.CostPerReplicaMonth != nil {
			if *p.CostPerReplicaMonth < 0 {
				return fmt.Errorf("inventory.pools.%s.cost_per_replica_month must not be negative, got %g", id, *p.CostPerReplicaMonth)
			}
			if inv.Currency == "" {
				return fmt.Errorf("inventory.pools.%s has a price but inventory.currency is empty: a cost figure is never rendered without its unit", id)
			}
		}
	}
	return nil
}

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
// input.
type TenantShare struct {
	Tenant             string   `json:"tenant"`
	DriverRaw          float64  `json:"driver_raw"`
	Driver             *float64 `json:"driver,omitempty"`
	Share              *float64 `json:"share,omitempty"`
	EquivalentReplicas *float64 `json:"equivalent_replicas,omitempty"`
	Cost               *float64 `json:"cost_per_month,omitempty"`
}

// PoolAllocation is one pool split across tenants.
type PoolAllocation struct {
	Pool   Pool          `json:"pool"`
	Window time.Duration `json:"-"`
	// WindowText is Window as an operator would write it ("24h", "7d").
	WindowText string    `json:"window"`
	End        time.Time `json:"end"`
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
	PoolCostMonth       *float64 `json:"pool_cost_per_month,omitempty"`
	Currency            string   `json:"currency,omitempty"`
	// Costed is true only when every input to a currency figure was
	// supplied. When false, the pool is "not costed" — not free.
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
	a := PoolAllocation{
		Pool:       m.Pool,
		Window:     m.Window,
		WindowText: windowString(m.Window),
		End:        m.End,
		Currency:   inv.Currency,
	}

	a.Assumptions = append(a.Assumptions,
		Assumption{
			Name:   "driver",
			Value:  fmt.Sprintf("%s (%s), averaged over %s", m.Pool.Driver, m.Pool.DriverUnit, windowString(m.Window)),
			Source: m.Source,
		},
		Assumption{Name: "driver query", Value: m.DriverQuery, Source: m.Source},
	)
	if m.Architecture != "" {
		a.Assumptions = append(a.Assumptions, Assumption{
			Name:   "write path",
			Value:  m.Architecture,
			Source: "detected from which driver query returned data",
		})
	}

	rf, rfNote := resolveReplicationFactor(m, inv)
	a.ReplicationFactor = rf.value
	if rfNote != "" {
		a.Notes = append(a.Notes, rfNote)
	}
	a.Assumptions = append(a.Assumptions, Assumption{Name: "replication factor", Value: rf.display(), Source: rf.source})

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
	a.Replicas = pinv.Replicas
	a.CostPerReplicaMonth = pinv.CostPerReplicaMonth
	if pinv.Replicas != nil {
		a.Assumptions = append(a.Assumptions, Assumption{
			Name:   m.Pool.ReplicaNoun + " replicas",
			Value:  fmt.Sprintf("%d", *pinv.Replicas),
			Source: inventorySource,
		})
	} else {
		a.Assumptions = append(a.Assumptions, Assumption{
			Name:   m.Pool.ReplicaNoun + " replicas",
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
			Value:  fmt.Sprintf("%s %s per %s-month", formatMoney(*pinv.CostPerReplicaMonth), inv.Currency, m.Pool.ReplicaNoun),
			Source: inventorySource,
		})
		a.Notes = append(a.Notes, fmt.Sprintf(
			"a price per %s was supplied without a replica count, so the pool's total cost is unknown and it is not costed",
			m.Pool.ReplicaNoun))
	default:
		poolCost := float64(*pinv.Replicas) * *pinv.CostPerReplicaMonth
		a.PoolCostMonth = &poolCost
		a.Costed = true
		a.Assumptions = append(a.Assumptions, Assumption{
			Name: "price",
			Value: fmt.Sprintf("%s %s per %s-month × %d = %s %s/month",
				formatMoney(*pinv.CostPerReplicaMonth), inv.Currency, m.Pool.ReplicaNoun,
				*pinv.Replicas, formatMoney(poolCost), inv.Currency),
			Source: inventorySource,
		})
	}

	if a.TotalRaw <= 0 {
		if len(a.Tenants) > 0 {
			a.Notes = append(a.Notes, fmt.Sprintf(
				"every measured tenant reported 0 %s, so shares are undefined rather than 0%%", m.Pool.DriverUnit))
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
		if pinv.Replicas != nil {
			eq := share * float64(*pinv.Replicas)
			t.EquivalentReplicas = &eq
		}
		if a.PoolCostMonth != nil {
			c := share * *a.PoolCostMonth
			t.Cost = &c
		}
	}
	return a
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
