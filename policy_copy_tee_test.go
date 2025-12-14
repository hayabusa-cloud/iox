// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"bytes"
	"errors"
	"testing"

	"code.hybscloud.com/iox"
)

// errReaderAlwaysWB returns ErrWouldBlock on every Read.
type errReaderAlwaysWB struct{}

func (errReaderAlwaysWB) Read(p []byte) (int, error) { return 0, iox.ErrWouldBlock }

// wbThenDataReader yields ErrWouldBlock once, then returns data and EOF.
type wbThenDataReader struct {
	state int
	data  []byte
}

func (r *wbThenDataReader) Read(p []byte) (int, error) {
	if r.state == 0 {
		r.state = 1
		return 0, iox.ErrWouldBlock
	}
	if len(r.data) == 0 {
		return 0, iox.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, iox.EOF
	}
	return n, nil
}

// wouldBlockOnceWriter returns ErrWouldBlock once, then writes all.
type wouldBlockOnceWriter struct {
	wb  bool
	buf bytes.Buffer
}

func (w *wouldBlockOnceWriter) Write(p []byte) (int, error) {
	if !w.wb {
		w.wb = true
		return 0, iox.ErrWouldBlock
	}
	n, _ := w.buf.Write(p)
	return n, nil
}

// partialThenWBWriter writes k bytes then ErrWouldBlock; next call writes rest.
type partialThenWBWriter struct {
	once bool
	k    int
	buf  bytes.Buffer
}

func (w *partialThenWBWriter) Write(p []byte) (int, error) {
	if !w.once {
		w.once = true
		if w.k > len(p) {
			w.k = len(p)
		}
		n, _ := w.buf.Write(p[:w.k])
		return n, iox.ErrWouldBlock
	}
	n, _ := w.buf.Write(p)
	return n, nil
}

func TestCopyPolicy_NilPreservesWouldBlock(t *testing.T) {
	var dst bytes.Buffer
	n, err := iox.CopyPolicy(&dst, errReaderAlwaysWB{}, nil)
	if !errors.Is(err, iox.ErrWouldBlock) || n != 0 {
		t.Fatalf("want (0, ErrWouldBlock) got (%d, %v)", n, err)
	}
}

func TestCopyPolicy_YieldRetryCompletes_ReadWouldBlockOnce(t *testing.T) {
	src := &wbThenDataReader{data: []byte("abc")}
	var dst bytes.Buffer
	// Retry on would-block
	pol := iox.YieldPolicy{}
	n, err := iox.CopyPolicy(&dst, src, pol)
	if err != nil || n != 3 || dst.String() != "abc" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}

func TestTeeReaderPolicy_NilPreservesWouldBlock(t *testing.T) {
	r := &wbThenDataReader{data: []byte("xy")}
	var side bytes.Buffer
	tr := iox.TeeReaderPolicy(r, &side, nil)
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if !errors.Is(err, iox.ErrWouldBlock) || n != 0 {
		t.Fatalf("want (0, ErrWouldBlock) got (%d, %v)", n, err)
	}
}

func TestTeeReaderPolicy_SideWriteWouldBlock_RetryCompletes(t *testing.T) {
	r := bytes.NewBufferString("hi")
	side := &partialThenWBWriter{k: 1}
	tr := iox.TeeReaderPolicy(r, side, iox.YieldPolicy{})
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if side.buf.String() != "hi" || string(buf[:n]) != "hi" {
		t.Fatalf("side=%q read=%q", side.buf.String(), string(buf[:n]))
	}
}

func TestTeeWriterPolicy_PrimaryWouldBlockOnceCompletes(t *testing.T) {
	p := &wouldBlockOnceWriter{}
	var tbuf bytes.Buffer
	w := iox.TeeWriterPolicy(p, &tbuf, iox.YieldPolicy{})
	n, err := w.Write([]byte("zz"))
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if p.buf.String() != "zz" || tbuf.String() != "zz" {
		t.Fatalf("primary=%q tee=%q", p.buf.String(), tbuf.String())
	}
}
