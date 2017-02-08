// Copyright (C) 2015 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package sync

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/logger"
)

const (
	logThreshold = 100 * time.Millisecond
	shortWait    = 5 * time.Millisecond
	longWait     = 125 * time.Millisecond
)

var skipTimingTests = false

func init() {
	// Check a few times that a short sleep does not in fact overrun the log
	// threshold. If it does, the timer accuracy is crap or the host is
	// overloaded and we can't reliably run the tests in here. In the normal
	// case this takes just 25*5 = 125 ms.
	for i := 0; i < 25; i++ {
		t0 := time.Now()
		time.Sleep(shortWait)
		if time.Since(t0) > logThreshold {
			skipTimingTests = true
			return
		}
	}
}

func TestTypes(t *testing.T) {
	debug = false
	l.SetDebug("sync", false)

	if _, ok := NewMutex().(*sync.Mutex); !ok {
		t.Error("Wrong type")
	}

	if _, ok := NewRWMutex().(*sync.RWMutex); !ok {
		t.Error("Wrong type")
	}

	if _, ok := NewWaitGroup().(*sync.WaitGroup); !ok {
		t.Error("Wrong type")
	}

	debug = true
	l.SetDebug("sync", true)

	if _, ok := NewMutex().(*loggedMutex); !ok {
		t.Error("Wrong type")
	}

	if _, ok := NewRWMutex().(*loggedRWMutex); !ok {
		t.Error("Wrong type")
	}

	if _, ok := NewWaitGroup().(*loggedWaitGroup); !ok {
		t.Error("Wrong type")
	}

	debug = false
	l.SetDebug("sync", false)
}

func TestMutex(t *testing.T) {
	if skipTimingTests {
		t.Skip("insufficient timer accuracy")
		return
	}

	debug = true
	l.SetDebug("sync", true)
	threshold = logThreshold

	msgmut := sync.Mutex{}
	var messages []string

	l.AddHandler(logger.LevelDebug, func(_ logger.LogLevel, message string) {
		msgmut.Lock()
		messages = append(messages, message)
		msgmut.Unlock()
	})

	mut := NewMutex()
	mut.Lock()
	time.Sleep(shortWait)
	mut.Unlock()

	if len(messages) > 0 {
		t.Errorf("Unexpected message count")
	}

	mut.Lock()
	time.Sleep(longWait)
	mut.Unlock()

	if len(messages) != 1 {
		t.Errorf("Unexpected message count")
	}

	debug = false
	l.SetDebug("sync", false)
}

func TestRWMutex(t *testing.T) {
	if skipTimingTests {
		t.Skip("insufficient timer accuracy")
		return
	}

	debug = true
	l.SetDebug("sync", true)
	threshold = logThreshold

	msgmut := sync.Mutex{}
	var messages []string

	l.AddHandler(logger.LevelDebug, func(_ logger.LogLevel, message string) {
		msgmut.Lock()
		messages = append(messages, message)
		msgmut.Unlock()
	})

	mut := NewRWMutex()
	mut.Lock()
	time.Sleep(shortWait)
	mut.Unlock()

	if len(messages) > 0 {
		t.Errorf("Unexpected message count")
	}

	mut.Lock()
	time.Sleep(longWait)
	mut.Unlock()

	if len(messages) != 1 {
		t.Errorf("Unexpected message count")
	}

	// Testing rlocker logging
	mut.RLock()
	go func() {
		time.Sleep(longWait)
		mut.RUnlock()
	}()

	mut.Lock()
	_ = 1 // skip empty critical section check
	mut.Unlock()

	if len(messages) != 2 {
		t.Errorf("Unexpected message count")
	}
	if !strings.Contains(messages[1], "RUnlockers while locking:\nat sync") || !strings.Contains(messages[1], "sync_test.go:") {
		t.Error("Unexpected message")
	}

	// Testing multiple rlockers
	mut.RLock()
	mut.RLock()
	mut.RLock()
	_ = 1 // skip empty critical section check
	mut.RUnlock()
	mut.RUnlock()
	mut.RUnlock()

	debug = false
	l.SetDebug("sync", false)
}

func TestWaitGroup(t *testing.T) {
	if skipTimingTests {
		t.Skip("insufficient timer accuracy")
		return
	}

	debug = true
	l.SetDebug("sync", true)
	threshold = logThreshold

	msgmut := sync.Mutex{}
	var messages []string

	l.AddHandler(logger.LevelDebug, func(_ logger.LogLevel, message string) {
		msgmut.Lock()
		messages = append(messages, message)
		msgmut.Unlock()
	})

	wg := NewWaitGroup()
	wg.Add(1)
	go func() {
		time.Sleep(shortWait)
		wg.Done()
	}()
	wg.Wait()

	if len(messages) > 0 {
		t.Errorf("Unexpected message count")
	}

	wg = NewWaitGroup()
	wg.Add(1)
	go func() {
		time.Sleep(longWait)
		wg.Done()
	}()
	wg.Wait()

	if len(messages) != 1 {
		t.Errorf("Unexpected message count")
	}

	debug = false
	l.SetDebug("sync", false)
}

func TestTimeoutCond(t *testing.T) {
	// WARNING this test relies heavily on threads not being stalled at particular points.
	// As such, it's pretty unstable on the build server. It has been left in as it still
	// exercises the deadlock detector, and one of the two things it tests is still functional.
	// See the comments in runLocks

	const (
		// Low values to avoid being intrusive in continuous testing. Can be
		// increased significantly for stress testing.
		iterations = 100
		routines   = 10

		timeMult = 2
	)

	c := NewTimeoutCond(NewMutex())

	// Start a routine to periodically broadcast on the cond.

	go func() {
		d := time.Duration(routines) * timeMult * time.Millisecond / 2
		t.Log("Broadcasting every", d)
		for i := 0; i < iterations; i++ {
			time.Sleep(d)

			c.L.Lock()
			c.Broadcast()
			c.L.Unlock()
		}
	}()

	// Start several routines that wait on it with different timeouts.

	var results [routines][2]int
	var wg sync.WaitGroup
	for i := 0; i < routines; i++ {
		i := i
		wg.Add(1)
		go func() {
			d := time.Duration(i) * timeMult * time.Millisecond
			t.Logf("Routine %d waits for %v\n", i, d)
			succ, fail := runLocks(t, iterations, c, d)
			results[i][0] = succ
			results[i][1] = fail
			wg.Done()
		}()
	}

	wg.Wait()

	// Print a table of routine number: successes, failures.

	for i, v := range results {
		t.Logf("%4d: %4d %4d\n", i, v[0], v[1])
	}
}

func runLocks(t *testing.T, iterations int, c *TimeoutCond, d time.Duration) (succ, fail int) {
	for i := 0; i < iterations; i++ {
		c.L.Lock()

		// The thread may be stalled, so we can't test the 'succeeded late' case reliably.
		// Therefore make sure that we start t0 before starting the timeout, and only test
		// the 'failed early' case.

		t0 := time.Now()
		w := c.SetupWait(d)

		res := w.Wait()
		waited := time.Since(t0)

		// Allow 20% slide in either direction, and a five milliseconds of
		// scheduling delay... In tweaking these it was clear that things
		// worked like the should, so if this becomes a spurious failure
		// kind of thing feel free to remove or give significantly more
		// slack.

		if !res && waited < d*8/10 {
			t.Errorf("Wait failed early, %v < %v", waited, d)
		}
		if res && waited > d*11/10+5*time.Millisecond {
			// Ideally this would be t.Errorf
			t.Logf("WARNING: Wait succeeded late, %v > %v. This is probably a thread scheduling issue", waited, d)
		}

		w.Stop()

		if res {
			succ++
		} else {
			fail++
		}
		c.L.Unlock()
	}
	return
}
