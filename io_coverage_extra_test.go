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
