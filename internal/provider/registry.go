package provider

import "strings"

// Registry resolves a pane to one enabled provider. A non-empty agent hint is
// authoritative; content detection is used only when the hint is absent.
type Registry struct {
	providers []Provider
}

func NewRegistry(providers ...Provider) *Registry {
	copyOfProviders := make([]Provider, 0, len(providers))
	for _, candidate := range providers {
		if candidate != nil {
			copyOfProviders = append(copyOfProviders, candidate)
		}
	}
	return &Registry{providers: copyOfProviders}
}

// Resolve returns a provider only for an authoritative hint match or exactly
// one content match. Unknown hints and ambiguous content fail closed.
func (r *Registry) Resolve(agent, content string) Provider {
	if r == nil {
		return nil
	}
	if strings.TrimSpace(agent) != "" {
		for _, candidate := range r.providers {
			if strings.EqualFold(strings.TrimSpace(agent), candidate.Name()) {
				return candidate
			}
		}
		return nil
	}

	var match Provider
	for _, candidate := range r.providers {
		if !candidate.DetectContent(content) {
			continue
		}
		if match != nil {
			return nil
		}
		match = candidate
	}
	return match
}

// Providers returns the enabled providers in registry order.
func (r *Registry) Providers() []Provider {
	if r == nil {
		return nil
	}
	return append([]Provider(nil), r.providers...)
}
