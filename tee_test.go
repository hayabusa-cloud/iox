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

// helper writer that fails after writing k bytes
type failAfterWriter struct {
	k   int
	err error
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.k <= 0 {
		return 0, w.err
	}
	n := w.k
	if n > len(p) {
		n = len(p)
	}
	w.k -= n
	return n, w.err
}

func TestTeeReader_Basic(t *testing.T) {
	r := bytes.NewBufferString("hello")
	var side bytes.Buffer
	tr := iox.TeeReader(r, &side)
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 5 {
		t.Fatalf("n=%d", n)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("buf=%q", string(buf[:n]))
	}
	if side.String() != "hello" {
		t.Fatalf("side=%q", side.String())
	}
}

func TestTeeReader_DataThenWouldBlock(t *testing.T) {
	r := &dataThenErrReader{data: []byte("ab"), err: iox.ErrWouldBlock}
	var side bytes.Buffer
	tr := iox.TeeReader(r, &side)
	b := make([]byte, 10)
	n, err := tr.Read(b)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("want ErrWouldBlock got %v", err)
	}
	if n != 2 || string(b[:n]) != "ab" {
		t.Fatalf("n=%d data=%q", n, string(b[:n]))
	}
	if side.String() != "ab" {
		t.Fatalf("side=%q", side.String())
	}
}

func TestTeeReader_WriteSideError(t *testing.T) {
	r := bytes.NewBufferString("xy")
	fw := &failAfterWriter{k: 1, err: errors.New("side-write-error")} // writes 1 then errors
	tr := iox.TeeReader(r, fw)
	buf := make([]byte, 4)
	n, err := tr.Read(buf)
	if !errors.Is(err, fw.err) {
		t.Fatalf("want side write error got %v", err)
	}
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
}

func TestTeeWriter_Basic(t *testing.T) {
	var p, q bytes.Buffer
	tw := iox.TeeWriter(&p, &q)
	n, err := tw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 5 {
		t.Fatalf("n=%d", n)
	}
	if p.String() != "hello" || q.String() != "hello" {
		t.Fatalf("p=%q q=%q", p.String(), q.String())
	}
}

func TestTeeWriter_PrimaryShortWrite(t *testing.T) {
	tw := iox.TeeWriter(shortWriter{limit: 2}, &bytes.Buffer{})
	n, err := tw.Write([]byte("abcd"))
	if !errors.Is(err, iox.ErrShortWrite) {
		t.Fatalf("want short write got %v", err)
	}
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
}

func TestTeeWriter_TeeError(t *testing.T) {
	var p bytes.Buffer
	teeErr := errors.New("teeErr")
	tw := iox.TeeWriter(&p, errZeroWriter{err: teeErr})
	n, err := tw.Write([]byte("hi"))
	if !errors.Is(err, teeErr) {
		t.Fatalf("want teeErr got %v", err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
	if p.String() != "hi" {
		t.Fatalf("primary wrote=%q", p.String())
	}
}

func TestAdapters_AsWriterTo_AsReaderFrom(t *testing.T) {
	// AsWriterTo wraps a reader and should still allow Copy to succeed
	src := iox.AsWriterTo(bytes.NewBufferString("data"))
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, src)
	if err != nil || n != 4 || dst.String() != "data" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}

	// AsReaderFrom wraps a writer and should allow Copy to succeed
	wrappedDst := iox.AsReaderFrom(&bytes.Buffer{})
	// Write through wrappedDst by copying from a reader
	n, err = iox.Copy(wrappedDst, bytes.NewBufferString("xyz"))
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
