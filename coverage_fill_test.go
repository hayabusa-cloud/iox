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
	if !iox.IsWouldBlock(err) || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestTeeWriterPolicy_TeeWouldBlock_ReturnsWouldBlock(t *testing.T) {
	var primary bytes.Buffer
	w := iox.TeeWriterPolicy(&primary, wbOnlyWriter{}, iox.ReturnPolicy{})
	n, err := w.Write([]byte("ab"))
	if !iox.IsWouldBlock(err) || n != 0 || primary.String() != "ab" {
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
	if !iox.IsWouldBlock(err) || n != 1 || p.buf.String() != "a" || tee.Len() != 0 {
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
	if !iox.IsMore(err) || n != 1 || primary.String() != "ab" || tee.buf.String() != "a" {
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
