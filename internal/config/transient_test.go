package config

import (
	"testing"
)

func TestLoadParsesTransientRetryIncludingFalse(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want bool
	}{
		{name: "true", data: "version: 1\nmonitoring:\n  transient_retry: true\n", want: true},
		{name: "false", data: "version: 1\nmonitoring:\n  transient_retry: false\n", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, found, err := Load(writeConfig(t, tc.data))
			if err != nil || !found || !cfg.Has("monitoring.transient_retry") || cfg.Monitoring.TransientRetry != tc.want {
				t.Fatalf("cfg=%#v found=%v err=%v, want transient_retry=%v", cfg, found, err, tc.want)
			}
		})
	}
}

func TestLoadParsesTransientMaxAttempts(t *testing.T) {
	cfg, found, err := Load(writeConfig(t, "version: 1\nmonitoring:\n  transient_max_attempts: 7\n"))
	if err != nil || !found || cfg.Monitoring.TransientMaxAttempts != 7 || !cfg.Has("monitoring.transient_max_attempts") {
		t.Fatalf("cfg=%#v found=%v err=%v, want transient_max_attempts=7", cfg, found, err)
	}
}
