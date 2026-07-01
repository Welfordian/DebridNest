#!/usr/bin/env bash
# DebridNest one-line installer
#   curl -fsSL https://raw.githubusercontent.com/Welfordian/DebridNest/main/scripts/install.sh | bash
#
# Optional environment variables:
#   DEBRIDNEST_REPO          Git clone URL (default: https://github.com/Welfordian/DebridNest.git)
#   DEBRIDNEST_INSTALL_DIR   Install directory (default: $HOME/debridnest)
#   DEBRIDNEST_PROFILE       Compose profile (default: stremio)
#   DEBRIDNEST_API_TOKEN     Skip prompt when set

set -euo pipefail

REPO="${DEBRIDNEST_REPO:-https://github.com/Welfordian/DebridNest.git}"
INSTALL_DIR="${DEBRIDNEST_INSTALL_DIR:-$HOME/debridnest}"
PROFILE="${DEBRIDNEST_PROFILE:-stremio}"

info() { printf '==> %s\n' "$*"; }
warn() { printf '!!> %s\n' "$*" >&2; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Required command not found: $1" >&2
    exit 1
  }
}

prompt_token() {
  if [[ -n "${DEBRIDNEST_API_TOKEN:-}" ]]; then
    return 0
  fi

  local suggested
  suggested="$(openssl rand -hex 32 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"

  if [[ -t 0 ]]; then
    read -r -p "DEBRIDNEST_API_TOKEN [random]: " DEBRIDNEST_API_TOKEN
  elif [[ -r /dev/tty ]]; then
    read -r -p "DEBRIDNEST_API_TOKEN [random]: " DEBRIDNEST_API_TOKEN </dev/tty
  else
    warn "Non-interactive shell without DEBRIDNEST_API_TOKEN; using generated token."
  fi

  DEBRIDNEST_API_TOKEN="${DEBRIDNEST_API_TOKEN:-$suggested}"
  export DEBRIDNEST_API_TOKEN
}

write_env() {
  local env_file="$1"
  if [[ -f "$env_file" ]]; then
    info "Keeping existing $env_file"
    return 0
  fi

  cp .env.example "$env_file"
  if grep -q '^DEBRIDNEST_API_TOKEN=' "$env_file"; then
    sed -i.bak "s|^DEBRIDNEST_API_TOKEN=.*|DEBRIDNEST_API_TOKEN=${DEBRIDNEST_API_TOKEN}|" "$env_file"
    rm -f "${env_file}.bak"
  else
    echo "DEBRIDNEST_API_TOKEN=${DEBRIDNEST_API_TOKEN}" >>"$env_file"
  fi
  info "Created $env_file"
}

main() {
  need_cmd git
  need_cmd docker

  if ! docker compose version >/dev/null 2>&1; then
    need_cmd docker-compose
    COMPOSE=(docker-compose)
  else
    COMPOSE=(docker compose)
  fi

  info "Installing DebridNest to $INSTALL_DIR"
  if [[ -d "$INSTALL_DIR/.git" ]]; then
    info "Updating existing clone"
    git -C "$INSTALL_DIR" pull --ff-only
  else
    mkdir -p "$(dirname "$INSTALL_DIR")"
    git clone "$REPO" "$INSTALL_DIR"
  fi

  cd "$INSTALL_DIR"
  prompt_token
  write_env .env

  info "Starting DebridNest (profile: $PROFILE)"
  "${COMPOSE[@]}" --profile "$PROFILE" up -d --build

  cat <<EOF

DebridNest is running.

  Dashboard:  http://localhost:8080/dashboard/
  API token:  $DEBRIDNEST_API_TOKEN
EOF

  if [[ "$PROFILE" == "stremio" ]]; then
    cat <<EOF
  Stremio:    http://127.0.0.1:7001/configure
  Jackett:    http://localhost:9117
EOF
  fi

  cat <<EOF

Edit $INSTALL_DIR/.env and restart with:
  cd $INSTALL_DIR && ${COMPOSE[*]} --profile $PROFILE up -d
EOF
}

main "$@"
