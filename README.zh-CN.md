# iox

为 Go `io` 包提供非阻塞语义：将“会阻塞（would-block）”与“多次完成（multi-shot）”作为一等信号。

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/iox)](https://goreportcard.com/report/github.com/hayabusa-cloud/iox)
[![Coverage Status](https://coveralls.io/repos/github/hayabusa-cloud/iox/badge.svg?branch=main)](https://coveralls.io/github/hayabusa-cloud/iox?branch=main)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

语言： [English](./README.md) | **简体中文** | [Español](./README.es.md) | [日本語](./README.ja.md) | [Français](./README.fr.md)

## 这个包是做什么的？

`iox` 面向非阻塞 I/O 栈：在这类系统里，“现在没有进展”与“现在有进展，但操作仍然活跃”是**正常控制流**，不是失败。

它引入两个具有明确契约的语义错误（semantic errors）：

- `ErrWouldBlock` — **当前无法取得任何进展**，必须等待就绪/完成事件。应立即返回；在下一次轮询之后重试。
- `ErrMore` — **已产生进展**，且操作仍然活跃；**后续还会有更多事件/完成**。应处理当前结果，并继续轮询。

`iox` 保持标准 `io` 的心智模型：

- 返回的计数始终表示“已传输字节数 / 已取得进展”
- 返回的错误用于驱动控制流（`nil`、语义型非失败、或真实失败）
- helper 与 `io.Reader`、`io.Writer` 兼容，并通过 `io.WriterTo` / `io.ReaderFrom` 做快速路径优化

## 语义契约

对于采用 `iox` 语义的操作：

| 返回错误 | 含义 | 调用方下一步必须做什么 |
|---|---|---|
| `nil` | 本次调用/传输已成功完成 | 继续你的状态机 |
| `ErrWouldBlock` | 当前无法取得进展 | 停止尝试；等待就绪/完成；再重试 |
| `ErrMore` | 已取得进展；后续还会继续完成 | 现在就处理；保持操作活跃；继续轮询 |
| 其他错误 | 失败 | 按需处理/记录/关闭/退避 |

补充说明：
- `iox.Copy` 可能返回 `(written > 0, ErrWouldBlock)` 或 `(written > 0, ErrMore)`，用于表达“已产生部分进展，然后发生停滞”或“已产生部分进展，并需要交付 multi-shot 的后续”。
- 对于 `(0, nil)` 的 Read，`iox.Copy` 会将其视为“现在停止复制”，并返回 `(written, nil)`，以避免在 helper 内隐藏自旋。

### 注意：`iox.Copy` 与 `(0, nil)` 的 Read

Go 的 `io.Reader` 契约允许 `Read` 返回 `(0, nil)` 来表示“没有进展”，而不是流结束。
行为良好的 Reader 应避免 `(0, nil)`（除非 `len(p) == 0`）。

`iox.Copy` 会把 `(0, nil)` 读视为“现在停止复制”，并返回 `(written, nil)`。
这样可以避免在非阻塞/事件循环代码中，把“自旋等待”隐藏在 helper 内。
如果你需要在多次 `(0, nil)` 之间强制前向进展检测，请在调用方实现该策略。

### 注意：`iox.Copy` 与部分写入恢复

当向非阻塞目标复制时，`dst.Write` 可能返回语义错误（`ErrWouldBlock` 或 `ErrMore`）并伴随部分写入（`nw < nr`）。此时，字节已从 `src` 读出但未完全写入 `dst`。

为防止数据丢失，`iox.Copy` 会尝试回滚源指针：
- 如果 `src` 实现了 `io.Seeker`，Copy 调用 `Seek(nw-nr, io.SeekCurrent)` 来回退未写入的字节。
- 如果 `src` **未**实现 `io.Seeker`，Copy 返回 `ErrNoSeeker` 以表明未写入的字节无法恢复。

**建议：**
- 向非阻塞目标复制时，使用可寻址的源（如 `*os.File`、`*bytes.Reader`）。
- 对于不可寻址的源（如网络套接字），使用 `CopyPolicy` 并为写入端语义错误配置 `PolicyRetry`。这可确保所有已读字节在返回前被写入，从而避免回滚需求。

## 快速开始

使用 `go get` 安装：

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

## API 概览

- Errors
  - `ErrWouldBlock`, `ErrMore`, `ErrNoSeeker`

- Copy
  - `Copy(dst Writer, src Reader) (int64, error)`
  - `CopyBuffer(dst Writer, src Reader, buf []byte) (int64, error)`
  - `CopyN(dst Writer, src Reader, n int64) (int64, error)`
  - `CopyNBuffer(dst Writer, src Reader, n int64, buf []byte) (int64, error)`

- Tee
  - `TeeReader(r Reader, w Writer) Reader`
  - `TeeWriter(primary, tee Writer) Writer`

- Adapters
  - `AsWriterTo(r Reader) Reader`（通过 `iox.Copy` 添加 `io.WriterTo`）
  - `AsReaderFrom(w Writer) Writer`（通过 `iox.Copy` 添加 `io.ReaderFrom`）

- Semantics
  - `IsNonFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

- Backoff
  - `Backoff` — 用于外部 I/O 等待的自适应退避
  - `DefaultBackoffBase` (500µs)、`DefaultBackoffMax` (100ms)

## Backoff — 外部 I/O 的自适应等待

当 `ErrWouldBlock` 表示当前无法取得进展时，调用方必须等待后再重试。`iox.Backoff` 为此提供自适应退避策略。

**三层进展模型：**

| 层级 | 机制 | 使用场景 |
|------|------|----------|
| Strike | 系统调用 | 直接内核命中 |
| Spin | 硬件让步 (`spin`) | 本地原子同步 |
| **Adapt** | 软件退避 (`iox.Backoff`) | 外部 I/O 就绪 |

**零值即可使用：**

```go
var b iox.Backoff  // 使用 DefaultBackoffBase (500µs) 和 DefaultBackoffMax (100ms)

for {
    n, err := conn.Read(buf)
    if err == iox.ErrWouldBlock {
        b.Wait()  // 带抖动的自适应睡眠
        continue
    }
    if err != nil {
        return err
    }
    process(buf[:n])
    b.Reset()  // 成功进展后重置
}
```

**算法：** 基于块的线性扩展，带 ±12.5% 抖动以防止惊群效应。
- 块 1：1 次 `base` 时长的睡眠
- 块 2：2 次 `2×base` 时长的睡眠
- 块 n：n 次 `min(n×base, max)` 时长的睡眠

**方法：**
- `Wait()` — 按当前时长睡眠，然后推进
- `Reset()` — 恢复到块 1
- `SetBase(d)` / `SetMax(d)` — 配置时间参数

## Tee 语义（计数与错误）

- `TeeReader` 的 `n` 表示从 `r` 读出的字节数（source progress），即使 side 写入失败/短写也不会改变 `n`。
- `TeeWriter` 的 `n` 表示 primary 接受的字节数（primary progress），即使 tee 写入失败/短写也不会改变 `n`。
- 当 `n > 0` 时，tee 适配器可能返回 `(n, err)`，且 `err` 可能来自 side/tee（包含 `ErrWouldBlock`/`ErrMore`）。调用方应先处理 `p[:n]`。
- 为了让策略（policy）驱动的 helper 行为更可预测，建议直接返回 `ErrWouldBlock`/`ErrMore`（避免包装）。

## 语义策略

一些 helper 支持可选的 `SemanticPolicy`，用于在遇到 `ErrWouldBlock` 或 `ErrMore` 时决定采取何种行为
（例如：立即返回，或让出/yield 后再重试）。

默认值为 `nil`，表示**保持非阻塞行为**：helper 会将 `ErrWouldBlock` / `ErrMore` 直接返回给调用方，
不会自行等待或重试。

## 快速路径与语义保持

`iox.Copy` 在可用时使用标准 `io` 的快速路径：

- 如果 `src` 实现了 `io.WriterTo`，`iox.Copy` 调用 `WriteTo`
- 否则如果 `dst` 实现了 `io.ReaderFrom`，`iox.Copy` 调用 `ReadFrom`
- 否则使用固定大小的栈缓冲（`32KiB`）以及读/写循环

为了在快速路径上保持 `ErrWouldBlock` / `ErrMore`，请确保你的 `WriteTo` / `ReadFrom` 实现在合适的情况下返回这些错误。

如果你只有普通的 `io.Reader` / `io.Writer`，但希望存在快速路径接口并保持语义，可以用下面的包装器：

- `iox.AsWriterTo(r)`：添加由 `iox.Copy` 实现的 `WriteTo`
- `iox.AsReaderFrom(w)`：添加由 `iox.Copy` 实现的 `ReadFrom`

## License

MIT — 见 [LICENSE](./LICENSE)。

©2025 Hayabusa Cloud Co., Ltd.
