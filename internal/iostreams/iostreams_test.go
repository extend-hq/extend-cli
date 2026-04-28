package iostreams

import (
	"os"
	"testing"
)

func TestTestStreamsAreWriteable(t *testing.T) {
	ios, _, out, errOut := Test()
	if _, err := ios.Out.Write([]byte("hello")); err != nil {
		t.Fatalf("write Out: %v", err)
	}
	if _, err := ios.ErrOut.Write([]byte("oops")); err != nil {
		t.Fatalf("write ErrOut: %v", err)
	}
	if got := out.String(); got != "hello" {
		t.Errorf("Out = %q, want %q", got, "hello")
	}
	if got := errOut.String(); got != "oops" {
		t.Errorf("ErrOut = %q, want %q", got, "oops")
	}
}

func TestDetectColor(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		stdoutTTY bool
		want      bool
	}{
		{"tty no envs", nil, true, true},
		{"non-tty no envs", nil, false, false},
		{"NO_COLOR overrides tty", map[string]string{"NO_COLOR": "1"}, true, false},
		{"NO_COLOR empty also disables", map[string]string{"NO_COLOR": ""}, true, false},
		{"CLICOLOR=0 disables on tty", map[string]string{"CLICOLOR": "0"}, true, false},
		{"CLICOLOR_FORCE enables off-tty", map[string]string{"CLICOLOR_FORCE": "1"}, false, true},
		{"NO_COLOR beats CLICOLOR_FORCE", map[string]string{"NO_COLOR": "1", "CLICOLOR_FORCE": "1"}, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateColorEnv(t)
			for k, v := range tc.env {
				if err := os.Setenv(k, v); err != nil {
					t.Fatalf("setenv %s: %v", k, err)
				}
			}
			if got := detectColor(tc.stdoutTTY); got != tc.want {
				t.Errorf("detectColor(stdoutTTY=%v) = %v, want %v", tc.stdoutTTY, got, tc.want)
			}
		})
	}
}

func isolateColorEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE"} {
		prev, had := os.LookupEnv(k)
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unsetenv %s: %v", k, err)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(k, prev)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
}
