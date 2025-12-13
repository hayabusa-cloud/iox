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
	wb := fmt.Errorf("wrap: %w", iox.ErrWouldBlock)
	if !iox.IsWouldBlock(wb) || !iox.IsSemantic(wb) || iox.IsMore(wb) {
		t.Fatalf("wrapped would-block not detected properly")
	}
	if iox.Classify(wb) != iox.OutcomeWouldBlock {
		t.Fatalf("classify wrapped wouldblock")
	}

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
}
