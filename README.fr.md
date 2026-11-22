# iox

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Benchmarks](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/iox/actions/workflows/benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/iox/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/iox)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Sémantique non bloquante pour le package Go `io` : signaux de premier ordre pour would-block et multi-shot.

Langue : [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | **Français**

## Aperçu

`iox` s’adresse aux piles d’E/S non bloquantes où « attendre que la ressource soit prête avant de réessayer » (`ErrWouldBlock`) et « continuer à observer la même opération » (`ErrMore`) font partie du flux de contrôle normal.

Ses fonctions de copie et de duplication de flux (tee) utilisent les interfaces standard `io.Reader` et `io.Writer` de Go. Dans le couple habituel `(n, err)`, `n` indique le nombre d’octets déjà transférés et `err` guide la suite du traitement. Avec l’un ou l’autre signal, `n` peut être nul ou positif. Tenez d’abord compte des `n` octets transférés.

## Résultats des E/S

| Erreur | Résultat de `Classify` | Action |
|---|---|---|
| `nil` | `OutcomeOK` | Poursuivre ou terminer le transfert |
| `ErrWouldBlock` | `OutcomeWouldBlock` | Attendre que la ressource soit prête avant de réessayer |
| `ErrMore` | `OutcomeMore` | Garder l’opération active et observer la prochaine complétion |
| Autre erreur | `OutcomeFailure` | Traiter l’erreur en tenant compte des octets déjà transférés |

Lors d’un appel direct à `Read`, `io.EOF` marque la fin de l’entrée. Traitez les derniers octets avant de terminer. Si vous passez `io.EOF` à `Classify`, vous obtenez `OutcomeFailure`.

## Installation et exemple

Ce module nécessite Go 1.27 ou une version ultérieure.

```shell
go get code.hybscloud.com/iox
```

Le deuxième appel à `Copy` transfère `"world"` avant que le lecteur renvoie `ErrWouldBlock`. L’appelant consomme ces cinq octets avec `CopyN`, puis utilise sa fonction `waitReadable` pour attendre que la lecture puisse reprendre. [Exemple d’attente](examples/wouldblock.go)

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

Le lecteur conserve les octets qui ne tiennent pas dans `p`. Une lecture de longueur nulle laisse le bloc intact. Il renvoie l’erreur avec le dernier octet du bloc, ou seule si le bloc est vide.

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

## Boucle de contrôle canonique

Les appelants devraient d'abord gérer le progrès, puis classifier le contrôle avec les fonctions auxiliaires du package :

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

## Aperçu de l’API

- Erreurs
  - `ErrWouldBlock`, `ErrMore`, `ErrNoSeeker`

- Copie
  - `Copy(dst Writer, src Reader) (int64, error)`
  - `CopyBuffer(dst Writer, src Reader, buf []byte) (int64, error)`
  - `CopyN(dst Writer, src Reader, n int64) (int64, error)`
  - `CopyNBuffer(dst Writer, src Reader, n int64, buf []byte) (int64, error)`
  - `CopyPolicy`, `CopyBufferPolicy`, `CopyNPolicy` et `CopyNBufferPolicy` acceptent un dernier argument de type `SemanticPolicy`.

- Tee
  - `TeeReader(r Reader, w Writer) Reader`
  - `TeeWriter(primary, tee Writer) Writer`
  - `TeeReaderPolicy` et `TeeWriterPolicy` acceptent aussi un dernier argument de type `SemanticPolicy`.

- Adaptateurs
  - `AsWriterTo(r Reader) Reader` (ajoute `io.WriterTo` via `iox.Copy`)
  - `AsReaderFrom(w Writer) Writer` (ajoute `io.ReaderFrom` via `iox.Copy`)

- Sémantique
  - `Outcome`
  - `Classify(err error) Outcome`
  - `IsSemantic(err error) bool`
  - `IsNonFailure(err error) bool`
  - `IsFailure(err error) bool`
  - `IsWouldBlock(err error) bool`
  - `IsMore(err error) bool`
  - `IsProgress(err error) bool`

- Backoff
  - `Backoff` — attente adaptative pour l'E/S externe
  - `DefaultBackoffBase` (500µs), `DefaultBackoffMax` (100ms)

## E/S reprenables

`Step.Next` renvoie une valeur, une erreur et un `Wait`. Si `Wait` a sa valeur zéro, l’étape est terminée. `Finish` fournit le résultat final. `Await` décrit l’attente d’une valeur d’un type donné et la continuation qui renvoie l’étape suivante.

Passez à `Wait.Arm` la fonction de mise en file de l’exécuteur. La source peut émettre un signal avant le retour de `Arm`. Le premier appel accepté à `Signal.Deliver` place un `Ready` dans la file. Un seul appel à `TryResume` peut le consommer et exécuter la continuation. Si `Deliver` renvoie `false`, la source conserve la valeur. [Exemple de réception d’une valeur typée](example_await_test.go)

`WriteStep` enregistre le nombre d’octets et l’erreur de chaque écriture. Si la destination accepte seulement un préfixe et renvoie `ErrWouldBlock`, un signal autorise une tentative d’écriture des octets restants vers la même destination. Si elle accepte tout le tampon, l’étape se termine avec l’erreur renvoyée, y compris `ErrWouldBlock`. Gardez le tampon valide et intact jusqu’à la fin de l’opération ou au retrait effectif de l’attente.

## Backoff — attente adaptative pour E/S externe

Si une notification de disponibilité est prévue, attendez-la après `ErrWouldBlock`. Sinon, `iox.Backoff.Wait` suspend la goroutine entre deux tentatives.

**Modèle de progrès à trois niveaux :**

| Niveau    | Mécanisme                          | Cas d'utilisation               |
|-----------|------------------------------------|---------------------------------|
| Strike    | Appel système                      | Appel direct au noyau           |
| Spin      | Yield matériel (`spin`)            | Synchronisation atomique locale |
| **Adapt** | Attente logicielle (`iox.Backoff`) | Disponibilité d'E/S externe     |

**Valeur zéro prête à l'emploi :**

```go
var b iox.Backoff // utilise DefaultBackoffBase (500µs) et DefaultBackoffMax (100ms)

for {
	n, err := conn.Read(buf)
	if n > 0 {
		process(buf[:n])
		b.Reset() // réinitialiser après une progression réussie
	}
	if iox.IsWouldBlock(err) {
		b.Wait() // attente adaptative avec jitter
		continue
	}
	if err != nil || n == 0 {
		return err
	}
}
```

Traitez d’abord les octets lus. Si l’erreur correspond à `ErrWouldBlock`, la boucle attend avant de réessayer, même si elle correspond aussi à `ErrMore`. Elle renvoie toute autre erreur telle quelle. Une lecture qui renvoie `(0, nil)` rend le contrôle à l’appelant et peut survenir avant la fin de l’entrée.

Le bloc `n` contient `n` intervalles entre tentatives. `Duration()` renvoie l’intervalle nominal, `min(n × base, max)`. Chaque intervalle peut s’en écarter de 12,5 % au plus et dépasser `max`.

- `NextDelay()` avance d’un intervalle et renvoie sa durée pour armer le temporisateur de la prochaine tentative.
- `Wait()` appelle `NextDelay()` et suspend la goroutine appelante pendant cet intervalle.
- `Reset()` revient au bloc 1 et conserve la durée de base et le plafond.
- `SetBase(d)` et `SetMax(d)` acceptent des durées positives et conservent le bloc en cours.

## Sémantique de Tee (comptes et erreurs)

- `TeeReader` retourne `n` comme le nombre d'octets lus depuis `r` (progrès source), même si l'écriture secondaire échoue ou est courte.
- `TeeWriter` retourne `n` comme le nombre d'octets acceptés par `primary` (progrès primaire), même si l'écriture tee échoue ou est courte.
- Quand `n > 0`, un adaptateur tee peut retourner `(n, err)` où `err` provient de l'écriture secondaire/tee (y compris `ErrWouldBlock`/`ErrMore`). Traitez d'abord `p[:n]`.
- Pour une meilleure interopérabilité avec les fonctions auxiliaires pilotées par politique, retournez `ErrWouldBlock`/`ErrMore` tels quels (évitez de les envelopper).

## Politique sémantique

Certaines fonctions auxiliaires acceptent optionnellement une `SemanticPolicy` pour décider quoi faire lorsqu'elles rencontrent `ErrWouldBlock` ou `ErrMore` (p. ex., retourner immédiatement ou céder puis réessayer).

Avec la politique par défaut (`nil`), l’appelant gère l’attente et les nouvelles tentatives. Si `n > 0`, `CopyNPolicy` et `CopyNBufferPolicy` renvoient `(n, nil)` dès que la destination a accepté les `n` octets. Un signal reçu avec le dernier octet ne déclenche aucun appel à la politique.

## Classification et répartition par sentinelle exacte

Les fonctions auxiliaires de classification (`IsMore`, `IsWouldBlock`, `IsSemantic`, `IsNonFailure`, `IsFailure` et `Classify`) utilisent `errors.Is`, donc les sentinelles sémantiques enveloppées sont classifiées correctement aux frontières d'API et dans les diagnostics. Préférez ces fonctions auxiliaires aux appels directs à `errors.Is` pour classifier les résultats de contrôle sémantique de `iox`.

`IsProgress` vaut true uniquement lorsque `Classify` sélectionne `OutcomeOK` ou `OutcomeMore`. Si une erreur correspond aux deux sentinelles sémantiques, `ErrWouldBlock` est prioritaire et `IsProgress` vaut false. `IsMore` et `IsWouldBlock` vérifient la correspondance avec chaque sentinelle ; les deux peuvent donc être true pour la même erreur. Ces prédicats ne mesurent pas le progrès en octets ; comptabilisez toujours les compteurs retournés séparément.

Les fonctions de copie et de tee consultent la politique uniquement si elles reçoivent directement l’une des sentinelles `ErrMore` ou `ErrWouldBlock`. Renvoyez ces sentinelles sans les envelopper pour que la politique puisse les traiter.

## Chemins rapides et préservation de la sémantique

`Copy` choisit en priorité `io.WriterTo` sur la source, puis `io.ReaderFrom` sur la destination. La boucle générique utilise le tampon fourni ou un tampon de 32 Kio par défaut. `CopyN` limite les lectures au nombre d’octets restants. Elle peut appeler `ReadFrom` sur la destination et ne fait pas appel au `WriteTo` de la source d’origine.

La méthode déléguée renvoie le nombre total d’octets transférés et une erreur. Renvoyez `ErrWouldBlock` ou `ErrMore` tels quels pour que la politique puisse traiter le signal. Si la méthode permet une reprise, elle doit conserver les octets restant à écrire. `YieldOnWriteWouldBlockPolicy` réessaie lorsque l’une ou l’autre méthode renvoie `ErrWouldBlock`, même si le signal provient d’une lecture.

Si vous avez un `io.Reader`/`io.Writer` classique mais voulez exposer les interfaces de chemin rapide *et* préserver la sémantique, enveloppez avec :

- `iox.AsWriterTo(r)` pour ajouter un `WriteTo` implémenté via `iox.Copy`
- `iox.AsReaderFrom(w)` pour ajouter un `ReadFrom` implémenté via `iox.Copy`

### Lectures vides et copies bornées

Dans la boucle générique de lecture et d’écriture, `Copy` s’arrête lorsqu’une lecture renvoie `(0, nil)` et renvoie `(written, nil)`. Cette situation peut survenir avant la fin de l’entrée. En cas de délégation à `WriteTo` ou `ReadFrom`, la méthode choisie contrôle ses propres lectures.

Pour une limite positive, `CopyN` et ses variantes renvoient `(n, nil)` dès que la destination a accepté `n` octets. Si EOF survient plus tôt ou que la copie se termine avec `nil` avant cette limite, elles renvoient `io.ErrUnexpectedEOF`. L’opération sous-jacente peut rester active après la fin de la copie bornée.

### Écritures partielles

Lorsque la boucle générique renvoie une erreur sémantique après une écriture partielle, elle revient au premier octet restant à écrire avec `seeker.Seek(int64(nw-nr), io.SeekCurrent)`. `nr` est le nombre d’octets lus et `nw` celui des octets acceptés par la destination. Si `Seek` échoue, son erreur prime sur celle de l’écriture. Si la source n’implémente pas `io.Seeker`, la boucle renvoie `ErrNoSeeker`.

Avec `CopyPolicy`, `PolicyRetry` conserve les octets en attente lorsqu’une écriture renvoie directement `ErrWouldBlock` ou `ErrMore`, même si la source ne permet pas de revenir en arrière. Une méthode déléguée `WriteTo` ou `ReadFrom` gère elle-même la position de la source et la reprise.

## Mesures de performance

`BenchmarkCopyBufferReuse` compare `iox.CopyBuffer`, `iox.CopyBufferPolicy` avec `ReturnPolicy` et `io.CopyBuffer` dans la boucle générique de lecture et d’écriture. Chaque cas réutilise un tampon de 32 Kio pour copier les mêmes données en mémoire jusqu’à EOF.

```shell
go test -run '^$' -bench '^BenchmarkCopyBufferReuse$' -benchmem -benchtime=200ms -count=10 -cpu=1 -timeout=120s .
```

Testez les deux révisions sur la même machine, avec la même version de Go et une charge comparable. Comparez `ns/op` et `allocs/op` à taille d’entrée égale. Pour mesurer la latence de réveil, utilisez une charge de travail qui attend que l’E/S soit prête.

## Licence

MIT — voir [LICENSE](./LICENSE).

©2025 Hayabusa Cloud Co., Ltd.
