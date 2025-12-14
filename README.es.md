# iox

Semántica no bloqueante para el paquete Go `io` : señales de primera clase para would-block y multi-shot.

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/iox)](https://goreportcard.com/report/github.com/hayabusa-cloud/iox)
[![Coverage Status](https://coveralls.io/repos/github/hayabusa-cloud/iox/badge.svg?branch=main)](https://coveralls.io/github/hayabusa-cloud/iox?branch=main)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Idioma: [English](./README.md) | [简体中文](./README.zh-CN.md) | **Español** | [日本語](./README.ja.md) | [Français](./README.fr.md)

## ¿Qué es este paquete?

`iox` es para stacks de I/O no bloqueantes donde “sin progreso ahora” y “progreso ahora, pero la operación sigue activa” son **flujo de control normal**, no fallos.

Introduce dos errores semánticos con contratos explícitos:

- `ErrWouldBlock` — **no es posible progresar ahora** sin esperar a readiness/completions. Devuelve de inmediato; reintenta tras tu siguiente polling.
- `ErrMore` — **hubo progreso** y la operación sigue activa; **llegarán más eventos**. Procesa el resultado actual y sigue haciendo polling.

`iox` mantiene intactos los modelos mentales estándar de `io`:

- los conteos devueltos siempre significan “bytes transferidos / progreso realizado”
- los errores devueltos guían el flujo de control (`nil`, no-fallo semántico, o fallo real)
- los helpers son compatibles con `io.Reader`, `io.Writer`, y optimizan con `io.WriterTo` / `io.ReaderFrom`

## Contrato de semántica

Para operaciones que adopten la semántica de `iox`:

| Error devuelto | Significado | Qué debe hacer el llamador a continuación |
|---|---|---|
| `nil` | completado con éxito para esta llamada / transferencia | continúa tu máquina de estados |
| `ErrWouldBlock` | no hay progreso posible ahora | deja de intentar; espera readiness/completion; reintenta |
| `ErrMore` | hubo progreso; seguirán más completions | procesa ahora; mantén la operación activa; continúa el polling |
| otro error | fallo | maneja/registro/cierra/backoff según corresponda |

Notas:
- `iox.Copy` puede devolver `(written > 0, ErrWouldBlock)` o `(written > 0, ErrMore)` para reportar progreso parcial antes de quedar bloqueado o antes de entregar una continuación multi-shot.
- Las lecturas `(0, nil)` se tratan como “detener la copia ahora” y devuelven `(written, nil)` para evitar ocultar spinning dentro de helpers.

### Nota: `iox.Copy` y lecturas `(0, nil)`

El contrato de Go `io.Reader` permite que `Read` devuelva `(0, nil)` para indicar “sin progreso”, no fin de stream.
Los Readers bien comportados deberían evitar `(0, nil)` salvo cuando `len(p) == 0`.

`iox.Copy` trata una lectura `(0, nil)` como “detener la copia ahora” y devuelve `(written, nil)`.
Esto evita ocultar el spinning dentro de un helper en código no bloqueante / de event-loop.
Si necesitas detección estricta de progreso hacia delante ante múltiples `(0, nil)`, implementa esa política en tu call site.

## Inicio rápido

Instala con `go get`:

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

## Resumen de API

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
  - `AsWriterTo(r Reader) Reader` (añade `io.WriterTo` vía `iox.Copy`)
  - `AsReaderFrom(w Writer) Writer` (añade `io.ReaderFrom` vía `iox.Copy`)

- Semantics
  - `IsNonFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

## Semántica de Tee (conteos y errores)

- `TeeReader` devuelve `n` como el número de bytes leídos desde `r` (progreso del origen), incluso si la escritura al side falla/es corta.
- `TeeWriter` devuelve `n` como el número de bytes aceptados por `primary` (progreso del primary), incluso si la escritura al tee falla/es corta.
- Cuando `n > 0`, un adaptador Tee puede devolver `(n, err)` donde `err` viene del side/tee (incluyendo `ErrWouldBlock`/`ErrMore`). Procesa primero `p[:n]`.
- Para la mejor interoperabilidad con helpers basados en policy, devuelve `ErrWouldBlock`/`ErrMore` tal cual (evita envolverlos).

## Política semántica

Algunos helpers aceptan opcionalmente un `SemanticPolicy` para decidir qué hacer cuando encuentran `ErrWouldBlock` o `ErrMore`
(por ejemplo, devolver inmediatamente frente a ceder/yield y reintentar).

El valor por defecto es `nil`, lo que significa que se **preserva el comportamiento no bloqueante**: el helper devuelve
`ErrWouldBlock` / `ErrMore` al llamador y no espera ni reintenta por su cuenta.

## Fast paths y preservación de semántica

`iox.Copy` usa los fast paths estándar de `io` cuando están disponibles:

- si `src` implementa `io.WriterTo`, `iox.Copy` llama a `WriteTo`
- si no, si `dst` implementa `io.ReaderFrom`, `iox.Copy` llama a `ReadFrom`
- si no, usa un buffer fijo en la pila (`32KiB`) y un bucle de lectura/escritura

Para preservar `ErrWouldBlock` / `ErrMore` en fast paths, asegúrate de que tus implementaciones de `WriteTo` / `ReadFrom` devuelvan esos errores cuando corresponda.

Si tienes un `io.Reader`/`io.Writer` normal pero quieres que existan las interfaces de fast path *y* preservar la semántica, envuelve con:

- `iox.AsWriterTo(r)` para añadir un `WriteTo` implementado vía `iox.Copy`
- `iox.AsReaderFrom(w)` para añadir un `ReadFrom` implementado vía `iox.Copy`

## Licencia

MIT — ver [LICENSE](./LICENSE).

©2025 Hayabusa Cloud Co., Ltd.
