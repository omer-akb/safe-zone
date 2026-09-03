package extproc

import (
	"errors"
	"strings"
	"testing"
)

func TestParseResponsesRequestExtractsSupportedUserText(t *testing.T) {
	body := []byte(`{
  "model": "gpt-test",
  "input": [
    {"role":"system","content":"not part of this capability"},
    {"type":"message","role":"user","content":"plain user text"},
    {"type":"message","role":"user","content":[
      {"type":"input_text","text":"structured user text"},
      {"type":"input_image","image_url":"https://example.test/image.png"}
    ]},
    {"type":"message","role":"assistant","content":[{"type":"output_text","text":"previous assistant text"}]},
    {"type":"function_call_output","call_id":"call_1","output":"tool output"}
  ],
  "unknown": {"keep": true}
}`)
	request, err := ParseResponsesRequest("application/json; charset=utf-8", body)
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	if len(request.UserContents) != 2 {
		t.Fatalf("user contents = %+v, want two entries", request.UserContents)
	}
	if got := request.UserContents[0]; got.ID != 0 || got.JSONPath != ".input[1].content" || got.Content != "plain user text" {
		t.Fatalf("first user content = %+v", got)
	}
	if got := request.UserContents[1]; got.ID != 1 || got.JSONPath != ".input[2].content[0].text" || got.Content != "structured user text" {
		t.Fatalf("second user content = %+v", got)
	}
}

func TestParseResponsesRequestAcceptsStringInput(t *testing.T) {
	request, err := ParseResponsesRequest("application/json", []byte(`{"model":"gpt-test","input":"hello"}`))
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	if len(request.UserContents) != 1 || request.UserContents[0].JSONPath != ".input" || request.UserContents[0].Content != "hello" {
		t.Fatalf("user contents = %+v", request.UserContents)
	}
}

func TestResponsesRequestMutatePreservesUnknownFieldsAndFormatting(t *testing.T) {
	body := []byte(`{ "unknown" : [ 1, 2 ], "input" : [ { "role" : "user", "content" : "replace one", "extra" : true }, { "role" : "user", "content" : [ { "type" : "input_text", "text" : "replace two" } ] } ] }`)
	request, err := ParseResponsesRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	mutated, err := request.Mutate([]ResponsesContentMutation{{ID: 0, Content: "[MASKED]"}, {ID: 1, Content: "safe\ntext"}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"replace one"`, `"[MASKED]"`, 1)
	want = strings.Replace(want, `"replace two"`, `"safe\ntext"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated request changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
	unchanged, err := request.Mutate(nil)
	if err != nil || string(unchanged) != string(body) {
		t.Fatalf("no-op mutation = %q, error = %v", unchanged, err)
	}
}

func TestParseResponsesRequestReturnsTypedErrors(t *testing.T) {
	tests := []struct {
		name, contentType, body, path string
		want                          error
		kind                          ResponsesErrorKind
	}{
		{name: "content type", contentType: "text/plain", body: `{}`, want: ErrUnsupportedResponsesType, kind: ResponsesUnsupportedType},
		{name: "empty", contentType: "application/json", body: " ", want: ErrEmptyResponsesBody, kind: ResponsesEmptyBody},
		{name: "invalid JSON", contentType: "application/json", body: `{"input":`, want: ErrInvalidResponsesJSON, kind: ResponsesInvalidJSON},
		{name: "ambiguous payload without input", contentType: "application/json", body: `{"model":"gpt-test"}`, path: ".input", want: ErrUnsupportedResponsesPayload, kind: ResponsesUnsupportedPayload},
		{name: "object input", contentType: "application/json", body: `{"input":{}}`, path: ".input", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "missing user content", contentType: "application/json", body: `{"input":[{"role":"user"}]}`, path: ".input[0].content", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "invalid input text", contentType: "application/json", body: `{"input":[{"role":"user","content":[{"type":"input_text","text":null}]}]}`, path: ".input[0].content[0].text", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseResponsesRequest(test.contentType, []byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.want)
			}
			var typed *ResponsesError
			if !errors.As(err, &typed) || typed.Kind != test.kind || typed.Path != test.path {
				t.Fatalf("typed error = %+v, want kind=%q path=%q", typed, test.kind, test.path)
			}
		})
	}
}

func TestParseResponsesRequestAllowsConversationContinuationWithoutNewInput(t *testing.T) {
	request, err := ParseResponsesRequest("application/json", []byte(`{"model":"gpt-test","previous_response_id":"resp_1"}`))
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	if len(request.UserContents) != 0 {
		t.Fatalf("user contents = %+v, want none", request.UserContents)
	}
}

func TestParseResponsesResponseExtractsAssistantOutputText(t *testing.T) {
	body := []byte(`{"id":"resp_1","object":"response","output":[{"type":"reasoning","summary":[]},{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"first","annotations":[]},{"type":"refusal","refusal":"cannot comply"}]},{"id":"call_1","type":"function_call","arguments":"{}"},{"id":"msg_2","type":"message","role":"assistant","content":[{"type":"output_text","text":"second","annotations":[]}]}],"output_text":"firstsecond","usage":{"total_tokens":5}}`)
	response, err := ParseResponsesResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseResponsesResponse() error = %v", err)
	}
	if len(response.AssistantContents) != 2 {
		t.Fatalf("assistant contents = %+v, want two entries", response.AssistantContents)
	}
	if got := response.AssistantContents[0]; got.JSONPath != ".output[1].content[0].text" || got.Content != "first" {
		t.Fatalf("first assistant content = %+v", got)
	}
	if got := response.AssistantContents[1]; got.JSONPath != ".output[3].content[0].text" || got.Content != "second" {
		t.Fatalf("second assistant content = %+v", got)
	}
}

func TestResponsesResponseMutateUpdatesNestedAndConvenienceOutputText(t *testing.T) {
	body := []byte(`{ "output_text" : "secret safe", "object" : "response", "unknown" : true, "output" : [ { "type" : "message", "role" : "assistant", "content" : [ { "type" : "output_text", "text" : "secret " }, { "type" : "output_text", "text" : "safe" } ] } ] }`)
	response, err := ParseResponsesResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseResponsesResponse() error = %v", err)
	}
	mutated, err := response.Mutate([]ResponsesContentMutation{{ID: 0, Content: "[MASKED] "}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"secret safe"`, `"[MASKED] safe"`, 1)
	want = strings.Replace(want, `"secret "`, `"[MASKED] "`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated response changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
}

func TestResponsesResponseUsesConvenienceOutputTextAsSafeFallback(t *testing.T) {
	response, err := ParseResponsesResponse("application/json", []byte(`{"object":"response","output":[],"output_text":"visible text"}`))
	if err != nil {
		t.Fatalf("ParseResponsesResponse() error = %v", err)
	}
	if len(response.AssistantContents) != 1 || response.AssistantContents[0].JSONPath != ".output_text" {
		t.Fatalf("assistant contents = %+v", response.AssistantContents)
	}
	mutated, err := response.Mutate([]ResponsesContentMutation{{ID: 0, Content: "[MASKED]"}})
	if err != nil || string(mutated) != `{"object":"response","output":[],"output_text":"[MASKED]"}` {
		t.Fatalf("fallback mutation = %s, error = %v", mutated, err)
	}
}

func TestResponsesMutationRejectsUnknownAndDuplicateTargets(t *testing.T) {
	request, err := ParseResponsesRequest("application/json", []byte(`{"input":"one"}`))
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	for _, mutations := range [][]ResponsesContentMutation{
		{{ID: 4, Content: "unknown"}},
		{{ID: 0, Content: "first"}, {ID: 0, Content: "second"}},
	} {
		_, err := request.Mutate(mutations)
		if !errors.Is(err, ErrInvalidResponsesMutation) {
			t.Fatalf("Mutate(%+v) error = %v, want ErrInvalidResponsesMutation", mutations, err)
		}
	}
}

func TestParseResponsesResponseReturnsTypedErrors(t *testing.T) {
	tests := []struct {
		name, body, path string
		want             error
		kind             ResponsesErrorKind
	}{
		{name: "wrong object", body: `{"object":"chat.completion","output":[]}`, path: ".object", want: ErrUnsupportedResponsesPayload, kind: ResponsesUnsupportedPayload},
		{name: "missing output", body: `{"object":"response"}`, path: ".output", want: ErrUnsupportedResponsesPayload, kind: ResponsesUnsupportedPayload},
		{name: "invalid message content", body: `{"object":"response","output":[{"type":"message","role":"assistant","content":null}]}`, path: ".output[0].content", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "invalid output text", body: `{"object":"response","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":null}]}]}`, path: ".output[0].content[0].text", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseResponsesResponse("application/json", []byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.want)
			}
			var typed *ResponsesError
			if !errors.As(err, &typed) || typed.Kind != test.kind || typed.Path != test.path {
				t.Fatalf("typed error = %+v, want kind=%q path=%q", typed, test.kind, test.path)
			}
		})
	}
}
