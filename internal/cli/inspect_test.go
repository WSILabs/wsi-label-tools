package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInspect_CMU1Small(t *testing.T) {
	p := filepath.Join("..", "..", "sample_files", "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("sample missing: %v", err)
	}
	var out, errOut bytes.Buffer
	err := Run([]string{"inspect", p}, &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v  stderr: %s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "IFDs:") {
		t.Errorf("unexpected stdout: %s", out.String())
	}
	if !strings.Contains(out.String(), "baseline") {
		t.Errorf("expected 'baseline' role in output: %s", out.String())
	}
}
