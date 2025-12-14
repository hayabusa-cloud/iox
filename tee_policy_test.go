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
	if !errors.Is(err, iox.ErrShortWrite) || n != 0 {
		t.Fatalf("want (0, ErrShortWrite) got (%d, %v)", n, err)
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
	if !errors.Is(err, iox.ErrShortWrite) || n != 0 {
		t.Fatalf("want (0, ErrShortWrite) got (%d, %v)", n, err)
	}
}
