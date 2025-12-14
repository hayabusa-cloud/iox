// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"errors"
	"testing"

	"code.hybscloud.com/iox"
)

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
	src := &simpleReader{s: []byte("zz")}
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
