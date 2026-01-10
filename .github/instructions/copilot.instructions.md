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

## Review Guidelines

**Only report true mistakes.**
