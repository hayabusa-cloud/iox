# iox

Go `io` パッケージ向けのノンブロッキングセマンティック：would-block と multi-shot を第一級のシグナルとして扱います。

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/iox)](https://goreportcard.com/report/github.com/hayabusa-cloud/iox)
[![Coverage Status](https://coveralls.io/repos/github/hayabusa-cloud/iox/badge.svg?branch=main)](https://coveralls.io/github/hayabusa-cloud/iox?branch=main)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

言語: [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | **日本語** | [Français](./README.fr.md)

## このパッケージは何ですか？

`iox` はノンブロッキング I/O スタック向けです。そこでは「今は進捗がない」と「今は進捗があるが、操作はまだアクティブ」が**通常の制御フロー**であり、失敗ではありません。

明確な約束を持つ 2 つの semantic error を導入します。

- `ErrWouldBlock` — **今は進めない**（readiness/completion を待つ必要がある）。直ちに return し、次の polling 後に再試行します。
- `ErrMore` — **進捗があった** かつ操作がアクティブのまま。**後続の completion が続く**。現在の結果を処理し、polling を継続します。

`iox` は標準 `io` の心的モデルを保ちます。

- 返る count は常に「転送したバイト数 / 進捗」を意味します
- 返る error が制御フローを決めます（`nil`、semantic non-failure、または実際の failure）
- helper は `io.Reader` / `io.Writer` と互換で、`io.WriterTo` / `io.ReaderFrom` による fast path を最適化します

## セマンティクス約束

`iox` セマンティクスを採用する操作では:

| 返る error | 意味 | 呼び出し側が次にやるべきこと |
|---|---|---|
| `nil` | この呼び出し/転送は成功として完了 | 状態機械を進める |
| `ErrWouldBlock` | 今は進めない | いったん止める; readiness/completion を待つ; 再試行 |
| `ErrMore` | 進捗があった; まだ completion が続く | いま処理する; 操作を active のまま維持; polling を継続 |
| その他の error | failure | 適切に処理/ログ/クローズ/バックオフ |

### 注意: `iox.Copy` と `(0, nil)` の Read

Go の `io.Reader` 約束では、`Read` が `(0, nil)` を返して「進捗なし」を表すことが許されています（EOF ではありません）。
良い Reader は `len(p) == 0` の場合を除き、`(0, nil)` を避けるべきです。

`iox.Copy` は `(0, nil)` を「いまはコピーを止める」と解釈し、`(written, nil)` を返します。
これは、ノンブロッキング/イベントループコードで helper の内部にスピンを隠さないためです。
繰り返しの `(0, nil)` に対して厳密な forward progress の検出が必要なら、その方針は呼び出し側で実装してください。

## クイックスタート

`go get` でインストール:

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

## API 概要

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
  - `AsWriterTo(r Reader) Reader`（`iox.Copy` により `io.WriterTo` を追加）
  - `AsReaderFrom(w Writer) Writer`（`iox.Copy` により `io.ReaderFrom` を追加）

- Semantics
  - `IsNonFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

## fast path とセマンティクス保持

`iox.Copy` は利用可能な場合、標準 `io` の fast path を使います:

- `src` が `io.WriterTo` を実装していれば `WriteTo` を呼びます
- そうでなければ `dst` が `io.ReaderFrom` を実装していれば `ReadFrom` を呼びます
- それ以外は固定サイズのスタックバッファ（`32KiB`）と read/write ループを使います

fast path で `ErrWouldBlock` / `ErrMore` を保持するため、`WriteTo` / `ReadFrom` の実装が適切な場面でそれらを返すようにしてください。

通常の `io.Reader`/`io.Writer` しか持っていないが、fast-path インターフェースも提供したい、かつセマンティクスも保持したい場合は次を使います:

- `iox.AsWriterTo(r)` により `iox.Copy` 実装の `WriteTo` を追加
- `iox.AsReaderFrom(w)` により `iox.Copy` 実装の `ReadFrom` を追加

## License

MIT — [LICENSE](./LICENSE) を参照。

©2025 Hayabusa Cloud Co., Ltd.
