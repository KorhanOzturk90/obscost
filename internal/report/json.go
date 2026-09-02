package report

import (
	"encoding/json"
	"io"
)

type jsonReporter struct{}

func (jsonReporter) Render(w io.Writer, result Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}
