// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"bytes"
	"testing"

	"code.hybscloud.com/iox"
)

func BenchmarkTeeReader(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			srcData := bytes.Repeat([]byte{'x'}, size)
			buf := make([]byte, 32*1024)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				r := bytes.NewReader(srcData)
				tr := iox.TeeReader(r, devNull{})
				for {
					n, err := tr.Read(buf)
					if n == 0 || err != nil {
						break
					}
				}
			}
		})
	}
}

func BenchmarkTeeWriter(b *testing.B) {
	sizes := []int{1 << 10, 32 << 10, 1 << 20}
	for _, size := range sizes {
		b.Run(byteSize(size), func(b *testing.B) {
			data := bytes.Repeat([]byte{'x'}, size)
			tw := iox.TeeWriter(devNull{}, devNull{})
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := tw.Write(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
