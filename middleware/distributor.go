package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/songquanpeng/one-api/common/ctxkey"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/model"
	"github.com/songquanpeng/one-api/relay/channeltype"
)

type ModelRequest struct {
	Model string `json:"model" form:"model"`
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		userId := c.GetInt(ctxkey.Id)
		userGroup, _ := model.CacheGetUserGroup(userId)
		c.Set(ctxkey.Group, userGroup)
		var requestModel string
		var channel *model.Channel
		var err error

		channelId, ok := c.Get(ctxkey.SpecificChannelId)
		if ok {
			// Admin specified channel - use it directly
			id, err := strconv.Atoi(channelId.(string))
			if err != nil {
				abortWithMessage(c, http.StatusBadRequest, "无效的渠道 Id")
				return
			}
			channel, err = model.GetChannelById(id, true)
			if err != nil {
				abortWithMessage(c, http.StatusBadRequest, "无效的渠道 Id")
				return
			}
			if channel.Status != model.ChannelStatusEnabled {
				abortWithMessage(c, http.StatusForbidden, "该渠道已被禁用")
				return
			}
		} else {
			requestModel = c.GetString(ctxkey.RequestModel)

			// Check if token has bound channel ids
			tokenChannelIds, hasTokenChannels := c.Get(ctxkey.TokenChannelIds)
			if hasTokenChannels && len(tokenChannelIds.([]int)) > 0 {
				channel, err = getRandomChannelFromList(userGroup, requestModel, tokenChannelIds.([]int))
				if err != nil {
					// Fallback to automatic selection if all token-bound channels are unavailable
					logger.Warnf(ctx, "令牌指定的渠道都不可用，尝试回退到自动选择")
					channel, err = model.CacheGetRandomSatisfiedChannel(userGroup, requestModel, false)
					if err != nil {
						message := fmt.Sprintf("当前分组 %s 下对于模型 %s 无可用渠道", userGroup, requestModel)
						abortWithMessage(c, http.StatusServiceUnavailable, message)
						return
					}
				}
			} else {
				// No token-bound channels, use automatic selection
				channel, err = model.CacheGetRandomSatisfiedChannel(userGroup, requestModel, false)
				if err != nil {
					message := fmt.Sprintf("当前分组 %s 下对于模型 %s 无可用渠道", userGroup, requestModel)
					if channel != nil {
						logger.SysError(fmt.Sprintf("渠道不存在：%d", channel.Id))
						message = "数据库一致性已被破坏，请联系管理员"
					}
					abortWithMessage(c, http.StatusServiceUnavailable, message)
					return
				}
			}
		}
		logger.Debugf(ctx, "user id %d, user group: %s, request model: %s, using channel #%d", userId, userGroup, requestModel, channel.Id)
		SetupContextForSelectedChannel(c, channel, requestModel)
		c.Next()
	}
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) {
	c.Set(ctxkey.Channel, channel.Type)
	c.Set(ctxkey.ChannelId, channel.Id)
	c.Set(ctxkey.ChannelName, channel.Name)
	if channel.SystemPrompt != nil && *channel.SystemPrompt != "" {
		c.Set(ctxkey.SystemPrompt, *channel.SystemPrompt)
	}
	c.Set(ctxkey.ModelMapping, channel.GetModelMapping())
	c.Set(ctxkey.OriginalModel, modelName) // for retry
	c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", channel.Key))
	c.Set(ctxkey.BaseURL, channel.GetBaseURL())
	cfg, _ := channel.LoadConfig()
	// this is for backward compatibility
	if channel.Other != nil {
		switch channel.Type {
		case channeltype.Azure:
			if cfg.APIVersion == "" {
				cfg.APIVersion = *channel.Other
			}
		case channeltype.Xunfei:
			if cfg.APIVersion == "" {
				cfg.APIVersion = *channel.Other
			}
		case channeltype.Gemini:
			if cfg.APIVersion == "" {
				cfg.APIVersion = *channel.Other
			}
		case channeltype.AIProxyLibrary:
			if cfg.LibraryID == "" {
				cfg.LibraryID = *channel.Other
			}
		case channeltype.Ali:
			if cfg.Plugin == "" {
				cfg.Plugin = *channel.Other
			}
		}
	}
	c.Set(ctxkey.Config, cfg)
}

// getRandomChannelFromList selects a random channel from the given channel ids list
// that is enabled and satisfies the group and model requirements
func getRandomChannelFromList(group string, modelName string, channelIds []int) (*model.Channel, error) {
	if len(channelIds) == 0 {
		return nil, errors.New("渠道列表为空")
	}

	// Try each channel id in order
	for _, id := range channelIds {
		channel, err := model.GetChannelById(id, false)
		if err != nil {
			// Channel doesn't exist, skip
			continue
		}
		if channel.Status != model.ChannelStatusEnabled {
			// Channel is disabled, skip
			continue
		}

		// Check if channel satisfies group and model requirements
		if isChannelSatisfyForGroupAndModel(channel, group, modelName) {
			return channel, nil
		}
	}

	return nil, errors.New("无可用渠道")
}

// isChannelSatisfyForGroupAndModel checks if a channel is available for the given group and model
func isChannelSatisfyForGroupAndModel(channel *model.Channel, group string, modelName string) bool {
	// Check group
	groups := strings.Split(channel.Group, ",")
	inGroup := false
	for _, g := range groups {
		if strings.TrimSpace(g) == group {
			inGroup = true
			break
		}
	}
	if !inGroup {
		return false
	}

	// Check model if specified
	if modelName != "" {
		models := strings.Split(channel.Models, ",")
		for _, m := range models {
			if strings.TrimSpace(m) == modelName {
				return true
			}
		}
		return false
	}

	return true
}
