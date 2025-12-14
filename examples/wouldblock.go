// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package examples

import (
	"bytes"

	"code.hybscloud.com/iox"
)

// wouldBlockReader simulates a non-blocking source that is not ready on the
// first attempt, then becomes ready on a subsequent poll.
type wouldBlockReader struct{ step int }

func (r *wouldBlockReader) Read(p []byte) (int, error) {
	switch r.step {
	case 0:
		r.step++
		// No progress is possible now without waiting.
		return 0, iox.ErrWouldBlock
	case 1:
		r.step++
		copy(p, []byte("ok"))
		return 2, nil
	default:
		return 0, iox.EOF
	}
}

// CopyWithWouldBlock demonstrates that ErrWouldBlock means "stop now and retry
// after readiness". The first copy returns (0, ErrWouldBlock); the second copy
// makes progress and completes.
func CopyWithWouldBlock() (firstN int64, firstErr error, secondN int64, secondErr error, out string) {
	src := &wouldBlockReader{}
	var dst bytes.Buffer

	firstN, firstErr = iox.Copy(&dst, src)
	secondN, secondErr = iox.Copy(&dst, src)

	return firstN, firstErr, secondN, secondErr, dst.String()
}
