# iox

Semántica no bloqueante para el paquete Go `io`: señales de primera clase para would-block y multi-shot.

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/iox)](https://goreportcard.com/report/github.com/hayabusa-cloud/iox)
[![codecov](https://codecov.io/gh/hayabusa-cloud/iox/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/iox)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Idioma: [English](./README.md) | [简体中文](./README.zh-CN.md) | **Español** | [日本語](./README.ja.md) | [Français](./README.fr.md)

## Descripción general

`iox` está diseñado para pilas de E/S no bloqueantes donde “sin progreso ahora” y “progreso ahora, pero la operación sigue activa” son **flujo de control normal**, no fallos.

Introduce dos errores semánticos con contratos explícitos:

- `ErrWouldBlock` — **no es posible progresar ahora** sin esperar señales de disponibilidad o finalización. Devuelve de inmediato; reintenta tras el siguiente sondeo.
- `ErrMore` — **hubo progreso** y la operación sigue activa; **llegarán más eventos**. Procesa el resultado actual y continúa sondeando.

`iox` mantiene intactos los modelos mentales estándar de `io`:

- los recuentos devueltos siempre significan “bytes transferidos / progreso realizado”
- los errores devueltos guían el flujo de control (`nil`, señal semántica de no fallo, o fallo real)
- las funciones auxiliares son compatibles con `io.Reader`, `io.Writer`, y se optimizan con `io.WriterTo` / `io.ReaderFrom`

## Contrato de semántica

Para operaciones que adopten la semántica de `iox`:

| Error devuelto  | Significado                                            | Qué debe hacer el llamador a continuación                       |
|-----------------|--------------------------------------------------------|-----------------------------------------------------------------|
| `nil`           | completado con éxito para esta llamada / transferencia | continúa tu máquina de estados                                  |
| `ErrWouldBlock` | no hay progreso posible ahora                          | deja de intentar; espera disponibilidad/finalización; reintenta |
| `ErrMore`       | hubo progreso; seguirán más finalizaciones             | procesa ahora; mantén la operación activa; continúa el sondeo   |
| otro error      | fallo                                                  | maneja/registra/cierra/aplica retroceso según corresponda       |

Procesa siempre el conteo devuelto antes de interpretar el error devuelto. El conteo informa el progreso ya realizado; el error selecciona la siguiente acción de control.

Notas:
- `iox.Copy` puede devolver `(written > 0, ErrWouldBlock)` o `(written > 0, ErrMore)` para reportar progreso parcial antes de quedar bloqueado o antes de entregar una continuación multi-shot.
- Las lecturas `(0, nil)` se tratan como “detener la copia ahora” y devuelven `(written, nil)` para evitar una espera activa oculta dentro de las funciones auxiliares.
- `CopyN`, `CopyNBuffer` y sus variantes con política son operaciones acotadas de "copiar exactamente n bytes". Cuando `written == n`, devuelven `nil`; no son APIs de ciclo de vida para suscripciones ni rutas multi-shot.
- Los valores numéricos de `Outcome` son codificaciones defensivas, no un orden semántico. Usa `switch`, `Classify` y predicados en vez de `<`, `>`, `min` o `max`.

### Nota: `iox.Copy` y lecturas `(0, nil)`

El contrato de Go `io.Reader` permite que `Read` devuelva `(0, nil)` para indicar “sin progreso”, no fin de flujo. Los lectores bien comportados deberían evitar `(0, nil)` salvo cuando `len(p) == 0`.

`iox.Copy` trata una lectura `(0, nil)` como “detener la copia ahora” y devuelve `(written, nil)`. Esto evita ocultar una espera activa dentro de una función auxiliar en código no bloqueante o de bucle de eventos. Si necesitas detección estricta de progreso hacia delante ante múltiples `(0, nil)`, implementa esa política en el punto de llamada.

### Nota: `iox.Copy` y recuperación de escritura parcial

Al copiar a un destino no bloqueante, `dst.Write` puede devolver un error semántico (`ErrWouldBlock` o `ErrMore`) con una escritura parcial (`nw < nr`). En este caso, los bytes se han leído de `src` pero no se han escrito completamente en `dst`.

Para prevenir la pérdida de datos, `iox.Copy` intenta retroceder el puntero del origen:
- Si `src` implementa `io.Seeker`, Copy llama a `Seek(nw-nr, io.SeekCurrent)` para rebobinar los bytes no escritos.
- Si `src` **no** implementa `io.Seeker`, Copy devuelve `ErrNoSeeker` para señalar que los bytes no escritos son irrecuperables.

Esta garantía de retroceso se aplica al bucle genérico de lectura/escritura que controla `iox.Copy`. Si se selecciona la ruta rápida estándar `io.WriterTo` o `io.ReaderFrom`, esa implementación es responsable de su propio avance de fuente y recuperación de escrituras parciales; debe preservar `ErrWouldBlock` / `ErrMore` y hacer que el reintento sea seguro para los bytes que aún no reportó como transferidos.

**Recomendaciones:**

- Usa fuentes con búsqueda (por ejemplo, `*os.File`, `*bytes.Reader`) al copiar a destinos no bloqueantes.
- Para fuentes sin búsqueda (por ejemplo, sockets de red), usa `CopyPolicy` con `PolicyRetry` para errores semánticos del lado de escritura. Esto garantiza que todos los bytes leídos se escriban antes de devolver, evitando la necesidad de retroceso.

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

## Bucle de control canónico

Los llamadores deben manejar primero el progreso y luego clasificar el control con las funciones auxiliares del paquete:

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

## Resumen de API

- Errores
  - `ErrWouldBlock`, `ErrMore`, `ErrNoSeeker`

- Copia
  - `Copy(dst Writer, src Reader) (int64, error)`
  - `CopyBuffer(dst Writer, src Reader, buf []byte) (int64, error)`
  - `CopyN(dst Writer, src Reader, n int64) (int64, error)`
  - `CopyNBuffer(dst Writer, src Reader, n int64, buf []byte) (int64, error)`

- Tee
  - `TeeReader(r Reader, w Writer) Reader`
  - `TeeWriter(primary, tee Writer) Writer`

- Adaptadores
  - `AsWriterTo(r Reader) Reader` (añade `io.WriterTo` vía `iox.Copy`)
  - `AsReaderFrom(w Writer) Writer` (añade `io.ReaderFrom` vía `iox.Copy`)

- Semántica
  - `Outcome`
  - `Classify(err error) Outcome`
  - `IsSemantic(err error) bool`
  - `IsNonFailure(err error) bool`
  - `IsFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

- Backoff
  - `Backoff` — retroceso adaptativo para espera de E/S externa
  - `DefaultBackoffBase` (500µs), `DefaultBackoffMax` (100ms)

## Backoff — espera adaptativa para E/S externa

Cuando `ErrWouldBlock` señala que no es posible progresar, el llamador debe esperar antes de reintentar. `iox.Backoff` proporciona una estrategia de retroceso adaptativo para esta espera.

**Modelo de progreso de tres niveles:**

| Nivel     | Mecanismo                              | Caso de uso                   |
|-----------|----------------------------------------|-------------------------------|
| Strike    | Llamada al sistema                     | Golpe directo al kernel       |
| Spin      | Cesión de hardware (`spin`)            | Sincronización atómica local  |
| **Adapt** | Retroceso por software (`iox.Backoff`) | Disponibilidad de E/S externa |

**Valor cero listo para usar:**

```go
var b iox.Backoff  // usa DefaultBackoffBase (500µs) y DefaultBackoffMax (100ms)

for {
	n, err := conn.Read(buf)
	if iox.IsWouldBlock(err) {
		b.Wait() // espera adaptativa con jitter
		continue
	}
	if err != nil {
		return err
	}
	process(buf[:n])
	b.Reset() // restablecer tras progreso exitoso
}
```

**Algoritmo:** Escalado lineal basado en bloques con ±12.5% de jitter para prevenir efectos de estampida.

- Bloque 1: 1 espera de `base`
- Bloque 2: 2 esperas de `2×base`
- Bloque n: n esperas de `min(n×base, max)`

**Métodos:**

- `Wait()` — espera durante la duración actual y luego avanza
- `Reset()` — restaura al bloque 1
- `SetBase(d)` / `SetMax(d)` — configurar tiempos

## Semántica de Tee (conteos y errores)

- `TeeReader` devuelve `n` como el número de bytes leídos desde `r` (progreso del origen), incluso si la escritura secundaria falla o es corta.
- `TeeWriter` devuelve `n` como el número de bytes aceptados por `primary` (progreso del escritor principal), incluso si la escritura al tee falla o es corta.
- Cuando `n > 0`, un adaptador Tee puede devolver `(n, err)` donde `err` viene de la escritura secundaria/tee (incluyendo `ErrWouldBlock`/`ErrMore`). Procesa primero `p[:n]`.
- Para la mejor interoperabilidad con funciones auxiliares basadas en política, devuelve `ErrWouldBlock`/`ErrMore` tal cual (evita envolverlos).

## Política semántica

Algunas funciones auxiliares aceptan opcionalmente un `SemanticPolicy` para decidir qué hacer cuando encuentran `ErrWouldBlock` o `ErrMore` (por ejemplo, devolver inmediatamente o ceder y reintentar).

El valor por defecto es `nil`, lo que significa que se **preserva el comportamiento no bloqueante**: la función auxiliar devuelve `ErrWouldBlock` / `ErrMore` al llamador y no espera ni reintenta por su cuenta.

## Clasificación y despacho por centinela exacto

Las funciones auxiliares de clasificación (`IsMore`, `IsWouldBlock`, `IsSemantic`, `IsNonFailure`, `IsFailure` y `Classify`) usan `errors.Is`, por lo que los centinelas semánticos envueltos se clasifican correctamente en límites de API y diagnóstico. Prefiere estas funciones auxiliares a llamadas directas a `errors.Is` al clasificar resultados de control semántico de `iox`.

Las rutas rápidas internas de copy y tee despachan decisiones de política por identidad exacta del centinela. Los productores bajo tu control deberían devolver `ErrMore` y `ErrWouldBlock` sin envolver en esas rutas. Un error semántico envuelto se devuelve y sigue siendo clasificable, pero no activa el reintento de política basado en centinelas exactos.

## Rutas rápidas y preservación de semántica

`iox.Copy` usa las rutas rápidas estándar de `io` cuando están disponibles:

- si `src` implementa `io.WriterTo`, `iox.Copy` llama a `WriteTo`
- de lo contrario, si `dst` implementa `io.ReaderFrom`, `iox.Copy` llama a `ReadFrom`
- si no, usa un buffer interno de tamaño fijo (`32KiB`) y un bucle de lectura/escritura

Para preservar `ErrWouldBlock` / `ErrMore` en rutas rápidas, asegúrate de que tus implementaciones de `WriteTo` / `ReadFrom` devuelvan esos errores cuando corresponda. Las rutas rápidas también deben mantener una contabilidad de progreso correcta: el recuento devuelto es el número de bytes realmente transferidos, y los bytes restantes deben seguir disponibles para un reintento posterior o estar representados por un error terminal real.

Si tienes un `io.Reader`/`io.Writer` normal pero quieres exponer las interfaces de ruta rápida *y* preservar la semántica, envuelve con:

- `iox.AsWriterTo(r)` para añadir un `WriteTo` implementado vía `iox.Copy`
- `iox.AsReaderFrom(w)` para añadir un `ReadFrom` implementado vía `iox.Copy`

## Licencia

MIT — ver [LICENSE](./LICENSE).

©2025 Hayabusa Cloud Co., Ltd.
