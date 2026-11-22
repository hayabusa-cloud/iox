// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package examples

import "io"

// NewCompletion exposes the example source to black-box tests.
func NewCompletion(data string, signal error) io.Reader {
	return &completions{{data: data, err: signal}}
}
