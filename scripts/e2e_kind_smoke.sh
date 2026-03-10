#!/usr/bin/env bash
set -euo pipefail

BINARY_PATH="${1:-./bin/khelper}"
NAMESPACE_MAIN="shop"
NAMESPACE_SECONDARY="ops"
APP_NAME="payment"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

apply_demo_deployment() {
  ns="$1"
  label_value="${APP_NAME}-${ns}"

  cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${APP_NAME}
  namespace: ${ns}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${label_value}
  template:
    metadata:
      labels:
        app: ${label_value}
    spec:
      containers:
        - name: app
          image: busybox:1.36.1
          command: ["/bin/sh", "-c"]
          args:
            - |
              i=0
              while true; do
                echo "tick namespace=${ns} app=${APP_NAME} i=\$i"
                i=\$((i+1))
                sleep 1
              done
EOF

  kubectl -n "${ns}" rollout status "deployment/${APP_NAME}" --timeout=180s
}

assert_contains() {
  needle="$1"
  haystack="$2"
  context="$3"
  if ! printf '%s' "$haystack" | grep -q "$needle"; then
    echo "$haystack"
    die "expected ${context} to contain: ${needle}"
  fi
}

main() {
  require_cmd kubectl
  require_cmd "$BINARY_PATH"

  if [ ! -x "$BINARY_PATH" ]; then
    die "khelper binary is not executable: $BINARY_PATH"
  fi

  kubectl create namespace "$NAMESPACE_MAIN" --dry-run=client -o yaml | kubectl apply -f -
  kubectl create namespace "$NAMESPACE_SECONDARY" --dry-run=client -o yaml | kubectl apply -f -

  apply_demo_deployment "$NAMESPACE_MAIN"
  apply_demo_deployment "$NAMESPACE_SECONDARY"

  sleep 3

  pods_out="$("$BINARY_PATH" --namespace "$NAMESPACE_MAIN" pods "$APP_NAME")"
  echo "$pods_out"
  assert_contains "$APP_NAME" "$pods_out" "pods output"

  logs_out="$("$BINARY_PATH" --namespace "$NAMESPACE_MAIN" logs "$APP_NAME" --tail=5)"
  echo "$logs_out"
  assert_contains "tick namespace=${NAMESPACE_MAIN}" "$logs_out" "logs output"

  events_out="$("$BINARY_PATH" --namespace "$NAMESPACE_MAIN" events "$APP_NAME" --since=1h)"
  echo "$events_out"

  "$BINARY_PATH" --namespace "$NAMESPACE_MAIN" restart "$APP_NAME" --timeout=2m

  status_json="$("$BINARY_PATH" --namespace "$NAMESPACE_MAIN" rollout status "$APP_NAME" -o json)"
  echo "$status_json"
  assert_contains '"complete": true' "$status_json" "rollout status json"

  set +e
  ambiguous_out="$("$BINARY_PATH" pods "$APP_NAME" -A 2>&1)"
  ambiguous_code=$?
  set -e
  if [ "$ambiguous_code" -ne 3 ]; then
    echo "$ambiguous_out"
    die "expected ambiguous exit code 3, got ${ambiguous_code}"
  fi

  set +e
  invalid_pick_out="$("$BINARY_PATH" pods "$APP_NAME" -A --kind=deployment --pick=99 2>&1)"
  invalid_pick_code=$?
  set -e
  if [ "$invalid_pick_code" -ne 4 ]; then
    echo "$invalid_pick_out"
    die "expected invalid-pick usage exit code 4, got ${invalid_pick_code}"
  fi

  set +e
  doctor_out="$("$BINARY_PATH" --namespace "$NAMESPACE_MAIN" doctor "$APP_NAME" --since=30m --logs-tail=20 -o json 2>&1)"
  doctor_code=$?
  set -e
  echo "$doctor_out"
  if [ "$doctor_code" -ne 0 ] && [ "$doctor_code" -ne 6 ]; then
    die "doctor returned unexpected exit code ${doctor_code}"
  fi
}

main "$@"
