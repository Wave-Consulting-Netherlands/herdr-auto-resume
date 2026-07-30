package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/walt-verweij/herdr-auto-resume/internal/runtime"
	herdradapter "github.com/walt-verweij/herdr-auto-resume/internal/runtime/herdr"
)

func passingDoctorDeps() doctorDeps {
	return doctorDeps{
		resolve: func(string) (string, error) { return "/usr/bin/herdr", nil },
		run: func(_ string, args ...string) ([]byte, error) {
			switch strings.Join(args, " ") {
			case "--version":
				return []byte("herdr 0.7.5\n"), nil
			case "status":
				return []byte(`{"protocol":17}`), nil
			case "api schema --json":
				return []byte(`{"schema":"ok"}`), nil
			default:
				return nil, nil
			}
		},
		socket: func(string) error { return nil },
		home:   func() (string, error) { return "/home/user", nil },
		newAdapter: func(herdradapter.Options, herdradapter.ExecFunc) runtime.Runtime {
			return &runtime.Fake{PanesList: []runtime.Pane{{ID: "w1:p1"}}}
		},
	}
}

func TestDoctorReportPassesAllChecks(t *testing.T) {
	var out bytes.Buffer
	if got := runDoctorCommand(nil, &out, passingDoctorDeps()); got != 0 {
		t.Fatalf("doctor exit = %d, want 0\n%s", got, out.String())
	}
	if strings.Contains(out.String(), "FAIL") || !strings.Contains(out.String(), "PASS binary") || !strings.Contains(out.String(), "PASS adapter") {
		t.Fatalf("report = %q", out.String())
	}
}

func TestDoctorReportFailsWhenRequiredCheckFails(t *testing.T) {
	deps := passingDoctorDeps()
	deps.resolve = func(string) (string, error) { return "", errors.New("not found") }
	deps.socket = func(string) error { return errors.New("missing") }
	deps.run = func(_ string, _ ...string) ([]byte, error) { return nil, errors.New("unavailable") }
	deps.newAdapter = func(herdradapter.Options, herdradapter.ExecFunc) runtime.Runtime {
		return &runtime.Fake{Errs: map[string]error{"ListPanes": errors.New("list failed")}}
	}
	var out bytes.Buffer
	if got := runDoctorCommand(nil, &out, deps); got == 0 {
		t.Fatalf("doctor exit = %d, want nonzero\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "FAIL binary") || !strings.Contains(out.String(), "FAIL socket") || !strings.Contains(out.String(), "FAIL status") || !strings.Contains(out.String(), "FAIL adapter") {
		t.Fatalf("report = %q", out.String())
	}
}

func TestDoctorSchemaParseFailureIsWarning(t *testing.T) {
	deps := passingDoctorDeps()
	deps.run = func(_ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "api schema --json" {
			return []byte("{\x01}"), nil
		}
		return passingDoctorDeps().run("herdr", args...)
	}
	var out bytes.Buffer
	if got := runDoctorCommand(nil, &out, deps); got != 0 {
		t.Fatalf("doctor exit = %d, want 0\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "WARN schema") {
		t.Fatalf("report = %q, want schema warning", out.String())
	}
}
