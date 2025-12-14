// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"bytes"
	"io"

	"code.hybscloud.com/iox"
)

// recPolicy is a configurable SemanticPolicy used to drive retry/return
// behavior and to record Yield invocations for verification.
type recPolicy struct {
	onWB   map[iox.Op]iox.PolicyAction
	onMore map[iox.Op]iox.PolicyAction
	yields []iox.Op
}

func (p *recPolicy) Yield(op iox.Op) { p.yields = append(p.yields, op) }
func (p *recPolicy) OnWouldBlock(op iox.Op) iox.PolicyAction {
	if p.onWB != nil {
		if a, ok := p.onWB[op]; ok {
			return a
		}
	}
	return iox.PolicyReturn
}
func (p *recPolicy) OnMore(op iox.Op) iox.PolicyAction {
	if p.onMore != nil {
		if a, ok := p.onMore[op]; ok {
			return a
		}
	}
	return iox.PolicyReturn
}

// scriptedWT implements iox.Reader with WriterTo fast path producing a scripted
// sequence of (n, err) results. For each call it writes n bytes into dst.
type scriptedWT struct {
	seq []struct {
		n   int64
		err error
	}
	i int
}

func (scriptedWT) Read(p []byte) (int, error) { return 0, io.EOF }

func (s *scriptedWT) WriteTo(dst iox.Writer) (int64, error) {
	if s.i >= len(s.seq) {
		return 0, io.EOF
	}
	st := s.seq[s.i]
	s.i++
	if st.n > 0 {
		buf := bytes.Repeat([]byte{'w'}, int(st.n))
		n, _ := dst.Write(buf)
		return int64(n), st.err
	}
	return 0, st.err
}

// scriptedRF implements iox.Writer with ReaderFrom fast path producing a scripted
// sequence of (n, err) results. It ignores the src and returns scripted results.
type scriptedRF struct {
	seq []struct {
		n   int64
		err error
	}
	i int
}

func (scriptedRF) Write(p []byte) (int, error) { return len(p), nil }

func (s *scriptedRF) ReadFrom(src iox.Reader) (int64, error) {
	if s.i >= len(s.seq) {
		return 0, io.EOF
	}
	st := s.seq[s.i]
	s.i++
	return st.n, st.err
}
