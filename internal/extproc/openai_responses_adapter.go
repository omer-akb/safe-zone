package extproc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ResponsesErrorKind classifies OpenAI Responses API payload errors without
// including request or response content in diagnostics.
type ResponsesErrorKind string

const (
	ResponsesEmptyBody          ResponsesErrorKind = "empty_body"
	ResponsesUnsupportedType    ResponsesErrorKind = "unsupported_content_type"
	ResponsesInvalidJSON        ResponsesErrorKind = "invalid_json"
	ResponsesUnsupportedPayload ResponsesErrorKind = "unsupported_payload"
	ResponsesUnsupportedContent ResponsesErrorKind = "unsupported_content"
	ResponsesInvalidMutation    ResponsesErrorKind = "invalid_mutation"
)

var (
	ErrEmptyResponsesBody          = errors.New("empty OpenAI Responses API body")
	ErrUnsupportedResponsesType    = errors.New("unsupported OpenAI Responses API content type")
	ErrInvalidResponsesJSON        = errors.New("invalid OpenAI Responses API JSON")
	ErrUnsupportedResponsesPayload = errors.New("unsupported OpenAI Responses API payload")
	ErrUnsupportedResponsesContent = errors.New("unsupported OpenAI Responses API content")
	ErrInvalidResponsesMutation    = errors.New("invalid OpenAI Responses API mutation")
)

// ResponsesError is safe to return to callers and logs. Path identifies only
// the structural location of invalid content and never contains its value.
type ResponsesError struct {
	Kind ResponsesErrorKind
	Path string
	Err  error
}

func (e *ResponsesError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("OpenAI Responses API %s at %s: %v", e.Kind, e.Path, e.Err)
	}
	return fmt.Sprintf("OpenAI Responses API %s: %v", e.Kind, e.Err)
}

func (e *ResponsesError) Unwrap() error { return e.Err }

func responsesError(kind ResponsesErrorKind, path string, err error) *ResponsesError {
	return &ResponsesError{Kind: kind, Path: path, Err: err}
}

// ResponsesTextContent identifies one supported mutable JSON string. The
// stable integer ID is used instead of trusting caller-supplied JSON paths.
type ResponsesTextContent struct {
	ID       int
	Role     string
	JSONPath string
	Content  string
	start    int
	end      int
}

type ResponsesContentMutation struct {
	ID      int
	Content string
}

// ResponsesRequest represents the text-only subset introduced in the first
// Phase 6 slice: top-level string input, instructions, and role=user or
// role=system message input. Other roles, tool payloads and multimodal fields
// are intentionally left for their separately tracked Phase 6 capabilities.
type ResponsesRequest struct {
	Contents []ResponsesTextContent
	body     []byte
}

// ParseResponsesRequest extracts supported system and user text while retaining
// source offsets so mutations leave all unrelated and unknown JSON
// byte-for-byte unchanged.
func ParseResponsesRequest(contentType string, body []byte) (*ResponsesRequest, error) {
	root, err := parseResponsesDocument(contentType, body)
	if err != nil {
		return nil, err
	}
	input := root.object["input"]
	request := &ResponsesRequest{body: append([]byte(nil), body...)}
	if instructions := root.object["instructions"]; instructions != nil {
		if instructions.kind != jsonString {
			return nil, responsesError(ResponsesUnsupportedContent, ".instructions", ErrUnsupportedResponsesContent)
		}
		request.addContent("system", ".instructions", instructions)
	}
	// Responses can continue a stored conversation or previous response without
	// supplying new input. Require a Responses-specific context field so an
	// invalid Chat Completions payload cannot be misclassified and allowed.
	if input == nil {
		for _, marker := range []string{"previous_response_id", "conversation", "prompt", "instructions"} {
			if root.object[marker] != nil {
				return request, nil
			}
		}
		return nil, responsesError(ResponsesUnsupportedPayload, ".input", ErrUnsupportedResponsesPayload)
	}
	switch input.kind {
	case jsonString:
		request.addContent("user", ".input", input)
	case jsonArray:
		for itemIndex, item := range input.array {
			path := fmt.Sprintf(".input[%d]", itemIndex)
			if item.kind != jsonObject {
				return nil, responsesError(ResponsesUnsupportedContent, path, ErrUnsupportedResponsesContent)
			}
			role := item.object["role"]
			if role == nil || role.kind != jsonString || (role.stringValue != "user" && role.stringValue != "system") {
				continue
			}
			content := item.object["content"]
			if content == nil {
				return nil, responsesError(ResponsesUnsupportedContent, path+".content", ErrUnsupportedResponsesContent)
			}
			switch content.kind {
			case jsonString:
				request.addContent(role.stringValue, path+".content", content)
			case jsonArray:
				for contentIndex, part := range content.array {
					partPath := fmt.Sprintf("%s.content[%d]", path, contentIndex)
					if part.kind != jsonObject {
						return nil, responsesError(ResponsesUnsupportedContent, partPath, ErrUnsupportedResponsesContent)
					}
					typeNode := part.object["type"]
					if typeNode == nil || typeNode.kind != jsonString {
						return nil, responsesError(ResponsesUnsupportedContent, partPath+".type", ErrUnsupportedResponsesContent)
					}
					if typeNode.stringValue != "input_text" {
						continue
					}
					text := part.object["text"]
					if text == nil || text.kind != jsonString {
						return nil, responsesError(ResponsesUnsupportedContent, partPath+".text", ErrUnsupportedResponsesContent)
					}
					request.addContent(role.stringValue, partPath+".text", text)
				}
			default:
				return nil, responsesError(ResponsesUnsupportedContent, path+".content", ErrUnsupportedResponsesContent)
			}
		}
	default:
		return nil, responsesError(ResponsesUnsupportedContent, ".input", ErrUnsupportedResponsesContent)
	}
	return request, nil
}

func (r *ResponsesRequest) addContent(role, path string, node *jsonNode) {
	r.Contents = append(r.Contents, ResponsesTextContent{
		ID: len(r.Contents), Role: role, JSONPath: path, Content: node.stringValue, start: node.start, end: node.end,
	})
}

func (r *ResponsesRequest) Mutate(mutations []ResponsesContentMutation) ([]byte, error) {
	if r == nil {
		return nil, responsesError(ResponsesInvalidMutation, "", ErrInvalidResponsesMutation)
	}
	return mutateResponsesContents(r.body, r.Contents, mutations, nil)
}

// ResponsesResponse represents assistant output_text blocks in a buffered
// Responses API response. Tool outputs, refusals and streaming events are not
// treated as assistant text by this first implementation slice.
type ResponsesResponse struct {
	AssistantContents []ResponsesTextContent
	body              []byte
	outputText        *jsonNode
}

func ParseResponsesResponse(contentType string, body []byte) (*ResponsesResponse, error) {
	root, err := parseResponsesDocument(contentType, body)
	if err != nil {
		return nil, err
	}
	object := root.object["object"]
	if object == nil || object.kind != jsonString || object.stringValue != "response" {
		return nil, responsesError(ResponsesUnsupportedPayload, ".object", ErrUnsupportedResponsesPayload)
	}
	output := root.object["output"]
	if output == nil || output.kind != jsonArray {
		return nil, responsesError(ResponsesUnsupportedPayload, ".output", ErrUnsupportedResponsesPayload)
	}

	response := &ResponsesResponse{body: append([]byte(nil), body...)}
	if outputText := root.object["output_text"]; outputText != nil {
		if outputText.kind != jsonString {
			return nil, responsesError(ResponsesUnsupportedContent, ".output_text", ErrUnsupportedResponsesContent)
		}
		response.outputText = outputText
	}
	for itemIndex, item := range output.array {
		itemPath := fmt.Sprintf(".output[%d]", itemIndex)
		if item.kind != jsonObject {
			return nil, responsesError(ResponsesUnsupportedContent, itemPath, ErrUnsupportedResponsesContent)
		}
		typeNode := item.object["type"]
		if typeNode == nil || typeNode.kind != jsonString || typeNode.stringValue != "message" {
			continue
		}
		role := item.object["role"]
		if role == nil || role.kind != jsonString || role.stringValue != "assistant" {
			continue
		}
		content := item.object["content"]
		if content == nil || content.kind != jsonArray {
			return nil, responsesError(ResponsesUnsupportedContent, itemPath+".content", ErrUnsupportedResponsesContent)
		}
		for contentIndex, part := range content.array {
			partPath := fmt.Sprintf("%s.content[%d]", itemPath, contentIndex)
			if part.kind != jsonObject {
				return nil, responsesError(ResponsesUnsupportedContent, partPath, ErrUnsupportedResponsesContent)
			}
			partType := part.object["type"]
			if partType == nil || partType.kind != jsonString {
				return nil, responsesError(ResponsesUnsupportedContent, partPath+".type", ErrUnsupportedResponsesContent)
			}
			if partType.stringValue != "output_text" {
				continue
			}
			text := part.object["text"]
			if text == nil || text.kind != jsonString {
				return nil, responsesError(ResponsesUnsupportedContent, partPath+".text", ErrUnsupportedResponsesContent)
			}
			response.AssistantContents = append(response.AssistantContents, ResponsesTextContent{
				ID: len(response.AssistantContents), JSONPath: partPath + ".text", Content: text.stringValue, start: text.start, end: text.end,
			})
		}
	}
	// output_text is normally a convenience aggregation of message output. If a
	// compatible proxy returns it without the underlying message blocks, inspect
	// it directly so visible assistant text never bypasses the guardrail.
	if response.outputText != nil && len(response.AssistantContents) == 0 {
		response.AssistantContents = append(response.AssistantContents, ResponsesTextContent{
			ID: 0, JSONPath: ".output_text", Content: response.outputText.stringValue,
			start: response.outputText.start, end: response.outputText.end,
		})
	}
	return response, nil
}

func (r *ResponsesResponse) Mutate(mutations []ResponsesContentMutation) ([]byte, error) {
	if r == nil {
		return nil, responsesError(ResponsesInvalidMutation, "", ErrInvalidResponsesMutation)
	}
	var derivedOutputText *string
	if r.outputText != nil && len(r.AssistantContents) > 0 && r.AssistantContents[0].JSONPath != ".output_text" && len(mutations) > 0 {
		values := make(map[int]string, len(r.AssistantContents))
		for _, content := range r.AssistantContents {
			values[content.ID] = content.Content
		}
		for _, mutation := range mutations {
			values[mutation.ID] = mutation.Content
		}
		var combined string
		for _, content := range r.AssistantContents {
			combined += values[content.ID]
		}
		derivedOutputText = &combined
	}
	return mutateResponsesContents(r.body, r.AssistantContents, mutations, func(targets *[]sourceReplacement) error {
		if derivedOutputText == nil {
			return nil
		}
		encoded, err := json.Marshal(*derivedOutputText)
		if err != nil {
			return err
		}
		*targets = append(*targets, sourceReplacement{start: r.outputText.start, end: r.outputText.end, value: encoded})
		return nil
	})
}

type sourceReplacement struct {
	start int
	end   int
	value []byte
}

func mutateResponsesContents(body []byte, contents []ResponsesTextContent, mutations []ResponsesContentMutation, extras func(*[]sourceReplacement) error) ([]byte, error) {
	if len(mutations) == 0 {
		return append([]byte(nil), body...), nil
	}
	targets := make(map[int]ResponsesTextContent, len(contents))
	for _, content := range contents {
		targets[content.ID] = content
	}
	seen := make(map[int]struct{}, len(mutations))
	replacements := make([]sourceReplacement, 0, len(mutations)+1)
	for _, mutation := range mutations {
		if _, duplicate := seen[mutation.ID]; duplicate {
			return nil, responsesError(ResponsesInvalidMutation, "", ErrInvalidResponsesMutation)
		}
		target, found := targets[mutation.ID]
		if !found {
			return nil, responsesError(ResponsesInvalidMutation, "", ErrInvalidResponsesMutation)
		}
		encoded, err := json.Marshal(mutation.Content)
		if err != nil {
			return nil, responsesError(ResponsesInvalidMutation, target.JSONPath, fmt.Errorf("%w: %v", ErrInvalidResponsesMutation, err))
		}
		seen[mutation.ID] = struct{}{}
		replacements = append(replacements, sourceReplacement{start: target.start, end: target.end, value: encoded})
	}
	if extras != nil {
		if err := extras(&replacements); err != nil {
			return nil, responsesError(ResponsesInvalidMutation, "", fmt.Errorf("%w: %v", ErrInvalidResponsesMutation, err))
		}
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start < replacements[j].start })
	result := make([]byte, 0, len(body))
	cursor := 0
	for _, replacement := range replacements {
		if replacement.start < cursor || replacement.end < replacement.start || replacement.end > len(body) {
			return nil, responsesError(ResponsesInvalidMutation, "", ErrInvalidResponsesMutation)
		}
		result = append(result, body[cursor:replacement.start]...)
		result = append(result, replacement.value...)
		cursor = replacement.end
	}
	return append(result, body[cursor:]...), nil
}

func parseResponsesDocument(contentType string, body []byte) (*jsonNode, error) {
	if !isJSONContentType(contentType) {
		return nil, responsesError(ResponsesUnsupportedType, "", ErrUnsupportedResponsesType)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, responsesError(ResponsesEmptyBody, "", ErrEmptyResponsesBody)
	}
	parser := jsonSourceParser{source: body}
	root, err := parser.parseDocument()
	if err != nil {
		return nil, responsesError(ResponsesInvalidJSON, "", fmt.Errorf("%w: %v", ErrInvalidResponsesJSON, err))
	}
	if root.kind != jsonObject {
		return nil, responsesError(ResponsesUnsupportedPayload, "", ErrUnsupportedResponsesPayload)
	}
	return root, nil
}
