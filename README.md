# iox

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Benchmarks](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/iox/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/iox)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Non-blocking semantics for Go `io` package: first-class signals for would-block and multi-shot.

Language: **English** | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | [Français](./README.fr.md)

## Overview

`iox` is for non-blocking I/O stacks where “wait for readiness before retrying” (`ErrWouldBlock`) and “keep observing the same operation” (`ErrMore`) are normal control flow.

Its copy and tee helpers use Go’s standard `io.Reader` and `io.Writer` interfaces. In the familiar `(n, err)` result, `n` reports bytes already transferred and `err` guides the caller’s next step. Either signal can accompany zero or more bytes, so account for `n` first.

## I/O results

| Error | `Classify` result | Caller action |
|---|---|---|
| `nil` | `OutcomeOK` | Continue or finish the transfer |
| `ErrWouldBlock` | `OutcomeWouldBlock` | Wait for readiness before retrying |
| `ErrMore` | `OutcomeMore` | Keep the operation active and observe the next completion |
| Other error | `OutcomeFailure` | Handle the error with the count already reported |

On a direct `Read`, `io.EOF` marks the end of input. Process any final bytes before finishing. Passing `io.EOF` to `Classify` yields `OutcomeFailure`.

## Installation and example

This module requires Go 1.27 or later.

```shell
go get code.hybscloud.com/iox
```

The second `Copy` transfers `"world"` before the reader reports `ErrWouldBlock`. The caller consumes those five bytes with `CopyN`, then uses its `waitReadable` function to wait for the source to become readable again. [Readiness example](examples/wouldblock.go)

```go
src := &reader{
	{data: "hello", err: iox.ErrMore},
	{data: "world"},
	{err: iox.ErrWouldBlock},
	{data: "iox"},
}
var dst bytes.Buffer

n, err := iox.Copy(&dst, src)
fmt.Printf("%d %v %q\n", n, err, dst.String()) // 5 io: expect more "hello"
_, _ = iox.CopyN(io.Discard, &dst, n)

if iox.Classify(err) == iox.OutcomeMore {
	n, err = iox.Copy(&dst, src)
	fmt.Printf("%d %v %q\n", n, err, dst.String()) // 5 io: would block "world"
	_, _ = iox.CopyN(io.Discard, &dst, n)
}

if iox.Classify(err) == iox.OutcomeWouldBlock {
	waitReadable()
	n, err = iox.Copy(&dst, src)
	fmt.Printf("%d %v %q\n", n, err, dst.String()) // 3 <nil> "iox"
}
return err
```

The reader keeps any bytes that do not fit in `p`. A zero-length read leaves the chunk untouched. The error is returned with the chunk’s last byte, or on its own for an empty chunk.

```go
type reader []struct {
	data string
	err  error
}

func (r *reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(*r) == 0 {
		return 0, io.EOF
	}
	chunk := &(*r)[0]
	n := copy(p, chunk.data)
	if n < len(chunk.data) {
		chunk.data = chunk.data[n:]
		return n, nil
	}
	*r = (*r)[1:]
	return n, chunk.err
}
```

## Canonical control loop

Callers should handle progress first, then classify control with the package helpers:

```go
for {
	n, err := op()
	if n > 0 {
		consume(n)
	}

	switch {
	case err == nil:
		return nil
	case iox.IsWouldBlock(err):
		wait()
		continue
	case iox.IsMore(err):
		continue
	default:
		return err
	}
}
```

## API overview

- Errors
  - `ErrWouldBlock`, `ErrMore`, `ErrNoSeeker`

- Copy
  - `Copy(dst Writer, src Reader) (int64, error)`
  - `CopyBuffer(dst Writer, src Reader, buf []byte) (int64, error)`
  - `CopyN(dst Writer, src Reader, n int64) (int64, error)`
  - `CopyNBuffer(dst Writer, src Reader, n int64, buf []byte) (int64, error)`
  - `CopyPolicy`, `CopyBufferPolicy`, `CopyNPolicy`, and `CopyNBufferPolicy` take a final `SemanticPolicy` argument.

- Tee
  - `TeeReader(r Reader, w Writer) Reader`
  - `TeeWriter(primary, tee Writer) Writer`
  - `TeeReaderPolicy` and `TeeWriterPolicy` also take a final `SemanticPolicy` argument.

- Adapters
  - `AsWriterTo(r Reader) Reader` (adds `io.WriterTo` via `iox.Copy`)
  - `AsReaderFrom(w Writer) Writer` (adds `io.ReaderFrom` via `iox.Copy`)

- Semantics
  - `Outcome`
  - `Classify(err error) Outcome`
  - `IsSemantic(err error) bool`
  - `IsNonFailure(err error) bool`
  - `IsFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

- Backoff
  - `Backoff` — adaptive back-off for external I/O waiting
  - `DefaultBackoffBase` (500µs), `DefaultBackoffMax` (100ms)

## Resumable I/O

`Step.Next` returns a value, an error and a `Wait`. A zero wait means the step has finished. `Finish` supplies the terminal result. `Await` describes a wait for a typed value and a continuation that returns the next step.

Call `Wait.Arm` with the driver’s enqueue function. The trigger can signal before `Arm` returns. The first accepted `Signal.Deliver` enqueues one `Ready`. A worker can claim it once with `TryResume` to run the continuation. If `Deliver` returns `false`, the source keeps its value. [Typed completion example](example_await_test.go)

`WriteStep` records the count and error from each write. If the writer accepts only a prefix and returns `ErrWouldBlock`, a wake permits one attempt to write the suffix to the same writer. If the writer accepts the whole buffer, the step finishes with its error, including `ErrWouldBlock`. Keep the buffer valid and unmodified until completion or successful withdrawal.

## Backoff — Adaptive Waiting for External I/O

When a readiness notification is available, wait for it after `ErrWouldBlock`. Otherwise, `iox.Backoff.Wait` sleeps the calling goroutine between attempts.

**Three-Tier Progress Model:**

| Tier | Mechanism                        | Use Case |
|------|----------------------------------|----------|
| Strike | System call                      | Direct kernel hit |
| Spin | Hardware yield (`spin`)          | Local atomic synchronization |
| **Adapt** | Software backoff (`iox.Backoff`) | External I/O readiness |

**Zero-value ready to use:**

```go
var b iox.Backoff // uses DefaultBackoffBase (500µs) and DefaultBackoffMax (100ms)

for {
	n, err := conn.Read(buf)
	if n > 0 {
		process(buf[:n])
		b.Reset() // reset on successful progress
	}
	if iox.IsWouldBlock(err) {
		b.Wait() // adaptive sleep with jitter
		continue
	}
	if err != nil || n == 0 {
		return err
	}
}
```

Process any bytes first. If the error matches `ErrWouldBlock`, the loop waits before retrying, even if it also matches `ErrMore`. The loop returns other errors unchanged. A `(0, nil)` read returns control to the caller and can occur before EOF.

Block `n` contains `n` retry intervals. `Duration()` reports the nominal interval, `min(n × base, max)`. Jitter varies each interval by up to 12.5%, so it can exceed `max`.

- `NextDelay()` advances the schedule once and returns the interval for a retry timer.
- `Wait()` calls `NextDelay()` and sleeps the calling goroutine for that interval.
- `Reset()` returns to block 1 and preserves the base and cap.
- `SetBase(d)` and `SetMax(d)` accept positive durations and preserve the current block.

## Tee semantics (counts and errors)

- `TeeReader` returns `n` as the number of bytes read from `r` (source progress), even if the side write fails/is short.
- `TeeWriter` returns `n` as the number of bytes accepted by `primary` (primary progress), even if the tee write fails/is short.
- When `n > 0`, a tee adapter may return `(n, err)` where `err` comes from the side/tee (including `ErrWouldBlock`/`ErrMore`). Process `p[:n]` first.
- For best interoperability with policy-driven helpers, return `ErrWouldBlock`/`ErrMore` as-is (avoid wrapping).

## Semantic Policy

Some helpers accept an optional `SemanticPolicy` to decide what to do when they encounter `ErrWouldBlock` or `ErrMore` (e.g., return immediately vs yield and retry).

The default `nil` policy leaves waiting and retrying to the caller. For `n > 0`, `CopyNPolicy` and `CopyNBufferPolicy` return `(n, nil)` once the destination accepts all `n` bytes. They skip policy callbacks for a signal reported with the final byte.

## Classification and exact sentinel dispatch

Classification helpers (`IsMore`, `IsWouldBlock`, `IsSemantic`, `IsNonFailure`, `IsFailure`, and `Classify`) use `errors.Is`, so wrapped semantic sentinels classify correctly at API boundaries and in diagnostics. Prefer these helpers over direct `errors.Is` calls when classifying `iox` semantic-control outcomes.

`IsProgress` is true only when `Classify` selects `OutcomeOK` or `OutcomeMore`. If an error matches both semantic sentinels, `ErrWouldBlock` takes precedence and `IsProgress` is false. `IsMore` and `IsWouldBlock` report sentinel membership, so both may be true for the same error. These predicates do not measure byte progress; always account for returned counts separately.

Copy and tee helpers consult the policy only for the exact `ErrMore` and `ErrWouldBlock` sentinels. Return these sentinels unwrapped when you want the policy to handle them.

## Fast paths and semantic preservation

`Copy` prefers the source’s `io.WriterTo`, then the destination’s `io.ReaderFrom`. The generic loop uses the supplied buffer or a 32 KiB default. `CopyN` limits the source to the remaining byte count. It can use the destination’s `ReadFrom` and bypasses the original source’s `WriteTo`.

A delegated method reports the total bytes transferred and one error. Return `ErrWouldBlock` or `ErrMore` unwrapped so a policy can handle the signal. A method that supports retries must preserve any unwritten bytes. `YieldOnWriteWouldBlockPolicy` retries `ErrWouldBlock` from either method, even when a read produced it.

If you have a plain `io.Reader`/`io.Writer` but want the fast-path interfaces to exist *and* preserve semantics, wrap with:

- `iox.AsWriterTo(r)` to add a `WriteTo` implemented via `iox.Copy`
- `iox.AsReaderFrom(w)` to add a `ReadFrom` implemented via `iox.Copy`

### Empty reads and bounded copies

In the generic read/write loop, `Copy` stops on a `(0, nil)` read and returns `(written, nil)`. Such a read can occur before EOF. A delegated `WriteTo` or `ReadFrom` controls its own reads.

For a positive limit, `CopyN` and its variants return `(n, nil)` once the destination has accepted `n` bytes. An early EOF or a shorter copy ending with `nil` yields `io.ErrUnexpectedEOF`. The underlying multi-shot operation can remain active after the bounded copy finishes.

### Partial writes

When the generic loop returns a semantic write error after a short write, it rewinds the unwritten suffix with `seeker.Seek(int64(nw-nr), io.SeekCurrent)`. Here `nr` is the number of bytes read and `nw` the number accepted by the destination. A failed seek takes precedence over the write error. A source without `io.Seeker` yields `ErrNoSeeker`.

With `CopyPolicy`, `PolicyRetry` retains that suffix across retries of an exact `ErrWouldBlock` or `ErrMore`, including on non-seekable sources. A delegated `WriteTo` or `ReadFrom` owns its own source position and recovery.

## Benchmarks

`BenchmarkCopyBufferReuse` compares `iox.CopyBuffer`, `iox.CopyBufferPolicy` with `ReturnPolicy`, and `io.CopyBuffer` on the generic read/write path. Each case reuses a 32 KiB buffer to copy the same in-memory input until EOF.

```shell
go test -run '^$' -bench '^BenchmarkCopyBufferReuse$' -benchmem -benchtime=200ms -count=10 -cpu=1 -timeout=120s .
```

Run both revisions on the same machine with the same Go version and similar load. Compare `ns/op` and `allocs/op` for matching input sizes. To measure wake-up latency, use a workload that waits for I/O readiness.

## License

MIT — see [LICENSE](./LICENSE).

©2025 Hayabusa Cloud Co., Ltd.
