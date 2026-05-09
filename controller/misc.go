package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/concurrency"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/ctxkey"
	"github.com/songquanpeng/one-api/common/i18n"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/common/message"
	"github.com/songquanpeng/one-api/model"

	"github.com/gin-gonic/gin"
)

func GetStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"version":                     common.Version,
			"start_time":                  common.StartTime,
			"email_verification":          config.EmailVerificationEnabled,
			"github_oauth":                config.GitHubOAuthEnabled,
			"github_client_id":            config.GitHubClientId,
			"lark_client_id":              config.LarkClientId,
			"system_name":                 config.SystemName,
			"logo":                        config.Logo,
			"footer_html":                 config.Footer,
			"wechat_qrcode":               config.WeChatAccountQRCodeImageURL,
			"wechat_login":                config.WeChatAuthEnabled,
			"server_address":              config.ServerAddress,
			"turnstile_check":             config.TurnstileCheckEnabled,
			"turnstile_site_key":          config.TurnstileSiteKey,
			"top_up_link":                 config.TopUpLink,
			"chat_link":                   config.ChatLink,
			"quota_per_unit":              config.QuotaPerUnit,
			"display_in_currency":         config.DisplayInCurrencyEnabled,
			"oidc":                        config.OidcEnabled,
			"oidc_client_id":              config.OidcClientId,
			"oidc_well_known":             config.OidcWellKnown,
			"oidc_authorization_endpoint": config.OidcAuthorizationEndpoint,
			"oidc_token_endpoint":         config.OidcTokenEndpoint,
			"oidc_userinfo_endpoint":      config.OidcUserinfoEndpoint,
		},
	})
	return
}

func GetNotice(c *gin.Context) {
	config.OptionMapRWMutex.RLock()
	defer config.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    config.OptionMap["Notice"],
	})
	return
}

func GetAbout(c *gin.Context) {
	config.OptionMapRWMutex.RLock()
	defer config.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    config.OptionMap["About"],
	})
	return
}

func GetHomePageContent(c *gin.Context) {
	config.OptionMapRWMutex.RLock()
	defer config.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    config.OptionMap["HomePageContent"],
	})
	return
}

func SendEmailVerification(c *gin.Context) {
	email := c.Query("email")
	if err := common.Validate.Var(email, "required,email"); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": i18n.Translate(c, "invalid_parameter"),
		})
		return
	}
	if config.EmailDomainRestrictionEnabled {
		allowed := false
		for _, domain := range config.EmailDomainWhitelist {
			if strings.HasSuffix(email, "@"+domain) {
				allowed = true
				break
			}
		}
		if !allowed {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员启用了邮箱域名白名单，您的邮箱地址的域名不在白名单中",
			})
			return
		}
	}
	if model.IsEmailAlreadyTaken(email) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "邮箱地址已被占用",
		})
		return
	}
	code := common.GenerateVerificationCode(6)
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
	subject := fmt.Sprintf("%s 邮箱验证邮件", config.SystemName)
	content := message.EmailTemplate(
		subject,
		fmt.Sprintf(`
			<p>您好！</p>
			<p>您正在进行 %s 邮箱验证。</p>
			<p>您的验证码为：</p>
			<p style="font-size: 24px; font-weight: bold; color: #333; background-color: #f8f8f8; padding: 10px; text-align: center; border-radius: 4px;">%s</p>
			<p style="color: #666;">验证码 %d 分钟内有效，如果不是本人操作，请忽略。</p>
		`, config.SystemName, code, common.VerificationValidMinutes),
	)
	err := message.SendEmail(subject, email, content)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func SendPasswordResetEmail(c *gin.Context) {
	email := c.Query("email")
	if err := common.Validate.Var(email, "required,email"); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": i18n.Translate(c, "invalid_parameter"),
		})
		return
	}
	if !model.IsEmailAlreadyTaken(email) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该邮箱地址未注册",
		})
		return
	}
	code := common.GenerateVerificationCode(0)
	common.RegisterVerificationCodeWithKey(email, code, common.PasswordResetPurpose)
	link := fmt.Sprintf("%s/user/reset?email=%s&token=%s", config.ServerAddress, email, code)
	subject := fmt.Sprintf("%s 密码重置", config.SystemName)
	content := message.EmailTemplate(
		subject,
		fmt.Sprintf(`
			<p>您好！</p>
			<p>您正在进行 %s 密码重置。</p>
			<p>请点击下面的按钮进行密码重置：</p>
			<p style="text-align: center; margin: 30px 0;">
				<a href="%s" style="background-color: #007bff; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px; display: inline-block;">重置密码</a>
			</p>
			<p style="color: #666;">如果按钮无法点击，请复制以下链接到浏览器中打开：</p>
			<p style="background-color: #f8f8f8; padding: 10px; border-radius: 4px; word-break: break-all;">%s</p>
			<p style="color: #666;">重置链接 %d 分钟内有效，如果不是本人操作，请忽略。</p>
		`, config.SystemName, link, link, common.VerificationValidMinutes),
	)
	err := message.SendEmail(subject, email, content)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("%s%s", i18n.Translate(c, "send_email_failed"), err.Error()),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

type PasswordResetRequest struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

func ResetPassword(c *gin.Context) {
	var req PasswordResetRequest
	err := json.NewDecoder(c.Request.Body).Decode(&req)
	if req.Email == "" || req.Token == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": i18n.Translate(c, "invalid_parameter"),
		})
		return
	}
	if !common.VerifyCodeWithKey(req.Email, req.Token, common.PasswordResetPurpose) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "重置链接非法或已过期",
		})
		return
	}
	password := common.GenerateVerificationCode(12)
	err = model.ResetUserPasswordByEmail(req.Email, password)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	common.DeleteKey(req.Email, common.PasswordResetPurpose)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    password,
	})
	return
}

// GetAutoConcurrencyLimit 获取当前用户自动并发限制信息
func GetAutoConcurrencyLimit(c *gin.Context) {
	evaluator := concurrency.GetLoadEvaluator()
	if evaluator == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "并发限制功能未启用",
		})
		return
	}

	// 获取当前用户ID
	userId := c.GetInt(ctxkey.Id)
	if userId == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "未登录",
		})
		return
	}

	// 获取配置模式
	configMode := config.OptionMap["ConcurrencyConfigMode"]
	if configMode == "" {
		configMode = "global"
	}

	// 根据配置模式决定配置来源
	baseLimit := concurrency.GetCurrentConfigInt("UserBaseConcurrentLimit", config.UserBaseConcurrentLimit)
	manualEnabled := config.OptionMap["EnableManualConcurrencyLimit"] == "true"
	autoEnabled := config.OptionMap["EnableAutoConcurrencyLimit"] == "true"
	hasPersonalConfig := false
	// 新增的个人配置字段（默认使用全局值）
	waitTimeout := concurrency.GetCurrentConfigInt("ConcurrencyWaitTimeout", config.ConcurrencyWaitTimeout)
	checkInterval := concurrency.GetCurrentConfigInt("ConcurrencyCheckInterval", config.ConcurrencyCheckInterval)
	cacheTTL := concurrency.GetCurrentConfigInt("RequestCacheTTL", config.RequestCacheTTL)
	dedupEnabled := concurrency.GetCurrentConfigBool("EnableRequestDeduplication", config.EnableRequestDeduplication)

	if configMode == "personal" {
		// 个人配置模式：优先使用用户个人配置
		// 先检查是否有已存在的个人配置
		existingConfig, _ := model.GetUserConcurrencyConfig(userId)
		hasPersonalConfig = existingConfig != nil

		// 只有存在个人配置时才使用，否则使用全局配置
		if existingConfig != nil {
			baseLimit = existingConfig.MaxConcurrent
			manualEnabled = existingConfig.Status == model.UserConcurrencyStatusNormal
			autoEnabled = existingConfig.EnableAutoLimit
			// 从个人配置读取新字段
			waitTimeout = existingConfig.WaitTimeout
			checkInterval = existingConfig.CheckInterval
			cacheTTL = existingConfig.CacheTTL
			dedupEnabled = existingConfig.EnableRequestDedup
		}
		// 如果没有个人配置，hasPersonalConfig 为 false，前端会显示提示
	}
	// configMode == "global" 时，配置直接使用全局值（从 OptionMap 读取）

	// 获取用户当前并发数
	limiter := concurrency.GetLimiter()
	currentConcurrent := 0
	if limiter != nil {
		currentConcurrent = limiter.GetCurrentConcurrent(userId)
	}

	// 刷新系统指标
	evaluator.RefreshMetrics()
	metrics := evaluator.GetMetrics()

	// 获取平均响应时长
	// 全局模式：使用所有用户请求时长Top10平均值
	// 个人模式：有个人配置则使用用户最近一次请求时长，否则与全局模式一致
	var avgDuration int64
	if configMode == "global" || !hasPersonalConfig {
		// 全局模式或无个人配置：使用Top10平均值
		avgDuration, _ = model.GetTop10AvgElapsedTime()
		if avgDuration == 0 {
			avgDuration = 1000
		}
	} else {
		// 个人模式且有个人配置：使用用户最近一次请求时长
		avgDuration = evaluator.GetUserAvgElapsedTime(userId)
	}

	// 计算用户级别的动态限制
	dynamicLimit := evaluator.CalculateUserDynamicLimit(userId, baseLimit, currentConcurrent)
	loadLevel := evaluator.GetLoadLevel()

	// 调试信息
	logger.Infof(c.Request.Context(), "[GetAutoConcurrencyLimit] mode=%s, userId=%d, baseLimit=%d, currentConcurrent=%d, dynamicLimit=%d",
		configMode, userId, baseLimit, currentConcurrent, dynamicLimit)

	// 计算各因子值
	durationFactor := concurrency.CalculateDurationFactor(avgDuration)
	concurrentFactor := concurrency.CalculateConcurrentFactor(currentConcurrent, baseLimit)
	trendFactor := evaluator.CalculateUserTrendFactor(userId)
	gpuFactor := concurrency.CalculateGPUFactor(metrics.GPUKVCacheUsage)

	// 从 OptionMap 获取最新的配置值
	gpuEnabled := config.OptionMap["EnableGPUMonitoring"] == "true"

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"config_mode":         configMode,
			"target_user_id":      userId,
			"has_personal_config": hasPersonalConfig,
			"dynamic_limit":       dynamicLimit,
			"manual_limit":        baseLimit,
			"actual_limit":        minDynamicLimit(dynamicLimit, baseLimit),
			"load_level":          loadLevel,
			"current_concurrent":  currentConcurrent,
			"wait_timeout":        waitTimeout,
			"check_interval":      checkInterval,
			"cache_ttl":           cacheTTL,
			"enable_request_dedup": dedupEnabled,
			"factors": gin.H{
				"duration":   durationFactor,
				"concurrent": concurrentFactor,
				"trend":      trendFactor,
				"gpu":        gpuFactor,
			},
			"weights": gin.H{
				"duration":   concurrency.GetCurrentConfigInt("DurationFactorWeight", config.DurationFactorWeight),
				"concurrent": concurrency.GetCurrentConfigInt("ConcurrentFactorWeight", config.ConcurrentFactorWeight),
				"trend":      concurrency.GetCurrentConfigInt("TrendFactorWeight", config.TrendFactorWeight),
				"gpu":        concurrency.GetCurrentConfigInt("GPUFactorWeight", config.GPUFactorWeight),
			},
			"metrics": gin.H{
				"avg_duration":     avgDuration,
				"gpu_usage":        metrics.GPUKVCacheUsage,
				"success_rate":     metrics.SuccessRate,
				"running_requests": metrics.RunningRequests,
			},
			"enabled": gin.H{
				"manual": manualEnabled,
				"auto":   autoEnabled,
				"dedup":  dedupEnabled,
				"gpu":    gpuEnabled,
			},
		},
	})
	return
}

func minDynamicLimit(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// UpdateUserConcurrencyConfigRequest 更新用户并发配置请求
// GetConcurrencyDebug 获取所有用户的并发计数（调试用）
func GetConcurrencyDebug(c *gin.Context) {
	limiter := concurrency.GetLimiter()
	if limiter == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "并发限制器未初始化",
		})
		return
	}

	stats, _ := limiter.GetAllStats()
	total := 0
	for _, count := range stats {
		total += count
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"user_counts":    stats,
			"total_count":    total,
			"global_enabled": concurrency.IsGlobalEnabled(),
		},
	})
}

// GetConcurrencyConfigMode 获取当前配置模式 (personal/global)
func GetConcurrencyConfigMode(c *gin.Context) {
	mode := config.OptionMap["ConcurrencyConfigMode"]
	if mode == "" {
		mode = "global" // 默认全局
	}
	userId := c.GetInt(ctxkey.Id)
	role := c.GetInt(ctxkey.Role)
	isAdmin := role >= model.RoleAdminUser

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"mode":            mode,
			"current_user_id": userId,
			"is_admin":        isAdmin,
		},
	})
}

// SetConcurrencyConfigMode 设置配置模式
func SetConcurrencyConfigMode(c *gin.Context) {
	var req struct {
		Mode         string `json:"mode"`           // "personal" or "global"
		TargetUserId int    `json:"target_user_id"` // 管理员指定的用户ID，0表示当前用户
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if req.Mode != "personal" && req.Mode != "global" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的模式"})
		return
	}
	if err := model.UpdateOption("ConcurrencyConfigMode", req.Mode); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "配置模式已更新",
		"data": gin.H{
			"target_user_id": req.TargetUserId,
		},
	})
}

// GetAutoConcurrencyLimitForUser 获取指定用户的自动并发限制信息 (管理员)
func GetAutoConcurrencyLimitForUser(c *gin.Context) {
	userIdStr := c.Param("userId")
	targetUserId, err := strconv.Atoi(userIdStr)
	if err != nil || targetUserId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的用户ID"})
		return
	}

	evaluator := concurrency.GetLoadEvaluator()
	if evaluator == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "并发限制功能未启用",
		})
		return
	}

	// 检查目标用户是否存在
	_, err = model.GetUserById(targetUserId, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户不存在",
		})
		return
	}

	// 检查用户是否有个人配置（只用 GetUserConcurrencyConfig，不自动创建）
	existingConfig, _ := model.GetUserConcurrencyConfig(targetUserId)
	hasPersonalConfig := existingConfig != nil

	// 获取目标用户的并发配置（优先个人配置，否则使用全局配置）
	baseLimit := concurrency.GetCurrentConfigInt("UserBaseConcurrentLimit", config.UserBaseConcurrentLimit)
	manualEnabled := config.OptionMap["EnableManualConcurrencyLimit"] == "true"
	autoEnabled := config.OptionMap["EnableAutoConcurrencyLimit"] == "true"
	waitTimeout := concurrency.GetCurrentConfigInt("ConcurrencyWaitTimeout", config.ConcurrencyWaitTimeout)
	checkInterval := concurrency.GetCurrentConfigInt("ConcurrencyCheckInterval", config.ConcurrencyCheckInterval)
	cacheTTL := concurrency.GetCurrentConfigInt("RequestCacheTTL", config.RequestCacheTTL)
	dedupEnabled := concurrency.GetCurrentConfigBool("EnableRequestDeduplication", config.EnableRequestDeduplication)

	if existingConfig != nil {
		baseLimit = existingConfig.MaxConcurrent
		manualEnabled = existingConfig.Status == model.UserConcurrencyStatusNormal
		autoEnabled = existingConfig.EnableAutoLimit
		waitTimeout = existingConfig.WaitTimeout
		checkInterval = existingConfig.CheckInterval
		cacheTTL = existingConfig.CacheTTL
		dedupEnabled = existingConfig.EnableRequestDedup
	}

	// 获取目标用户当前并发数
	limiter := concurrency.GetLimiter()
	currentConcurrent := 0
	if limiter != nil {
		currentConcurrent = limiter.GetCurrentConcurrent(targetUserId)
	}

	// 刷新系统指标
	evaluator.RefreshMetrics()
	metrics := evaluator.GetMetrics()

	// 获取平均响应时长
	// 有个人配置则使用用户最近一次请求时长，否则使用Top10平均值
	var avgDuration int64
	if hasPersonalConfig {
		// 有个人配置：使用用户最近一次请求时长
		avgDuration = evaluator.GetUserAvgElapsedTime(targetUserId)
	} else {
		// 无个人配置：使用Top10平均值
		avgDuration, _ = model.GetTop10AvgElapsedTime()
		if avgDuration == 0 {
			avgDuration = 1000
		}
	}

	// 计算用户级别的动态限制
	dynamicLimit := evaluator.CalculateUserDynamicLimit(targetUserId, baseLimit, currentConcurrent)
	loadLevel := evaluator.GetLoadLevel()

	// 从 OptionMap 获取最新的配置值
	gpuEnabled := config.OptionMap["EnableGPUMonitoring"] == "true"

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"target_user_id":      targetUserId,
			"config_mode":         "personal",
			"has_personal_config": hasPersonalConfig,
			"dynamic_limit":       dynamicLimit,
			"manual_limit":        baseLimit,
			"actual_limit":        minDynamicLimit(dynamicLimit, baseLimit),
			"load_level":          loadLevel,
			"current_concurrent":  currentConcurrent,
			"wait_timeout":        waitTimeout,
			"check_interval":      checkInterval,
			"cache_ttl":           cacheTTL,
			"enable_request_dedup": dedupEnabled,
			"factors": gin.H{
				"duration":   concurrency.CalculateDurationFactor(avgDuration),
				"concurrent": concurrency.CalculateConcurrentFactor(currentConcurrent, baseLimit),
				"trend":      evaluator.CalculateUserTrendFactor(targetUserId),
				"gpu":        concurrency.CalculateGPUFactor(metrics.GPUKVCacheUsage),
			},
			"weights": gin.H{
				"duration":   concurrency.GetCurrentConfigInt("DurationFactorWeight", config.DurationFactorWeight),
				"concurrent": concurrency.GetCurrentConfigInt("ConcurrentFactorWeight", config.ConcurrentFactorWeight),
				"trend":      concurrency.GetCurrentConfigInt("TrendFactorWeight", config.TrendFactorWeight),
				"gpu":        concurrency.GetCurrentConfigInt("GPUFactorWeight", config.GPUFactorWeight),
			},
			"metrics": gin.H{
				"avg_duration":     avgDuration,
				"gpu_usage":        metrics.GPUKVCacheUsage,
				"success_rate":     metrics.SuccessRate,
				"running_requests": metrics.RunningRequests,
			},
			"enabled": gin.H{
				"manual": manualEnabled,
				"auto":   autoEnabled,
				"dedup":  dedupEnabled,
				"gpu":    gpuEnabled,
			},
		},
	})
}

// ResetConcurrencyConfig 重置并发控制配置为默认值
func ResetConcurrencyConfig(c *gin.Context) {
	// 从配置文件默认值写入数据库
	defaults := map[string]string{
		"EnableManualConcurrencyLimit": "true",
		"EnableAutoConcurrencyLimit":   "true",
		"UserBaseConcurrentLimit":      "5",
		"ConcurrencyWaitTimeout":       "30",
		"ConcurrencyCheckInterval":     "100",
		"DurationFactorWeight":         "25",
		"ConcurrentFactorWeight":       "25",
		"TrendFactorWeight":            "20",
		"GPUFactorWeight":              "30",
		"EnableGPUMonitoring":          "false",
		"VLLMAPIURL":                   "http://localhost:8000",
		"GPUKVCacheWarn":               "85",
		"GPUKVCacheMax":                "95",
		"EnableRequestDeduplication":   "true",
		"RequestCacheTTL":              "30",
	}

	// 批量更新配置
	for key, value := range defaults {
		if err := model.UpdateOption(key, value); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("重置配置失败: %s", err.Error()),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "并发控制配置已恢复默认值",
	})
}
