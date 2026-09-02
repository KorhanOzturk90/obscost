package report

import (
	"fmt"
	"io"
	"sort"
	"text/template"
)

type mdReporter struct{}

type mdKV struct {
	Key   string
	Value string
}

type mdFinding struct {
	CheckID     string
	Severity    string
	Tenant      string
	File        string
	Group       string
	Rule        string
	Message     string
	Values      []mdKV
	Remediation string
}

type mdSeverityCount struct {
	Severity string
	Count    int
}

type mdData struct {
	GeneratedAt    string
	RulesScanned   int
	Tenants        []string
	SeverityCounts []mdSeverityCount
	Findings       []mdFinding
}

// mdTemplateSrc deliberately avoids ranging over Go maps directly (map
// iteration order is unspecified) — all data handed to the template is
// pre-sorted into slices by Render, so output is byte-for-byte
// deterministic across runs (required for golden-file testing).
const mdTemplateSrc = `# promcost check report

Generated: {{ .GeneratedAt }}
Rules scanned: {{ .RulesScanned }}
{{- if .Tenants }}
Tenants: {{ range $i, $t := .Tenants }}{{ if $i }}, {{ end }}{{ $t }}{{ end }}
{{- end }}

## Summary

| severity | count |
|---|---|
{{- range .SeverityCounts }}
| {{ .Severity }} | {{ .Count }} |
{{- end }}
{{ if .Findings }}
## Findings
{{ range .Findings }}
### [{{ .Severity }}] {{ .CheckID }} — {{ .File }}:{{ .Group }}:{{ .Rule }}

{{ .Message }}
{{- if .Tenant }}

Tenant: {{ .Tenant }}
{{- end }}
{{- if .Values }}

| key | value |
|---|---|
{{- range .Values }}
| {{ .Key }} | {{ .Value }} |
{{- end }}
{{- end }}
{{- if .Remediation }}

Remediation: {{ .Remediation }}
{{- end }}

---
{{ end }}
{{- else }}
No findings.
{{- end }}
`

var mdTmpl = template.Must(template.New("md").Parse(mdTemplateSrc))

func (mdReporter) Render(w io.Writer, result Result) error {
	data := mdData{
		GeneratedAt:  result.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"),
		RulesScanned: result.RulesScanned,
		Tenants:      append([]string(nil), result.TenantsSeen...),
	}
	sort.Strings(data.Tenants)

	counts := map[string]int{}
	for _, f := range result.Findings {
		counts[f.Severity.String()]++
	}
	for _, sev := range []string{"error", "warn", "info"} {
		if c, ok := counts[sev]; ok {
			data.SeverityCounts = append(data.SeverityCounts, mdSeverityCount{Severity: sev, Count: c})
		}
	}

	for _, f := range result.Findings {
		mf := mdFinding{
			CheckID:     f.CheckID,
			Severity:    f.Severity.String(),
			Tenant:      f.Tenant,
			File:        f.Location.File,
			Group:       f.Location.Group,
			Rule:        f.Location.Rule,
			Message:     f.Message,
			Remediation: f.Remediation,
		}

		keys := make([]string, 0, len(f.Values))
		for k := range f.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			mf.Values = append(mf.Values, mdKV{Key: k, Value: fmt.Sprintf("%v", f.Values[k])})
		}

		data.Findings = append(data.Findings, mf)
	}

	return mdTmpl.Execute(w, data)
}
