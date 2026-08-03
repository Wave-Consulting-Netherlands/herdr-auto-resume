package main

import (
	"errors"
	"fmt"
	"io"
)

// resolveTransportDefault is the single transport policy used by run and doctor.
// An empty transport is the built-in default; a non-empty transport was set by
// YAML or an explicit flag and must not be silently changed.
func resolveTransportDefault(runtime, transport, session string, explicit bool, warning io.Writer) (string, error) {
	if explicit {
		if transport != "cli" && transport != "socket" {
			return "", fmt.Errorf("unsupported transport %q", transport)
		}
		if transport == "socket" && runtime != "herdr" {
			return "", errors.New("socket transport requires --runtime herdr")
		}
		if transport == "socket" && session != "" {
			return "", errors.New("--session is unsupported with socket transport; use --socket")
		}
		return transport, nil
	}

	if transport != "" {
		return "", fmt.Errorf("unsupported transport %q", transport)
	}
	if runtime == "herdr" && session == "" {
		return "socket", nil
	}

	reason := "--session"
	if runtime != "herdr" {
		reason = "--runtime " + runtime
		if session != "" {
			reason += " and --session"
		}
	}
	if warning != nil {
		fmt.Fprintf(warning, "warning: defaulting transport to cli because %s is incompatible with socket transport\n", reason)
	}
	return "cli", nil
}
