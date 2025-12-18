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
// Policy test helper types and tests
// -----------------------------------------------------------------------------

// recPolicy is a configurable SemanticPolicy used to drive retry/return
// behavior and to record Yield invocations for verification.
type recPolicy struct {
	onWB   map[iox.Op]iox.PolicyAction
	onMore map[iox.Op]iox.PolicyAction
	yields []iox.Op
}
func (p *recPolicy) Yield(op iox.Op) { p.yields = append(p.yields, op) }
func (p *recPolicy) OnWouldBlock(op iox.Op) iox.PolicyAction {
	if p.onWB != nil {
		if a, ok := p.onWB[op]; ok {
			return a
		}
	}
	return iox.PolicyReturn
}
func (p *recPolicy) OnMore(op iox.Op) iox.PolicyAction {
	if p.onMore != nil {
		if a, ok := p.onMore[op]; ok {
			return a
		}
	}
	return iox.PolicyReturn
}
// scriptedWT implements iox.Reader with WriterTo fast path producing a scripted
// sequence of (n, err) results. For each call it writes n bytes into dst.
type scriptedWT struct {
	seq []struct {
		n   int64
		err error
	}
	i int
}
func (scriptedWT) Read(p []byte) (int, error) { return 0, io.EOF }
func (s *scriptedWT) WriteTo(dst iox.Writer) (int64, error) {
	if s.i >= len(s.seq) {
		return 0, io.EOF
	}
	st := s.seq[s.i]
	s.i++
	if st.n > 0 {
		buf := bytes.Repeat([]byte{'w'}, int(st.n))
		n, _ := dst.Write(buf)
		return int64(n), st.err
	}
	return 0, st.err
}
// scriptedRF implements iox.Writer with ReaderFrom fast path producing a scripted
// sequence of (n, err) results. It ignores the src and returns scripted results.
type scriptedRF struct {
	seq []struct {
		n   int64
		err error
	}
	i int
}
func (scriptedRF) Write(p []byte) (int, error) { return len(p), nil }
func (s *scriptedRF) ReadFrom(src iox.Reader) (int64, error) {
	if s.i >= len(s.seq) {
		return 0, io.EOF
	}
	st := s.seq[s.i]
	s.i++
	return st.n, st.err
}
func TestCopyPolicy_WriterTo_WouldBlock_RetryCompletes(t *testing.T) {
	var src scriptedWT
	src.seq = append(src.seq,
		struct {
			n   int64
			err error
		}{n: 2, err: iox.ErrWouldBlock},
		struct {
			n   int64
			err error
		}{n: 3, err: nil},
	)
	var dst bytes.Buffer
	p := &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpCopyWriterTo: iox.PolicyRetry}}
	n, err := iox.CopyPolicy(&dst, &src, p)
	if err != nil || n != 5 || dst.String() != "wwwww" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
	if len(p.yields) == 0 || p.yields[0] != iox.OpCopyWriterTo {
		t.Fatalf("expected yield on OpCopyWriterTo, yields=%v", p.yields)
	}
}
func TestCopyPolicy_WriterTo_WouldBlock_Returns(t *testing.T) {
	var src scriptedWT
	src.seq = append(src.seq, struct {
		n   int64
		err error
	}{n: 4, err: iox.ErrWouldBlock})
	var dst bytes.Buffer
	p := &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpCopyWriterTo: iox.PolicyReturn}}
	n, err := iox.CopyPolicy(&dst, &src, p)
	if !errors.Is(err, iox.ErrWouldBlock) || n != 4 {
		t.Fatalf("want (4, ErrWouldBlock) got (%d, %v)", n, err)
	}
}
func TestCopyPolicy_WriterTo_More_RetryThenOK(t *testing.T) {
	var src scriptedWT
	src.seq = append(src.seq,
		struct {
			n   int64
			err error
		}{n: 2, err: iox.ErrMore},
		struct {
			n   int64
			err error
		}{n: 3, err: nil},
	)
	var dst bytes.Buffer
	p := &recPolicy{onMore: map[iox.Op]iox.PolicyAction{iox.OpCopyWriterTo: iox.PolicyRetry}}
	n, err := iox.CopyPolicy(&dst, &src, p)
	if err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
func TestCopyPolicy_WriterTo_More_Returns(t *testing.T) {
	var src scriptedWT
	src.seq = append(src.seq, struct {
		n   int64
		err error
	}{n: 7, err: iox.ErrMore})
	var dst bytes.Buffer
	p := &recPolicy{onMore: map[iox.Op]iox.PolicyAction{iox.OpCopyWriterTo: iox.PolicyReturn}}
	n, err := iox.CopyPolicy(&dst, &src, p)
	if !errors.Is(err, iox.ErrMore) || n != 7 {
		t.Fatalf("want (7, ErrMore) got (%d, %v)", n, err)
	}
}
func TestCopyPolicy_ReaderFrom_WouldBlock_RetryCompletes(t *testing.T) {
	var dst scriptedRF
	dst.seq = append(dst.seq,
		struct {
			n   int64
			err error
		}{n: 1, err: iox.ErrWouldBlock},
		struct {
			n   int64
			err error
		}{n: 4, err: nil},
	)
	src := bytes.NewBufferString("xxxxx")
	p := &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpCopyReaderFrom: iox.PolicyRetry}}
	n, err := iox.CopyPolicy(&dst, src, p)
	if err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
// Duplicate ReaderFrom fast-path More case is covered in copy_policy_misc_test.go.
func TestCopyPolicy_SlowPath_WriteShortZeroIsErrShortWrite(t *testing.T) {
	r := &simpleReader{s: []byte("abc")}
	w := shortZeroWriter{}
	p := &recPolicy{}
	n, err := iox.CopyPolicy(w, r, p)
	if !errors.Is(err, iox.ErrShortWrite) || n != 0 {
		t.Fatalf("want (0, ErrShortWrite) got (%d, %v)", n, err)
	}
}
func TestCopyPolicy_SlowPath_ReadWouldBlock_RetryThenOK(t *testing.T) {
	r := &scriptedReader{steps: []struct {
		b   []byte
		err error
	}{
		{b: []byte("hi"), err: iox.ErrWouldBlock},
		{b: []byte("!"), err: io.EOF},
	}}
	var dst sliceWriter
	p := &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpCopyRead: iox.PolicyRetry}}
	n, err := iox.CopyPolicy(&dst, r, p)
	if err != nil || n != 3 || string(dst.data) != "hi!" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, string(dst.data))
	}
}
// wtWBOnce implements Reader and WriterTo; first WriteTo would-block, then writes all.
type wtWBOnce struct {
	data     []byte
	attempts int
}
func (w *wtWBOnce) Read(p []byte) (int, error) {
	// basic Reader to satisfy the Reader type; unused in fast path
	if len(w.data) == 0 {
		return 0, iox.EOF
	}
	n := copy(p, w.data)
	w.data = w.data[n:]
	if len(w.data) == 0 {
		return n, iox.EOF
	}
	return n, nil
}
func (w *wtWBOnce) WriteTo(dst iox.Writer) (int64, error) {
	if w.attempts == 0 {
		w.attempts = 1
		return 0, iox.ErrWouldBlock
	}
	n, err := dst.Write(w.data)
	return int64(n), err
}
func TestCopyPolicy_WriterToFastPath_RetryOnWouldBlock_Completes(t *testing.T) {
	src := &wtWBOnce{data: []byte("fastpathWT")}
	var dst bytes.Buffer
	n, err := iox.CopyPolicy(&dst, src, iox.YieldPolicy{})
	if err != nil || int(n) != len("fastpathWT") || dst.String() != "fastpathWT" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
// wtEOF returns data with io.EOF; CopyPolicy must map to nil and return total.
type wtEOF struct{ data []byte }
func (w wtEOF) Read(p []byte) (int, error) { return 0, iox.EOF }
func (w wtEOF) WriteTo(dst iox.Writer) (int64, error) {
	n, _ := dst.Write(w.data)
	return int64(n), iox.EOF
}
func TestCopyPolicy_WriterToFastPath_EOFMapsToNil(t *testing.T) {
	src := wtEOF{data: []byte("eofdata")}
	var dst bytes.Buffer
	n, err := iox.CopyPolicy(&dst, src, iox.YieldPolicy{})
	if err != nil || int(n) != len("eofdata") || dst.String() != "eofdata" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
// wtWB returns ErrWouldBlock immediately; with ReturnPolicy it should return WB.
type wtWB struct{}
func (wtWB) Read(p []byte) (int, error)        { return 0, iox.EOF }
func (wtWB) WriteTo(iox.Writer) (int64, error) { return 0, iox.ErrWouldBlock }
func TestCopyPolicy_WriterToFastPath_WouldBlock_ReturnPolicy(t *testing.T) {
	n, err := iox.CopyPolicy(bytes.NewBuffer(nil), wtWB{}, iox.ReturnPolicy{})
	if !iox.IsWouldBlock(err) || n != 0 {
		t.Fatalf("want WouldBlock, got n=%d err=%v", n, err)
	}
}
// wtMoreThenOK returns More once, then writes on retry.
type wtMoreThenOK struct {
	data  []byte
	state int
}
func (w *wtMoreThenOK) Read(p []byte) (int, error) { return 0, iox.EOF }
func (w *wtMoreThenOK) WriteTo(dst iox.Writer) (int64, error) {
	if w.state == 0 {
		w.state = 1
		return 0, iox.ErrMore
	}
	n, _ := dst.Write(w.data)
	return int64(n), nil
}
func TestCopyPolicy_WriterToFastPath_MoreRetryThenOK(t *testing.T) {
	src := &wtMoreThenOK{data: []byte("moreok")}
	pol := iox.PolicyFunc{MoreFunc: func(op iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	var dst bytes.Buffer
	n, err := iox.CopyPolicy(&dst, src, pol)
	if err != nil || int(n) != len("moreok") || dst.String() != "moreok" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
// rfWBOnce implements Writer and ReaderFrom; first ReadFrom would-block, then drains src.
type rfWBOnce struct {
	attempts int
	buf      bytes.Buffer
}
func (w *rfWBOnce) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *rfWBOnce) ReadFrom(src iox.Reader) (int64, error) {
	if w.attempts == 0 {
		w.attempts = 1
		return 0, iox.ErrWouldBlock
	}
	var total int64
	var tmp [8]byte
	for {
		n, err := src.Read(tmp[:])
		if n > 0 {
			if m, _ := w.buf.Write(tmp[:n]); m > 0 {
				total += int64(m)
			}
		}
		if err != nil {
			if err == iox.EOF {
				return total, nil
			}
			return total, err
		}
	}
}
func TestCopyPolicy_ReaderFromFastPath_RetryOnWouldBlock_Completes(t *testing.T) {
	dst := &rfWBOnce{}
	src := &simpleReader{s: []byte("fastpathRF")}
	n, err := iox.CopyPolicy(dst, src, iox.YieldPolicy{})
	if err != nil || int(n) != len("fastpathRF") || dst.buf.String() != "fastpathRF" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.buf.String())
	}
}
// simpleReader implements Reader only (no WriterTo) to force ReaderFrom fast path.
type simpleReader struct {
	s   []byte
	off int
}
// rfEOF returns (n, EOF); CopyPolicy must map to nil and return total.
type rfEOF struct{}
func (rfEOF) Write(p []byte) (int, error)            { return len(p), nil }
func (rfEOF) ReadFrom(src iox.Reader) (int64, error) { return 5, iox.EOF }
func TestCopyPolicy_ReaderFromFastPath_EOFMapsToNil(t *testing.T) {
	dst := rfEOF{}
	n, err := iox.CopyPolicy(dst, &simpleReader{s: []byte("hello")}, iox.YieldPolicy{})
	if err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
// rfWB returns WouldBlock; ReturnPolicy should return WB.
type rfWB struct{}
func (rfWB) Write(p []byte) (int, error)        { return len(p), nil }
func (rfWB) ReadFrom(iox.Reader) (int64, error) { return 0, iox.ErrWouldBlock }
func TestCopyPolicy_ReaderFromFastPath_WouldBlock_ReturnPolicy(t *testing.T) {
	n, err := iox.CopyPolicy(rfWB{}, &simpleReader{s: []byte("abc")}, iox.ReturnPolicy{})
	if !iox.IsWouldBlock(err) || n != 0 {
		t.Fatalf("want WouldBlock, got n=%d err=%v", n, err)
	}
}
// rfMoreThenOK returns More once then drains.
type rfMoreThenOK struct{ state int }
func (rfMoreThenOK) Write(p []byte) (int, error) { return len(p), nil }
func (r *rfMoreThenOK) ReadFrom(src iox.Reader) (int64, error) {
	if r.state == 0 {
		r.state = 1
		return 0, iox.ErrMore
	}
	// drain src
	var tmp [16]byte
	var total int64
	for {
		n, err := src.Read(tmp[:])
		if n > 0 {
			total += int64(n)
		}
		if err != nil {
			if err == iox.EOF {
				return total, nil
			}
			return total, err
		}
	}
}
func TestCopyPolicy_ReaderFromFastPath_MoreRetryThenOK(t *testing.T) {
	pol := iox.PolicyFunc{MoreFunc: func(op iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	dst := &rfMoreThenOK{}
	n, err := iox.CopyPolicy(dst, &simpleReader{s: []byte("rfmore")}, pol)
	if err != nil || n != int64(len("rfmore")) {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
func (r *simpleReader) Read(p []byte) (int, error) {
	if r.off >= len(r.s) {
		return 0, iox.EOF
	}
	n := copy(p, r.s[r.off:])
	r.off += n
	if r.off >= len(r.s) {
		return n, iox.EOF
	}
	return n, nil
}
func TestCopyBufferPolicy_PanicOnEmptyBuffer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on empty buffer")
		}
	}()
	var dst bytes.Buffer
	src := bytes.NewBufferString("x")
	empty := make([]byte, 0)
	_, _ = iox.CopyBufferPolicy(&dst, src, empty, iox.YieldPolicy{})
}
func TestCopyNBufferPolicy_PanicOnEmptyBuffer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on empty buffer")
		}
	}()
	var dst bytes.Buffer
	src := bytes.NewBufferString("xyz")
	empty := make([]byte, 0)
	_, _ = iox.CopyNBufferPolicy(&dst, src, 2, empty, iox.YieldPolicy{})
}
func TestCopyNPolicy_ExactN_UsingPolicyPath(t *testing.T) {
	var dst bytes.Buffer
	src := bytes.NewBufferString("abc")
	n, err := iox.CopyNPolicy(&dst, src, 3, iox.YieldPolicy{})
	if err != nil || n != 3 || dst.String() != "abc" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
// wtMoreOnce triggers policy OnMore fast-path branch.
type wtMoreOnce struct{}
func (wtMoreOnce) Read(p []byte) (int, error)        { return 0, iox.EOF }
func (wtMoreOnce) WriteTo(iox.Writer) (int64, error) { return 0, iox.ErrMore }
func TestCopyPolicy_WriterToFastPath_More_Returns(t *testing.T) {
	var dst bytes.Buffer
	n, err := iox.CopyPolicy(&dst, wtMoreOnce{}, iox.YieldPolicy{})
	if !iox.IsMore(err) || n != 0 {
		t.Fatalf("want More from WriterTo fast path: n=%d err=%v", n, err)
	}
}
// rfMoreOnce triggers policy OnMore fast-path branch for ReaderFrom.
type rfMoreOnce struct{}
func (rfMoreOnce) Write(p []byte) (int, error)        { return len(p), nil }
func (rfMoreOnce) ReadFrom(iox.Reader) (int64, error) { return 0, iox.ErrMore }
func TestCopyPolicy_ReaderFromFastPath_More_Returns(t *testing.T) {
	n, err := iox.CopyPolicy(rfMoreOnce{}, &simpleReader{s: []byte("ignored")}, iox.YieldPolicy{})
	if !iox.IsMore(err) || n != 0 {
		t.Fatalf("want More from ReaderFrom fast path: n=%d err=%v", n, err)
	}
}
func TestCopyNPolicy_Short_NoUnexpectedEOF_NoErrShort(t *testing.T) {
	var dst bytes.Buffer
	// src returns fewer than N without error → UnexpectedEOF
	src := bytes.NewBufferString("ab")
	n, err := iox.CopyNPolicy(&dst, src, 3, iox.YieldPolicy{})
	// Current CopyNPolicy returns underlying result directly (no UnexpectedEOF mapping)
	if err != nil || n != 2 {
		t.Fatalf("want (2,nil) got n=%d err=%v", n, err)
	}
}
func TestCopyNPolicy_ShortEOF_NoUnexpectedEOF(t *testing.T) {
	var dst bytes.Buffer
	src := bytes.NewBufferString("a")
	n, err := iox.CopyNPolicy(&dst, src, 2, iox.YieldPolicy{})
	if err != nil || n != 1 {
		t.Fatalf("want (1,nil) got n=%d err=%v", n, err)
	}
}
func TestCopyNBufferPolicy_ExactN_WithBuf(t *testing.T) {
	var dst bytes.Buffer
	src := bytes.NewBufferString("abcd")
	buf := make([]byte, 2)
	n, err := iox.CopyNBufferPolicy(&dst, src, 4, buf, iox.YieldPolicy{})
	if err != nil || n != 4 || dst.String() != "abcd" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
func TestCopyBufferPolicy_WithBuf_SlowPath(t *testing.T) {
	var dst bytes.Buffer
	src := &simpleReader{s: []byte("xyz")}
	buf := make([]byte, 2)
	n, err := iox.CopyBufferPolicy(&dst, src, buf, iox.YieldPolicy{})
	if err != nil || n != 3 || dst.String() != "xyz" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
func TestCopyPolicy_ImmediateEOF_Nil(t *testing.T) {
	var dst bytes.Buffer
	src := &simpleReader{s: nil}
	n, err := iox.CopyPolicy(&dst, src, iox.YieldPolicy{})
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
type zeroThenNilReader2 struct{ called bool }
func (r *zeroThenNilReader2) Read(p []byte) (int, error) {
	if !r.called {
		r.called = true
		return 0, nil
	}
	return 0, iox.EOF
}
func TestCopyPolicy_ZeroThenNil_Returns(t *testing.T) {
	var dst bytes.Buffer
	n, err := iox.CopyPolicy(&dst, &zeroThenNilReader2{}, iox.YieldPolicy{})
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
// writerMoreOnce writes k bytes then returns ErrMore.
type writerMoreOnce struct {
	k   int
	buf bytes.Buffer
}
func (w *writerMoreOnce) Write(p []byte) (int, error) {
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
func TestCopyPolicy_SlowPath_WriteWouldBlock_RetryCompletes(t *testing.T) {
	dst := &partialThenWBWriter{k: 1}
	src := bytes.NewBufferString("hi")
	n, err := iox.CopyPolicy(dst, src, iox.YieldPolicy{})
	if err != nil || n != 2 || dst.buf.String() != "hi" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.buf.String())
	}
}
func TestCopyPolicy_SlowPath_WriteMore_Returns(t *testing.T) {
	dst := &writerMoreOnce{k: 1}
	src := bytes.NewBufferString("xy")
	n, err := iox.CopyPolicy(dst, src, iox.YieldPolicy{})
	if !iox.IsMore(err) || n != 1 || dst.buf.String() != "x" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.buf.String())
	}
}
func TestCopyPolicy_SlowPath_WriteZeroNilShortWrite(t *testing.T) {
	src := bytes.NewBufferString("z")
	n, err := iox.CopyPolicy(zeroWriter{}, src, iox.YieldPolicy{})
	if err == nil || err != iox.ErrShortWrite || n != 0 {
		t.Fatalf("want ErrShortWrite got n=%d err=%v", n, err)
	}
}
func TestCopyPolicy_SlowPath_ReadDataThenMore_Returns(t *testing.T) {
	r := &dataThenMoreReader{data: []byte("ab")}
	var dst bytes.Buffer
	n, err := iox.CopyPolicy(&dst, r, iox.YieldPolicy{})
	if !iox.IsMore(err) || n != 2 || dst.String() != "ab" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}
func TestPolicy_YieldMethods_NoOpCoverage(t *testing.T) {
	// Touch no-op/default Yield paths for coverage completeness.
	var rp iox.ReturnPolicy
	rp.Yield(iox.OpCopyRead)
	var ywp iox.YieldOnWriteWouldBlockPolicy
	ywp.Yield(iox.OpCopyWrite)
}
// wbOnceThenOK writer returns (0, ErrWouldBlock) once, then writes all.
type wbOnceThenOK struct{ once bool }
func (w *wbOnceThenOK) Write(p []byte) (int, error) {
	if !w.once {
		w.once = true
		return 0, iox.ErrWouldBlock
	}
	return len(p), nil
}
// errAfterN writer writes up to n bytes then returns a generic error.
type errAfterN struct {
	left int
}
func (w *errAfterN) Write(p []byte) (int, error) {
	if w.left <= 0 {
		return 0, errors.New("boom")
	}
	if w.left > len(p) {
		w.left = len(p)
	}
	n := w.left
	w.left = 0
	return n, errors.New("boom")
}
func TestCopyPolicy_SlowPath_WriteWouldBlock_RetryBranch(t *testing.T) {
	src := &simpleReader{s: []byte("abcd")}
	dst := &wbOnceThenOK{}
	pol := &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpCopyWrite: iox.PolicyRetry}}
	n, err := iox.CopyPolicy(dst, src, pol)
	if err != nil || n != 4 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
func TestCopyPolicy_SlowPath_WriteWouldBlock_ReturnBranch(t *testing.T) {
	// Use bytes.Reader (implements io.Seeker) so rollback succeeds and ErrWouldBlock is returned.
	src := bytes.NewReader([]byte("zz"))
	dst := &wbOnceThenOK{}
	// Return on writer would-block
	pol := &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpCopyWrite: iox.PolicyReturn}}
	n, err := iox.CopyPolicy(dst, src, pol)
	if !errors.Is(err, iox.ErrWouldBlock) || n != 0 {
		t.Fatalf("want (0, ErrWouldBlock) got (%d, %v)", n, err)
	}
}
func TestCopyPolicy_SlowPath_WriteGenericError_AfterProgress(t *testing.T) {
	src := &simpleReader{s: []byte("xx")}
	dst := &errAfterN{left: 1}
	n, err := iox.CopyPolicy(dst, src, &recPolicy{})
	if err == nil || err.Error() != "boom" || n != 1 {
		t.Fatalf("want (1, boom) got (%d, %v)", n, err)
	}
}
// Readers to hit read-side return branches without fast paths.
type zeroWBReader struct{}
func (zeroWBReader) Read([]byte) (int, error) { return 0, iox.ErrWouldBlock }
type zeroMoreThenEOFReader struct{ once bool }
func (r *zeroMoreThenEOFReader) Read([]byte) (int, error) {
	if !r.once {
		r.once = true
		return 0, iox.ErrMore
	}
	return 0, iox.EOF
}
func TestCopyPolicy_SlowPath_ReadWouldBlock_Returns(t *testing.T) {
	n, err := iox.CopyPolicy(&sliceWriter{}, zeroWBReader{}, &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpCopyRead: iox.PolicyReturn}})
	if !errors.Is(err, iox.ErrWouldBlock) || n != 0 {
		t.Fatalf("want (0, ErrWouldBlock) got (%d, %v)", n, err)
	}
}
func TestCopyPolicy_SlowPath_ReadMore_RetryThenEOF(t *testing.T) {
	n, err := iox.CopyPolicy(&sliceWriter{}, &zeroMoreThenEOFReader{}, &recPolicy{onMore: map[iox.Op]iox.PolicyAction{iox.OpCopyRead: iox.PolicyRetry}})
	// After retry, EOF is absorbed as nil per Copy semantics.
	if err != nil || n != 0 {
		t.Fatalf("want (0, nil) got (%d, %v)", n, err)
	}
}
func TestOpString_AllValuesAndUnknown(t *testing.T) {
	cases := []struct {
		op   iox.Op
		want string
	}{
		{iox.OpCopyRead, "CopyRead"},
		{iox.OpCopyWrite, "CopyWrite"},
		{iox.OpCopyWriterTo, "CopyWriterTo"},
		{iox.OpCopyReaderFrom, "CopyReaderFrom"},
		{iox.OpTeeReaderRead, "TeeReaderRead"},
		{iox.OpTeeReaderSideWrite, "TeeReaderSideWrite"},
		{iox.OpTeeWriterPrimaryWrite, "TeeWriterPrimaryWrite"},
		{iox.OpTeeWriterTeeWrite, "TeeWriterTeeWrite"},
		{iox.Op(255), "Op(unknown)"}, // default branch
	}
	for _, tc := range cases {
		if got := tc.op.String(); got != tc.want {
			t.Fatalf("op=%v got=%q want=%q", tc.op, got, tc.want)
		}
	}
}
func TestPolicies_Return_Yield_YieldOnWriteWouldBlock(t *testing.T) {
	// ReturnPolicy always returns PolicyReturn.
	var rp iox.ReturnPolicy
	if rp.OnWouldBlock(iox.OpCopyRead) != iox.PolicyReturn || rp.OnMore(iox.OpCopyRead) != iox.PolicyReturn {
		t.Fatalf("ReturnPolicy must always return PolicyReturn")
	}
	// YieldPolicy retries only on would-block.
	var yp iox.YieldPolicy
	if yp.OnWouldBlock(iox.OpCopyRead) != iox.PolicyRetry {
		t.Fatalf("YieldPolicy OnWouldBlock should retry")
	}
	if yp.OnMore(iox.OpCopyRead) != iox.PolicyReturn {
		t.Fatalf("YieldPolicy OnMore should return")
	}
	// YieldOnWriteWouldBlockPolicy: retry on writer-side ops only.
	var ywp iox.YieldOnWriteWouldBlockPolicy
	writeOps := []iox.Op{
		iox.OpCopyWrite, iox.OpCopyWriterTo, iox.OpCopyReaderFrom,
		iox.OpTeeReaderSideWrite, iox.OpTeeWriterPrimaryWrite, iox.OpTeeWriterTeeWrite,
	}
	for _, op := range writeOps {
		if ywp.OnWouldBlock(op) != iox.PolicyRetry {
			t.Fatalf("YieldOnWriteWouldBlockPolicy OnWouldBlock(%v) should retry", op)
		}
	}
	if ywp.OnWouldBlock(iox.OpCopyRead) != iox.PolicyReturn || ywp.OnMore(iox.OpCopyRead) != iox.PolicyReturn {
		t.Fatalf("YieldOnWriteWouldBlockPolicy non-write ops should return")
	}
}
func TestPolicyFunc_DefaultsAndOverrides(t *testing.T) {
	// Defaults: return PolicyReturn; Yield should be callable (covers default path).
	pf := iox.PolicyFunc{}
	_ = pf.OnWouldBlock(iox.OpCopyRead)
	_ = pf.OnMore(iox.OpCopyRead)
	pf.Yield(iox.OpCopyRead)
	// Overrides: ensure our funcs are invoked.
	var gotYield, gotWB, gotMore bool
	pf2 := iox.PolicyFunc{
		YieldFunc: func(op iox.Op) { gotYield = true },
		WouldBlockFunc: func(op iox.Op) iox.PolicyAction {
			gotWB = true
			return iox.PolicyRetry
		},
		MoreFunc: func(op iox.Op) iox.PolicyAction {
			gotMore = true
			return iox.PolicyReturn
		},
	}
	_ = pf2.OnWouldBlock(iox.OpCopyWrite)
	_ = pf2.OnMore(iox.OpCopyWrite)
	pf2.Yield(iox.OpCopyWrite)
	if !gotYield || !gotWB || !gotMore {
		t.Fatalf("PolicyFunc overrides not invoked: yield=%v wb=%v more=%v", gotYield, gotWB, gotMore)
	}
}
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
