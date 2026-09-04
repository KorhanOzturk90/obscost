package report

import (
	"encoding/json"
	"io"
)

type workloadJSONReporter struct{}

func (workloadJSONReporter) Render(w io.Writer, result WorkloadResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}
