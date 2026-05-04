# iox

Go `io` パッケージ向けのノンブロッキングセマンティクス：would-block と multi-shot を第一級のシグナルとして扱います。

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/iox)](https://goreportcard.com/report/github.com/hayabusa-cloud/iox)
[![codecov](https://codecov.io/gh/hayabusa-cloud/iox/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/iox)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

言語: [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | **日本語** | [Français](./README.fr.md)

## 概要

`iox` はノンブロッキング I/O スタック向けです。そこでは「今は進捗がない」と「今は進捗があるが、操作はまだアクティブ」が**通常の制御フロー**であり、失敗ではありません。

明確な契約を持つ 2 つのセマンティックエラーを導入します。

- `ErrWouldBlock` — **今は進めない**（ready 状態または完了を待つ必要がある）。直ちに返し、次のポーリング後に再試行します。
- `ErrMore` — **進捗があった**うえで操作がアクティブのままです。**後続の完了が続きます**。現在の結果を処理し、ポーリングを継続します。

`iox` は標準 `io` の考え方を保ちます。

- 返る count は常に「転送したバイト数 / 進捗」を意味します
- 返る error が制御フローを決めます（`nil`、失敗ではないセマンティックシグナル、または実際の失敗）
- ヘルパーは `io.Reader` / `io.Writer` と互換で、`io.WriterTo` / `io.ReaderFrom` による高速パスを最適化します

## セマンティクス契約

`iox` セマンティクスを採用する操作では:

| 返る error        | 意味                | 呼び出し側が次にやるべきこと                  |
|-----------------|-------------------|---------------------------------|
| `nil`           | この呼び出し/転送は成功として完了 | 状態マシンを進める                       |
| `ErrWouldBlock` | 今は進めない            | いったん止める; ready 状態または完了を待つ; 再試行  |
| `ErrMore`       | 進捗があった; まだ完了が続く   | いま処理する; 操作をアクティブなまま維持; ポーリングを継続 |
| その他の error      | 失敗                | 適切に処理/ログ/クローズ/バックオフ             |

返る error を解釈する前に、必ず返る count を先に処理してください。count はすでに発生した進捗を表し、error は次の制御アクションを選択します。

補足:
- `iox.Copy` は「部分的に進捗した後に停止した」または「multi-shot の継続を交付する」ことを示すために、`(written > 0, ErrWouldBlock)` や `(written > 0, ErrMore)` を返すことがあります。
- `(0, nil)` の Read は「いまはコピーを止める」として扱い、ヘルパー内に隠れたスピンを作らないために `(written, nil)` を返します。
- `CopyN`、`CopyNBuffer`、およびそれらのポリシー付きバリアントは「ちょうど n バイトをコピーする」有界操作です。`written == n` になった時点で `nil` を返します。サブスクリプションや multi-shot ルートのライフサイクル API ではありません。
- `Outcome` の数値は防御的なエンコードであり、セマンティックな順序ではありません。`<`、`>`、`min`、`max` ではなく、`switch`、`Classify`、述語を使ってください。

### 注意: `iox.Copy` と `(0, nil)` の Read

Go の `io.Reader` 約束では、`Read` が `(0, nil)` を返して「進捗なし」を表すことが許されています（EOF ではありません）。良い Reader は `len(p) == 0` の場合を除き、`(0, nil)` を避けるべきです。

`iox.Copy` は `(0, nil)` を「いまはコピーを止める」と解釈し、`(written, nil)` を返します。これは、ノンブロッキングまたはイベントループのコードでヘルパー内部にスピンを隠さないためです。繰り返しの `(0, nil)` に対して厳密な前進検出が必要なら、その方針は呼び出し側で実装してください。

### 注意: `iox.Copy` と部分書き込みリカバリ

ノンブロッキングな宛先へコピーする際、`dst.Write` がセマンティックエラー（`ErrWouldBlock` または `ErrMore`）を部分書き込み（`nw < nr`）とともに返すことがあります。この場合、バイトは `src` から読み取られましたが、`dst` へ完全には書き込まれていません。

データ損失を防ぐため、`iox.Copy` はソースポインタのロールバックを試みます：
- `src` が `io.Seeker` を実装している場合、Copy は `Seek(nw-nr, io.SeekCurrent)` を呼び出して未書き込みバイトを巻き戻します。
- `src` が `io.Seeker` を実装して**いない**場合、Copy は `ErrNoSeeker` を返し、未書き込みバイトが回復不能であることを示します。

このロールバック保証は、`iox.Copy` が所有する通常の読み書きループに適用されます。標準の `io.WriterTo` または `io.ReaderFrom` 高速パスが選択された場合、その高速パス実装がソース位置の進行と部分書き込みの回復を所有します。実装は `ErrWouldBlock` / `ErrMore` を保持し、戻り値の count で未転送とされたバイトを後続の再試行で安全に扱えるようにしなければなりません。

**推奨事項：**
- ノンブロッキングな宛先へコピーする際は、シーク可能なソース（例: `*os.File`、`*bytes.Reader`）を使用してください。
- シーク不可能なソース（例: ネットワークソケット）の場合、書き込み側のセマンティックエラーに対して `PolicyRetry` を設定した `CopyPolicy` を使用してください。これにより、すべての読み取りバイトが返却前に書き込まれることが保証され、ロールバックの必要がなくなります。

## クイックスタート

`go get` でインストール:

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
	_, _ = iox.CopyN(io.Discard, &dst, 5)                    // "hello" を消費

	n, err = iox.Copy(&dst, src)
	fmt.Printf("n=%d err=%v buf=%q\n", n, err, dst.String()) // n=5  err=io: would block   buf="world"
	_, _ = iox.CopyN(io.Discard, &dst, 5)                    // "world" を消費

	n, err = iox.Copy(&dst, src)
	fmt.Printf("n=%d err=%v buf=%q\n", n, err, dst.String()) // n=3  err=<nil>            buf="iox"
}
```

## 標準的な制御ループ

呼び出し側はまず進捗を処理し、その後でパッケージのヘルパーによって制御を分類してください。

```go
for {
	n, err := op()
	if n > 0 {
		consume(n)
	}

	switch {
	case err == nil:
		return nil
	case iox.IsMore(err):
		continue
	case iox.IsWouldBlock(err):
		wait()
		continue
	default:
		return err
	}
}
```

## API 概要

- エラー
  - `ErrWouldBlock`, `ErrMore`, `ErrNoSeeker`

- コピー
  - `Copy(dst Writer, src Reader) (int64, error)`
  - `CopyBuffer(dst Writer, src Reader, buf []byte) (int64, error)`
  - `CopyN(dst Writer, src Reader, n int64) (int64, error)`
  - `CopyNBuffer(dst Writer, src Reader, n int64, buf []byte) (int64, error)`

- Tee
  - `TeeReader(r Reader, w Writer) Reader`
  - `TeeWriter(primary, tee Writer) Writer`

- アダプター
  - `AsWriterTo(r Reader) Reader`（`iox.Copy` により `io.WriterTo` を追加）
  - `AsReaderFrom(w Writer) Writer`（`iox.Copy` により `io.ReaderFrom` を追加）

- セマンティクス
  - `Outcome`
  - `Classify(err error) Outcome`
  - `IsSemantic(err error) bool`
  - `IsNonFailure(err error) bool`
  - `IsFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

- Backoff
  - `Backoff` — 外部 I/O 待機のための適応型バックオフ
  - `DefaultBackoffBase` (500µs)、`DefaultBackoffMax` (100ms)

## Backoff — 外部 I/O のための適応型待機

`ErrWouldBlock` が進捗不可を示した場合、呼び出し側は再試行前に待機する必要があります。`iox.Backoff` はこの待機のための適応型バックオフ戦略を提供します。

**三層進捗モデル：**

| 層         | メカニズム                       | ユースケース            |
|-----------|-----------------------------|-------------------|
| Strike    | システムコール                     | カーネル直撃            |
| Spin      | ハードウェア yield (`spin`)       | ローカルなアトミック同期      |
| **Adapt** | ソフトウェアバックオフ (`iox.Backoff`) | 外部 I/O の ready 状態 |

**ゼロ値のまますぐ使用可能：**

```go
var b iox.Backoff  // DefaultBackoffBase (500µs) と DefaultBackoffMax (100ms) を使用

for {
	n, err := conn.Read(buf)
	if iox.IsWouldBlock(err) {
		b.Wait() // ジッター付きの適応型待機
		continue
	}
	if err != nil {
		return err
	}
	process(buf[:n])
	b.Reset() // 進捗成功後にリセット
}
```

**アルゴリズム：** ブロックベースの線形スケーリング、±12.5% のジッター付き（待機の集中を防止）。

- ブロック 1: `base` 時間の待機 1 回
- ブロック 2: `2×base` 時間の待機 2 回
- ブロック n: `min(n×base, max)` 時間の待機 n 回

**メソッド：**

- `Wait()` — 現在の時間だけ待機し、次へ進む
- `Reset()` — ブロック 1 に戻す
- `SetBase(d)` / `SetMax(d)` — タイミング設定

## Tee のセマンティクス（count と error）

- `TeeReader` は `n` を `r` から読めたバイト数（ソース側の進捗）として返します。副次書き込みが失敗または短書き込みでも `n` は変わりません。
- `TeeWriter` は `n` を `primary` が受理したバイト数（primary 側の進捗）として返します。tee 書き込みが失敗または短書き込みでも `n` は変わりません。
- `n > 0` のとき、tee アダプターは副次/tee 側由来の `err`（`ErrWouldBlock`/`ErrMore` を含む）とともに `(n, err)` を返すことがあります。まず `p[:n]` を処理してください。
- ポリシー駆動のヘルパーと相性を良くするため、`ErrWouldBlock`/`ErrMore` はそのまま返すことを推奨します（ラップしない）。

## セマンティックポリシー

一部のヘルパーはオプションで `SemanticPolicy` を受け取り、`ErrWouldBlock` や `ErrMore` に遭遇したときにどう振る舞うか（例: すぐ返すか、いったん制御を譲って再試行するか）を決めます。

デフォルトは `nil` です。つまり**非ブロッキングの挙動は保持されます**。ヘルパーは `ErrWouldBlock` / `ErrMore` を呼び出し側へ返し、自身では待機や再試行を行いません。

## 分類と厳密なセンチネルディスパッチ

分類ヘルパー（`IsMore`、`IsWouldBlock`、`IsSemantic`、`IsNonFailure`、`IsFailure`、`Classify`）は `errors.Is` を使うため、ラップされたセマンティックセンチネルも API 境界や診断では正しく分類されます。`iox` のセマンティック制御結果を分類するときは、直接の `errors.Is` 呼び出しよりもこれらのヘルパーを優先してください。

内部の copy / tee 高速パスはポリシー判断をセンチネルの厳密な同一性でディスパッチします。制御下にあるプロデューサーは、そのパスでは `ErrMore` と `ErrWouldBlock` をラップせずに返してください。ラップされたセマンティックエラーはそのまま返され、分類も可能ですが、厳密なセンチネルに基づくポリシー再試行は起動しません。

## 高速パスとセマンティクス保持

`iox.Copy` は利用可能な場合、標準 `io` の高速パスを使います:

- `src` が `io.WriterTo` を実装していれば `WriteTo` を呼びます
- そうでなければ `dst` が `io.ReaderFrom` を実装していれば `ReadFrom` を呼びます
- それ以外は内部の固定サイズバッファ（`32KiB`）と read/write ループを使います

高速パスで `ErrWouldBlock` / `ErrMore` を保持するため、`WriteTo` / `ReadFrom` の実装が適切な場面でそれらを返すようにしてください。高速パスは自身の進捗カウントも正確に保つ必要があります。返す count は実際に転送されたバイト数であり、残りのバイトは後続の再試行で利用可能であるか、実際の終端エラーとして表現されなければなりません。

通常の `io.Reader`/`io.Writer` しか持っていないが、高速パスインターフェースも提供したい、かつセマンティクスも保持したい場合は次を使います:

- `iox.AsWriterTo(r)` により `iox.Copy` 実装の `WriteTo` を追加
- `iox.AsReaderFrom(w)` により `iox.Copy` 実装の `ReadFrom` を追加

## ライセンス

MIT — [LICENSE](./LICENSE) を参照。

©2025 Hayabusa Cloud Co., Ltd.
