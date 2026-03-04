# khelper

`khelper` is a production-oriented Go CLI that complements `kubectl` with shorter, ergonomic commands for common Kubernetes workflows.

It uses `client-go` directly (no shelling out to `kubectl`), reads kubeconfig the same way `kubectl` does, and works on macOS and Linux.

## Features

- Short commands for pods, logs, restart, shell, metrics, context, and namespace workflows
- Deterministic target resolution (`deployment -> statefulset -> pod` by default)
- Config via `~/.khelper.yaml`, environment variables (`KHELPER_*`), and flags
- Table output by default with optional JSON output (`-o json`)
- Colored pod status in TTY mode
- Robust exit codes and `ERROR: ...` formatted failures

## Installation

### Go install

```bash
go install github.com/alaxay8/khelper@latest

BIN_DIR="$(go env GOBIN)"
[ -z "$BIN_DIR" ] && BIN_DIR="$(go env GOPATH)/bin"
ls -l "$BIN_DIR/khelper"
"$BIN_DIR/khelper" version
```

Install system-wide:

```bash
sudo install -m 755 "$BIN_DIR/khelper" /usr/local/bin/khelper
hash -r
khelper version
```

Install without sudo:

```bash
mkdir -p "$HOME/.local/bin"
install -m 755 "$BIN_DIR/khelper" "$HOME/.local/bin/khelper"
echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.bashrc"
source "$HOME/.bashrc"
khelper version
```

### Build locally

```bash
make build
./bin/khelper version
```

### Build all target binaries (macOS + Linux, amd64 + arm64)

```bash
make release
ls -1 dist/
```

### Install helper script (auto-detect OS/ARCH)

The repository includes [scripts/install.sh](./scripts/install.sh). It supports:

- Linux and macOS
- `amd64`, `arm64`, `arm`, `ppc64le`, `s390x` where supported
- Local install from `./dist` or from local `./khelper`
- Source build (`go build`) with Go auto-install on supported Linux package managers
- Optional GitHub Releases download in explicit release mode

Examples:

```bash
./scripts/install.sh
./scripts/install.sh --mode local
./scripts/install.sh --mode build
./scripts/install.sh --mode release --version v0.1.0
```

On Linux, `auto` mode can install Go via package manager (`apt`, `dnf`, `yum`, `zypper`, `apk`, `pacman`) if Go is missing.
It requires root/sudo privileges and internet access.

You can also run:

```bash
make install
```

### Uninstall

```bash
./scripts/uninstall.sh
```

Minimal uninstall (keep config and local build artifacts):

```bash
./scripts/uninstall.sh --minimal
```

### Troubleshooting

`khelper: command not found` after `go install`:

```bash
BIN_DIR="$(go env GOBIN)"
[ -z "$BIN_DIR" ] && BIN_DIR="$(go env GOPATH)/bin"
echo "$BIN_DIR"
ls -l "$BIN_DIR/khelper"
```

`cannot execute binary file: Exec format error`:

- Binary OS/architecture does not match host.
- Check with:

```bash
uname -s
uname -m
file "$(command -v khelper || echo /usr/local/bin/khelper)"
```

- Rebuild/install for the correct target architecture.

## Configuration

Optional file: `~/.khelper.yaml`

```yaml
kubeconfig: /Users/you/.kube/config
context: dev-cluster
namespace: shop
output: table
```

Environment variables are also supported:

- `KHELPER_KUBECONFIG`
- `KHELPER_CONTEXT`
- `KHELPER_NAMESPACE`
- `KHELPER_OUTPUT`
- `KHELPER_VERBOSE`

## Global Flags

- `--kubeconfig string`
- `--context string`
- `--namespace, -n string`
- `--verbose`
- `--output, -o table|json`

## Commands

### Version

```bash
khelper version
```

### Contexts

```bash
khelper ctx list
khelper ctx use dev-cluster
```

### Namespaces

```bash
khelper ns list
khelper ns use shop
```

### Pods

```bash
khelper pods payment
khelper pods payment --wide
khelper pods payment --kind=deployment --pick=2
khelper pods payment -o json
```

### Logs

```bash
khelper logs payment
khelper logs payment --follow --since=10m --tail=200
khelper logs payment --container api
khelper logs payment --all-containers --follow
```

### Restart

```bash
khelper restart payment
khelper restart payment --kind=deployment --timeout=10m
```

### Shell

```bash
khelper shell payment
khelper shell payment --container api
khelper shell payment --command sh --tty
```

### Top (metrics)

```bash
khelper top
khelper top --pods
khelper top --nodes
khelper top --namespace shop --pods
```

`khelper top` requires the Kubernetes Metrics API (`metrics.k8s.io`).
If metrics are unavailable, install/configure [metrics-server](https://github.com/kubernetes-sigs/metrics-server).

## Target Resolution Rules

Given a target like `payment`:

1. If `--kind` is set, resolution is restricted to that kind.
2. Default kind order is: `Deployment -> StatefulSet -> Pod`.
3. Namespace resolution order: `--namespace`, current context namespace from kubeconfig, then `default`.
4. Matching order per kind: `metadata.name == target`, then selector `app=<target>`, then selector `app.kubernetes.io/name=<target>`.
5. Multiple matches require `--pick=N`.
6. For logs/shell pod resolution, choose newest `Running` pod by `startTime`; if none are running, choose the newest pod and warn.

## Development

```bash
make lint
make test
make build
make release
```

## Exit Codes

- `0` success
- `1` general error
- `2` target/context not found
- `3` ambiguous target (requires `--pick`)
- `4` usage/config error
- `5` unavailable dependency (for example metrics API not installed)
