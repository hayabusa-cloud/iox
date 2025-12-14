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
