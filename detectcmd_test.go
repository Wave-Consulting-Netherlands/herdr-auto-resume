package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectCommandPrintsAnalysisWithoutSending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(path, []byte("⎿ You've hit your limit · resets 3pm (UTC)\nOpening your options…\n❯"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if got := runCLI([]string{"detect", "--file", path}, &stdout, &stderr); got != 0 {
		t.Fatalf("detect exit = %d, stderr=%q", got, stderr.String())
	}
	for _, label := range []string{"IsLimited=", "Actionable=", "MenuVisible=", "Family=", "Kind=", "Timezone=", "ParsedTimeUTC=", "ParsedTimeLocal=", "Confidence=", "Evidence="} {
		if !strings.Contains(stdout.String(), label) {
			t.Errorf("detect output %q missing %q", stdout.String(), label)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("detect stderr = %q, want empty", stderr.String())
	}
}

func TestDetectCommandRequiresFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := runCLI([]string{"detect"}, &stdout, &stderr); got != 2 {
		t.Fatalf("detect exit = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "--file") {
		t.Fatalf("stderr = %q, want --file guidance", stderr.String())
	}
}
