// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
    "bytes"
    "errors"
    "testing"

    "code.hybscloud.com/iox"
)

// This test documents the intended contract: on ErrMore, callers should
// return to their loop and retry later; subsequent calls continue progress.
func TestCopy_ReturnOnErrMore_AcrossCalls(t *testing.T) {
    s := &scriptedReader{steps: []struct {
        b   []byte
        err error
    }{
        {b: []byte("ab"), err: nil},          // deliver first chunk
        {b: nil, err: iox.ErrMore},            // signal multi-shot: more will follow
        {b: []byte("cd"), err: nil},          // next chunk on subsequent attempt
        {b: nil, err: iox.EOF},                // final completion
    }}

    var dst bytes.Buffer

    // First attempt should make progress and return ErrMore.
    n1, err := iox.Copy(&dst, s)
    if !errors.Is(err, iox.ErrMore) {
        t.Fatalf("want ErrMore got %v", err)
    }
    if n1 != 2 || dst.String() != "ab" {
        t.Fatalf("first: n=%d dst=%q", n1, dst.String())
    }

    // Next attempt should continue and complete without error.
    n2, err := iox.Copy(&dst, s)
    if err != nil {
        t.Fatalf("unexpected err on second call: %v", err)
    }
    if n2 != 2 || dst.String() != "abcd" {
        t.Fatalf("second: n=%d dst=%q", n2, dst.String())
    }
}
