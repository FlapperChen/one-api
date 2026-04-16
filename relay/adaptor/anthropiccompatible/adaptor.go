package anthropiccompatible

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/relay/adaptor"
	"github.com/songquanpeng/one-api/relay/meta"
	"github.com/songquanpeng/one-api/relay/model"
)

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
	// Pass through the request body directly without conversion
	// The request body is already in Anthropic format
	return nil, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, meta *meta.Meta, requestBody io.Reader) (*http.Response, error) {
	return adaptor.DoRequestHelper(a, c, meta, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode) {
	// Pass through the response directly without conversion
	for k, v := range resp.Header {
		for _, vv := range v {
			c.Writer.Header().Set(k, vv)
		}
	}

	c.Writer.WriteHeader(resp.StatusCode)
	if _, gerr := io.Copy(c.Writer, resp.Body); gerr != nil {
		return nil, &model.ErrorWithStatusCode{
			StatusCode: http.StatusInternalServerError,
			Error: model.Error{
				Message: gerr.Error(),
			},
		}
	}

	return nil, nil
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