package ui

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Spinner is a lightweight terminal spinner for long-running waits. On a
// non-terminal (pipe/CI) it degrades to nothing — the caller's plain status
// lines carry the information instead.
type Spinner struct {
	mu     sync.Mutex
	msg    string
	stop   chan struct{}
	done   chan struct{}
	active bool
}

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewSpinner creates a spinner with an initial message (not yet started).
func NewSpinner(msg string) *Spinner { return &Spinner{msg: msg} }

// Start begins animating on stderr. No-op off a TTY.
func (s *Spinner) Start() {
	if !IsTerminal() || s.active {
		return
	}
	s.active = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		t := time.NewTicker(90 * time.Millisecond)
		defer t.Stop()
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Fprint(os.Stderr, "\r\033[K") // clear the line
				return
			case <-t.C:
				s.mu.Lock()
				m := s.msg
				s.mu.Unlock()
				fmt.Fprintf(os.Stderr, "\r%s %s", Cyan(frames[i%len(frames)]), m)
				i++
			}
		}
	}()
}

// Update changes the spinner's message in place.
func (s *Spinner) Update(msg string) {
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// Stop halts the animation and clears the line. Safe to call more than once.
func (s *Spinner) Stop() {
	if !s.active {
		return
	}
	s.active = false
	close(s.stop)
	<-s.done
}
