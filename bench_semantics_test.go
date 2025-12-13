// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"errors"
	"testing"

	"code.hybscloud.com/iox"
)

func BenchmarkClassify_nil(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = iox.Classify(nil)
	}
}

func BenchmarkClassify_WouldBlock(b *testing.B) {
	b.ReportAllocs()
	err := iox.ErrWouldBlock
	for i := 0; i < b.N; i++ {
		_ = iox.Classify(err)
	}
}

func BenchmarkClassify_More(b *testing.B) {
	b.ReportAllocs()
	err := iox.ErrMore
	for i := 0; i < b.N; i++ {
		_ = iox.Classify(err)
	}
}

func BenchmarkClassify_Wrapped(b *testing.B) {
	b.ReportAllocs()
	err := errors.Join(iox.ErrMore)
	for i := 0; i < b.N; i++ {
		_ = iox.Classify(err)
	}
}
