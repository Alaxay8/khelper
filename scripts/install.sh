#!/usr/bin/env sh
set -eu

BINARY_NAME="khelper"
MODE="auto" # auto | local | build | release
INSTALL_DIR=""
REPO="alexey/khelper"
VERSION="latest"

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)"

usage() {
  cat <<USAGE
Usage: $0 [options]

Install khelper for the current OS/ARCH.

Default behavior (mode=auto):
  1) use local binary artifacts
  2) build from source with Go (if available)
  3) do NOT download from GitHub

Modes:
  auto    local artifacts, then local build
  local   local artifacts only
  build   local build only
  release download from GitHub Releases only

Options:
  --install-dir <dir>      Install directory (default: OS-dependent)
  --mode <auto|local|build|release>
  --repo <owner/repo>      GitHub repo for release downloads (default: alexey/khelper)
  --version <tag|latest>   Release tag or 'latest' (default: latest)
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

runnable_binary() {
  candidate="$1"
  chmod +x "$candidate" >/dev/null 2>&1 || true
  "$candidate" version >/dev/null 2>&1
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
    --help|-h)
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
  *) die "unsupported OS '$OS_RAW' (supported: Linux, Darwin)" ;;
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
  - $LOCAL_ASSET_REPO_BIN
Tip: build on a machine with Go:
  GOOS=$OS GOARCH=$ARCH CGO_ENABLED=0 go build -o khelper ."
  fi
fi

if [ -z "$SRC" ] && { [ "$MODE" = "auto" ] || [ "$MODE" = "build" ]; }; then
  if ! command -v go >/dev/null 2>&1; then
    if [ "$MODE" = "build" ]; then
      die "Go is required for --mode build"
    fi
  elif [ ! -f "${REPO_ROOT}/go.mod" ]; then
    if [ "$MODE" = "build" ]; then
      die "go.mod not found under ${REPO_ROOT}"
    fi
  else
    BUILD_OUT="${TMP_DIR}/${BINARY_NAME}"
    echo "Building ${BINARY_NAME} for ${OS}/${ARCH} from source..."
    (
      cd "$REPO_ROOT"
      GOOS="$OS" GOARCH="$ARCH" CGO_ENABLED=0 go build -o "$BUILD_OUT" .
    )
    if ! runnable_binary "$BUILD_OUT"; then
      die "built binary is not runnable: $BUILD_OUT"
    fi
    SRC="$BUILD_OUT"
  fi
fi

if [ -z "$SRC" ] && [ "$MODE" = "release" ]; then
  command -v curl >/dev/null 2>&1 || die "curl is required for release downloads"
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

if [ -z "$SRC" ]; then
  die "no install source found. Use one of:
  1) Put a runnable binary at ${LOCAL_ASSET_REPO_BIN}
  2) Build artifacts in dist/: make release
  3) Install Go and run this script (mode=auto/build)
  4) Use --mode release after publishing GitHub release assets"
fi

DEST="${INSTALL_DIR}/${BINARY_NAME}"

if [ ! -d "$INSTALL_DIR" ]; then
  if mkdir -p "$INSTALL_DIR" 2>/dev/null; then :
  elif command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p "$INSTALL_DIR"
  else
    die "cannot create install dir '$INSTALL_DIR' (try root/sudo or --install-dir)"
  fi
fi

if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "$SRC" "$DEST"
elif command -v sudo >/dev/null 2>&1; then
  sudo install -m 0755 "$SRC" "$DEST"
else
  die "no write permission to '$INSTALL_DIR' and sudo is unavailable"
fi

echo "Installed ${BINARY_NAME} to ${DEST}"
"$DEST" version || true
