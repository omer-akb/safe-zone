package extproc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"strings"
)

// ChatRequestErrorKind classifies input errors without exposing request body
// content to callers or logs.
type ChatRequestErrorKind string

const (
	ChatRequestEmptyBody          ChatRequestErrorKind = "empty_body"
	ChatRequestUnsupportedType    ChatRequestErrorKind = "unsupported_content_type"
	ChatRequestInvalidJSON        ChatRequestErrorKind = "invalid_json"
	ChatRequestUnsupportedRequest ChatRequestErrorKind = "unsupported_request"
	ChatRequestUnsupportedContent ChatRequestErrorKind = "unsupported_content"
	ChatRequestInvalidMutation    ChatRequestErrorKind = "invalid_mutation"
)

var (
	ErrEmptyChatRequestBody       = errors.New("empty OpenAI chat request body")
	ErrUnsupportedChatContentType = errors.New("unsupported OpenAI chat request content type")
	ErrInvalidChatRequestJSON     = errors.New("invalid OpenAI chat request JSON")
	ErrUnsupportedChatRequest     = errors.New("unsupported OpenAI chat request")
	ErrUnsupportedChatContent     = errors.New("unsupported OpenAI chat message content")
	ErrInvalidChatMutation        = errors.New("invalid OpenAI chat request mutation")
)

// ChatRequestError is safe to return to a caller: it identifies the input
// class and location, but never includes raw request content.
type ChatRequestError struct {
	Kind         ChatRequestErrorKind
	MessageIndex int
	Path         string
	Err          error
}

func (e *ChatRequestError) Error() string {
	if e.MessageIndex >= 0 {
		return fmt.Sprintf("OpenAI chat request %s at messages[%d]%s: %v", e.Kind, e.MessageIndex, e.Path, e.Err)
	}
	return fmt.Sprintf("OpenAI chat request %s: %v", e.Kind, e.Err)
}

func (e *ChatRequestError) Unwrap() error { return e.Err }

type ChatResponseErrorKind string

const (
	ChatResponseEmptyBody           ChatResponseErrorKind = "empty_body"
	ChatResponseUnsupportedType     ChatResponseErrorKind = "unsupported_content_type"
	ChatResponseInvalidJSON         ChatResponseErrorKind = "invalid_json"
	ChatResponseUnsupportedResponse ChatResponseErrorKind = "unsupported_response"
	ChatResponseUnsupportedContent  ChatResponseErrorKind = "unsupported_content"
	ChatResponseInvalidMutation     ChatResponseErrorKind = "invalid_mutation"
)

var (
	ErrEmptyChatResponseBody          = errors.New("empty OpenAI chat response body")
	ErrUnsupportedChatResponseType    = errors.New("unsupported OpenAI chat response content type")
	ErrInvalidChatResponseJSON        = errors.New("invalid OpenAI chat response JSON")
	ErrUnsupportedChatResponse        = errors.New("unsupported OpenAI chat response")
	ErrUnsupportedChatResponseContent = errors.New("unsupported OpenAI chat response content")
	ErrInvalidChatResponseMutation    = errors.New("invalid OpenAI chat response mutation")
)

type ChatResponseError struct {
	Kind ChatResponseErrorKind
	Path string
	Err  error
}

func (e *ChatResponseError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("OpenAI chat response %s at %s: %v", e.Kind, e.Path, e.Err)
	}
	return fmt.Sprintf("OpenAI chat response %s: %v", e.Kind, e.Err)
}

func (e *ChatResponseError) Unwrap() error { return e.Err }

// ChatUserContent identifies one supported mutable field in a Chat Completions
// request. JSONPath is stable for diagnostics and does not contain content.
type ChatUserContent struct {
	MessageIndex int
	JSONPath     string
	Content      string
	valueStart   int
	valueEnd     int
}

// ChatContentMutation replaces exactly one user content field identified by
// MessageIndex. Mutations can only target entries returned by ParseChatRequest.
type ChatContentMutation struct {
	MessageIndex int
	Content      string
}

// ChatRequest is a gateway-neutral representation of the supported subset of
// an OpenAI Chat Completions request. Its raw body remains private so callers
// can only produce mutations through the checked method below.
type ChatRequest struct {
	UserContents []ChatUserContent
	body         []byte
}

// ChatAssistantContent identifies one supported assistant content field in a
// non-streaming Chat Completions response. JSONPath is safe for diagnostics and
// the source offsets allow a later masking phase to rewrite only this value.
type ChatAssistantContent struct {
	ChoiceIndex int
	JSONPath    string
	Content     string
	valueStart  int
	valueEnd    int
}

// ChatResponseContentMutation replaces exactly one assistant content field
// identified by ChoiceIndex. Mutations can only target entries returned by
// ParseChatResponse.
type ChatResponseContentMutation struct {
	ChoiceIndex int
	Content     string
}

type ChatResponse struct {
	AssistantContents []ChatAssistantContent
	body              []byte
	root              *jsonNode
}

func chatResponseError(kind ChatResponseErrorKind, path string, err error) *ChatResponseError {
	return &ChatResponseError{
		Kind: kind,
		Path: path,
		Err:  err,
	}
}

func ParseChatResponse(contentType string, body []byte) (*ChatResponse, error) {
	if !isJSONContentType(contentType) {
		return nil, chatResponseError(
			ChatResponseUnsupportedType,
			"",
			ErrUnsupportedChatResponseType,
		)
	}

	if len(bytes.TrimSpace(body)) == 0 {
		return nil, chatResponseError(
			ChatResponseEmptyBody,
			"",
			ErrEmptyChatResponseBody,
		)
	}

	parser := jsonSourceParser{source: body}
	root, err := parser.parseDocument()
	if err != nil {
		return nil, chatResponseError(
			ChatResponseInvalidJSON,
			"",
			fmt.Errorf("%w: %v", ErrInvalidChatResponseJSON, err),
		)
	}
	if root.kind != jsonObject {
		return nil, chatResponseError(
			ChatResponseUnsupportedResponse,
			"",
			ErrUnsupportedChatResponse,
		)
	}
	choices := root.object["choices"]
	if choices == nil || choices.kind != jsonArray {
		return nil, chatResponseError(
			ChatResponseUnsupportedResponse,
			".choices",
			ErrUnsupportedChatResponse,
		)
	}

	response := &ChatResponse{
		body: append([]byte(nil), body...),
		root: root,
	}
	for index, choice := range choices.array {
		path := fmt.Sprintf(".choices[%d].message.content", index)
		if choice.kind != jsonObject {
			return nil, chatResponseError(ChatResponseUnsupportedResponse, fmt.Sprintf(".choices[%d]", index), ErrUnsupportedChatResponse)
		}
		message := choice.object["message"]
		if message == nil || message.kind != jsonObject {
			return nil, chatResponseError(ChatResponseUnsupportedResponse, fmt.Sprintf(".choices[%d].message", index), ErrUnsupportedChatResponse)
		}
		role := message.object["role"]
		if role == nil || role.kind != jsonString || role.stringValue != "assistant" {
			return nil, chatResponseError(ChatResponseUnsupportedResponse, fmt.Sprintf(".choices[%d].message.role", index), ErrUnsupportedChatResponse)
		}
		content := message.object["content"]
		if content == nil || content.kind != jsonString {
			return nil, chatResponseError(ChatResponseUnsupportedContent, path, ErrUnsupportedChatResponseContent)
		}
		response.AssistantContents = append(response.AssistantContents, ChatAssistantContent{
			ChoiceIndex: index,
			JSONPath:    path,
			Content:     content.stringValue,
			valueStart:  content.start,
			valueEnd:    content.end,
		})
	}

	return response, nil
}

// Mutate serializes response-content replacements safely while retaining the
// exact original bytes for all unknown fields, choice ordering and untouched
// JSON values.
func (r *ChatResponse) Mutate(mutations []ChatResponseContentMutation) ([]byte, error) {
	if r == nil {
		return nil, chatResponseError(ChatResponseInvalidMutation, "", ErrInvalidChatResponseMutation)
	}
	if len(mutations) == 0 {
		return append([]byte(nil), r.body...), nil
	}
	targets := make(map[int]ChatAssistantContent, len(r.AssistantContents))
	for _, target := range r.AssistantContents {
		targets[target.ChoiceIndex] = target
	}
	replacements := make(map[int][]byte, len(mutations))
	for _, mutation := range mutations {
		path := fmt.Sprintf(".choices[%d].message.content", mutation.ChoiceIndex)
		if _, exists := replacements[mutation.ChoiceIndex]; exists {
			return nil, chatResponseError(ChatResponseInvalidMutation, path, ErrInvalidChatResponseMutation)
		}
		if _, found := targets[mutation.ChoiceIndex]; !found {
			return nil, chatResponseError(ChatResponseInvalidMutation, path, ErrInvalidChatResponseMutation)
		}
		encoded, err := json.Marshal(mutation.Content)
		if err != nil {
			return nil, chatResponseError(ChatResponseInvalidMutation, path, fmt.Errorf("%w: %v", ErrInvalidChatResponseMutation, err))
		}
		replacements[mutation.ChoiceIndex] = encoded
	}
	result := make([]byte, 0, len(r.body))
	cursor := 0
	for _, target := range r.AssistantContents {
		replacement, changed := replacements[target.ChoiceIndex]
		if !changed {
			continue
		}
		result = append(result, r.body[cursor:target.valueStart]...)
		result = append(result, replacement...)
		cursor = target.valueEnd
	}
	return append(result, r.body[cursor:]...), nil
}

// ParseChatRequest accepts only an application/json OpenAI Chat Completions
// request. It extracts role=user string content fields and preserves enough
// source offsets to safely rewrite only those JSON string values later.
func ParseChatRequest(contentType string, body []byte) (*ChatRequest, error) {
	if !isJSONContentType(contentType) {
		return nil, chatRequestError(ChatRequestUnsupportedType, -1, "", ErrUnsupportedChatContentType)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, chatRequestError(ChatRequestEmptyBody, -1, "", ErrEmptyChatRequestBody)
	}
	parser := jsonSourceParser{source: body}
	root, err := parser.parseDocument()
	if err != nil {
		return nil, chatRequestError(ChatRequestInvalidJSON, -1, "", fmt.Errorf("%w: %v", ErrInvalidChatRequestJSON, err))
	}
	if root.kind != jsonObject {
		return nil, chatRequestError(ChatRequestUnsupportedRequest, -1, "", ErrUnsupportedChatRequest)
	}
	// stream controls the upstream response representation. Request-side
	// extraction and mutation are identical for streamed and buffered Chat
	// Completions requests; response processing chooses the SSE path later.
	messages := root.object["messages"]
	if messages == nil || messages.kind != jsonArray {
		return nil, chatRequestError(ChatRequestUnsupportedRequest, -1, ".messages", ErrUnsupportedChatRequest)
	}

	request := &ChatRequest{body: append([]byte(nil), body...)}
	for index, message := range messages.array {
		path := fmt.Sprintf(".messages[%d].content", index)
		if message.kind != jsonObject {
			return nil, chatRequestError(ChatRequestUnsupportedRequest, index, "", ErrUnsupportedChatRequest)
		}
		role := message.object["role"]
		if role == nil || role.kind != jsonString || role.stringValue != "user" {
			continue
		}
		content := message.object["content"]
		if content == nil || content.kind != jsonString {
			return nil, chatRequestError(ChatRequestUnsupportedContent, index, ".content", ErrUnsupportedChatContent)
		}
		request.UserContents = append(request.UserContents, ChatUserContent{
			MessageIndex: index,
			JSONPath:     path,
			Content:      content.stringValue,
			valueStart:   content.start,
			valueEnd:     content.end,
		})
	}
	return request, nil
}

// Mutate serializes replacements safely while retaining the exact original
// bytes for all unknown fields, message ordering and untouched JSON values.
func (r *ChatRequest) Mutate(mutations []ChatContentMutation) ([]byte, error) {
	if r == nil {
		return nil, chatRequestError(ChatRequestInvalidMutation, -1, "", ErrInvalidChatMutation)
	}
	if len(mutations) == 0 {
		return append([]byte(nil), r.body...), nil
	}
	targets := make(map[int]ChatUserContent, len(r.UserContents))
	for _, target := range r.UserContents {
		targets[target.MessageIndex] = target
	}
	replacements := make(map[int][]byte, len(mutations))
	for _, mutation := range mutations {
		if _, exists := replacements[mutation.MessageIndex]; exists {
			return nil, chatRequestError(ChatRequestInvalidMutation, mutation.MessageIndex, ".content", ErrInvalidChatMutation)
		}
		if _, found := targets[mutation.MessageIndex]; !found {
			return nil, chatRequestError(ChatRequestInvalidMutation, mutation.MessageIndex, ".content", ErrInvalidChatMutation)
		}
		encoded, err := json.Marshal(mutation.Content)
		if err != nil {
			return nil, chatRequestError(ChatRequestInvalidMutation, mutation.MessageIndex, ".content", fmt.Errorf("%w: %v", ErrInvalidChatMutation, err))
		}
		replacements[mutation.MessageIndex] = encoded
	}
	result := make([]byte, 0, len(r.body))
	cursor := 0
	for _, target := range r.UserContents {
		replacement, changed := replacements[target.MessageIndex]
		if !changed {
			continue
		}
		result = append(result, r.body[cursor:target.valueStart]...)
		result = append(result, replacement...)
		cursor = target.valueEnd
	}
	return append(result, r.body[cursor:]...), nil
}

func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func chatRequestError(kind ChatRequestErrorKind, messageIndex int, path string, err error) *ChatRequestError {
	return &ChatRequestError{Kind: kind, MessageIndex: messageIndex, Path: path, Err: err}
}

type jsonNodeKind uint8

const (
	jsonObject jsonNodeKind = iota
	jsonArray
	jsonString
	jsonBoolean
	jsonOther
)

type jsonNode struct {
	kind        jsonNodeKind
	start, end  int
	stringValue string
	boolean     bool
	object      map[string]*jsonNode
	array       []*jsonNode
}

// jsonSourceParser validates JSON while retaining byte offsets. It is limited
// to the JSON grammar; it has no OpenAI- or gateway-specific dependency.
type jsonSourceParser struct {
	source []byte
	pos    int
}

func (p *jsonSourceParser) parseDocument() (*jsonNode, error) {
	p.skipSpace()
	node, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos != len(p.source) {
		return nil, fmt.Errorf("unexpected trailing data")
	}
	return node, nil
}

func (p *jsonSourceParser) parseValue() (*jsonNode, error) {
	p.skipSpace()
	if p.pos >= len(p.source) {
		return nil, fmt.Errorf("unexpected end of input")
	}
	switch p.source[p.pos] {
	case '{':
		return p.parseObject()
	case '[':
		return p.parseArray()
	case '"':
		return p.parseStringNode()
	case 't', 'f':
		return p.parseBoolean()
	case 'n':
		return p.parseLiteral("null", jsonOther)
	default:
		return p.parseNumber()
	}
}

func (p *jsonSourceParser) parseObject() (*jsonNode, error) {
	start := p.pos
	p.pos++
	node := &jsonNode{kind: jsonObject, start: start, object: make(map[string]*jsonNode)}
	p.skipSpace()
	if p.consume('}') {
		node.end = p.pos
		return node, nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.source) || p.source[p.pos] != '"' {
			return nil, fmt.Errorf("object key must be a string")
		}
		key, err := p.parseStringNode()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if !p.consume(':') {
			return nil, fmt.Errorf("object key is missing colon")
		}
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		node.object[key.stringValue] = value
		p.skipSpace()
		if p.consume('}') {
			node.end = p.pos
			return node, nil
		}
		if !p.consume(',') {
			return nil, fmt.Errorf("object is missing comma")
		}
	}
}

func (p *jsonSourceParser) parseArray() (*jsonNode, error) {
	start := p.pos
	p.pos++
	node := &jsonNode{kind: jsonArray, start: start}
	p.skipSpace()
	if p.consume(']') {
		node.end = p.pos
		return node, nil
	}
	for {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		node.array = append(node.array, value)
		p.skipSpace()
		if p.consume(']') {
			node.end = p.pos
			return node, nil
		}
		if !p.consume(',') {
			return nil, fmt.Errorf("array is missing comma")
		}
	}
}

func (p *jsonSourceParser) parseStringNode() (*jsonNode, error) {
	start := p.pos
	p.pos++
	escaped := false
	for p.pos < len(p.source) {
		character := p.source[p.pos]
		p.pos++
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			raw := p.source[start:p.pos]
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			return &jsonNode{kind: jsonString, start: start, end: p.pos, stringValue: value}, nil
		}
		if character < 0x20 {
			return nil, fmt.Errorf("unescaped control character in string")
		}
	}
	return nil, fmt.Errorf("unterminated string")
}

func (p *jsonSourceParser) parseBoolean() (*jsonNode, error) {
	if bytes.HasPrefix(p.source[p.pos:], []byte("true")) {
		return p.parseLiteral("true", jsonBoolean)
	}
	if bytes.HasPrefix(p.source[p.pos:], []byte("false")) {
		node, err := p.parseLiteral("false", jsonBoolean)
		if node != nil {
			node.boolean = false
		}
		return node, err
	}
	return nil, fmt.Errorf("invalid boolean")
}

func (p *jsonSourceParser) parseLiteral(literal string, kind jsonNodeKind) (*jsonNode, error) {
	if !bytes.HasPrefix(p.source[p.pos:], []byte(literal)) {
		return nil, fmt.Errorf("invalid literal")
	}
	start := p.pos
	p.pos += len(literal)
	node := &jsonNode{kind: kind, start: start, end: p.pos}
	if literal == "true" {
		node.boolean = true
	}
	return node, nil
}

func (p *jsonSourceParser) parseNumber() (*jsonNode, error) {
	start := p.pos
	for p.pos < len(p.source) && !isJSONDelimiter(p.source[p.pos]) {
		p.pos++
	}
	if start == p.pos {
		return nil, fmt.Errorf("invalid value")
	}
	if !json.Valid(p.source[start:p.pos]) {
		return nil, fmt.Errorf("invalid number")
	}
	return &jsonNode{kind: jsonOther, start: start, end: p.pos}, nil
}

func (p *jsonSourceParser) skipSpace() {
	for p.pos < len(p.source) && (p.source[p.pos] == ' ' || p.source[p.pos] == '\n' || p.source[p.pos] == '\r' || p.source[p.pos] == '\t') {
		p.pos++
	}
}

func (p *jsonSourceParser) consume(want byte) bool {
	if p.pos < len(p.source) && p.source[p.pos] == want {
		p.pos++
		return true
	}
	return false
}

func isJSONDelimiter(value byte) bool {
	return value == ' ' || value == '\n' || value == '\r' || value == '\t' || value == ',' || value == ']' || value == '}'
}
