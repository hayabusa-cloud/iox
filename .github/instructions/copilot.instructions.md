# iox — Copilot Instructions

## Semantics

| Error | Meaning |
|-------|---------|
| `nil` | Completion |
| `ErrWouldBlock` | Wait for readiness before retrying |
| `ErrMore` | The operation remains active and another observation is expected |
| other | Failure |

Both signals are non-failure control outcomes. Account for the returned count first. Either signal can accompany zero or positive progress.

## Classification

| Function | True when |
|----------|-----------|
| `IsSemantic(err)` | Matches ErrWouldBlock or ErrMore, including wrapped errors |
| `IsNonFailure(err)` | nil or semantic |
| `IsProgress(err)` | `Classify` selects `OutcomeOK` or `OutcomeMore`. WouldBlock takes precedence if both sentinels match. |

## Backoff Usage

`iox.Backoff` implements the "Adapt" tier of the Strike-Spin-Adapt progress model:

| Tier | Use Case |
|------|----------|
| Strike | System call (direct kernel hit) |
| Spin | Hardware yield for atomic synchronization |
| **Adapt** | External I/O wait (`iox.Backoff`) |

- Zero-value is ready to use (default: 500µs base, 100ms max)
- Call `Reset()` after positive byte progress, then `Wait()` if the operation would block.
- Algorithm: Block-based linear scaling with ±12.5% jitter

## Policy Behavior

`SemanticPolicy` controls helper responses to exact semantic sentinels. A bounded copy completes when the destination accepts its final requested byte.

| Policy | `ErrWouldBlock` | `ErrMore` |
|--------|-----------------|-----------|
| `nil` (default) | Return to caller | Return to caller |
| `ReturnPolicy` | Return | Return |
| `YieldPolicy` | Yield + retry | Return to caller |
| `YieldOnWriteWouldBlockPolicy` | Yield on writes and on delegated `WriteTo` or `ReadFrom` calls, then retry. Return on direct reads. | Return |
| Custom | `OnWouldBlock(op)` | `OnMore(op)` |

## Tee Count Semantics

- `TeeReader.Read` returns `n` = bytes read from source (even if side-write fails)
- `TeeWriter.Write` returns `n` = bytes accepted by primary (even if tee-write fails)
- When `n > 0` with error: process `p[:n]` first, then handle error

## Review Guidelines

**Only report true mistakes.**
