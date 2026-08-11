package extproc

import (
	"errors"
	"strings"
	"testing"
)

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
		{name: "streaming", ctype: "application/json", body: `{"stream":true,"messages":[]}`, want: ErrUnsupportedChatRequest, kind: ChatRequestUnsupportedRequest},
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
