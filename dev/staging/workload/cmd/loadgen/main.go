// Command loadgen drives a set of synthetic Mimir tenants with known shapes
// and records the ground truth of what it produced, so promcost's output
// can be checked against a known answer rather than eyeballed.
//
//	loadgen run   --profile profiles/standard.yaml --gateway http://mimir:8080 --state gt.json
//	loadgen rules --profile profiles/standard.yaml --out rules/
//	loadgen plan  --profile profiles/standard.yaml
//
// Every request goes through the gateway with X-Scope-OrgID: remote write
// to /api/v1/push, range queries to /prometheus/api/v1/query_range (the
// query-frontend), rules to /prometheus/config/v1/rules (the ruler API).
// See ../../README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(os.Args[2:])
	case "rules":
		err = cmdRules(os.Args[2:])
	case "plan":
		err = cmdPlan(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: loadgen run|rules|plan --profile FILE [flags]  (-h on a subcommand for its flags)")
	os.Exit(2)
}

type headerFlags map[string]string

func (h headerFlags) String() string { return fmt.Sprint(map[string]string(h)) }
func (h headerFlags) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("want Name=value, got %q", v)
	}
	h[k] = val
	return nil
}

func cmdPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	profile := fs.String("profile", "", "workload profile YAML")
	_ = fs.Parse(args)
	p, err := LoadProfile(*profile)
	if err != nil {
		return err
	}
	st := &truthStore{gt: planTruth(p, "", time.Now().UTC())}
	_, err = os.Stdout.Write(append(st.snapshot(), '\n'))
	return err
}

func cmdRules(args []string) error {
	fs := flag.NewFlagSet("rules", flag.ExitOnError)
	profile := fs.String("profile", "", "workload profile YAML")
	out := fs.String("out", "", "directory to write <tenant>/<namespace>.yaml rule files into")
	_ = fs.Parse(args)
	p, err := LoadProfile(*profile)
	if err != nil {
		return err
	}
	if *out == "" {
		return errors.New("--out is required")
	}
	return writeRuleFiles(p, *out)
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	profile := fs.String("profile", "", "workload profile YAML")
	gateway := fs.String("gateway", "http://localhost:8080", "Mimir gateway base URL (push, query-frontend and ruler API behind it)")
	state := fs.String("state", "ground-truth.json", "ground-truth file; an existing one for the same profile is resumed")
	listen := fs.String("listen", ":9100", "serve GET /ground-truth and /healthz here (empty to disable)")
	only := fs.String("tenants", "", "comma-separated subset of the profile's tenants to drive (default: all)")
	noRules := fs.Bool("no-rules", false, "do not push rules (for a ruler that reads local files)")
	batch := fs.Int("batch", 2000, "series per remote-write request")
	headers := headerFlags{}
	fs.Var(headers, "header", "extra HTTP header Name=value on every request (repeatable), e.g. for gateway auth")
	_ = fs.Parse(args)

	p, err := LoadProfile(*profile)
	if err != nil {
		return err
	}
	if *only != "" {
		keep := map[string]bool{}
		for _, n := range strings.Split(*only, ",") {
			keep[strings.TrimSpace(n)] = true
		}
		var ts []Tenant
		for _, t := range p.Tenants {
			if keep[t.Name] {
				ts = append(ts, t)
			}
		}
		p.Tenants = ts
	}
	gw := strings.TrimRight(*gateway, "/")
	store := &truthStore{gt: planTruth(p, gw, time.Now().UTC()), path: *state}
	resumed, err := store.resume()
	if err != nil {
		return err
	}
	log.Printf("profile %s, %d tenants, gateway %s, run started %s (resumed: %v)",
		p.Name, len(p.Tenants), gw, store.gt.StartedAt.Format(time.RFC3339), resumed)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	client := &http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 16}}
	if *listen != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/ground-truth", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(store.snapshot())
		})
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
		srv := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("listen %s: %v", *listen, err)
			}
		}()
		defer srv.Close()
	}

	// Persist every 15s, so a crash loses at most that much of the record.
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := store.save(); err != nil {
					log.Printf("save ground truth: %v", err)
				}
			}
		}
	}()

	var wg sync.WaitGroup
	for _, t := range p.Tenants {
		wg.Add(1)
		go func(t Tenant) {
			defer wg.Done()
			runTenant(ctx, t, p, store, tenantDeps{
				pusher: &pusher{client: client, url: gw + "/api/v1/push", tenant: t.Name, headers: headers},
				ruler:  &rulerClient{client: client, base: gw, headers: headers},
				query:  &queryClient{client: client, base: gw, tenant: t.Name, headers: headers},
				rules:  !*noRules,
				batch:  *batch,
			})
		}(t)
	}
	wg.Wait()
	return store.save()
}

type tenantDeps struct {
	pusher *pusher
	ruler  *rulerClient
	query  *queryClient
	rules  bool
	batch  int
}

// runTenant waits for the tenant's start time, pushes its rules, then
// writes and queries until its stop time or shutdown.
func runTenant(ctx context.Context, t Tenant, p Profile, store *truthStore, d tenantDeps) {
	start := store.gt.StartedAt.Add(t.Timing.StartAfter.D)
	var stop time.Time
	if t.Timing.StopAfter.D > 0 {
		stop = store.gt.StartedAt.Add(t.Timing.StopAfter.D)
		if !time.Now().Before(stop) {
			log.Printf("[%s] stop time %s already passed; staying silent", t.Name, stop.Format(time.RFC3339))
			return
		}
	}
	if wait := time.Until(start); wait > 0 {
		log.Printf("[%s] starts in %s", t.Name, wait.Round(time.Second))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}

	runCtx := ctx
	if !stop.IsZero() {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithDeadline(ctx, stop)
		defer cancel()
	}

	if d.rules && t.Rules != nil {
		groups, _ := generateRules(t)
		for attempt := 0; ; attempt++ {
			err := d.ruler.replace(runCtx, t.Name, p.RuleNamespace, groups)
			if err == nil {
				now := time.Now().UTC()
				store.update(t.Name, func(tt *TenantTruth) { tt.RulesPushedAt = &now })
				log.Printf("[%s] pushed %d rule groups", t.Name, len(groups))
				break
			}
			store.update(t.Name, func(tt *TenantTruth) { tt.Counters.fail(err) })
			log.Printf("[%s] rules: %v (retrying)", t.Name, err)
			select {
			case <-runCtx.Done():
				return
			case <-time.After(time.Duration(min(attempt+1, 6)) * 5 * time.Second):
			}
		}
	}

	store.update(t.Name, func(tt *TenantTruth) { tt.Segments = append(tt.Segments, Segment{}) })
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); writeLoop(runCtx, t, store, d) }()
	if t.Queries != nil {
		wg.Add(1)
		go func() { defer wg.Done(); queryLoop(runCtx, t, store, d.query) }()
	}
	wg.Wait()

	now := time.Now().UTC()
	store.update(t.Name, func(tt *TenantTruth) { tt.Segments[len(tt.Segments)-1].StoppedAt = &now })
	if ctx.Err() == nil {
		log.Printf("[%s] stopped as planned", t.Name)
		_ = store.save()
	}
}
