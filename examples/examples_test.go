// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package examples_test

import (
	"errors"
	"io"
	"testing"

	"code.hybscloud.com/iox"
	ex "code.hybscloud.com/iox/examples"
)

func TestCompletionShortReads(t *testing.T) {
	check := func(r io.Reader, size int, want string, wantErr error) {
		t.Helper()
		p := make([]byte, size)
		n, err := r.Read(p)
		if n != len(want) || err != wantErr {
			t.Fatalf("Read(%d) = (%d, %v), want (%d, %v)", size, n, err, len(want), wantErr)
		}
		if got := string(p[:n]); got != want {
			t.Fatalf("read %q, want %q", got, want)
		}
	}
	for _, signal := range []error{iox.ErrMore, iox.ErrWouldBlock} {
		r := ex.NewCompletion("abc", signal)
		check(r, 0, "", nil)
		check(r, 1, "a", nil)
		check(r, 0, "", nil)
		check(r, 4, "bc", signal)
		check(r, 1, "", io.EOF)

		empty := ex.NewCompletion("", signal)
		check(empty, 0, "", nil)
		check(empty, 1, "", signal)
		check(empty, 1, "", io.EOF)
	}
}

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

func TestCopyWithPolicy(t *testing.T) {
	n, err, out := ex.CopyWithPolicy()

	// The policy retries on ErrWouldBlock, so the example should complete in a single call.
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}
	if n != 2 {
		t.Fatalf("want n=2 got %d", n)
	}
	if out != "ok" {
		t.Fatalf("want out=ok got %q", out)
	}
}
