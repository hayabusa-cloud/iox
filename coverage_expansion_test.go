// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"errors"
	"io"
	"testing"

	"code.hybscloud.com/iox"
)

// =============================================================================
// Mock types for coverage expansion
// =============================================================================

// failingSeeker is a ReadSeeker that returns data on Read but fails on Seek.
type failingSeeker struct {
	data    []byte
	pos     int
	seekErr error
}

func (f *failingSeeker) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *failingSeeker) Seek(offset int64, whence int) (int64, error) {
	return 0, f.seekErr
}

// partialWBWriter writes partial data and returns ErrWouldBlock.
// It writes exactly `partial` bytes, then returns ErrWouldBlock.
type partialWBWriter struct {
	partial int
	buf     []byte
}

func (w *partialWBWriter) Write(p []byte) (int, error) {
	if w.partial <= 0 {
		return 0, iox.ErrWouldBlock
	}
	n := w.partial
	if n > len(p) {
		n = len(p)
	}
	w.buf = append(w.buf, p[:n]...)
	w.partial = 0
	return n, iox.ErrWouldBlock
}

// partialMoreWriter writes partial data and returns ErrMore.
type partialMoreWriter struct {
	partial int
	buf     []byte
}

func (w *partialMoreWriter) Write(p []byte) (int, error) {
	if w.partial <= 0 {
		return 0, iox.ErrMore
	}
	n := w.partial
	if n > len(p) {
		n = len(p)
	}
	w.buf = append(w.buf, p[:n]...)
	w.partial = 0
	return n, iox.ErrMore
}

// plainReader is a simple Reader that does NOT implement Seeker.
type plainReader struct {
	data []byte
	pos  int
}

func (r *plainReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// =============================================================================
// Test: Seeker Rollback Failures in copyBuffer
// =============================================================================

func TestCopy_SeekerRollbackFailure_ErrWouldBlock(t *testing.T) {
	t.Run("Seek fails on partial write with ErrWouldBlock", func(t *testing.T) {
		seekErr := errors.New("seek failed")
		src := &failingSeeker{data: []byte("hello"), seekErr: seekErr}
		dst := &partialWBWriter{partial: 2} // writes 2 bytes, then ErrWouldBlock

		n, err := iox.Copy(dst, src)

		// Should return the seek error, not ErrWouldBlock
		if !errors.Is(err, seekErr) {
			t.Fatalf("expected seek error, got: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected n=2, got n=%d", n)
		}
	})
}

func TestCopy_SeekerRollbackFailure_ErrMore(t *testing.T) {
	t.Run("Seek fails on partial write with ErrMore", func(t *testing.T) {
		seekErr := errors.New("seek failed")
		src := &failingSeeker{data: []byte("world"), seekErr: seekErr}
		dst := &partialMoreWriter{partial: 3} // writes 3 bytes, then ErrMore

		n, err := iox.Copy(dst, src)

		// Should return the seek error, not ErrMore
		if !errors.Is(err, seekErr) {
			t.Fatalf("expected seek error, got: %v", err)
		}
		if n != 3 {
			t.Fatalf("expected n=3, got n=%d", n)
		}
	})
}

// =============================================================================
// Test: ErrNoSeeker Propagation in copyBuffer
// =============================================================================

func TestCopy_ErrNoSeeker_ErrWouldBlock(t *testing.T) {
	t.Run("Non-seekable source with partial write and ErrWouldBlock", func(t *testing.T) {
		src := &plainReader{data: []byte("test")}
		dst := &partialWBWriter{partial: 2} // writes 2 bytes, then ErrWouldBlock

		n, err := iox.Copy(dst, src)

		if !errors.Is(err, iox.ErrNoSeeker) {
			t.Fatalf("expected ErrNoSeeker, got: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected n=2, got n=%d", n)
		}
	})
}

func TestCopy_ErrNoSeeker_ErrMore(t *testing.T) {
	t.Run("Non-seekable source with partial write and ErrMore", func(t *testing.T) {
		src := &plainReader{data: []byte("data")}
		dst := &partialMoreWriter{partial: 1} // writes 1 byte, then ErrMore

		n, err := iox.Copy(dst, src)

		if !errors.Is(err, iox.ErrNoSeeker) {
			t.Fatalf("expected ErrNoSeeker, got: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected n=1, got n=%d", n)
		}
	})
}

// =============================================================================
// Test: copyBufferPolicy Seeker Rollback with PolicyReturn
// =============================================================================

func TestCopyPolicy_SeekerRollbackFailure_ErrWouldBlock(t *testing.T) {
	t.Run("Policy returns, Seek fails on partial write with ErrWouldBlock", func(t *testing.T) {
		seekErr := errors.New("seek failed in policy path")
		src := &failingSeeker{data: []byte("abcdef"), seekErr: seekErr}
		dst := &partialWBWriter{partial: 3}

		// Policy that returns (not retries) on ErrWouldBlock
		pol := iox.ReturnPolicy{}

		n, err := iox.CopyPolicy(dst, src, pol)

		if !errors.Is(err, seekErr) {
			t.Fatalf("expected seek error, got: %v", err)
		}
		if n != 3 {
			t.Fatalf("expected n=3, got n=%d", n)
		}
	})
}

func TestCopyPolicy_SeekerRollbackFailure_ErrMore(t *testing.T) {
	t.Run("Policy returns, Seek fails on partial write with ErrMore", func(t *testing.T) {
		seekErr := errors.New("seek failed in policy path")
		src := &failingSeeker{data: []byte("ghijkl"), seekErr: seekErr}
		dst := &partialMoreWriter{partial: 4}

		// Policy that returns (not retries) on ErrMore
		pol := iox.ReturnPolicy{}

		n, err := iox.CopyPolicy(dst, src, pol)

		if !errors.Is(err, seekErr) {
			t.Fatalf("expected seek error, got: %v", err)
		}
		if n != 4 {
			t.Fatalf("expected n=4, got n=%d", n)
		}
	})
}

// =============================================================================
// Test: copyBufferPolicy ErrNoSeeker with PolicyReturn
// =============================================================================

func TestCopyPolicy_ErrNoSeeker_ErrWouldBlock(t *testing.T) {
	t.Run("Policy returns, non-seekable source with partial write and ErrWouldBlock", func(t *testing.T) {
		src := &plainReader{data: []byte("mnop")}
		dst := &partialWBWriter{partial: 2}

		pol := iox.ReturnPolicy{}

		n, err := iox.CopyPolicy(dst, src, pol)

		if !errors.Is(err, iox.ErrNoSeeker) {
			t.Fatalf("expected ErrNoSeeker, got: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected n=2, got n=%d", n)
		}
	})
}

func TestCopyPolicy_ErrNoSeeker_ErrMore(t *testing.T) {
	t.Run("Policy returns, non-seekable source with partial write and ErrMore", func(t *testing.T) {
		src := &plainReader{data: []byte("qrst")}
		dst := &partialMoreWriter{partial: 1}

		pol := iox.ReturnPolicy{}

		n, err := iox.CopyPolicy(dst, src, pol)

		if !errors.Is(err, iox.ErrNoSeeker) {
			t.Fatalf("expected ErrNoSeeker, got: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected n=1, got n=%d", n)
		}
	})
}

// =============================================================================
// Test: copyBufferPolicy Seeker Rollback Success (returns semantic error)
// =============================================================================

// workingSeeker is a ReadSeeker that successfully seeks.
type workingSeeker struct {
	data []byte
	pos  int
}

func (s *workingSeeker) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.pos:])
	s.pos += n
	return n, nil
}

func (s *workingSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		s.pos = int(offset)
	case io.SeekCurrent:
		s.pos += int(offset)
	case io.SeekEnd:
		s.pos = len(s.data) + int(offset)
	}
	if s.pos < 0 {
		s.pos = 0
	}
	if s.pos > len(s.data) {
		s.pos = len(s.data)
	}
	return int64(s.pos), nil
}

func TestCopyPolicy_SeekerRollbackSuccess_ErrWouldBlock(t *testing.T) {
	t.Run("Policy returns, Seek succeeds, returns ErrWouldBlock", func(t *testing.T) {
		src := &workingSeeker{data: []byte("uvwxyz")}
		dst := &partialWBWriter{partial: 2}

		pol := iox.ReturnPolicy{}

		n, err := iox.CopyPolicy(dst, src, pol)

		// Should return ErrWouldBlock after successful rollback
		if !errors.Is(err, iox.ErrWouldBlock) {
			t.Fatalf("expected ErrWouldBlock, got: %v", err)
		}
		if n != 2 {
			t.Fatalf("expected n=2, got n=%d", n)
		}
		// Verify source was rolled back (pos should be at 2, not 6)
		if src.pos != 2 {
			t.Fatalf("expected src.pos=2 after rollback, got %d", src.pos)
		}
	})
}

func TestCopyPolicy_SeekerRollbackSuccess_ErrMore(t *testing.T) {
	t.Run("Policy returns, Seek succeeds, returns ErrMore", func(t *testing.T) {
		src := &workingSeeker{data: []byte("123456")}
		dst := &partialMoreWriter{partial: 3}

		pol := iox.ReturnPolicy{}

		n, err := iox.CopyPolicy(dst, src, pol)

		// Should return ErrMore after successful rollback
		if !errors.Is(err, iox.ErrMore) {
			t.Fatalf("expected ErrMore, got: %v", err)
		}
		if n != 3 {
			t.Fatalf("expected n=3, got n=%d", n)
		}
		// Verify source was rolled back (pos should be at 3, not 6)
		if src.pos != 3 {
			t.Fatalf("expected src.pos=3 after rollback, got %d", src.pos)
		}
	})
}
