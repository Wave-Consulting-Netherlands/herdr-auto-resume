package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	appconfig "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/config"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/revive"
	herdradapter "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/herdr"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/sessionfile"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

type reviveConfig struct {
	Runtime   string
	Transport string
	HerdrBin  string
	Socket    string
	StateFile string
}

func reviveCommand(args []string, stdout, stderr io.Writer) int {
	cfg, prefix, err := parseReviveFlags(args[1:], stderr)
	if err != nil {
		return 2
	}
	statePath := cfg.StateFile
	if statePath == "auto" || statePath == "" {
		statePath = store.DefaultPath()
	}
	if statePath == "off" {
		fmt.Fprintln(stderr, "error: revive requires a persistent --state-file")
		return 2
	}
	transport, err := resolveTransportDefault("herdr", cfg.Transport, "", cfg.Transport != "", stderr)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	runtimeCfg := runConfig{Runtime: "herdr", Transport: transport, HerdrBin: cfg.HerdrBin, Socket: cfg.Socket}
	rt, err := runtimeForRun(runtimeCfg)
	if err != nil {
		fmt.Fprintf(stderr, "error: initialize herdr runtime: %v\n", err)
		return 1
	}
	scanner, err := sessionfile.New(sessionfile.Config{StatePath: statePath})
	if err != nil {
		fmt.Fprintf(stderr, "error: initialize session discovery: %v\n", err)
		return 1
	}
	// Revive snapshots are intentionally unfiltered. The double-attach veto
	// must see panes outside any configured watcher workspace.
	spawner := herdradapter.New(herdradapter.Options{Bin: cfg.HerdrBin, SocketPath: cfg.Socket})
	op := revive.New(revive.Config{Scanner: scanner, Runtime: rt, Spawner: spawner, Log: stderr})
	if err := op.Run(prefix, stdout); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func parseReviveFlags(args []string, stderr io.Writer) (reviveConfig, string, error) {
	positionals, flagArgs := splitReviveArgs(args)
	fs := flag.NewFlagSet("revive", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := reviveConfig{Runtime: "herdr", HerdrBin: "herdr", StateFile: "auto"}
	configPath := fs.String("config", appconfig.DefaultPath(), "configuration file path")
	fs.StringVar(&cfg.Runtime, "runtime", "herdr", "runtime adapter; revive requires herdr")
	fs.StringVar(&cfg.Transport, "transport", "", "herdr transport: cli or socket")
	fs.StringVar(&cfg.HerdrBin, "herdr-bin", "herdr", "herdr binary path")
	fs.StringVar(&cfg.Socket, "socket", "", "herdr socket path")
	fs.StringVar(&cfg.StateFile, "state-file", "auto", "persistent state path, auto, or off")
	if err := fs.Parse(flagArgs); err != nil {
		return reviveConfig{}, "", err
	}
	explicit := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
	})
	fileConfig, found, err := appconfig.Load(*configPath)
	if err != nil {
		return reviveConfig{}, "", fmt.Errorf("load config: %w", err)
	}
	if explicit["config"] && !found {
		return reviveConfig{}, "", fmt.Errorf("config file %s was not found", *configPath)
	}
	if found {
		if fileConfig.Has("runtime.type") && !explicit["runtime"] {
			cfg.Runtime = fileConfig.Runtime.Type
		}
		if fileConfig.Has("runtime.transport") && !explicit["transport"] {
			cfg.Transport = fileConfig.Runtime.Transport
		}
		if fileConfig.Has("runtime.herdr_bin") && !explicit["herdr-bin"] {
			cfg.HerdrBin = fileConfig.Runtime.HerdrBin
		}
		if fileConfig.Has("runtime.socket") && !explicit["socket"] {
			cfg.Socket = fileConfig.Runtime.Socket
		}
		if !explicit["state-file"] && fileConfig.Has("state.file") {
			cfg.StateFile = fileConfig.State.File
		}
	}
	if cfg.Runtime != "herdr" {
		return reviveConfig{}, "", errors.New("revive requires runtime.type herdr; tmux has no persistent session identity")
	}
	if len(positionals) != 1 {
		return reviveConfig{}, "", errors.New("usage: herdr-auto-resume revive <session-id-prefix> [options]")
	}
	return cfg, positionals[0], nil
}

func splitReviveArgs(args []string) (positionals, flags []string) {
	valueFlags := map[string]bool{"--config": true, "--runtime": true, "--transport": true, "--herdr-bin": true, "--socket": true, "--state-file": true}
	for i := 0; i < len(args); i++ {
		name := args[i]
		base := name
		if index := strings.IndexByte(name, '='); index >= 0 {
			base = name[:index]
		}
		if valueFlags[base] {
			flags = append(flags, name)
			if !strings.Contains(name, "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positionals = append(positionals, name)
	}
	return positionals, flags
}
