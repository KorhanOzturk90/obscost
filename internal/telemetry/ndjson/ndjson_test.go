package ndjson

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "executions.ndjson")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestRead_ValidMultiLine(t *testing.T) {
	content := `{"tenant":"analytics","namespace":"a/rules.yaml","group":"g","rule_name":"r1","timestamp":"2026-01-01T00:00:00Z","duration_seconds":1.5,"samples_processed":100}
{"tenant":"analytics","namespace":"a/rules.yaml","group":"g","rule_name":"r2","timestamp":"2026-01-01T00:01:00Z","duration_seconds":2.5,"samples_processed":200}
`
	src := New(Config{Path: writeFile(t, content)})
	executions, readErrs, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(readErrs) != 0 {
		t.Fatalf("unexpected read errors: %v", readErrs)
	}
	if len(executions) != 2 {
		t.Fatalf("len(executions) = %d, want 2", len(executions))
	}
	if executions[0].RuleName != "r1" || executions[1].RuleName != "r2" {
		t.Errorf("unexpected rule names: %+v", executions)
	}
}

func TestRead_BlankLinesTolerated(t *testing.T) {
	content := "\n" + `{"tenant":"t","namespace":"n","group":"g","rule_name":"r","timestamp":"2026-01-01T00:00:00Z"}` + "\n\n"
	src := New(Config{Path: writeFile(t, content)})
	executions, readErrs, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(readErrs) != 0 {
		t.Fatalf("unexpected read errors: %v", readErrs)
	}
	if len(executions) != 1 {
		t.Fatalf("len(executions) = %d, want 1", len(executions))
	}
}

func TestRead_MalformedLineMidFile(t *testing.T) {
	content := `{"tenant":"t","namespace":"n","group":"g","rule_name":"r1","timestamp":"2026-01-01T00:00:00Z"}
not valid json
{"tenant":"t","namespace":"n","group":"g","rule_name":"r2","timestamp":"2026-01-01T00:01:00Z"}
`
	src := New(Config{Path: writeFile(t, content)})
	executions, readErrs, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(executions) != 2 {
		t.Fatalf("len(executions) = %d, want 2 (valid lines before/after the bad one)", len(executions))
	}
	if len(readErrs) != 1 {
		t.Fatalf("len(readErrs) = %d, want 1", len(readErrs))
	}
	if readErrs[0].Line != 2 {
		t.Errorf("readErrs[0].Line = %d, want 2 (the malformed line's number)", readErrs[0].Line)
	}
}

func TestRead_MissingRequiredField(t *testing.T) {
	content := `{"tenant":"t","namespace":"n","group":"g","timestamp":"2026-01-01T00:00:00Z"}
{"tenant":"t","namespace":"n","group":"g","rule_name":"r"}
`
	src := New(Config{Path: writeFile(t, content)})
	executions, readErrs, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(executions) != 0 {
		t.Fatalf("len(executions) = %d, want 0", len(executions))
	}
	if len(readErrs) != 2 {
		t.Fatalf("len(readErrs) = %d, want 2 (missing rule_name on line 1, missing timestamp on line 2)", len(readErrs))
	}
}

func TestRead_NonexistentFile(t *testing.T) {
	src := New(Config{Path: filepath.Join(t.TempDir(), "does-not-exist.ndjson")})
	_, _, err := src.Read(context.Background())
	if err == nil {
		t.Fatal("expected a top-level error for a missing file")
	}
}
