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

// Helpers
type errReader struct{ err error }

func (e errReader) Read(p []byte) (int, error) { return 0, e.err }

type zeroThenNilReader struct{ called bool }

func (r *zeroThenNilReader) Read(p []byte) (int, error) {
	if r.called {
		return 0, iox.EOF
	}
	r.called = true
	return 0, nil
}

type scriptedReader struct {
	steps []struct {
		b   []byte
		err error
	}
	i int
}

func (s *scriptedReader) Read(p []byte) (int, error) {
	if s.i >= len(s.steps) {
		return 0, iox.EOF
	}
	st := s.steps[s.i]
	s.i++
	if len(st.b) > 0 {
		n := copy(p, st.b)
		return n, nil
	}
	return 0, st.err
}

type shortWriter struct{ limit int }

func (w shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n := w.limit
	if n > len(p) {
		n = len(p)
	}
	if n < 0 {
		n = 0
	}
	return n, nil
}

type errWriter struct {
	n   int
	err error
}

func (w errWriter) Write(p []byte) (int, error) {
	n := w.n
	if n > len(p) {
		n = len(p)
	}
	if n < 0 {
		n = 0
	}
	return n, w.err
}

// simple writer without ReaderFrom
type sliceWriter struct{ data []byte }

func (w *sliceWriter) Write(p []byte) (int, error) { w.data = append(w.data, p...); return len(p), nil }

// writer returning 0, nil to trigger short write branch with nw==0
type shortZeroWriter struct{}

func (shortZeroWriter) Write(p []byte) (int, error) { return 0, nil }

type wtReader struct {
	n   int64
	err error
}

func (r wtReader) Read(p []byte) (int, error) { return 0, errors.New("unexpected Read call") }

func (r wtReader) WriteTo(w iox.Writer) (int64, error) {
	if r.n > 0 {
		buf := bytes.Repeat([]byte{'x'}, int(r.n))
		nn, _ := w.Write(buf)
		return int64(nn), r.err
	}
	return 0, r.err
}

type rfWriter struct {
	n   int64
	err error
}

func (w rfWriter) Write(p []byte) (int, error) { return len(p), nil }

func (w rfWriter) ReadFrom(r iox.Reader) (int64, error) { return w.n, w.err }

type noWTReader struct{ r *bytes.Reader }

func (r noWTReader) Read(p []byte) (int, error) { return r.r.Read(p) }

// ReaderFrom variants used to drive CopyN post-conditions.
type rfShortNil struct{}

func (rfShortNil) Write(p []byte) (int, error)          { return len(p), nil }
func (rfShortNil) ReadFrom(r iox.Reader) (int64, error) { return 3, nil }

type rfShortEOF struct{}

func (rfShortEOF) Write(p []byte) (int, error)          { return len(p), nil }
func (rfShortEOF) ReadFrom(r iox.Reader) (int64, error) { return 4, iox.EOF }

type dataThenErrReader struct {
	data []byte
	err  error
	used bool
}

func (r *dataThenErrReader) Read(p []byte) (int, error) {
	if r.used {
		return 0, iox.EOF
	}
	r.used = true
	n := copy(p, r.data)
	return n, r.err
}

// A ReaderFrom that actually consumes from the supplied reader, to exercise limitedReader.
type rfConsume struct{}

func (rfConsume) Write(p []byte) (int, error) { return len(p), nil }

func (rfConsume) ReadFrom(r iox.Reader) (int64, error) {
	var buf [7]byte
	var total int64
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			total += int64(n)
		}
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, nil
		}
	}
}

// Tests
func TestCopy_StdEOF(t *testing.T) {
	src := bytes.NewBufferString("hello")
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, src)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 5 {
		t.Fatalf("n=%d", n)
	}
	if got := dst.String(); got != "hello" {
		t.Fatalf("dst=%q", got)
	}
}

func TestCopy_SlowPath_StdEOF(t *testing.T) {
	src := noWTReader{r: bytes.NewReader([]byte("hello"))}
	var dst sliceWriter
	n, err := iox.Copy(&dst, src)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 5 || string(dst.data) != "hello" {
		t.Fatalf("n=%d dst=%q", n, string(dst.data))
	}
}

func TestCopy_WouldBlock(t *testing.T) {
	src := errReader{err: iox.ErrWouldBlock}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, src)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("want ErrWouldBlock, got %v", err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_More(t *testing.T) {
	s := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{
		{b: []byte("hi"), err: nil},
		{b: nil, err: iox.ErrMore},
	}}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, s)
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("want ErrMore, got %v", err)
	}
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
	if dst.String() != "hi" {
		t.Fatalf("dst=%q", dst.String())
	}
}

func TestCopy_ShortWrite(t *testing.T) {
	s := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{{b: bytes.Repeat([]byte{'a'}, 10)}}}
	w := shortWriter{limit: 5}
	n, err := iox.Copy(w, s)
	if !errors.Is(err, iox.ErrShortWrite) {
		t.Fatalf("want ErrShortWrite, got %v", err)
	}
	if n != 5 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_ShortWriteZeroBytesNilError(t *testing.T) {
	s := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{{b: bytes.Repeat([]byte{'a'}, 10)}}}
	n, err := iox.Copy(shortZeroWriter{}, s)
	if !errors.Is(err, iox.ErrShortWrite) {
		t.Fatalf("want ErrShortWrite, got %v", err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_WriteError(t *testing.T) {
	s := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{{b: bytes.Repeat([]byte{'a'}, 7)}}}
	writeErr := errors.New("writeErr")
	n, err := iox.Copy(errWriter{n: 3, err: writeErr}, s)
	if !errors.Is(err, writeErr) {
		t.Fatalf("want writeErr, got %v", err)
	}
	if n != 3 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_WriterTo_EOFMapsToNil(t *testing.T) {
	src := wtReader{n: 4, err: iox.EOF}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, src)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 4 {
		t.Fatalf("n=%d", n)
	}
	if dst.Len() != 4 {
		t.Fatalf("len=%d", dst.Len())
	}
}

func TestCopy_WriterTo_PropagatesSpecial(t *testing.T) {
	for _, tc := range []struct{ err error }{{iox.ErrWouldBlock}, {iox.ErrMore}} {
		src := wtReader{n: 3, err: tc.err}
		var dst bytes.Buffer
		n, err := iox.Copy(&dst, src)
		if !errors.Is(err, tc.err) {
			t.Fatalf("want %v got %v", tc.err, err)
		}
		if n != 3 {
			t.Fatalf("n=%d", n)
		}
	}
}

func TestCopy_WriterTo_OKNoError(t *testing.T) {
	src := wtReader{n: 5, err: nil}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, src)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 5 || dst.Len() != 5 {
		t.Fatalf("n=%d len=%d", n, dst.Len())
	}
}

func TestCopy_WriterTo_ZeroEOF(t *testing.T) {
	src := wtReader{n: 0, err: iox.EOF}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, src)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 0 || dst.Len() != 0 {
		t.Fatalf("n=%d len=%d", n, dst.Len())
	}
}

func TestCopy_WriterTo_ZeroNil(t *testing.T) {
	src := wtReader{n: 0, err: nil}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, src)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 0 || dst.Len() != 0 {
		t.Fatalf("n=%d len=%d", n, dst.Len())
	}
}

func TestCopy_ReaderFrom_EOFMapsToNil(t *testing.T) {
	dst := rfWriter{n: 6, err: iox.EOF}
	src := noWTReader{r: bytes.NewReader([]byte("ignored"))}
	n, err := iox.Copy(dst, src)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 6 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_ReaderFrom_PropagatesSpecial(t *testing.T) {
	for _, tc := range []struct{ err error }{{iox.ErrWouldBlock}, {iox.ErrMore}} {
		dst := rfWriter{n: 2, err: tc.err}
		src := noWTReader{r: bytes.NewReader([]byte("ignored"))}
		n, err := iox.Copy(dst, src)
		if !errors.Is(err, tc.err) {
			t.Fatalf("want %v got %v", tc.err, err)
		}
		if n != 2 {
			t.Fatalf("n=%d", n)
		}
	}
}

func TestCopy_ReaderFrom_OKNoError(t *testing.T) {
	dst := rfWriter{n: 7, err: nil}
	src := noWTReader{r: bytes.NewReader([]byte("ignored"))}
	n, err := iox.Copy(dst, src)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 7 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_ReaderFrom_ZeroEOF(t *testing.T) {
	dst := rfWriter{n: 0, err: iox.EOF}
	src := noWTReader{r: bytes.NewReader(nil)}
	n, err := iox.Copy(dst, src)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_WriterTo_PropagatesOtherError(t *testing.T) {
	writerToErr := errors.New("writerToErr-wt")
	src := wtReader{n: 2, err: writerToErr}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, src)
	if !errors.Is(err, writerToErr) {
		t.Fatalf("want writerToErr got %v", err)
	}
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_ReaderFrom_PropagatesOtherError(t *testing.T) {
	readerFromErr := errors.New("readerFromErr-rf")
	dst := rfWriter{n: 9, err: readerFromErr}
	src := noWTReader{r: bytes.NewReader([]byte("ignored"))}
	n, err := iox.Copy(dst, src)
	if !errors.Is(err, readerFromErr) {
		t.Fatalf("want readerFromErr got %v", err)
	}
	if n != 9 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_DataThenEOF_SameCall(t *testing.T) {
	r := &dataThenErrReader{data: []byte("abc"), err: iox.EOF}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, r)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 3 || dst.String() != "abc" {
		t.Fatalf("n=%d dst=%q", n, dst.String())
	}
}

func TestCopy_DataThenEOF_SameCall_SlowPath(t *testing.T) {
	r := &dataThenErrReader{data: []byte("abc"), err: iox.EOF}
	var dst sliceWriter
	n, err := iox.Copy(&dst, r)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 3 || string(dst.data) != "abc" {
		t.Fatalf("n=%d dst=%q", n, string(dst.data))
	}
}

func TestCopy_SlowPath_ThenZeroNil(t *testing.T) {
	// First returns data with nil err, then returns (0, nil)
	r := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{
		{b: []byte("abc"), err: nil},
		{b: nil, err: nil},
	}}
	var dst sliceWriter
	n, err := iox.Copy(&dst, r)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 3 || string(dst.data) != "abc" {
		t.Fatalf("n=%d dst=%q", n, string(dst.data))
	}
}

func TestCopy_DataThenWouldBlock_SameCall(t *testing.T) {
	r := &dataThenErrReader{data: []byte("xy"), err: iox.ErrWouldBlock}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, r)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("err=%v", err)
	}
	if n != 2 || dst.String() != "xy" {
		t.Fatalf("n=%d dst=%q", n, dst.String())
	}
}

func TestCopy_DataThenWouldBlock_SameCall_SlowPath(t *testing.T) {
	r := &dataThenErrReader{data: []byte("xy"), err: iox.ErrWouldBlock}
	var dst sliceWriter
	n, err := iox.Copy(&dst, r)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("err=%v", err)
	}
	if n != 2 || string(dst.data) != "xy" {
		t.Fatalf("n=%d dst=%q", n, string(dst.data))
	}
}

func TestCopy_DataThenMore_SameCall(t *testing.T) {
	r := &dataThenErrReader{data: []byte("12"), err: iox.ErrMore}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, r)
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("err=%v", err)
	}
	if n != 2 || dst.String() != "12" {
		t.Fatalf("n=%d dst=%q", n, dst.String())
	}
}

func TestCopy_DataThenMore_SameCall_SlowPath(t *testing.T) {
	r := &dataThenErrReader{data: []byte("12"), err: iox.ErrMore}
	var dst sliceWriter
	n, err := iox.Copy(&dst, r)
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("err=%v", err)
	}
	if n != 2 || string(dst.data) != "12" {
		t.Fatalf("n=%d dst=%q", n, string(dst.data))
	}
}

func TestCopy_DataThenOtherErr_SameCall(t *testing.T) {
	otherErr := errors.New("otherErr")
	r := &dataThenErrReader{data: []byte("ab"), err: otherErr}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, r)
	if !errors.Is(err, otherErr) {
		t.Fatalf("want otherErr got %v", err)
	}
	if n != 2 || dst.String() != "ab" {
		t.Fatalf("n=%d dst=%q", n, dst.String())
	}
}

func TestCopy_OtherErrNoData(t *testing.T) {
	readErr := errors.New("readErr")
	src := errReader{err: readErr}
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, src)
	if !errors.Is(err, readErr) {
		t.Fatalf("want readErr got %v", err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}

type errZeroWriter struct{ err error }

func (w errZeroWriter) Write(p []byte) (int, error) { return 0, w.err }

func TestCopy_WriteErrorZeroBytes(t *testing.T) {
	s := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{{b: []byte("hello")}}}
	zeroWriteErr := errors.New("write-error-zero")
	n, err := iox.Copy(errZeroWriter{err: zeroWriteErr}, s)
	if !errors.Is(err, zeroWriteErr) {
		t.Fatalf("want zeroWriteErr got %v", err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopyN_ReaderFrom_LimitedReaderStopsAtN(t *testing.T) {
	src := bytes.NewBufferString("abcdefghij")
	// Wrap to avoid WriterTo fast path
	nw := noWTReader{r: bytes.NewReader(src.Bytes())}
	n, err := iox.CopyN(rfConsume{}, nw, 5)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 5 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopyN_ReaderFrom_ShortNoErr_UnexpectedEOF(t *testing.T) {
	src := noWTReader{r: bytes.NewReader([]byte("ignored"))}
	n, err := iox.CopyN(rfShortNil{}, src, 5)
	if !errors.Is(err, iox.ErrUnexpectedEOF) {
		t.Fatalf("want UnexpectedEOF got %v", err)
	}
	if n != 3 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopyN_ReaderFrom_ShortEOF_UnexpectedEOF(t *testing.T) {
	src := noWTReader{r: bytes.NewReader([]byte("ignored"))}
	n, err := iox.CopyN(rfShortEOF{}, src, 5)
	if !errors.Is(err, iox.ErrUnexpectedEOF) {
		t.Fatalf("want UnexpectedEOF got %v", err)
	}
	if n != 4 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_EmptySrcImmediateEOF(t *testing.T) {
	var dst bytes.Buffer
	src := bytes.NewBuffer(nil)
	n, err := iox.Copy(&dst, src)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopy_SlowPath_MultiChunks_WithBuf(t *testing.T) {
	srcData := bytes.Repeat([]byte{'z'}, 4096+37)
	src := noWTReader{r: bytes.NewReader(srcData)}
	var dst sliceWriter
	buf := make([]byte, 256)
	n, err := iox.CopyBuffer(&dst, src, buf)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if int(n) != len(srcData) || len(dst.data) != len(srcData) {
		t.Fatalf("n=%d len=%d", n, len(dst.data))
	}
}

func TestCopy_ErrMore_NoData(t *testing.T) {
	src := errReader{err: iox.ErrMore}
	var dst sliceWriter
	n, err := iox.Copy(&dst, src)
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("want ErrMore got %v", err)
	}
	if n != 0 || len(dst.data) != 0 {
		t.Fatalf("n=%d len=%d", n, len(dst.data))
	}
}

func TestCopyN_ReaderFrom_ExactNil(t *testing.T) {
	src := noWTReader{r: bytes.NewReader([]byte("ignored"))}
	n, err := iox.CopyN(rfWriter{n: 5, err: nil}, src, 5)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 5 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopyN_ReaderFrom_ExactEOF_Ignored(t *testing.T) {
	src := noWTReader{r: bytes.NewReader([]byte("ignored"))}
	n, err := iox.CopyN(rfWriter{n: 4, err: iox.EOF}, src, 4)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 4 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopyBuffer_PanicOnEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic")
		}
	}()
	var dst bytes.Buffer
	src := bytes.NewBufferString("x")
	_, _ = iox.CopyBuffer(&dst, src, make([]byte, 0))
}

func TestCopy_ZeroThenNil(t *testing.T) {
	var r zeroThenNilReader
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, &r)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopyN_Exact(t *testing.T) {
	src := bytes.NewBufferString("abcdef")
	var dst bytes.Buffer
	n, err := iox.CopyN(&dst, src, 6)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 6 || dst.String() != "abcdef" {
		t.Fatalf("n=%d dst=%q", n, dst.String())
	}
}

func TestCopyN_ShortEOF(t *testing.T) {
	src := bytes.NewBufferString("abc")
	var dst bytes.Buffer
	n, err := iox.CopyN(&dst, src, 5)
	if !errors.Is(err, iox.ErrUnexpectedEOF) {
		t.Fatalf("want UnexpectedEOF got %v", err)
	}
	if n != 3 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopyN_WouldBlockBeforeN(t *testing.T) {
	s := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{
		{b: []byte("abcd")}, {b: nil, err: iox.ErrWouldBlock},
	}}
	var dst bytes.Buffer
	n, err := iox.CopyN(&dst, s, 8)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("want ErrWouldBlock got %v", err)
	}
	if n != 4 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopyN_MoreBeforeN(t *testing.T) {
	s := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{
		{b: []byte("xx")}, {b: nil, err: iox.ErrMore},
	}}
	var dst bytes.Buffer
	n, err := iox.CopyN(&dst, s, 5)
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("want ErrMore got %v", err)
	}
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
}

func TestCopyBuffer_WithProvidedBuf(t *testing.T) {
	src := bytes.NewBuffer(bytes.Repeat([]byte{'z'}, 1024))
	var dst bytes.Buffer
	buf := make([]byte, 128)
	n, err := iox.CopyBuffer(&dst, src, buf)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 1024 || dst.Len() != 1024 {
		t.Fatalf("n=%d len=%d", n, dst.Len())
	}
}

func TestCopyN_ZeroOrNegN(t *testing.T) {
	src := bytes.NewBufferString("abc")
	var dst bytes.Buffer
	n, err := iox.CopyN(&dst, src, 0)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, err = iox.CopyN(&dst, src, -5)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
