// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package examples_test

import (
	"errors"
	"testing"

	"code.hybscloud.com/iox"
	ex "code.hybscloud.com/iox/examples"
)

func TestCopyWithErrMore(t *testing.T) {
	firstN, firstErr, secondN, secondErr, thirdN, thirdErr, out := ex.CopyWithErrMore()

	if !errors.Is(firstErr, iox.ErrMore) {
		t.Fatalf("first call: want ErrMore got %v", firstErr)
	}
	if firstN != 2 {
		t.Fatalf("first call: want n=2 got %d", firstN)
	}
	if !errors.Is(secondErr, iox.ErrMore) {
		t.Fatalf("second call: want ErrMore got %v", secondErr)
	}
	if secondN != 2 {
		t.Fatalf("second call: want n=2 got %d", secondN)
	}
	if thirdErr != nil {
		t.Fatalf("third call: unexpected err %v", thirdErr)
	}
	if thirdN != 2 {
		t.Fatalf("third call: want n=2 got %d", thirdN)
	}
	if out != "abcdef" {
		t.Fatalf("want out=abcdef got %q", out)
	}
}

func TestCopyWithWouldBlock(t *testing.T) {
	firstN, firstErr, secondN, secondErr, out := ex.CopyWithWouldBlock()

	if !errors.Is(firstErr, iox.ErrWouldBlock) {
		t.Fatalf("first call: want ErrWouldBlock got %v", firstErr)
	}
	if firstN != 0 {
		t.Fatalf("first call: want n=0 got %d", firstN)
	}
	if secondErr != nil {
		t.Fatalf("second call: unexpected err %v", secondErr)
	}
	if secondN != 2 {
		t.Fatalf("second call: want n=2 got %d", secondN)
	}
	if out != "ok" {
		t.Fatalf("want out=ok got %q", out)
	}
}
