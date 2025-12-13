// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import "errors"

// iox introduces two semantic errors for non-blocking and multi-shot I/O.
//
// Mental model:
//   - ErrWouldBlock: retry later (wait for readiness/event, then try again).
//   - ErrMore: keep polling (operation remains active; more completions will follow).
//
// Notes:
//   - ErrWouldBlock and ErrMore are expected control flow; treat them like
//     readiness/completion states, not failures.
//   - Either may help partial progress: counts first, semantics second.

// ErrWouldBlock means “no further progress without waiting”.
// Linux analogy: EAGAIN/EWOULDBLOCK / not-ready / no completion available.
// Next step: wait (via poll/epoll/io_uring/etc), then retry.
var ErrWouldBlock = errors.New("io: would block")

// ErrMore means “this operation remains active; more completions will follow”
// (multi-shot / streaming style).
// Next step: keep polling and processing results.
var ErrMore = errors.New("io: expect more")
