// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import (
	"io"
)

// Copy copies from src to dst until either EOF is reached on src or an error occurs.
//
// iox semantics extension:
//   - ErrWouldBlock: return immediately because the next step would block.
//     written may be > 0 (partial progress); retry after readiness/completion.
//   - ErrMore: return immediately because progress happened and the operation remains active;
//     written may be > 0; keep polling for more completions.
//
// Partial write recovery (Seeker rollback):
//
// If dst.Write returns a semantic error (ErrWouldBlock or ErrMore) with a partial
// write (nw < nr), Copy attempts to roll back the source pointer by calling
// src.Seek(nw-nr, io.SeekCurrent) if src implements io.Seeker. This allows the
// caller to retry Copy without data loss.
//
// If src does NOT implement io.Seeker and a partial write occurs with a semantic
// error, Copy returns ErrNoSeeker to prevent silent data corruption. Callers
// using non-blocking destinations with non-seekable sources (e.g., sockets) should
// use CopyPolicy with PolicyRetry to ensure all read bytes are written before
// returning.
func Copy(dst Writer, src Reader) (written int64, err error) {
	return copyBuffer(dst, src, nil)
}

// CopyPolicy is like Copy but consults policy when encountering semantic errors.
//
// Semantics:
//   - If policy is nil, behavior is identical to Copy (default non-blocking semantics).
//   - If policy returns PolicyRetry on ErrWouldBlock/ErrMore, the engine will
//     call policy.Yield(op) and retry from that point; otherwise it returns.
//
// Partial write recovery:
//
// When policy returns PolicyReturn (not retry) on a semantic error with partial
// write progress, CopyPolicy attempts Seeker rollback on src (same as Copy).
// If src is not seekable, ErrNoSeeker is returned to prevent silent data loss.
// When policy returns PolicyRetry, the engine retries the write internally,
// ensuring all read bytes are written before the next read—no rollback needed.
//
// For non-seekable sources (e.g., network sockets) where data integrity is
// required, configure policy to return PolicyRetry for write-side semantic
// errors. This guarantees forward progress without data loss.
func CopyPolicy(dst Writer, src Reader, policy SemanticPolicy) (written int64, err error) {
	if policy == nil {
		return copyBuffer(dst, src, nil)
	}
	return copyBufferPolicy(dst, src, nil, policy)
}

// CopyBuffer is like Copy but stages through buf if needed.
// If buf is nil, a stack buffer is used.
// If buf has zero length, CopyBuffer panics.
//
// Partial write recovery: same Seeker rollback semantics as Copy. Returns
// ErrNoSeeker if src is not seekable and a partial write occurs with a
// semantic error. See Copy documentation for details.
func CopyBuffer(dst Writer, src Reader, buf []byte) (written int64, err error) {
	if buf != nil && len(buf) == 0 {
		panic("empty buffer in CopyBuffer")
	}
	return copyBuffer(dst, src, buf)
}

// CopyBufferPolicy is like CopyBuffer but consults policy on semantic errors.
//
//   - nil policy: identical to CopyBuffer
//   - non-nil: PolicyRetry triggers policy.Yield(op) and a retry; otherwise the
//     semantic error is returned unchanged.
//
// Partial write recovery: same semantics as CopyPolicy. When policy returns
// PolicyReturn on a partial write, Seeker rollback is attempted; returns
// ErrNoSeeker if src is not seekable. When policy returns PolicyRetry, the
// write is retried internally without rollback.
func CopyBufferPolicy(dst Writer, src Reader, buf []byte, policy SemanticPolicy) (written int64, err error) {
	if buf != nil && len(buf) == 0 {
		panic("empty buffer in CopyBufferPolicy")
	}
	if policy == nil {
		return copyBuffer(dst, src, buf)
	}
	return copyBufferPolicy(dst, src, buf, policy)
}

// CopyN copies n bytes (or until an error) from src to dst.
// On return, written == n if and only if err == nil.
//
// iox semantics extension:
//   - ErrWouldBlock / ErrMore may be returned when progress stops early;
//     written may be > 0 and is the number of bytes already copied.
func CopyN(dst Writer, src Reader, n int64) (written int64, err error) {
	if n <= 0 {
		return 0, nil
	}

	lr := limitedReader{R: src, N: n}

	if rf, ok := dst.(ReaderFrom); ok {
		written, err = rf.ReadFrom(&lr)
	} else {
		written, err = copyBuffer(dst, &lr, nil)
	}

	if written == n {
		return n, nil
	}

	if err == nil {
		return written, io.ErrUnexpectedEOF
	}
	if err == io.EOF {
		return written, io.ErrUnexpectedEOF
	}
	return written, err
}

// CopyNPolicy is like CopyN but consults policy on semantic errors.
//
//   - nil policy: identical to CopyN
//   - non-nil: uses the policy-aware engine; PolicyRetry yields and retries.
func CopyNPolicy(dst Writer, src Reader, n int64, policy SemanticPolicy) (written int64, err error) {
	if n <= 0 {
		return 0, nil
	}
	if policy == nil {
		return CopyN(dst, src, n)
	}
	lr := limitedReader{R: src, N: n}
	return copyBufferPolicy(dst, &lr, nil, policy)
}

// CopyNBuffer is like CopyN but stages through buf if needed.
// If buf is nil, a stack buffer is used.
// If buf has zero length, CopyNBuffer panics.
func CopyNBuffer(dst Writer, src Reader, n int64, buf []byte) (written int64, err error) {
	if n <= 0 {
		return 0, nil
	}
	if buf != nil && len(buf) == 0 {
		panic("empty buffer in CopyNBuffer")
	}
	lr := limitedReader{R: src, N: n}
	if rf, ok := dst.(ReaderFrom); ok {
		written, err = rf.ReadFrom(&lr)
	} else {
		written, err = copyBuffer(dst, &lr, buf)
	}
	if written == n {
		return n, nil
	}
	if err == nil || err == io.EOF {
		return written, io.ErrUnexpectedEOF
	}
	return written, err
}

// CopyNBufferPolicy is like CopyNBuffer but consults policy on semantic errors.
//
//   - nil policy: identical to CopyNBuffer
func CopyNBufferPolicy(dst Writer, src Reader, n int64, buf []byte, policy SemanticPolicy) (written int64, err error) {
	if n <= 0 {
		return 0, nil
	}
	if buf != nil && len(buf) == 0 {
		panic("empty buffer in CopyNBufferPolicy")
	}
	if policy == nil {
		return CopyNBuffer(dst, src, n, buf)
	}
	lr := limitedReader{R: src, N: n}
	return copyBufferPolicy(dst, &lr, buf, policy)
}

type limitedReader struct {
	R Reader
	N int64
}

func (l *limitedReader) Read(p []byte) (n int, err error) {
	if l.N <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > l.N {
		p = p[:l.N]
	}
	n, err = l.R.Read(p)
	if n > 0 {
		l.N -= int64(n)
	}
	return n, err
}

// Buffer is the default stack buffer used by Copy when none is supplied.
type Buffer [32 * 1024]byte

func copyBuffer(dst Writer, src Reader, buf []byte) (written int64, err error) {
	if wt, ok := src.(WriterTo); ok {
		written, err = wt.WriteTo(dst)
		if err == io.EOF {
			err = nil
		}
		return written, err
	}
	if rf, ok := dst.(ReaderFrom); ok {
		written, err = rf.ReadFrom(src)
		if err == io.EOF {
			err = nil
		}
		return written, err
	}

	var local Buffer
	if buf == nil {
		buf = local[:]
	}

	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				// Attempt Seeker rollback on partial write with semantic error.
				// This allows the caller to retry without data loss.
				if nw < nr && IsSemantic(ew) {
					if seeker, ok := src.(io.Seeker); ok {
						if _, seekErr := seeker.Seek(int64(nw-nr), io.SeekCurrent); seekErr != nil {
							return written, seekErr
						}
					} else {
						// Source is not seekable; unwritten bytes are unrecoverable.
						return written, ErrNoSeeker
					}
				}
				return written, ew
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}

		if er != nil {
			if er == io.EOF {
				return written, nil
			}
			if er == ErrWouldBlock {
				return written, ErrWouldBlock
			}
			if er == ErrMore {
				return written, ErrMore
			}
			return written, er
		}

		if nr == 0 {
			return written, nil
		}
	}
}

// copyBufferPolicy is a policy-aware copy implementation.
// policy is guaranteed non-nil by callers.
func copyBufferPolicy(dst Writer, src Reader, buf []byte, policy SemanticPolicy) (written int64, err error) {
	// Fast paths with policy awareness: loop and consult policy on semantic errors.
	if wt, ok := src.(WriterTo); ok {
		var total int64
		for {
			n, e := wt.WriteTo(dst)
			if n > 0 {
				total += n
			}
			if e == nil {
				return total, nil
			}
			if e == io.EOF {
				return total, nil
			}
			if e == ErrWouldBlock {
				if policy.OnWouldBlock(OpCopyWriterTo) == PolicyRetry {
					policy.Yield(OpCopyWriterTo)
					continue
				}
				return total, ErrWouldBlock
			}
			if e == ErrMore {
				if policy.OnMore(OpCopyWriterTo) == PolicyRetry {
					policy.Yield(OpCopyWriterTo)
					continue
				}
				return total, ErrMore
			}
			return total, e
		}
	}
	if rf, ok := dst.(ReaderFrom); ok {
		var total int64
		for {
			n, e := rf.ReadFrom(src)
			if n > 0 {
				total += n
			}
			if e == nil {
				return total, nil
			}
			if e == io.EOF {
				return total, nil
			}
			if e == ErrWouldBlock {
				if policy.OnWouldBlock(OpCopyReaderFrom) == PolicyRetry {
					policy.Yield(OpCopyReaderFrom)
					continue
				}
				return total, ErrWouldBlock
			}
			if e == ErrMore {
				if policy.OnMore(OpCopyReaderFrom) == PolicyRetry {
					policy.Yield(OpCopyReaderFrom)
					continue
				}
				return total, ErrMore
			}
			return total, e
		}
	}

	var local Buffer
	if buf == nil {
		buf = local[:]
	}

	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			// write possibly in multiple attempts if writer would-block/more
			off := 0
			for off < nr {
				nw, ew := dst.Write(buf[off:nr])
				if nw > 0 {
					written += int64(nw)
					off += nw
				}
				if ew != nil {
					if ew == ErrWouldBlock {
						if policy.OnWouldBlock(OpCopyWrite) == PolicyRetry {
							policy.Yield(OpCopyWrite)
							continue
						}
						// Attempt Seeker rollback on partial write when policy returns.
						if off < nr {
							if seeker, ok := src.(io.Seeker); ok {
								if _, seekErr := seeker.Seek(int64(off-nr), io.SeekCurrent); seekErr != nil {
									return written, seekErr
								}
							} else {
								// Source is not seekable; unwritten bytes are unrecoverable.
								return written, ErrNoSeeker
							}
						}
						return written, ErrWouldBlock
					}
					if ew == ErrMore {
						if policy.OnMore(OpCopyWrite) == PolicyRetry {
							policy.Yield(OpCopyWrite)
							continue
						}
						// Attempt Seeker rollback on partial write when policy returns.
						if off < nr {
							if seeker, ok := src.(io.Seeker); ok {
								if _, seekErr := seeker.Seek(int64(off-nr), io.SeekCurrent); seekErr != nil {
									return written, seekErr
								}
							} else {
								// Source is not seekable; unwritten bytes are unrecoverable.
								return written, ErrNoSeeker
							}
						}
						return written, ErrMore
					}
					return written, ew
				}
				if nw == 0 {
					return written, io.ErrShortWrite
				}
			}
		}

		if er != nil {
			if er == io.EOF {
				return written, nil
			}
			if er == ErrWouldBlock {
				if policy.OnWouldBlock(OpCopyRead) == PolicyRetry {
					policy.Yield(OpCopyRead)
					continue
				}
				return written, ErrWouldBlock
			}
			if er == ErrMore {
				if policy.OnMore(OpCopyRead) == PolicyRetry {
					policy.Yield(OpCopyRead)
					continue
				}
				return written, ErrMore
			}
			return written, er
		}

		if nr == 0 {
			return written, nil
		}
	}
}
