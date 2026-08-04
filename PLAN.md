# Gremlyn — Plan d'implémentation

> État au 2026-08-04. Objectif : passer d'un projet qui a de la valeur à un projet qui a de l'impact.
> L'écart est entièrement de la distribution — mais il y a un blocage technique à lever d'abord.

---

## Le constat qui ordonne tout le reste

**Le score de résilience ne mesure rien aujourd'hui.**

Dans `gremlyn-arena/internal/session/runner.go` :

| Ligne | Ce qui se passe |
|---|---|
| `buildTestMessage` (l.138) | fabrique un message synthétique |
| `g.Inject(...)` (l.98) | le mute |
| `assessOutcome` (l.174-201) | retourne outcome + score par **table de correspondance sur le nom du gremlin** |
| `AgentResponse` (l.114) | contient littéralement `{"injected":"true"}` |

`latency` → toujours `Survived, 70`. `loop` → toujours `Degraded, 40`. `hallucination` → toujours `Degraded, 30`.

Aucun agent dans la boucle. Aucun proxy. **Le score est une fonction de la config, pas du code testé.** Deux agents radicalement différents sortent le même chiffre.

### Conséquences

1. Le mode CI **ne peut pas** être la première priorité. Un `min-resilience-score: 70` qui bloque un merge sur ce chiffre donnerait une garantie fausse — et ça tue la crédibilité d'un projet sécu une seule fois.
2. Ce n'est **pas** un rewrite. `gremlyn-core/pkg/proxy/pipeline.go` a déjà tout : interface `Handler`, ordre par priorité, filtrage par direction, et le `RequestCorrelator` qui relie requête↔réponse. Arena ne l'utilise juste pas. C'est un branchement.

---

## P0 — Fermer la boucle

### P0.0 — Fusionner les repos · taille S · ⏸ en attente de décision

Le travail P0.2 touche core **et** arena ensemble : c'est exactement le cas où quatre repos coûtent le plus (tag core → `go get` → rebuild à chaque itération de debug).

Cible : un repo, `cmd/{gremlyn,shield,arena}`, `pkg/` partagé, `internal/{shield,arena}/`. On garde le découplage de packages, on perd la danse `replace`.

**Irréversible → nécessite un go explicite.** Non bloquant pour P0.1.

### P0.0bis — Le proxy ne parlait pas MCP · ✅ FAIT (découvert en cours de route)

`gremlyn wrap` n'avait **jamais** fonctionné de bout en bout. Deux bugs, antérieurs à la fusion :

1. **Mauvais framing.** `StdioTransport` utilisait le framing `Content-Length:` — celui de **LSP** — en affirmant dans son commentaire que c'était « as used by MCP over stdio ». MCP sur stdio est du **JSON délimité par sauts de ligne**. Le proxy ne pouvait parser aucun octet émis par un vrai client ni par un vrai serveur.
2. **Le teardown jetait les réponses en vol.** `Start` sélectionnait sur un canal partagé par les trois boucles : le client atteignant EOF tuait l'enfant avant que sa réponse ait été lue.

**C'est très probablement la raison pour laquelle le runner Arena fabrique ses propres messages** — le vrai chemin ne fonctionnait pas, donc il a été contourné.

Corrigé : `LineFramer`, semi-fermeture avec drainage borné, `WithClientIO` pour rendre le chemin de données testable, et `wrap_dataflow_test.go` (aucun test n'exerçait le chemin de données, c'est pour ça que les bugs ont survécu).

Vérifié : `gremlyn wrap -- npx -y @modelcontextprotocol/server-memory` → `initialize` et `tools/list` traversent, les 9 outils intacts.

### P0.1 — `GremlinHandler` : les gremlins deviennent des étapes de pipeline · taille S · ⏳ à faire

```
agent → [proxy + GremlinHandler] → vrai serveur MCP
```

Adaptateur qui implémente `proxy.Handler` autour de l'interface `Gremlin` existante. Les gremlins ne changent pas — leur contrat (no-op byte-exact, seedé, borné) devient enfin utile parce qu'il opère sur du trafic réel.

Livré : `gremlyn-arena/internal/chaos/{handler.go,injection.go}` + tests, dont une preuve end-to-end à travers un vrai `proxy.Pipeline`.

Décisions de design prises :
- **Un seul gremlin injecte par message** (premier qui déclenche gagne). Sinon les mutations se composent et l'attribution de l'outcome devient impossible.
- **Ordre stable obligatoire.** `Registry.List()` itère une map → ordre non déterministe → casse la rejouabilité. Le handler prend une slice ordonnée.
- **Une erreur de gremlin ne casse jamais le flux.** On log et on skip. Un outil de chaos qui casse le trafic de l'utilisateur sur son propre bug est inacceptable.
- **Pas d'outcome à l'injection.** L'outcome n'est pas connu au moment où le gremlin frappe — il vient de l'observation d'après (P0.2). Le handler émet une `Injection` (le fait), pas un `ArenaEvent` (l'observation résolue). Ça évite d'écrire des outcomes bidons en base.

### P0.2 — `ObserverHandler` : l'outcome devient observationnel · taille M · ← le vrai travail

Le coeur de la valeur produit. Après une injection, on regarde ce que l'agent fait ensuite dans le trafic proxifié. Le corrélateur fournit déjà `mctx.CorrelatedRequest`.

| Comportement observé | Outcome | Interprétation |
|---|---|---|
| Rappelle le même tool/method après l'injection | `Survived` | a détecté et retenté |
| Appelle un tool différent / change de stratégie | `Survived` | a compensé |
| Arrête la séquence sans rien retenter | `Degraded` | a abandonné |
| Continue sans réagir | `Crashed` | a avalé le mensonge — le pire cas |
| Aucun message dans la fenêtre | `Crashed` | a hang |

Implémentation : un `AsyncHandler` qui enregistre la séquence post-injection dans une fenêtre bornée, résout chaque `Injection` en `ArenaEvent`, et remplace `assessOutcome` par une fonction de cette séquence.

Ça reste une heuristique — à dire explicitement dans les docs. Mais une heuristique sur du **comportement réel observé**, ce qui est une autre classe de mesure.

À faire aussi : brancher le runner sur le vrai proxy (aujourd'hui il fabrique ses messages), et retirer `buildTestMessage`/`assessOutcome`.

### P0.3 — Valider la discriminance · taille S · ← GATE

Sessions appariées : même seed, mêmes gremlins, deux cibles. Un agent robuste (qui retry) vs un harness naïf (qui forward tout). **Les scores doivent se séparer nettement.**

Si non → la dimension est cassée, et on le sait avant que quelqu'un poste « j'ai eu 84, ça veut dire quoi ? ».

**Ne pas passer à P1 si ce gate échoue.**

---

## P1 — Distribution · taille S

**Réponse sur Homebrew : bonne idée, mauvais point de départ.** Brew seul est macOS-centrique et implique de maintenir une formule à la main.

La vraie réponse : **GoReleaser**. Un fichier de config → binaires cross-compilés, checksums, release GitHub, **formule Homebrew générée**, images Docker, changelog. Brew devient un output gratuit.

Ordre de priorité :

1. `go install github.com/...@latest` — gratuit, marche déjà, une ligne de README
2. GoReleaser → GitHub Releases + `sha256sum` — un outil sécu qui livre des binaires non vérifiables n'est pas livrable
3. Script `curl -fsSL ... | sh` — le standard des outils dev
4. Tap Homebrew — quasi zéro effort en plus via GoReleaser
5. Image Docker — nécessaire pour le CI (P2), pas pour le local

`CGO_ENABLED=0` (SQLite pur Go) rend tout ça propre : binaires statiques, image `distroless`, zéro dépendance runtime. **À protéger** — toute dépendance CGO casse la matrice de release.

---

## P2 — Mode CI

### P2.0 — Valider le contrat agent headless · ✅ GATE PASSÉ

Validé contre un agent MCP réel : **Claude Code**.

```bash
claude -p "<prompt>" --mcp-config mcp.json --output-format json --max-turns 1
```

Avec `mcp.json` pointant `command` sur `gremlyn wrap -- <serveur MCP>`. Vérifié par instrumentation : **l'agent spawne bien le proxy** (log de spawn confirmé). Le contrat `agent.command` de P2.1 tient.

Flags disponibles et pertinents : `-p/--print`, `--mcp-config`, `--output-format json|stream-json`, `--allowed-tools`, `--permission-mode`, `--max-turns`, `--input-format`.

**⚠️ Piège trouvé, à traiter en P2.1.** Lors de la première sonde, l'agent a tourné, retourné `is_error: false` et un exit 0 — **sans jamais appeler l'outil**. Un agent qui n'appelle aucun outil traverse zéro gremlin et sortirait donc avec un score « résilient » parfait alors que rien n'a été testé.

Conséquence de design, non négociable pour `arena ci` :
> **Une session sans appel d'outil est un échec de session, pas un succès.** Le harness doit vérifier qu'au moins un gremlin a effectivement été traversé, et sortir en erreur sinon. Sans ça, la CI rend un vert qui ne veut rien dire — exactement le défaut que P0 corrige côté score.

**Conséquence pour P0.3.** Un agent LLM réel est non déterministe et coûteux (les deux sondes : ~0,58 $). Un gate de mesure a besoin de comportements **reproductibles** : la validation de discriminance utilise un agent de référence scripté (robuste vs fragile), pas un LLM. Les agents réels servent à valider le *contrat*, pas à calibrer le *score*.

### P2.1 — `arena ci` en process · taille M

```yaml
# .gremlyn/arena.yaml
agent:
  command: ["python", "-m", "myagent", "--headless"]
mcp_servers:
  - command: ["npx", "@modelcontextprotocol/server-memory"]
scenarios:
  - name: tool-failure
    gremlins: [timeout, corruption]
    seed: 42
    prompts: ["Search for recent orders"]
thresholds:
  min_overall: 70
  min_dimension:
    data_integrity: 60
```

```bash
gremlyn arena ci --config .gremlyn/arena.yaml --format json --out report.json
```

Contraintes :
- **Zéro serveur, zéro DB requise.** Aujourd'hui `internal/cli/arena.go` est un client HTTP vers un serveur qui tourne. En CI il faut exécuter la session **en process**, SQLite éphémère ou en mémoire. C'est un refactor du chemin d'exécution, pas un flag.
- **Exit code piloté par les seuils** : `0` si tout passe, `1` sinon. C'est tout ce que le CI regarde.
- **Sortie déterministe** : seed dans la config → deux runs donnent le même score. Déjà acquis par construction — c'est l'avantage compétitif.
- **Double sortie** : JSON machine + résumé lisible dans les logs du job.

### P2.2 — GitHub Action · taille S

```yaml
- uses: gremlyn-ai/arena-action@v1
  with:
    config: .gremlyn/arena.yaml
    min-score: 70
```

Composite action : télécharge le binaire (ou image Docker), lance la commande, publie un résumé dans le job summary. **Commentaire de PR avec le delta de score vs la base** — c'est ça qui crée l'habitude, et c'est ce qui transforme le produit d'un « outil sympa essayé une fois » en quelque chose qui tourne à chaque PR.

---

## Séquencement

```
P0.0    fusion monorepo                 S   ✅ fait
P0.0bis framing MCP + teardown proxy    M   ✅ fait (wrap fonctionne enfin)
P0.1    GremlinHandler → pipeline       S   ✅ fait
P0.2    ObserverHandler → outcome réel  M   ⏳ en cours ← le vrai travail
P0.3    validation discriminance        S   ← GATE
─────────────────────────────────────────────────────
P1      GoReleaser + go install + brew  S   → publiable
─────────────────────────────────────────────────────
P2.0    valider contrat agent headless  S   ✅ GATE PASSÉ
P2.1    arena ci en process             M
P2.2    GitHub Action                   S
```

## Dette identifiée, non traitée

- **`.claude/` décrit encore un monde à quatre repos** : les chemins sont corrigés, mais le narratif (chaîne de versions core→consumers, boucles shell sur 4 dossiers) est obsolète dans `skills/release-build`, `agents/release-infrastructure`, `workflow.md`, `commands/{workflow,journal,changelog,commit}`, `skills/{stack-health,bug-triage}`, `agents/{code-reviewer,test-results-analyzer}`, `rules/go/{lint-vet,layering}`.
- **`golangci-lint`, `goimports`, `benchstat` ne sont pas installés** — `make lint` et `make check` échouent donc sur l'étape lint.
- **Les anciens dossiers sont sauvegardés** dans `../gremlyn-old-repos-backup/` (13 Mo) et peuvent être supprimés après vérification.

---

## Hors périmètre pour l'instant

Shield, le mode HTTP/SSE, PostgreSQL, le sidecar ML, les couches de détection L2/L3.

Tout ça existe déjà à un niveau suffisant, et **rien de tout ça ne débloque un utilisateur**.

Shield sera repositionné plus tard comme **observabilité MCP** (voir tout ce qui passe entre l'agent et ses outils) plutôt que comme *firewall* — besoin réel, immédiatement utile, et aucun claim de précision de détection à défendre.

## Le cadrage, qui compte autant que le code

Un projet open source vit ou meurt sur la première ligne de son README.

- ❌ « Chaos engineering + security platform for AI agents at the MCP layer »
- ✅ « Casse ton agent IA exprès, avant que la prod le fasse pour toi. »
