# iox

Sémantique non bloquante pour le paquet Go `io` : signaux de premier ordre pour would-block et multi-shot.

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/iox)](https://goreportcard.com/report/github.com/hayabusa-cloud/iox)
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

Notes :
- `iox.Copy` peut retourner `(written > 0, ErrWouldBlock)` ou `(written > 0, ErrMore)` pour signaler un progrès partiel avant un arrêt (would-block) ou avant de livrer une continuation multi-shot.
- Les lectures `(0, nil)` sont traitées comme « arrêter la copie maintenant » et retournent `(written, nil)` pour éviter de cacher du spinning dans les helpers.

### Note : `iox.Copy` et les lectures `(0, nil)`

Le contrat Go `io.Reader` autorise `Read` à retourner `(0, nil)` pour signifier « pas de progrès », pas fin de flux.
Les Readers bien comportés devraient éviter `(0, nil)` sauf lorsque `len(p) == 0`.

`iox.Copy` traite une lecture `(0, nil)` comme « arrêter la copie maintenant » et retourne `(written, nil)`.
Cela évite de cacher du spinning dans un helper en code non bloquant / event-loop.
Si vous avez besoin d’une détection stricte du forward progress malgré des `(0, nil)` répétés, implémentez cette politique au niveau du call site.

### Note : `iox.Copy` et récupération d'écriture partielle

Lors de la copie vers une destination non bloquante, `dst.Write` peut retourner une erreur sémantique (`ErrWouldBlock` ou `ErrMore`) avec une écriture partielle (`nw < nr`). Dans ce cas, les octets ont été lus depuis `src` mais pas entièrement écrits vers `dst`.

Pour éviter la perte de données, `iox.Copy` tente de rembobiner le pointeur source :
- Si `src` implémente `io.Seeker`, Copy appelle `Seek(nw-nr, io.SeekCurrent)` pour rembobiner les octets non écrits.
- Si `src` n'implémente **pas** `io.Seeker`, Copy retourne `ErrNoSeeker` pour signaler que les octets non écrits sont irrécupérables.

**Recommandations :**
- Utilisez des sources seekables (p. ex., `*os.File`, `*bytes.Reader`) lors de la copie vers des destinations non bloquantes.
- Pour les sources non seekables (p. ex., sockets réseau), utilisez `CopyPolicy` avec `PolicyRetry` pour les erreurs sémantiques côté écriture. Cela garantit que tous les octets lus sont écrits avant le retour, évitant le besoin de rollback.

## Démarrage rapide

Installer avec `go get` :

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

## Aperçu de l’API

- Errors
  - `ErrWouldBlock`, `ErrMore`, `ErrNoSeeker`

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

- Backoff
  - `Backoff` — backoff adaptatif pour l'attente d'I/O externe
  - `DefaultBackoffBase` (500µs), `DefaultBackoffMax` (100ms)

## Backoff — Attente Adaptative pour I/O Externe

Quand `ErrWouldBlock` signale qu'aucun progrès n'est possible, l'appelant doit attendre avant de réessayer. `iox.Backoff` fournit une stratégie de backoff adaptatif pour cette attente.

**Modèle de Progrès à Trois Niveaux :**

| Niveau | Mécanisme | Cas d'Usage |
|--------|-----------|-------------|
| Strike | Appel système | Frappe directe du kernel |
| Spin | Yield matériel (`spin`) | Synchronisation atomique locale |
| **Adapt** | Backoff logiciel (`iox.Backoff`) | Readiness I/O externe |

**Valeur zéro prête à l'emploi :**

```go
var b iox.Backoff  // utilise DefaultBackoffBase (500µs) et DefaultBackoffMax (100ms)

for {
    n, err := conn.Read(buf)
    if err == iox.ErrWouldBlock {
        b.Wait()  // sleep adaptatif avec jitter
        continue
    }
    if err != nil {
        return err
    }
    process(buf[:n])
    b.Reset()  // réinitialiser après progrès réussi
}
```

**Algorithme :** Mise à l'échelle linéaire par blocs avec ±12.5% de jitter pour éviter les thundering herds.
- Bloc 1 : 1 sleep de `base`
- Bloc 2 : 2 sleeps de `2×base`
- Bloc n : n sleeps de `min(n×base, max)`

**Méthodes :**
- `Wait()` — dort la durée actuelle, puis avance
- `Reset()` — restaure au bloc 1
- `SetBase(d)` / `SetMax(d)` — configurer les timings

## Sémantique de Tee (comptes et erreurs)

- `TeeReader` retourne `n` comme le nombre d’octets lus depuis `r` (progrès source), même si l’écriture côté side échoue/est courte.
- `TeeWriter` retourne `n` comme le nombre d’octets acceptés par `primary` (progrès primary), même si l’écriture côté tee échoue/est courte.
- Quand `n > 0`, un adaptateur tee peut retourner `(n, err)` où `err` provient du side/tee (y compris `ErrWouldBlock`/`ErrMore`). Traitez d’abord `p[:n]`.
- Pour une meilleure interopérabilité avec les helpers pilotés par policy, retournez `ErrWouldBlock`/`ErrMore` tels quels (évitez de les envelopper).

## Politique sémantique

Certains helpers acceptent optionnellement une `SemanticPolicy` pour décider quoi faire lorsqu’ils rencontrent `ErrWouldBlock` ou `ErrMore`
(p. ex., retourner immédiatement vs céder/yield et réessayer).

La valeur par défaut est `nil`, ce qui signifie que le **comportement non bloquant est préservé** : le helper retourne
`ErrWouldBlock` / `ErrMore` à l’appelant et n’attend ni ne réessaie de lui-même.

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
