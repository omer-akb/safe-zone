# Envoy Gateway Integration

This guide covers Envoy Gateway v1.8.3 with Gateway API v1.5.1. The checked-in
Kind example under `deployments/envoy-gateway/` is the reproducible reference
environment.

## Network isolation

The Kind bootstrap applies the `same-namespace` overlay under
`deployments/envoy-gateway/kustomize/network-policy/`.
It selects `tsz-ext-proc` pods, denies all ingress and egress by default, then
permits only:

- ext_proc gRPC traffic on TCP `9002` from Envoy proxy pods in
  `envoy-gateway-system` bearing `security.thyris.ai/tsz-peer: "true"`;
- DNS to CoreDNS on TCP/UDP `53`;
- PostgreSQL and Redis in the reference namespace on TCP `5432` and `6379`.

The `EnvoyProxy` resource in `echo-demo.yaml` injects the peer label, so the
policy does not rely on Envoy Gateway implementation labels. This pod-label
configuration is verified against Envoy Gateway v1.8.3.

The reference deployment intentionally keeps PostgreSQL and Redis in
`tsz-byg-demo`. Kustomize overlays make the topology explicit:

```bash
# Current Kind topology
kubectl kustomize deployments/envoy-gateway/kustomize/network-policy/overlays/same-namespace

# Production topology: processor in tsz-system; PostgreSQL and Redis in tsz-data
kubectl kustomize deployments/envoy-gateway/kustomize/network-policy/overlays/separate-data-namespace
```

For another namespace layout, change the `namespace` and `tsz-data` values in
the separate-data-namespace overlay together; it retains the dependency
`podSelector` and does not grant namespace-wide egress.
Keep the HTTP health/metrics port (`8080`) out of the ext_proc ingress rule.
Expose it separately and only to an authenticated operational or Prometheus
workload when needed.

## Availability and autoscaling

The reference `tsz-ext-proc.yaml` includes an initial production scaling
profile: two to ten replicas, CPU and memory targets of 60% and 80%, a
five-minute scale-down stabilization window, and a PDB allowing at most one
voluntary pod eviction. CPU utilization is calculated from the container CPU
request, so do not remove that request while the HPA is enabled.

These values are intentionally conservative defaults, not a capacity promise.
Before production rollout, use processor request rate, p95/p99 latency, CPU,
memory, error rate, and concurrent-stream measurements to tune them. The PDB
only protects voluntary disruptions such as node drain and does not protect
against an unexpected node or process failure.

NetworkPolicy is an L3/L4 control and does not provide TLS. Production Envoy
to TSZ traffic must additionally use TLS or mTLS; this reference policy limits
which workloads may initiate the gRPC connection.

## TLS and mTLS for the external processor

The runnable reference is
`examples/bring-your-gateway/19-mtls-and-network-policy`. It uses an isolated,
throwaway ECDSA P-256 CA to verify the TSZ gRPC server and authenticate Envoy
with a client certificate. It is a local demonstration, not a production CA.

Enable TSZ mTLS only by setting all three values below; the process fails at
startup if one is missing, unreadable, or invalid:

```yaml
env:
  - name: TSZ_GRPC_TLS_CERT_FILE
    value: /tls/tls.crt
  - name: TSZ_GRPC_TLS_KEY_FILE
    value: /tls/tls.key
  - name: TSZ_GRPC_TLS_CLIENT_CA_FILE
    value: /tls/client-ca.crt
```

TSZ then requires and verifies a client certificate on the gRPC listener with
TLS 1.2 or newer. Envoy Gateway v1.8.3 is configured through a namespaced
`Backend`: `caCertificateRefs` holds the TSZ server trust anchor,
`clientCertificateRef` holds Envoy's client certificate, and `sni` must match
a DNS SAN on TSZ's server certificate. The Kind bootstrap enables Envoy
Gateway's optional Backend API for this example.

In production, obtain and rotate both leaf certificates through the
organization's PKI (typically cert-manager backed by Vault, cloud private CA,
or another approved issuer). Do not commit private keys, reuse the example CA,
or configure `insecureSkipVerify`.

During `kind-bootstrap.sh verify-replica-lifecycle` and
`verify-controller-reconciliation`, the bootstrap creates two temporary probe
pods. A pod in `envoy-gateway-system` with the TSZ peer label must connect to
`tsz-ext-proc:9002`; an unlabeled pod in `tsz-byg-demo` must be rejected. The
bootstrap fails if either assertion is not true.

## Choosing an installation profile

| | Preview (manual) | Native (managed) |
| --- | --- | --- |
| Policy source | `tsz-policy` CLI plus a hand-applied `EnvoyExtensionPolicy` | `TSZGuardrailPolicy` CRD |
| Route identity | Trusted `X-TSZ-Policy` header set by a gateway `RequestHeaderModifier` / `ClientTrafficPolicy` | Envoy `ext_proc` `xds.route_name` attribute |
| Lifecycle | Imperative operator actions | Declarative reconciliation by `tsz-controller` |
| Status visibility | Policy CLI and processor logs | `kubectl get tszguardrailpolicy` conditions |
| Best fit | Evaluation, quick-start, or one manually managed route | Multi-route, multi-team, and production Kubernetes environments |

Choose one profile for an ext-proc deployment. The resolver is global:

```yaml
env:
  - name: TSZ_POLICY_RESOLUTION_MODE
    value: header # or attribute
```

Do not combine a manual attachment and a managed attachment on the same route.
Do not set `header` for some routes and `attribute` for others in a shared
`tsz-ext-proc` Deployment. Such a configuration can select the wrong identity
source or cause a mandatory policy to be unresolved.
git 
## Response-only local replies

Envoy Gateway can invoke `ext_proc` for a response even when an earlier native
filter, such as JWT authentication or local rate limiting, rejected the request
before TSZ received request headers. Such a callback has no pinned TSZ policy
snapshot. In Envoy Gateway v1.8.3, the standard
`BackendTrafficPolicy.responseOverride.source: Local` response filter runs
after `ext_proc` on the response path, so it cannot provide a trustworthy
marker to TSZ at that point.

TSZ therefore handles every response-only callback through the global
`TSZ_FAIL_MODE`: `closed` is the default and returns TSZ's safe response-stage
`403`; `open` is an explicit availability choice that continues the original
response. Both outcomes emit the bounded
`tsz_extproc_response_without_request_state_total{outcome=...}` metric and a
safe degraded response audit event with reason
`response_without_request_state`. Do not set `open` in production merely to
preserve JWT or rate-limit local replies; evaluate that availability trade-off
explicitly.

Likewise, if a native local reply follows a request callback but its body is
not a supported OpenAI response shape (for example Envoy's local rate-limit
body), the pinned response failure policy applies. Its default closed outcome
is also TSZ's safe response-stage `403`; it is not a promise to preserve the
original `429` body.

## Prometheus metrics

`tsz-ext-proc` exposes Prometheus metrics on its existing HTTP health port at
`GET /metrics`. The deployment manifest names this port `health` (default
`8080`); expose it only to the Prometheus scraper, not to public traffic.

```bash
kubectl -n tsz-byg-demo port-forward service/tsz-ext-proc 8080:8080
curl -s http://127.0.0.1:8080/metrics | grep '^tsz_extproc_'
```

The processor emits transaction, decision, detection-category, processing
duration, bounded failure-reason, timeout, body-size, active-stream, and
stream-halt metrics. `action`, `stage`, `policy`, `type`, `direction`, and
`reason` are the only labels. RID, Envoy request IDs, tenant/user identifiers,
request bodies, raw PII, credentials, and error text are never labels or
metric values.

## OpenTelemetry tracing

Tracing is disabled unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set. The endpoint
uses OTLP/gRPC and TLS by default; set `OTEL_EXPORTER_OTLP_INSECURE=true` only
for a local trusted collector without TLS.

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: otel-collector.observability.svc.cluster.local:4317
  - name: OTEL_EXPORTER_OTLP_INSECURE
    value: "false"
```

TSZ creates `tsz.extproc.request` and `tsz.extproc.response` spans, child
`tsz.guardrail.validator` spans, `tsz.semantic_model` spans for AI validators,
and instrumentation spans for PostgreSQL and Redis operations. A W3C
`traceparent` is continued only when Envoy supplies it through a configured,
trusted ext-proc attribute; TSZ does not trust a client header directly.

Safe span attributes include processing stage, policy ID/version, action,
detection count, degraded state, TSZ RID, and Envoy request ID. Request and
response bodies, raw PII, credentials, validator text, Redis command
arguments, and database query variables are never recorded in spans.

## Preview / portable installation

Use this profile when the gateway integration is managed manually or the
gateway does not have a native TSZ controller adapter.

1. Create, compile, and activate a policy with `tsz-policy`.
2. Deploy `tsz-ext-proc` with `TSZ_POLICY_RESOLUTION_MODE=header` (or omit the
   variable; `header` is the default).
3. Apply
   `deployments/envoy-gateway/tsz-ext-proc-envoy-extension-policy.yaml` after
   changing its `targetRefs` and backend reference for your namespace.
4. Configure a gateway-owned **early** header mutation that overwrites
   `X-TSZ-Policy` with the activated policy name. Do not trust a value sent by
   the downstream client. The `ClientTrafficPolicy` in `echo-demo.yaml` shows
   the Envoy Gateway pattern.
5. Verify the route through Envoy and inspect the processor policy cache.

The manual `EnvoyExtensionPolicy` remains a supported preview example. It is
not removed when installing the native controller; remove it deliberately as
part of migration.

## Native / managed installation

Use this profile when Kubernetes should be the attachment and lifecycle control
plane.

1. Install the generated CRD and RBAC:

   ```bash
   kubectl apply -f config/crd/bases/security.thyris.ai_tszguardrailpolicies.yaml
   kubectl apply -f config/rbac/role.yaml
   ```

2. Deploy `tsz-controller` using
   `deployments/envoy-gateway/controller/controller.yaml`.
3. Deploy `tsz-ext-proc` with
   `TSZ_POLICY_RESOLUTION_MODE=attribute`. The checked-in native manifest is
   `deployments/envoy-gateway/tsz-ext-proc.yaml`.
4. Remove any manual TSZ `EnvoyExtensionPolicy` for routes being migrated.
5. Apply a `TSZGuardrailPolicy`. The ready-to-run Kind example is
   `deployments/envoy-gateway/controller/checkpoint-inline-policy.yaml`.

For an inline policy, the controller creates and activates a deterministic,
controller-owned policy snapshot. For `policySource: PostgresRef`, it verifies
the specified immutable policy version and references it without writing a new
snapshot. In both cases it creates an owned `EnvoyExtensionPolicy` and stores
the trusted native route binding.

Verify reconciliation:

```bash
kubectl -n tsz-byg-demo get tszguardrailpolicy
kubectl -n tsz-byg-demo get tszguardrailpolicy checkpoint-inline -o yaml
kubectl -n tsz-byg-demo get envoyextensionpolicy -o yaml
```

Look for `Accepted`, `ResolvedRefs`, `Programmed`, and `PolicySynced`. A
conflicted attachment is explicitly reported as `Programmed=False` with reason
`Conflicted`; it must not leave a managed `EnvoyExtensionPolicy` behind.

## Migration between profiles

1. Confirm all target routes are covered by native `TSZGuardrailPolicy`
   objects and report `Programmed=True`.
2. Change every replica of `tsz-ext-proc` to
   `TSZ_POLICY_RESOLUTION_MODE=attribute`, then roll it out.
3. Remove the manual TSZ `EnvoyExtensionPolicy` and the old trusted-header
   identity mutation for the migrated routes.
4. Verify traffic and the CRD conditions before considering the migration
   complete.

To move back to preview, reverse the order: restore the manual attachment and
trusted header identity first, roll ext-proc to `header`, and only then delete
the managed CRD.
