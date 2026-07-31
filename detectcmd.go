package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/codex"
)

func detectCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "read pane content from this file")
	providerName := fs.String("provider", "claude", "provider to analyze: claude or codex")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "error: detect requires --file path")
		return 2
	}
	content, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintf(stderr, "error: read fixture: %v\n", err)
		return 1
	}
	var selected provider.Provider
	switch *providerName {
	case "claude":
		selected = claude.New("")
	case "codex":
		selected = codex.New("")
	default:
		fmt.Fprintf(stderr, "error: unsupported provider %q\n", *providerName)
		return 2
	}
	now := time.Now()
	analysis := selected.Analyze(string(content), now)
	fmt.Fprintf(stdout, "IsLimited=%t\nActionable=%t\nMenuVisible=%t\nFamily=%s\nKind=%s\nTimezone=%s\n", analysis.IsLimited, analysis.Actionable, analysis.MenuVisible, analysis.Family, analysis.Reset.Kind, analysis.Reset.Timezone)
	if analysis.Reset.ParsedTime.IsZero() {
		fmt.Fprintln(stdout, "ParsedTimeUTC=-")
		fmt.Fprintln(stdout, "ParsedTimeLocal=-")
	} else {
		fmt.Fprintf(stdout, "ParsedTimeUTC=%s\nParsedTimeLocal=%s\n", analysis.Reset.ParsedTime.UTC().Format(time.RFC3339), analysis.Reset.ParsedTime.In(now.Location()).Format(time.RFC3339))
	}
	fmt.Fprintf(stdout, "Confidence=%s\nEvidence=%s\n", analysis.Reset.Confidence, analysis.Evidence)
	return 0
}
