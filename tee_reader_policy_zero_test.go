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
	if !errors.Is(err, iox.ErrWouldBlock) || n != 0 {
		t.Fatalf("want (0, ErrWouldBlock) got (%d, %v)", n, err)
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
