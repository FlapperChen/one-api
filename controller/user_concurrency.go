package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common/concurrency"
	"github.com/songquanpeng/one-api/common/ctxkey"
	"github.com/songquanpeng/one-api/model"
)

// GetUserConcurrencyConfig 获取用户并发配置
func GetUserConcurrencyConfig(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	if userId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的用户ID"})
		return
	}

	config, err := model.GetOrCreateUserConcurrencyConfig(userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	// 获取当前并发数
	currentConcurrent := 0
	if limiter := concurrency.GetLimiter(); limiter != nil {
		currentConcurrent = limiter.GetCurrentConcurrent(userId)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"user_id":            config.UserId,
			"max_concurrent":     config.MaxConcurrent,
			"enable_auto_limit":  config.EnableAutoLimit,
			"status":             config.Status,
			"current_concurrent": currentConcurrent,
			"dynamic_limit":      config.MaxConcurrent, // TODO: 计算动态限制
		},
	})
}

// UpdateUserConcurrencyConfig 更新用户并发配置
func UpdateUserConcurrencyConfig(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	if userId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的用户ID"})
		return
	}

	var req struct {
		MaxConcurrent   int  `json:"max_concurrent"`
		EnableAutoLimit bool `json:"enable_auto_limit"`
		Status          int  `json:"status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	config, err := model.GetOrCreateUserConcurrencyConfig(userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	// 更新配置
	if req.MaxConcurrent > 0 {
		config.MaxConcurrent = req.MaxConcurrent
	}
	config.EnableAutoLimit = req.EnableAutoLimit
	if req.Status > 0 {
		config.Status = req.Status
	}

	if err := model.UpdateUserConcurrencyConfig(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "配置已更新",
	})
}

// GetUserCurrentConcurrent 获取用户当前并发数
func GetUserCurrentConcurrent(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	if userId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的用户ID"})
		return
	}

	currentConcurrent := 0
	if limiter := concurrency.GetLimiter(); limiter != nil {
		currentConcurrent = limiter.GetCurrentConcurrent(userId)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"user_id":            userId,
			"current_concurrent": currentConcurrent,
		},
	})
}

// GetAllConcurrencyStats 获取所有用户并发统计 (管理员)
func GetAllConcurrencyStats(c *gin.Context) {
	if limiter := concurrency.GetLimiter(); limiter != nil {
		stats, err := limiter.GetAllStats()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    stats,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{},
	})
}

// GetUserConcurrencyConfigById 根据ID获取用户并发配置 (管理员)
func GetUserConcurrencyConfigById(c *gin.Context) {
	userIdStr := c.Param("id")
	userId, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的用户ID"})
		return
	}

	config, err := model.GetOrCreateUserConcurrencyConfig(userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	currentConcurrent := 0
	if limiter := concurrency.GetLimiter(); limiter != nil {
		currentConcurrent = limiter.GetCurrentConcurrent(userId)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"user_id":            config.UserId,
			"max_concurrent":     config.MaxConcurrent,
			"enable_auto_limit":  config.EnableAutoLimit,
			"status":             config.Status,
			"current_concurrent": currentConcurrent,
		},
	})
}

// UpdateUserConcurrencyConfigById 根据ID更新用户并发配置 (管理员)
func UpdateUserConcurrencyConfigById(c *gin.Context) {
	userIdStr := c.Param("id")
	userId, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的用户ID"})
		return
	}

	var req struct {
		MaxConcurrent   int  `json:"max_concurrent"`
		EnableAutoLimit bool `json:"enable_auto_limit"`
		Status          int  `json:"status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	config, err := model.GetOrCreateUserConcurrencyConfig(userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	if req.MaxConcurrent > 0 {
		config.MaxConcurrent = req.MaxConcurrent
	}
	config.EnableAutoLimit = req.EnableAutoLimit
	if req.Status > 0 {
		config.Status = req.Status
	}

	if err := model.UpdateUserConcurrencyConfig(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "配置已更新",
	})
}

// GetSystemLoadInfo 获取系统负载信息
func GetSystemLoadInfo(c *gin.Context) {
	evaluator := concurrency.GetLoadEvaluator()
	if evaluator == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"enabled": false,
			},
		})
		return
	}

	evaluator.RefreshMetrics()
	metrics := evaluator.GetMetrics()
	loadLevel := evaluator.GetLoadLevel()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":        true,
			"load_level":     loadLevel,
			"response_time":  metrics.ResponseTime,
			"success_rate":   metrics.SuccessRate,
			"request_count":  metrics.RequestCount,
			"error_count":    metrics.ErrorCount,
		},
	})
}

// DeleteUserConcurrencyConfig 删除用户并发配置（使其恢复使用全局配置）
func DeleteUserConcurrencyConfig(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	if userId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的用户ID"})
		return
	}

	result := model.DB.Delete(&model.UserConcurrencyConfig{}, "user_id = ?", userId)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "个人配置已清除，将使用全局配置",
	})
}

// DeleteUserConcurrencyConfigById 删除指定用户的并发配置（管理员）
func DeleteUserConcurrencyConfigById(c *gin.Context) {
	userIdStr := c.Param("id")
	userId, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的用户ID"})
		return
	}

	result := model.DB.Delete(&model.UserConcurrencyConfig{}, "user_id = ?", userId)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("用户 %d 的个人配置已清除，将使用全局配置", userId),
	})
}