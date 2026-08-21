package extproc

import (
	"encoding/json"
	"errors"
	"strings"
)

var (
	// ErrInvalidSSEEvent is returned when a complete SSE data event does not
	// contain valid JSON (excluding OpenAI's [DONE] sentinel).
	ErrInvalidSSEEvent = errors.New("invalid OpenAI SSE event")
	// ErrIncompleteSSEEvent is returned when Envoy signals end-of-stream while
	// an SSE line or event is still incomplete.
	ErrIncompleteSSEEvent = errors.New("incomplete OpenAI SSE event")
)

// SSEParseError identifies a malformed SSE event without retaining its data.
// Event payloads may contain model output, so they must never be included in
// an error message or logged by callers.
type SSEParseError struct {
	Err error
}

func (e *SSEParseError) Error() string { return e.Err.Error() }
func (e *SSEParseError) Unwrap() error { return e.Err }

// OpenAISSEEvent is one complete OpenAI-compatible SSE event. DeltaContents
// contains the supported choices[].delta.content values in choice order. The
// parser deliberately does not apply guardrails, mutate bytes, or decide how
// an invalid event affects the stream.
type OpenAISSEEvent struct {
	Done          bool
	DeltaContents []string
}

// OpenAISSEParser incrementally parses an SSE response delivered in arbitrary
// ext_proc response-body chunks. It is owned by one response stream and is not
// safe for concurrent use.
type OpenAISSEParser struct {
	pendingLine string
	dataLines   []string
	eventOpen   bool
}

// Feed consumes a response-body chunk and returns each complete SSE event it
// contains. Chunks may split anywhere, including inside an SSE line or JSON
// string. A parse error is safe to surface because it contains no event data.
func (p *OpenAISSEParser) Feed(chunk []byte) ([]OpenAISSEEvent, error) {
	if p == nil {
		return nil, &SSEParseError{Err: ErrIncompleteSSEEvent}
	}
	p.pendingLine += string(chunk)
	var events []OpenAISSEEvent
	for {
		newline := strings.IndexByte(p.pendingLine, '\n')
		if newline < 0 {
			return events, nil
		}
		line := strings.TrimSuffix(p.pendingLine[:newline], "\r")
		p.pendingLine = p.pendingLine[newline+1:]
		event, complete, err := p.consumeLine(line)
		if err != nil {
			return events, err
		}
		if complete {
			events = append(events, event)
		}
	}
}

// Finish must be called once Envoy marks the response body end-of-stream. A
// final empty line is required to terminate an SSE event; accepting a partial
// event here would hide truncated model output from the next streaming phase.
func (p *OpenAISSEParser) Finish() error {
	if p == nil || p.pendingLine != "" || p.eventOpen {
		return &SSEParseError{Err: ErrIncompleteSSEEvent}
	}
	return nil
}

func (p *OpenAISSEParser) consumeLine(line string) (OpenAISSEEvent, bool, error) {
	if line == "" {
		if !p.eventOpen {
			return OpenAISSEEvent{}, false, nil
		}
		if len(p.dataLines) == 0 {
			p.eventOpen = false
			return OpenAISSEEvent{}, false, nil
		}
		event, err := openAISSEEvent(p.dataLines)
		p.dataLines = nil
		p.eventOpen = false
		return event, true, err
	}

	p.eventOpen = true
	if strings.HasPrefix(line, ":") { // SSE comment / keepalive
		return OpenAISSEEvent{}, false, nil
	}
	field, value, found := strings.Cut(line, ":")
	if !found {
		return OpenAISSEEvent{}, false, nil
	}
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}
	if field == "data" {
		p.dataLines = append(p.dataLines, value)
	}
	return OpenAISSEEvent{}, false, nil
}

func openAISSEEvent(dataLines []string) (OpenAISSEEvent, error) {
	payload := strings.Join(dataLines, "\n")
	if payload == "[DONE]" {
		return OpenAISSEEvent{Done: true}, nil
	}
	var response struct {
		Choices []struct {
			Delta struct {
				Content *string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		return OpenAISSEEvent{}, &SSEParseError{Err: ErrInvalidSSEEvent}
	}
	event := OpenAISSEEvent{}
	for _, choice := range response.Choices {
		if choice.Delta.Content != nil {
			event.DeltaContents = append(event.DeltaContents, *choice.Delta.Content)
		}
	}
	return event, nil
}
