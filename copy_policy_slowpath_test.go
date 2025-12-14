// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"bytes"
	"testing"

	"code.hybscloud.com/iox"
)

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
