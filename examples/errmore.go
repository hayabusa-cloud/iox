// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Package examples demonstrates how copies report data, readiness, and
// multi-shot completions.
package examples

import (
	"io"
	"strings"

	"code.hybscloud.com/iox"
)

// Each completion delivers its error with its last byte. An empty
// completion can deliver a signal on its own.
type completions []struct {
	data string
	err  error
}

func (r *completions) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(*r) == 0 {
		return 0, io.EOF
	}
	c := &(*r)[0]
	n := copy(p, c.data)
	if n < len(c.data) {
		c.data = c.data[n:]
		return n, nil
	}
	*r = (*r)[1:]
	return n, c.err
}

// CopyWithErrMore reads three queued chunks through a one-byte copy buffer.
// The first two copies each return two bytes and [iox.ErrMore]. The third
// reaches the end of the input.
func CopyWithErrMore() (
	firstN int64, firstErr error,
	secondN int64, secondErr error,
	thirdN int64, thirdErr error,
	out string,
) {
	src := &completions{
		{data: "ab", err: iox.ErrMore},
		{data: "cd", err: iox.ErrMore},
		{data: "ef"},
	}
	var dst strings.Builder
	var buf [1]byte

	firstN, firstErr = iox.CopyBuffer(&dst, src, buf[:])
	secondN, secondErr = iox.CopyBuffer(&dst, src, buf[:])
	thirdN, thirdErr = iox.CopyBuffer(&dst, src, buf[:])

	return firstN, firstErr, secondN, secondErr, thirdN, thirdErr, dst.String()
}
