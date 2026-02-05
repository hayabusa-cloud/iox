# iox — Copilot Instructions

## Semantics

| Error | Meaning |
|-------|---------|
| `nil` | Completion |
| `ErrWouldBlock` | No progress; retry later |
| `ErrMore` | Progress made; more coming |
| other | Failure |

`ErrWouldBlock` and `ErrMore` are **control flow**, not failures.

## Classification

| Function | True when |
|----------|-----------|
| `IsSemantic(err)` | ErrWouldBlock or ErrMore |
| `IsNonFailure(err)` | nil or semantic |
| `IsProgress(err)` | nil or ErrMore |

## Backoff Usage

`iox.Backoff` implements the "Adapt" tier of the Strike-Spin-Adapt progress model:

| Tier | Use Case |
|------|----------|
| Strike | System call (direct kernel hit) |
| Spin | Hardware yield for atomic synchronization |
| **Adapt** | External I/O wait (`iox.Backoff`) |

- Zero-value is ready to use (default: 500µs base, 100ms max)
- Call `Wait()` on `ErrWouldBlock`, `Reset()` on success
- Algorithm: Block-based linear scaling with ±12.5% jitter

## Policy Behavior

`SemanticPolicy` controls helper responses to semantic errors:

| Policy | `ErrWouldBlock` | `ErrMore` |
|--------|-----------------|-----------|
| `nil` (default) | Return to caller | Return to caller |
| `ReturnPolicy` | Return | Return |
| `YieldPolicy` | Yield + retry | Continue |
| Custom | `OnWouldBlock(op)` | `OnMore(op)` |

## Tee Count Semantics

- `TeeReader.Read` returns `n` = bytes read from source (even if side-write fails)
- `TeeWriter.Write` returns `n` = bytes accepted by primary (even if tee-write fails)
- When `n > 0` with error: process `p[:n]` first, then handle error

## Review Guidelines

**Only report true mistakes.**
