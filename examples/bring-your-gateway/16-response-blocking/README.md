# Response blocking

The local mock returns a synthetic secret in `choices[].message.content`. TSZ detects it and Envoy returns the safe TSZ HTTP 403 response instead of the upstream response. This is buffered, non-streaming enforcement; it does not cover SSE responses.

Prerequisites: Docker, Kind, kubectl, Helm, curl and jq. The shared bootstrap pins Envoy Gateway v1.8.3.

Run and clean up:

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/16-response-blocking
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/16-response-blocking
```

A safe request is `request.json`; the mock response contains a synthetic generic API key only as a fixture. The policy explicitly compiles the seeded `GENERIC_API_KEY` pattern, so the expected result is HTTP 403, the `TSZ_RESPONSE_GUARDRAIL_BLOCKED` error code, and no raw credential in the client response. The smoke test verifies that the mock received the request before TSZ blocked the response. Inspect Envoy Gateway and `tsz-ext-proc` logs or `io.thyris.tsz` metadata when troubleshooting; rerun bootstrap if pods are not ready.
