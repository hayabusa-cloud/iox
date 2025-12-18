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

// -----------------------------------------------------------------------------
// Outcome and Classify tests
// -----------------------------------------------------------------------------

func TestSemantics_ClassifyAndPredicates(t *testing.T) {
	sentinelErr := errors.New("sentinelErr")
	cases := []struct {
		name            string
		err             error
		wantWB          bool
		wantMore        bool
		wantSemantic    bool
		wantNonFailure  bool
		wantProgress    bool
		wantOutcome     iox.Outcome
		wantOutcomeText string
	}{
		{"nil", nil, false, false, false, true, true, iox.OutcomeOK, "OK"},
		{"wouldblock", iox.ErrWouldBlock, true, false, true, true, false, iox.OutcomeWouldBlock, "WouldBlock"},
		{"more", iox.ErrMore, false, true, true, true, true, iox.OutcomeMore, "More"},
		{"sentinelErr", sentinelErr, false, false, false, false, false, iox.OutcomeFailure, "Failure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := iox.IsWouldBlock(tc.err); got != tc.wantWB {
				t.Fatalf("IsWouldBlock=%v", got)
			}
			if got := iox.IsMore(tc.err); got != tc.wantMore {
				t.Fatalf("IsMore=%v", got)
			}
			if got := iox.IsSemantic(tc.err); got != tc.wantSemantic {
				t.Fatalf("IsSemantic=%v", got)
			}
			if got := iox.IsNonFailure(tc.err); got != tc.wantNonFailure {
				t.Fatalf("IsNonFailure=%v", got)
			}
			if got := iox.IsProgress(tc.err); got != tc.wantProgress {
				t.Fatalf("IsProgress=%v", got)
			}
			if got := iox.Classify(tc.err); got != tc.wantOutcome {
				t.Fatalf("Classify=%v", got)
			}
			if s := iox.Classify(tc.err).String(); s != tc.wantOutcomeText {
				t.Fatalf("Outcome.String()=%q", s)
			}
		})
	}
}

func TestSemantics_WrappedErrors(t *testing.T) {
	t.Run("WrappedWouldBlock", func(t *testing.T) {
		wb := fmt.Errorf("wrap: %w", iox.ErrWouldBlock)
		if !iox.IsWouldBlock(wb) || !iox.IsSemantic(wb) || iox.IsMore(wb) {
			t.Fatalf("wrapped would-block not detected properly")
		}
		if iox.Classify(wb) != iox.OutcomeWouldBlock {
			t.Fatalf("classify wrapped wouldblock")
		}
	})

	t.Run("DoubleWrappedMore", func(t *testing.T) {
		more := fmt.Errorf("wrap1: %w", fmt.Errorf("wrap2: %w", iox.ErrMore))
		if !iox.IsMore(more) || !iox.IsSemantic(more) || iox.IsWouldBlock(more) {
			t.Fatalf("wrapped more not detected properly")
		}
		if !iox.IsNonFailure(more) || !iox.IsProgress(more) {
			t.Fatalf("non-failure/progress for wrapped more")
		}
		if iox.Classify(more) != iox.OutcomeMore {
			t.Fatalf("classify wrapped more")
		}
	})
}

func TestOutcomeString_DefaultFailureBranch(t *testing.T) {
	if got := iox.Outcome(255).String(); got != "Failure" {
		t.Fatalf("Outcome.String() default = %q", got)
	}
}

// -----------------------------------------------------------------------------
// Semantic helper function tests
// -----------------------------------------------------------------------------

func TestSemanticHelpers(t *testing.T) {
	t.Run("IsWouldBlock_Wrapped", func(t *testing.T) {
		if !iox.IsWouldBlock(fmt.Errorf("wb: %w", iox.ErrWouldBlock)) {
			t.Fatal("IsWouldBlock false")
		}
	})

	t.Run("IsMore_Wrapped", func(t *testing.T) {
		if !iox.IsMore(fmt.Errorf("more: %w", iox.ErrMore)) {
			t.Fatal("IsMore false")
		}
	})

	t.Run("IsSemantic", func(t *testing.T) {
		if !iox.IsSemantic(iox.ErrMore) || !iox.IsSemantic(iox.ErrWouldBlock) {
			t.Fatal("IsSemantic false")
		}
	})

	t.Run("IsNonFailure", func(t *testing.T) {
		if !iox.IsNonFailure(nil) || !iox.IsNonFailure(iox.ErrWouldBlock) || !iox.IsNonFailure(iox.ErrMore) {
			t.Fatal("IsNonFailure false for non-failures")
		}
		if iox.IsNonFailure(errors.New("x")) {
			t.Fatal("IsNonFailure true for other error")
		}
	})

	t.Run("IsProgress", func(t *testing.T) {
		if !iox.IsProgress(nil) || !iox.IsProgress(iox.ErrMore) {
			t.Fatal("IsProgress false for progress cases")
		}
		if iox.IsProgress(iox.ErrWouldBlock) {
			t.Fatal("IsProgress true for would-block")
		}
	})
}

func TestOutcomeStringAndClassify(t *testing.T) {
	t.Run("OutcomeStrings", func(t *testing.T) {
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
	})

	t.Run("ClassifyVariants", func(t *testing.T) {
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
	})
}
