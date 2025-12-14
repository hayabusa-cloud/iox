// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"errors"
	"fmt"
	"testing"

	"code.hybscloud.com/iox"
)

func TestOutcomeStringAndClassify(t *testing.T) {
	if s := iox.OutcomeOK.String(); s != "OK" {
		t.Fatalf("OutcomeOK=%q", s)
	}
	if s := iox.OutcomeWouldBlock.String(); s != "WouldBlock" {
		t.Fatalf("OutcomeWouldBlock=%q", s)
	}
	if s := iox.OutcomeMore.String(); s != "More" {
		t.Fatalf("OutcomeMore=%q", s)
	}
	if s := (iox.Outcome(255)).String(); s != "Failure" {
		t.Fatalf("Outcome(255)=%q", s)
	}

	if got := iox.Classify(nil); got != iox.OutcomeOK {
		t.Fatalf("Classify(nil)=%v", got)
	}
	if got := iox.Classify(fmt.Errorf("wrap: %w", iox.ErrWouldBlock)); got != iox.OutcomeWouldBlock {
		t.Fatalf("Classify(ErrWouldBlock)=%v", got)
	}
	if got := iox.Classify(fmt.Errorf("wrap: %w", iox.ErrMore)); got != iox.OutcomeMore {
		t.Fatalf("Classify(ErrMore)=%v", got)
	}
	if got := iox.Classify(errors.New("x")); got != iox.OutcomeFailure {
		t.Fatalf("Classify(other)=%v", got)
	}
}

func TestSemanticHelpers(t *testing.T) {
	if !iox.IsWouldBlock(fmt.Errorf("wb: %w", iox.ErrWouldBlock)) {
		t.Fatal("IsWouldBlock false")
	}
	if !iox.IsMore(fmt.Errorf("more: %w", iox.ErrMore)) {
		t.Fatal("IsMore false")
	}
	if !iox.IsSemantic(iox.ErrMore) || !iox.IsSemantic(iox.ErrWouldBlock) {
		t.Fatal("IsSemantic false")
	}
	if !iox.IsNonFailure(nil) || !iox.IsNonFailure(iox.ErrWouldBlock) || !iox.IsNonFailure(iox.ErrMore) {
		t.Fatal("IsNonFailure false for non-failures")
	}
	if iox.IsNonFailure(errors.New("x")) {
		t.Fatal("IsNonFailure true for other error")
	}
	if !iox.IsProgress(nil) || !iox.IsProgress(iox.ErrMore) {
		t.Fatal("IsProgress false for progress cases")
	}
	if iox.IsProgress(iox.ErrWouldBlock) {
		t.Fatal("IsProgress true for would-block")
	}
}

func TestOpStringAndPolicies(t *testing.T) {
	// (Policy tests removed; see policy_extra_test.go for coverage)
}
