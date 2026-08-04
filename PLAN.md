# Gremlyn — Plan d'implémentation

> État au 2026-08-04. **Tout P0, P1 et P2 est livré.**
>
> Le score de résilience mesure enfin l'agent, `gremlyn wrap` fonctionne, la distribution
> est prête et la CI existe. Ce qui reste est du produit, plus du déblocage.

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

### P0.0 — Fusionner les repos · ✅ FAIT

Le travail P0.2 touche core **et** arena ensemble : c'est exactement le cas où quatre repos coûtent le plus (tag core → `go get` → rebuild à chaque itération de debug).

Cible : un repo, `cmd/{gremlyn,shield,arena}`, `pkg/` partagé, `internal/{shield,arena}/`. On garde le découplage de packages, on perd la danse `replace`.

Fait. Les anciens dossiers sont sauvegardés dans `../gremlyn-old-repos-backup/` (13 Mo) et peuvent être supprimés.

Découverte : les quatre repos avaient **zéro commit**. Aucun historique à préserver, donc fusion sans risque.

### P0.0bis — Le proxy ne parlait pas MCP · ✅ FAIT (découvert en cours de route)

`gremlyn wrap` n'avait **jamais** fonctionné de bout en bout. Deux bugs, antérieurs à la fusion :

1. **Mauvais framing.** `StdioTransport` utilisait le framing `Content-Length:` — celui de **LSP** — en affirmant dans son commentaire que c'était « as used by MCP over stdio ». MCP sur stdio est du **JSON délimité par sauts de ligne**. Le proxy ne pouvait parser aucun octet émis par un vrai client ni par un vrai serveur.
2. **Le teardown jetait les réponses en vol.** `Start` sélectionnait sur un canal partagé par les trois boucles : le client atteignant EOF tuait l'enfant avant que sa réponse ait été lue.

**C'est très probablement la raison pour laquelle le runner Arena fabrique ses propres messages** — le vrai chemin ne fonctionnait pas, donc il a été contourné.

Corrigé : `LineFramer`, semi-fermeture avec drainage borné, `WithClientIO` pour rendre le chemin de données testable, et `wrap_dataflow_test.go` (aucun test n'exerçait le chemin de données, c'est pour ça que les bugs ont survécu).

Vérifié : `gremlyn wrap -- npx -y @modelcontextprotocol/server-memory` → `initialize` et `tools/list` traversent, les 9 outils intacts.

### P0.1 — `GremlinHandler` : les gremlins deviennent des étapes de pipeline · ✅ FAIT

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

### P0.2 — `ObserverHandler` : l'outcome devient observationnel · ✅ FAIT

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

### P0.3 — Valider la discriminance · ✅ GATE PASSÉ

Sessions appariées, même seed, mêmes gremlins, deux agents dont la seule différence est de réagir ou non à un timeout :

```
agent robuste (retry)  → overall 40, coverage complète
agent fragile (ignore) → overall 10, coverage complète
```

**Les scores se séparent.** Avant cette série, les deux auraient donné le même chiffre.

Validé par `internal/cli/arenaci_test.go`, avec un agent de référence scripté (`testdata/refagent`) plutôt qu'un LLM — un gate de mesure a besoin de comportements reproductibles.

---

## P1 — Distribution · ✅ FAIT

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

### P2.1 — `arena ci` en process · ✅ FAIT

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

### P2.2 — GitHub Action · ✅ FAIT

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
P0.0    fusion monorepo                 ✅
P0.0bis framing MCP + teardown proxy    ✅  ← wrap fonctionne pour la 1re fois
P0.0ter seeding + timeout gremlin       ✅  ← sessions rejouables
P0.1    GremlinHandler → pipeline       ✅
P0.2    ObserverHandler → outcome réel  ✅  ← le score mesure l'agent
P0.3    validation discriminance        ✅  GATE : 40 vs 10
─────────────────────────────────────────────────────
P1      GoReleaser + install + README   ✅  → publiable
─────────────────────────────────────────────────────
P2.0    contrat agent headless          ✅  GATE
P2.1    arena ci                        ✅
P2.2    GitHub Action                   ✅
```

## Ce qui reste — produit, plus déblocage

1. **Publier.** Créer `github.com/gremlyn-ai/gremlyn`, pousser, tagger `v0.1.0`. Le remote est déjà configuré, la CI et la release attendent. Il faut un secret `HOMEBREW_TAP_GITHUB_TOKEN` et un repo `gremlyn-ai/homebrew-tap` pour la formule.
2. **Un GIF d'une session de chaos** dans le README. C'est le pitch : un agent qui se casse la gueule se comprend en trois secondes.
3. **Mesurer avant d'annoncer quoi que ce soit.** Aucun chiffre de détection n'est publié parce qu'aucun n'a été mesuré. Voir l'agent `data-scientist`.
4. **Rendre les paramètres des gremlins configurables** par scénario (aujourd'hui `BuildGremlins` code en dur les délais, tailles, payloads).
5. **Shield** : repositionner en observabilité MCP, puis L2/L4. Toujours hors du chemin critique.

## À faire avant le premier push

**Purger les binaires de l'historique.** `bin/` a été commité par erreur sur plusieurs commits : 76 Mo de blobs, `.git` à 36 Mo. C'est corrigé pour la suite (untracké + gitignoré), mais les blobs restent dans l'historique. Rien n'a été poussé, donc c'est encore gratuit à nettoyer :

```bash
# nécessite git-filter-repo (pip install git-filter-repo)
git filter-repo --invert-paths --path bin/
```

Après un push, ces 76 Mo pèsent sur chaque `git clone`, définitivement.

**Aussi :** créer `gremlyn-ai/homebrew-tap` et le secret `HOMEBREW_TAP_GITHUB_TOKEN`, sinon l'étape Homebrew de la release échoue.

## Dette connue

- **`internal/shield/{detection/{classifier,llmjudge,structural},behavioral/*}.go`** sont documentés comme s'ils existaient. Ils sont marqués *(planned)* dans `.claude/` mais pas écrits.
- **Le mode HTTP/SSE du proxy** n'a jamais été testé contre un vrai client. Seul `wrap` (stdio) est vérifié de bout en bout.
- **`.claude/settings.local.json`** contient des chemins Windows périmés. Gitignoré, sans effet, mais bruyant.
- **Les paramètres des gremlins ne sont pas configurables** par scénario.
- **La fenêtre d'observation par défaut est de 30 s.** Trop courte, un agent lent passe pour fragile ; trop longue, une action ultérieure sans rapport compte comme une réaction. Non calibrée sur du trafic réel.
- **5 warnings de lint dans le dashboard**, laissés en place volontairement. Trois variables mortes (`toggle` dans `TerminalPanel.tsx`, `totalBlocked` dans `ThreatChart.tsx`, la prop `statusText` de `TopBar.tsx`) ressemblent à des features à moitié câblées — à regarder, pas à supprimer à l'aveugle. Les deux autres sont `no-page-custom-font` dans `app/layout.tsx` : les polices passent par `<link>` au lieu de `next/font`, ce qui est une décision de chargement, pas un détail de lint.

---

## Hors périmètre pour l'instant

Shield, le mode HTTP/SSE, PostgreSQL, le sidecar ML, les couches de détection L2/L3.

Tout ça existe déjà à un niveau suffisant, et **rien de tout ça ne débloque un utilisateur**.

Shield sera repositionné plus tard comme **observabilité MCP** (voir tout ce qui passe entre l'agent et ses outils) plutôt que comme *firewall* — besoin réel, immédiatement utile, et aucun claim de précision de détection à défendre.

## Le cadrage, qui compte autant que le code

Un projet open source vit ou meurt sur la première ligne de son README.

- ❌ « Chaos engineering + security platform for AI agents at the MCP layer »
- ✅ « Casse ton agent IA exprès, avant que la prod le fasse pour toi. »
