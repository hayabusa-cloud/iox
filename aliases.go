// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Package iox provides non-blocking I/O helpers that extend Go's standard io
// semantics with explicit non-failure control-flow errors (see ErrWouldBlock,
// ErrMore) while remaining compatible with standard library interfaces.
//
// IDE note: iox re-exports (aliases) the core io interfaces so that users can
// stay in the "iox" namespace while reading documentation and navigating types.
// The contracts below mirror the standard io expectations, with iox-specific
// behavior documented where relevant (typically at call sites such as Copy).
package iox

import (
	"io"
)

// Reader is implemented by types that can read bytes into p.
//
// Read must return the number of bytes read (0 <= n <= len(p)) and any error
// encountered. Even if Read returns n > 0, it may return a non-nil error to
// signal a condition observed after producing those bytes.
//
// Callers should treat a return of (0, nil) as "no progress": it does not mean
// end-of-stream. Well-behaved implementations should avoid returning (0, nil)
// except when len(p) == 0.
//
// Reader is an alias of io.Reader.
type Reader = io.Reader

// Writer is implemented by types that can write bytes from p.
//
// Write must return the number of bytes written (0 <= n <= len(p)) and any
// error encountered. If Write returns n < len(p), it must return a non-nil error
// (except in the special case of len(p) == 0).
//
// Writer is an alias of io.Writer.
type Writer = io.Writer

// Closer is implemented by types that can release resources.
//
// Close should be idempotent where practical; callers should not assume any
// particular behavior beyond resource release and an error indicating failure.
//
// Closer is an alias of io.Closer.
type Closer = io.Closer

// Seeker is implemented by types that can set the offset for the next Read or
// Write.
//
// Seek sets the offset based on whence and returns the new absolute offset.
//
// Seeker is an alias of io.Seeker.
type Seeker = io.Seeker

// ReaderFrom is an optional optimization for Writers.
//
// If implemented by a Writer, Copy-like helpers may call ReadFrom to transfer
// data from r more efficiently than a generic read/write loop.
//
// ReaderFrom is an alias of io.ReaderFrom.
type ReaderFrom = io.ReaderFrom

// WriterTo is an optional optimization for Readers.
//
// If implemented by a Reader, Copy-like helpers may call WriteTo to transfer
// data to w more efficiently than a generic read/write loop.
//
// WriterTo is an alias of io.WriterTo.
type WriterTo = io.WriterTo

// ReaderAt reads from the underlying input at a given offset.
//
// ReaderAt should not affect and should not be affected by the current seek
// offset. Implementations must return a non-nil error when n < len(p).
//
// ReaderAt is an alias of io.ReaderAt.
type ReaderAt = io.ReaderAt

// WriterAt writes to the underlying output at a given offset.
//
// WriterAt should not affect and should not be affected by the current seek
// offset. Implementations must return a non-nil error when n < len(p).
//
// WriterAt is an alias of io.WriterAt.
type WriterAt = io.WriterAt

// ReadWriter groups the basic Read and Write methods.
//
// ReadWriter is an alias of io.ReadWriter.
type ReadWriter = io.ReadWriter

// ReadCloser groups Read and Close.
//
// ReadCloser is an alias of io.ReadCloser.
type ReadCloser = io.ReadCloser

// WriteCloser groups Write and Close.
//
// WriteCloser is an alias of io.WriteCloser.
type WriteCloser = io.WriteCloser

// ReadWriteCloser groups Read, Write, and Close.
//
// ReadWriteCloser is an alias of io.ReadWriteCloser.
type ReadWriteCloser = io.ReadWriteCloser

// ReadSeeker groups Read and Seek.
//
// ReadSeeker is an alias of io.ReadSeeker.
type ReadSeeker = io.ReadSeeker

// WriteSeeker groups Write and Seek.
//
// WriteSeeker is an alias of io.WriteSeeker.
type WriteSeeker = io.WriteSeeker

// ReadWriteSeeker groups Read, Write, and Seek.
//
// ReadWriteSeeker is an alias of io.ReadWriteSeeker.
type ReadWriteSeeker = io.ReadWriteSeeker

// ByteReader reads and returns a single byte.
//
// ByteReader is an alias of io.ByteReader.
type ByteReader = io.ByteReader

// ByteScanner is a ByteReader that can "unread" the last byte read.
//
// ByteScanner is an alias of io.ByteScanner.
type ByteScanner = io.ByteScanner

// ByteWriter writes a single byte.
//
// ByteWriter is an alias of io.ByteWriter.
type ByteWriter = io.ByteWriter

// RuneReader reads and returns a single UTF-8 encoded rune.
//
// RuneReader is an alias of io.RuneReader.
type RuneReader = io.RuneReader

// RuneScanner is a RuneReader that can "unread" the last rune read.
//
// RuneScanner is an alias of io.RuneScanner.
type RuneScanner = io.RuneScanner

// StringWriter writes the contents of s more efficiently than Write([]byte(s))
// for implementations that can avoid an allocation/copy.
//
// StringWriter is an alias of io.StringWriter.
type StringWriter = io.StringWriter

// Common sentinel errors re-exported for convenience.
//
// Note: iox also defines semantic non-failure errors (ErrWouldBlock, ErrMore)
// used by iox helpers and adapters; those are not part of the standard io set.
var (
	// EOF is returned by Read when no more input is available.
	// Functions should return EOF only to signal a graceful end of input.
	EOF = io.EOF

	// ErrClosedPipe is returned on write to a closed pipe.
	// It may also be returned by other operations that behave like a closed pipe.
	ErrClosedPipe = io.ErrClosedPipe

	// ErrNoProgress reports that a Reader returned no data and no error after
	// multiple Read calls. It is used by some io helpers to detect broken Readers
	// (i.e., lack of forward progress).
	ErrNoProgress = io.ErrNoProgress

	// ErrShortBuffer means a provided buffer was too small to complete the operation.
	// Callers typically retry with a larger buffer.
	ErrShortBuffer = io.ErrShortBuffer

	// ErrShortWrite means a write accepted fewer bytes than requested and returned
	// no explicit error (or equivalently, could not complete the full write).
	ErrShortWrite = io.ErrShortWrite

	// ErrUnexpectedEOF means EOF was encountered earlier than expected.
	// It is commonly used by fixed-size reads/copies when the stream ends mid-record.
	ErrUnexpectedEOF = io.ErrUnexpectedEOF
)
