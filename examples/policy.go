// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package examples

import (
	"bytes"

	"code.hybscloud.com/iox"
)

// CopyWithPolicy uses [iox.YieldPolicy] to wait for readiness and retry
// [iox.ErrWouldBlock] within a single copy.
func CopyWithPolicy() (n int64, err error, out string) {
	src, ready := wouldBlockSource()
	var dst bytes.Buffer

	policy := iox.YieldPolicy{
		YieldFunc: func(iox.Op) { <-ready },
	}

	n, err = iox.CopyPolicy(&dst, src, policy)
	return n, err, dst.String()
}
