// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"testing"

	"code.hybscloud.com/iox"
)

func BenchmarkPolicy_ReturnPolicy_OnWouldBlock(b *testing.B) {
	var p iox.ReturnPolicy
	var sink iox.PolicyAction
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = p.OnWouldBlock(iox.OpCopyRead)
	}
	_ = sink
}

func BenchmarkPolicy_ReturnPolicy_OnMore(b *testing.B) {
	var p iox.ReturnPolicy
	var sink iox.PolicyAction
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = p.OnMore(iox.OpCopyWrite)
	}
	_ = sink
}

func BenchmarkPolicy_YieldPolicy_OnWouldBlock(b *testing.B) {
	var p iox.YieldPolicy
	var sink iox.PolicyAction
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = p.OnWouldBlock(iox.OpCopyRead)
	}
	_ = sink
}

func BenchmarkPolicy_YieldPolicy_OnMore(b *testing.B) {
	var p iox.YieldPolicy
	var sink iox.PolicyAction
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = p.OnMore(iox.OpCopyRead)
	}
	_ = sink
}

func BenchmarkPolicy_YieldOnWriteWouldBlock_OnWouldBlock(b *testing.B) {
	var p iox.YieldOnWriteWouldBlockPolicy
	var sink iox.PolicyAction
	ops := []iox.Op{iox.OpCopyWrite, iox.OpTeeWriterPrimaryWrite, iox.OpTeeWriterTeeWrite}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = p.OnWouldBlock(ops[i%len(ops)])
	}
	_ = sink
}

func BenchmarkPolicyFunc_Defaults(b *testing.B) {
	pf := iox.PolicyFunc{}
	var sink iox.PolicyAction
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pf.Yield(iox.OpCopyRead)
		sink = pf.OnWouldBlock(iox.OpCopyRead)
		sink = pf.OnMore(iox.OpCopyRead)
	}
	_ = sink
}

func BenchmarkOpString_All(b *testing.B) {
	ops := []iox.Op{
		iox.OpCopyRead, iox.OpCopyWrite, iox.OpCopyWriterTo, iox.OpCopyReaderFrom,
		iox.OpTeeReaderRead, iox.OpTeeReaderSideWrite, iox.OpTeeWriterPrimaryWrite, iox.OpTeeWriterTeeWrite,
	}
	var s string
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s = ops[i%len(ops)].String()
	}
	_ = s
}
