# iox

Sémantique non bloquante pour le paquet Go `io` : signaux de premier ordre pour would-block et multi-shot.

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Go Report Card](https://goreportcard.com/badge/code.hybscloud.com/iox)](https://goreportcard.com/report/code.hybscloud.com/iox)
[![Coverage Status](https://coveralls.io/repos/github/hayabusa-cloud/iox/badge.svg?branch=main)](https://coveralls.io/github/hayabusa-cloud/iox?branch=main)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Langue : [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | **Français**

## À quoi sert ce package ?

`iox` vise les stacks d’I/O non bloquantes où « aucun progrès maintenant » et « progrès maintenant, mais l’opération reste active » sont un **flux de contrôle normal**, pas des échecs.

Il introduit deux erreurs sémantiques (semantic errors) avec des contrats explicites :

- `ErrWouldBlock` — **aucun progrès n’est possible maintenant** sans attendre readiness/completions. Retourner immédiatement ; réessayer après votre prochain polling.
- `ErrMore` — **du progrès a eu lieu** et l’opération reste active ; **d’autres événements suivront**. Traiter le résultat courant et continuer à poller.

`iox` conserve les modèles mentaux standard de `io` :

- les compteurs retournés signifient toujours « octets transférés / progrès effectué »
- l’erreur retournée pilote le flux de contrôle (`nil`, non-échec sémantique, ou échec réel)
- les helpers sont compatibles avec `io.Reader`, `io.Writer`, et optimisent via `io.WriterTo` / `io.ReaderFrom`

## Contrat de sémantique

Pour les opérations qui adoptent la sémantique `iox` :

| Erreur retournée | Signification | Ce que l’appelant doit faire ensuite |
|---|---|---|
| `nil` | terminé avec succès pour cet appel / transfert | continuer votre machine à états |
| `ErrWouldBlock` | aucun progrès possible maintenant | arrêter la tentative ; attendre readiness/completion ; réessayer |
| `ErrMore` | du progrès a eu lieu ; d’autres completions suivront | traiter maintenant ; garder l’opération active ; continuer le polling |
| autre erreur | échec | gérer/journaliser/fermer/backoff selon le cas |

### Note : `iox.Copy` et les lectures `(0, nil)`

Le contrat Go `io.Reader` autorise `Read` à retourner `(0, nil)` pour signifier « pas de progrès », pas fin de flux.
Les Readers bien comportés devraient éviter `(0, nil)` sauf lorsque `len(p) == 0`.

`iox.Copy` traite une lecture `(0, nil)` comme « arrêter la copie maintenant » et retourne `(written, nil)`.
Cela évite de cacher du spinning dans un helper en code non bloquant / event-loop.
Si vous avez besoin d’une détection stricte du forward progress malgré des `(0, nil)` répétés, implémentez cette politique au niveau du call site.

## Démarrage rapide

Installer avec `go get` :

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

## Aperçu de l’API

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
  - `AsWriterTo(r Reader) Reader` (ajoute `io.WriterTo` via `iox.Copy`)
  - `AsReaderFrom(w Writer) Writer` (ajoute `io.ReaderFrom` via `iox.Copy`)

- Semantics
  - `IsNonFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

## Fast paths et préservation de sémantique

`iox.Copy` utilise les fast paths standard de `io` quand ils sont disponibles :

- si `src` implémente `io.WriterTo`, `iox.Copy` appelle `WriteTo`
- sinon, si `dst` implémente `io.ReaderFrom`, `iox.Copy` appelle `ReadFrom`
- sinon, il utilise un buffer fixe sur la pile (`32KiB`) et une boucle lecture/écriture

Pour préserver `ErrWouldBlock` / `ErrMore` sur les fast paths, assurez-vous que vos implémentations `WriteTo` / `ReadFrom` retournent ces erreurs quand c’est approprié.

Si vous avez un `io.Reader`/`io.Writer` classique mais voulez exposer les interfaces fast-path *et* préserver la sémantique, enveloppez avec :

- `iox.AsWriterTo(r)` pour ajouter un `WriteTo` implémenté via `iox.Copy`
- `iox.AsReaderFrom(w)` pour ajouter un `ReadFrom` implémenté via `iox.Copy`

## Licence

MIT — voir [LICENSE](./LICENSE).

©2025 Hayabusa Cloud Co., Ltd.
