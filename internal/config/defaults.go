package config

import "time"

// Default returns the built-in defaults from spec §3's example config
// (plus the high_cardinality_labels wordlist, this milestone's addition).
func Default() Config {
	return Config{
		Tenancy: TenancyConfig{
			Header:   "X-Scope-OrgID",
			Unmapped: "error",
		},
		Checks: ChecksConfig{
			Thresholds: ThresholdsConfig{
				SubqueryStepsWarn:     500,
				SubqueryStepsError:    2000,
				RecordingRangeWarn:    Duration(24 * time.Hour),
				LimitHeadroomWarnPct:  60,
				LimitHeadroomErrorPct: 90,
				OutputCardinalityWarn: 10000,
				PresenceWindow:        Duration(time.Hour),
				HighCardinalityLabels: []string{
					"pod", "instance", "id", "path", "le",
					"uid", "container_id", "replicaset", "pod_name",
				},
			},
		},
	}
}
