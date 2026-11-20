// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import "errors"

// ErrWouldBlock signals that further progress requires readiness.
// Process any bytes already transferred before waiting to retry.
var ErrWouldBlock = errors.New("io: would block")

// ErrMore signals that the operation remains active. Process any bytes
// already transferred and continue observing the operation.
var ErrMore = errors.New("io: expect more")

// ErrNoSeeker means the generic copy loop could not rewind a non-seekable
// source after a partial write with a semantic error. The source has already
// advanced past the unwritten bytes. To keep the suffix on a non-seekable
// source, use [CopyPolicy] from the start and select [PolicyRetry] when the
// write returns an exact [ErrWouldBlock] or [ErrMore].
var ErrNoSeeker = errors.New("io: source is not seekable; partial write unrecoverable")
