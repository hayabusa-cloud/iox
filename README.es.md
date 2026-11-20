# iox

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Benchmarks](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/iox/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/iox)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Semántica no bloqueante para el paquete Go `io`: señales de primera clase para would-block y multi-shot.

Idioma: [English](./README.md) | [简体中文](./README.zh-CN.md) | **Español** | [日本語](./README.ja.md) | [Français](./README.fr.md)

## Descripción general

`iox` está pensado para pilas de E/S no bloqueante en las que «espera a que el recurso esté listo antes de reintentar» (`ErrWouldBlock`) y «sigue observando la misma operación» (`ErrMore`) forman parte del flujo de control normal.

Sus funciones de copia y duplicación de datos (tee) usan las interfaces estándar `io.Reader` e `io.Writer` de Go. En el par habitual `(n, err)`, `n` indica cuántos bytes se han transferido y `err` guía el siguiente paso. Con cualquiera de las dos señales, `n` puede ser cero o positivo, así que contabiliza primero los bytes indicados por `n`.

## Resultados de E/S

| Error | Resultado de `Classify` | Acción |
|---|---|---|
| `nil` | `OutcomeOK` | Continúa o termina la transferencia |
| `ErrWouldBlock` | `OutcomeWouldBlock` | Espera a que el recurso esté listo antes de reintentar |
| `ErrMore` | `OutcomeMore` | Mantén la operación activa y espera la siguiente finalización |
| Otro error | `OutcomeFailure` | Gestiona el error teniendo en cuenta los bytes ya transferidos |

En una llamada directa a `Read`, `io.EOF` indica el final de la entrada. Procesa los últimos bytes antes de terminar. Si pasas `io.EOF` a `Classify`, obtienes `OutcomeFailure`.

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
  - `CopyPolicy`, `CopyBufferPolicy`, `CopyNPolicy` y `CopyNBufferPolicy` reciben un último argumento de tipo `SemanticPolicy`.

- Tee
  - `TeeReader(r Reader, w Writer) Reader`
  - `TeeWriter(primary, tee Writer) Writer`
  - `TeeReaderPolicy` y `TeeWriterPolicy` también reciben un último argumento de tipo `SemanticPolicy`.

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

Si puedes recibir una notificación de disponibilidad, espérala tras `ErrWouldBlock`. En caso contrario, `iox.Backoff.Wait` suspende la goroutine entre intentos.

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

Procesa primero los bytes leídos. Si el error coincide con `ErrWouldBlock`, el bucle espera antes de reintentar, incluso si también coincide con `ErrMore`. Devuelve cualquier otro error tal como lo recibió. Una lectura que devuelve `(0, nil)` cede el control a quien llama y puede ocurrir antes del fin de la entrada.

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

`Copy` da prioridad a `io.WriterTo` del origen y después a `io.ReaderFrom` del destino. El bucle genérico usa el búfer suministrado o uno de 32 KiB por defecto. `CopyN` limita las lecturas al número de bytes pendientes. Puede usar `ReadFrom` del destino y omite `WriteTo` del origen original.

El método delegado devuelve el total de bytes transferidos y un error. Devuelve `ErrWouldBlock` o `ErrMore` sin envolver para que la política pueda gestionar la señal. Si el método admite reintentos, debe conservar los bytes pendientes de escritura. `YieldOnWriteWouldBlockPolicy` reintenta cuando cualquiera de los dos métodos devuelve `ErrWouldBlock`, incluso si la señal proviene de una lectura.

Si tienes un `io.Reader`/`io.Writer` normal pero quieres exponer las interfaces de ruta rápida *y* preservar la semántica, envuelve con:

- `iox.AsWriterTo(r)` para añadir un `WriteTo` implementado vía `iox.Copy`
- `iox.AsReaderFrom(w)` para añadir un `ReadFrom` implementado vía `iox.Copy`

### Lecturas vacías y copias acotadas

En el bucle genérico de lectura y escritura, `Copy` se detiene si una lectura devuelve `(0, nil)` y devuelve `(written, nil)`. Esto puede ocurrir antes del fin de la entrada. Si se delega en `WriteTo` o `ReadFrom`, ese método controla sus propias lecturas.

Si el límite es positivo, `CopyN` y sus variantes devuelven `(n, nil)` en cuanto el destino ha aceptado `n` bytes. Si se alcanza EOF antes de tiempo o la copia termina con `nil` sin llegar al límite, devuelven `io.ErrUnexpectedEOF`. La operación subyacente puede seguir activa después de completar la copia acotada.

### Escrituras parciales

Si el bucle genérico devuelve un error semántico tras una escritura parcial, retrocede en el origen con `seeker.Seek(int64(nw-nr), io.SeekCurrent)`. `nr` es el número de bytes leídos y `nw` el de bytes aceptados por el destino. Si `Seek` falla, su error tiene prioridad sobre el de escritura. Si el origen no implementa `io.Seeker`, se devuelve `ErrNoSeeker`.

Con `CopyPolicy`, `PolicyRetry` conserva los bytes pendientes para reintentar una escritura que devuelve directamente `ErrWouldBlock` o `ErrMore`, incluso si el origen no permite reposicionarse. Si se delega en `WriteTo` o `ReadFrom`, ese método gestiona la posición del origen y la recuperación.

## Licencia

MIT — ver [LICENSE](./LICENSE).

©2025 Hayabusa Cloud Co., Ltd.
