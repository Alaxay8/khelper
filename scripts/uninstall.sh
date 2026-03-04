#!/usr/bin/env sh
set -eu

BINARY_NAME="khelper"
WORKDIR="/opt/khelper"
PURGE_CONFIG="true"
PURGE_ARTIFACTS="true"

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)"

usage() {
  cat <<USAGE
Usage: $0 [options]

Remove khelper binaries and related files.

Options:
  --workdir <dir>      Extra workdir to clean artifacts from (default: /opt/khelper)
  --minimal            Remove binaries and completions only
  --no-config          Do not remove ~/.khelper.yaml
  --no-artifacts       Do not remove local build artifacts (khelper, bin/, dist/)
  --help               Show this help

Examples:
  $0
  $0 --minimal
  $0 --workdir /srv/khelper
USAGE
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

run_privileged() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif has_cmd sudo; then
    sudo "$@"
  else
    die "need root privileges for: $*"
  fi
}

removed=0

remove_path() {
  p="$1"
  if [ ! -e "$p" ] && [ ! -L "$p" ]; then
    return 0
  fi

  parent="$(dirname "$p")"

  if [ -w "$parent" ] || [ -w "$p" ]; then
    rm -rf -- "$p"
  else
    run_privileged rm -rf -- "$p"
  fi

  echo "Removed $p"
  removed=$((removed + 1))
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --workdir)
      WORKDIR="$2"
      shift 2
      ;;
    --minimal)
      PURGE_CONFIG="false"
      PURGE_ARTIFACTS="false"
      shift
      ;;
    --no-config)
      PURGE_CONFIG="false"
      shift
      ;;
    --no-artifacts)
      PURGE_ARTIFACTS="false"
      shift
      ;;
    --help|-h|help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

remove_path "/usr/local/bin/${BINARY_NAME}"
remove_path "/usr/bin/${BINARY_NAME}"
remove_path "/opt/homebrew/bin/${BINARY_NAME}"

if [ -n "${HOME:-}" ]; then
  remove_path "$HOME/.local/bin/${BINARY_NAME}"
  remove_path "$HOME/bin/${BINARY_NAME}"
  remove_path "$HOME/.local/share/bash-completion/completions/${BINARY_NAME}"
  remove_path "$HOME/.zfunc/_${BINARY_NAME}"
  remove_path "$HOME/.config/fish/completions/${BINARY_NAME}.fish"

  if [ "$PURGE_CONFIG" = "true" ]; then
    remove_path "$HOME/.khelper.yaml"
  fi
fi

remove_path "/etc/bash_completion.d/${BINARY_NAME}"
remove_path "/usr/local/etc/bash_completion.d/${BINARY_NAME}"

if [ "$PURGE_ARTIFACTS" = "true" ]; then
  remove_path "${REPO_ROOT}/${BINARY_NAME}"
  remove_path "${REPO_ROOT}/bin"
  remove_path "${REPO_ROOT}/dist"

  remove_path "${WORKDIR}/${BINARY_NAME}"
  remove_path "${WORKDIR}/bin"
  remove_path "${WORKDIR}/dist"
fi

if has_cmd hash; then
  hash -r 2>/dev/null || true
fi

echo "Done. Removed ${removed} path(s)."
