package mcpsrv

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestLockTableEmptiesOut(t *testing.T) {
	s := New(nil)
	for _, root := range []string{"/one", "/two", "/one"} {
		s.lock(root)()
	}
	if len(s.locks) != 0 {
		t.Errorf("%d entries left in the lock table, want 0", len(s.locks))
	}
}

func TestLockTableEmptiesOutUnderContention(t *testing.T) {
	s := New(nil)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, root := range []string{"/one", "/two"} {
				s.lock(root)()
			}
		}()
	}
	wg.Wait()

	if len(s.locks) != 0 {
		t.Errorf("%d entries left in the lock table, want 0", len(s.locks))
	}
}

func TestOneRootIsOneLock(t *testing.T) {
	s := New(nil)
	unlock := s.lock("/one")

	s.mu.Lock()
	l, ok := s.locks["/one"]
	s.mu.Unlock()
	if !ok {
		t.Fatal("the held lock is missing from the table")
	}

	done := make(chan struct{})
	go func() {
		s.lock("/one")()
		close(done)
	}()

	// A waiter joins the entry that is already there instead of making its own,
	// which is what keeps two spellings of one tree on one lock.
	deadline := time.After(10 * time.Second)
	for {
		s.mu.Lock()
		refs, entries := l.refs, len(s.locks)
		s.mu.Unlock()
		if entries != 1 {
			t.Fatalf("%d entries in the lock table, want 1", entries)
		}
		if refs == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the waiter never joined the held lock")
		default:
			runtime.Gosched()
		}
	}

	unlock()
	<-done

	if l.refs != 0 || len(s.locks) != 0 {
		t.Errorf("refs = %d, table = %d, want 0 and 0", l.refs, len(s.locks))
	}
}
