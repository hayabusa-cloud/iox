# iox

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Benchmarks](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/iox/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/iox)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Semántica no bloqueante para el paquete Go `io`: señales de primera clase para would-block y multi-shot.

Idioma: [English](./README.md) | [简体中文](./README.zh-CN.md) | **Español** | [日本語](./README.ja.md) | [Français](./README.fr.md)

## Descripción general

`iox` está diseñado para pilas de E/S no bloqueantes donde “seguir avanzando requiere disponibilidad” y “la operación sigue activa” son **flujo de control normal**, no fallos.

Introduce dos errores semánticos con contratos explícitos:

- `ErrWouldBlock` — **no es posible seguir avanzando** sin esperar señales de disponibilidad o finalización. Devuelve de inmediato; reintenta tras el siguiente sondeo.
- `ErrMore` — **la operación sigue activa** y se esperan observaciones posteriores. El conteo puede ser cero; procesa los datos devueltos y continúa sondeando.

`iox` mantiene intactos los modelos mentales estándar de `io`:

- los recuentos devueltos siempre significan “bytes transferidos / progreso realizado”
- los errores devueltos guían el flujo de control (`nil`, señal semántica de no fallo, o fallo real)
- las funciones auxiliares son compatibles con `io.Reader`, `io.Writer`, y se optimizan con `io.WriterTo` / `io.ReaderFrom`

## Contrato de semántica

Para operaciones que adopten la semántica de `iox`:

| Error devuelto  | Significado                                            | Qué debe hacer el llamador a continuación                       |
|-----------------|--------------------------------------------------------|-----------------------------------------------------------------|
| `nil`           | completado con éxito para esta llamada / transferencia | continúa tu máquina de estados                                  |
| `ErrWouldBlock` | seguir avanzando requiere disponibilidad | deja de intentar; espera disponibilidad/finalización; reintenta |
| `ErrMore`       | la operación sigue activa; el conteo puede ser cero | procesa los datos devueltos; mantén la operación activa; continúa el sondeo |
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

## Instalación y ejemplo

Este módulo requiere Go 1.27 o posterior.

```shell
go get code.hybscloud.com/iox
```

La segunda llamada a `Copy` transfiere `"world"` antes de que el lector devuelva `ErrWouldBlock`. Quien llama consume esos cinco bytes con `CopyN` y luego usa su función `waitReadable` para esperar hasta que se pueda volver a leer del origen. [Ejemplo de espera](examples/wouldblock.go)

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

if iox.IsMore(err) {
	n, err = iox.Copy(&dst, src)
	fmt.Printf("%d %v %q\n", n, err, dst.String()) // 5 io: would block "world"
	_, _ = iox.CopyN(io.Discard, &dst, n)
}

if iox.IsWouldBlock(err) {
	waitReadable()
	n, err = iox.Copy(&dst, src)
	fmt.Printf("%d %v %q\n", n, err, dst.String()) // 3 <nil> "iox"
}
return err
```

El lector conserva los bytes que no caben en `p`. Una lectura de longitud cero deja el bloque intacto. El error se devuelve con el último byte del bloque o por sí solo si el bloque está vacío.

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

## E/S reanudable

`Step.Next` devuelve un valor, un error y un `Wait`. Si `Wait` tiene su valor cero, el paso ha terminado. `Finish` proporciona el resultado final. `Await` describe la espera de un valor de un tipo concreto y la continuación que devuelve el siguiente paso.

Llama a `Wait.Arm` con la función que usa el ejecutor para encolar trabajo. La fuente puede emitir una señal antes de que `Arm` regrese. La primera llamada aceptada a `Signal.Deliver` encola un `Ready`. Un trabajador puede reclamarlo una sola vez con `TryResume` para ejecutar la continuación. Si `Deliver` devuelve `false`, la fuente conserva el valor. [Ejemplo de finalización tipada](example_await_test.go)

`WriteStep` registra el número de bytes y el error de cada escritura. Si el escritor acepta solo un prefijo y devuelve `ErrWouldBlock`, una señal permite intentar escribir los bytes restantes en el mismo destino. Si acepta todo el búfer, el paso termina con el error devuelto, incluido `ErrWouldBlock`. Mantenga el búfer válido y sin modificar hasta que termine la operación o se retire la espera con éxito.

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
var b iox.Backoff // usa DefaultBackoffBase (500µs) y DefaultBackoffMax (100ms)

for {
	n, err := conn.Read(buf)
	if n > 0 {
		process(buf[:n])
		b.Reset() // restablecer tras progreso exitoso
	}
	if iox.IsWouldBlock(err) {
		b.Wait() // espera adaptativa con jitter
		continue
	}
	if err != nil || n == 0 {
		return err
	}
}
```

Procesa los bytes devueltos antes de esperar o retornar. Este bucle propaga `ErrMore` sin cambios y devuelve el control con `(0, nil)` sin interpretarlo como EOF.

El bloque `n` contiene `n` intervalos de reintento. `Duration()` devuelve el intervalo nominal, `min(n × base, max)`. Cada intervalo puede variar hasta un 12,5 % respecto al nominal y superar `max`.

- `NextDelay()` avanza un intervalo y devuelve su duración para programar el temporizador de reintento.
- `Wait()` llama a `NextDelay()` y suspende la goroutine que hace la llamada durante ese intervalo.
- `Reset()` vuelve al bloque 1 y conserva la duración base y el límite.
- `SetBase(d)` y `SetMax(d)` aceptan duraciones positivas y conservan el bloque actual.

## Semántica de Tee (conteos y errores)

- `TeeReader` devuelve `n` como el número de bytes leídos desde `r` (progreso del origen), incluso si la escritura secundaria falla o es corta.
- `TeeWriter` devuelve `n` como el número de bytes aceptados por `primary` (progreso del escritor principal), incluso si la escritura al tee falla o es corta.
- Cuando `n > 0`, un adaptador Tee puede devolver `(n, err)` donde `err` viene de la escritura secundaria/tee (incluyendo `ErrWouldBlock`/`ErrMore`). Procesa primero `p[:n]`.
- Para la mejor interoperabilidad con funciones auxiliares basadas en política, devuelve `ErrWouldBlock`/`ErrMore` tal cual (evita envolverlos).

## Política semántica

Algunas funciones auxiliares aceptan opcionalmente un `SemanticPolicy` para decidir qué hacer cuando encuentran `ErrWouldBlock` o `ErrMore` (por ejemplo, devolver inmediatamente o ceder y reintentar).

Con la política predeterminada (`nil`), quien llama se encarga de esperar y reintentar. Si `n > 0`, `CopyNPolicy` y `CopyNBufferPolicy` devuelven `(n, nil)` en cuanto el destino ha aceptado los `n` bytes. Una señal recibida con el último byte no activa las funciones de la política.

## Clasificación y despacho por centinela exacto

Las funciones auxiliares de clasificación (`IsMore`, `IsWouldBlock`, `IsSemantic`, `IsNonFailure`, `IsFailure` y `Classify`) usan `errors.Is`, por lo que los centinelas semánticos envueltos se clasifican correctamente en límites de API y diagnóstico. Prefiere estas funciones auxiliares a llamadas directas a `errors.Is` al clasificar resultados de control semántico de `iox`.

`IsProgress` es true solo cuando `Classify` selecciona `OutcomeOK` u `OutcomeMore`. Si un error coincide con ambos centinelas semánticos, `ErrWouldBlock` tiene prioridad e `IsProgress` es false. `IsMore` e `IsWouldBlock` comprueban la coincidencia con cada centinela, por lo que ambos pueden ser true para el mismo error. Estos predicados no miden el progreso en bytes; contabiliza siempre los conteos devueltos por separado.

Las funciones de copia y tee solo consultan la política cuando reciben directamente los centinelas `ErrMore` o `ErrWouldBlock`. Devuélvelos sin envolver para que la política pueda procesarlos.

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
