// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"bytes"
	"testing"

	"code.hybscloud.com/iox"
)

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
