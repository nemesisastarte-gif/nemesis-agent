#!/usr/bin/env bash
# NemesisCode — lancement local : la machine hôte devient l'environnement de
# développement de l'agent. Aucune infrastructure cloud requise.
#
#   scripts/nemesis-local.sh start    # backend (:8888) + frontend (:5173)
#   scripts/nemesis-local.sh stop
#   scripts/nemesis-local.sh status
#   scripts/nemesis-local.sh logs
#
# Par défaut : base SQLite + Redis en mémoire (aucun service externe).
# Surchargez via l'environnement :
#   MCAI_DATABASE_DRIVER=postgres MCAI_DATABASE_MASTER="postgres://..." \
#   MCAI_REDIS_HOST=127.0.0.1 scripts/nemesis-local.sh start
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_DIR="$ROOT/.local-runtime"
BACKEND_LOG="$RUNTIME_DIR/backend.log"
FRONTEND_LOG="$RUNTIME_DIR/frontend.log"
PID_FILE="$RUNTIME_DIR/pids"
BACKEND_BIN="$RUNTIME_DIR/nemesis-server"
FRONTEND_PORT="${FRONTEND_PORT:-5173}"
BACKEND_ADDR="${MCAI_SERVER_ADDR:-:8888}"
BACKEND_PORT="${BACKEND_PORT:-8888}"

default_env() {
  export MCAI_TASKFLOW_MODE="${MCAI_TASKFLOW_MODE:-local}"
  export MCAI_DATABASE_DRIVER="${MCAI_DATABASE_DRIVER:-sqlite}"
  export MCAI_REDIS_HOST="${MCAI_REDIS_HOST:-}"          # vide → Redis en mémoire intégré
  export MCAI_INIT_TEAM_EMAIL="${MCAI_INIT_TEAM_EMAIL:-Admin}"
  export MCAI_INIT_TEAM_PASSWORD="${MCAI_INIT_TEAM_PASSWORD:-Admin}"
  export MCAI_INIT_TEAM_NAME="${MCAI_INIT_TEAM_NAME:-NemesisCode Admin}"
  export MCAI_INIT_TEAM_IMAGE="${MCAI_INIT_TEAM_IMAGE:-local-env}"
  export MCAI_SECURITY_CAPTCHA_ENABLED="${MCAI_SECURITY_CAPTCHA_ENABLED:-false}"
  export MCAI_SERVER_ADDR="${MCAI_SERVER_ADDR:-$BACKEND_ADDR}"
  export MCAI_ROOT_PATH="${MCAI_ROOT_PATH:-$RUNTIME_DIR}"
  export MCAI_LOGGER_LEVEL="${MCAI_LOGGER_LEVEL:-info}"
  # Stockage fichier local (avatars, pièces jointes…) — aucun S3/rustfs requis.
  export MCAI_OBJECT_STORAGE_ENABLED="${MCAI_OBJECT_STORAGE_ENABLED:-true}"
  export MCAI_OBJECT_STORAGE_PROVIDER="${MCAI_OBJECT_STORAGE_PROVIDER:-local}"
  export MCAI_OBJECT_STORAGE_LOCAL_DIR="${MCAI_OBJECT_STORAGE_LOCAL_DIR:-$RUNTIME_DIR/uploads}"
  # Le vrai moteur ohmyagent (binaire fourni séparément — protocole --stdio).
  export MCAI_TASKFLOW_LOCAL_AGENT_BIN="${MCAI_TASKFLOW_LOCAL_AGENT_BIN:-ohmyagent}"
  export MCAI_TASKFLOW_LOCAL_PERMISSION_MODE="${MCAI_TASKFLOW_LOCAL_PERMISSION_MODE:-yolo}"
}

is_running() {
  [ -f "$PID_FILE" ] && kill -0 "$(head -1 "$PID_FILE")" 2>/dev/null
}

cmd_start() {
  if is_running; then
    echo "NemesisCode est déjà lancé (PID $(head -1 "$PID_FILE"))."
    return 0
  fi

  mkdir -p "$RUNTIME_DIR"
  default_env

  echo "==> Compilation du backend (Go)…"
  (cd "$ROOT/backend" && go build -o "$BACKEND_BIN" ./cmd/server)

  echo "==> Démarrage du backend sur $MCAI_SERVER_ADDR"
  echo "    DB : $MCAI_DATABASE_DRIVER | Redis : ${MCAI_REDIS_HOST:-mémoire intégrée}"
  "$BACKEND_BIN" >"$BACKEND_LOG" 2>&1 &
  local backend_pid=$!

  echo "==> Démarrage du frontend (Vite) sur :$FRONTEND_PORT"
  (cd "$ROOT/frontend" && [ -d node_modules ] || pnpm install --frozen-lockfile)
  TARGET="http://127.0.0.1:$BACKEND_PORT" pnpm --dir "$ROOT/frontend" \
    run dev:offline -- --host 0.0.0.0 --port "$FRONTEND_PORT" >"$FRONTEND_LOG" 2>&1 &
  local frontend_pid=$!

  printf '%s\n%s\n' "$backend_pid" "$frontend_pid" > "$PID_FILE"

  # Attente du backend
  for _ in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:$BACKEND_PORT/health" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done

  echo
  echo "✅ NemesisCode est prêt :"
  echo "   Frontend : http://localhost:$FRONTEND_PORT"
  echo "   Backend  : http://localhost:$BACKEND_PORT"
  echo "   Connexion : identifiant ${MCAI_INIT_TEAM_EMAIL} / mot de passe ${MCAI_INIT_TEAM_PASSWORD}"
  echo "   Espaces de travail : ~/.nemesiscode/workspaces"
  echo "   Logs : $RUNTIME_DIR"
  echo
  echo "⚠️  Le moteur agent (MCAI_TASKFLOW_LOCAL_AGENT_BIN) doit être installé"
  echo "    pour que les tâches démarrent."
}

cmd_stop() {
  if [ ! -f "$PID_FILE" ]; then
    echo "NemesisCode n'est pas lancé."
    return 0
  fi
  while read -r pid; do
    kill "$pid" 2>/dev/null || true
  done < "$PID_FILE"
  rm -f "$PID_FILE"
  echo "NemesisCode arrêté."
}

cmd_status() {
  if is_running; then
    echo "NemesisCode : en cours (PID $(head -1 "$PID_FILE"))"
  else
    echo "NemesisCode : arrêté"
  fi
}

cmd_logs() {
  tail -f "$BACKEND_LOG" "$FRONTEND_LOG"
}

case "${1:-}" in
  start)  cmd_start ;;
  stop)   cmd_stop ;;
  status) cmd_status ;;
  logs)   cmd_logs ;;
  *)      sed -n '2,12p' "$0" ;;
esac
