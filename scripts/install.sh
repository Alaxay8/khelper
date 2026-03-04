#!/usr/bin/env sh
set -eu

BINARY_NAME="khelper"
MODE="auto"
INSTALL_DIR=""
REPO="alaxay8/khelper"
VERSION="latest"

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)"

usage() {
  cat <<USAGE
Usage: $0 [options]

Install khelper for the current OS and architecture.

Modes:
  auto    local artifacts, then local build (installs Go on supported systems if missing)
  local   local artifacts only
  build   local build only
  release download from GitHub Releases only

Options:
  --install-dir <dir>      Install directory (default: OS-dependent)
  --mode <auto|local|build|release>
  --repo <owner/repo>      GitHub repo for release downloads (default: alaxay8/khelper)
  --version <tag|latest>   Release tag or latest (default: latest)
  --help                   Show this help

Examples:
  $0
  $0 --mode local
  $0 --mode build
  $0 --mode release --version v0.1.0
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

runnable_binary() {
  candidate="$1"
  chmod +x "$candidate" >/dev/null 2>&1 || true
  "$candidate" version >/dev/null 2>&1
}

ensure_go() {
  if has_cmd go; then
    return 0
  fi

  if [ "$OS" = "darwin" ]; then
    if has_cmd brew; then
      run_privileged brew update
      run_privileged brew install go
    else
      die "Go is not installed. Install it manually on macOS: brew install go"
    fi
  elif [ "$OS" = "linux" ]; then
    if has_cmd apt-get; then
      run_privileged apt-get update
      run_privileged apt-get install -y golang-go
    elif has_cmd dnf; then
      run_privileged dnf install -y golang
    elif has_cmd yum; then
      run_privileged yum install -y golang
    elif has_cmd zypper; then
      run_privileged zypper --non-interactive install go
    elif has_cmd apk; then
      run_privileged apk add --no-cache go
    elif has_cmd pacman; then
      run_privileged pacman -Sy --noconfirm go
    else
      die "Go is not installed and no supported package manager was found"
    fi
  else
    die "Go bootstrap is not supported on this OS"
  fi

  if ! has_cmd go; then
    die "failed to install Go"
  fi
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --mode)
      MODE="$2"
      shift 2
      ;;
    --repo)
      REPO="$2"
      shift 2
      ;;
    --version)
      VERSION="$2"
      shift 2
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

case "$MODE" in
  auto|local|build|release) ;;
  *) die "invalid mode '$MODE' (expected: auto|local|build|release)" ;;
esac

OS_RAW="$(uname -s)"
ARCH_RAW="$(uname -m)"

case "$OS_RAW" in
  Linux) OS="linux" ;;
  Darwin) OS="darwin" ;;
  *) die "unsupported OS '$OS_RAW'" ;;
esac

case "$ARCH_RAW" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l|armv6l) ARCH="arm" ;;
  ppc64le) ARCH="ppc64le" ;;
  s390x) ARCH="s390x" ;;
  *) die "unsupported architecture '$ARCH_RAW'" ;;
esac

if [ -z "$INSTALL_DIR" ]; then
  if [ "$OS" = "darwin" ] && [ "$ARCH" = "arm64" ] && [ -d "/opt/homebrew/bin" ]; then
    INSTALL_DIR="/opt/homebrew/bin"
  elif [ -n "${HOME:-}" ] && [ -d "$HOME/.local/bin" ] && [ -w "$HOME/.local/bin" ]; then
    INSTALL_DIR="$HOME/.local/bin"
  else
    INSTALL_DIR="/usr/local/bin"
  fi
fi

ASSET_NAME="${BINARY_NAME}_${OS}_${ARCH}"
LOCAL_ASSET_CWD_DIST="$(pwd)/dist/${ASSET_NAME}"
LOCAL_ASSET_REPO_DIST="${REPO_ROOT}/dist/${ASSET_NAME}"
LOCAL_ASSET_CWD_BIN="$(pwd)/${BINARY_NAME}"
LOCAL_ASSET_REPO_BIN="${REPO_ROOT}/${BINARY_NAME}"

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t khelper-install)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT INT TERM

SRC=""

try_local() {
  candidate="$1"
  if [ ! -f "$candidate" ]; then
    return 1
  fi
  if runnable_binary "$candidate"; then
    SRC="$candidate"
    return 0
  fi
  return 2
}

if [ "$MODE" = "auto" ] || [ "$MODE" = "local" ]; then
  if try_local "$LOCAL_ASSET_CWD_DIST"; then :
  elif try_local "$LOCAL_ASSET_REPO_DIST"; then :
  elif try_local "$LOCAL_ASSET_CWD_BIN"; then :
  elif try_local "$LOCAL_ASSET_REPO_BIN"; then :
  elif [ "$MODE" = "local" ]; then
    die "no runnable local binary found. Checked:
  - $LOCAL_ASSET_CWD_DIST
  - $LOCAL_ASSET_REPO_DIST
  - $LOCAL_ASSET_CWD_BIN
  - $LOCAL_ASSET_REPO_BIN"
  fi
fi

if [ -z "$SRC" ] && { [ "$MODE" = "auto" ] || [ "$MODE" = "build" ]; }; then
  [ -f "${REPO_ROOT}/go.mod" ] || die "go.mod not found under ${REPO_ROOT}"
  ensure_go
  BUILD_OUT="${TMP_DIR}/${BINARY_NAME}"
  echo "Building ${BINARY_NAME} for ${OS}/${ARCH}..."
  (
    cd "$REPO_ROOT"
    GOOS="$OS" GOARCH="$ARCH" CGO_ENABLED=0 go build -o "$BUILD_OUT" .
  )
  runnable_binary "$BUILD_OUT" || die "built binary is not runnable: $BUILD_OUT"
  SRC="$BUILD_OUT"
fi

if [ -z "$SRC" ] && [ "$MODE" = "release" ]; then
  has_cmd curl || die "curl is required for release downloads"
  if [ "$VERSION" = "latest" ]; then
    URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"
  else
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET_NAME}"
  fi
  SRC="${TMP_DIR}/${BINARY_NAME}"
  echo "Downloading ${URL}"
  curl -fsSL "$URL" -o "$SRC" || die "failed to download '${ASSET_NAME}' from ${URL}"
  chmod +x "$SRC"
fi

[ -n "$SRC" ] || die "no install source found"

DEST="${INSTALL_DIR}/${BINARY_NAME}"

if [ ! -d "$INSTALL_DIR" ]; then
  run_privileged mkdir -p "$INSTALL_DIR"
fi

if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "$SRC" "$DEST"
else
  run_privileged install -m 0755 "$SRC" "$DEST"
fi

echo "Installed ${BINARY_NAME} to ${DEST}"
"$DEST" version || true
