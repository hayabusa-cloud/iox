// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"io"
	"testing"

	"code.hybscloud.com/iox"
)

func TestCopyBufferReusesStorage(t *testing.T) {
	for _, variant := range []struct {
		name string
		copy func(io.Writer, io.Reader, []byte) (int64, error)
	}{
		{"CopyBuffer", iox.CopyBuffer},
		{"NilPolicy", func(w io.Writer, r io.Reader, buf []byte) (int64, error) {
			return iox.CopyBufferPolicy(w, r, buf, nil)
		}},
		{"ReturnPolicy", func(w io.Writer, r io.Reader, buf []byte) (int64, error) {
			return iox.CopyBufferPolicy(w, r, buf, iox.ReturnPolicy{})
		}},
	} {
		for _, control := range []error{io.EOF, iox.ErrWouldBlock, iox.ErrMore} {
			t.Run(variant.name+"/"+control.Error(), func(t *testing.T) {
				src := &dataThenErrReader2{data: []byte("payload"), err: control}
				buf := make([]byte, 16)
				wantErr := control
				if control == io.EOF {
					wantErr = nil
				}
				allocs := testing.AllocsPerRun(100, func() {
					src.done = false
					n, err := variant.copy(devNull{}, src, buf)
					if n != int64(len(src.data)) || err != wantErr {
						t.Fatalf("copy = (%d, %v), want (%d, %v)", n, err, len(src.data), wantErr)
					}
				})
				if allocs != 0 {
					t.Fatalf("copy with supplied storage allocated %g times", allocs)
				}
			})
		}
	}
}
