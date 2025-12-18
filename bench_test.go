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

// -----------------------------------------------------------------------------
// Benchmark helper types
// -----------------------------------------------------------------------------

// devNull is a sink writer that discards all bytes.
type devNull struct{}

func (devNull) Write(p []byte) (int, error) { return len(p), nil }

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
				src := bytes.NewReader(data)
				_, err := iox.Copy(devNull{}, src)
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
				src := bytes.NewReader(data)
				_, err := iox.CopyBuffer(devNull{}, src, buf)
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
