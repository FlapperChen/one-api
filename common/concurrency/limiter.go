package concurrency

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/model"
)

var (
	ErrWaitTimeout       = errors.New("请求排队超时，请稍后再试")
	ErrConcurrencyLimit  = errors.New("并发请求数超限")
)

// 内存并发计数器
type inMemoryCounter struct {
	mu     sync.RWMutex
	counts map[int]int // userId -> current concurrent count
}

var counter = &inMemoryCounter{
	counts: make(map[int]int),
}

// Incr 增加用户并发计数
func (c *inMemoryCounter) Incr(userId int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[userId]++
	return c.counts[userId]
}

// Decr 减少用户并发计数
func (c *inMemoryCounter) Decr(userId int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[userId]--
	if c.counts[userId] <= 0 {
		delete(c.counts, userId)
	}
}

// Get 获取用户当前并发数
func (c *inMemoryCounter) Get(userId int) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.counts[userId]
}

// GetAll 获取所有用户并发数
func (c *inMemoryCounter) GetAll() map[int]int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[int]int, len(c.counts))
	for k, v := range c.counts {
		result[k] = v
	}
	return result
}

// 并发限制器
type UserConcurrencyLimiter struct {
	waitTimeout   time.Duration
	checkInterval time.Duration
	loadEvaluator *LoadEvaluator
	fallbackLimit int
}

var (
	limiter     *UserConcurrencyLimiter
	limiterOnce sync.Once
)

// 初始化并发限制器
func InitLimiter() *UserConcurrencyLimiter {
	limiterOnce.Do(func() {
		limiter = &UserConcurrencyLimiter{
			waitTimeout:   time.Duration(getConfigInt("ConcurrencyWaitTimeout", config.ConcurrencyWaitTimeout)) * time.Second,
			checkInterval: time.Duration(getConfigInt("ConcurrencyCheckInterval", config.ConcurrencyCheckInterval)) * time.Millisecond,
			loadEvaluator: GetLoadEvaluator(),
			fallbackLimit: getConfigInt("UserBaseConcurrentLimit", config.UserBaseConcurrentLimit),
		}
	})
	return limiter
}

// 获取并发限制器单例
func GetLimiter() *UserConcurrencyLimiter {
	return limiter
}

// isGlobalConcurrencyEnabled 检查全局并发限制是否启用
func isGlobalConcurrencyEnabled() bool {
	if val, ok := config.OptionMap["EnableManualConcurrencyLimit"]; ok {
		return val == "true"
	}
	return config.EnableConcurrencyLimit
}

// 获取用户当前并发数
func (l *UserConcurrencyLimiter) GetCurrentConcurrent(userId int) int {
	return counter.Get(userId)
}

// 获取所有用户的并发统计
func (l *UserConcurrencyLimiter) GetAllStats() (map[int]int, error) {
	return counter.GetAll(), nil
}

// 获取用户的并发限制
func (l *UserConcurrencyLimiter) getUserLimit(userId int) int {
	userConfig, err := model.GetOrCreateUserConcurrencyConfig(userId)
	if err != nil || userConfig == nil {
		return getConfigInt("UserBaseConcurrentLimit", config.UserBaseConcurrentLimit)
	}
	return userConfig.MaxConcurrent
}

// 检查用户是否启用了自动限制
func (l *UserConcurrencyLimiter) isAutoLimitEnabled(userId int) bool {
	config, err := model.GetUserConcurrencyConfig(userId)
	if err != nil || config == nil {
		return true
	}
	return config.EnableAutoLimit
}

// 计算动态限制
func (l *UserConcurrencyLimiter) calculateLimit(userId int) int {
	l.loadEvaluator.RefreshMetrics()
	baseLimit := l.getUserLimit(userId)
	if !l.isAutoLimitEnabled(userId) {
		return baseLimit
	}
	return l.loadEvaluator.CalculateDynamicLimit(baseLimit)
}

// 获取限制（用于日志和调试）
func (l *UserConcurrencyLimiter) GetLimit(userId int) int {
	return l.calculateLimit(userId)
}

// 获取用户配置
func (l *UserConcurrencyLimiter) GetUserConfig(userId int) map[string]interface{} {
	config, err := model.GetUserConcurrencyConfig(userId)
	if err != nil || config == nil {
		return map[string]interface{}{
			"user_id":           userId,
			"max_concurrent":    5,
			"enable_auto_limit": true,
			"status":            1,
			"current":           counter.Get(userId),
			"dynamic_limit":     l.calculateLimit(userId),
		}
	}
	return map[string]interface{}{
		"user_id":           config.UserId,
		"max_concurrent":    config.MaxConcurrent,
		"enable_auto_limit": config.EnableAutoLimit,
		"status":            config.Status,
		"current":           counter.Get(userId),
		"dynamic_limit":     l.calculateLimit(userId),
	}
}

// Acquire 获取并发许可
func (l *UserConcurrencyLimiter) Acquire(ctx context.Context, userId int) error {
	logger.Infof(nil, "Acquire: 用户 %d 开始获取许可", userId)

	if !isGlobalConcurrencyEnabled() {
		logger.Warnf(nil, "Acquire: 全局并发未启用，跳过")
		return nil
	}

	if !model.IsUserConcurrencyEnabled(userId) {
		logger.Warnf(nil, "Acquire: 用户 %d 被禁用", userId)
		return nil
	}

	logger.Infof(nil, "Acquire: 用户 %d 开始获取许可, 当前计数=%d", userId, counter.Get(userId))

	limit := l.calculateLimit(userId)
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			logger.Warnf(ctx, "用户 %d 并发请求等待超时", userId)
			return ErrWaitTimeout
		default:
			current := counter.Get(userId)
			if current < limit {
				counter.Incr(userId)
				// 更新用户滑动窗口
				UpdateWindowOnAcquire(userId, counter.Get(userId))
				waitTime := time.Since(startTime)
				if waitTime > time.Second {
					logger.Debugf(ctx, "用户 %d 等待 %v 后获得并发许可 (current=%d, limit=%d)",
						userId, waitTime, current+1, limit)
				}
				return nil
			}
			time.Sleep(l.checkInterval)
		}
	}
}

// Release 释放并发许可
func (l *UserConcurrencyLimiter) Release(ctx context.Context, userId int) {
	if !isGlobalConcurrencyEnabled() {
		return
	}
	logger.Infof(nil, "Release: 用户 %d 释放许可前, 当前计数=%d", userId, counter.Get(userId))
	counter.Decr(userId)
	// 更新用户滑动窗口
	UpdateWindowOnRelease(userId, counter.Get(userId))
	logger.Infof(nil, "Release: 用户 %d 释放许可后, 当前计数=%d", userId, counter.Get(userId))
}

// SetConcurrent 强制设置用户并发数（用于调试）
func (l *UserConcurrencyLimiter) SetConcurrent(userId int, count int) error {
	counter.mu.Lock()
	defer counter.mu.Unlock()
	if count <= 0 {
		delete(counter.counts, userId)
	} else {
		counter.counts[userId] = count
	}
	return nil
}

// IsGlobalEnabled 返回全局并发限制是否启用
func IsGlobalEnabled() bool {
	return isGlobalConcurrencyEnabled()
}