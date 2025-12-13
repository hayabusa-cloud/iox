// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import "io"

// TeeReader returns a Reader that writes to w what it reads from r.
// It mirrors io.TeeReader but propagates iox semantics:
//   - If r.Read returns data with ErrWouldBlock or ErrMore, the data is first
//     written to w, then the special error is returned unchanged.
//   - If writing to w fails, that error is returned.
//   - Short writes to w are reported as io.ErrShortWrite.
func TeeReader(r Reader, w Writer) Reader {
	return teeReader{r: r, w: w}
}

type teeReader struct {
	r Reader
	w Writer
}

func (t teeReader) Read(p []byte) (n int, err error) {
	n, err = t.r.Read(p)
	if n > 0 {
		if nw, ew := t.w.Write(p[:n]); ew != nil {
			return nw, ew
		} else if nw != n {
			return nw, io.ErrShortWrite
		}
	}
	// Map EOF to EOF as in io.TeeReader behavior
	if err == ErrWouldBlock {
		return n, ErrWouldBlock
	}
	if err == ErrMore {
		return n, ErrMore
	}
	return n, err
}

// TeeWriter returns a Writer that duplicates all writes to primary and tee.
// If writing to primary returns an error or short count, it is returned
// immediately. Otherwise, the data is written to tee. If writing to tee fails
// or is short, the error (or io.ErrShortWrite) is returned.
// Special errors ErrWouldBlock and ErrMore are propagated unchanged.
func TeeWriter(primary Writer, tee Writer) Writer {
	return teeWriter{w: primary, tee: tee}
}

type teeWriter struct {
	w   Writer
	tee Writer
}

func (t teeWriter) Write(p []byte) (n int, err error) {
	n, err = t.w.Write(p)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	n2, err2 := t.tee.Write(p)
	if err2 != nil {
		return n2, err2
	}
	if n2 != len(p) {
		return n2, io.ErrShortWrite
	}
	return len(p), nil
}

// WriterToAdapter adapts a Reader to implement WriterTo using iox.Copy.
type WriterToAdapter struct{ R Reader }

// Read forwards to the underlying Reader to preserve Reader semantics.
func (a WriterToAdapter) Read(p []byte) (int, error) { return a.R.Read(p) }

// WriteTo delegates to iox.Copy to preserve extended semantics.
func (a WriterToAdapter) WriteTo(dst Writer) (int64, error) { return Copy(dst, a.R) }

// ReaderFromAdapter adapts a Writer to implement ReaderFrom using iox.Copy.
type ReaderFromAdapter struct{ W Writer }

// Write forwards to the underlying Writer to preserve Writer semantics.
func (a ReaderFromAdapter) Write(p []byte) (int, error) { return a.W.Write(p) }

// ReadFrom delegates to iox.Copy to preserve extended semantics.
func (a ReaderFromAdapter) ReadFrom(src Reader) (int64, error) { return Copy(a.W, src) }

// AsWriterTo wraps r so that it also implements WriterTo via iox semantics.
func AsWriterTo(r Reader) Reader { return WriterToAdapter{R: r} }

// AsReaderFrom wraps w so that it also implements ReaderFrom via iox semantics.
func AsReaderFrom(w Writer) Writer { return ReaderFromAdapter{W: w} }
