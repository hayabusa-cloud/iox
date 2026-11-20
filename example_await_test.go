// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"fmt"

	"code.hybscloud.com/iox"
)

// The source remembers where to deliver the next frame for this demand.
type frameSource struct{ next iox.Signal[[]byte] }

func (f *frameSource) Arm(_ iox.Ticket, next iox.Signal[[]byte]) error {
	f.next = next
	return nil
}

func ExampleAwait() {
	var source frameSource
	step := iox.Await(iox.Ticket{Demand: 1}, &source, func(frame []byte) iox.Step[int] {
		return iox.Finish(len(frame), nil)
	})
	_, _, wait := step.Next()

	queue := make(chan iox.Ready[int], 1)
	if err := wait.Arm(func(r iox.Ready[int]) { queue <- r }); err != nil {
		panic(err)
	}
	// An empty frame arrives. The driver dispatches the work on a later turn.
	if !source.next.Deliver([]byte{}) {
		return
	}
	if next, ok := (<-queue).TryResume(); ok {
		size, err, _ := next.Next()
		fmt.Println(size, err)
	}
	// Output: 0 <nil>
}
