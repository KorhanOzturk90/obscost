// Package ndjson implements telemetry.Source for a newline-delimited JSON
// file: one rule.RuleExecution per line, decoded directly via its own json
// struct tags — that encoding IS the schema, there is no separate mapping
// struct to keep in sync.
package ndjson

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/KorhanOzturk90/obscost/internal/rule"
	"github.com/KorhanOzturk90/obscost/internal/telemetry"
)

type Config struct {
	Path string
}

type Source struct {
	cfg Config
}

func New(cfg Config) *Source {
	return &Source{cfg: cfg}
}

// initialBufSize/maxBufSize give the scanner room for lines carrying a
// long QueryText field without failing on an ordinary long line.
const (
	initialBufSize = 64 * 1024
	maxBufSize     = 8 * 1024 * 1024
)

func (s *Source) Read(_ context.Context) ([]rule.RuleExecution, []telemetry.ReadError, error) {
	f, err := os.Open(s.cfg.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("open telemetry file %s: %w", s.cfg.Path, err)
	}
	defer func() { _ = f.Close() }()

	var (
		executions []rule.RuleExecution
		readErrs   []telemetry.ReadError
	)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, initialBufSize), maxBufSize)

	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}

		var e rule.RuleExecution
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			readErrs = append(readErrs, telemetry.ReadError{Source: s.cfg.Path, Line: line, Err: err})
			continue
		}
		if e.RuleName == "" {
			readErrs = append(readErrs, telemetry.ReadError{Source: s.cfg.Path, Line: line, Err: fmt.Errorf("missing required field rule_name")})
			continue
		}
		if e.Timestamp.IsZero() {
			readErrs = append(readErrs, telemetry.ReadError{Source: s.cfg.Path, Line: line, Err: fmt.Errorf("missing or zero-value required field timestamp")})
			continue
		}

		executions = append(executions, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("read telemetry file %s: %w", s.cfg.Path, err)
	}

	return executions, readErrs, nil
}
