// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

// Package iox provides non‑blocking and multi-shot I/O helpers that extend
// Go's standard io semantics while remaining fully compatible with its
// interfaces and fast paths (WriterTo/ReaderFrom).
//
// Extended result semantics
//   - ErrWouldBlock: the operation cannot make progress now without waiting.
//     Return immediately; retry later.
//   - ErrMore: the current completion made progress and more completions will
//     follow (multi‑shot style). Process now, keep polling for more.
//
// These semantics propagate through Copy/CopyN and Tee helpers, including when
// WriterTo/ReaderFrom fast paths are used. Use the iox.Copy family instead of
// io.Copy when you need to preserve these semantics.
//
// Note: Copy treats a (0, nil) read as “stop copying now” and returns (written, nil)
// to avoid hidden spinning inside a helper in event-loop code.
