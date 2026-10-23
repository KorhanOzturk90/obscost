package promapi

import (
	"strings"
	"testing"
)

// With no selector the queries are sent exactly as written, so the trail
// keeps their readable form and nothing else changes.
func TestSelector_ZeroLeavesQueriesUnchanged(t *testing.T) {
	q := `sum by (user) (increase(x[1h]))`
	got, err := Selector{}.Apply(q)
	if err != nil || got != q {
		t.Errorf("apply = %q, %v; want %q unchanged", got, err, q)
	}
}

func TestParseSelector(t *testing.T) {
	for _, ok := range []string{`{cluster="prod-eu"}`, ` {cluster="a", namespace!="b"} `, `{cluster=~"prod-.*"}`, ""} {
		if _, err := ParseSelector(ok); err != nil {
			t.Errorf("ParseSelector(%q) = %v, want ok", ok, err)
		}
	}
	for in, want := range map[string]string{
		`cluster="prod-eu"`:              "in braces",
		`up{cluster="prod-eu"}`:          "in braces",
		`{__name__="up"}`:                "__name__",
		`{user="analytics"}`:             "tenant label",
		`{cluster="a"} or {cluster="b"}`: "selector",
		`{cluster="a"}[5m]`:              "selector",
		`{}`:                             "selector",
		`{cluster="a"`:                   "selector",
		`sum({cluster="a"})`:             "in braces",
		`{cluster="a"} offset 5m`:        "selector",
	} {
		_, err := ParseSelector(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseSelector(%q) = %v, want an error mentioning %q", in, err, want)
		}
	}
}
