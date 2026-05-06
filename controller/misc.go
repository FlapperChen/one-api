package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/concurrency"
	"github.com/songquanpeng/one-api/common/i18n"
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

// GetAutoConcurrencyLimit 获取当前自动并发限制信息
func GetAutoConcurrencyLimit(c *gin.Context) {
	evaluator := concurrency.GetLoadEvaluator()
	if evaluator == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "并发限制功能未启用",
		})
		return
	}

	// 刷新指标（获取最新的 GPU 数据）
	evaluator.RefreshMetrics()
	metrics := evaluator.GetMetrics()
	dynamicLimit := evaluator.CalculateDynamicLimit(config.UserBaseConcurrentLimit)
	loadLevel := evaluator.GetLoadLevel()

	// 计算各因子值
	durationFactor := concurrency.CalculateDurationFactor(metrics.AvgRequestDuration)
	concurrentFactor := concurrency.CalculateConcurrentFactor(metrics.CurrentConcurrent, config.UserBaseConcurrentLimit)
	trendFactor := evaluator.CalculateTrendFactor()
	gpuFactor := concurrency.CalculateGPUFactor(metrics.GPUKVCacheUsage)

	// 从 OptionMap 获取最新的配置值
	gpuEnabled := config.OptionMap["EnableGPUMonitoring"] == "true"
	dedupEnabled := config.OptionMap["EnableRequestDeduplication"] == "true"
	manualEnabled := config.OptionMap["EnableManualConcurrencyLimit"] == "true"
	autoEnabled := config.OptionMap["EnableAutoConcurrencyLimit"] == "true"

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"dynamic_limit":      dynamicLimit,
			"manual_limit":       config.UserBaseConcurrentLimit,
			"actual_limit":       minDynamicLimit(dynamicLimit, config.UserBaseConcurrentLimit),
			"load_level":         loadLevel,
			"factors": gin.H{
				"duration":   durationFactor,
				"concurrent": concurrentFactor,
				"trend":      trendFactor,
				"gpu":        gpuFactor,
			},
			"weights": gin.H{
				"duration":   config.DurationFactorWeight,
				"concurrent": config.ConcurrentFactorWeight,
				"trend":      config.TrendFactorWeight,
				"gpu":        config.GPUFactorWeight,
			},
			"metrics": gin.H{
				"avg_duration":      metrics.AvgRequestDuration,
				"current_concurrent": metrics.CurrentConcurrent,
				"gpu_usage":         metrics.GPUKVCacheUsage,
				"success_rate":      metrics.SuccessRate,
				"running_requests":  metrics.RunningRequests,
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
