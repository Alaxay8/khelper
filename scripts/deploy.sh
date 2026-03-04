#!/usr/bin/env bash
set -euo pipefail

REMOTE_DIR="/opt/khelper"
INSTALL_DIR="/usr/local/bin"
HOSTS_FILE=""
USE_SUDO="false"
GO_BIN="${GO_BIN:-go}"
HOSTS=()

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd -P)"
BUILD_DIR="${TMPDIR:-/tmp}/khelper-deploy-$RANDOM-$$"

usage() {
  cat <<USAGE
Usage: $0 [options] <host1> [host2 ...]

Build and deploy khelper to one or more hosts in one command.
For each host, script detects uname -s/uname -m, builds matching binary,
uploads it to remote host and installs into PATH.

Options:
  --hosts-file <path>   File with hosts (one per line, '#' comments supported)
  --remote-dir <dir>    Remote staging dir (default: /opt/khelper)
  --install-dir <dir>   Remote install dir (default: /usr/local/bin)
  --sudo                Use sudo for remote install/mkdir commands
  --help                Show help

Examples:
  $0 root@node1 root@node2
  $0 --hosts-file ./hosts.txt --sudo
USAGE
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

cleanup() {
  rm -rf "$BUILD_DIR"
}
trap cleanup EXIT INT TERM

map_goos() {
  case "$1" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *) return 1 ;;
  esac
}

map_goarch() {
  case "$1" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    armv7l|armv6l) echo "arm" ;;
    ppc64le) echo "ppc64le" ;;
    s390x) echo "s390x" ;;
    *) return 1 ;;
  esac
}

read_hosts_file() {
  local file="$1"
  [[ -f "$file" ]] || die "hosts file not found: $file"

  while IFS= read -r line; do
    line="${line%%#*}"
    line="${line#${line%%[![:space:]]*}}"
    line="${line%${line##*[![:space:]]}}"
    [[ -z "$line" ]] && continue
    HOSTS+=("$line")
  done < "$file"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    help|h)
      usage
      exit 0
      ;;
    --hosts-file)
      HOSTS_FILE="$2"
      shift 2
      ;;
    --remote-dir)
      REMOTE_DIR="$2"
      shift 2
      ;;
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --sudo)
      USE_SUDO="true"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    --*)
      die "unknown option: $1"
      ;;
    *)
      HOSTS+=("$1")
      shift
      ;;
  esac
done

if [[ -n "$HOSTS_FILE" ]]; then
  read_hosts_file "$HOSTS_FILE"
fi

[[ ${#HOSTS[@]} -gt 0 ]] || die "no hosts provided"
command -v "$GO_BIN" >/dev/null 2>&1 || die "Go not found (set GO_BIN or install Go)"
command -v ssh >/dev/null 2>&1 || die "ssh not found"
command -v scp >/dev/null 2>&1 || die "scp not found"

mkdir -p "$BUILD_DIR"

VERSION="$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)"
COMMIT="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo none)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X 'github.com/alexey/khelper/cmd.Version=${VERSION}' -X 'github.com/alexey/khelper/cmd.Commit=${COMMIT}' -X 'github.com/alexey/khelper/cmd.BuildDate=${DATE}'"

build_binary() {
  local goos="$1"
  local goarch="$2"
  local out="$BUILD_DIR/khelper_${goos}_${goarch}"

  if [[ ! -f "$out" ]]; then
    echo "Building khelper for ${goos}/${goarch}..."
    (
      cd "$REPO_ROOT"
      GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 "$GO_BIN" build -ldflags "$LDFLAGS" -o "$out" .
    )
  fi

  echo "$out"
}

for host in "${HOSTS[@]}"; do
  echo "==> ${host}"
  remote_os_raw="$(ssh "$host" 'uname -s')" || die "failed to query OS for ${host}"
  remote_arch_raw="$(ssh "$host" 'uname -m')" || die "failed to query arch for ${host}"

  remote_goos="$(map_goos "$remote_os_raw")" || die "unsupported OS '$remote_os_raw' on ${host}"
  remote_goarch="$(map_goarch "$remote_arch_raw")" || die "unsupported arch '$remote_arch_raw' on ${host}"

  bin_path="$(build_binary "$remote_goos" "$remote_goarch")"

  if [[ "$USE_SUDO" = "true" ]]; then
    ssh "$host" "sudo mkdir -p '$REMOTE_DIR'" || die "failed to prepare remote dir on ${host}"
  else
    ssh "$host" "mkdir -p '$REMOTE_DIR'" || die "failed to prepare remote dir on ${host}"
  fi

  scp "$bin_path" "${host}:${REMOTE_DIR}/khelper" || die "failed to copy binary to ${host}"

  if [[ "$USE_SUDO" = "true" ]]; then
    ssh "$host" "sudo chmod +x '$REMOTE_DIR/khelper' && sudo install -m 0755 '$REMOTE_DIR/khelper' '$INSTALL_DIR/khelper' && '$INSTALL_DIR/khelper' version" \
      || die "failed to install on ${host}"
  else
    ssh "$host" "chmod +x '$REMOTE_DIR/khelper' && install -m 0755 '$REMOTE_DIR/khelper' '$INSTALL_DIR/khelper' && '$INSTALL_DIR/khelper' version" \
      || die "failed to install on ${host}"
  fi

  echo "Installed on ${host} (${remote_goos}/${remote_goarch})"
  echo

done

echo "Done."
