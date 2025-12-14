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

// TeeReaderPolicy is like TeeReader but consults policy when encountering
// semantic errors from r.Read or the side write to w.
//
//   - nil policy: identical to TeeReader
//   - non-nil: PolicyRetry triggers policy.Yield(op) and a retry. For read-side
//     semantics when n>0, the bytes are delivered to the caller after side write,
//     and a retry decision of PolicyRetry results in returning (n, nil).
func TeeReaderPolicy(r Reader, w Writer, policy SemanticPolicy) Reader {
	if policy == nil {
		return TeeReader(r, w)
	}
	return teeReaderWithPolicy{r: r, w: w, p: policy}
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

type teeReaderWithPolicy struct {
	r Reader
	w Writer
	p SemanticPolicy
}

func (t teeReaderWithPolicy) Read(p []byte) (int, error) {
	for {
		n, er := t.r.Read(p)
		if n > 0 {
			// Write to side, retrying on policy if needed.
			off := 0
			for off < n {
				nw, ew := t.w.Write(p[off:n])
				if nw > 0 {
					off += nw
				}
				if ew != nil {
					if ew == ErrWouldBlock {
						if t.p.OnWouldBlock(OpTeeReaderSideWrite) == PolicyRetry {
							t.p.Yield(OpTeeReaderSideWrite)
							continue
						}
						return off, ErrWouldBlock
					}
					if ew == ErrMore {
						if t.p.OnMore(OpTeeReaderSideWrite) == PolicyRetry {
							t.p.Yield(OpTeeReaderSideWrite)
							continue
						}
						return off, ErrMore
					}
					return off, ew
				}
				if nw == 0 {
					return off, io.ErrShortWrite
				}
			}

			// After side write completes, decide based on read-side semantic.
			if er == ErrWouldBlock {
				if t.p.OnWouldBlock(OpTeeReaderRead) == PolicyRetry {
					t.p.Yield(OpTeeReaderRead)
					// Treat as successful read for this call.
					return n, nil
				}
				return n, ErrWouldBlock
			}
			if er == ErrMore {
				if t.p.OnMore(OpTeeReaderRead) == PolicyRetry {
					t.p.Yield(OpTeeReaderRead)
					return n, nil
				}
				return n, ErrMore
			}
			return n, er
		}

		// n == 0 path: we may need to loop on policy retry.
		if er == ErrWouldBlock {
			if t.p.OnWouldBlock(OpTeeReaderRead) == PolicyRetry {
				t.p.Yield(OpTeeReaderRead)
				continue
			}
			return 0, ErrWouldBlock
		}
		if er == ErrMore {
			if t.p.OnMore(OpTeeReaderRead) == PolicyRetry {
				t.p.Yield(OpTeeReaderRead)
				continue
			}
			return 0, ErrMore
		}
		return 0, er
	}
}

// TeeWriter returns a Writer that duplicates all writes to primary and tee.
// If writing to primary returns an error or short count, it is returned
// immediately. Otherwise, the data is written to tee. If writing to tee fails
// or is short, the error (or io.ErrShortWrite) is returned.
// Special errors ErrWouldBlock and ErrMore are propagated unchanged.
func TeeWriter(primary Writer, tee Writer) Writer {
	return teeWriter{w: primary, tee: tee}
}

// TeeWriterPolicy is like TeeWriter but consults policy on semantic errors.
//
//   - nil policy: identical to TeeWriter
//   - non-nil: PolicyRetry yields and retries writing remaining bytes for either
//     the primary or tee writes. Short writes are reported as io.ErrShortWrite.
func TeeWriterPolicy(primary Writer, tee Writer, policy SemanticPolicy) Writer {
	if policy == nil {
		return TeeWriter(primary, tee)
	}
	return teeWriterWithPolicy{w: primary, tee: tee, p: policy}
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

type teeWriterWithPolicy struct {
	w   Writer
	tee Writer
	p   SemanticPolicy
}

func (t teeWriterWithPolicy) Write(p []byte) (int, error) {
	// Primary write with retry per policy.
	off := 0
	for off < len(p) {
		nw, ew := t.w.Write(p[off:])
		if nw > 0 {
			off += nw
		}
		if ew != nil {
			if ew == ErrWouldBlock {
				if t.p.OnWouldBlock(OpTeeWriterPrimaryWrite) == PolicyRetry {
					t.p.Yield(OpTeeWriterPrimaryWrite)
					continue
				}
				return off, ErrWouldBlock
			}
			if ew == ErrMore {
				if t.p.OnMore(OpTeeWriterPrimaryWrite) == PolicyRetry {
					t.p.Yield(OpTeeWriterPrimaryWrite)
					continue
				}
				return off, ErrMore
			}
			return off, ew
		}
		if nw == 0 {
			return off, io.ErrShortWrite
		}
	}

	// Tee write with retry per policy.
	off = 0
	for off < len(p) {
		nw, ew := t.tee.Write(p[off:])
		if nw > 0 {
			off += nw
		}
		if ew != nil {
			if ew == ErrWouldBlock {
				if t.p.OnWouldBlock(OpTeeWriterTeeWrite) == PolicyRetry {
					t.p.Yield(OpTeeWriterTeeWrite)
					continue
				}
				return off, ErrWouldBlock
			}
			if ew == ErrMore {
				if t.p.OnMore(OpTeeWriterTeeWrite) == PolicyRetry {
					t.p.Yield(OpTeeWriterTeeWrite)
					continue
				}
				return off, ErrMore
			}
			return off, ew
		}
		if nw == 0 {
			return off, io.ErrShortWrite
		}
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
