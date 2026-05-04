// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

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
// These semantics propagate through Copy/CopyN and Tee helpers. When a
// WriterTo/ReaderFrom fast path is selected, that fast-path implementation owns
// its own source advancement and partial-write recovery; it must preserve
// ErrWouldBlock/ErrMore instead of converting them to nil or EOF. Use the
// iox.Copy family instead of io.Copy when you need to preserve these semantics.
//
// Note: Copy treats a (0, nil) read as "stop copying now" and returns (written, nil)
// to avoid hidden spinning inside a helper in event-loop code.
package iox
