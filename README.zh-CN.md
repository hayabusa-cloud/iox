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

### 注意：`iox.Copy` 与 `(0, nil)` 的 Read

Go 的 `io.Reader` 契约允许 `Read` 返回 `(0, nil)` 来表示“没有进展”，而不是流结束。
行为良好的 Reader 应避免 `(0, nil)`（除非 `len(p) == 0`）。

`iox.Copy` 会把 `(0, nil)` 读视为“现在停止复制”，并返回 `(written, nil)`。
这样可以避免在非阻塞/事件循环代码中，把“自旋等待”隐藏在 helper 内。
如果你需要在多次 `(0, nil)` 之间强制前向进展检测，请在调用方实现该策略。

## 快速开始

使用 `go get` 安装：

```shell
go get code.hybscloud.com/iox
```

```go
package main

import (
    "bytes"
    "fmt"

    "code.hybscloud.com/iox"
)

func main() {
    src := bytes.NewBufferString("hello")
    var dst bytes.Buffer
    n, err := iox.Copy(&dst, src)
    fmt.Println(n, err, dst.String()) // 5 <nil> hello
}
```

## API 概览

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
  - `AsWriterTo(r Reader) Reader`（通过 `iox.Copy` 添加 `io.WriterTo`）
  - `AsReaderFrom(w Writer) Writer`（通过 `iox.Copy` 添加 `io.ReaderFrom`）

- Semantics
  - `IsNonFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

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
