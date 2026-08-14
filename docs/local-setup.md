# NemesisCode en local (machine nue / Termux)

Guide d'installation et de lancement de NemesisCode **sans aucune
infrastructure cloud** : la machine qui exécute le backend devient
l'environnement de développement de l'agent.

## Option rapide : paquet .deb tout-en-un (Linux 64 bits)

Pour un PC Linux x86-64 (y compris vieux PC sans AVX — Core 2 Duo), un
paquet `.deb` prêt à l'emploi est fourni : binaire statique (aucune
dépendance glibc, SQLite 100% Go), frontend compilé servi par le backend,
Redis intégré. **Aucune installation supplémentaire.**

```bash
curl -fLO https://github.com/nemesisastarte-gif/nemesis-agent/raw/v1.2.0/releases/v1.2.0/nemesiscode_1.2.0_amd64.deb
curl -fLO https://github.com/nemesisastarte-gif/nemesis-agent/raw/v1.2.0/releases/v1.2.0/SHA256SUMS
sha256sum -c SHA256SUMS
sudo dpkg -i nemesiscode_1.2.0_amd64.deb
nemesiscode on       # → http://localhost:5000  (Admin / Admin)
nemesiscode off      # arrêt
```

Voir [docs/deb-package.md](./deb-package.md) pour tous les détails
(moteur opencode embarqué, port, désinstallation, reconstruction).

## Prérequis

| Composant | Linux (Debian/Ubuntu) | Termux (Android) |
|---|---|---|
| Go 1.25+ | `apt install golang-go` (ou [téléchargement](https://go.dev/dl/)) | `pkg install golang` |
| Node 22+ | `apt install nodejs npm` | `pkg install nodejs-lts` |
| pnpm | `npm i -g pnpm` | `npm i -g pnpm` |
| Git | `apt install git` | `pkg install git` |
| Compilateur C (pour SQLite) | `apt install gcc` | `pkg install clang` |

Facultatif : PostgreSQL et Redis **si** vous préférez les services externes
au lieu des valeurs par défaut (SQLite + Redis en mémoire).

## Installation

```bash
git clone https://github.com/teteekoue/NemesisCode.git
cd NemesisCode

# Compilation du backend (première fois)
cd backend && go build -o ../.local-runtime/nemesis-server ./cmd/server && cd ..
```

## Lancement en une commande

```bash
scripts/nemesis-local.sh start
```

Le script :

1. compile le backend si nécessaire ;
2. démarre le backend avec :
   - `MCAI_TASKFLOW_MODE=local` — la machine hôte est l'environnement de dev ;
   - `MCAI_DATABASE_DRIVER=sqlite` — base fichier `~/.nemesiscode/nemesiscode.db`
     (aucun PostgreSQL requis) ;
   - `MCAI_REDIS_HOST` vide — **Redis en mémoire intégré** (miniredis) ;
   - compte initial `Admin` / `Admin`
     (surchargeable via `MCAI_INIT_TEAM_EMAIL` / `MCAI_INIT_TEAM_PASSWORD`) ;
   - image d'environnement `local-env` créée pour le compte initial
     (`MCAI_INIT_TEAM_IMAGE`) ;
   - captcha désactivé (`MCAI_SECURITY_CAPTCHA_ENABLED=false`).
3. démarre le frontend Vite sur `http://localhost:5173` (proxy `/api` → backend).

Arrêt : `scripts/nemesis-local.sh stop` · État : `status` · Logs : `logs`.

### Avec PostgreSQL + Redis externes (optionnel)

```bash
MCAI_DATABASE_DRIVER=postgres \
MCAI_DATABASE_MASTER="postgres://user:pass@127.0.0.1:5432/nemesiscode?sslmode=disable" \
MCAI_REDIS_HOST=127.0.0.1 \
scripts/nemesis-local.sh start
```

## Premier lancement manuel (sans le script)

```bash
export MCAI_TASKFLOW_MODE=local
export MCAI_DATABASE_DRIVER=sqlite
export MCAI_REDIS_HOST=                # vide → Redis en mémoire
export MCAI_INIT_TEAM_EMAIL=Admin
export MCAI_INIT_TEAM_PASSWORD=Admin
export MCAI_INIT_TEAM_IMAGE=local-env
export MCAI_SECURITY_CAPTCHA_ENABLED=false
export MCAI_SERVER_ADDR=:8888

cd backend && go run ./cmd/server
```

Puis, dans un autre terminal :

```bash
cd frontend
pnpm install
TARGET=http://127.0.0.1:8888 pnpm run dev:offline -- --host 0.0.0.0 --port 5173
```

## Configuration initiale dans l'interface

1. Ouvrez `http://localhost:5173`, connectez-vous avec le compte initial.
2. **Settings → AI models** : choisissez un provider (OpenAI, NVIDIA NIM,
   Fireworks, Cohere, DeepSeek… ou **Custom**) et renseignez votre API token —
   la liste des modèles du fournisseur se charge automatiquement.
3. **Settings → Images** : l'image `local-env` est disponible (créée par
   l'init-team).
4. Créez un **projet** (ou liez un dépôt Git), puis une **tâche** : l'agent
   s'exécute sur la machine hôte dans `~/.nemesiscode/workspaces/<tâche>`.
5. Le **terminal**, les **fichiers** et les **diffs** de l'interface web
   agissent directement sur la machine hôte.

## Le moteur agent : opencode (embarqué dans le .deb)

Le moteur d'exécution des tâches est **opencode** (https://github.com/anomalyco/opencode,
MIT) — l'agent de codage open source d'origine de ce projet. Le paquet .deb
`nemesiscode_*_amd64.deb` **embarque le binaire** (variante « baseline »
compatible vieux processeurs : SSE2 suffit, aucun AVX requis) : aucune
installation supplémentaire. En mode développement (sans .deb), le backend
cherche `opencode` dans le PATH ou via `MCAI_TASKFLOW_LOCAL_AGENT_BIN`.

Contrat piloté par le backend :

```text
opencode run --format json --auto [--continue] --model nemesiscode-ai/<modèle> "<message>"
```

- `--format json` : événements NDJSON → événements ACP du frontend ;
- `--auto` : auto-approve les permissions (mode local « confiance ») ;
- `--continue` : reprise de la session du workspace (flux « continuer ») ;
- config LLM : `<workspace>/opencode.json` écrit par le backend
  (provider `nemesiscode-ai` → base_url + api_key du modèle de l'UI).

Détails : [docs/deb-package-engine.md](./deb-package-engine.md).

## Variables utiles

| Variable | Défaut | Rôle |
|---|---|---|
| `MCAI_TASKFLOW_MODE` | `remote` | `local` = machine hôte comme environnement de dev |
| `MCAI_DATABASE_DRIVER` | `postgres` | `sqlite` pour une base fichier sans service |
| `MCAI_DATABASE_SQLITE_PATH` | `~/.nemesiscode/nemesiscode.db` | chemin de la base SQLite |
| `MCAI_REDIS_HOST` | (vide) | vide = Redis en mémoire intégré |
| `MCAI_TASKFLOW_LOCAL_WORKSPACE_ROOT` | `~/.nemesiscode/workspaces` | racine des espaces de travail |
| `MCAI_TASKFLOW_LOCAL_AGENT_BIN` | `opencode` | binaire du moteur agent |
| `MCAI_TASKFLOW_LOCAL_SHELL` | `$SHELL` | shell des terminaux web |
| `MCAI_INIT_TEAM_EMAIL` / `PASSWORD` | `Admin` / `Admin` | compte initial (admin, propriétaire de l'hôte local) |
| `MCAI_INIT_TEAM_IMAGE` | `local-env` | image d'environnement par défaut |
| `MCAI_SECURITY_CAPTCHA_ENABLED` | `true` | `false` recommandé en local |
| `MCAI_OBJECT_STORAGE_PROVIDER` | `s3` | `local` = stockage fichier sur disque (aucun S3/rustfs) |
| `MCAI_OBJECT_STORAGE_LOCAL_DIR` | `~/.nemesiscode/uploads` | répertoire du stockage local |

## Mobile (APK)

L'application mobile (Expo / React Native) se connecte au serveur :

- à la compilation : `EXPO_PUBLIC_API_URL=http://<ip-du-serveur>:8888` ;
- à la connexion : champ « 服务器地址 » (serveur) dans l'écran de login
  (affiché après 6 appuis sur le logo) ;
- l'onglet Profil → « 服务器地址 » affiche le serveur courant.

```bash
cd mobile
pnpm install
EXPO_PUBLIC_API_URL=http://192.168.1.10:8888 npx expo run:android
```

## Limites du mode local

- Le terminal utilise un PTY Linux complet ; sur les plateformes non-Linux, le
  backend conserve un fallback à base de pipes sans redimensionnement natif.
- Redis en mémoire : les files temporaires sont perdues au redémarrage
  (sessions utilisateur et données persistées en base restent intactes).
- En développement depuis les sources, `opencode` doit être installé ou fourni
  via `MCAI_TASKFLOW_LOCAL_AGENT_BIN`. Il est inclus dans le paquet `.deb`.
- Pas d'isolation : l'agent tourne avec les droits de l'utilisateur qui lance
  le backend — à réserver à une machine de confiance.
