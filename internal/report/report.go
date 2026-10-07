// Package report renders promcost's workload attribution results for
// humans (md, html) and machines (json).
package report

type Format string

const (
	FormatMD   Format = "md"
	FormatJSON Format = "json"
	FormatHTML Format = "html"
)
