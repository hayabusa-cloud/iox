// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Package examples contains small, runnable snippets that demonstrate
// how to use iox's non-blocking semantics in typical copy-style flows.
package examples

import (
	"bytes"

	"code.hybscloud.com/iox"
)

// multiShotReader simulates a source that produces multiple completions
// for a single logical operation. It returns ErrMore to signal that the
// operation remains active and more data will follow on subsequent polls.
type multiShotReader struct{ step int }

func (r *multiShotReader) Read(p []byte) (int, error) {
	switch r.step {
	case 0:
		r.step++
		// First completion: we made progress (wrote 2 bytes) and
		// explicitly signal multi-shot with ErrMore. Caller must keep
		// the operation active and retry after the next poll.
		copy(p, []byte("ab"))
		return 2, iox.ErrMore
	case 1:
		r.step++
		// Second completion: again progress + ErrMore. This indicates the
		// logical operation is still ongoing; more completions will follow.
		copy(p, []byte("cd"))
		return 2, iox.ErrMore
	case 2:
		r.step++
		// Final completion for this logical operation. We return progress
		// and nil to indicate successful completion.
		copy(p, []byte("ef"))
		return 2, nil
	default:
		return 0, iox.EOF
	}
}

// CopyWithErrMore demonstrates handling of ErrMore across repeated calls.
//
// Contract for this scripted source:
//   - First attempt: progress + ErrMore (more completions will follow).
//   - Second attempt: progress + ErrMore (still ongoing).
//   - Third attempt: progress + nil (final completion of the logical op).
//
// Caller must treat ErrMore as: "process this completion and keep the
// operation active; retry after the next poll/CQE".
func CopyWithErrMore() (
	firstN int64, firstErr error,
	secondN int64, secondErr error,
	thirdN int64, thirdErr error,
	out string,
) {
	src := &multiShotReader{}
	var dst bytes.Buffer

	// First call: expect progress + ErrMore.
	firstN, firstErr = iox.Copy(&dst, src)

	// Second call: continue and still expect ErrMore.
	secondN, secondErr = iox.Copy(&dst, src)

	// Third call: continue and expect completion with nil error.
	thirdN, thirdErr = iox.Copy(&dst, src)

	return firstN, firstErr, secondN, secondErr, thirdN, thirdErr, dst.String()
}
