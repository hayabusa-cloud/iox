// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import (
	"io"
)

// Copy copies from src to dst and returns the number of bytes written.
// [ErrWouldBlock] and [ErrMore] can accompany partial progress.
//
// A [WriterTo] source takes precedence over a [ReaderFrom] destination.
// The selected method owns its progress and partial-write recovery.
// Otherwise Copy uses a read/write loop and stops on a (0, nil) read.
// An [io.EOF] from Read or a delegated method ends a successful copy.
// Copy returns an [io.EOF] from a direct Write to the caller.
//
// If a generic write accepts only part of a read and returns a semantic
// error, Copy rewinds the unwritten suffix on a [Seeker]. It returns the
// seek error if rewinding fails, or [ErrNoSeeker] if src cannot seek.
func Copy(dst Writer, src Reader) (written int64, err error) {
	return copyBuffer(dst, src, nil)
}

// CopyPolicy is [Copy] with a policy for exact [ErrWouldBlock] and [ErrMore]
// signals. A nil policy uses Copy. [PolicyRetry] calls Yield before resuming.
//
// On a partial write, the generic loop retains the unwritten suffix across
// retries. Returning the signal uses Copy's seek recovery. For a delegated
// [WriterTo] or [ReaderFrom], the policy receives the [Op] for that method.
// A retry calls the method again, so it must preserve any unwritten bytes.
func CopyPolicy(dst Writer, src Reader, policy SemanticPolicy) (written int64, err error) {
	if policy == nil {
		return copyBuffer(dst, src, nil)
	}
	return copyBufferPolicy(dst, src, nil, policy, -1)
}

// CopyBuffer is [Copy] with caller-supplied scratch space for the generic
// loop. A nil buf selects the default [Buffer]. A non-nil, empty buf panics.
// [WriterTo] and [ReaderFrom] perform their own buffering.
func CopyBuffer(dst Writer, src Reader, buf []byte) (written int64, err error) {
	if buf != nil && len(buf) == 0 {
		panic("empty buffer in CopyBuffer")
	}
	return copyBuffer(dst, src, buf)
}

// CopyBufferPolicy is [CopyPolicy] with the buffer rules of [CopyBuffer].
// A nil policy uses CopyBuffer.
func CopyBufferPolicy(dst Writer, src Reader, buf []byte, policy SemanticPolicy) (written int64, err error) {
	if buf != nil && len(buf) == 0 {
		panic("empty buffer in CopyBufferPolicy")
	}
	if policy == nil {
		return copyBuffer(dst, src, buf)
	}
	return copyBufferPolicy(dst, src, buf, policy, -1)
}

// CopyN copies n bytes from src to dst. For n <= 0 it returns (0, nil).
// For n > 0 it returns (n, nil) once the destination has accepted n bytes.
// An early EOF or a short copy with nil error returns [ErrUnexpectedEOF].
// Other errors can accompany the number of bytes already written.
//
// CopyN limits reads to the remaining byte count. It can delegate to
// [ReaderFrom] on dst. Otherwise it uses [Copy]'s generic loop and
// partial-write recovery. After a recoverable interruption, resume with
// n-written bytes.
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

	return finishCopyN(n, written, err)
}

// CopyNPolicy is [CopyN] with a policy for exact semantic signals while bytes
// remain. It skips policy callbacks for a signal received with the final
// requested byte. A nil policy uses CopyN.
func CopyNPolicy(dst Writer, src Reader, n int64, policy SemanticPolicy) (written int64, err error) {
	if n <= 0 {
		return 0, nil
	}
	if policy == nil {
		return CopyN(dst, src, n)
	}
	lr := limitedReader{R: src, N: n}
	written, err = copyBufferPolicy(dst, &lr, nil, policy, n)
	return finishCopyN(n, written, err)
}

// CopyNBuffer is [CopyN] with scratch space for the generic loop. A nil buf
// selects the default [Buffer]. For n > 0, a non-nil, empty buf panics.
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
	return finishCopyN(n, written, err)
}

// CopyNBufferPolicy is [CopyNPolicy] with the buffer rules of [CopyNBuffer].
// A nil policy uses CopyNBuffer.
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
	written, err = copyBufferPolicy(dst, &lr, buf, policy, n)
	return finishCopyN(n, written, err)
}

func finishCopyN(target, written int64, err error) (int64, error) {
	if written == target {
		return written, nil
	}
	if err == nil || err == io.EOF {
		return written, io.ErrUnexpectedEOF
	}
	return written, err
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

// rollbackSeeker attempts to rewind src by delta bytes if it implements io.Seeker.
// Returns nil on success, ErrNoSeeker if src is not seekable, or the seek error.
func rollbackSeeker(src Reader, delta int) error {
	if lr, ok := src.(*limitedReader); ok {
		if err := rollbackSeeker(lr.R, delta); err != nil {
			return err
		}
		// Restore the unread budget together with the underlying cursor.
		lr.N -= int64(delta)
		return nil
	}
	seeker, ok := src.(io.Seeker)
	if !ok {
		return ErrNoSeeker
	}
	_, err := seeker.Seek(int64(delta), io.SeekCurrent)
	return err
}

// Buffer is the default fixed-size buffer type used by Copy when none is supplied.
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

	if buf == nil {
		var local Buffer
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
					if rollbackErr := rollbackSeeker(src, nw-nr); rollbackErr != nil {
						return written, rollbackErr
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

// copyBufferPolicy takes a non-nil policy. For a bounded source, target is
// the requested write count. An unbounded copy uses -1.
func copyBufferPolicy(dst Writer, src Reader, buf []byte, policy SemanticPolicy, target int64) (written int64, err error) {
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
			if e == nil || e == io.EOF || total == target {
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

	if buf == nil {
		var local Buffer
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
					if written == target {
						return written, nil
					}
					if ew == ErrWouldBlock {
						if policy.OnWouldBlock(OpCopyWrite) == PolicyRetry {
							policy.Yield(OpCopyWrite)
							continue
						}
						// Attempt Seeker rollback on partial write when policy returns.
						if off < nr {
							if rollbackErr := rollbackSeeker(src, off-nr); rollbackErr != nil {
								return written, rollbackErr
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
							if rollbackErr := rollbackSeeker(src, off-nr); rollbackErr != nil {
								return written, rollbackErr
							}
						}
						return written, ErrMore
					}
					// Attempt Seeker rollback on partial write with semantic error.
					if off < nr && IsSemantic(ew) {
						if rollbackErr := rollbackSeeker(src, off-nr); rollbackErr != nil {
							return written, rollbackErr
						}
					}
					return written, ew
				}
				if nw == 0 {
					return written, io.ErrShortWrite
				}
			}
		}

		if er != nil {
			if er == io.EOF || written == target {
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
