// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import "io"

// Copy copies from src to dst until either EOF is reached on src or an error occurs.
//
// iox semantics extension:
//   - ErrWouldBlock: return immediately (no progress now).
//   - ErrMore: return immediately (progress happened; more will follow).
func Copy(dst Writer, src Reader) (written int64, err error) {
	return copyBuffer(dst, src, nil)
}

// CopyBuffer is like Copy but stages through buf if needed.
// If buf is nil, a stack buffer is used.
// If buf has zero length, CopyBuffer panics.
func CopyBuffer(dst Writer, src Reader, buf []byte) (written int64, err error) {
	if buf != nil && len(buf) == 0 {
		panic("empty buffer in CopyBuffer")
	}
	return copyBuffer(dst, src, buf)
}

// CopyN copies n bytes (or until an error) from src to dst.
// On return, written == n if and only if err == nil.
//
// iox semantics extension:
//   - ErrWouldBlock / ErrMore may be returned when progress stops early.
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
