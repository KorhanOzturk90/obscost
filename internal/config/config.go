// Package config loads and represents promcost.yaml: the backend `report`
// talks to (the ruler API) and the tenancy block that maps rule files to
// tenants.
//
// Sections the v0 static analyzer used (checks, pint, limits, cost_model)
// were removed with it (docs/adr/0006-remove-static-analysis.md). A config
// that still contains one fails to load with an error naming it, rather
// than yaml's generic unknown-field message.
package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type BackendAuth struct {
	BearerTokenEnv string `yaml:"bearer_token_env,omitempty"`
}

type BackendConfig struct {
	URL     string      `yaml:"url,omitempty"`
	Auth    BackendAuth `yaml:"auth,omitempty"`
	Timeout Duration    `yaml:"timeout,omitempty"`
}

// DiscoverySource is one entry in tenancy.discovery. Only Source=="static"
// is resolvable this milestone (see internal/tenancy); the other types
// parse cleanly so a full spec-example config loads.
type DiscoverySource struct {
	Source    string            `yaml:"source"`
	Key       string            `yaml:"key,omitempty"`
	Transform string            `yaml:"transform,omitempty"`
	Map       map[string]string `yaml:"map,omitempty"`
}

type TenancyConfig struct {
	Header    string            `yaml:"header,omitempty"`
	Discovery []DiscoverySource `yaml:"discovery,omitempty"`
	// Unmapped is "error" | "skip" | "tenant:<name>".
	Unmapped string `yaml:"unmapped,omitempty"`
}

type Config struct {
	Backend BackendConfig `yaml:"backend,omitempty"`
	Tenancy TenancyConfig `yaml:"tenancy,omitempty"`
}

// removedSections are top-level keys that belonged to the static analyzer,
// and removedBackendKeys are backend fields only it read. Load rejects
// them by name so an old config gets an actionable error.
var (
	removedSections    = []string{"checks", "pint", "limits", "cost_model"}
	removedBackendKeys = []string{"type", "max_concurrent_queries"}
)

// Load reads promcost.yaml from path, starting from Default() so any
// section or field the file omits keeps its default value. An empty path
// returns Default() unchanged (no config file supplied).
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %s: %w", path, err)
	}
	if err := checkRemoved(data); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// checkRemoved reports the first removed section or backend field present
// in data. Malformed YAML is left for the strict decode to report.
func checkRemoved(data []byte) error {
	var top map[string]yaml.Node
	if err := yaml.Unmarshal(data, &top); err != nil {
		return nil
	}
	for _, key := range removedSections {
		if _, ok := top[key]; ok {
			return fmt.Errorf("section %q was removed along with the static analyzer (promcost check); delete it", key)
		}
	}
	backend, ok := top["backend"]
	if !ok {
		return nil
	}
	var fields map[string]yaml.Node
	if err := backend.Decode(&fields); err != nil {
		return nil
	}
	for _, key := range removedBackendKeys {
		if _, ok := fields[key]; ok {
			return fmt.Errorf("field \"backend.%s\" was removed along with the static analyzer (promcost check); delete it", key)
		}
	}
	return nil
}
