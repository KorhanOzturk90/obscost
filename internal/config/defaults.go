package config

// Default returns the built-in defaults applied before promcost.yaml is
// read.
func Default() Config {
	return Config{
		Tenancy: TenancyConfig{
			Header:   "X-Scope-OrgID",
			Unmapped: "error",
		},
	}
}
