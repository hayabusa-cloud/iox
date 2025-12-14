# iox

Non-blocking semantics for Go `io` package: first-class signals for would-block and multi-shot.

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/iox)](https://goreportcard.com/report/github.com/hayabusa-cloud/iox)
[![Coverage Status](https://coveralls.io/repos/github/hayabusa-cloud/iox/badge.svg?branch=main)](https://coveralls.io/github/hayabusa-cloud/iox?branch=main)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Language: **English** | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | [Français](./README.fr.md)

## What this package is?

`iox` is for non-blocking I/O stacks where “no progress right now” and “progress now, but the operation remains active” are **normal control flow**, not failures.

It introduces two semantic errors with explicit contracts:

- `ErrWouldBlock` — **no progress is possible now** without waiting for readiness/completions. Return immediately; retry after your next polling.
- `ErrMore` — **progress happened** and the operation remains active; **more events will follow**. Process the current result and keep polling.

`iox` keeps standard `io` mental models intact:

- returned counts always mean “bytes transferred / progress made”
- returned errors drive control flow (`nil`, semantic non-failure, or real failure)
- helpers are compatible with `io.Reader`, `io.Writer`, and optimize via `io.WriterTo` / `io.ReaderFrom`

## Semantics contract

For operations that adopt `iox` semantics:

| Return error | Meaning | What the caller must do next |
|---|---|---|
| `nil` | completed successfully for this call / transfer | continue your state machine |
| `ErrWouldBlock` | no progress possible now | stop attempting; wait for readiness/completion; retry |
| `ErrMore` | progress happened; more completions will follow | process now; keep the operation active; continue polling |
| other error | failure | handle/log/close/backoff as appropriate |

Notes:
- `iox.Copy` may return `(written > 0, ErrWouldBlock)` or `(written > 0, ErrMore)` to report partial progress before stalling or before delivering a multi-shot continuation.
- `(0, nil)` reads are treated as “stop copying now” and return `(written, nil)` to avoid hidden spinning inside helpers.

### Note: `iox.Copy` and `(0, nil)` reads

The Go `io.Reader` contract allows `Read` to return `(0, nil)` to mean “no progress”, not end-of-stream.
Well-behaved Readers should avoid `(0, nil)` except when `len(p) == 0`.

`iox.Copy` intentionally treats a `(0, nil)` read as “stop copying now” and returns `(written, nil)`.
This avoids hidden spinning inside a helper in non-blocking/event-loop code.
If you need strict forward-progress detection across repeated `(0, nil)`, implement that policy at your call site.

## Quick start

Install with `go get`:
```shell
go get code.hybscloud.com/iox
```

```go
type reader struct{ step int }

func (r *reader) Read(p []byte) (int, error) {
	switch r.step {
	case 0:
		r.step++
		return copy(p, "hello"), iox.ErrMore
	case 1:
		r.step++
		return copy(p, "world"), nil
	case 2:
		r.step++
		return 0, iox.ErrWouldBlock
	case 3:
		r.step++
		return copy(p, "iox"), nil
	default:
		return 0, io.EOF
	}
}

func main() {
	src := &reader{}
	var dst bytes.Buffer

	n, err := iox.Copy(&dst, src)
	fmt.Printf("n=%d err=%v buf=%q\n", n, err, dst.String()) // n=5  err=io: expect more  buf="hello"
	_, _ = iox.CopyN(io.Discard, &dst, 5)                    // consume "hello"

	n, err = iox.Copy(&dst, src)
	fmt.Printf("n=%d err=%v buf=%q\n", n, err, dst.String()) // n=5  err=io: would block   buf="world"
	_, _ = iox.CopyN(io.Discard, &dst, 5)                    // consume "world"

	n, err = iox.Copy(&dst, src)
	fmt.Printf("n=%d err=%v buf=%q\n", n, err, dst.String()) // n=3  err=<nil>            buf="iox"
}
```

## API overview

- Errors
  - `ErrWouldBlock`, `ErrMore`

- Copy
  - `Copy(dst Writer, src Reader) (int64, error)`
  - `CopyBuffer(dst Writer, src Reader, buf []byte) (int64, error)`
  - `CopyN(dst Writer, src Reader, n int64) (int64, error)`
  - `CopyNBuffer(dst Writer, src Reader, n int64, buf []byte) (int64, error)`

- Tee
  - `TeeReader(r Reader, w Writer) Reader`
  - `TeeWriter(primary, tee Writer) Writer`

- Adapters
  - `AsWriterTo(r Reader) Reader` (adds `io.WriterTo` via `iox.Copy`)
  - `AsReaderFrom(w Writer) Writer` (adds `io.ReaderFrom` via `iox.Copy`)

- Semantics
  - `IsNonFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

## Tee semantics (counts and errors)

- `TeeReader` returns `n` as the number of bytes read from `r` (source progress), even if the side write fails/is short.
- `TeeWriter` returns `n` as the number of bytes accepted by `primary` (primary progress), even if the tee write fails/is short.
- When `n > 0`, a tee adapter may return `(n, err)` where `err` comes from the side/tee (including `ErrWouldBlock`/`ErrMore`). Process `p[:n]` first.
- For best interoperability with policy-driven helpers, return `ErrWouldBlock`/`ErrMore` as-is (avoid wrapping).

## Semantic Policy

Some helpers accept an optional `SemanticPolicy` to decide what to do when they encounter `ErrWouldBlock` or `ErrMore`
(e.g., return immediately vs yield and retry).

The default is `nil`, which means **non-blocking behavior is preserved**: the helper returns `ErrWouldBlock` / `ErrMore`
to the caller and does not wait or retry on its own.

## Fast paths and semantic preservation

`iox.Copy` uses standard "io" fast paths when available:

- if `src` implements `io.WriterTo`, `iox.Copy` calls `WriteTo`
- else if `dst` implements `io.ReaderFrom`, `iox.Copy` calls `ReadFrom`
- else it uses a fixed-size stack buffer (`32KiB`) and a read/write loop

To preserve `ErrWouldBlock` / `ErrMore` across fast paths, ensure your `WriteTo` / `ReadFrom` implementations return those errors when appropriate.

If you have a plain `io.Reader`/`io.Writer` but want the fast-path interfaces to exist *and* preserve semantics, wrap with:

- `iox.AsWriterTo(r)` to add a `WriteTo` implemented via `iox.Copy`
- `iox.AsReaderFrom(w)` to add a `ReadFrom` implemented via `iox.Copy`

## License

MIT — see [LICENSE](./LICENSE).

©2025 Hayabusa Cloud Co., Ltd.
