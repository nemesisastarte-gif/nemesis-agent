# Mode Local — la machine hôte devient l'environnement de développement

> Design du mode « non-cloud » pour NemesisCode. Étape 1 du passage à un
> usage personnel / petite équipe sur machine nue (Linux, Termux, etc.).

## Problème

Aujourd'hui le backend est un client d'une infra cloud multi-services :

```
backend → taskflow (service externe, image fermée) → Runner → VM (conteneur)
backend → rustfs (stockage objet S3)  → fichiers
backend → ingress (nginx)             → entrée web
backend → clickhouse / loki           → logs & métriques
```

Quand l'utilisateur crée une tâche, le backend demande une **VM** à taskflow ;
la VM contient l'environnement de dev (repo cloné, outils) et taskflow y lance
l'agent (moteur `ohmyagent`). Sans taskflow, aucune tâche ne peut tourner.

## Objectif

Quand le backend est lancé en mode `local` sur une machine hôte, **cette
machine EST l'environnement de développement** : plus de VM, plus de taskflow,
plus de rustfs. L'agent tourne directement sur l'hôte, dans un répertoire de
travail dédié par tâche, et le terminal / les fichiers / les diffs sont ceux de
la machine.

```
backend (mode local)
  ├─ VM virtuelle  = répertoire ~/.nemesiscode/workspaces/<vm_id>
  ├─ Agent         = processus ohmyagent lancé sur l'hôte (cwd = workspace)
  ├─ Terminal      = shell local (bash/sh) dans le workspace
  ├─ Fichiers      = système de fichiers local
  └─ Ports         = ports locaux (le process tourne déjà sur l'hôte)
```

## Point d'injection

Le backend consomme taskflow exclusivement à travers l'interface
`taskflow.Clienter` (`backend/pkg/taskflow/client.go`) :

```go
type Clienter interface {
    VirtualMachiner() VirtualMachiner
    Host() Hoster
    FileManager() FileManager
    TaskManager() TaskManager
    PortForwarder() PortForwarder
    Stats(ctx) (*Stats, error)
    TaskLive(ctx, taskID, flush, fn func(*TaskChunk) error) error
}
```

On fournit une implémentation **locale** de cette interface dans
`backend/pkg/taskflow/local/`. Le choix se fait à la construction (DI) selon la
config : `MCAI_TASKFLOW_MODE=local` (défaut `remote`). **Aucun autre code du
backend n'est modifié** : les handlers, lifecycle hooks et usecases restent
identiques.

## Sémantique du mode local

| Concept cloud | Équivalent local |
|---|---|
| VM (conteneur isolé) | Répertoire `$WORKSPACE_ROOT/<vm_id>` (défaut `~/.nemesiscode/workspaces`) |
| Clone du repo dans la VM | `git clone` dans le workspace (token éventuel injecté dans l'URL) |
| Lancement de l'agent | `$NEMESIS_AGENT_BIN --task-config <workspace>/nemesis-task.json`, cwd = workspace |
| Config de tâche | `CreateTaskReq` sérialisé dans `nemesis-task.json` (contrat documenté ci-dessous) |
| Sortie de tâche (TaskLive) | stdout/stderr du processus → `TaskChunk{Event, Kind, Data, Seq, Timestamp}` + buffer rejouable |
| Terminal | shell local (pipes ; resize ignoré en v1, PTY possible ensuite via `x/sys`) |
| Fichiers | opérations FS locales, bornées au workspace (anti-traversée) |
| Diffs repo | `git diff` / `git status --porcelain` dans le workspace |
| Port forwarding | enregistrements locaux (le service est déjà joignable sur l'hôte) |
| Hibernate/Resume | marqueurs d'état (pas de vraie suspension) |
| Hôte | la machine elle-même (`runtime.NumCPU`, hostname, GOOS/GOARCH) |

## Contrat agent — le VRAI moteur ohmyagent (protocole --stdio)

Le mode local pilote le **vrai moteur** `ohmyagent` (sous-module `agent/`,
dépôt `chaitin/OhMyAgent`) via son protocole natif : **JSON-RPC 2.0 ligne par
ligne** sur stdin/stdout, lancé avec `ohmyagent --stdio`. C'est exactement le
protocole que le client desktop (Rust) utilise (référence :
`desktop/src/driver/` — transport.rs / normalize.rs).

1. Le backend lance :
   ```
   $NEMESIS_AGENT_BIN --stdio
     cwd  <workspace>/.ohmyagent
     env  OHMYAGENT_CONFIG_DIR=<workspace>/.ohmyagent
          NEMESIS_TASK_ID / NEMESIS_VM_ID / NEMESIS_WORKSPACE
   ```
   (le fichier `nemesis-task.json` reste écrit dans le workspace comme
   référence/débogage, mais n'est pas le contrat).

2. Handshake : le moteur émet `system/ready`
   `{protocolVersion: "1.0", version, capabilities, shutdownGraceMs}` —
   le backend vérifie `protocolVersion == "1.0"`.

3. Sessions :
   - `session/create` `{cwd: <workspace>, permission_mode, interactive: true,
     model: <model-id>}` → `{session_id}`
   - `session/sendMessage` `{session_id, message: <texte de la tâche>}`
   - `session/destroy`, `session/compact`, `session/exists`

4. Notifications reçues (moteur → backend) et mappées en TaskChunk :
   | Notification | Mapping TaskChunk |
   |---|---|
   | `event/stream` `model_delta` | `task-running` / `acp_event` / ACP `agent_message_chunk` |
   | `event/stream` `thinking_delta` | ACP `agent_thought_chunk` |
   | `event/stream` `tool_call` | ACP `tool_call` (in_progress) |
   | `event/stream` `tool_result` | ACP `tool_call_update` |
   | `event/stream` `error` | `task-error` (sauf `transient_retry`) |
   | `permission/request` | auto-approuvé (mode local, `permission_mode=yolo`) |
   | `question/request` | répondu `cancelled` (v1) |
   | `turn/stopped` | `task-ended` (`success` / `failed`) |

   Le Data des chunks ACP est `base64(JSON {update: {sessionUpdate: ...}})`
   — le format exact attendu par le frontend web (`task-message-handler.ts`).

5. Arrêt : `session/destroy` puis SIGINT (SIGKILL après 3 s si nécessaire).

## Configuration

Variables d'environnement (préfixe `MCAI_`, le backend n'a pas de config.yaml) :

| Variable | Défaut | Rôle |
|---|---|---|
| `MCAI_TASKFLOW_MODE` | `remote` | `local` pour activer le mode machine hôte |
| `MCAI_TASKFLOW_LOCAL_WORKSPACE_ROOT` | `~/.nemesiscode/workspaces` | racine des environnements |
| `MCAI_TASKFLOW_LOCAL_AGENT_BIN` | `ohmyagent` (dans le PATH) | binaire du moteur agent |
| `MCAI_TASKFLOW_LOCAL_AGENT_ARGS` | `--task-config <file>` | arguments du moteur |
| `MCAI_TASKFLOW_LOCAL_SHELL` | `$SHELL` puis `/bin/sh` | shell des terminaux |
| `MCAI_TASKFLOW_LOCAL_HOST_ID` | `local-<hostname>` | ID de la « VM hôte » |
| `MCAI_INIT_TEAM_EMAIL` / `PASSWORD` | — | admin qui « possède » l'hôte local (recommandé : l'hôte est alors visible par toute l'équipe) |
| `MCAI_TASKFLOW_LOCAL_KEEP_WORKSPACE_ON_DELETE` | `false` | conserver le workspace après suppression de la tâche |

## Auto-enregistrement de l'hôte

Le flux de création de tâche (`PrepareCreate`) exige une ligne `host` en base
(`host.ID = HostID`). En mode local, le backend **s'enregistre lui-même** au
démarrage (`pkg/localhost.EnsureHost`, appelé depuis `cmd/server/main.go`) :

- propriétaire : utilisateur init-team (`MCAI_INIT_TEAM_EMAIL`) si configuré,
  sinon premier admin, sinon premier utilisateur (avec warning) ;
- la ligne est upsertée (idempotent) : `local-<hostname>` (ou
  `MCAI_TASKFLOW_LOCAL_HOST_ID`).

## Étapes (feuille de route)

1. **✅ Étape 1 (cette PR)** : config + implémentation locale de
   `taskflow.Clienter` + bascule DI + auto-enregistrement de l'hôte local.
   Le backend complet tourne sur une machine avec Postgres + Redis locaux
   (docker-compose `db`/`redis` ou installs natives) et **sans**
   taskflow / rustfs / ingress / clickhouse / loki (ces derniers sont déjà
   no-op quand leur adresse est vide).
2. **✅ Étape 2 (faite)** : Postgres optionnel (`MCAI_DATABASE_DRIVER=sqlite`,
   fichier `~/.nemesiscode/nemesiscode.db`, auto-migration ent — les fichiers
   `migration/*.sql` restent en dialecte Postgres) et Redis optionnel
   (host vide → serveur Redis en mémoire `miniredis` intégré au processus,
   sessions/tasker/notifications inclus). **Stockage objet local**
   (`MCAI_OBJECT_STORAGE_PROVIDER=local`) : avatars, pièces jointes et
   presigns servis depuis le disque (`~/.nemesiscode/uploads`) via
   `/api/v1/assets` et la route PUT `/api/v1/uploader/direct` — aucun
   rustfs/S3 requis.
3. **✅ Étape 3 (faite)** : `scripts/nemesis-local.sh` (start/stop/status/logs)
   + `docs/local-setup.md` (installation Termux/Linux).
4. **✅ Étape 4 (faite)** : le mode local pilote le vrai moteur `ohmyagent`
   via son protocole natif `--stdio` (JSON-RPC ligne par ligne) : handshake
   `system/ready`, `session/create` + `session/sendMessage`, notifications
   `event/stream` mappées en chunks ACP, auto-approve des permissions,
   `turn/stopped` → `task-ended`. Le binaire `ohmyagent` reste à fournir
   (boutique privée / build du dépôt `chaitin/OhMyAgent`).

## Limites connues (v1)

- Terminal sans PTY : pas de resize, pas de couleurs TUI avancées (pipes).
  Le `Shell` local est *ctx-aware* : `BlockRead` se débloque quand le ctx du
  handler WS est annulé (déconnexion navigateur), ce qui permet au handler de
  retourner et de stopper proprement le process shell (`defer shell.Stop()`).
- Pas d'isolation : l'agent tourne avec les droits de l'utilisateur qui lance
  le backend (c'est le principe du mode local — à réserver à une machine de
  confiance).
- `AutoApprove` / `AskUserQuestion` : journalisés, pas encore transmis au
  moteur (étape 4).
- `TaskLive` : buffer en mémoire (pas de persistance Loki/ClickHouse) ; le
  frontend rejoue l'historique via `flush=true` tant que le processus vit.
