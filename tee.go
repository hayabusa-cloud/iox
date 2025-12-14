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
//
// Count semantics:
//   - The returned n is the number of bytes read from r.
//   - If the side write fails or is short, n is still the read count.
//     This avoids byte loss: the bytes were already consumed from r.
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
		nw, ew := t.w.Write(p[:n])
		if ew != nil {
			return n, ew
		}
		if nw != n {
			return n, io.ErrShortWrite
		}
	}
	// Propagate semantic errors unchanged (including wrapped forms).
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
			// Note: returned n must remain the read count to avoid byte loss.
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
						return n, ew
					}
					if ew == ErrMore {
						if t.p.OnMore(OpTeeReaderSideWrite) == PolicyRetry {
							t.p.Yield(OpTeeReaderSideWrite)
							continue
						}
						return n, ew
					}
					return n, ew
				}
				if nw == 0 {
					return n, io.ErrShortWrite
				}
			}

			// After side write completes, decide based on read-side semantic.
			if er == ErrWouldBlock {
				if t.p.OnWouldBlock(OpTeeReaderRead) == PolicyRetry {
					t.p.Yield(OpTeeReaderRead)
					// Treat as successful read for this call.
					return n, nil
				}
				return n, er
			}
			if er == ErrMore {
				if t.p.OnMore(OpTeeReaderRead) == PolicyRetry {
					t.p.Yield(OpTeeReaderRead)
					return n, nil
				}
				return n, er
			}
			return n, er
		}

		// n == 0 path: we may need to loop on policy retry.
		if er == ErrWouldBlock {
			if t.p.OnWouldBlock(OpTeeReaderRead) == PolicyRetry {
				t.p.Yield(OpTeeReaderRead)
				continue
			}
			return 0, er
		}
		if er == ErrMore {
			if t.p.OnMore(OpTeeReaderRead) == PolicyRetry {
				t.p.Yield(OpTeeReaderRead)
				continue
			}
			return 0, er
		}
		return 0, er
	}
}

// TeeWriter returns a Writer that writes to primary and also mirrors the bytes
// accepted by primary to tee.
//
// Call order and error precedence:
//   - First, it calls primary.Write(p).
//   - If primary accepts n>0 bytes, it then calls tee.Write(p[:n]).
//   - If the tee write fails or is short, that error (or io.ErrShortWrite) is
//     returned, even if primary also returned an error.
//   - Otherwise, the primary error (if any) is returned.
//
// Special errors ErrWouldBlock and ErrMore are propagated unchanged.
//
// Count semantics:
//   - The returned n is the number of bytes accepted by the primary writer.
//   - If the tee write fails after primary has accepted bytes, n is still the
//     primary count. This makes retry-by-slicing (p[n:]) safe: it will not
//     duplicate primary writes.
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
	if n > 0 {
		n2, err2 := t.tee.Write(p[:n])
		if err2 != nil {
			return n, err2
		}
		if n2 != n {
			return n, io.ErrShortWrite
		}
	}
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

type teeWriterWithPolicy struct {
	w   Writer
	tee Writer
	p   SemanticPolicy
}

func (t teeWriterWithPolicy) Write(p []byte) (int, error) {
	// Primary write with retry per policy. As progress is accepted by primary,
	// mirror the accepted prefix to tee.
	off := 0
	for off < len(p) {
		nw, ew := t.w.Write(p[off:])
		if nw > 0 {
			// Mirror the newly accepted bytes to tee.
			teeOff := 0
			chunk := p[off : off+nw]
			for teeOff < len(chunk) {
				n2, e2 := t.tee.Write(chunk[teeOff:])
				if n2 > 0 {
					teeOff += n2
				}
				if e2 != nil {
					if e2 == ErrWouldBlock {
						if t.p.OnWouldBlock(OpTeeWriterTeeWrite) == PolicyRetry {
							t.p.Yield(OpTeeWriterTeeWrite)
							continue
						}
						return off + nw, e2
					}
					if e2 == ErrMore {
						if t.p.OnMore(OpTeeWriterTeeWrite) == PolicyRetry {
							t.p.Yield(OpTeeWriterTeeWrite)
							continue
						}
						return off + nw, e2
					}
					return off + nw, e2
				}
				if n2 == 0 {
					return off + nw, io.ErrShortWrite
				}
			}
			off += nw
		}
		if ew != nil {
			if ew == ErrWouldBlock {
				if t.p.OnWouldBlock(OpTeeWriterPrimaryWrite) == PolicyRetry {
					t.p.Yield(OpTeeWriterPrimaryWrite)
					continue
				}
				return off, ew
			}
			if ew == ErrMore {
				if t.p.OnMore(OpTeeWriterPrimaryWrite) == PolicyRetry {
					t.p.Yield(OpTeeWriterPrimaryWrite)
					continue
				}
				return off, ew
			}
			return off, ew
		}
		if nw == 0 {
			return off, io.ErrShortWrite
		}
	}
	return off, nil
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
