# iox

Sémantique non bloquante pour le package Go `io` : signaux de premier ordre pour would-block et multi-shot.

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/iox.svg)](https://pkg.go.dev/code.hybscloud.com/iox)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/iox)](https://goreportcard.com/report/github.com/hayabusa-cloud/iox)
[![codecov](https://codecov.io/gh/hayabusa-cloud/iox/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/iox)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Langue : [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | **Français**

## Aperçu

`iox` vise les piles d'E/S non bloquantes où « aucun progrès maintenant » et « progrès maintenant, mais l'opération reste active » sont un **flux de contrôle normal**, pas des échecs.

Il introduit deux erreurs sémantiques avec des contrats explicites :

- `ErrWouldBlock` — **aucun progrès n'est possible maintenant** sans attendre un signal de disponibilité ou de complétion. Retourner immédiatement ; réessayer après le prochain sondage.
- `ErrMore` — **du progrès a eu lieu** et l'opération reste active ; **d'autres événements suivront**. Traiter le résultat courant et continuer le sondage.

`iox` conserve les modèles mentaux standard de `io` :

- les comptes retournés signifient toujours « octets transférés / progrès effectué »
- l'erreur retournée pilote le flux de contrôle (`nil`, signal sémantique non défaillant, ou échec réel)
- les fonctions auxiliaires sont compatibles avec `io.Reader`, `io.Writer`, et s'optimisent via `io.WriterTo` / `io.ReaderFrom`

## Contrat de sémantique

Pour les opérations qui adoptent la sémantique `iox` :

| Erreur retournée | Signification                                        | Ce que l’appelant doit faire ensuite                                  |
|------------------|------------------------------------------------------|-----------------------------------------------------------------------|
| `nil`            | terminé avec succès pour cet appel / transfert       | continuer votre machine à états                                       |
| `ErrWouldBlock`  | aucun progrès possible maintenant                    | arrêter la tentative ; attendre disponibilité/complétion ; réessayer  |
| `ErrMore`        | du progrès a eu lieu ; d'autres complétions suivront | traiter maintenant ; garder l'opération active ; continuer le sondage |
| autre erreur     | échec                                                | gérer/journaliser/fermer/appliquer une attente selon le cas           |

Traitez toujours le compteur retourné avant d'interpréter l'erreur retournée. Le compteur indique le progrès déjà effectué ; l'erreur sélectionne la prochaine action de contrôle.

Notes :

- `iox.Copy` peut retourner `(written > 0, ErrWouldBlock)` ou `(written > 0, ErrMore)` pour signaler un progrès partiel avant une situation de blocage ou avant de livrer une continuation multi-shot.
- Les lectures `(0, nil)` sont traitées comme « arrêter la copie maintenant » et retournent `(written, nil)` pour éviter de cacher une attente active dans les fonctions auxiliaires.
- `CopyN`, `CopyNBuffer` et leurs variantes avec politique sont des opérations bornées de "copie exactement n octets". Quand `written == n`, elles retournent `nil` ; ce ne sont pas des APIs de cycle de vie pour abonnements ou routes multi-shot.
- Les valeurs numériques de `Outcome` sont des encodages défensifs, pas un ordre sémantique. Utilisez `switch`, `Classify` et les prédicats au lieu de `<`, `>`, `min` ou `max`.

### Note : `iox.Copy` et les lectures `(0, nil)`

Le contrat Go `io.Reader` autorise `Read` à retourner `(0, nil)` pour signifier « pas de progrès », pas fin de flux. Les lecteurs bien comportés devraient éviter `(0, nil)` sauf lorsque `len(p) == 0`.

`iox.Copy` traite une lecture `(0, nil)` comme « arrêter la copie maintenant » et retourne `(written, nil)`. Cela évite de cacher une attente active dans une fonction auxiliaire en code non bloquant ou de boucle d'événements. Si vous avez besoin d'une détection stricte de progression malgré des `(0, nil)` répétés, implémentez cette politique au point d'appel.

### Note : `iox.Copy` et récupération d'écriture partielle

Lors de la copie vers une destination non bloquante, `dst.Write` peut retourner une erreur sémantique (`ErrWouldBlock` ou `ErrMore`) avec une écriture partielle (`nw < nr`). Dans ce cas, les octets ont été lus depuis `src` mais pas entièrement écrits vers `dst`.

Pour éviter la perte de données, `iox.Copy` tente de rembobiner le pointeur source :
- Si `src` implémente `io.Seeker`, Copy appelle `Seek(nw-nr, io.SeekCurrent)` pour rembobiner les octets non écrits.
- Si `src` n'implémente **pas** `io.Seeker`, Copy retourne `ErrNoSeeker` pour signaler que les octets non écrits sont irrécupérables.

Cette garantie de retour arrière s'applique à la boucle générique lecture/écriture contrôlée par `iox.Copy`. Si le chemin rapide standard `io.WriterTo` ou `io.ReaderFrom` est sélectionné, cette implémentation est responsable de son propre avancement de source et de sa récupération d'écritures partielles ; elle doit préserver `ErrWouldBlock` / `ErrMore` et rendre le réessai sûr pour les octets qu'elle n'a pas encore déclarés transférés.

**Recommandations :**

- Utilisez des sources repositionnables (p. ex., `*os.File`, `*bytes.Reader`) lors de la copie vers des destinations non bloquantes.
- Pour les sources non repositionnables (p. ex., sockets réseau), utilisez `CopyPolicy` avec `PolicyRetry` pour les erreurs sémantiques côté écriture. Cela garantit que tous les octets lus sont écrits avant le retour, évitant le besoin de retour arrière.

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
	_, _ = iox.CopyN(io.Discard, &dst, 5)                    // consomme "hello"

	n, err = iox.Copy(&dst, src)
	fmt.Printf("n=%d err=%v buf=%q\n", n, err, dst.String()) // n=5  err=io: would block   buf="world"
	_, _ = iox.CopyN(io.Discard, &dst, 5)                    // consomme "world"

	n, err = iox.Copy(&dst, src)
	fmt.Printf("n=%d err=%v buf=%q\n", n, err, dst.String()) // n=3  err=<nil>            buf="iox"
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

## Aperçu de l’API

- Erreurs
  - `ErrWouldBlock`, `ErrMore`, `ErrNoSeeker`

- Copie
  - `Copy(dst Writer, src Reader) (int64, error)`
  - `CopyBuffer(dst Writer, src Reader, buf []byte) (int64, error)`
  - `CopyN(dst Writer, src Reader, n int64) (int64, error)`
  - `CopyNBuffer(dst Writer, src Reader, n int64, buf []byte) (int64, error)`

- Tee
  - `TeeReader(r Reader, w Writer) Reader`
  - `TeeWriter(primary, tee Writer) Writer`

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

## Backoff — attente adaptative pour E/S externe

Quand `ErrWouldBlock` signale qu'aucun progrès n'est possible, l'appelant doit attendre avant de réessayer. `iox.Backoff` fournit une stratégie d'attente adaptative pour ce cas.

**Modèle de progrès à trois niveaux :**

| Niveau    | Mécanisme                          | Cas d'utilisation               |
|-----------|------------------------------------|---------------------------------|
| Strike    | Appel système                      | Appel direct au noyau           |
| Spin      | Yield matériel (`spin`)            | Synchronisation atomique locale |
| **Adapt** | Attente logicielle (`iox.Backoff`) | Disponibilité d'E/S externe     |

**Valeur zéro prête à l'emploi :**

```go
var b iox.Backoff  // utilise DefaultBackoffBase (500µs) et DefaultBackoffMax (100ms)

for {
	n, err := conn.Read(buf)
	if iox.IsWouldBlock(err) {
		b.Wait() // attente adaptative avec jitter
		continue
	}
	if err != nil {
		return err
	}
	process(buf[:n])
	b.Reset() // réinitialiser après une progression réussie
}
```

**Algorithme :** Mise à l'échelle linéaire par blocs avec ±12.5% de jitter pour éviter les effets de ruée.

- Bloc 1 : 1 attente de `base`
- Bloc 2 : 2 attentes de `2×base`
- Bloc n : n attentes de `min(n×base, max)`

**Méthodes :**

- `Wait()` — attend pendant la durée actuelle, puis avance
- `Reset()` — restaure au bloc 1
- `SetBase(d)` / `SetMax(d)` — configurer les durées

## Sémantique de Tee (comptes et erreurs)

- `TeeReader` retourne `n` comme le nombre d'octets lus depuis `r` (progrès source), même si l'écriture secondaire échoue ou est courte.
- `TeeWriter` retourne `n` comme le nombre d'octets acceptés par `primary` (progrès primaire), même si l'écriture tee échoue ou est courte.
- Quand `n > 0`, un adaptateur tee peut retourner `(n, err)` où `err` provient de l'écriture secondaire/tee (y compris `ErrWouldBlock`/`ErrMore`). Traitez d'abord `p[:n]`.
- Pour une meilleure interopérabilité avec les fonctions auxiliaires pilotées par politique, retournez `ErrWouldBlock`/`ErrMore` tels quels (évitez de les envelopper).

## Politique sémantique

Certaines fonctions auxiliaires acceptent optionnellement une `SemanticPolicy` pour décider quoi faire lorsqu'elles rencontrent `ErrWouldBlock` ou `ErrMore` (p. ex., retourner immédiatement ou céder puis réessayer).

La valeur par défaut est `nil`, ce qui signifie que le **comportement non bloquant est préservé** : la fonction auxiliaire retourne `ErrWouldBlock` / `ErrMore` à l’appelant et n’attend ni ne réessaie de lui-même.

## Classification et répartition par sentinelle exacte

Les fonctions auxiliaires de classification (`IsMore`, `IsWouldBlock`, `IsSemantic`, `IsNonFailure`, `IsFailure` et `Classify`) utilisent `errors.Is`, donc les sentinelles sémantiques enveloppées sont classifiées correctement aux frontières d'API et dans les diagnostics. Préférez ces fonctions auxiliaires aux appels directs à `errors.Is` pour classifier les résultats de contrôle sémantique de `iox`.

Les chemins rapides internes de copy et tee répartissent les décisions de politique par identité exacte de sentinelle. Les producteurs sous votre contrôle devraient retourner `ErrMore` et `ErrWouldBlock` sans les envelopper sur ces chemins. Une erreur sémantique enveloppée est quand même retournée et classifiable, mais elle ne déclenche pas le réessai de politique fondé sur la sentinelle exacte.

## Chemins rapides et préservation de la sémantique

`iox.Copy` utilise les chemins rapides standard de `io` quand ils sont disponibles :

- si `src` implémente `io.WriterTo`, `iox.Copy` appelle `WriteTo`
- sinon, si `dst` implémente `io.ReaderFrom`, `iox.Copy` appelle `ReadFrom`
- sinon, il utilise un buffer interne de taille fixe (`32KiB`) et une boucle lecture/écriture

Pour préserver `ErrWouldBlock` / `ErrMore` sur les chemins rapides, assurez-vous que vos implémentations `WriteTo` / `ReadFrom` retournent ces erreurs quand c'est approprié. Les chemins rapides doivent aussi garder une comptabilité de progression exacte : le compte retourné est le nombre d'octets réellement transférés, et les octets restants doivent rester disponibles pour un réessai ultérieur ou être représentés par une vraie erreur terminale.

Si vous avez un `io.Reader`/`io.Writer` classique mais voulez exposer les interfaces de chemin rapide *et* préserver la sémantique, enveloppez avec :

- `iox.AsWriterTo(r)` pour ajouter un `WriteTo` implémenté via `iox.Copy`
- `iox.AsReaderFrom(w)` pour ajouter un `ReadFrom` implémenté via `iox.Copy`

## Licence

MIT — voir [LICENSE](./LICENSE).

©2025 Hayabusa Cloud Co., Ltd.
