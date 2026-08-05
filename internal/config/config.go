// Package config loads the small, strict YAML configuration surface.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version    int
	Runtime    RuntimeConfig
	Monitoring MonitoringConfig
	Resume     ResumeConfig
	Providers  ProvidersConfig
	State      StateConfig
	present    map[string]bool
}

func (c Config) Has(field string) bool { return c.present[field] }

type RuntimeConfig struct {
	Type      string
	Transport string
	HerdrBin  string
	Socket    string
	Workspace string
}

type MonitoringConfig struct {
	Panes               []string
	Interval            time.Duration
	Lines               int
	WaitForPanes        bool
	AdmitSessionMatches bool
	AdmitAgentEvents    bool
}

type ResumeConfig struct {
	Margin          time.Duration
	MaxWait         time.Duration
	VerifyTimeout   time.Duration
	AnswerLimitMenu bool
}

type ProvidersConfig struct {
	Enabled            []string
	ClaudePrompt       string
	CodexPrompt        string
	SessionFileChannel bool
}

type StateConfig struct {
	File string
}

type rawConfig struct {
	Version    *int                `yaml:"version"`
	Runtime    rawRuntimeConfig    `yaml:"runtime"`
	Monitoring rawMonitoringConfig `yaml:"monitoring"`
	Resume     rawResumeConfig     `yaml:"resume"`
	Providers  rawProvidersConfig  `yaml:"providers"`
	State      rawStateConfig      `yaml:"state"`
}

type rawRuntimeConfig struct {
	Type      string  `yaml:"type"`
	Transport *string `yaml:"transport"`
	HerdrBin  string  `yaml:"herdr_bin"`
	Socket    *string `yaml:"socket"`
	Workspace string  `yaml:"workspace"`
}

type rawMonitoringConfig struct {
	Panes               []string `yaml:"panes"`
	Interval            string   `yaml:"interval"`
	Lines               *int     `yaml:"lines"`
	WaitForPanes        *bool    `yaml:"wait_for_panes"`
	AdmitSessionMatches *bool    `yaml:"admit_session_matches"`
	AdmitAgentEvents    *bool    `yaml:"admit_agent_events"`
}

type rawResumeConfig struct {
	Margin          string `yaml:"margin"`
	MaxWait         string `yaml:"max_wait"`
	VerifyTimeout   string `yaml:"verify_timeout"`
	AnswerLimitMenu *bool  `yaml:"answer_limit_menu"`
}

type rawProvidersConfig struct {
	Enabled            []string `yaml:"enabled"`
	ClaudePrompt       string   `yaml:"claude_prompt"`
	CodexPrompt        string   `yaml:"codex_prompt"`
	SessionFileChannel *bool    `yaml:"session_file_channel"`
}

type rawStateConfig struct {
	File string `yaml:"file"`
}

// DefaultPath returns the per-user configuration path, honoring XDG_CONFIG_HOME.
func DefaultPath() string {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, "herdr-auto-resume", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".config", "herdr-auto-resume", "config.yaml")
	}
	return filepath.Join(home, ".config", "herdr-auto-resume", "config.yaml")
}

// Load reads path. A missing file is not an error and returns found=false.
func Load(path string) (Config, bool, error) {
	path, err := expandPath(path)
	if err != nil {
		return Config{}, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var raw rawConfig
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, true, fmt.Errorf("decode config %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, true, fmt.Errorf("config %s: multiple YAML documents are not supported", path)
		}
		return Config{}, true, fmt.Errorf("decode config %s: %w", path, err)
	}
	if raw.Version == nil {
		return Config{}, true, fmt.Errorf("config %s: version is required", path)
	}
	if *raw.Version != 1 {
		return Config{}, true, fmt.Errorf("config %s: unsupported version %d (want 1)", path, *raw.Version)
	}

	parsed := Config{
		Version: 1,
		Runtime: RuntimeConfig{
			Type:      raw.Runtime.Type,
			HerdrBin:  raw.Runtime.HerdrBin,
			Workspace: raw.Runtime.Workspace,
		},
		Monitoring: MonitoringConfig{Panes: append([]string(nil), raw.Monitoring.Panes...)},
		Providers: ProvidersConfig{
			Enabled:      append([]string(nil), raw.Providers.Enabled...),
			ClaudePrompt: raw.Providers.ClaudePrompt, CodexPrompt: raw.Providers.CodexPrompt,
		},
		State: StateConfig{File: raw.State.File},
	}
	parsed.present = make(map[string]bool)
	mark := func(field string, present bool) {
		if present {
			parsed.present[field] = true
		}
	}
	mark("runtime.type", raw.Runtime.Type != "")
	mark("runtime.transport", raw.Runtime.Transport != nil)
	mark("runtime.herdr_bin", raw.Runtime.HerdrBin != "")
	mark("runtime.socket", raw.Runtime.Socket != nil)
	mark("runtime.workspace", raw.Runtime.Workspace != "")
	mark("monitoring.panes", raw.Monitoring.Panes != nil)
	mark("monitoring.interval", raw.Monitoring.Interval != "")
	mark("monitoring.lines", raw.Monitoring.Lines != nil)
	mark("monitoring.wait_for_panes", raw.Monitoring.WaitForPanes != nil)
	mark("monitoring.admit_session_matches", raw.Monitoring.AdmitSessionMatches != nil)
	mark("monitoring.admit_agent_events", raw.Monitoring.AdmitAgentEvents != nil)
	mark("resume.margin", raw.Resume.Margin != "")
	mark("resume.max_wait", raw.Resume.MaxWait != "")
	mark("resume.verify_timeout", raw.Resume.VerifyTimeout != "")
	mark("resume.answer_limit_menu", raw.Resume.AnswerLimitMenu != nil)
	mark("providers.enabled", raw.Providers.Enabled != nil)
	mark("providers.claude_prompt", raw.Providers.ClaudePrompt != "")
	mark("providers.codex_prompt", raw.Providers.CodexPrompt != "")
	mark("providers.session_file_channel", raw.Providers.SessionFileChannel != nil)
	mark("state.file", raw.State.File != "")
	if raw.Runtime.Transport != nil {
		parsed.Runtime.Transport = *raw.Runtime.Transport
	}
	if raw.Providers.SessionFileChannel != nil {
		parsed.Providers.SessionFileChannel = *raw.Providers.SessionFileChannel
	}
	if raw.Runtime.Socket != nil {
		parsed.Runtime.Socket = *raw.Runtime.Socket
	}
	if raw.Monitoring.WaitForPanes != nil {
		parsed.Monitoring.WaitForPanes = *raw.Monitoring.WaitForPanes
	}
	if raw.Monitoring.AdmitSessionMatches != nil {
		parsed.Monitoring.AdmitSessionMatches = *raw.Monitoring.AdmitSessionMatches
	}
	if raw.Monitoring.AdmitAgentEvents != nil {
		parsed.Monitoring.AdmitAgentEvents = *raw.Monitoring.AdmitAgentEvents
	}
	if raw.Resume.AnswerLimitMenu != nil {
		parsed.Resume.AnswerLimitMenu = *raw.Resume.AnswerLimitMenu
	}
	if raw.Monitoring.Interval != "" {
		parsed.Monitoring.Interval, err = time.ParseDuration(raw.Monitoring.Interval)
		if err != nil {
			return Config{}, true, fmt.Errorf("config monitoring.interval: %w", err)
		}
		if parsed.Monitoring.Interval <= 0 {
			return Config{}, true, fmt.Errorf("config monitoring.interval: must be positive")
		}
	}
	if raw.Monitoring.Lines != nil {
		parsed.Monitoring.Lines = *raw.Monitoring.Lines
		if parsed.Monitoring.Lines <= 0 {
			return Config{}, true, fmt.Errorf("config monitoring.lines: must be positive")
		}
	}
	if raw.Resume.Margin != "" {
		parsed.Resume.Margin, err = time.ParseDuration(raw.Resume.Margin)
		if err != nil {
			return Config{}, true, fmt.Errorf("config resume.margin: %w", err)
		}
	}
	if raw.Resume.MaxWait != "" {
		parsed.Resume.MaxWait, err = time.ParseDuration(raw.Resume.MaxWait)
		if err != nil {
			return Config{}, true, fmt.Errorf("config resume.max_wait: %w", err)
		}
		if parsed.Resume.MaxWait <= 0 {
			return Config{}, true, fmt.Errorf("config resume.max_wait: must be positive")
		}
	}
	if raw.Resume.VerifyTimeout != "" {
		parsed.Resume.VerifyTimeout, err = time.ParseDuration(raw.Resume.VerifyTimeout)
		if err != nil {
			return Config{}, true, fmt.Errorf("config resume.verify_timeout: %w", err)
		}
		if parsed.Resume.VerifyTimeout <= 0 {
			return Config{}, true, fmt.Errorf("config resume.verify_timeout: must be positive")
		}
	}
	if parsed.Runtime.Socket, err = expandPath(parsed.Runtime.Socket); err != nil {
		return Config{}, true, err
	}
	if parsed.State.File, err = expandPath(parsed.State.File); err != nil {
		return Config{}, true, err
	}
	if err := validate(parsed); err != nil {
		return Config{}, true, fmt.Errorf("validate config %s: %w", path, err)
	}
	return parsed, true, nil
}

func expandPath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand %q: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}
