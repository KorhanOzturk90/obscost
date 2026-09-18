package timeline

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/KorhanOzturk90/obscost/internal/rule"
)

// WriteJSON writes tl as one indented JSON document.
func WriteJSON(w io.Writer, tl Timeline) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(tl)
}

// WriteMarkdown writes tl as a Markdown report: per tenant, the span the
// snapshots cover and a table of changes with the window each one fell in.
func WriteMarkdown(w io.Writer, tl Timeline) error {
	var b strings.Builder
	b.WriteString("# Rule change timeline\n\n")
	b.WriteString("Each change became visible in the ruler API after the first time and no later than the second (UTC, clock of the machine that took the snapshots).\n")

	if len(tl.Tenants) == 0 {
		b.WriteString("\nNo snapshots.\n")
	}
	for _, t := range tl.Tenants {
		fmt.Fprintf(&b, "\n## Tenant `%s`\n\n", t.Tenant)
		if t.Snapshots == 1 {
			fmt.Fprintf(&b, "1 snapshot, observed %s. That is a baseline: changes need a second snapshot.\n", formatTime(t.FirstObserved))
			continue
		}
		fmt.Fprintf(&b, "%d snapshots, first observed %s, last observed %s.\n\n", t.Snapshots, formatTime(t.FirstObserved), formatTime(t.LastObserved))
		if len(t.Changes) == 0 {
			b.WriteString("No rule changes.\n")
			continue
		}
		b.WriteString("| After | No later than | Change | Rule | What |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, c := range t.Changes {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				formatTime(c.Window.After), formatTime(c.Window.NotAfter), c.Type,
				cell(ruleLabel(c.Rule)), cell(describe(c)))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func ruleLabel(id rule.RuleID) string {
	return fmt.Sprintf("%s / %s / %s", id.Namespace, id.Group, id.Name)
}

func describe(c Change) string {
	switch c.Type {
	case ChangeAdded:
		return c.After.Kind
	case ChangeRemoved:
		return c.Before.Kind
	}
	parts := make([]string, 0, len(c.Fields))
	for _, f := range c.Fields {
		switch f {
		case FieldNamespace, FieldGroup:
			// covered by "moved from" below
		case FieldFor:
			parts = append(parts, fmt.Sprintf("for %s → %s", seconds(c.Before.ForSeconds), seconds(c.After.ForSeconds)))
		case FieldKeepFiringFor:
			parts = append(parts, fmt.Sprintf("keep_firing_for %s → %s", seconds(c.Before.KeepFiringForSeconds), seconds(c.After.KeepFiringForSeconds)))
		case FieldInterval:
			parts = append(parts, fmt.Sprintf("interval %s → %s", seconds(c.Before.IntervalSeconds), seconds(c.After.IntervalSeconds)))
		case FieldKind:
			parts = append(parts, fmt.Sprintf("kind %s → %s", c.Before.Kind, c.After.Kind))
		default:
			parts = append(parts, string(f))
		}
	}
	if c.PreviousRule != nil {
		parts = append(parts, "moved from "+ruleLabel(*c.PreviousRule))
	}
	return strings.Join(parts, "; ")
}

func seconds(s float64) string {
	return time.Duration(s * float64(time.Second)).String()
}

// cell makes s safe inside a Markdown table cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", " ")
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
