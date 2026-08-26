package extproc

import (
	"errors"
	"strings"
	"testing"
)

func TestParseChatResponseAcceptsChatCompletionsShape(t *testing.T) {
	body := []byte(`{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"},{"index":1,"message":{"role":"assistant","content":"second answer"},"finish_reason":"length"}],"usage":{"total_tokens":7}}`)
	response, err := ParseChatResponse("application/json; charset=utf-8", body)
	if err != nil {
		t.Fatalf("ParseChatResponse() error = %v", err)
	}
	if response.root == nil || response.root.kind != jsonObject {
		t.Fatalf("response root = %+v, want JSON object", response.root)
	}
	if len(response.AssistantContents) != 2 {
		t.Fatalf("assistant contents = %+v, want two entries", response.AssistantContents)
	}
	first, second := response.AssistantContents[0], response.AssistantContents[1]
	if first.ChoiceIndex != 0 || first.JSONPath != ".choices[0].message.content" || first.Content != "hello" {
		t.Fatalf("first assistant content = %+v", first)
	}
	if second.ChoiceIndex != 1 || second.JSONPath != ".choices[1].message.content" || second.Content != "second answer" {
		t.Fatalf("second assistant content = %+v", second)
	}
	if string(response.body) != string(body) {
		t.Fatalf("response body = %q, want %q", response.body, body)
	}
	body[0] = '['
	if response.body[0] != '{' {
		t.Fatal("response must retain an independent copy of the original body")
	}
}

func TestParseChatResponseReturnsTypedErrors(t *testing.T) {
	tests := []struct {
		name  string
		ctype string
		body  string
		want  error
		kind  ChatResponseErrorKind
		path  string
	}{
		{name: "unsupported content type", ctype: "text/plain", body: `{}`, want: ErrUnsupportedChatResponseType, kind: ChatResponseUnsupportedType},
		{name: "empty body", ctype: "application/json", body: " \n\t", want: ErrEmptyChatResponseBody, kind: ChatResponseEmptyBody},
		{name: "invalid JSON", ctype: "application/json", body: `{"choices":[`, want: ErrInvalidChatResponseJSON, kind: ChatResponseInvalidJSON},
		{name: "array root", ctype: "application/json", body: `[]`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse},
		{name: "missing choices", ctype: "application/json", body: `{"object":"chat.completion"}`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse, path: ".choices"},
		{name: "non-array choices", ctype: "application/json", body: `{"choices":{}}`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse, path: ".choices"},
		{name: "non-object choice", ctype: "application/json", body: `{"choices":[null]}`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse, path: ".choices[0]"},
		{name: "missing message", ctype: "application/json", body: `{"choices":[{}]}`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse, path: ".choices[0].message"},
		{name: "non-assistant role", ctype: "application/json", body: `{"choices":[{"message":{"role":"tool","content":"no"}}]}`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse, path: ".choices[0].message.role"},
		{name: "null content", ctype: "application/json", body: `{"choices":[{"message":{"role":"assistant","content":null}}]}`, want: ErrUnsupportedChatResponseContent, kind: ChatResponseUnsupportedContent, path: ".choices[0].message.content"},
		{name: "array content", ctype: "application/json", body: `{"choices":[{"message":{"role":"assistant","content":[]}}]}`, want: ErrUnsupportedChatResponseContent, kind: ChatResponseUnsupportedContent, path: ".choices[0].message.content"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseChatResponse(test.ctype, []byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("ParseChatResponse() error = %v, want errors.Is(_, %v)", err, test.want)
			}
			var typed *ChatResponseError
			if !errors.As(err, &typed) || typed.Kind != test.kind || typed.Path != test.path {
				t.Fatalf("ParseChatResponse() typed error = %+v, want kind %q and path %q", typed, test.kind, test.path)
			}
		})
	}
}

func TestChatResponseMutatePreservesUnknownFieldsFormattingAndChoiceOrder(t *testing.T) {
	body := []byte(`{ "id" : "chatcmpl-test", "unknown" : { "array" : [ 1, 2 ] }, "choices" : [ { "message" : { "role" : "assistant", "content" : "replace first" }, "finish_reason" : "stop" }, { "message" : { "role" : "assistant", "content" : "replace second" }, "extra" : true } ], "usage" : { "total_tokens" : 5 } }`)
	response, err := ParseChatResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseChatResponse() error = %v", err)
	}
	mutated, err := response.Mutate([]ChatResponseContentMutation{
		{ChoiceIndex: 0, Content: "[MASKED]"},
		{ChoiceIndex: 1, Content: ""},
	})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"replace first"`, `"[MASKED]"`, 1)
	want = strings.Replace(want, `"replace second"`, `""`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated body changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
	unchanged, err := response.Mutate(nil)
	if err != nil {
		t.Fatalf("Mutate(nil) error = %v", err)
	}
	if string(unchanged) != string(body) {
		t.Fatalf("no-op mutation reformatted body\ngot:  %s\nwant: %s", unchanged, body)
	}
}

func TestChatResponseMutateRejectsUnknownOrDuplicateTargets(t *testing.T) {
	response, err := ParseChatResponse("application/json", []byte(`{"choices":[{"message":{"role":"assistant","content":"one"}}]}`))
	if err != nil {
		t.Fatalf("ParseChatResponse() error = %v", err)
	}
	for _, mutations := range [][]ChatResponseContentMutation{
		{{ChoiceIndex: 2, Content: "unknown"}},
		{{ChoiceIndex: 0, Content: "first"}, {ChoiceIndex: 0, Content: "second"}},
	} {
		_, err := response.Mutate(mutations)
		if !errors.Is(err, ErrInvalidChatResponseMutation) {
			t.Fatalf("Mutate(%+v) error = %v, want ErrInvalidChatResponseMutation", mutations, err)
		}
	}
}

func TestParseChatRequestExtractsOnlyUserStringContent(t *testing.T) {
	body := []byte(`{
  "model": "gpt-test",
  "messages": [
    {"role":"system","content":"system instructions"},
    {"role":"developer","content":"developer instructions"},
    {"role":"assistant","content":"assistant reply"},
    {"role":"tool","content":"tool output"},
    {"role":"user","content":"first user message"},
    {"role":"user","content":"second user message"}
  ],
  "unknown_top_level": {"keep": true}
}`)
	request, err := ParseChatRequest("application/json; charset=utf-8", body)
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	if len(request.UserContents) != 2 {
		t.Fatalf("user contents = %+v, want two entries", request.UserContents)
	}
	first, second := request.UserContents[0], request.UserContents[1]
	if first.MessageIndex != 4 || first.JSONPath != ".messages[4].content" || first.Content != "first user message" {
		t.Fatalf("first user content = %+v", first)
	}
	if second.MessageIndex != 5 || second.JSONPath != ".messages[5].content" || second.Content != "second user message" {
		t.Fatalf("second user content = %+v", second)
	}
}

func TestChatRequestMutatePreservesUnknownFieldsFormattingAndMessageOrder(t *testing.T) {
	body := []byte(`{ "model" : "gpt-test", "unknown" : { "array" : [ 1, 2 ] }, "messages" : [ { "role" : "system", "content" : [ { "type" : "text", "text" : "leave unchanged" } ] }, { "role" : "user", "content" : "replace me", "extra" : { "x" : true } }, { "role" : "assistant", "content" : "leave assistant" } ], "stream" : false }`)
	request, err := ParseChatRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	if len(request.UserContents) != 1 || request.UserContents[0].MessageIndex != 1 {
		t.Fatalf("user contents = %+v", request.UserContents)
	}
	mutated, err := request.Mutate([]ChatContentMutation{{MessageIndex: 1, Content: "masked\ncontent"}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"replace me"`, `"masked\ncontent"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated body changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
	unchanged, err := request.Mutate(nil)
	if err != nil {
		t.Fatalf("Mutate(nil) error = %v", err)
	}
	if string(unchanged) != string(body) {
		t.Fatalf("no-op mutation reformatted body\ngot:  %s\nwant: %s", unchanged, body)
	}
}

func TestParseChatRequestReturnsTypedErrors(t *testing.T) {
	tests := []struct {
		name  string
		ctype string
		body  string
		want  error
		kind  ChatRequestErrorKind
	}{
		{name: "unsupported content type", ctype: "text/plain", body: `{}`, want: ErrUnsupportedChatContentType, kind: ChatRequestUnsupportedType},
		{name: "empty body", ctype: "application/json", body: " \n\t", want: ErrEmptyChatRequestBody, kind: ChatRequestEmptyBody},
		{name: "invalid JSON", ctype: "application/json", body: `{"messages":[`, want: ErrInvalidChatRequestJSON, kind: ChatRequestInvalidJSON},
		{name: "Responses API shape", ctype: "application/json", body: `{"input":"not chat completions"}`, want: ErrUnsupportedChatRequest, kind: ChatRequestUnsupportedRequest},
		{name: "user multimodal array", ctype: "application/json", body: `{"messages":[{"role":"user","content":[{"type":"text","text":"no"}]}]}`, want: ErrUnsupportedChatContent, kind: ChatRequestUnsupportedContent},
		{name: "user object content", ctype: "application/json", body: `{"messages":[{"role":"user","content":{"text":"no"}}]}`, want: ErrUnsupportedChatContent, kind: ChatRequestUnsupportedContent},
		{name: "user missing content", ctype: "application/json", body: `{"messages":[{"role":"user"}]}`, want: ErrUnsupportedChatContent, kind: ChatRequestUnsupportedContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseChatRequest(test.ctype, []byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("ParseChatRequest() error = %v, want errors.Is(_, %v)", err, test.want)
			}
			var typed *ChatRequestError
			if !errors.As(err, &typed) || typed.Kind != test.kind {
				t.Fatalf("ParseChatRequest() typed error = %+v, want kind %q", typed, test.kind)
			}
		})
	}
}

func TestParseChatRequestAcceptsStreamingRequests(t *testing.T) {
	request, err := ParseChatRequest("application/json", []byte(`{"stream":true,"messages":[{"role":"user","content":"safe"}]}`))
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	if len(request.UserContents) != 1 || request.UserContents[0].Content != "safe" {
		t.Fatalf("user contents = %+v", request.UserContents)
	}
}

func TestChatRequestMutateRejectsUnknownOrDuplicateTargets(t *testing.T) {
	request, err := ParseChatRequest("application/json", []byte(`{"messages":[{"role":"user","content":"one"}]}`))
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	for _, mutations := range [][]ChatContentMutation{
		{{MessageIndex: 2, Content: "unknown"}},
		{{MessageIndex: 0, Content: "first"}, {MessageIndex: 0, Content: "second"}},
	} {
		_, err := request.Mutate(mutations)
		if !errors.Is(err, ErrInvalidChatMutation) {
			t.Fatalf("Mutate(%+v) error = %v, want ErrInvalidChatMutation", mutations, err)
		}
	}
}
