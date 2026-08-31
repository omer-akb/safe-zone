# BYG performance tests

This directory contains repeatable performance scenarios for the Envoy Gateway
reference deployment. Test output is written to `test-reports/perf/` and is
not committed.

## Regex-only request-path baseline

`extproc-regex-only.js` sends non-streaming, safe OpenAI Chat Completions
requests through Envoy Gateway, `tsz-ext-proc`, and the local mock OpenAI
upstream. The `01-minimal-inspection` policy has no AI semantic validator, so
the scenario exercises the deterministic request path.

Run it from the repository root:

```bash
make perf-extproc-regex-only
```

The runner requires Docker, Kind, `kubectl`, `curl`, and `k6`. It creates or
refreshes the pinned Kind reference environment, activates the minimal policy,
attaches the `EnvoyExtensionPolicy`, verifies one functional request, opens a
temporary Envoy port-forward, and writes the k6 JSON summary.

The default load profile is 25 requests per second for two minutes. Adjust it
without changing the scenario:

```bash
TSZ_PERF_RATE=50 TSZ_PERF_DURATION=5m make perf-extproc-regex-only
```

Additional knobs are `TSZ_PERF_PRE_ALLOCATED_VUS`, `TSZ_PERF_MAX_VUS`,
`TSZ_PERF_LOCAL_PORT`, and `TSZ_PERF_RESULTS_DIR`. Set
`TSZ_BYG_SKIP_BOOTSTRAP=1` only when the same prepared Kind environment is
already running.

## Baseline result

Environment: local Kind reference deployment, Envoy Gateway v1.8.3, local mock
OpenAI upstream, k6 v2.2.0.

| Date | Profile | Load | Requests | HTTP/check failures | End-to-end p95 | Mean |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| 2026-08-28 | regex-only request path | 25 RPS for 2 min | 3,001 | 0 | 12.105 ms | 7.023 ms |

This is an end-to-end baseline, not a measurement of TSZ's added latency. To
validate the issue's regex-only added-latency target, run an equivalent route
without the `EnvoyExtensionPolicy` and compare its p95 with this result.
