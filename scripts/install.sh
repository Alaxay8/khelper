#!/usr/bin/env sh
set -eu

BINARY_NAME="khelper"
INSTALL_DIR="/usr/local/bin"
REPO="alexey/khelper"
VERSION="latest"
MODE="auto" # auto | local | release
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)"

is_runnable_binary() {
  candidate="$1"
  chmod +x "$candidate" >/dev/null 2>&1 || true
  "$candidate" version >/dev/null 2>&1
}

usage() {
  cat <<USAGE
Usage: $0 [options]

Install khelper for the current OS/ARCH (linux/darwin, amd64/arm64).

Options:
  --install-dir <dir>   Install directory (default: /usr/local/bin)
  --repo <owner/repo>   GitHub repo for release downloads (default: alexey/khelper)
  --version <tag|latest> Release tag or 'latest' (default: latest)
  --mode <auto|local|release>
                        auto: use local asset if present, otherwise GitHub release
                        local: require local asset only (no download)
                        release: download from GitHub release
  --help                Show this help

Examples:
  $0
  $0 --mode local
  $0 --version v0.1.0
  $0 --install-dir /opt/homebrew/bin
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-dir)
      INSTALL_DIR="$2"
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
    --mode)
      MODE="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

case "$MODE" in
  auto|local|release) ;;
  *)
    echo "ERROR: invalid mode '$MODE' (expected: auto|local|release)" >&2
    exit 1
    ;;
esac

OS_RAW="$(uname -s)"
ARCH_RAW="$(uname -m)"

case "$OS_RAW" in
  Linux) OS="linux" ;;
  Darwin) OS="darwin" ;;
  *)
    echo "ERROR: unsupported OS '$OS_RAW' (supported: Linux, Darwin)" >&2
    exit 1
    ;;
esac

case "$ARCH_RAW" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "ERROR: unsupported architecture '$ARCH_RAW' (supported: x86_64/amd64, aarch64/arm64)" >&2
    exit 1
    ;;
esac

ASSET_NAME="${BINARY_NAME}_${OS}_${ARCH}"
LOCAL_ASSET_CWD_DIST="$(pwd)/dist/${ASSET_NAME}"
LOCAL_ASSET_REPO_DIST="${REPO_ROOT}/dist/${ASSET_NAME}"
LOCAL_ASSET_CWD_BIN="$(pwd)/${BINARY_NAME}"
LOCAL_ASSET_REPO_BIN="${REPO_ROOT}/${BINARY_NAME}"

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t khelper-install)"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

SRC=""

if [ "$MODE" = "auto" ] || [ "$MODE" = "local" ]; then
  if [ -f "$LOCAL_ASSET_CWD_DIST" ]; then
    SRC="$LOCAL_ASSET_CWD_DIST"
  elif [ -f "$LOCAL_ASSET_REPO_DIST" ]; then
    SRC="$LOCAL_ASSET_REPO_DIST"
  elif [ -f "$LOCAL_ASSET_CWD_BIN" ]; then
    if is_runnable_binary "$LOCAL_ASSET_CWD_BIN"; then
      SRC="$LOCAL_ASSET_CWD_BIN"
    else
      echo "ERROR: local binary exists but is not runnable on this host: $LOCAL_ASSET_CWD_BIN" >&2
      echo "Check architecture: file $LOCAL_ASSET_CWD_BIN && uname -m" >&2
      exit 1
    fi
  elif [ -f "$LOCAL_ASSET_REPO_BIN" ]; then
    if is_runnable_binary "$LOCAL_ASSET_REPO_BIN"; then
      SRC="$LOCAL_ASSET_REPO_BIN"
    else
      echo "ERROR: local binary exists but is not runnable on this host: $LOCAL_ASSET_REPO_BIN" >&2
      echo "Check architecture: file $LOCAL_ASSET_REPO_BIN && uname -m" >&2
      exit 1
    fi
  elif [ "$MODE" = "local" ]; then
    echo "ERROR: local asset not found for '${ASSET_NAME}'." >&2
    echo "Checked paths:" >&2
    echo "  - ${LOCAL_ASSET_CWD_DIST}" >&2
    echo "  - ${LOCAL_ASSET_REPO_DIST}" >&2
    echo "  - ${LOCAL_ASSET_CWD_BIN}" >&2
    echo "  - ${LOCAL_ASSET_REPO_BIN}" >&2
    echo "Build release artifacts first: make release" >&2
    exit 1
  fi
fi

if [ -z "$SRC" ]; then
  if ! command -v curl >/dev/null 2>&1; then
    echo "ERROR: curl is required to download release assets" >&2
    exit 1
  fi

  if [ "$VERSION" = "latest" ]; then
    URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"
  else
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET_NAME}"
  fi

  SRC="${TMP_DIR}/${BINARY_NAME}"
  echo "Downloading ${URL}"
  if ! curl -fsSL "$URL" -o "$SRC"; then
    echo "ERROR: failed to download release asset '${ASSET_NAME}' from ${URL}" >&2
    echo "Hint: if you already built the binary locally, use --mode local." >&2
    echo "Expected local files:" >&2
    echo "  - ${LOCAL_ASSET_REPO_DIST}" >&2
    echo "  - ${LOCAL_ASSET_REPO_BIN}" >&2
    exit 1
  fi
fi

chmod +x "$SRC"
DEST="${INSTALL_DIR}/${BINARY_NAME}"

if [ ! -d "$INSTALL_DIR" ]; then
  if mkdir -p "$INSTALL_DIR" 2>/dev/null; then
    :
  elif command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p "$INSTALL_DIR"
  else
    echo "ERROR: cannot create install dir '$INSTALL_DIR' (try running as root)" >&2
    exit 1
  fi
fi

if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "$SRC" "$DEST"
elif command -v sudo >/dev/null 2>&1; then
  sudo install -m 0755 "$SRC" "$DEST"
else
  echo "ERROR: no write permission to '$INSTALL_DIR' and sudo is unavailable" >&2
  exit 1
fi

echo "Installed ${BINARY_NAME} to ${DEST}"
"$DEST" version || true
