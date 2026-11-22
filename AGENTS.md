# iox

Use `iox` with Go 1.27 or later for nonblocking I/O through Go’s `io.Reader` and `io.Writer` interfaces.

- Process the returned count before classifying the error. On `ErrWouldBlock`, wait for readiness before retrying any outstanding work. On `ErrMore`, keep observing the current operation. If both match, `Classify` selects `OutcomeWouldBlock`.
- Copy and tee policies dispatch on exact sentinels. When resuming a recoverable `CopyN`, request only the bytes still outstanding.
- With `WriteStep`, keep the buffer valid and unmodified until completion or successful withdrawal. A trigger must cover wakes during registration. A successful `Signal.Deliver` enqueues one `Ready` for a worker to claim with `TryResume`.

From the module root, run `go vet ./...` and `go test -race -timeout=120s ./...`. The [benchmark instructions](README.md#benchmarks) cover local copy and allocation measurements.
