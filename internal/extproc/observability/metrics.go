// Package observability provides PII-safe Prometheus metrics for the BYG data
// plane. Metric labels are deliberately limited to bounded policy outcomes.
package observability

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"thyris-sz/internal/extproc"
)

// ExtProcMetrics records operational data for Envoy external processing. It
// intentionally accepts no request identifiers, route names, tenants, bodies,
// or error messages, since those values are either sensitive or unbounded.
type ExtProcMetrics struct {
	requests             prometheus.Counter
	responses            prometheus.Counter
	actions              *prometheus.CounterVec
	detections           *prometheus.CounterVec
	processingDuration   *prometheus.HistogramVec
	failures             *prometheus.CounterVec
	timeouts             prometheus.Counter
	bodyBytes            *prometheus.HistogramVec
	activeStreams        prometheus.Gauge
	streamHalts          prometheus.Counter
	responseWithoutState *prometheus.CounterVec
}

func NewExtProcMetrics(registerer prometheus.Registerer) (*ExtProcMetrics, error) {
	metrics := &ExtProcMetrics{
		requests:             prometheus.NewCounter(prometheus.CounterOpts{Namespace: "tsz", Subsystem: "extproc", Name: "requests_total", Help: "External-processing request transactions received."}),
		responses:            prometheus.NewCounter(prometheus.CounterOpts{Namespace: "tsz", Subsystem: "extproc", Name: "responses_total", Help: "External-processing response transactions received."}),
		actions:              prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "tsz", Subsystem: "extproc", Name: "actions_total", Help: "Guardrail decisions by action, stage, and policy."}, []string{"action", "stage", "policy"}),
		detections:           prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "tsz", Subsystem: "extproc", Name: "detections_total", Help: "Detection-category occurrences by processing stage."}, []string{"type", "stage"}),
		processingDuration:   prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "tsz", Subsystem: "extproc", Name: "processing_duration_seconds", Help: "Guardrail processing duration by stage.", Buckets: prometheus.DefBuckets}, []string{"stage"}),
		failures:             prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "tsz", Subsystem: "extproc", Name: "failures_total", Help: "Bounded ext-proc failures by reason."}, []string{"reason"}),
		timeouts:             prometheus.NewCounter(prometheus.CounterOpts{Namespace: "tsz", Subsystem: "extproc", Name: "timeouts_total", Help: "Processor operations that exceeded the configured timeout."}),
		bodyBytes:            prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "tsz", Subsystem: "extproc", Name: "body_bytes", Help: "Buffered body bytes by direction.", Buckets: prometheus.ExponentialBuckets(256, 4, 8)}, []string{"direction"}),
		activeStreams:        prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "tsz", Subsystem: "extproc", Name: "active_streams", Help: "Currently active external-processing gRPC streams."}),
		streamHalts:          prometheus.NewCounter(prometheus.CounterOpts{Namespace: "tsz", Subsystem: "extproc", Name: "stream_halts_total", Help: "Streaming responses halted by a guardrail decision."}),
		responseWithoutState: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "tsz", Subsystem: "extproc", Name: "response_without_request_state_total", Help: "Response callbacks received without pinned request stream state."}, []string{"outcome"}),
	}
	collectors := []prometheus.Collector{metrics.requests, metrics.responses, metrics.actions, metrics.detections, metrics.processingDuration, metrics.failures, metrics.timeouts, metrics.bodyBytes, metrics.activeStreams, metrics.streamHalts, metrics.responseWithoutState}
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			return nil, err
		}
	}
	return metrics, nil
}

func (m *ExtProcMetrics) IncRequest()       { m.requests.Inc() }
func (m *ExtProcMetrics) IncResponse()      { m.responses.Inc() }
func (m *ExtProcMetrics) IncActiveStreams() { m.activeStreams.Inc() }
func (m *ExtProcMetrics) DecActiveStreams() { m.activeStreams.Dec() }
func (m *ExtProcMetrics) IncTimeout()       { m.timeouts.Inc() }
func (m *ExtProcMetrics) IncStreamHalt()    { m.streamHalts.Inc() }
func (m *ExtProcMetrics) IncFailure(reason string) {
	m.failures.WithLabelValues(boundedReason(reason)).Inc()
}
func (m *ExtProcMetrics) ObserveBodyBytes(direction string, bytes int) {
	if bytes > 0 {
		m.bodyBytes.WithLabelValues(boundedDirection(direction)).Observe(float64(bytes))
	}
}
func (m *ExtProcMetrics) ObserveDuration(stage extproc.ProcessingStage, seconds float64) {
	m.processingDuration.WithLabelValues(boundedStage(stage)).Observe(seconds)
}
func (m *ExtProcMetrics) ObserveAction(action extproc.Action, stage extproc.ProcessingStage, policyID string) {
	m.actions.WithLabelValues(boundedAction(action), boundedStage(stage), boundedPolicy(policyID)).Inc()
}
func (m *ExtProcMetrics) ObserveDetections(categories []string, stage extproc.ProcessingStage) {
	for _, category := range categories {
		if category = strings.TrimSpace(category); category != "" {
			m.detections.WithLabelValues(category, boundedStage(stage)).Inc()
		}
	}
}

// ObserveResponseWithoutRequestState preserves the previously exported
// degraded-response signal while sharing the main ext-proc registry.
func (m *ExtProcMetrics) ObserveResponseWithoutRequestState(outcome string) {
	m.responseWithoutState.WithLabelValues(boundedReason(outcome)).Inc()
}

func boundedStage(stage extproc.ProcessingStage) string {
	if stage == extproc.StageResponse {
		return "response"
	}
	return "request"
}
func boundedAction(action extproc.Action) string {
	switch action {
	case extproc.ActionAllow, extproc.ActionMask, extproc.ActionBlock, extproc.ActionAuditOnly:
		return string(action)
	default:
		return "UNKNOWN"
	}
}
func boundedPolicy(policyID string) string {
	policyID = strings.TrimSpace(policyID)
	if policyID == "" {
		return "unresolved"
	}
	return policyID
}
func boundedDirection(direction string) string {
	if direction == "response" {
		return "response"
	}
	return "request"
}
func boundedReason(reason string) string {
	switch reason {
	case "timeout", "processor", "policy_resolution", "body_limit", "stream_buffer", "audit", "protocol", "response_without_request_state", "fail_open", "fail_closed":
		return reason
	default:
		return "other"
	}
}
