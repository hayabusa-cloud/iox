# iox

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Benchmarks](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/iox/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/iox)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Go `io` パッケージ向けのノンブロッキングセマンティクス：would-block と multi-shot を第一級のシグナルとして扱います。

言語: [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | **日本語** | [Français](./README.fr.md)

## 概要

`iox` はノンブロッキング I/O スタック向けのパッケージです。「I/O の準備完了を待ってから再試行する」（`ErrWouldBlock`）と「同じ操作の観測を続ける」（`ErrMore`）を通常の制御フローとして扱います。

コピーや分岐（tee）用の関数は、Go 標準の `io.Reader` と `io.Writer` インターフェースを使います。おなじみの戻り値 `(n, err)` では、`n` は転送済みのバイト数を表し、`err` は呼び出し側の次の動作を示します。どちらのシグナルでも `n` はゼロまたは正の値を取るので、まず `n` バイト分の進捗を反映します。

## I/O の結果

| エラー | `Classify` の結果 | 呼び出し側の処理 |
|---|---|---|
| `nil` | `OutcomeOK` | 転送を続けるか終了する |
| `ErrWouldBlock` | `OutcomeWouldBlock` | I/O の準備完了を待って再試行する |
| `ErrMore` | `OutcomeMore` | 操作を維持し、次の完了を受け取る |
| その他のエラー | `OutcomeFailure` | 転送済みのバイト数を考慮してエラーを処理する |

`Read` を直接呼び出した場合、`io.EOF` は入力の終端を示します。最後に返されたバイトを処理してから読み取りを終了します。`io.EOF` を `Classify` に渡すと `OutcomeFailure` が返ります。

## インストールと使用例

このモジュールには Go 1.27 以降が必要です。

```shell
go get code.hybscloud.com/iox
```

2 回目の `Copy` は `"world"` を転送した後、`ErrWouldBlock` を返します。呼び出し側は `CopyN` でこの 5 バイトを消費してから、自身の `waitReadable` 関数で再び読み取り可能になるまで待ちます。[読み取りの再試行例](examples/wouldblock.go)

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

`p` に収まらなかったデータは次の `Read` に持ち越します。長さが 0 のバッファを渡すと、読み取り位置は進みません。エラーはチャンクの最後のバイトと一緒に返し、空のチャンクではエラーだけを返します。

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

## API 概要

- エラー
  - `ErrWouldBlock`, `ErrMore`, `ErrNoSeeker`

- コピー
  - `Copy(dst Writer, src Reader) (int64, error)`
  - `CopyBuffer(dst Writer, src Reader, buf []byte) (int64, error)`
  - `CopyN(dst Writer, src Reader, n int64) (int64, error)`
  - `CopyNBuffer(dst Writer, src Reader, n int64, buf []byte) (int64, error)`
  - `CopyPolicy`、`CopyBufferPolicy`、`CopyNPolicy`、`CopyNBufferPolicy` は最後の引数に `SemanticPolicy` を取ります。

- Tee
  - `TeeReader(r Reader, w Writer) Reader`
  - `TeeWriter(primary, tee Writer) Writer`
  - `TeeReaderPolicy` と `TeeWriterPolicy` も最後の引数に `SemanticPolicy` を取ります。

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

## 再開可能な I/O

`Step.Next` は値、エラー、`Wait` を返します。`Wait` がゼロ値なら、そのステップは完了しています。`Finish` は完了時の結果を返すステップを作ります。`Await` には、指定した型の値を待つ条件と、その値から次のステップを返す継続を渡します。

`Wait.Arm` には、実行側のキューに処理を追加する関数を渡します。`Arm` が戻る前にシグナルが届くこともあります。最初に受理された `Signal.Deliver` が `Ready` を 1 件キューに入れます。ワーカーは `TryResume` で一度だけその処理を引き受け、継続を実行できます。`Deliver` が `false` を返した場合、値は引き続き送信元が所有します。 [型付きの値を受け取る例](example_await_test.go)

`WriteStep` は各書き込みのバイト数とエラーを記録します。先頭の一部だけが受理されて `ErrWouldBlock` が返ると、通知を受けた後で残りを同じ書き込み先に 1 回書き込めます。全バイトが受理された場合は、`ErrWouldBlock` を含め、返されたエラーをそのまま結果として終了します。処理が完了するか待機の取り消しに成功するまで、バッファを有効なまま保持し、変更しないでください。

## Backoff — 外部 I/O のための適応型待機

準備完了通知を利用できる場合は、`ErrWouldBlock` を受け取ったらその通知を待ちます。通知がない場合は、`iox.Backoff.Wait` で呼び出し元の goroutine を休止させてから再試行できます。

**三層進捗モデル：**

| 層         | メカニズム                       | ユースケース            |
|-----------|-----------------------------|-------------------|
| Strike    | システムコール                     | カーネル直撃            |
| Spin      | ハードウェア yield (`spin`)       | ローカルなアトミック同期      |
| **Adapt** | ソフトウェアバックオフ (`iox.Backoff`) | 外部 I/O の ready 状態 |

**ゼロ値のまますぐ使用可能：**

```go
var b iox.Backoff // DefaultBackoffBase (500µs) と DefaultBackoffMax (100ms) を使用

for {
	n, err := conn.Read(buf)
	if n > 0 {
		process(buf[:n])
		b.Reset() // 進捗成功後にリセット
	}
	if iox.IsWouldBlock(err) {
		b.Wait() // ジッター付きの適応型待機
		continue
	}
	if err != nil || n == 0 {
		return err
	}
}
```

読み取ったバイトを先に処理します。エラーが `ErrWouldBlock` に一致すれば、`ErrMore` にも一致する場合を含め、待機してから再試行します。それ以外のエラーはそのまま返します。読み取りが `(0, nil)` を返すと呼び出し側に制御を戻します。この結果は入力の終端に達する前にも起こり得ます。

ブロック `n` では、`n` 回の再試行に同じ基準時間 `min(n × base, max)` を使います。`Duration()` はこの基準時間を返します。各回の間隔は基準時間から最大 12.5% 変動し、`max` を超えることがあります。

- `NextDelay()` はスケジュールを 1 回進め、再試行タイマーに設定する間隔を返します。
- `Wait()` は `NextDelay()` を呼び、その間隔だけ呼び出し元のゴルーチンを休止します。
- `Reset()` は基準時間と上限の設定を保ったままブロック 1 に戻します。
- `SetBase(d)` と `SetMax(d)` は正の時間を受け取り、現在のブロックを維持します。

## Tee のセマンティクス（count と error）

- `TeeReader` は `n` を `r` から読めたバイト数（ソース側の進捗）として返します。副次書き込みが失敗または短書き込みでも `n` は変わりません。
- `TeeWriter` は `n` を `primary` が受理したバイト数（primary 側の進捗）として返します。tee 書き込みが失敗または短書き込みでも `n` は変わりません。
- `n > 0` のとき、tee アダプターは副次/tee 側由来の `err`（`ErrWouldBlock`/`ErrMore` を含む）とともに `(n, err)` を返すことがあります。まず `p[:n]` を処理してください。
- ポリシー駆動のヘルパーと相性を良くするため、`ErrWouldBlock`/`ErrMore` はそのまま返すことを推奨します（ラップしない）。

## セマンティックポリシー

一部のヘルパーはオプションで `SemanticPolicy` を受け取り、`ErrWouldBlock` や `ErrMore` に遭遇したときにどう振る舞うか（例: すぐ返すか、いったん制御を譲って再試行するか）を決めます。

デフォルトの `nil` ポリシーでは、呼び出し側が待機と再試行を担います。`n > 0` のとき、`CopyNPolicy` と `CopyNBufferPolicy` は書き込み先が合計 `n` バイトを受け取った時点で `(n, nil)` を返します。最後のバイトとともに返されたシグナルについては、ポリシーのコールバックを呼びません。

## 分類と厳密なセンチネルディスパッチ

分類ヘルパー（`IsMore`、`IsWouldBlock`、`IsSemantic`、`IsNonFailure`、`IsFailure`、`Classify`）は `errors.Is` を使うため、ラップされたセマンティックセンチネルも API 境界や診断では正しく分類されます。`iox` のセマンティック制御結果を分類するときは、直接の `errors.Is` 呼び出しよりもこれらのヘルパーを優先してください。

`IsProgress` は `Classify` が `OutcomeOK` または `OutcomeMore` を選ぶ場合にのみ true を返します。同じ error が両方のセマンティックセンチネルに一致する場合は `ErrWouldBlock` が優先され、`IsProgress` は false になります。`IsMore` と `IsWouldBlock` はそれぞれのセンチネルへの一致を調べるため、両方が true になることがあります。これらの述語はバイト数の進捗を測りません。返された count は常に別途処理してください。

コピーや tee のヘルパーがポリシーに判断を委ねるのは、`ErrMore` または `ErrWouldBlock` を直接受け取った場合だけです。ポリシーで処理したいときは、これらのセンチネルをラップせずに返してください。

## 高速パスとセマンティクス保持

`Copy` はまず読み取り元の `io.WriterTo` を調べ、次に書き込み先の `io.ReaderFrom` を調べます。汎用ループは指定されたバッファを使い、省略時には 32 KiB のバッファを使います。`CopyN` は読み取り量を残りのバイト数に制限します。書き込み先の `ReadFrom` は使えますが、元の読み取り元の `WriteTo` は呼び出しません。

委譲先のメソッドは、転送したバイト数の合計とエラーを返します。ポリシーでシグナルを処理するには、`ErrWouldBlock` または `ErrMore` をそのまま返します。再試行に対応するメソッドは、未書き込みのバイトを保持する必要があります。`YieldOnWriteWouldBlockPolicy` は、読み取り時に発生したシグナルでも、どちらかのメソッドが `ErrWouldBlock` を返せば再試行します。

通常の `io.Reader`/`io.Writer` しか持っていないが、高速パスインターフェースも提供したい、かつセマンティクスも保持したい場合は次を使います:

- `iox.AsWriterTo(r)` により `iox.Copy` 実装の `WriteTo` を追加
- `iox.AsReaderFrom(w)` により `iox.Copy` 実装の `ReadFrom` を追加

### 空読みとバイト数を指定したコピー

汎用の読み書きループでは、読み取りが `(0, nil)` を返すと `Copy` は停止して `(written, nil)` を返します。入力の終端に達する前でも、この結果は起こり得ます。`WriteTo` や `ReadFrom` に委譲した場合、読み取りは委譲先が制御します。

`n` が正の場合、`CopyN` とその派生関数は、書き込み先が合計 `n` バイトを受け取った時点で `(n, nil)` を返します。それより早く EOF に達した場合や、コピーが `nil` で終了してもバイト数が不足している場合は `io.ErrUnexpectedEOF` を返します。指定量のコピーが完了した後も、元のマルチショット操作は継続できます。

### 部分書き込み

汎用ループが部分書き込みでセマンティックエラーを返す際は、`seeker.Seek(int64(nw-nr), io.SeekCurrent)` で未書き込み部分の先頭まで戻ります。`nr` は読み取ったバイト数、`nw` は書き込み先が受け取ったバイト数です。`Seek` が失敗した場合はそのエラーを優先し、読み取り元が `io.Seeker` を実装していなければ `ErrNoSeeker` を返します。

`CopyPolicy` で書き込みが `ErrWouldBlock` または `ErrMore` を直接返し、ポリシーが `PolicyRetry` を選ぶと、未書き込みのバイトを保持して再試行します。シークできない読み取り元でも使えます。`WriteTo` や `ReadFrom` に委譲した場合は、委譲先が読み取り位置と再試行を管理します。

## ライセンス

MIT — [LICENSE](./LICENSE) を参照。

©2025 Hayabusa Cloud Co., Ltd.
