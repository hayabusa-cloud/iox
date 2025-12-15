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

func TestCopyNBuffer_Basic(t *testing.T) {
	src := bytes.NewBuffer(bytes.Repeat([]byte{'a'}, 1000))
	var dst bytes.Buffer
	buf := make([]byte, 128)
	n, err := iox.CopyNBuffer(&dst, src, 900, buf)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 900 || dst.Len() != 900 {
		t.Fatalf("n=%d len=%d", n, dst.Len())
	}
}

func TestCopyNBuffer_PanicOnEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic")
		}
	}()
	src := bytes.NewBufferString("abc")
	var dst bytes.Buffer
	_, _ = iox.CopyNBuffer(&dst, src, 2, make([]byte, 0))
}

// cover fast path of CopyNBuffer via ReaderFrom
func TestCopyNBuffer_ReaderFromFastPath(t *testing.T) {
	src := bytes.NewBufferString("ignored")
	n, err := iox.CopyNBuffer(rfN{n: 7}, src, 7, make([]byte, 64))
	if err != nil || n != 7 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestAdapters_WriterToAdapter_Read(t *testing.T) {
	r := iox.AsWriterTo(bytes.NewBufferString("ABCD"))
	buf := make([]byte, 3)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 3 || string(buf[:n]) != "ABC" {
		t.Fatalf("n=%d data=%q", n, string(buf[:n]))
	}
	n, err = r.Read(buf)
	// bytes.Buffer returns (1, nil) for the last byte; next Read returns (0, EOF)
	if n != 1 || string(buf[:1]) != "D" {
		t.Fatalf("n=%d err=%v data=%q", n, err, string(buf[:n]))
	}
	n, err = r.Read(buf)
	if !errors.Is(err, iox.EOF) || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

type captureWriter struct{ b bytes.Buffer }

func (w *captureWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

func TestAdapters_ReaderFromAdapter_ReadFrom(t *testing.T) {
	cw := &captureWriter{}
	w := iox.AsReaderFrom(cw)
	rf := w.(iox.ReaderFrom)
	n, err := rf.ReadFrom(bytes.NewBufferString("hello"))
	if err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if cw.b.String() != "hello" {
		t.Fatalf("got=%q", cw.b.String())
	}
}

func TestTeeReader_ZeroThenNil(t *testing.T) {
	zr := &zeroThenNilReader{}
	var side bytes.Buffer
	tr := iox.TeeReader(zr, &side)
	buf := make([]byte, 4)
	n, err := tr.Read(buf)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 0 || side.Len() != 0 {
		t.Fatalf("n=%d side=%d", n, side.Len())
	}
}

// teeWriter: tee short write
type shortTee struct{}

func (shortTee) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestTeeWriter_TeeShortWrite(t *testing.T) {
	var p bytes.Buffer
	tw := iox.TeeWriter(&p, shortTee{})
	n, err := tw.Write([]byte("abcd"))
	if !errors.Is(err, iox.ErrShortWrite) {
		t.Fatalf("want short write got %v", err)
	}
	// Count semantics: n reflects primary progress.
	if n != 4 {
		t.Fatalf("n=%d", n)
	}
	if p.String() != "abcd" {
		t.Fatalf("primary=%q", p.String())
	}
}

// additional helpers and tests
type rfN struct{ n int64 }

func (rfN) Write(p []byte) (int, error)            { return len(p), nil }
func (w rfN) ReadFrom(r iox.Reader) (int64, error) { return w.n, nil }

type shortSideWriter struct{}

func (shortSideWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestTeeReader_ShortWriteToSide(t *testing.T) {
	tr := iox.TeeReader(bytes.NewBufferString("abc"), shortSideWriter{})
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if !errors.Is(err, iox.ErrShortWrite) {
		t.Fatalf("want short write got %v", err)
	}
	// Count semantics: n reflects bytes consumed from the source.
	if n != 3 {
		t.Fatalf("n=%d", n)
	}
}

func TestTeeWriter_PrimaryWriteError(t *testing.T) {
	primaryErr := errors.New("primaryErr-primary")
	tw := iox.TeeWriter(errZeroWriter{err: primaryErr}, &bytes.Buffer{})
	n, err := tw.Write([]byte("abc"))
	if !errors.Is(err, primaryErr) {
		t.Fatalf("want primaryErr got %v", err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}

func TestTeeReader_DataThenMore(t *testing.T) {
	r := &dataThenErrReader{data: []byte("zz"), err: iox.ErrMore}
	var side bytes.Buffer
	tr := iox.TeeReader(r, &side)
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("want ErrMore got %v", err)
	}
	if n != 2 || string(buf[:2]) != "zz" {
		t.Fatalf("n=%d data=%q", n, string(buf[:n]))
	}
	if side.String() != "zz" {
		t.Fatalf("side=%q", side.String())
	}
}

func TestCopyNBuffer_SpecialErrors(t *testing.T) {
	s := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{
		{b: []byte("abcd")}, {b: nil, err: iox.ErrWouldBlock},
	}}
	var dst bytes.Buffer
	buf := make([]byte, 2)
	n, err := iox.CopyNBuffer(&dst, s, 8, buf)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("want ErrWouldBlock got %v", err)
	}
	if n != 4 {
		t.Fatalf("n=%d", n)
	}

	s2 := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{
		{b: []byte("xy")}, {b: nil, err: iox.ErrMore},
	}}
	n, err = iox.CopyNBuffer(&dst, s2, 5, buf)
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("want ErrMore got %v", err)
	}
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
}
