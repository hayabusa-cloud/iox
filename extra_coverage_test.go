// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"bytes"
	"testing"

	"code.hybscloud.com/iox"
)

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
