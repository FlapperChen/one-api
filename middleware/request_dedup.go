package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/ctxkey"
	"github.com/songquanpeng/one-api/common/logger"
	dbmodel "github.com/songquanpeng/one-api/model"
)

// RequestDeduplication 请求去重中间件
// 识别重复请求，如果是客户端重试则直接返回缓存响应
func RequestDeduplication() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 检查是否启用去重
		if !config.EnableRequestDeduplication {
			c.Next()
			return
		}

		userId := c.GetInt(ctxkey.Id)
		if userId == 0 {
			c.Next()
			return
		}

		// 读取请求体
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Next()
			return
		}
		// 恢复 body
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// 解析请求体
		var reqBody map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
			c.Next()
			return
		}

		// 计算请求指纹
		fingerprint := dbmodel.CalculateFingerprint(reqBody)
		fullHash := dbmodel.CalculateFullHash(reqBody)

		// 检查是否有已完成的相同请求
		cached, err := dbmodel.GetRequestCache(userId, fingerprint)
		if err == nil && cached != nil && cached.Status == dbmodel.RequestCacheStatusCompleted {
			// 发现重复请求，直接返回缓存响应
			logger.Debugf(c.Request.Context(), "请求去重命中，用户 %d，指纹 %s，重复计数 %d",
				userId, fingerprint[:8], cached.DuplicateCount+1)

			// 增加重复计数
			dbmodel.IncrementDuplicateCount(cached.Id)

			// 返回缓存响应
			c.Header("X-Request-Dedup", "hit")
			c.Header("X-Original-Request-Id", fingerprint[:8])
			c.Data(http.StatusOK, "application/json", []byte(cached.ResponseBody))
			return
		}

		// 检查是否有正在处理的相同请求
		processing, err := dbmodel.GetProcessingRequest(userId, fingerprint)
		if err == nil && processing != nil {
			// 发现正在处理的请求，加入等待
			logger.Debugf(c.Request.Context(), "请求等待，用户 %d，指纹 %s，等待处理中",
				userId, fingerprint[:8])

			// 增加重复计数
			dbmodel.IncrementDuplicateCount(processing.Id)

			// 设置等待标记
			c.Set(ctxkey.RequestWaitingDuplicate, true)
			c.Set(ctxkey.RequestDuplicateOf, processing.Id)
			c.Set(ctxkey.RequestFingerprint, fingerprint)
		} else {
			// 创建新的请求缓存
			newCache := &dbmodel.RequestCache{
				UserId:      userId,
				Fingerprint: fingerprint,
				RequestHash: fullHash,
				Model:       getModelFromRequest(reqBody),
				RequestType: getRequestTypeFromPath(c.Request.URL.Path),
				Status:      dbmodel.RequestCacheStatusProcessing,
				RequestBody: string(bodyBytes),
				TTL:         config.RequestCacheTTL,
			}

			if err := dbmodel.CreateRequestCache(newCache); err != nil {
				logger.Warnf(c.Request.Context(), "创建请求缓存失败: %v", err)
			} else {
				c.Set(ctxkey.RequestCacheId, newCache.Id)
				c.Set(ctxkey.RequestFingerprint, fingerprint)
			}
		}

		c.Next()
	}
}

// CompleteRequestCache 完成请求缓存
func CompleteRequestCache(c *gin.Context, responseBody string) {
	if cacheId, exists := c.Get(ctxkey.RequestCacheId); exists {
		if id, ok := cacheId.(int64); ok {
			if err := dbmodel.CompleteRequestCache(id, responseBody); err != nil {
				logger.Warnf(c.Request.Context(), "更新请求缓存失败: %v", err)
			}
		}
	}
}

// FailRequestCache 标记请求缓存失败
func FailRequestCache(c *gin.Context, errorMsg string) {
	if cacheId, exists := c.Get(ctxkey.RequestCacheId); exists {
		if id, ok := cacheId.(int64); ok {
			if err := dbmodel.FailRequestCache(id, errorMsg); err != nil {
				logger.Warnf(c.Request.Context(), "更新请求缓存失败: %v", err)
			}
		}
	}
}

// getModelFromRequest 从请求体提取模型名称
func getModelFromRequest(reqBody map[string]interface{}) string {
	if model, ok := reqBody["model"].(string); ok {
		return model
	}
	return ""
}

// getRequestTypeFromPath 从URL路径判断请求类型
func getRequestTypeFromPath(path string) string {
	if strings.Contains(path, "chat/completions") {
		return "chat"
	}
	if strings.Contains(path, "completions") {
		return "completion"
	}
	if strings.Contains(path, "embeddings") {
		return "embedding"
	}
	return "unknown"
}