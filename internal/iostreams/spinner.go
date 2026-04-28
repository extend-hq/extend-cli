package iostreams

import (
	"fmt"
	"io"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type Spinner struct {
	out     io.Writer
	enabled bool
	mu      sync.Mutex
	msg     string
	stop    chan struct{}
	done    chan struct{}
}

func (s *IOStreams) StartSpinner(msg string) *Spinner {
	sp := &Spinner{
		out:     s.ErrOut,
		enabled: s.IsStderrTTY(),
		msg:     msg,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	if !sp.enabled {
		fmt.Fprintln(sp.out, msg)
		close(sp.done)
		return sp
	}
	go sp.run()
	return sp
}

func (s *Spinner) Update(msg string) {
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

func (s *Spinner) Stop(finalMsg string) {
	if s.enabled {
		select {
		case <-s.done:
		default:
			close(s.stop)
			<-s.done
		}
	}
	if finalMsg != "" {
		fmt.Fprintln(s.out, finalMsg)
	}
}

func (s *Spinner) run() {
	defer close(s.done)
	t := time.NewTicker(80 * time.Millisecond)
	defer t.Stop()
	i := 0
	for {
		select {
		case <-s.stop:
			fmt.Fprint(s.out, "\r\033[K")
			return
		case <-t.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			fmt.Fprintf(s.out, "\r\033[K%s %s", spinnerFrames[i%len(spinnerFrames)], msg)
			i++
		}
	}
}
