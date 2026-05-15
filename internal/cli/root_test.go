package cli

import (
	"testing"
	"time"

	"github.com/extend-hq/extend-cli/internal/client"
)

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
// SKILL.md catalog both read EnvVars to produce their tables; a new
// env var added without an entry here renders incomplete help.
func TestEnvHTTPTimeoutInEnvVarRegistry(t *testing.T) {
	var found bool
	for _, ev := range client.EnvVars {
		if ev.Name == client.EnvHTTPTimeout {
			found = true
			if ev.Description == "" {
				t.Errorf("EnvHTTPTimeout entry has empty Description")
			}
		}
	}
	if !found {
		t.Errorf("EnvHTTPTimeout must appear in client.EnvVars so 'extend help auth' documents it")
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
			got, ok := resolveHTTPTimeout(tc.flag, tc.env)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", ok, tc.wantOK)
			}
			if tc.wantOK && got != tc.want {
				t.Errorf("timeout = %v, want %v", got, tc.want)
			}
		})
	}
}
