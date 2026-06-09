package cli

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestUnconfiguredKeyError pins the contract of the "no API key" error:
// it names the right key env var (incl. an --env label), points at
// `extend setup`, and links the dashboard for the resolved region, with
// US as the fallback for an unset or unknown region.
func TestUnconfiguredKeyError(t *testing.T) {
	cases := []struct {
		name, keyEnv, region, wantDash string
	}{
		{"unset region → US", "EXTEND_API_KEY", "", "https://dashboard.extend.ai"},
		{"eu", "EXTEND_API_KEY", "eu", "https://dashboard.eu1.extend.ai"},
		{"legacy us2 still resolves", "EXTEND_API_KEY", "us2", "https://dashboard.us2.extend.app"},
		{"unknown → US fallback", "EXTEND_API_KEY", "xx", "https://dashboard.extend.ai"},
		{"env-label key name", "EXTEND_TEST_API_KEY", "", "https://dashboard.extend.ai"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := unconfiguredKeyError(tc.keyEnv, tc.region, nil).Error()
			for _, want := range []string{tc.keyEnv, tc.wantDash, "extend setup"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q missing %q", msg, want)
				}
			}
		})
	}
}

// TestUnconfiguredKeyErrorReportsUnreadableConfig pins the hint that turns a
// silent "key not set" into a diagnosable one: when a config file was present
// but could not be read or parsed, the error must say so (and name 'extend
// config') so the user stops hunting for a missing key that's actually there.
// This is the exact trap a shadowed/older binary or an unreadable file hits.
func TestUnconfiguredKeyErrorReportsUnreadableConfig(t *testing.T) {
	fileErr := errors.New("parse /home/u/.config/extend/config.json: unexpected end of JSON input")
	msg := unconfiguredKeyError("EXTEND_API_KEY", "us", fileErr).Error()
	for _, want := range []string{
		"EXTEND_API_KEY",         // still names the key var
		"could not be read",      // the new hint
		"extend config",          // where to look
		"unexpected end of JSON", // surfaces the underlying cause
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}

	// Without a file error the hint must not appear (no false alarm when the
	// user simply hasn't configured anything yet).
	if msg := unconfiguredKeyError("EXTEND_API_KEY", "us", nil).Error(); strings.Contains(msg, "could not be read") {
		t.Errorf("error %q should not mention an unreadable config when there was none", msg)
	}
}

// TestHTTPTimeoutFlagRegistered checks the persistent --http-timeout
// flag is wired on the production root. Without it, `--http-timeout`
// would silently fall through cobra parsing and confuse users who
// followed the help.
func TestHTTPTimeoutFlagRegistered(t *testing.T) {
	root := NewRoot()
	flag := root.PersistentFlags().Lookup("http-timeout")
	if flag == nil {
		t.Fatal("--http-timeout persistent flag must be registered on root")
	}
	if flag.Value.Type() != "duration" {
		t.Errorf("--http-timeout type = %q, want duration", flag.Value.Type())
	}
	if flag.DefValue != "0s" {
		t.Errorf("--http-timeout default = %q, want 0s (unset → use client default)", flag.DefValue)
	}
}

// TestEnvHTTPTimeoutInEnvVarRegistry guards against drift in the
// canonical env-var list. Help rendering (`extend help auth`) and the
// SKILL.md catalog both read envVars to produce their tables; a new
// env var added without an entry here renders incomplete help.
func TestEnvHTTPTimeoutInEnvVarRegistry(t *testing.T) {
	var found bool
	for _, ev := range envVars {
		if ev.Name == envHTTPTimeout {
			found = true
			if ev.Description == "" {
				t.Errorf("envHTTPTimeout entry has empty Description")
			}
		}
	}
	if !found {
		t.Errorf("envHTTPTimeout must appear in envVars so 'extend help auth' documents it")
	}
}

// TestResolveHTTPTimeout pins the precedence rules between --http-timeout
// and EXTEND_HTTP_TIMEOUT. The matrix below covers the cases agents and
// scripts will actually hit: a flag overriding env, a flag alone, env
// alone, a malformed env value getting ignored (the test cases marked
// `wantOK=false` — silent fallback so a typo doesn't break every
// command), and a zero env value mapping to "disable the http.Client
// timeout entirely". The "both unset" row encodes the contract that
// an unset flag (Duration zero) and an unset env leave the client
// default (DefaultHTTPTimeout) in place.
func TestResolveHTTPTimeout(t *testing.T) {
	cases := []struct {
		name   string
		flag   time.Duration
		env    string
		want   time.Duration
		wantOK bool
	}{
		{"flag wins over env", 30 * time.Second, "2m", 30 * time.Second, true},
		{"flag alone", 45 * time.Second, "", 45 * time.Second, true},
		{"env alone", 0, "2m", 2 * time.Minute, true},
		{"env disables timeout", 0, "0s", 0, true},
		{"malformed env ignored", 0, "not-a-duration", 0, false},
		{"both unset", 0, "", 0, false},
		{"negative env ignored", 0, "-5s", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, _ := resolveHTTPTimeout(tc.flag, tc.env)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", ok, tc.wantOK)
			}
			if tc.wantOK && got != tc.want {
				t.Errorf("timeout = %v, want %v", got, tc.want)
			}
		})
	}
}
