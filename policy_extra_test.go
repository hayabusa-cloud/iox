// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"testing"

	"code.hybscloud.com/iox"
)

func TestOpString_AllValuesAndUnknown(t *testing.T) {
	cases := []struct {
		op   iox.Op
		want string
	}{
		{iox.OpCopyRead, "CopyRead"},
		{iox.OpCopyWrite, "CopyWrite"},
		{iox.OpCopyWriterTo, "CopyWriterTo"},
		{iox.OpCopyReaderFrom, "CopyReaderFrom"},
		{iox.OpTeeReaderRead, "TeeReaderRead"},
		{iox.OpTeeReaderSideWrite, "TeeReaderSideWrite"},
		{iox.OpTeeWriterPrimaryWrite, "TeeWriterPrimaryWrite"},
		{iox.OpTeeWriterTeeWrite, "TeeWriterTeeWrite"},
		{iox.Op(255), "Op(unknown)"}, // default branch
	}
	for _, tc := range cases {
		if got := tc.op.String(); got != tc.want {
			t.Fatalf("op=%v got=%q want=%q", tc.op, got, tc.want)
		}
	}
}

func TestPolicies_Return_Yield_YieldOnWriteWouldBlock(t *testing.T) {
	// ReturnPolicy always returns PolicyReturn.
	var rp iox.ReturnPolicy
	if rp.OnWouldBlock(iox.OpCopyRead) != iox.PolicyReturn || rp.OnMore(iox.OpCopyRead) != iox.PolicyReturn {
		t.Fatalf("ReturnPolicy must always return PolicyReturn")
	}
	// YieldPolicy retries only on would-block.
	var yp iox.YieldPolicy
	if yp.OnWouldBlock(iox.OpCopyRead) != iox.PolicyRetry {
		t.Fatalf("YieldPolicy OnWouldBlock should retry")
	}
	if yp.OnMore(iox.OpCopyRead) != iox.PolicyReturn {
		t.Fatalf("YieldPolicy OnMore should return")
	}

	// YieldOnWriteWouldBlockPolicy: retry on writer-side ops only.
	var ywp iox.YieldOnWriteWouldBlockPolicy
	writeOps := []iox.Op{
		iox.OpCopyWrite, iox.OpCopyWriterTo, iox.OpCopyReaderFrom,
		iox.OpTeeReaderSideWrite, iox.OpTeeWriterPrimaryWrite, iox.OpTeeWriterTeeWrite,
	}
	for _, op := range writeOps {
		if ywp.OnWouldBlock(op) != iox.PolicyRetry {
			t.Fatalf("YieldOnWriteWouldBlockPolicy OnWouldBlock(%v) should retry", op)
		}
	}
	if ywp.OnWouldBlock(iox.OpCopyRead) != iox.PolicyReturn || ywp.OnMore(iox.OpCopyRead) != iox.PolicyReturn {
		t.Fatalf("YieldOnWriteWouldBlockPolicy non-write ops should return")
	}
}

func TestPolicyFunc_DefaultsAndOverrides(t *testing.T) {
	// Defaults: return PolicyReturn; Yield should be callable (covers default path).
	pf := iox.PolicyFunc{}
	_ = pf.OnWouldBlock(iox.OpCopyRead)
	_ = pf.OnMore(iox.OpCopyRead)
	pf.Yield(iox.OpCopyRead)

	// Overrides: ensure our funcs are invoked.
	var gotYield, gotWB, gotMore bool
	pf2 := iox.PolicyFunc{
		YieldFunc: func(op iox.Op) { gotYield = true },
		WouldBlockFunc: func(op iox.Op) iox.PolicyAction {
			gotWB = true
			return iox.PolicyRetry
		},
		MoreFunc: func(op iox.Op) iox.PolicyAction {
			gotMore = true
			return iox.PolicyReturn
		},
	}
	_ = pf2.OnWouldBlock(iox.OpCopyWrite)
	_ = pf2.OnMore(iox.OpCopyWrite)
	pf2.Yield(iox.OpCopyWrite)
	if !gotYield || !gotWB || !gotMore {
		t.Fatalf("PolicyFunc overrides not invoked: yield=%v wb=%v more=%v", gotYield, gotWB, gotMore)
	}
}
