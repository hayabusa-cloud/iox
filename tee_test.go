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

// -----------------------------------------------------------------------------
// TeeReader and TeeWriter tests
// -----------------------------------------------------------------------------

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
	// Count semantics: n reflects bytes already consumed from r.
	if n != 2 {
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
	// Count semantics: n reflects primary progress.
	if n != 2 {
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
func TestTeeReaderPolicy_ReadWouldBlock_RetryReturnsDataNil(t *testing.T) {
	r := &dataThenErrReader{data: []byte("abc"), err: iox.ErrWouldBlock}
	var side bytes.Buffer
	pol := &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpTeeReaderRead: iox.PolicyRetry}}
	tr := iox.TeeReaderPolicy(r, &side, pol)
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if err != nil || n != 3 || side.String() != "abc" || string(buf[:n]) != "abc" {
		t.Fatalf("n=%d err=%v side=%q read=%q", n, err, side.String(), string(buf[:n]))
	}
}
func TestTeeReaderPolicy_ReadMore_RetryReturnsDataNil(t *testing.T) {
	r := &dataThenErrReader{data: []byte("zz"), err: iox.ErrMore}
	var side bytes.Buffer
	pol := &recPolicy{onMore: map[iox.Op]iox.PolicyAction{iox.OpTeeReaderRead: iox.PolicyRetry}}
	tr := iox.TeeReaderPolicy(r, &side, pol)
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if err != nil || n != 2 || side.String() != "zz" {
		t.Fatalf("n=%d err=%v side=%q", n, err, side.String())
	}
}
func TestTeeReaderPolicy_SideWriteErrMore_Returns(t *testing.T) {
	r := bytes.NewBufferString("hi")
	side := errThenCountWriter{err: iox.ErrMore}
	tr := iox.TeeReaderPolicy(r, side, &recPolicy{onMore: map[iox.Op]iox.PolicyAction{iox.OpTeeReaderSideWrite: iox.PolicyReturn}})
	buf := make([]byte, 4)
	n, err := tr.Read(buf)
	if !errors.Is(err, iox.ErrMore) || n != 2 {
		t.Fatalf("want (2, ErrMore) got (%d, %v)", n, err)
	}
}
func TestTeeReaderPolicy_SideWriteShortZero_ErrShortWrite(t *testing.T) {
	r := bytes.NewBufferString("k")
	tr := iox.TeeReaderPolicy(r, shortZeroWriter{}, &recPolicy{})
	var b [1]byte
	n, err := tr.Read(b[:])
	// Count semantics: n reflects bytes consumed from the source.
	if !errors.Is(err, iox.ErrShortWrite) || n != 1 {
		t.Fatalf("want (1, ErrShortWrite) got (%d, %v)", n, err)
	}
}
func TestTeeWriterPolicy_TeeWouldBlock_RetryCompletes(t *testing.T) {
	var primary bytes.Buffer
	tee := &partialThenWBWriter{k: 1}
	w := iox.TeeWriterPolicy(&primary, tee, &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpTeeWriterTeeWrite: iox.PolicyRetry}})
	n, err := w.Write([]byte("ok"))
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if primary.String() != "ok" || tee.buf.String() != "ok" {
		t.Fatalf("primary=%q tee=%q", primary.String(), tee.buf.String())
	}
}
func TestTeeWriterPolicy_PrimaryShortZero_ErrShortWrite(t *testing.T) {
	w := iox.TeeWriterPolicy(shortZeroWriter{}, &bytes.Buffer{}, &recPolicy{})
	n, err := w.Write([]byte("x"))
	if !errors.Is(err, iox.ErrShortWrite) || n != 0 {
		t.Fatalf("want (0, ErrShortWrite) got (%d, %v)", n, err)
	}
}
func TestTeeWriterPolicy_TeeShortZero_ErrShortWrite(t *testing.T) {
	w := iox.TeeWriterPolicy(&bytes.Buffer{}, shortZeroWriter{}, &recPolicy{})
	n, err := w.Write([]byte("x"))
	// Count semantics: n reflects primary progress.
	if !errors.Is(err, iox.ErrShortWrite) || n != 1 {
		t.Fatalf("want (1, ErrShortWrite) got (%d, %v)", n, err)
	}
}

// dataThenMoreReader returns data with ErrMore once.
type dataThenMoreReader struct {
	data []byte
	done bool
}

func (r *dataThenMoreReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, iox.EOF
	}
	r.done = true
	n := copy(p, r.data)
	return n, iox.ErrMore
}

// sideMoreWriter writes up to k bytes then returns ErrMore.
type sideMoreWriter struct{ k int }

func (w *sideMoreWriter) Write(p []byte) (int, error) {
	if w.k <= 0 {
		return 0, iox.ErrMore
	}
	if w.k > len(p) {
		w.k = len(p)
	}
	n := w.k
	w.k = 0
	return n, iox.ErrMore
}

// zeroWriter returns (0, nil), violating io contract → triggers ErrShortWrite in engines.
type zeroWriter struct{}

func (zeroWriter) Write(p []byte) (int, error) { return 0, nil }
func TestTeeReaderPolicy_ReadDataThenMore_ReturnsMore(t *testing.T) {
	r := &dataThenMoreReader{data: []byte("ab")}
	var side bytes.Buffer
	tr := iox.TeeReaderPolicy(r, &side, iox.YieldPolicy{})
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if !iox.IsMore(err) || n != 2 || string(buf[:n]) != "ab" || side.String() != "ab" {
		t.Fatalf("n=%d err=%v read=%q side=%q", n, err, string(buf[:n]), side.String())
	}
}
func TestTeeReaderPolicy_SideWriteMore_ReturnsMore(t *testing.T) {
	r := bytes.NewBufferString("abc")
	w := &sideMoreWriter{k: 2}
	tr := iox.TeeReaderPolicy(r, w, iox.YieldPolicy{})
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	// Count semantics: n reflects bytes consumed from the source,
	// even when the side writer returns a boundary/error.
	if !iox.IsMore(err) || n != 3 || string(buf[:n]) != "abc" {
		t.Fatalf("n=%d err=%v data=%q", n, err, string(buf[:n]))
	}
}
func TestTeeReaderPolicy_SideShortWrite_Error(t *testing.T) {
	r := bytes.NewBufferString("x")
	tr := iox.TeeReaderPolicy(r, zeroWriter{}, iox.YieldPolicy{})
	buf := make([]byte, 2)
	n, err := tr.Read(buf)
	if err == nil || err != iox.ErrShortWrite || n != 1 {
		t.Fatalf("want (1, ErrShortWrite) got n=%d err=%v", n, err)
	}
}

// wouldBlockOnceWriter reused for tee side retry; defined in policy_copy_tee_test.go
func TestTeeWriterPolicy_TeeWouldBlockOnce_Completes(t *testing.T) {
	var primary bytes.Buffer
	tee := &wouldBlockOnceWriter{}
	tw := iox.TeeWriterPolicy(&primary, tee, iox.YieldPolicy{})
	n, err := tw.Write([]byte("zz"))
	if err != nil || n != 2 || primary.String() != "zz" || tee.buf.String() != "zz" {
		t.Fatalf("n=%d err=%v primary=%q tee=%q", n, err, primary.String(), tee.buf.String())
	}
}

type moreWriter struct{ k int }

func (w *moreWriter) Write(p []byte) (int, error) {
	if w.k <= 0 {
		return 0, iox.ErrMore
	}
	if w.k > len(p) {
		w.k = len(p)
	}
	n := w.k
	w.k = 0
	return n, iox.ErrMore
}
func TestTeeWriterPolicy_PrimaryMore_Returns(t *testing.T) {
	p := &moreWriter{k: 1}
	var tee bytes.Buffer
	tw := iox.TeeWriterPolicy(p, &tee, iox.YieldPolicy{})
	n, err := tw.Write([]byte("xy"))
	if !iox.IsMore(err) || n != 1 {
		t.Fatalf("want More after 1 byte, got n=%d err=%v", n, err)
	}
	// Count semantics: tee mirrors the bytes accepted by primary.
	if tee.String() != "x" {
		t.Fatalf("tee=%q", tee.String())
	}
}
func TestTeeWriterPolicy_TeeMore_Returns(t *testing.T) {
	var primary bytes.Buffer
	tee := &moreWriter{k: 0} // immediate ErrMore with 0 written
	tw := iox.TeeWriterPolicy(&primary, tee, iox.YieldPolicy{})
	n, err := tw.Write([]byte("q"))
	// Count semantics: n reflects primary progress.
	if !iox.IsMore(err) || n != 1 || primary.String() != "q" {
		t.Fatalf("n=%d err=%v primary=%q", n, err, primary.String())
	}
}
func TestTeeWriterPolicy_TeeShortWrite_Error(t *testing.T) {
	var primary bytes.Buffer
	tw := iox.TeeWriterPolicy(&primary, zeroWriter{}, iox.YieldPolicy{})
	n, err := tw.Write([]byte("hello"))
	// Count semantics: n reflects primary progress.
	if err == nil || err != iox.ErrShortWrite || n != 5 {
		t.Fatalf("want (5, ErrShortWrite) got n=%d err=%v", n, err)
	}
}
func TestTeeReaderPolicy_SideGenericError_Returns(t *testing.T) {
	r := bytes.NewBufferString("yz")
	fw := &failAfterWriter{k: 1, err: errors.New("side-err")}
	tr := iox.TeeReaderPolicy(r, fw, iox.YieldPolicy{})
	buf := make([]byte, 4)
	n, err := tr.Read(buf)
	// Count semantics: n reflects bytes consumed from the source.
	if err == nil || n != 2 || err.Error() != "side-err" {
		t.Fatalf("want side-err with n=2, got n=%d err=%v", n, err)
	}
}

// dataThenErrReader exists in tee_test.go; reuse with EOF to hit EOF branch.
func TestTeeReaderPolicy_DataThenEOF_SameCall(t *testing.T) {
	r := &dataThenErrReader{data: []byte("ok"), err: iox.EOF}
	var side bytes.Buffer
	tr := iox.TeeReaderPolicy(r, &side, iox.YieldPolicy{})
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if err != iox.EOF || n != 2 || side.String() != "ok" || string(buf[:n]) != "ok" {
		t.Fatalf("n=%d err=%v side=%q got=%q", n, err, side.String(), string(buf[:n]))
	}
}
func TestTeeWriterPolicy_PrimaryGenericError_Returns(t *testing.T) {
	pw := &failAfterWriter{k: 1, err: errors.New("primary-err")}
	var tee bytes.Buffer
	tw := iox.TeeWriterPolicy(pw, &tee, iox.YieldPolicy{})
	n, err := tw.Write([]byte("ab"))
	if err == nil || n != 1 || err.Error() != "primary-err" {
		t.Fatalf("want primary-err after 1 byte, got n=%d err=%v", n, err)
	}
}
func TestTeeWriterPolicy_TeeGenericError_Returns(t *testing.T) {
	var primary bytes.Buffer
	tw := iox.TeeWriterPolicy(&primary, errZeroWriter{err: errors.New("tee-err")}, iox.YieldPolicy{})
	n, err := tw.Write([]byte("xy"))
	// Count semantics: n reflects primary progress.
	if err == nil || n != 2 || err.Error() != "tee-err" || primary.String() != "xy" {
		t.Fatalf("want tee-err with n=2, got n=%d err=%v primary=%q", n, err, primary.String())
	}
}

// sideWB writer always would-block without progress.
type sideWB struct{}

func (sideWB) Write([]byte) (int, error) { return 0, iox.ErrWouldBlock }

// sideMoreOnce writer returns ErrMore once, then writes all.
type sideMoreOnce struct {
	once bool
	buf  bytes.Buffer
}

func (w *sideMoreOnce) Write(p []byte) (int, error) {
	if !w.once {
		w.once = true
		return 0, iox.ErrMore
	}
	n, _ := w.buf.Write(p)
	return n, nil
}

// rWBThenEOF returns (0, ErrWouldBlock) then EOF.
type rWBThenEOF struct{ once bool }

func (r *rWBThenEOF) Read([]byte) (int, error) {
	if !r.once {
		r.once = true
		return 0, iox.ErrWouldBlock
	}
	return 0, iox.EOF
}

// rMoreThenEOF returns (0, ErrMore) then EOF.
type rMoreThenEOF struct{ once bool }

func (r *rMoreThenEOF) Read([]byte) (int, error) {
	if !r.once {
		r.once = true
		return 0, iox.ErrMore
	}
	return 0, iox.EOF
}
func TestTeeReaderPolicy_SideWrite_WouldBlock_Returns(t *testing.T) {
	r := bytes.NewBufferString("x")
	tr := iox.TeeReaderPolicy(r, sideWB{}, &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpTeeReaderSideWrite: iox.PolicyReturn}})
	var b [1]byte
	n, err := tr.Read(b[:])
	// Count semantics: n reflects bytes consumed from the source.
	if !errors.Is(err, iox.ErrWouldBlock) || n != 1 {
		t.Fatalf("want (1, ErrWouldBlock) got (%d, %v)", n, err)
	}
}
func TestTeeReaderPolicy_SideWrite_More_RetryThenOK(t *testing.T) {
	r := bytes.NewBufferString("OK")
	var side sideMoreOnce
	tr := iox.TeeReaderPolicy(r, &side, &recPolicy{onMore: map[iox.Op]iox.PolicyAction{iox.OpTeeReaderSideWrite: iox.PolicyRetry}})
	buf := make([]byte, 8)
	n, err := tr.Read(buf)
	if err != nil || n != 2 || string(buf[:n]) != "OK" || side.buf.String() != "OK" {
		t.Fatalf("n=%d err=%v read=%q side=%q", n, err, string(buf[:n]), side.buf.String())
	}
}
func TestTeeReaderPolicy_ReadZero_WouldBlock_Returns(t *testing.T) {
	tr := iox.TeeReaderPolicy(&rWBThenEOF{}, &bytes.Buffer{}, &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpTeeReaderRead: iox.PolicyReturn}})
	var b [1]byte
	n, err := tr.Read(b[:0])
	if !errors.Is(err, iox.ErrWouldBlock) || n != 0 {
		t.Fatalf("want (0, ErrWouldBlock) got (%d, %v)", n, err)
	}
}
func TestTeeReaderPolicy_ReadZero_WouldBlock_RetryThenEOF(t *testing.T) {
	tr := iox.TeeReaderPolicy(&rWBThenEOF{}, &bytes.Buffer{}, &recPolicy{onWB: map[iox.Op]iox.PolicyAction{iox.OpTeeReaderRead: iox.PolicyRetry}})
	var b [1]byte
	n, err := tr.Read(b[:0])
	if err != iox.EOF || n != 0 {
		t.Fatalf("want (0, EOF) got (%d, %v)", n, err)
	}
}
func TestTeeReaderPolicy_ReadZero_More_Returns(t *testing.T) {
	tr := iox.TeeReaderPolicy(&rMoreThenEOF{}, &bytes.Buffer{}, &recPolicy{onMore: map[iox.Op]iox.PolicyAction{iox.OpTeeReaderRead: iox.PolicyReturn}})
	var b [1]byte
	n, err := tr.Read(b[:0])
	if !errors.Is(err, iox.ErrMore) || n != 0 {
		t.Fatalf("want (0, ErrMore) got (%d, %v)", n, err)
	}
}
func TestTeeReaderPolicy_ReadZero_More_RetryThenEOF(t *testing.T) {
	tr := iox.TeeReaderPolicy(&rMoreThenEOF{}, &bytes.Buffer{}, &recPolicy{onMore: map[iox.Op]iox.PolicyAction{iox.OpTeeReaderRead: iox.PolicyRetry}})
	var b [1]byte
	n, err := tr.Read(b[:0])
	if err != iox.EOF || n != 0 {
		t.Fatalf("want (0, EOF) got (%d, %v)", n, err)
	}
}
