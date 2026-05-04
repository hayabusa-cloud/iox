// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"code.hybscloud.com/iox"
)

// -----------------------------------------------------------------------------
// Benchmark helper types
// -----------------------------------------------------------------------------

// devNull is a sink writer that discards all bytes.
type devNull struct{}

func (devNull) Write(p []byte) (int, error) { return len(p), nil }

// benchReader is a Reader-only source for generic read/write path benchmarks.
type benchReader struct {
	data []byte
	off  int
}

func (r *benchReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	if r.off >= len(r.data) {
		return n, io.EOF
	}
	return n, nil
}

// benchWT is a Reader that implements WriterTo.
type benchWT struct{ buf []byte }

func (r benchWT) Read(p []byte) (int, error) { return 0, io.EOF }

func (r benchWT) WriteTo(w iox.Writer) (int64, error) {
	n, err := w.Write(r.buf)
	return int64(n), err
}

// benchRF is a Writer that implements ReaderFrom by pulling from r.
type benchRF struct{}

func (benchRF) Write(p []byte) (int, error) { return len(p), nil }

func (benchRF) ReadFrom(r iox.Reader) (int64, error) {
	var n int64
	buf := make([]byte, 32*1024)
	for {
		nr, er := r.Read(buf)
		if nr > 0 {
			n += int64(nr)
		}
		if er != nil {
			if er == io.EOF {
				return n, nil
			}
			return n, er
		}
	}
}

// byteSize returns a human-readable size name for sub-benchmarks.
func byteSize(n int) string {
	switch {
	case n >= 1<<20:
		return "1MiB"
	case n >= 32<<10:
		return "32KiB"
	case n >= 1<<10:
		return "1KiB"
	default:
		return "bytes"
	}
}

// -----------------------------------------------------------------------------
// Copy benchmarks
// -----------------------------------------------------------------------------

func BenchmarkCopy_SlowPath(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := benchReader{data: data}
				_, err := iox.Copy(devNull{}, &src)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCopyBuffer_SlowPath(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			buf := make([]byte, 32*1024)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := benchReader{data: data}
				_, err := iox.CopyBuffer(devNull{}, &src, buf)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCopy_WriterTo(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			src := benchWT{buf: data}
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := iox.Copy(devNull{}, src)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCopy_ReaderFrom(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := bytes.NewReader(data)
				_, err := iox.Copy(benchRF{}, src)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCopyN(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := bytes.NewReader(data)
				_, err := iox.CopyN(devNull{}, src, int64(size))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCopyNBuffer(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			buf := make([]byte, 64*1024)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := bytes.NewReader(data)
				_, err := iox.CopyNBuffer(devNull{}, src, int64(size), buf)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Tee benchmarks
// -----------------------------------------------------------------------------

func BenchmarkTeeReader(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			srcData := bytes.Repeat([]byte{'x'}, size)
			buf := make([]byte, 32*1024)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				r := bytes.NewReader(srcData)
				tr := iox.TeeReader(r, devNull{})
				for {
					n, err := tr.Read(buf)
					if n == 0 || err != nil {
						break
					}
				}
			}
		})
	}
}

func BenchmarkTeeWriter(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			tw := iox.TeeWriter(devNull{}, devNull{})
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := tw.Write(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Semantics benchmarks
// -----------------------------------------------------------------------------

func BenchmarkClassify_nil(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = iox.Classify(nil)
	}
}

func BenchmarkClassify_WouldBlock(b *testing.B) {
	b.ReportAllocs()
	err := iox.ErrWouldBlock
	for i := 0; i < b.N; i++ {
		_ = iox.Classify(err)
	}
}

func BenchmarkClassify_More(b *testing.B) {
	b.ReportAllocs()
	err := iox.ErrMore
	for i := 0; i < b.N; i++ {
		_ = iox.Classify(err)
	}
}

func BenchmarkClassify_Wrapped(b *testing.B) {
	b.ReportAllocs()
	err := errors.Join(iox.ErrMore)
	for i := 0; i < b.N; i++ {
		_ = iox.Classify(err)
	}
}

// -----------------------------------------------------------------------------
// Policy benchmarks
// -----------------------------------------------------------------------------

func BenchmarkPolicy_ReturnPolicy_OnWouldBlock(b *testing.B) {
	var p iox.ReturnPolicy
	var sink iox.PolicyAction
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = p.OnWouldBlock(iox.OpCopyRead)
	}
	_ = sink
}

func BenchmarkPolicy_ReturnPolicy_OnMore(b *testing.B) {
	var p iox.ReturnPolicy
	var sink iox.PolicyAction
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = p.OnMore(iox.OpCopyWrite)
	}
	_ = sink
}

func BenchmarkPolicy_YieldPolicy_OnWouldBlock(b *testing.B) {
	var p iox.YieldPolicy
	var sink iox.PolicyAction
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = p.OnWouldBlock(iox.OpCopyRead)
	}
	_ = sink
}

func BenchmarkPolicy_YieldPolicy_OnMore(b *testing.B) {
	var p iox.YieldPolicy
	var sink iox.PolicyAction
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = p.OnMore(iox.OpCopyRead)
	}
	_ = sink
}

func BenchmarkPolicy_YieldOnWriteWouldBlock_OnWouldBlock(b *testing.B) {
	var p iox.YieldOnWriteWouldBlockPolicy
	var sink iox.PolicyAction
	ops := []iox.Op{iox.OpCopyWrite, iox.OpTeeWriterPrimaryWrite, iox.OpTeeWriterTeeWrite}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = p.OnWouldBlock(ops[i%len(ops)])
	}
	_ = sink
}

func BenchmarkPolicyFunc_Defaults(b *testing.B) {
	pf := iox.PolicyFunc{}
	var sink iox.PolicyAction
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pf.Yield(iox.OpCopyRead)
		sink = pf.OnWouldBlock(iox.OpCopyRead)
		sink = pf.OnMore(iox.OpCopyRead)
	}
	_ = sink
}

func BenchmarkOpString_All(b *testing.B) {
	ops := []iox.Op{
		iox.OpCopyRead, iox.OpCopyWrite, iox.OpCopyWriterTo, iox.OpCopyReaderFrom,
		iox.OpTeeReaderRead, iox.OpTeeReaderSideWrite, iox.OpTeeWriterPrimaryWrite, iox.OpTeeWriterTeeWrite,
	}
	var s string
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s = ops[i%len(ops)].String()
	}
	_ = s
}

// -----------------------------------------------------------------------------
// Backoff benchmarks
// -----------------------------------------------------------------------------

func BenchmarkBackoff_Duration(b *testing.B) {
	var bo iox.Backoff
	var d time.Duration
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		d = bo.Duration()
	}
	_ = d
}

func BenchmarkBackoff_Block(b *testing.B) {
	var bo iox.Backoff
	var n int
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n = bo.Block()
	}
	_ = n
}

func BenchmarkBackoff_Reset(b *testing.B) {
	var bo iox.Backoff
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bo.Reset()
	}
}

func BenchmarkBackoff_SetBase(b *testing.B) {
	var bo iox.Backoff
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bo.SetBase(time.Millisecond)
	}
}

func BenchmarkBackoff_SetMax(b *testing.B) {
	var bo iox.Backoff
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bo.SetMax(time.Second)
	}
}

// -----------------------------------------------------------------------------
// Extended semantics benchmarks
// -----------------------------------------------------------------------------

func BenchmarkIsSemantic_WouldBlock(b *testing.B) {
	err := iox.ErrWouldBlock
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsSemantic(err)
	}
	_ = sink
}

func BenchmarkIsSemantic_More(b *testing.B) {
	err := iox.ErrMore
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsSemantic(err)
	}
	_ = sink
}

func BenchmarkIsSemantic_OtherError(b *testing.B) {
	err := io.ErrClosedPipe
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsSemantic(err)
	}
	_ = sink
}

func BenchmarkIsNonFailure_nil(b *testing.B) {
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsNonFailure(nil)
	}
	_ = sink
}

func BenchmarkIsNonFailure_WouldBlock(b *testing.B) {
	err := iox.ErrWouldBlock
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsNonFailure(err)
	}
	_ = sink
}

func BenchmarkIsNonFailure_OtherError(b *testing.B) {
	err := io.ErrClosedPipe
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsNonFailure(err)
	}
	_ = sink
}

func BenchmarkIsProgress_nil(b *testing.B) {
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsProgress(nil)
	}
	_ = sink
}

func BenchmarkIsProgress_More(b *testing.B) {
	err := iox.ErrMore
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsProgress(err)
	}
	_ = sink
}

func BenchmarkIsWouldBlock(b *testing.B) {
	err := iox.ErrWouldBlock
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsWouldBlock(err)
	}
	_ = sink
}

func BenchmarkIsMore(b *testing.B) {
	err := iox.ErrMore
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsMore(err)
	}
	_ = sink
}

func BenchmarkOutcome_String(b *testing.B) {
	outcomes := []iox.Outcome{iox.OutcomeOK, iox.OutcomeWouldBlock, iox.OutcomeMore, iox.OutcomeFailure}
	var s string
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s = outcomes[i%len(outcomes)].String()
	}
	_ = s
}

// -----------------------------------------------------------------------------
// CopyPolicy benchmarks
// -----------------------------------------------------------------------------

func BenchmarkCopyPolicy_ReturnPolicy(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			var p iox.ReturnPolicy
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := bytes.NewReader(data)
				_, err := iox.CopyPolicy(devNull{}, src, p)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCopyPolicy_YieldPolicy(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			var p iox.YieldPolicy
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := bytes.NewReader(data)
				_, err := iox.CopyPolicy(devNull{}, src, p)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCopyBufferPolicy_ReturnPolicy(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			buf := make([]byte, 32*1024)
			var p iox.ReturnPolicy
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := bytes.NewReader(data)
				_, err := iox.CopyBufferPolicy(devNull{}, src, buf, p)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Copy comparison benchmarks (iox vs std io)
// -----------------------------------------------------------------------------

func BenchmarkCopy_iox_vs_io(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		data := bytes.Repeat([]byte{'x'}, size)
		b.Run("iox/"+byteSize(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := bytes.NewReader(data)
				_, err := iox.Copy(devNull{}, src)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("io/"+byteSize(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				src := bytes.NewReader(data)
				_, err := io.Copy(devNull{}, src)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// TeeReader/TeeWriter chained benchmarks
// -----------------------------------------------------------------------------

func BenchmarkTeeReader_Chained(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			srcData := bytes.Repeat([]byte{'x'}, size)
			buf := make([]byte, 32*1024)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				r := bytes.NewReader(srcData)
				// Chain two TeeReaders
				tr1 := iox.TeeReader(r, devNull{})
				tr2 := iox.TeeReader(tr1, devNull{})
				for {
					n, err := tr2.Read(buf)
					if n == 0 || err != nil {
						break
					}
				}
			}
		})
	}
}

func BenchmarkTeeWriter_Chained(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			// Chain two TeeWriters
			tw1 := iox.TeeWriter(devNull{}, devNull{})
			tw2 := iox.TeeWriter(tw1, devNull{})
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := tw2.Write(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Error wrapping benchmarks
// -----------------------------------------------------------------------------

func BenchmarkClassify_DeepWrapped(b *testing.B) {
	// Create a deeply wrapped error chain
	err := iox.ErrMore
	for range 5 {
		err = errors.Join(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = iox.Classify(err)
	}
}

func BenchmarkIsSemantic_DeepWrapped(b *testing.B) {
	err := iox.ErrWouldBlock
	for range 5 {
		err = errors.Join(err)
	}
	var sink bool
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = iox.IsSemantic(err)
	}
	_ = sink
}
