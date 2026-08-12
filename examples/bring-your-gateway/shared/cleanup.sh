#!/usr/bin/env bash
set -euo pipefail
example_dir="${1:-}"
if [[ -n "${example_dir}" ]]; then
  name="tsz-example-$(basename "${example_dir}")"
  name="${name//[^a-z0-9-]/-}"
  kubectl -n tsz-byg-demo delete job,configmap "${name}" --ignore-not-found
fi
kubectl -n tsz-byg-demo delete -f deployments/envoy-gateway/tsz-ext-proc-envoy-extension-policy.yaml --ignore-not-found
# Failure examples enable a local-only fault on the Deployment. Always remove
# it during cleanup so the next example and the bootstrap health check start
# from a healthy ext-proc process.
if kubectl -n tsz-byg-demo get deployment tsz-ext-proc >/dev/null 2>&1; then
  kubectl -n tsz-byg-demo set env deployment/tsz-ext-proc TSZ_EXAMPLE_AUDIT_FAILURE-
  kubectl -n tsz-byg-demo rollout status deployment/tsz-ext-proc --timeout=90s
fi
