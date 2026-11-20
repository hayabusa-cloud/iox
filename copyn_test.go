// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"

	"code.hybscloud.com/iox"
)

var boundedCopies = []struct {
	name string
	copy func(io.Writer, io.Reader, int64) (int64, error)
}{
	{"CopyN", iox.CopyN},
	{"CopyNBuffer", func(w io.Writer, r io.Reader, n int64) (int64, error) {
		return iox.CopyNBuffer(w, r, n, make([]byte, 3))
	}},
	{"CopyNPolicy", func(w io.Writer, r io.Reader, n int64) (int64, error) {
		return iox.CopyNPolicy(w, r, n, iox.ReturnPolicy{})
	}},
	{"CopyNBufferPolicy", func(w io.Writer, r io.Reader, n int64) (int64, error) {
		return iox.CopyNBufferPolicy(w, r, n, make([]byte, 3), iox.ReturnPolicy{})
	}},
}

// boundedCopyWriter accepts a prefix with an error on its first write.
type boundedCopyWriter struct {
	data  []byte
	limit int
	err   error
}

func (w *boundedCopyWriter) Write(p []byte) (int, error) {
	n, err := len(p), w.err
	if err != nil {
		n = min(n, w.limit)
		w.err = nil
	}
	w.data = append(w.data, p[:n]...)
	return n, err
}

func TestCopyNPartialWriteRecovery(t *testing.T) {
	for _, variant := range boundedCopies {
		for _, control := range []struct {
			name string
			err  error
		}{
			{"WouldBlock", iox.ErrWouldBlock},
			{"More", iox.ErrMore},
			{"WrappedWouldBlock", fmt.Errorf("write: %w", iox.ErrWouldBlock)},
			{"WrappedMore", fmt.Errorf("write: %w", iox.ErrMore)},
		} {
			for _, accepted := range []int{0, 2} {
				t.Run(fmt.Sprintf("%s/%s/%d", variant.name, control.name, accepted), func(t *testing.T) {
					src := bytes.NewReader([]byte("__abcdefghTAIL"))
					if _, err := src.Seek(2, io.SeekStart); err != nil {
						t.Fatal(err)
					}
					dst := &boundedCopyWriter{limit: accepted, err: control.err}
					n, err := variant.copy(dst, src, 8)
					if n != int64(accepted) || err != control.err {
						t.Fatalf("first copy = (%d, %v), want (%d, %v)", n, err, accepted, control.err)
					}
					if pos, err := src.Seek(0, io.SeekCurrent); err != nil || pos != 2+n {
						t.Fatalf("source position = (%d, %v), want %d", pos, err, 2+n)
					}
					m, err := variant.copy(dst, src, 8-n)
					if n+m != 8 || err != nil || string(dst.data) != "abcdefgh" {
						t.Fatalf("retry = (%d, %v), total %d, data %q", m, err, n+m, dst.data)
					}
					if tail, err := io.ReadAll(src); err != nil || string(tail) != "TAIL" {
						t.Fatalf("remaining source = (%q, %v), want TAIL", tail, err)
					}
				})
			}
		}
	}
}

func TestCopyNRecoveryFailure(t *testing.T) {
	seekErr := errors.New("seek failed")
	for _, variant := range boundedCopies {
		for _, control := range []error{iox.ErrWouldBlock, iox.ErrMore} {
			for _, source := range []struct {
				name string
				new  func() io.Reader
				err  error
			}{
				{"Nonseekable", func() io.Reader {
					return struct{ io.Reader }{bytes.NewReader([]byte("abcdefgh"))}
				}, iox.ErrNoSeeker},
				{"SeekFailure", func() io.Reader {
					return &failingSeeker{data: []byte("abcdefgh"), seekErr: seekErr}
				}, seekErr},
			} {
				t.Run(variant.name+"/"+control.Error()+"/"+source.name, func(t *testing.T) {
					dst := &boundedCopyWriter{limit: 2, err: control}
					n, err := variant.copy(dst, source.new(), 8)
					if n != 2 || err != source.err || string(dst.data) != "ab" {
						t.Fatalf("copy = (%d, %v), data %q; want (2, %v), ab", n, err, dst.data, source.err)
					}
				})
			}
		}
	}
}

func TestCopyNReaderFromRetryRestoresLimit(t *testing.T) {
	for _, control := range []error{iox.ErrWouldBlock, iox.ErrMore} {
		for _, buffered := range []bool{false, true} {
			for _, nested := range []bool{false, true} {
				t.Run(fmt.Sprintf("%v/buffered=%t/nested=%t", control, buffered, nested), func(t *testing.T) {
					src := bytes.NewReader([]byte("abcdefghTAIL"))
					dst := &boundedCopyWriter{limit: 2, err: control}
					var writer io.Writer = iox.AsReaderFrom(dst)
					if nested {
						writer = boundedReaderFrom{dst: dst, n: 8, t: t}
					}
					yields := 0
					retry := func(op iox.Op) iox.PolicyAction {
						if op != iox.OpCopyReaderFrom {
							t.Fatalf("unexpected policy operation %v", op)
						}
						return iox.PolicyRetry
					}
					policy := iox.PolicyFunc{
						WouldBlockFunc: retry,
						MoreFunc:       retry,
						YieldFunc:      func(iox.Op) { yields++ },
					}
					var n int64
					var err error
					if buffered {
						n, err = iox.CopyNBufferPolicy(writer, src, 8, make([]byte, 3), policy)
					} else {
						n, err = iox.CopyNPolicy(writer, src, 8, policy)
					}
					if n != 8 || err != nil || yields != 1 || string(dst.data) != "abcdefgh" || src.Len() != 4 {
						t.Fatalf("copy = (%d, %v), yields %d, data %q, unread %d", n, err, yields, dst.data, src.Len())
					}
				})
			}
		}
	}
}

func TestCopyNPolicyStopsAtLimit(t *testing.T) {
	for _, op := range []iox.Op{iox.OpCopyRead, iox.OpCopyWrite, iox.OpCopyReaderFrom} {
		for _, signal := range []error{iox.ErrWouldBlock, iox.ErrMore} {
			for _, limit := range []int64{1, 3} {
				for _, buffered := range []bool{false, true} {
					name := fmt.Sprintf("%s/%s/limit=%d/buffered=%t", op, signal, limit, buffered)
					t.Run(name, func(t *testing.T) {
						checkCopyNPolicyLimit(t, op, signal, limit, buffered)
					})
				}
			}
		}
	}
}

func checkCopyNPolicyLimit(t *testing.T, op iox.Op, signal error, limit int64, buffered bool) {
	t.Helper()
	const data = "abcTAIL"
	src := bytes.NewBufferString(data)
	var dst countingReaderFrom

	// Each accepted byte carries a signal, including the one that completes the request.
	var r io.Reader = &funcReader{read: func(p []byte) (int, error) {
		n, err := src.Read(p[:min(1, len(p))])
		if n > 0 {
			err = signal
		}
		return n, err
	}}
	var w io.Writer = struct{ io.Writer }{&dst}
	switch op {
	case iox.OpCopyWrite:
		r = src
		w = iox.TeeWriter(errWriter{n: 1, err: signal}, &dst)
	case iox.OpCopyReaderFrom:
		w = &dst
	}

	type call struct {
		op      iox.Op
		written int
		yield   bool
	}
	var got, want []call
	retry := func(op iox.Op) iox.PolicyAction {
		got = append(got, call{op, dst.Len(), false})
		return iox.PolicyRetry
	}
	policy := iox.PolicyFunc{
		WouldBlockFunc: retry,
		MoreFunc:       retry,
		YieldFunc:      func(op iox.Op) { got = append(got, call{op, dst.Len(), true}) },
	}
	var n int64
	var err error
	if buffered {
		n, err = iox.CopyNBufferPolicy(w, r, limit, make([]byte, 3), policy)
	} else {
		n, err = iox.CopyNPolicy(w, r, limit, policy)
	}
	if n != limit || err != nil || dst.String() != data[:limit] || src.String() != data[limit:] {
		t.Fatalf("copy = (%d, %v), destination %q, remaining source %q", n, err, dst.String(), src.String())
	}
	for written := 1; written < int(limit); written++ {
		want = append(want, call{op, written, false}, call{op, written, true})
	}
	if !slices.Equal(got, want) {
		t.Errorf("policy calls = %+v, want %+v", got, want)
	}
	if op == iox.OpCopyReaderFrom && dst.calls != int(limit) {
		t.Errorf("ReadFrom calls = %d, want %d", dst.calls, limit)
	}
}

type countingReaderFrom struct {
	bytes.Buffer
	calls int
}

func (w *countingReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	w.calls++
	return w.Buffer.ReadFrom(r)
}

// A delegated bounded copy can introduce another private limiter.
type boundedReaderFrom struct {
	dst io.Writer
	n   int64
	t   *testing.T
}

func (w boundedReaderFrom) Write(p []byte) (int, error) { return w.dst.Write(p) }

func (w boundedReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	if _, ok := r.(io.Seeker); ok {
		w.t.Fatal("limiter unexpectedly exposes Seek")
	}
	if _, ok := r.(io.WriterTo); ok {
		w.t.Fatal("limiter unexpectedly exposes WriteTo")
	}
	return iox.CopyN(w.dst, r, w.n)
}
