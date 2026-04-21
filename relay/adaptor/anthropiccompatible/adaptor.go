package anthropiccompatible

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/common/render"
	"github.com/songquanpeng/one-api/relay/adaptor"
	"github.com/songquanpeng/one-api/relay/meta"
	"github.com/songquanpeng/one-api/relay/model"
)

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	// For tool_use blocks
	Name      string `json:"name,omitempty"`
	Input     any    `json:"input,omitempty"`
	Id        string `json:"id,omitempty"`
	ToolUseId string `json:"tool_use_id,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Response struct {
	Id         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Model      string         `json:"model"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
	Error      ResponseError  `json:"error"`
}

type ResponseError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

var _ adaptor.Adaptor = new(Adaptor)

const channelName = "anthropic-compatible"

type Adaptor struct{}

func (a *Adaptor) Init(meta *meta.Meta) {
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	// Use /v1/messages for Anthropic compatible format
	return fmt.Sprintf("%s/v1/messages", meta.BaseURL), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Request, meta *meta.Meta) error {
	adaptor.SetupCommonRequestHeader(c, req, meta)

	// Set Anthropic specific headers
	req.Header.Set("x-api-key", meta.APIKey)

	anthropicVersion := c.Request.Header.Get("anthropic-version")
	if anthropicVersion == "" {
		anthropicVersion = "2023-06-01"
	}
	req.Header.Set("anthropic-version", anthropicVersion)

	// Pass through anthropic-beta header if present
	anthropicBeta := c.Request.Header.Get("anthropic-beta")
	if anthropicBeta != "" {
		req.Header.Set("anthropic-beta", anthropicBeta)
	}

	return nil
}

func (a *Adaptor) ConvertRequest(c *gin.Context, relayMode int, request *model.GeneralOpenAIRequest) (any, error) {
	// Check if request body exists
	if c.Request.Body == nil {
		// Test context: build request from request object
		logger.Infof(c.Request.Context(), "[AnthropicCompatible] ConvertRequest: no body, building from request object")
		return buildAnthropicRequest(request), nil
	}

	// Step 1: Read raw request from cache
	bodyBytes, err := common.GetRequestBody(c)
	if err != nil {
		logger.Errorf(c.Request.Context(), "[AnthropicCompatible] GetRequestBody error: %v", err)
		return nil, err
	}

	// Step 2: Log raw request for debugging
	bodyStr := string(bodyBytes)
	if len(bodyStr) > 500 {
		logger.Infof(c.Request.Context(), "[AnthropicCompatible] Raw request: %s...", bodyStr[:500])
	} else {
		logger.Infof(c.Request.Context(), "[AnthropicCompatible] Raw request: %s", bodyStr)
	}

	// Step 3: Pass through (return nil, nil to let getRequestBody use cached body)
	return nil, nil
}

func buildAnthropicRequest(request *model.GeneralOpenAIRequest) map[string]interface{} {
	maxTokens := request.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	anthropicRequest := map[string]interface{}{
		"model":      request.Model,
		"messages":   convertMessages(request.Messages),
		"max_tokens": maxTokens,
	}

	if request.Temperature != nil {
		anthropicRequest["temperature"] = *request.Temperature
	}
	if request.TopP != nil {
		anthropicRequest["top_p"] = *request.TopP
	}
	if request.TopK != 0 {
		anthropicRequest["top_k"] = request.TopK
	}

	// Convert tools to Anthropic format
	if len(request.Tools) > 0 {
		claudeTools := make([]map[string]interface{}, 0, len(request.Tools))
		for _, tool := range request.Tools {
			if params, ok := tool.Function.Parameters.(map[string]any); ok {
				claudeTools = append(claudeTools, map[string]interface{}{
					"name":        tool.Function.Name,
					"description": tool.Function.Description,
					"input_schema": params,
				})
			}
		}
		if len(claudeTools) > 0 {
			anthropicRequest["tools"] = claudeTools
		}
	}

	// Convert tool_choice
	if request.ToolChoice != nil {
		if choiceStr, ok := request.ToolChoice.(string); ok {
			anthropicRequest["tool_choice"] = map[string]interface{}{"type": choiceStr}
		} else if choiceMap, ok := request.ToolChoice.(map[string]any); ok {
			anthropicRequest["tool_choice"] = choiceMap
		}
	}

	// Reasoning effort (Anthropic extended thinking)
	if request.ReasoningEffort != nil {
		anthropicRequest["thinking"] = map[string]interface{}{
			"type":         "enabled",
			"budget_tokens": request.ReasoningEffort,
		}
	}

	return anthropicRequest
}

func convertMessages(messages []model.Message) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))
	var systemPrompt string
	filteredMessages := make([]model.Message, 0)

	for _, msg := range messages {
		if msg.Role == "system" {
			if content, ok := msg.Content.(string); ok {
				systemPrompt = content
			}
			continue
		}
		filteredMessages = append(filteredMessages, msg)
	}

	for _, msg := range filteredMessages {
		message := map[string]interface{}{
			"role": msg.Role,
		}
		if content, ok := msg.Content.(string); ok {
			message["content"] = content
		} else {
			message["content"] = msg.Content
		}
		result = append(result, message)
	}

	// Prepend system prompt if present
	if systemPrompt != "" {
		result = append([]map[string]interface{}{
			{"role": "system", "content": systemPrompt},
		}, result...)
	}

	return result
}

func (a *Adaptor) DoRequest(c *gin.Context, meta *meta.Meta, requestBody io.Reader) (*http.Response, error) {
	return adaptor.DoRequestHelper(a, c, meta, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode) {
	logger.Infof(c.Request.Context(), "[AnthropicCompatible] DoResponse called, APIType=%d, ChannelType=%d, IsStream=%v", meta.APIType, meta.ChannelType, meta.IsStream)

	// Check if this is a streaming response (starts with SSE format)
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		logger.Errorf(c.Request.Context(), "[AnthropicCompatible] ReadAll error: %v", readErr)
		return nil, &model.ErrorWithStatusCode{
			StatusCode: http.StatusInternalServerError,
			Error: model.Error{
				Message: readErr.Error(),
			},
		}
	}

	// Detect streaming response by checking first few bytes
	respLen := len(responseBody)
	if respLen > 10 && strings.HasPrefix(string(responseBody[:10]), "event:") {
		logger.Infof(c.Request.Context(), "[AnthropicCompatible] Detected streaming response, using StreamHandler")

		// Restore body for StreamHandler to read
		resp.Body = io.NopCloser(strings.NewReader(string(responseBody)))

		// For streaming, we can't pass through in DoResponse since the body is already read
		// Call StreamHandler which will handle the stream
		streamErr, streamUsage := a.StreamHandler(c, resp)
		return streamUsage, streamErr
	}

	// DEBUG: Log full response body (truncate for safety)
	logger.Infof(c.Request.Context(), "[AnthropicCompatible] Response body length: %d bytes", respLen)
	if respLen > 500 {
		logger.Infof(c.Request.Context(), "[AnthropicCompatible] Response body (first 500 chars): %s", string(responseBody[:500]))
	} else {
		logger.Infof(c.Request.Context(), "[AnthropicCompatible] Response body: %s", string(responseBody))
	}

	// Step 1: PRIORITY - Pass through the raw response first (regardless of parsing success)
	passThroughResponse(c, resp, responseBody)

	// Step 2: Then parse usage for statistics (doesn't affect the already-passed-through response)
	var claudeResponse Response
	if unmarshalErr := json.Unmarshal(responseBody, &claudeResponse); unmarshalErr != nil {
		logger.Errorf(c.Request.Context(), "[AnthropicCompatible] Unmarshal error: %v", unmarshalErr)
		return nil, nil
	}

	// Check for error in response
	if claudeResponse.Error.Type != "" {
		logger.Errorf(c.Request.Context(), "[AnthropicCompatible] Response error: Type=%s, Message=%s",
			claudeResponse.Error.Type, claudeResponse.Error.Message)
		return nil, &model.ErrorWithStatusCode{
			StatusCode: resp.StatusCode,
			Error: model.Error{
				Message: claudeResponse.Error.Message,
				Type:    claudeResponse.Error.Type,
			},
		}
	}

	// Extract usage from the response
	usage = &model.Usage{
		PromptTokens:     claudeResponse.Usage.InputTokens,
		CompletionTokens: claudeResponse.Usage.OutputTokens,
		TotalTokens:      claudeResponse.Usage.InputTokens + claudeResponse.Usage.OutputTokens,
	}

	logger.Infof(c.Request.Context(), "[AnthropicCompatible] Parsed usage: input=%d, output=%d, total=%d",
		usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)

	return usage, nil
}

func passThroughResponse(c *gin.Context, resp *http.Response, responseBody []byte) {
	// Pass through the response headers
	for k, v := range resp.Header {
		for _, vv := range v {
			c.Writer.Header().Set(k, vv)
		}
	}

	// Write the response body
	c.Writer.WriteHeader(resp.StatusCode)
	if _, writeErr := c.Writer.Write(responseBody); writeErr != nil {
		logger.Errorf(c.Request.Context(), "[AnthropicCompatible] Write response error: %v", writeErr)
	}
}

func (a *Adaptor) GetModelList() []string {
	// Return empty list, models will be dynamically loaded from channel config
	return nil
}

func (a *Adaptor) GetChannelName() string {
	return channelName
}

func (a *Adaptor) ConvertImageRequest(request *model.ImageRequest) (any, error) {
	// Not implemented for now
	return nil, nil
}

// ShouldUseRawRequestBody returns true to indicate we should use the raw request body
func (a *Adaptor) ShouldUseRawRequestBody(c *gin.Context, meta *meta.Meta) bool {
	// Check if the request Content-Type is application/json
	contentType := c.Request.Header.Get("Content-Type")
	return strings.HasPrefix(contentType, "application/json")
}

// StreamHandler handles streaming responses for Claude Code compatibility
func (a *Adaptor) StreamHandler(c *gin.Context, resp *http.Response) (err *model.ErrorWithStatusCode, usage *model.Usage) {
	logger.Infof(c.Request.Context(), "[AnthropicCompatible] StreamHandler called")

	common.SetEventStreamHeaders(c)

	// Pass through all response headers
	for k, v := range resp.Header {
		for _, vv := range v {
			c.Writer.Header().Set(k, vv)
		}
	}

	// Create a TeeReader to both count tokens and pass through the stream
	var promptTokens, completionTokens int
	reader := resp.Body

	// Process stream line by line
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)

	for {
		n, readErr := reader.Read(tmp)
		if n > 0 {
			// Pass through to client
			if _, writeErr := c.Writer.Write(tmp[:n]); writeErr != nil {
				logger.Errorf(c.Request.Context(), "[AnthropicCompatible] Write error: %v", writeErr)
				break
			}
			c.Writer.Flush()

			// Collect data for parsing
			buf = append(buf, tmp[:n]...)

			// Process complete lines
			for {
				lineEnd := -1
				for i := 0; i < len(buf); i++ {
					if buf[i] == '\n' {
						lineEnd = i
						break
					}
				}
				if lineEnd < 0 {
					break
				}

				line := string(buf[:lineEnd])
				buf = buf[lineEnd+1:]

				// Parse SSE line
				if strings.HasPrefix(line, "data:") {
					line = strings.TrimPrefix(line, "data:")
					line = strings.TrimSpace(line)

					// Try to parse as message_delta event which contains usage
					if strings.Contains(line, "\"type\":\"message_delta\"") {
						var deltaEvent StreamMessageDelta
						if jsonErr := json.Unmarshal([]byte(line), &deltaEvent); jsonErr == nil {
							completionTokens = deltaEvent.Usage.OutputTokens
							logger.Infof(c.Request.Context(), "[AnthropicCompatible] Stream usage: output=%d", completionTokens)
						}
					}

					// Parse message_start to get input tokens
					if strings.Contains(line, "\"type\":\"message_start\"") {
						var startEvent StreamMessageStart
						if jsonErr := json.Unmarshal([]byte(line), &startEvent); jsonErr == nil {
							promptTokens = startEvent.Message.Usage.InputTokens
						}
					}
				}
			}
		}

		if readErr != nil {
			break
		}
	}

	// Write any remaining buffer
	if len(buf) > 0 {
		c.Writer.Write(buf)
		c.Writer.Flush()
	}

	render.Done(c)

	usage = &model.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}

	logger.Infof(c.Request.Context(), "[AnthropicCompatible] Stream complete: input=%d, output=%d, total=%d",
		promptTokens, completionTokens, promptTokens+completionTokens)

	return nil, usage
}

// Stream event structures
type StreamMessageStart struct {
	Type    string `json:"type"`
	Message struct {
		Id      string `json:"id"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Model   string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage   Usage  `json:"usage"`
	} `json:"message"`
}

type StreamMessageDelta struct {
	Type    string `json:"type"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}