# Observability and SIEM

This runnable Kind example sends a masked synthetic PII request through Envoy,
then verifies the PII-safe Prometheus metric family and a local SIEM webhook
event. The event contains TSZ RID, Envoy request ID, policy/version, action and
category; it never contains the request body or matched value.

Run `examples/bring-your-gateway/20-observability/run.sh` and clean up with
`examples/bring-your-gateway/20-observability/cleanup.sh`. It needs Docker,
Kind, kubectl, curl and jq. The local mock provider confirms masking before
upstream delivery. The mock sink is test-only; use an approved TLS-protected
collector in production. Trace exporter configuration and trusted traceparent
propagation are documented in `docs/operations/BYG_OBSERVABILITY.md`.
