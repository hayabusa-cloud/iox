// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"code.hybscloud.com/iox"
)

func TestWriteStepPipeReadiness(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	raw, err := w.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	page := make([]byte, os.Getpagesize())
	n, err := w.Write(page)
	if err != nil {
		t.Fatal(err)
	}
	fill := int64(n)
	if err = w.SetWriteDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	for {
		n, err := w.Write(page)
		fill += int64(n)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	probes := 0
	dst := stepWriter(func(p []byte) (int, error) {
		probes++
		n, err := w.Write(p)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return n, iox.ErrWouldBlock
		}
		return n, err
	})
	registered := make(chan struct{})
	polled := make(chan error, 1)
	gate := stepTrigger[struct{}](func(_ iox.Ticket, wake iox.Signal[struct{}]) error {
		go func() {
			first := true
			err := raw.Write(func(uintptr) bool {
				if first {
					first = false
					close(registered)
					return false // Ask the runtime poller to wait for writability.
				}
				wake.Deliver(struct{}{})
				return true
			})
			if first { // A poller error can precede the first callback.
				close(registered)
			}
			polled <- err
		}()
		return nil
	})
	step := iox.WriteStep(dst, []byte("AB"), iox.Ticket{Demand: 1}, gate)
	o, seen := step.Observation()
	_, _, wait := step.Next()
	if !seen || o != (iox.Observation{Err: iox.ErrWouldBlock}) || wait.IsZero() {
		t.Fatal("first write", o, wait)
	}
	deadline := time.Now().Add(3 * time.Second)
	if err = w.SetWriteDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err = r.SetReadDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	q := make(chan iox.Ready[int64], 1)
	if err = wait.Arm(func(r iox.Ready[int64]) { q <- r }); err != nil {
		t.Fatal(err)
	}
	<-registered
	if _, err = io.CopyN(io.Discard, r, fill); err != nil {
		t.Fatal(err)
	}
	if err = <-polled; err != nil {
		t.Fatal(err)
	}
	select {
	case ready := <-q:
		next, ok := ready.TryResume()
		n, err, w := next.Next()
		if !ok || n != 2 || err != nil || !w.IsZero() || probes != 2 {
			t.Fatalf("resumed write (%d,%v), probes %d", n, err, probes)
		}
	default:
		t.Fatal("writable pipe produced no ready work")
	}
	var p [2]byte
	if _, err = io.ReadFull(r, p[:]); err != nil || string(p[:]) != "AB" {
		t.Fatal("pipe payload", p, err)
	}
}
