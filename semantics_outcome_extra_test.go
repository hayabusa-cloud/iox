// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"testing"

	"code.hybscloud.com/iox"
)

func TestOutcomeString_DefaultFailureBranch(t *testing.T) {
	if got := iox.Outcome(255).String(); got != "Failure" {
		t.Fatalf("Outcome.String() default = %q", got)
	}
}
