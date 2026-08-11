#!/usr/bin/env bash
# Run one request-only BYG example through a real Envoy Gateway route. The
# request body is never printed: examples may deliberately contain synthetic
# sensitive-looking values.
set -euo pipefail

example_dir="${1:?usage: ./run.sh <example-directory>}"
shared_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${shared_dir}/../../.." && pwd)"
namespace="tsz-byg-demo"
kubeconfig="${TSZ_BYG_KUBECONFIG:-${TMPDIR:-/tmp}/tsz-byg-tools/tsz-byg.kubeconfig}"
expected_status="$(<"${example_dir}/expected-status")"
# A prior interrupted run can leave a kubectl port-forward process alive.
# Per-run ports keep a retry from colliding with that process.
port_base="${TSZ_BYG_EXAMPLE_PORT_BASE:-$((20000 + ($$ % 10000)))}"
envoy_port="${port_base}"
mock_port="$((port_base + 1))"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

wait_for_policy_accepted() {
  local resource="$1"
  local statuses

  # Envoy Gateway reports policy acceptance on each target ancestor rather
  # than in the resource's top-level status.conditions array.
  for _ in $(seq 1 60); do
    statuses="$(kubectl -n "${namespace}" get "${resource}" \
      -o jsonpath='{range .status.ancestors[*].conditions[?(@.type=="Accepted")]}{.status}{"\n"}{end}' \
      2>/dev/null || true)"
    if grep -Fxq 'True' <<<"${statuses}"; then
      return 0
    fi
    sleep 1
  done

  echo "Timed out waiting for ${resource} Accepted=True" >&2
  return 1
}

[[ -f "${example_dir}/policy.json" && -f "${example_dir}/request.json" ]] || {
  echo "example must contain policy.json and request.json" >&2
  exit 2
}

if [[ "${TSZ_BYG_SKIP_BOOTSTRAP:-0}" != "1" ]]; then
  # Pass the path explicitly so Kind refreshes it if Docker has reassigned the
  # API-server port of an already-existing cluster.
  TSZ_BYG_KUBECONFIG="${kubeconfig}" \
    "${repo_root}/deployments/envoy-gateway/kind-bootstrap.sh" up
  TSZ_BYG_KUBECONFIG="${kubeconfig}" \
    "${repo_root}/deployments/envoy-gateway/kind-bootstrap.sh" verify-replica-lifecycle
fi
export KUBECONFIG="${kubeconfig}"

name="tsz-example-$(basename "${example_dir}")"
name="${name//[^a-z0-9-]/-}"
kubectl -n "${namespace}" delete job "${name}" --ignore-not-found
kubectl -n "${namespace}" delete configmap "${name}" --ignore-not-found
kubectl -n "${namespace}" create configmap "${name}" --from-file=policy.json="${example_dir}/policy.json"
cat <<EOF | kubectl apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: ${name}
  namespace: ${namespace}
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: activate
          image: thyris-sz:local
          imagePullPolicy: IfNotPresent
          command: ["/app/tsz-policy", "-name", "default", "-file", "/policy/policy.json"]
          env:
            - name: DB_DSN
              value: postgres://postgres:postgres@postgres.tsz-byg-demo.svc.cluster.local:5432/thyris?sslmode=disable&TimeZone=Europe/Istanbul
            - name: REDIS_URL
              value: redis://:thyrisredis@redis.tsz-byg-demo.svc.cluster.local:6379/0
          volumeMounts:
            - name: policy
              mountPath: /policy
              readOnly: true
      volumes:
        - name: policy
          configMap:
            name: ${name}
EOF
kubectl -n "${namespace}" wait --for=condition=complete "job/${name}" --timeout=90s
kubectl apply -f "${repo_root}/deployments/envoy-gateway/tsz-ext-proc-envoy-extension-policy.yaml"
wait_for_policy_accepted envoyextensionpolicy/tsz-request-guardrail
wait_for_policy_accepted clienttrafficpolicy/tsz-route-policy-identity

# Replace the bootstrap's simple nginx response with the safety-preserving
# inspection mock. The mock never retains raw request content.
kubectl -n "${namespace}" set image deployment/mock-openai nginx=thyris-sz:local
kubectl -n "${namespace}" patch deployment/mock-openai --type=strategic -p \
  '{"spec":{"template":{"spec":{"containers":[{"name":"nginx","command":["/app/byg-mock-openai"],"volumeMounts":null}]}}}}'
kubectl -n "${namespace}" rollout status deployment/mock-openai --timeout=90s

if [[ "$(basename "${example_dir}")" == "05-fail-open" || "$(basename "${example_dir}")" == "06-fail-closed" ]]; then
  # This affects only the local example Deployment and lets the stream-pinned
  # failure policy decide after an otherwise safe request is processed.
  kubectl -n "${namespace}" set env deployment/tsz-ext-proc TSZ_EXAMPLE_AUDIT_FAILURE=1
  kubectl -n "${namespace}" rollout status deployment/tsz-ext-proc --timeout=90s
fi

envoy_service="$(kubectl -n envoy-gateway-system get service -l "gateway.envoyproxy.io/owning-gateway-namespace=${namespace},gateway.envoyproxy.io/owning-gateway-name=echo-gateway" -o jsonpath='{.items[0].metadata.name}')"
[[ -n "${envoy_service}" ]] || { echo "Envoy data-plane Service not found" >&2; exit 1; }
work_dir="$(mktemp -d)"
cleanup() {
  [[ -n "${forward_pid:-}" ]] && kill "${forward_pid}" >/dev/null 2>&1 || true
  [[ -n "${mock_forward_pid:-}" ]] && kill "${mock_forward_pid}" >/dev/null 2>&1 || true
  rm -rf "${work_dir}"
}
trap cleanup EXIT
kubectl -n envoy-gateway-system port-forward "service/${envoy_service}" "${envoy_port}:80" >"${work_dir}/port-forward.log" 2>&1 &
forward_pid=$!
kubectl -n "${namespace}" port-forward service/mock-openai "${mock_port}:8080" >"${work_dir}/mock-port-forward.log" 2>&1 &
mock_forward_pid=$!
sleep 2
before_sequence="$(curl --silent "http://127.0.0.1:${mock_port}/inspect" | jq -r '.sequence')"
status="$(curl --silent --output "${work_dir}/response.json" --write-out '%{http_code}' \
  --header 'content-type: application/json' \
  --header 'X-TSZ-Policy: client-must-not-win' \
  --data-binary "@${example_dir}/request.json" \
  "http://127.0.0.1:${envoy_port}/v1/chat/completions")"
[[ "${status}" == "${expected_status}" ]] || {
  cat "${work_dir}/port-forward.log" >&2
  echo "expected HTTP ${expected_status}, got ${status}" >&2
  exit 1
}
if [[ "${status}" == "400" ]]; then
  grep -Fq '"policy_id":"default"' "${work_dir}/response.json" || {
    echo "block response did not identify the route-owned default policy" >&2; exit 1;
  }
else
  grep -Fq 'chatcmpl-kind-mock' "${work_dir}/response.json" || {
    echo "unexpected mock upstream response" >&2; exit 1;
  }
fi
after="$(curl --silent "http://127.0.0.1:${mock_port}/inspect")"
after_sequence="$(jq -r '.sequence' <<<"${after}")"
if [[ "${status}" == "400" ]]; then
  [[ "${before_sequence}" == "${after_sequence}" ]] || {
    echo "blocked request reached the mock upstream" >&2; exit 1;
  }
fi
if [[ "$(basename "${example_dir}")" == "02-request-masking" ]]; then
  [[ "$(jq -r '.masked' <<<"${after}")" == "true" ]] || {
    echo "mock upstream did not observe an EMAIL mask placeholder" >&2; exit 1;
  }
  [[ "$(jq -r '.contains_synthetic_email' <<<"${after}")" == "false" ]] || {
    echo "synthetic PII reached the mock upstream without masking" >&2; exit 1;
  }
fi
if [[ "$(basename "${example_dir}")" == "05-fail-open" ]]; then
  expected_hash="$(sha256_file "${example_dir}/request.json")"
  [[ "$(jq -r '.sha256' <<<"${after}")" == "${expected_hash}" ]] || {
    echo "fail-open changed the upstream request body" >&2; exit 1;
  }
fi
printf 'PASS %s: HTTP %s\n' "$(basename "${example_dir}")" "${status}"
