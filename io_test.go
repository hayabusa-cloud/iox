// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"code.hybscloud.com/iox"
)

// -----------------------------------------------------------------------------
// Copy, CopyN, CopyBuffer, and basic io helper tests
// -----------------------------------------------------------------------------

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
// dataThenErrReader is provided in other test files in this package.
func TestAsWriterTo_WriteTo_PropagatesErrMore(t *testing.T) {
	src := &dataThenErrReader{data: []byte("ab"), err: iox.ErrMore}
	wrapped := iox.AsWriterTo(src)
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, wrapped)
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("want ErrMore got %v", err)
	}
	if n != 2 || dst.String() != "ab" {
		t.Fatalf("n=%d dst=%q", n, dst.String())
	}
}
func TestAsReaderFrom_ReadFrom_PropagatesErrWouldBlock(t *testing.T) {
	src := &dataThenErrReader{data: []byte("zz"), err: iox.ErrWouldBlock}
	var primary bytes.Buffer
	wrapped := iox.AsReaderFrom(&primary)
	n, err := iox.Copy(wrapped, src)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("want ErrWouldBlock got %v", err)
	}
	if n != 2 || primary.String() != "zz" {
		t.Fatalf("n=%d primary=%q", n, primary.String())
	}
}
// teeWriter: special errors (ErrMore) are propagated unchanged.
func TestTeeWriter_TeeErrMore_Propagated(t *testing.T) {
	var primary bytes.Buffer
	tee := errThenCountWriter{err: iox.ErrMore}
	tw := iox.TeeWriter(&primary, tee)
	n, err := tw.Write([]byte("hello"))
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("want ErrMore got %v", err)
	}
	if n != len("hello") {
		t.Fatalf("n=%d", n)
	}
	if primary.String() != "hello" {
		t.Fatalf("primary=%q", primary.String())
	}
}
func TestTeeWriter_TeeWouldBlock_Propagated(t *testing.T) {
	var primary bytes.Buffer
	tee := errThenCountWriter{err: iox.ErrWouldBlock}
	tw := iox.TeeWriter(&primary, tee)
	n, err := tw.Write([]byte("zz"))
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("want ErrWouldBlock got %v", err)
	}
	if n != len("zz") || primary.String() != "zz" {
		t.Fatalf("n=%d primary=%q", n, primary.String())
	}
}
// Helper writer that accepts all bytes and returns the provided error.
type errThenCountWriter struct{ err error }
func (w errThenCountWriter) Write(p []byte) (int, error) {
	return len(p), w.err
}
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
// =============================================================================
// Mock types for coverage expansion
// =============================================================================
// failingSeeker is a ReadSeeker that returns data on Read but fails on Seek.
type failingSeeker struct {
	data    []byte
	pos     int
	seekErr error
}
func (f *failingSeeker) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}
func (f *failingSeeker) Seek(offset int64, whence int) (int64, error) {
	return 0, f.seekErr
}
// partialWBWriter writes partial data and returns ErrWouldBlock.
// It writes exactly `partial` bytes, then returns ErrWouldBlock.
type partialWBWriter struct {
	partial int
	buf     []byte
}
func (w *partialWBWriter) Write(p []byte) (int, error) {
	if w.partial <= 0 {
		return 0, iox.ErrWouldBlock
	}
	n := w.partial
	if n > len(p) {
		n = len(p)
	}
	w.buf = append(w.buf, p[:n]...)
	w.partial = 0
	return n, iox.ErrWouldBlock
}
// partialMoreWriter writes partial data and returns ErrMore.
type partialMoreWriter struct {
	partial int
	buf     []byte
}
func (w *partialMoreWriter) Write(p []byte) (int, error) {
	if w.partial <= 0 {
		return 0, iox.ErrMore
	}
	n := w.partial
	if n > len(p) {
		n = len(p)
	}
	w.buf = append(w.buf, p[:n]...)
	w.partial = 0
	return n, iox.ErrMore
}
// plainReader is a simple Reader that does NOT implement Seeker.
type plainReader struct {
	data []byte
	pos  int
}
func (r *plainReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
// =============================================================================
// Test: Seeker Rollback Failures in copyBuffer
// =============================================================================
func TestCopy_SeekerRollbackFailure_ErrWouldBlock(t *testing.T) {
	t.Run("Seek fails on partial write with ErrWouldBlock", func(t *testing.T) {
		seekErr := errors.New("seek failed")
		src := &failingSeeker{data: []byte("hello"), seekErr: seekErr}
		dst := &partialWBWriter{partial: 2} // writes 2 bytes, then ErrWouldBlock
		n, err := iox.Copy(dst, src)
		// Should return the seek error, not ErrWouldBlock
		if !errors.Is(err, seekErr) {
			t.Fatalf("expected seek error, got: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected n=2, got n=%d", n)
		}
	})
}
func TestCopy_SeekerRollbackFailure_ErrMore(t *testing.T) {
	t.Run("Seek fails on partial write with ErrMore", func(t *testing.T) {
		seekErr := errors.New("seek failed")
		src := &failingSeeker{data: []byte("world"), seekErr: seekErr}
		dst := &partialMoreWriter{partial: 3} // writes 3 bytes, then ErrMore
		n, err := iox.Copy(dst, src)
		// Should return the seek error, not ErrMore
		if !errors.Is(err, seekErr) {
			t.Fatalf("expected seek error, got: %v", err)
		}
		if n != 3 {
			t.Fatalf("expected n=3, got n=%d", n)
		}
	})
}
// =============================================================================
// Test: ErrNoSeeker Propagation in copyBuffer
// =============================================================================
func TestCopy_ErrNoSeeker_ErrWouldBlock(t *testing.T) {
	t.Run("Non-seekable source with partial write and ErrWouldBlock", func(t *testing.T) {
		src := &plainReader{data: []byte("test")}
		dst := &partialWBWriter{partial: 2} // writes 2 bytes, then ErrWouldBlock
		n, err := iox.Copy(dst, src)
		if !errors.Is(err, iox.ErrNoSeeker) {
			t.Fatalf("expected ErrNoSeeker, got: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected n=2, got n=%d", n)
		}
	})
}
func TestCopy_ErrNoSeeker_ErrMore(t *testing.T) {
	t.Run("Non-seekable source with partial write and ErrMore", func(t *testing.T) {
		src := &plainReader{data: []byte("data")}
		dst := &partialMoreWriter{partial: 1} // writes 1 byte, then ErrMore
		n, err := iox.Copy(dst, src)
		if !errors.Is(err, iox.ErrNoSeeker) {
			t.Fatalf("expected ErrNoSeeker, got: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected n=1, got n=%d", n)
		}
	})
}
// =============================================================================
// Test: copyBufferPolicy Seeker Rollback with PolicyReturn
// =============================================================================
func TestCopyPolicy_SeekerRollbackFailure_ErrWouldBlock(t *testing.T) {
	t.Run("Policy returns, Seek fails on partial write with ErrWouldBlock", func(t *testing.T) {
		seekErr := errors.New("seek failed in policy path")
		src := &failingSeeker{data: []byte("abcdef"), seekErr: seekErr}
		dst := &partialWBWriter{partial: 3}
		// Policy that returns (not retries) on ErrWouldBlock
		pol := iox.ReturnPolicy{}
		n, err := iox.CopyPolicy(dst, src, pol)
		if !errors.Is(err, seekErr) {
			t.Fatalf("expected seek error, got: %v", err)
		}
		if n != 3 {
			t.Fatalf("expected n=3, got n=%d", n)
		}
	})
}
func TestCopyPolicy_SeekerRollbackFailure_ErrMore(t *testing.T) {
	t.Run("Policy returns, Seek fails on partial write with ErrMore", func(t *testing.T) {
		seekErr := errors.New("seek failed in policy path")
		src := &failingSeeker{data: []byte("ghijkl"), seekErr: seekErr}
		dst := &partialMoreWriter{partial: 4}
		// Policy that returns (not retries) on ErrMore
		pol := iox.ReturnPolicy{}
		n, err := iox.CopyPolicy(dst, src, pol)
		if !errors.Is(err, seekErr) {
			t.Fatalf("expected seek error, got: %v", err)
		}
		if n != 4 {
			t.Fatalf("expected n=4, got n=%d", n)
		}
	})
}
// =============================================================================
// Test: copyBufferPolicy ErrNoSeeker with PolicyReturn
// =============================================================================
func TestCopyPolicy_ErrNoSeeker_ErrWouldBlock(t *testing.T) {
	t.Run("Policy returns, non-seekable source with partial write and ErrWouldBlock", func(t *testing.T) {
		src := &plainReader{data: []byte("mnop")}
		dst := &partialWBWriter{partial: 2}
		pol := iox.ReturnPolicy{}
		n, err := iox.CopyPolicy(dst, src, pol)
		if !errors.Is(err, iox.ErrNoSeeker) {
			t.Fatalf("expected ErrNoSeeker, got: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected n=2, got n=%d", n)
		}
	})
}
func TestCopyPolicy_ErrNoSeeker_ErrMore(t *testing.T) {
	t.Run("Policy returns, non-seekable source with partial write and ErrMore", func(t *testing.T) {
		src := &plainReader{data: []byte("qrst")}
		dst := &partialMoreWriter{partial: 1}
		pol := iox.ReturnPolicy{}
		n, err := iox.CopyPolicy(dst, src, pol)
		if !errors.Is(err, iox.ErrNoSeeker) {
			t.Fatalf("expected ErrNoSeeker, got: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected n=1, got n=%d", n)
		}
	})
}
// =============================================================================
// Test: copyBufferPolicy Seeker Rollback Success (returns semantic error)
// =============================================================================
// workingSeeker is a ReadSeeker that successfully seeks.
type workingSeeker struct {
	data []byte
	pos  int
}
func (s *workingSeeker) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.pos:])
	s.pos += n
	return n, nil
}
func (s *workingSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		s.pos = int(offset)
	case io.SeekCurrent:
		s.pos += int(offset)
	case io.SeekEnd:
		s.pos = len(s.data) + int(offset)
	}
	if s.pos < 0 {
		s.pos = 0
	}
	if s.pos > len(s.data) {
		s.pos = len(s.data)
	}
	return int64(s.pos), nil
}
func TestCopyPolicy_SeekerRollbackSuccess_ErrWouldBlock(t *testing.T) {
	t.Run("Policy returns, Seek succeeds, returns ErrWouldBlock", func(t *testing.T) {
		src := &workingSeeker{data: []byte("uvwxyz")}
		dst := &partialWBWriter{partial: 2}
		pol := iox.ReturnPolicy{}
		n, err := iox.CopyPolicy(dst, src, pol)
		// Should return ErrWouldBlock after successful rollback
		if !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("expected ErrWouldBlock, got: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected n=2, got n=%d", n)
		}
		// Verify source was rolled back (pos should be at 2, not 6)
		if src.pos != 2 {
			t.Fatalf("expected src.pos=2 after rollback, got %d", src.pos)
		}
	})
}
func TestCopyPolicy_SeekerRollbackSuccess_ErrMore(t *testing.T) {
	t.Run("Policy returns, Seek succeeds, returns ErrMore", func(t *testing.T) {
		src := &workingSeeker{data: []byte("123456")}
		dst := &partialMoreWriter{partial: 3}
		pol := iox.ReturnPolicy{}
		n, err := iox.CopyPolicy(dst, src, pol)
		// Should return ErrMore after successful rollback
		if !errors.Is(err, iox.ErrMore) {
			t.Fatalf("expected ErrMore, got: %v", err)
		}
		if n != 3 {
			t.Fatalf("expected n=3, got n=%d", n)
		}
		// Verify source was rolled back (pos should be at 3, not 6)
		if src.pos != 3 {
			t.Fatalf("expected src.pos=3 after rollback, got %d", src.pos)
		}
	})
}
// dataThenErrReader returns all data in the first call and then returns err
// with the same call (n>0, err!=nil). Subsequent calls return EOF.
type dataThenErrReader2 struct {
	data []byte
	err  error
	done bool
}
func (r *dataThenErrReader2) Read(p []byte) (int, error) {
	if r.done {
		return 0, iox.EOF
	}
	r.done = true
	n := copy(p, r.data)
	return n, r.err
}
// zeroThenEOFReader returns (0, ErrWouldBlock|ErrMore) first, then EOF.
type zeroThenEOFReader struct {
	err error
	hit bool
}
func (r *zeroThenEOFReader) Read([]byte) (int, error) {
	if !r.hit {
		r.hit = true
		return 0, r.err
	}
	return 0, iox.EOF
}
// errReader returns a configured error without producing data.
type errOnlyReader2 struct{ err error }
func (r errOnlyReader2) Read([]byte) (int, error) { return 0, r.err }
// ----- Small, targeted coverage tests -----
func TestCopyBufferPolicy_NilPolicy_Delegates(t *testing.T) {
	var dst bytes.Buffer
	src := bytes.NewBufferString("abc")
	n, err := iox.CopyBufferPolicy(&dst, src, nil, nil)
	if err != nil || n != 3 || dst.String() != "abc" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
func TestCopyNPolicy_NonPositiveN_ReturnsZeroNil(t *testing.T) {
	var dst bytes.Buffer
	src := bytes.NewBufferString("ignored")
	if n, err := iox.CopyNPolicy(&dst, src, 0, iox.YieldPolicy{}); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, err := iox.CopyNPolicy(&dst, src, -5, iox.ReturnPolicy{}); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
func TestCopyNBufferPolicy_NonPositiveN_AndNilPolicy(t *testing.T) {
	var dst bytes.Buffer
	src := bytes.NewBufferString("abcd")
	// n<=0 early return
	if n, err := iox.CopyNBufferPolicy(&dst, src, 0, nil, iox.YieldPolicy{}); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	// nil policy branch -> delegate to CopyNBuffer
	n, err := iox.CopyNBufferPolicy(&dst, bytes.NewBufferString("xy"), 2, nil, nil)
	if err != nil || n != 2 || dst.String()[len(dst.String())-2:] != "xy" {
		t.Fatalf("n=%d err=%v tail=%q", n, err, dst.String())
	}
}
func TestCopyPolicy_ReadSide_WouldBlock_Returns(t *testing.T) {
	var dst bytes.Buffer
	r := &dataThenErrReader2{data: []byte("ok"), err: iox.ErrWouldBlock}
	n, err := iox.CopyPolicy(&dst, r, iox.ReturnPolicy{})
	if !errors.Is(err, iox.ErrWouldBlock) || n != 2 || dst.String() != "ok" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
func TestCopyPolicy_ReadSide_More_Returns(t *testing.T) {
	var dst bytes.Buffer
	r := &dataThenErrReader2{data: []byte("mm"), err: iox.ErrMore}
	n, err := iox.CopyPolicy(&dst, r, iox.ReturnPolicy{})
	if !errors.Is(err, iox.ErrMore) || n != 2 || dst.String() != "mm" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
func TestCopyPolicy_ReadSide_OtherError_Returns(t *testing.T) {
	var dst bytes.Buffer
	other := errors.New("boom")
	r := &dataThenErrReader2{data: []byte("zz"), err: other}
	n, err := iox.CopyPolicy(&dst, r, iox.YieldPolicy{})
	if !errors.Is(err, other) || n != 2 || dst.String() != "zz" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
func TestCopyPolicy_ReadZero_WouldBlock_RetryThenEOF(t *testing.T) {
	var dst bytes.Buffer
	r := &zeroThenEOFReader{err: iox.ErrWouldBlock}
	pol := iox.PolicyFunc{WouldBlockFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	n, err := iox.CopyPolicy(&dst, r, pol)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
// Slow-path zero-read semantic branches (avoid fast-path by using sliceWriter)
func TestCopyPolicy_SlowPath_ReadZeroWouldBlock_RetryThenEOF(t *testing.T) {
	var dst sliceWriter
	r := &zeroThenEOFReader{err: iox.ErrWouldBlock}
	pol := iox.PolicyFunc{WouldBlockFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	n, err := iox.CopyPolicy(&dst, r, pol)
	if err != nil || n != 0 || len(dst.data) != 0 {
		t.Fatalf("n=%d err=%v dstlen=%d", n, err, len(dst.data))
	}
}
func TestCopyPolicy_SlowPath_ReadZeroWouldBlock_Returns(t *testing.T) {
	var dst sliceWriter
	r := &zeroThenEOFReader{err: iox.ErrWouldBlock}
	n, err := iox.CopyPolicy(&dst, r, iox.ReturnPolicy{})
	if !iox.IsWouldBlock(err) || n != 0 || len(dst.data) != 0 {
		t.Fatalf("n=%d err=%v dstlen=%d", n, err, len(dst.data))
	}
}
func TestCopyPolicy_SlowPath_ReadZeroMore_RetryThenEOF(t *testing.T) {
	var dst sliceWriter
	r := &zeroThenEOFReader{err: iox.ErrMore}
	pol := iox.PolicyFunc{MoreFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	n, err := iox.CopyPolicy(&dst, r, pol)
	if err != nil || n != 0 || len(dst.data) != 0 {
		t.Fatalf("n=%d err=%v dstlen=%d", n, err, len(dst.data))
	}
}
func TestCopyPolicy_SlowPath_ReadZeroMore_Returns(t *testing.T) {
	var dst sliceWriter
	r := &zeroThenEOFReader{err: iox.ErrMore}
	n, err := iox.CopyPolicy(&dst, r, iox.ReturnPolicy{})
	if !iox.IsMore(err) || n != 0 || len(dst.data) != 0 {
		t.Fatalf("n=%d err=%v dstlen=%d", n, err, len(dst.data))
	}
}
// Slow-path variants to ensure copyBufferPolicy branches are hit (avoid ReaderFrom fast-path)
func TestCopyPolicy_SlowPath_ReadSide_More_Returns(t *testing.T) {
	var dst sliceWriter
	r := &dataThenErrReader2{data: []byte("MM"), err: iox.ErrMore}
	n, err := iox.CopyPolicy(&dst, r, iox.ReturnPolicy{})
	if !errors.Is(err, iox.ErrMore) || n != 2 || string(dst.data) != "MM" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, string(dst.data))
	}
}
func TestCopyPolicy_SlowPath_ReadSide_WouldBlock_Returns(t *testing.T) {
	var dst sliceWriter
	r := &dataThenErrReader2{data: []byte("WB"), err: iox.ErrWouldBlock}
	n, err := iox.CopyPolicy(&dst, r, iox.ReturnPolicy{})
	if !errors.Is(err, iox.ErrWouldBlock) || n != 2 || string(dst.data) != "WB" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, string(dst.data))
	}
}
// --- copyBufferPolicy: n==0 semantic retry followed by progress ---
// seqReader yields (0, err1) then (n>0, nil) once.
type seqReader struct {
	step int
	err  error
	data []byte
}
func (r *seqReader) Read(p []byte) (int, error) {
	if r.step == 0 {
		r.step = 1
		return 0, r.err
	}
	if r.step == 1 {
		r.step = 2
		n := copy(p, r.data)
		return n, nil
	}
	return 0, iox.EOF
}
func TestCopyPolicy_SlowPath_ReadZeroWouldBlock_RetryThenProgress(t *testing.T) {
	var dst sliceWriter
	r := &seqReader{err: iox.ErrWouldBlock, data: []byte("k1")}
	pol := iox.PolicyFunc{WouldBlockFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	n, err := iox.CopyPolicy(&dst, r, pol)
	if err != nil || n != 2 || string(dst.data) != "k1" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, string(dst.data))
	}
}
func TestCopyPolicy_SlowPath_ReadZeroMore_RetryThenProgress(t *testing.T) {
	var dst sliceWriter
	r := &seqReader{err: iox.ErrMore, data: []byte("k2")}
	pol := iox.PolicyFunc{MoreFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	n, err := iox.CopyPolicy(&dst, r, pol)
	if err != nil || n != 2 || string(dst.data) != "k2" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, string(dst.data))
	}
}
// (complex multi-iteration inner write-loop scenario intentionally omitted to
// avoid tight coupling with internal control flow semantics.)
// --- copyBufferPolicy: read-side progress then EOF return ---
type readerDataThenEOF struct {
	data []byte
	done bool
}
func (r *readerDataThenEOF) Read(p []byte) (int, error) {
	if r.done {
		return 0, iox.EOF
	}
	r.done = true
	n := copy(p, r.data)
	return n, nil
}
func TestCopyPolicy_SlowPath_ReadDataThenEOF_ReturnsNil(t *testing.T) {
	var dst sliceWriter
	r := &readerDataThenEOF{data: []byte("xy")}
	n, err := iox.CopyPolicy(&dst, r, iox.YieldPolicy{})
	if err != nil || n != 2 || string(dst.data) != "xy" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, string(dst.data))
	}
}
// --- copyBufferPolicy: progress then generic error after multiple write iterations ---
// readerDataThenErr returns data on first read, then generic error.
type readerDataThenErr struct {
	data []byte
	err  error
	done bool
}
func (r *readerDataThenErr) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	n := copy(p, r.data)
	return n, nil
}
// chunkWriter forces short writes (n < len(p)) with nil error, which makes the
// inner write-loop iterate multiple times before finishing a chunk.
type chunkWriter struct {
	limit int
	buf   bytes.Buffer
}
func (w *chunkWriter) Write(p []byte) (int, error) {
	if w.limit <= 0 || w.limit >= len(p) {
		n, _ := w.buf.Write(p)
		return n, nil
	}
	n, _ := w.buf.Write(p[:w.limit])
	return n, nil
}
func TestCopyPolicy_SlowPath_WriteLoop_ThenGenericReadError(t *testing.T) {
	// Writer forces inner loop to iterate multiple times before finishing the chunk.
	w := &chunkWriter{limit: 1}
	boom := errors.New("read-err")
	r := &readerDataThenErr{data: []byte("abc"), err: boom}
	n, err := iox.CopyPolicy(w, r, iox.YieldPolicy{})
	if !errors.Is(err, boom) || n != 3 || w.buf.String() != "abc" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, w.buf.String())
	}
}
// --- copyBufferPolicy: zero WouldBlock retry then data+More with Return ---
type seqWBThenDataMore struct{ step int }
func (s *seqWBThenDataMore) Read(p []byte) (int, error) {
	if s.step == 0 {
		s.step = 1
		return 0, iox.ErrWouldBlock
	}
	copy(p, []byte("hi"))
	return 2, iox.ErrMore
}
func TestCopyPolicy_SlowPath_ZeroWBRetry_ThenDataMore_ReturnsMore(t *testing.T) {
	var dst sliceWriter
	r := &seqWBThenDataMore{}
	pol := iox.PolicyFunc{WouldBlockFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	n, err := iox.CopyPolicy(&dst, r, pol)
	if !iox.IsMore(err) || n != 2 || string(dst.data) != "hi" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, string(dst.data))
	}
}
func TestCopyPolicy_ReadDataThenMore_RetryThenEOF(t *testing.T) {
	// First returns data+More, policy says retry; next returns EOF.
	var dst bytes.Buffer
	step := 0
	src := &funcReader{read: func(p []byte) (int, error) {
		if step == 0 {
			step = 1
			copy(p, []byte("hi"))
			return 2, iox.ErrMore
		}
		return 0, iox.EOF
	}}
	pol := iox.PolicyFunc{MoreFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	n, err := iox.CopyPolicy(&dst, src, pol)
	if err != nil || n != 2 || dst.String() != "hi" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
func TestPolicy_ReturnPolicy_Yield_NoOp(t *testing.T) {
	// Exercise the no-op Yield method for coverage.
	iox.ReturnPolicy{}.Yield(iox.OpCopyRead)
}
func TestPolicy_DefaultYields_Callable(t *testing.T) {
	// Exercise default runtime.Gosched() branches.
	iox.YieldPolicy{}.Yield(iox.OpCopyWrite)
	iox.YieldOnWriteWouldBlockPolicy{}.Yield(iox.OpCopyWrite)
}
// funcReader adapts a function to an iox.Reader.
type funcReader struct{ read func(p []byte) (int, error) }
func (r *funcReader) Read(p []byte) (int, error) { return r.read(p) }
func TestCopyNPolicy_NilPolicy_Delegates(t *testing.T) {
	var dst bytes.Buffer
	src := bytes.NewBufferString("12345")
	n, err := iox.CopyNPolicy(&dst, src, 5, nil)
	if err != nil || n != 5 || dst.String() != "12345" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
// (WriterTo fast-path WB cases are already covered in copy_policy_test.go)
func TestTeeWriterPolicy_NilPolicy_Delegates(t *testing.T) {
	var primary, tee bytes.Buffer
	w := iox.TeeWriterPolicy(&primary, &tee, nil)
	n, err := w.Write([]byte("abc"))
	if err != nil || n != 3 || primary.String() != "abc" || tee.String() != "abc" {
		t.Fatalf("n=%d err=%v p=%q t=%q", n, err, primary.String(), tee.String())
	}
}
func TestPolicy_YieldFuncs_Branches(t *testing.T) {
	var c1, c2 int
	iox.YieldPolicy{YieldFunc: func(iox.Op) { c1++ }}.Yield(iox.OpCopyRead)
	iox.YieldOnWriteWouldBlockPolicy{YieldFunc: func(iox.Op) { c2++ }}.Yield(iox.OpCopyWrite)
	if c1 != 1 || c2 != 1 {
		t.Fatalf("counters: %d %d", c1, c2)
	}
}
// --- Additional tee policy return-path coverage ---
type wbOnlyWriter struct{}
func (wbOnlyWriter) Write(p []byte) (int, error) { return 0, iox.ErrWouldBlock }
func TestTeeReaderPolicy_SideWouldBlock_ReturnsWouldBlock(t *testing.T) {
	r := bytes.NewBufferString("ab")
	tr := iox.TeeReaderPolicy(r, wbOnlyWriter{}, iox.ReturnPolicy{})
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	// Count semantics: n reflects bytes consumed from the source.
	if !iox.IsWouldBlock(err) || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
func TestTeeWriterPolicy_TeeWouldBlock_ReturnsWouldBlock(t *testing.T) {
	var primary bytes.Buffer
	w := iox.TeeWriterPolicy(&primary, wbOnlyWriter{}, iox.ReturnPolicy{})
	n, err := w.Write([]byte("ab"))
	// Count semantics: n reflects primary progress.
	if !iox.IsWouldBlock(err) || n != 2 || primary.String() != "ab" {
		t.Fatalf("n=%d err=%v primary=%q", n, err, primary.String())
	}
}
// --- copyBufferPolicy slow-path write ErrMore branches ---
type partialThenMoreWriter struct {
	once bool
	k    int
	buf  bytes.Buffer
}
func (w *partialThenMoreWriter) Write(p []byte) (int, error) {
	if !w.once {
		w.once = true
		if w.k > len(p) {
			w.k = len(p)
		}
		n, _ := w.buf.Write(p[:w.k])
		return n, iox.ErrMore
	}
	n, _ := w.buf.Write(p)
	return n, nil
}
func TestCopyPolicy_SlowPath_WriteMore_RetryThenOK(t *testing.T) {
	src := bytes.NewBufferString("go")
	dst := &partialThenMoreWriter{k: 1}
	pol := iox.PolicyFunc{MoreFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	n, err := iox.CopyPolicy(dst, src, pol)
	if err != nil || n != 2 || dst.buf.String() != "go" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.buf.String())
	}
}
// --- Remaining coverage for teeReaderWithPolicy and teeWriterWithPolicy ---
// dataThenWB2 returns data and ErrWouldBlock in the same call.
type dataThenWB2 struct {
	data []byte
	done bool
}
func (r *dataThenWB2) Read(p []byte) (int, error) {
	if r.done {
		return 0, iox.EOF
	}
	r.done = true
	n := copy(p, r.data)
	return n, iox.ErrWouldBlock
}
func TestTeeReaderPolicy_DataThenWouldBlock_RetryReturnsNil(t *testing.T) {
	r := &dataThenWB2{data: []byte("xy")}
	var side bytes.Buffer
	pol := iox.PolicyFunc{WouldBlockFunc: func(op iox.Op) iox.PolicyAction {
		if op == iox.OpTeeReaderRead {
			return iox.PolicyRetry
		}
		return iox.PolicyReturn
	}}
	tr := iox.TeeReaderPolicy(r, &side, pol)
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if err != nil || n != 2 || string(buf[:n]) != "xy" || side.String() != "xy" {
		t.Fatalf("n=%d err=%v read=%q side=%q", n, err, string(buf[:n]), side.String())
	}
}
// teeMoreOnceThenOK returns ErrMore once, then writes all on retry.
type teeMoreOnceThenOK struct {
	more bool
	buf  bytes.Buffer
}
func (w *teeMoreOnceThenOK) Write(p []byte) (int, error) {
	if !w.more {
		w.more = true
		return 0, iox.ErrMore
	}
	return w.buf.Write(p)
}
func TestTeeWriterPolicy_TeeMore_RetryCompletes(t *testing.T) {
	var primary bytes.Buffer
	tee := &teeMoreOnceThenOK{}
	pol := iox.PolicyFunc{MoreFunc: func(op iox.Op) iox.PolicyAction {
		if op == iox.OpTeeWriterTeeWrite {
			return iox.PolicyRetry
		}
		return iox.PolicyReturn
	}}
	w := iox.TeeWriterPolicy(&primary, tee, pol)
	n, err := w.Write([]byte("ok"))
	if err != nil || n != 2 || primary.String() != "ok" || tee.buf.String() != "ok" {
		t.Fatalf("n=%d err=%v primary=%q tee=%q", n, err, primary.String(), tee.buf.String())
	}
}
// writerErrAlways returns a generic error.
type writerErrAlways struct{ err error }
func (w writerErrAlways) Write([]byte) (int, error) { return 0, w.err }
func TestCopyPolicy_SlowPath_WriteGenericError_Returns(t *testing.T) {
	src := bytes.NewBufferString("x")
	boom := errors.New("boom")
	n, err := iox.CopyPolicy(writerErrAlways{err: boom}, src, iox.YieldPolicy{})
	if !errors.Is(err, boom) || n != 0 {
		t.Fatalf("want (0, boom) got (%d, %v)", n, err)
	}
}
// --- copyBufferPolicy fast-path generic errors ---
// wtGenericErr returns (n, generic error) on WriteTo.
type wtGenericErr struct {
	n   int64
	err error
}
func (w wtGenericErr) Read(p []byte) (int, error)        { return 0, iox.EOF }
func (w wtGenericErr) WriteTo(iox.Writer) (int64, error) { return w.n, w.err }
func TestCopyPolicy_WriterToFastPath_GenericError(t *testing.T) {
	n, err := iox.CopyPolicy(bytes.NewBuffer(nil), wtGenericErr{n: 7, err: errors.New("wt-err")}, iox.YieldPolicy{})
	if n != 7 || err == nil || err.Error() != "wt-err" {
		t.Fatalf("want (7, wt-err) got (%d, %v)", n, err)
	}
}
// rfGenericErr returns (n, generic error) on ReadFrom.
type rfGenericErr struct {
	n   int64
	err error
}
func (rfGenericErr) Write(p []byte) (int, error)          { return len(p), nil }
func (r rfGenericErr) ReadFrom(iox.Reader) (int64, error) { return r.n, r.err }
func TestCopyPolicy_ReaderFromFastPath_GenericError(t *testing.T) {
	n, err := iox.CopyPolicy(rfGenericErr{n: 3, err: errors.New("rf-err")}, &simpleReader{s: []byte("abc")}, iox.YieldPolicy{})
	if n != 3 || err == nil || err.Error() != "rf-err" {
		t.Fatalf("want (3, rf-err) got (%d, %v)", n, err)
	}
}
// --- Additional tee coverage for PolicyRetry paths ---
// dataThenMore2 returns data and ErrMore in the same call.
type dataThenMore2 struct {
	data []byte
	done bool
}
func (r *dataThenMore2) Read(p []byte) (int, error) {
	if r.done {
		return 0, iox.EOF
	}
	r.done = true
	n := copy(p, r.data)
	return n, iox.ErrMore
}
func TestTeeReaderPolicy_DataThenMore_RetryReturnsNil(t *testing.T) {
	r := &dataThenMore2{data: []byte("ab")}
	var side bytes.Buffer
	pol := iox.PolicyFunc{MoreFunc: func(op iox.Op) iox.PolicyAction {
		if op == iox.OpTeeReaderRead {
			return iox.PolicyRetry
		}
		return iox.PolicyReturn
	}}
	tr := iox.TeeReaderPolicy(r, &side, pol)
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if err != nil || n != 2 || string(buf[:n]) != "ab" || side.String() != "ab" {
		t.Fatalf("n=%d err=%v read=%q side=%q", n, err, string(buf[:n]), side.String())
	}
}
// primaryMoreOnceOK: primary returns ErrMore once, then writes all.
type primaryMoreOnceOK struct {
	more bool
	buf  bytes.Buffer
}
func (w *primaryMoreOnceOK) Write(p []byte) (int, error) {
	if !w.more {
		w.more = true
		return 0, iox.ErrMore
	}
	return w.buf.Write(p)
}
func TestTeeWriterPolicy_PrimaryMore_RetryCompletes(t *testing.T) {
	p := &primaryMoreOnceOK{}
	var tee bytes.Buffer
	pol := iox.PolicyFunc{MoreFunc: func(op iox.Op) iox.PolicyAction {
		if op == iox.OpTeeWriterPrimaryWrite {
			return iox.PolicyRetry
		}
		return iox.PolicyReturn
	}}
	w := iox.TeeWriterPolicy(p, &tee, pol)
	n, err := w.Write([]byte("go"))
	if err != nil || n != 2 || p.buf.String() != "go" || tee.String() != "go" {
		t.Fatalf("n=%d err=%v primary=%q tee=%q", n, err, p.buf.String(), tee.String())
	}
}
// --- copyBufferPolicy slow-path read: data + WouldBlock with PolicyRetry returns (n,nil) ---
func TestCopyPolicy_ReadDataThenWouldBlock_RetryReturnsNil(t *testing.T) {
	var dst sliceWriter // avoid ReaderFrom fast-path; exercise slow path OpCopyRead
	r := &dataThenErrReader2{data: []byte("qq"), err: iox.ErrWouldBlock}
	pol := iox.PolicyFunc{WouldBlockFunc: func(op iox.Op) iox.PolicyAction {
		if op == iox.OpCopyRead {
			return iox.PolicyRetry
		}
		return iox.PolicyReturn
	}}
	n, err := iox.CopyPolicy(&dst, r, pol)
	if err != nil || n != 2 || string(dst.data) != "qq" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, string(dst.data))
	}
}
// --- Partial progress coverage for copyBufferPolicy and teeWriterWithPolicy ---
// writerPartialWB writes k bytes then ErrWouldBlock.
type writerPartialWB struct {
	k   int
	buf bytes.Buffer
}
func (w *writerPartialWB) Write(p []byte) (int, error) {
	if w.k <= 0 {
		return 0, iox.ErrWouldBlock
	}
	if w.k > len(p) {
		w.k = len(p)
	}
	n, _ := w.buf.Write(p[:w.k])
	w.k = 0
	return n, iox.ErrWouldBlock
}
func TestCopyPolicy_SlowPath_WritePartial_ThenWouldBlock_ReturnsCounts(t *testing.T) {
	src := bytes.NewBufferString("hello")
	dst := &writerPartialWB{k: 2}
	n, err := iox.CopyPolicy(dst, src, iox.ReturnPolicy{})
	if !iox.IsWouldBlock(err) || n != 2 || dst.buf.String() != "he" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.buf.String())
	}
}
// writerPartialMore writes k bytes then ErrMore.
type writerPartialMore struct {
	k   int
	buf bytes.Buffer
}
func (w *writerPartialMore) Write(p []byte) (int, error) {
	if w.k <= 0 {
		return 0, iox.ErrMore
	}
	if w.k > len(p) {
		w.k = len(p)
	}
	n, _ := w.buf.Write(p[:w.k])
	w.k = 0
	return n, iox.ErrMore
}
func TestCopyPolicy_SlowPath_WritePartial_ThenMore_ReturnsCounts(t *testing.T) {
	src := bytes.NewBufferString("world")
	dst := &writerPartialMore{k: 3}
	n, err := iox.CopyPolicy(dst, src, iox.ReturnPolicy{})
	if !iox.IsMore(err) || n != 3 || dst.buf.String() != "wor" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.buf.String())
	}
}
// writerZeroNil writes 0 and returns nil error – should trigger ErrShortWrite.
type writerZeroNil struct{}
func (writerZeroNil) Write([]byte) (int, error) { return 0, nil }
func TestCopyPolicy_SlowPath_WriteZeroNil_ErrShortWrite(t *testing.T) {
	src := bytes.NewBufferString("Z")
	n, err := iox.CopyPolicy(writerZeroNil{}, src, iox.YieldPolicy{})
	if !errors.Is(err, iox.ErrShortWrite) || n != 0 {
		t.Fatalf("want (0, ErrShortWrite) got (%d, %v)", n, err)
	}
}
// primaryPartialWB writes k bytes then ErrWouldBlock on primary.
type primaryPartialWB struct {
	k   int
	buf bytes.Buffer
}
func (w *primaryPartialWB) Write(p []byte) (int, error) {
	if w.k <= 0 {
		return 0, iox.ErrWouldBlock
	}
	if w.k > len(p) {
		w.k = len(p)
	}
	n, _ := w.buf.Write(p[:w.k])
	w.k = 0
	return n, iox.ErrWouldBlock
}
func TestTeeWriterPolicy_PrimaryPartialWouldBlock_ReturnsCounts(t *testing.T) {
	p := &primaryPartialWB{k: 1}
	var tee bytes.Buffer
	w := iox.TeeWriterPolicy(p, &tee, iox.ReturnPolicy{})
	n, err := w.Write([]byte("ab"))
	// Count semantics: n reflects primary progress, and tee mirrors
	// bytes accepted by primary even when primary returns a semantic boundary.
	if !iox.IsWouldBlock(err) || n != 1 || p.buf.String() != "a" || tee.String() != "a" {
		t.Fatalf("n=%d err=%v primary=%q tee=%q", n, err, p.buf.String(), tee.String())
	}
}
// teePartialMore writes k bytes then ErrMore on tee side.
type teePartialMore struct {
	k   int
	buf bytes.Buffer
}
func (w *teePartialMore) Write(p []byte) (int, error) {
	if w.k <= 0 {
		return 0, iox.ErrMore
	}
	if w.k > len(p) {
		w.k = len(p)
	}
	n, _ := w.buf.Write(p[:w.k])
	w.k = 0
	return n, iox.ErrMore
}
func TestTeeWriterPolicy_TeePartialMore_ReturnsCounts(t *testing.T) {
	var primary bytes.Buffer
	tee := &teePartialMore{k: 1}
	w := iox.TeeWriterPolicy(&primary, tee, iox.ReturnPolicy{})
	n, err := w.Write([]byte("ab"))
	// Count semantics: n reflects primary progress.
	if !iox.IsMore(err) || n != 2 || primary.String() != "ab" || tee.buf.String() != "a" {
		t.Fatalf("n=%d err=%v primary=%q tee=%q", n, err, primary.String(), tee.buf.String())
	}
}
// --- teeReaderWithPolicy n==0 semantic returns ---
type alwaysWBReader struct{}
func (alwaysWBReader) Read([]byte) (int, error) { return 0, iox.ErrWouldBlock }
type alwaysMoreReader struct{}
func (alwaysMoreReader) Read([]byte) (int, error) { return 0, iox.ErrMore }
func TestTeeReaderPolicy_ZeroThenWouldBlock_ReturnsWB(t *testing.T) {
	tr := iox.TeeReaderPolicy(alwaysWBReader{}, &bytes.Buffer{}, iox.ReturnPolicy{})
	var buf [4]byte
	n, err := tr.Read(buf[:])
	if !iox.IsWouldBlock(err) || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
func TestTeeReaderPolicy_ZeroThenMore_ReturnsMore(t *testing.T) {
	tr := iox.TeeReaderPolicy(alwaysMoreReader{}, &bytes.Buffer{}, iox.ReturnPolicy{})
	var buf [4]byte
	n, err := tr.Read(buf[:])
	if !iox.IsMore(err) || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
// Side write ErrMore once with PolicyRetry, then succeed.
type sideMoreOnceOK struct {
	tried bool
	buf   bytes.Buffer
}
func (w *sideMoreOnceOK) Write(p []byte) (int, error) {
	if !w.tried {
		w.tried = true
		return 0, iox.ErrMore
	}
	return w.buf.Write(p)
}
func TestTeeReaderPolicy_SideMore_RetryCompletes(t *testing.T) {
	r := bytes.NewBufferString("abc")
	w := &sideMoreOnceOK{}
	pol := iox.PolicyFunc{MoreFunc: func(op iox.Op) iox.PolicyAction {
		if op == iox.OpTeeReaderSideWrite {
			return iox.PolicyRetry
		}
		return iox.PolicyReturn
	}}
	tr := iox.TeeReaderPolicy(r, w, pol)
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if err != nil || n != 3 || string(buf[:n]) != "abc" || w.buf.String() != "abc" {
		t.Fatalf("n=%d err=%v read=%q side=%q", n, err, string(buf[:n]), w.buf.String())
	}
}
// --- Mixed retry branches within a single CopyPolicy slow-path write ---
// writerWBThenMore writes k bytes, then on first call returns ErrWouldBlock;
// next Write attempt returns 0, ErrMore.
type writerWBThenMore struct {
	k     int
	tried bool
	buf   bytes.Buffer
}
func (w *writerWBThenMore) Write(p []byte) (int, error) {
	if !w.tried {
		w.tried = true
		if w.k > len(p) {
			w.k = len(p)
		}
		n, _ := w.buf.Write(p[:w.k])
		return n, iox.ErrWouldBlock
	}
	return 0, iox.ErrMore
}
func TestCopyPolicy_SlowPath_WriteWouldBlockRetryThenMore_Returns(t *testing.T) {
	// Use a source that provides more than k bytes so the inner write loop
	// can retry within the same outer read.
	src := bytes.NewBufferString("abcdef")
	dst := &writerWBThenMore{k: 2}
	pol := iox.PolicyFunc{
		WouldBlockFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry },
		MoreFunc:       func(iox.Op) iox.PolicyAction { return iox.PolicyReturn },
	}
	n, err := iox.CopyPolicy(dst, src, pol)
	if !iox.IsMore(err) || n != 2 || dst.buf.String() != "ab" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.buf.String())
	}
}
// --- Fast-path WriterTo: retry then generic error ---
type wtWBThenErr struct {
	n    int64
	step int
}
func (w *wtWBThenErr) Read(p []byte) (int, error) { return 0, iox.EOF }
func (w *wtWBThenErr) WriteTo(iox.Writer) (int64, error) {
	if w.step == 0 {
		w.step = 1
		return 0, iox.ErrWouldBlock
	}
	return w.n, errors.New("wt-generic")
}
func TestCopyPolicy_WriterToFastPath_WouldBlockRetryThenGenericError(t *testing.T) {
	src := &wtWBThenErr{n: 5}
	pol := iox.PolicyFunc{WouldBlockFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	n, err := iox.CopyPolicy(bytes.NewBuffer(nil), src, pol)
	if n != 5 || err == nil || err.Error() != "wt-generic" {
		t.Fatalf("want (5, wt-generic) got (%d, %v)", n, err)
	}
}
// --- Fast-path ReaderFrom: retry then generic error ---
type rfWBThenErr struct {
	n    int64
	step int
}
func (rfWBThenErr) Write(p []byte) (int, error) { return len(p), nil }
func (w *rfWBThenErr) ReadFrom(iox.Reader) (int64, error) {
	if w.step == 0 {
		w.step = 1
		return 0, iox.ErrWouldBlock
	}
	return w.n, errors.New("rf-generic")
}
func TestCopyPolicy_ReaderFromFastPath_WouldBlockRetryThenGenericError(t *testing.T) {
	dst := &rfWBThenErr{n: 4}
	pol := iox.PolicyFunc{WouldBlockFunc: func(iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	n, err := iox.CopyPolicy(dst, &simpleReader{s: []byte("data")}, pol)
	if n != 4 || err == nil || err.Error() != "rf-generic" {
		t.Fatalf("want (4, rf-generic) got (%d, %v)", n, err)
	}
}
func TestCopyPolicy_SlowPath_ReadSide_GenericError_Returns(t *testing.T) {
	var dst sliceWriter
	other := errors.New("read-generic")
	r := &dataThenErrReader2{data: []byte("gg"), err: other}
	n, err := iox.CopyPolicy(&dst, r, iox.YieldPolicy{})
	if !errors.Is(err, other) || n != 2 || string(dst.data) != "gg" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, string(dst.data))
	}
}
// --- Slow-path: nr==0 then nil return ---
type zeroThenNilReader3 struct{ called bool }
func (r *zeroThenNilReader3) Read([]byte) (int, error) {
	if !r.called {
		r.called = true
		return 0, nil
	}
	return 0, iox.EOF
}
func TestCopyPolicy_SlowPath_ZeroThenNil_Returns(t *testing.T) {
	var dst sliceWriter
	n, err := iox.CopyPolicy(&dst, &zeroThenNilReader3{}, iox.YieldPolicy{})
	if err != nil || n != 0 || len(dst.data) != 0 {
		t.Fatalf("n=%d err=%v dstlen=%d", n, err, len(dst.data))
	}
}
// Ensure teeReaderWithPolicy returns (n, ErrWouldBlock) when policy chooses return.
func TestTeeReaderPolicy_DataThenWouldBlock_ReturnsWouldBlock(t *testing.T) {
	r := &dataThenWB2{data: []byte("xy")}
	var side bytes.Buffer
	tr := iox.TeeReaderPolicy(r, &side, iox.ReturnPolicy{})
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if !iox.IsWouldBlock(err) || n != 2 || string(buf[:n]) != "xy" || side.String() != "xy" {
		t.Fatalf("n=%d err=%v read=%q side=%q", n, err, string(buf[:n]), side.String())
	}
}
// writerMoreOnceThenOK returns (0, ErrMore) once, then writes all data.
type writerMoreOnceThenOK struct {
	tried bool
	buf   bytes.Buffer
}
func (w *writerMoreOnceThenOK) Write(p []byte) (int, error) {
	if !w.tried {
		w.tried = true
		return 0, iox.ErrMore
	}
	return w.buf.Write(p)
}
func TestCopyPolicy_SlowPath_WriteMoreZeroThenOK_Retry(t *testing.T) {
	// Use a Reader-only source to avoid WriterTo fast-path.
	step := 0
	src := &funcReader{read: func(p []byte) (int, error) {
		if step == 0 {
			step = 1
			copy(p, []byte("hi"))
			return 2, nil
		}
		return 0, iox.EOF
	}}
	dst := &writerMoreOnceThenOK{}
	var yielded []iox.Op
	pol := iox.PolicyFunc{
		YieldFunc: func(op iox.Op) { yielded = append(yielded, op) },
		MoreFunc:  func(iox.Op) iox.PolicyAction { return iox.PolicyRetry },
	}
	n, err := iox.CopyPolicy(dst, src, pol)
	if err != nil || n != 2 || dst.buf.String() != "hi" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.buf.String())
	}
	if len(yielded) == 0 || yielded[0] != iox.OpCopyWrite {
		t.Fatalf("expected yield on OpCopyWrite, got %v", yielded)
	}
}
// writerZeroMore always returns (0, ErrMore).
type writerZeroMore struct{}
func (writerZeroMore) Write([]byte) (int, error) { return 0, iox.ErrMore }
func TestCopyPolicy_SlowPath_WriteZeroMore_ReturnsMore(t *testing.T) {
	// Use bytes.Reader (implements io.Seeker) so rollback succeeds and ErrMore is returned.
	src := bytes.NewReader([]byte("x"))
	n, err := iox.CopyPolicy(writerZeroMore{}, src, iox.ReturnPolicy{})
	if !iox.IsMore(err) || n != 0 {
		t.Fatalf("want (0, ErrMore) got (%d, %v)", n, err)
	}
}
// Cover ReturnPolicy.Yield empty body.
func TestReturnPolicy_Yield_NoOp(t *testing.T) {
	var rp iox.ReturnPolicy
	// Should be a no-op; just ensure it is callable (coverage for the method body).
	rp.Yield(iox.OpCopyWrite)
}
func TestCopyN_SlowPath_UnexpectedEOF(t *testing.T) {
	// Cover CopyN slow path (dst does not implement ReaderFrom) and its
	// io.ErrUnexpectedEOF conversion when progress stops early.
	src := noWTReader{r: bytes.NewReader([]byte("abc"))}
	var dst sliceWriter
	n, err := iox.CopyN(&dst, src, 5)
	if !errors.Is(err, iox.ErrUnexpectedEOF) {
		t.Fatalf("want UnexpectedEOF got %v", err)
	}
	if n != 3 {
		t.Fatalf("n=%d", n)
	}
}
func TestCopyNBuffer_NonPositiveN_EarlyReturn(t *testing.T) {
	// Cover n<=0 early return and ensure it happens before buf length checks.
	n, err := iox.CopyNBuffer(&bytes.Buffer{}, bytes.NewBufferString("ignored"), 0, make([]byte, 0))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}
func TestCopyNBuffer_SlowPath_UnexpectedEOF(t *testing.T) {
	// Cover CopyNBuffer slow path (dst does not implement ReaderFrom) and the
	// unexpected-EOF conversion when written<n and copyBuffer returns nil.
	src := noWTReader{r: bytes.NewReader([]byte("ab"))}
	var dst sliceWriter
	n, err := iox.CopyNBuffer(&dst, src, 5, make([]byte, 8))
	if !errors.Is(err, iox.ErrUnexpectedEOF) {
		t.Fatalf("want UnexpectedEOF got %v", err)
	}
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
}
func TestCopy_GenericReadErrorPropagates(t *testing.T) {
	// Cover copyBuffer path returning a non-semantic, non-EOF error.
	oops := errors.New("oops")
	var dst sliceWriter
	n, err := iox.Copy(&dst, errReader{err: oops})
	if !errors.Is(err, oops) {
		t.Fatalf("want %v got %v", oops, err)
	}
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}
// This test documents the intended contract: on ErrMore, callers should
// return to their loop and retry later; subsequent calls continue progress.
func TestCopy_ReturnOnErrMore_AcrossCalls(t *testing.T) {
	s := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{
		{b: []byte("ab"), err: nil}, // deliver first chunk
		{b: nil, err: iox.ErrMore},  // signal multi-shot: more will follow
		{b: []byte("cd"), err: nil}, // next chunk on subsequent attempt
		{b: nil, err: iox.EOF},      // final completion
	}}
	var dst bytes.Buffer
	// First attempt should make progress and return ErrMore.
	n1, err := iox.Copy(&dst, s)
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("want ErrMore got %v", err)
	}
	if n1 != 2 || dst.String() != "ab" {
		t.Fatalf("first: n=%d dst=%q", n1, dst.String())
	}
	// Next attempt should continue and complete without error.
	n2, err := iox.Copy(&dst, s)
	if err != nil {
		t.Fatalf("unexpected err on second call: %v", err)
	}
	if n2 != 2 || dst.String() != "abcd" {
		t.Fatalf("second: n=%d dst=%q", n2, dst.String())
	}
}
