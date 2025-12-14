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
