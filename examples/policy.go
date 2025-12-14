// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package examples

import (
	"bytes"
	"runtime"

	"code.hybscloud.com/iox"
)

// CopyWithPolicy shows how to specify a SemanticPolicy for Copy.
//
// In most cases you don't pass a policy (nil keeps the default non-blocking
// behavior), but if you want the helper to yield/retry on ErrWouldBlock,
// pass a policy that returns PolicyRetry and provides a Yield hook.
func CopyWithPolicy() (n int64, err error, out string) {
	src := &wouldBlockReader{}
	var dst bytes.Buffer

	policy := iox.PolicyFunc{
		WouldBlockFunc: func(op iox.Op) iox.PolicyAction {
			return iox.PolicyRetry
		},
		YieldFunc: func(op iox.Op) {
			// In real code this could be "poll one tick", "wait for readiness",
			// or any backpressure strategy. For a tiny example, just yield.
			runtime.Gosched()
		},
	}

	n, err = iox.CopyPolicy(&dst, src, policy)
	return n, err, dst.String()
}
