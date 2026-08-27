#!/usr/bin/env bash
set -euo pipefail

example_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${example_dir}/../../.." && pwd)"
namespace="tsz-byg-demo"
kubeconfig="${TSZ_BYG_KUBECONFIG:-${TMPDIR:-/tmp}/tsz-byg-tools/tsz-byg.kubeconfig}"

"${example_dir}/generate-certs.sh"
certs="${example_dir}/certs"
TSZ_BYG_KUBECONFIG="$kubeconfig" "${repo_root}/deployments/envoy-gateway/kind-bootstrap.sh" up
TSZ_BYG_KUBECONFIG="$kubeconfig" "${repo_root}/deployments/envoy-gateway/kind-bootstrap.sh" verify-replica-lifecycle
export KUBECONFIG="$kubeconfig"

kubectl -n "$namespace" create secret generic tsz-ext-proc-server-tls \
  --from-file=tls.crt="$certs/server.crt" --from-file=tls.key="$certs/server.key" \
  --from-file=client-ca.crt="$certs/ca.crt" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$namespace" create secret tls envoy-ext-proc-client-tls \
  --cert="$certs/client.crt" --key="$certs/client.key" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$namespace" create configmap tsz-ext-proc-server-ca \
  --from-file=ca.crt="$certs/ca.crt" --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "$namespace" patch deployment tsz-ext-proc --type=strategic -p '
{"spec":{"template":{"spec":{"containers":[{"name":"tsz-ext-proc","env":[
{"name":"TSZ_GRPC_TLS_CERT_FILE","value":"/tls/tls.crt"},
{"name":"TSZ_GRPC_TLS_KEY_FILE","value":"/tls/tls.key"},
{"name":"TSZ_GRPC_TLS_CLIENT_CA_FILE","value":"/tls/client-ca.crt"}],
"volumeMounts":[{"name":"tsz-ext-proc-server-tls","mountPath":"/tls","readOnly":true}]}],
"volumes":[{"name":"tsz-ext-proc-server-tls","secret":{"secretName":"tsz-ext-proc-server-tls"}}]}}}}'
kubectl -n "$namespace" rollout status deployment/tsz-ext-proc --timeout=180s

kubectl -n "$namespace" delete envoyextensionpolicy tsz-request-guardrail --ignore-not-found
kubectl apply -f "${example_dir}/resources.yaml"
kubectl -n "$namespace" wait --for=condition=Accepted backend/tsz-ext-proc-mtls --timeout=90s
kubectl -n "$namespace" wait --for=condition=Accepted envoyextensionpolicy/tsz-request-guardrail-mtls --timeout=90s
printf 'mTLS resources are installed. Run the normal BYG request check after the Envoy policy reports Accepted.\n'
