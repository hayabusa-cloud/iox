// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package examples

import (
	"bytes"

	"code.hybscloud.com/iox"
)

// The script queues one readiness notification for the retry that reads "ok".
func wouldBlockSource() (*completions, <-chan struct{}) {
	ready := make(chan struct{}, 1)
	ready <- struct{}{}
	return &completions{{err: iox.ErrWouldBlock}, {data: "ok"}}, ready
}

// CopyWithWouldBlock receives a readiness notification after the first copy
// returns (0, [iox.ErrWouldBlock]). The second copy transfers "ok".
func CopyWithWouldBlock() (firstN int64, firstErr error, secondN int64, secondErr error, out string) {
	src, ready := wouldBlockSource()
	var dst bytes.Buffer

	firstN, firstErr = iox.Copy(&dst, src)
	<-ready
	secondN, secondErr = iox.Copy(&dst, src)

	return firstN, firstErr, secondN, secondErr, dst.String()
}
