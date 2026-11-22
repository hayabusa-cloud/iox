# iox

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Benchmarks](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/iox/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/iox)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

为 Go `io` 包提供非阻塞语义：把“会阻塞（would-block）”与“多次完成（multi-shot）”作为一等控制信号。

语言： [English](./README.md) | **简体中文** | [Español](./README.es.md) | [日本語](./README.ja.md) | [Français](./README.fr.md)

## 概述

`iox` 面向非阻塞 I/O 栈。在这类系统中，“等待 I/O 就绪后重试”（`ErrWouldBlock`）和“继续观察同一操作”（`ErrMore`）都是正常的控制流。

它的复制和分流（tee）函数使用 Go 标准库的 `io.Reader` 和 `io.Writer` 接口。在常见的返回值 `(n, err)` 中，`n` 表示已传输的字节数，`err` 指明调用方下一步该怎么做。无论返回哪种信号，`n` 都可能为零或正数。先将这 `n` 个字节计入传输进度，再处理信号。

## I/O 结果

| 错误 | `Classify` 的结果 | 调用方的操作 |
|---|---|---|
| `nil` | `OutcomeOK` | 继续或结束传输 |
| `ErrWouldBlock` | `OutcomeWouldBlock` | 等待 I/O 就绪后重试 |
| `ErrMore` | `OutcomeMore` | 保持操作有效，接收下一次完成通知 |
| 其他错误 | `OutcomeFailure` | 处理错误时计入已经传输的字节 |

直接调用 `Read` 时，`io.EOF` 表示输入结束。先处理最后返回的字节，再结束读取。将 `io.EOF` 传给 `Classify` 会得到 `OutcomeFailure`。

## 安装与示例

此模块要求使用 Go 1.27 或更高版本。

```shell
go get code.hybscloud.com/iox
```

第二次调用 `Copy` 时，先传输了 `"world"`，随后读取器才返回 `ErrWouldBlock`。调用方用 `CopyN` 取走这五个字节，再调用自己提供的 `waitReadable` 等待数据源重新可读。[等待就绪的示例](examples/wouldblock.go)

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

读取器会将 `p` 放不下的字节留到下次读取。传入长度为零的缓冲区时，读取位置不变。每个数据块的错误随最后一个字节一起返回，空块则单独返回错误。

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

## 标准控制循环

调用方应先处理进展，再用包内辅助函数分类控制：

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

## API 概览

- 错误
  - `ErrWouldBlock`, `ErrMore`, `ErrNoSeeker`

- 复制
  - `Copy(dst Writer, src Reader) (int64, error)`
  - `CopyBuffer(dst Writer, src Reader, buf []byte) (int64, error)`
  - `CopyN(dst Writer, src Reader, n int64) (int64, error)`
  - `CopyNBuffer(dst Writer, src Reader, n int64, buf []byte) (int64, error)`
  - `CopyPolicy`、`CopyBufferPolicy`、`CopyNPolicy` 和 `CopyNBufferPolicy` 的最后一个参数均为 `SemanticPolicy`。

- Tee
  - `TeeReader(r Reader, w Writer) Reader`
  - `TeeWriter(primary, tee Writer) Writer`
  - `TeeReaderPolicy` 和 `TeeWriterPolicy` 的最后一个参数也都是 `SemanticPolicy`。

- 适配器
  - `AsWriterTo(r Reader) Reader`（通过 `iox.Copy` 添加 `io.WriterTo`）
  - `AsReaderFrom(w Writer) Writer`（通过 `iox.Copy` 添加 `io.ReaderFrom`）

- 语义
  - `Outcome`
  - `Classify(err error) Outcome`
  - `IsSemantic(err error) bool`
  - `IsNonFailure(err error) bool`
  - `IsFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

- 退避
  - `Backoff` — 用于外部 I/O 等待的自适应退避
  - `DefaultBackoffBase` (500µs)、`DefaultBackoffMax` (100ms)

## 可恢复的 I/O

`Step.Next` 返回一个值、一个错误和一个 `Wait`。`Wait` 为零值表示该步骤已完成。`Finish` 给出最终结果。`Await` 描述要等待的值的类型，以及收到该值后返回下一步的续体。

调用 `Wait.Arm` 时传入执行器的入队函数。事件源可以在 `Arm` 返回前发出信号。首次被接受的 `Signal.Deliver` 调用将一个 `Ready` 放入队列。工作线程可用 `TryResume` 领取这项工作并执行续体，只有一次领取会成功。如果 `Deliver` 返回 `false`，事件源仍然拥有该值。 [接收类型化结果的示例](example_await_test.go)

`WriteStep` 记录每次写入的字节数和错误。如果写入端只接受了前缀并返回 `ErrWouldBlock`，收到通知后可以向同一写入端尝试写入一次剩余部分。如果写入端接受了整个缓冲区，操作就以此次返回的错误结束，包括 `ErrWouldBlock`。操作完成或成功撤回等待之前，请保持缓冲区有效且不修改其内容。

## Backoff — 外部 I/O 的自适应等待

如果可以接收就绪通知，遇到 `ErrWouldBlock` 时就等待通知。否则，可以用 `iox.Backoff.Wait` 在两次尝试之间挂起当前 goroutine。

**三层进展模型：**

| 层级 | 机制 | 使用场景 |
|------|------|----------|
| Strike | 系统调用 | 直接内核命中 |
| Spin | 硬件让步 (`spin`) | 本地原子同步 |
| **Adapt** | 软件退避 (`iox.Backoff`) | 外部 I/O 就绪 |

**零值即可使用：**

```go
var b iox.Backoff // 使用 DefaultBackoffBase (500µs) 和 DefaultBackoffMax (100ms)

for {
	n, err := conn.Read(buf)
	if n > 0 {
		process(buf[:n])
		b.Reset() // 成功进展后重置
	}
	if iox.IsWouldBlock(err) {
		b.Wait() // 带抖动的自适应等待
		continue
	}
	if err != nil || n == 0 {
		return err
	}
}
```

先处理已读取的字节。只要错误匹配 `ErrWouldBlock`，循环就会等待后再重试，即使该错误也匹配 `ErrMore`。其他错误原样返回。读取返回 `(0, nil)` 时，循环将控制权交还调用方，此时输入可能尚未结束。

第 `n` 个阶段包含 `n` 个重试间隔。`Duration()` 返回基准间隔 `min(n × base, max)`。每次重试的间隔在基准值上下浮动最多 12.5%，因此可能超过 `max`。

- `NextDelay()` 返回下一个重试间隔并推进重试进度，调用方可用返回值设置定时器。
- `Wait()` 调用 `NextDelay()`，让当前 goroutine 休眠相应时长。
- `Reset()` 回到第 1 个阶段，保留基准时长和上限。
- `SetBase(d)` 和 `SetMax(d)` 接受正的时长，保留当前阶段。

## Tee 语义（计数与错误）

- `TeeReader` 的 `n` 表示从 `r` 读出的字节数（源侧进展），即使副写入失败或短写也不会改变 `n`。
- `TeeWriter` 的 `n` 表示 primary 接受的字节数（primary 侧进展），即使 tee 写入失败或短写也不会改变 `n`。
- 当 `n > 0` 时，tee 适配器可能返回 `(n, err)`，且 `err` 可能来自副写入/tee（包含 `ErrWouldBlock`/`ErrMore`）。调用方应先处理 `p[:n]`。
- 为了让策略驱动的辅助函数行为更可预测，建议直接返回 `ErrWouldBlock`/`ErrMore`（避免包装）。

## 语义策略

一些辅助函数支持可选的 `SemanticPolicy`，用于在遇到 `ErrWouldBlock` 或 `ErrMore` 时决定采取何种行为（例如：立即返回，或让出/yield 后再重试）。

使用默认的 `nil` 策略时，由调用方负责等待和重试。当 `n > 0` 时，目标端一旦累计接收了 `n` 个字节，`CopyNPolicy` 和 `CopyNBufferPolicy` 就返回 `(n, nil)`。随最后一个字节返回的信号不会触发策略回调。

## 分类与精确哨兵分发

分类辅助函数（`IsMore`、`IsWouldBlock`、`IsSemantic`、`IsNonFailure`、`IsFailure` 和 `Classify`）使用 `errors.Is`，因此包装过的语义哨兵在 API 边界和诊断中也能正确分类。在分类 `iox` 语义控制结果时，优先使用这些辅助函数，而不是直接调用 `errors.Is`。

只有当 `Classify` 选择 `OutcomeOK` 或 `OutcomeMore` 时，`IsProgress` 才返回 true。如果同一错误匹配两个语义哨兵，`ErrWouldBlock` 优先，`IsProgress` 返回 false。`IsMore` 和 `IsWouldBlock` 检查是否匹配各自的哨兵，因此可以同时为 true。这些谓词不衡量字节进展；始终单独处理返回计数。

复制和 tee 辅助函数只有在直接收到 `ErrMore` 或 `ErrWouldBlock` 时才会调用策略。要让策略处理这些哨兵错误，请直接返回它们，不要包装。

## 快速路径与语义保持

`Copy` 优先使用源端的 `io.WriterTo`，其次使用目标端的 `io.ReaderFrom`。通用循环使用传入的缓冲区，未指定时默认使用 32 KiB 的缓冲区。`CopyN` 将读取量限制为剩余字节数。它可以调用目标端的 `ReadFrom`，并跳过原始源端的 `WriteTo`。

受委托的方法返回已传输的总字节数和一个错误。要让策略处理信号，请原样返回 `ErrWouldBlock` 或 `ErrMore`。支持重试的方法必须保留尚未写入的字节。只要其中一个方法返回 `ErrWouldBlock`，`YieldOnWriteWouldBlockPolicy` 就会重试，即使该信号来自读取操作。

如果你只有普通的 `io.Reader` / `io.Writer`，但希望同时提供快速路径接口并保持语义，可以使用下面的包装器：

- `iox.AsWriterTo(r)`：添加由 `iox.Copy` 实现的 `WriteTo`
- `iox.AsReaderFrom(w)`：添加由 `iox.Copy` 实现的 `ReadFrom`

### 空读与定长复制

在通用读写循环中，如果一次读取返回 `(0, nil)`，`Copy` 会停止并返回 `(written, nil)`。这种情况也可能发生在输入结束之前。委托给 `WriteTo` 或 `ReadFrom` 时，由受委托的方法控制读取。

当 `n` 为正数时，目标端累计接收 `n` 个字节后，`CopyN` 及其变体就返回 `(n, nil)`。如果提前遇到 EOF，或者复制以 `nil` 结束但字节数不足，则返回 `io.ErrUnexpectedEOF`。定长复制结束后，底层操作仍可继续产生完成通知。

### 部分写入

通用循环在部分写入后返回语义错误时，会调用 `seeker.Seek(int64(nw-nr), io.SeekCurrent)` 回退到第一个尚未写入的字节。`nr` 是读取的字节数，`nw` 是目标端已接收的字节数。如果 `Seek` 失败，优先返回其错误。如果源端未实现 `io.Seeker`，则返回 `ErrNoSeeker`。

使用 `CopyPolicy` 时，如果写入直接返回 `ErrWouldBlock` 或 `ErrMore`，且策略选择 `PolicyRetry`，通用循环会保留尚未写入的字节以供重试。源端不能回退时也可以这样做。委托给 `WriteTo` 或 `ReadFrom` 时，由受委托的方法管理源端位置和恢复。

## 基准测试

`BenchmarkCopyBufferReuse` 比较 `iox.CopyBuffer`、使用 `ReturnPolicy` 的 `iox.CopyBufferPolicy` 和 `io.CopyBuffer` 的通用读写路径。每个用例都复用 32 KiB 的缓冲区，复制相同的内存数据直到 EOF。

```shell
go test -run '^$' -bench '^BenchmarkCopyBufferReuse$' -benchmem -benchtime=200ms -count=10 -cpu=1 -timeout=120s .
```

在同一台机器上使用相同的 Go 版本，在相近负载下分别测试两个版本。按相同的输入大小比较 `ns/op` 和 `allocs/op`。要测量唤醒延迟，请使用实际等待 I/O 就绪的负载。

## 许可证

MIT — 见 [LICENSE](./LICENSE)。

©2025 Hayabusa Cloud Co., Ltd.
