package iostreams

import (
	"strings"
	"testing"
	"time"
)

func TestSpinner_NonTTYPrintsMessageOnce(t *testing.T) {
	ios, _, _, errOut := Test()
	sp := ios.StartSpinner("starting...")
	sp.Update("middle")
	sp.Update("almost there")
	sp.Stop("done!")

	got := errOut.String()
	if !strings.Contains(got, "starting...") {
		t.Errorf("expected initial message in output, got: %q", got)
	}
	if !strings.Contains(got, "done!") {
		t.Errorf("expected final message in output, got: %q", got)
	}
	if strings.Contains(got, "\033[") {
		t.Errorf("non-TTY output should not include ANSI escapes, got: %q", got)
	}
}

func TestSpinner_StopWithoutFinalMsgIsClean(t *testing.T) {
	ios, _, _, errOut := Test()
	sp := ios.StartSpinner("starting")
	sp.Stop("")
	got := errOut.String()
	if strings.TrimSpace(got) != "starting" {
		t.Errorf("expected only the initial message, got: %q", got)
	}
}

func TestSpinner_StopIdempotent(t *testing.T) {
	ios, _, _, _ := Test()
	sp := ios.StartSpinner("starting")
	sp.Stop("")
	sp.Stop("")
}

func TestSpinner_GoroutinePathWithUpdate(t *testing.T) {
	ios, _, _, errOut := Test()
	ios.stderrTTY = true
	sp := ios.StartSpinner("first")
	for i := 0; i < 20; i++ {
		sp.Update("status-" + string(rune('A'+i%26)))
		time.Sleep(15 * time.Millisecond)
	}
	sp.Stop("done")
	got := errOut.String()
	if !strings.Contains(got, "done") {
		t.Errorf("expected 'done' final message, got: %q", got)
	}
	if !strings.Contains(got, "status-") {
		t.Errorf("expected spinner to render at least one status update, got: %q", got)
	}
}

func TestSpinner_ConcurrentUpdates(t *testing.T) {
	ios, _, _, _ := Test()
	ios.stderrTTY = true
	sp := ios.StartSpinner("base")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			sp.Update("a")
		}
	}()
	for i := 0; i < 100; i++ {
		sp.Update("b")
	}
	<-done
	sp.Stop("")
}
