// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"bytes"
	"testing"

	"code.hybscloud.com/iox"
)

// wtWBOnce implements Reader and WriterTo; first WriteTo would-block, then writes all.
type wtWBOnce struct {
	data     []byte
	attempts int
}

func (w *wtWBOnce) Read(p []byte) (int, error) {
	// basic Reader to satisfy the Reader type; unused in fast path
	if len(w.data) == 0 {
		return 0, iox.EOF
	}
	n := copy(p, w.data)
	w.data = w.data[n:]
	if len(w.data) == 0 {
		return n, iox.EOF
	}
	return n, nil
}

func (w *wtWBOnce) WriteTo(dst iox.Writer) (int64, error) {
	if w.attempts == 0 {
		w.attempts = 1
		return 0, iox.ErrWouldBlock
	}
	n, err := dst.Write(w.data)
	return int64(n), err
}

func TestCopyPolicy_WriterToFastPath_RetryOnWouldBlock_Completes(t *testing.T) {
	src := &wtWBOnce{data: []byte("fastpathWT")}
	var dst bytes.Buffer
	n, err := iox.CopyPolicy(&dst, src, iox.YieldPolicy{})
	if err != nil || int(n) != len("fastpathWT") || dst.String() != "fastpathWT" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}

// wtEOF returns data with io.EOF; CopyPolicy must map to nil and return total.
type wtEOF struct{ data []byte }

func (w wtEOF) Read(p []byte) (int, error) { return 0, iox.EOF }
func (w wtEOF) WriteTo(dst iox.Writer) (int64, error) {
	n, _ := dst.Write(w.data)
	return int64(n), iox.EOF
}

func TestCopyPolicy_WriterToFastPath_EOFMapsToNil(t *testing.T) {
	src := wtEOF{data: []byte("eofdata")}
	var dst bytes.Buffer
	n, err := iox.CopyPolicy(&dst, src, iox.YieldPolicy{})
	if err != nil || int(n) != len("eofdata") || dst.String() != "eofdata" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}

// wtWB returns ErrWouldBlock immediately; with ReturnPolicy it should return WB.
type wtWB struct{}

func (wtWB) Read(p []byte) (int, error)        { return 0, iox.EOF }
func (wtWB) WriteTo(iox.Writer) (int64, error) { return 0, iox.ErrWouldBlock }

func TestCopyPolicy_WriterToFastPath_WouldBlock_ReturnPolicy(t *testing.T) {
	n, err := iox.CopyPolicy(bytes.NewBuffer(nil), wtWB{}, iox.ReturnPolicy{})
	if !iox.IsWouldBlock(err) || n != 0 {
		t.Fatalf("want WouldBlock, got n=%d err=%v", n, err)
	}
}

// wtMoreThenOK returns More once, then writes on retry.
type wtMoreThenOK struct {
	data  []byte
	state int
}

func (w *wtMoreThenOK) Read(p []byte) (int, error) { return 0, iox.EOF }
func (w *wtMoreThenOK) WriteTo(dst iox.Writer) (int64, error) {
	if w.state == 0 {
		w.state = 1
		return 0, iox.ErrMore
	}
	n, _ := dst.Write(w.data)
	return int64(n), nil
}

func TestCopyPolicy_WriterToFastPath_MoreRetryThenOK(t *testing.T) {
	src := &wtMoreThenOK{data: []byte("moreok")}
	pol := iox.PolicyFunc{MoreFunc: func(op iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	var dst bytes.Buffer
	n, err := iox.CopyPolicy(&dst, src, pol)
	if err != nil || int(n) != len("moreok") || dst.String() != "moreok" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.String())
	}
}

// rfWBOnce implements Writer and ReaderFrom; first ReadFrom would-block, then drains src.
type rfWBOnce struct {
	attempts int
	buf      bytes.Buffer
}

func (w *rfWBOnce) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *rfWBOnce) ReadFrom(src iox.Reader) (int64, error) {
	if w.attempts == 0 {
		w.attempts = 1
		return 0, iox.ErrWouldBlock
	}
	var total int64
	var tmp [8]byte
	for {
		n, err := src.Read(tmp[:])
		if n > 0 {
			if m, _ := w.buf.Write(tmp[:n]); m > 0 {
				total += int64(m)
			}
		}
		if err != nil {
			if err == iox.EOF {
				return total, nil
			}
			return total, err
		}
	}
}

func TestCopyPolicy_ReaderFromFastPath_RetryOnWouldBlock_Completes(t *testing.T) {
	dst := &rfWBOnce{}
	src := &simpleReader{s: []byte("fastpathRF")}
	n, err := iox.CopyPolicy(dst, src, iox.YieldPolicy{})
	if err != nil || int(n) != len("fastpathRF") || dst.buf.String() != "fastpathRF" {
		t.Fatalf("n=%d err=%v dst=%q", n, err, dst.buf.String())
	}
}

// simpleReader implements Reader only (no WriterTo) to force ReaderFrom fast path.
type simpleReader struct {
	s   []byte
	off int
}

// rfEOF returns (n, EOF); CopyPolicy must map to nil and return total.
type rfEOF struct{}

func (rfEOF) Write(p []byte) (int, error)            { return len(p), nil }
func (rfEOF) ReadFrom(src iox.Reader) (int64, error) { return 5, iox.EOF }

func TestCopyPolicy_ReaderFromFastPath_EOFMapsToNil(t *testing.T) {
	dst := rfEOF{}
	n, err := iox.CopyPolicy(dst, &simpleReader{s: []byte("hello")}, iox.YieldPolicy{})
	if err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

// rfWB returns WouldBlock; ReturnPolicy should return WB.
type rfWB struct{}

func (rfWB) Write(p []byte) (int, error)        { return len(p), nil }
func (rfWB) ReadFrom(iox.Reader) (int64, error) { return 0, iox.ErrWouldBlock }

func TestCopyPolicy_ReaderFromFastPath_WouldBlock_ReturnPolicy(t *testing.T) {
	n, err := iox.CopyPolicy(rfWB{}, &simpleReader{s: []byte("abc")}, iox.ReturnPolicy{})
	if !iox.IsWouldBlock(err) || n != 0 {
		t.Fatalf("want WouldBlock, got n=%d err=%v", n, err)
	}
}

// rfMoreThenOK returns More once then drains.
type rfMoreThenOK struct{ state int }

func (rfMoreThenOK) Write(p []byte) (int, error) { return len(p), nil }
func (r *rfMoreThenOK) ReadFrom(src iox.Reader) (int64, error) {
	if r.state == 0 {
		r.state = 1
		return 0, iox.ErrMore
	}
	// drain src
	var tmp [16]byte
	var total int64
	for {
		n, err := src.Read(tmp[:])
		if n > 0 {
			total += int64(n)
		}
		if err != nil {
			if err == iox.EOF {
				return total, nil
			}
			return total, err
		}
	}
}

func TestCopyPolicy_ReaderFromFastPath_MoreRetryThenOK(t *testing.T) {
	pol := iox.PolicyFunc{MoreFunc: func(op iox.Op) iox.PolicyAction { return iox.PolicyRetry }}
	dst := &rfMoreThenOK{}
	n, err := iox.CopyPolicy(dst, &simpleReader{s: []byte("rfmore")}, pol)
	if err != nil || n != int64(len("rfmore")) {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func (r *simpleReader) Read(p []byte) (int, error) {
	if r.off >= len(r.s) {
		return 0, iox.EOF
	}
	n := copy(p, r.s[r.off:])
	r.off += n
	if r.off >= len(r.s) {
		return n, iox.EOF
	}
	return n, nil
}
