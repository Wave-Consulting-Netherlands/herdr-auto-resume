package config

import (
	"fmt"
	"strings"
)

func validate(cfg Config) error {
	if cfg.Runtime.Type != "" && cfg.Runtime.Type != "herdr" && cfg.Runtime.Type != "tmux" {
		return fmt.Errorf("runtime.type %q is unsupported", cfg.Runtime.Type)
	}
	if cfg.Runtime.Transport != "" && cfg.Runtime.Transport != "cli" && cfg.Runtime.Transport != "socket" {
		return fmt.Errorf("runtime.transport %q is unsupported", cfg.Runtime.Transport)
	}
	if cfg.Runtime.Transport == "socket" && cfg.Runtime.Type == "tmux" {
		return fmt.Errorf("runtime.transport socket requires runtime.type herdr")
	}
	if cfg.Monitoring.AdmitSessionMatches && cfg.Runtime.Type == "tmux" {
		return fmt.Errorf("monitoring.admit_session_matches requires a session-identity runtime; runtime.type tmux has no agent session")
	}
	if cfg.Providers.SessionFileChannel && cfg.Runtime.Type == "tmux" {
		return fmt.Errorf("providers.session_file_channel requires a session-identity runtime; runtime.type tmux has no agent session")
	}
	if cfg.Monitoring.AdmitSessionMatches && !cfg.Providers.SessionFileChannel {
		return fmt.Errorf("monitoring.admit_session_matches requires providers.session_file_channel")
	}
	if cfg.Resume.AnswerLimitMenu && cfg.Runtime.Type == "tmux" {
		return fmt.Errorf("resume.answer_limit_menu requires a session-identity runtime; runtime.type tmux has no agent session")
	}
	if cfg.Monitoring.Interval < 0 {
		return fmt.Errorf("monitoring.interval must be positive")
	}
	if cfg.Monitoring.Lines < 0 {
		return fmt.Errorf("monitoring.lines must be positive")
	}
	if cfg.Monitoring.TransientMaxAttempts < 0 {
		return fmt.Errorf("monitoring.transient_max_attempts must be positive")
	}
	if cfg.Resume.Margin < 0 {
		return fmt.Errorf("resume.margin must be non-negative")
	}
	if cfg.Resume.MaxWait < 0 {
		return fmt.Errorf("resume.max_wait must be positive")
	}
	if cfg.Resume.VerifyTimeout < 0 {
		return fmt.Errorf("resume.verify_timeout must be positive")
	}
	if cfg.Providers.Enabled != nil {
		if len(cfg.Providers.Enabled) == 0 {
			return fmt.Errorf("providers.enabled must contain at least one provider")
		}
		seen := make(map[string]struct{}, len(cfg.Providers.Enabled))
		for _, provider := range cfg.Providers.Enabled {
			provider = strings.ToLower(strings.TrimSpace(provider))
			if provider != "claude" && provider != "codex" {
				return fmt.Errorf("providers.enabled contains unsupported provider %q", provider)
			}
			if _, ok := seen[provider]; ok {
				return fmt.Errorf("providers.enabled contains duplicate provider %q", provider)
			}
			seen[provider] = struct{}{}
		}
	}
	return nil
}
