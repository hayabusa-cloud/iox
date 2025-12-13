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

// dataThenErrReader is provided in other test files in this package.

func TestAsWriterTo_WriteTo_PropagatesErrMore(t *testing.T) {
	src := &dataThenErrReader{data: []byte("ab"), err: iox.ErrMore}
	wrapped := iox.AsWriterTo(src)
	var dst bytes.Buffer
	n, err := iox.Copy(&dst, wrapped)
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("want ErrMore got %v", err)
	}
	if n != 2 || dst.String() != "ab" {
		t.Fatalf("n=%d dst=%q", n, dst.String())
	}
}

func TestAsReaderFrom_ReadFrom_PropagatesErrWouldBlock(t *testing.T) {
	src := &dataThenErrReader{data: []byte("zz"), err: iox.ErrWouldBlock}
	var primary bytes.Buffer
	wrapped := iox.AsReaderFrom(&primary)
	n, err := iox.Copy(wrapped, src)
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("want ErrWouldBlock got %v", err)
	}
	if n != 2 || primary.String() != "zz" {
		t.Fatalf("n=%d primary=%q", n, primary.String())
	}
}

// teeWriter: special errors (ErrMore) are propagated unchanged.
func TestTeeWriter_TeeErrMore_Propagated(t *testing.T) {
	var primary bytes.Buffer
	tee := errThenCountWriter{err: iox.ErrMore}
	tw := iox.TeeWriter(&primary, tee)
	n, err := tw.Write([]byte("hello"))
	if !errors.Is(err, iox.ErrMore) {
		t.Fatalf("want ErrMore got %v", err)
	}
	if n != len("hello") {
		t.Fatalf("n=%d", n)
	}
	if primary.String() != "hello" {
		t.Fatalf("primary=%q", primary.String())
	}
}

func TestTeeWriter_TeeWouldBlock_Propagated(t *testing.T) {
	var primary bytes.Buffer
	tee := errThenCountWriter{err: iox.ErrWouldBlock}
	tw := iox.TeeWriter(&primary, tee)
	n, err := tw.Write([]byte("zz"))
	if !errors.Is(err, iox.ErrWouldBlock) {
		t.Fatalf("want ErrWouldBlock got %v", err)
	}
	if n != len("zz") || primary.String() != "zz" {
		t.Fatalf("n=%d primary=%q", n, primary.String())
	}
}

// Helper writer that accepts all bytes and returns the provided error.
type errThenCountWriter struct{ err error }

func (w errThenCountWriter) Write(p []byte) (int, error) {
	return len(p), w.err
}
