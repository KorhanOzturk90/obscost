package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KorhanOzturk90/obscost/internal/config"
	"github.com/KorhanOzturk90/obscost/internal/loader/rulerapi"
	"github.com/KorhanOzturk90/obscost/internal/timeline"
)

// newTimelineCmd wires `timeline`: a record of when rule definitions
// changed in Mimir, built from ruler API snapshots (ADR 0004 decision 6,
// internal/timeline). `capture` takes one snapshot per tenant and records
// the changes since that tenant's previous one; `diff` turns stored
// snapshots into a timeline. Both are one-off runs. Something that polls on
// a schedule (a CronJob, later the per-cluster agent) calls `capture`.
func newTimelineCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "Record when rule definitions changed in Mimir, from ruler API snapshots",
	}
	cmd.AddCommand(newTimelineCaptureCmd(stdout, stderr))
	cmd.AddCommand(newTimelineDiffCmd(stdout))
	return cmd
}

func newTimelineCaptureCmd(stdout, stderr io.Writer) *cobra.Command {
	var tenants, snapshotDir, configPath string
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Snapshot each tenant's rules from the ruler API and record the changes since the previous snapshot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTimelineCapture(cmd.Context(), stdout, stderr, tenants, snapshotDir, configPath, time.Now)
		},
	}
	cmd.Flags().StringVar(&tenants, "tenant", "", "comma-separated tenant IDs to snapshot (required)")
	cmd.Flags().StringVar(&snapshotDir, "snapshot-dir", "", "directory to store snapshots in, one subdirectory per tenant (required)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to promcost.yaml (backend.url is the Mimir to snapshot)")
	_ = cmd.MarkFlagRequired("tenant")
	_ = cmd.MarkFlagRequired("snapshot-dir")
	return cmd
}

// runTimelineCapture snapshots every tenant, continuing past a tenant that
// fails, and returns an error at the end if any did. A failed tenant gets
// no snapshot at all: an empty one would read as every rule removed.
func runTimelineCapture(ctx context.Context, stdout, stderr io.Writer, tenantsFlag, snapshotDir, configPath string, now func() time.Time) error {
	tenants := splitTenants(tenantsFlag)
	if len(tenants) == 0 {
		return fmt.Errorf("--tenant contained no tenant IDs")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if cfg.Backend.URL == "" {
		return fmt.Errorf("config's backend.url is empty: there is no ruler API to snapshot")
	}

	fetcher := newRulerClient(cfg, tenants)
	store := timeline.DirStore{Root: snapshotDir}

	failed := 0
	for _, tenant := range tenants {
		snap, err := timeline.Capture(ctx, fetcher, cfg.Backend.URL, tenant, now)
		if err == nil {
			var changes []timeline.Change
			if changes, err = timeline.Record(ctx, store, snap); err == nil {
				_, _ = fmt.Fprintln(stdout, captureSummary(snap, changes, store.SnapshotPath(snap)))
				continue
			}
		}
		failed++
		_, _ = fmt.Fprintf(stderr, "tenant %s: %v\n", tenant, err)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d tenant(s) could not be snapshotted", failed, len(tenants))
	}
	return nil
}

func captureSummary(snap timeline.Snapshot, changes []timeline.Change, path string) string {
	rules := 0
	for _, g := range snap.Groups {
		rules += len(g.Rules)
	}
	return fmt.Sprintf("tenant %s: %d rule(s) in %d group(s), %d change(s) since the previous snapshot, wrote %s",
		snap.Tenant, rules, len(snap.Groups), len(changes), path)
}

func newTimelineDiffCmd(stdout io.Writer) *cobra.Command {
	var tenants, snapshotDir, format string
	cmd := &cobra.Command{
		Use:   "diff [SNAPSHOT.json...]",
		Short: "Build a change timeline from snapshot files, or from every snapshot under --snapshot-dir",
		RunE: func(_ *cobra.Command, args []string) error {
			return runTimelineDiff(stdout, args, tenants, snapshotDir, format)
		},
	}
	cmd.Flags().StringVar(&snapshotDir, "snapshot-dir", "", "directory of snapshots, as written by timeline capture, read recursively")
	cmd.Flags().StringVar(&tenants, "tenant", "", "comma-separated tenant IDs to include (default: every tenant found)")
	cmd.Flags().StringVar(&format, "format", "md", "output format: md|json")
	return cmd
}

func runTimelineDiff(stdout io.Writer, files []string, tenantsFlag, snapshotDir, format string) error {
	if format != "md" && format != "json" {
		return fmt.Errorf("unknown --format %q, want md|json", format)
	}

	var snaps []timeline.Snapshot
	switch {
	case snapshotDir != "" && len(files) > 0:
		return errors.New("give snapshot files or --snapshot-dir, not both")
	case snapshotDir != "":
		var err error
		if snaps, err = timeline.ReadSnapshotDir(snapshotDir); err != nil {
			return err
		}
	case len(files) > 0:
		for _, f := range files {
			snap, err := timeline.ReadSnapshotFile(f)
			if err != nil {
				return err
			}
			snaps = append(snaps, snap)
		}
	default:
		return errors.New("give snapshot files (two or more to see changes), or --snapshot-dir")
	}

	if want := splitTenants(tenantsFlag); len(want) > 0 {
		keep := make(map[string]bool, len(want))
		for _, t := range want {
			keep[t] = true
		}
		filtered := snaps[:0]
		for _, s := range snaps {
			if keep[s.Tenant] {
				filtered = append(filtered, s)
			}
		}
		snaps = filtered
	}

	tl, err := timeline.Build(snaps)
	if err != nil {
		return err
	}
	if format == "json" {
		return timeline.WriteJSON(stdout, tl)
	}
	return timeline.WriteMarkdown(stdout, tl)
}

// newRulerClient builds a ruler API client from config the same way
// `report`'s ruler-API definitions source does.
func newRulerClient(cfg config.Config, tenants []string) *rulerapi.Loader {
	header := cfg.Tenancy.Header
	if header == "" {
		header = "X-Scope-OrgID"
	}
	var bearerToken string
	if cfg.Backend.Auth.BearerTokenEnv != "" {
		bearerToken = os.Getenv(cfg.Backend.Auth.BearerTokenEnv)
	}
	return rulerapi.New(rulerapi.Config{
		BaseURL:     cfg.Backend.URL,
		Header:      header,
		Tenants:     tenants,
		BearerToken: bearerToken,
		Timeout:     cfg.Backend.Timeout.Duration(),
	})
}

func splitTenants(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}
