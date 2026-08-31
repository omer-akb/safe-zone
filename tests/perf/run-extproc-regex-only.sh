#!/usr/bin/env bash

set -euo pipefail

# Prepares the minimal-inspection BYG example, then measures its deterministic
# request path through Envoy, ext_proc, and the mock OpenAI upstream. That
# policy contains no AI semantic validator.

readonly DEMO_NAMESPACE="${TSZ_PERF_NAMESPACE:-tsz-byg-demo}"
readonly GATEWAY_NAMESPACE="${TSZ_PERF_GATEWAY_NAMESPACE:-envoy-gateway-system}"
readonly GATEWAY_NAME="${TSZ_PERF_GATEWAY_NAME:-echo-gateway}"
readonly LOCAL_PORT="${TSZ_PERF_LOCAL_PORT:-18080}"
readonly RESULTS_DIR="${TSZ_PERF_RESULTS_DIR:-test-reports/perf}"
readonly K6_BIN="${K6_BIN:-k6}"
readonly KUBECONFIG_PATH="${TSZ_BYG_KUBECONFIG:-${TMPDIR:-/tmp}/tsz-byg-tools/tsz-byg.kubeconfig}"
readonly PERF_EXAMPLE="examples/bring-your-gateway/01-minimal-inspection"

fail() {
  echo "perf: $*" >&2
  exit 1
}

command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v "${K6_BIN}" >/dev/null 2>&1 || fail "k6 is required (set K6_BIN to its path if needed)"

# The example runner creates or refreshes the pinned Kind environment, deploys
# the ext_proc processor, activates the deterministic policy, and attaches the
# EnvoyExtensionPolicy. It also performs one functional request before load is
# generated, so a benchmark never silently bypasses the processor.
export KUBECONFIG="${KUBECONFIG_PATH}"
TSZ_BYG_KUBECONFIG="${KUBECONFIG_PATH}" \
  examples/bring-your-gateway/shared/run.sh "${PERF_EXAMPLE}"
kubectl cluster-info >/dev/null || fail "the prepared Kubernetes cluster is not reachable"

envoy_service="$(kubectl -n "${GATEWAY_NAMESPACE}" get service \
  -l "gateway.envoyproxy.io/owning-gateway-namespace=${DEMO_NAMESPACE},gateway.envoyproxy.io/owning-gateway-name=${GATEWAY_NAME}" \
  -o jsonpath='{.items[0].metadata.name}')"
[[ -n "${envoy_service}" ]] || fail "Envoy data-plane service for ${DEMO_NAMESPACE}/${GATEWAY_NAME} was not found"

temp_dir="$(mktemp -d)"
port_forward_pid=""
cleanup() {
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
    wait "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${temp_dir}"
}
trap cleanup EXIT INT TERM

kubectl -n "${GATEWAY_NAMESPACE}" port-forward "service/${envoy_service}" "${LOCAL_PORT}:80" \
  >"${temp_dir}/port-forward.log" 2>&1 &
port_forward_pid=$!

for _ in $(seq 1 30); do
  if curl --silent --fail --output /dev/null \
    --header 'Content-Type: application/json' \
    --data '{"model":"mock-openai","messages":[{"role":"user","content":"health check"}]}' \
    "http://127.0.0.1:${LOCAL_PORT}/v1/chat/completions"; then
    break
  fi
  sleep 1
done

kill -0 "${port_forward_pid}" 2>/dev/null || {
  cat "${temp_dir}/port-forward.log" >&2
  fail "Envoy port-forward did not start"
}

mkdir -p "${RESULTS_DIR}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
summary_file="${RESULTS_DIR}/extproc-regex-only-${timestamp}.json"

echo "perf: targeting http://127.0.0.1:${LOCAL_PORT} through ${GATEWAY_NAMESPACE}/${envoy_service}"
echo "perf: writing k6 summary to ${summary_file}"
"${K6_BIN}" run \
  --summary-export "${summary_file}" \
  --env "TSZ_PERF_BASE_URL=http://127.0.0.1:${LOCAL_PORT}" \
  --env "TSZ_PERF_RATE=${TSZ_PERF_RATE:-25}" \
  --env "TSZ_PERF_DURATION=${TSZ_PERF_DURATION:-2m}" \
  --env "TSZ_PERF_PRE_ALLOCATED_VUS=${TSZ_PERF_PRE_ALLOCATED_VUS:-10}" \
  --env "TSZ_PERF_MAX_VUS=${TSZ_PERF_MAX_VUS:-50}" \
  tests/perf/extproc-regex-only.js
