package concurrency

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/model"
)

var (
	ErrWaitTimeout        = errors.New("请求排队超时，请稍后再试")
	ErrConcurrencyLimit   = errors.New("并发请求数超限")
	ErrRedisNotAvailable  = errors.New("Redis服务不可用")
)

// 并发限制器
type UserConcurrencyLimiter struct {
	redis         redis.Cmdable
	waitTimeout   time.Duration
	checkInterval time.Duration
	loadEvaluator *LoadEvaluator
	fallbackLimit int // Redis不可用时的降级限制
}

var (
	limiter     *UserConcurrencyLimiter
	limiterOnce sync.Once
)

// 初始化并发限制器
func InitLimiter() *UserConcurrencyLimiter {
	limiterOnce.Do(func() {
		limiter = &UserConcurrencyLimiter{
			redis:         common.RDB,
			waitTimeout:   time.Duration(config.ConcurrencyWaitTimeout) * time.Second,
			checkInterval: time.Duration(config.ConcurrencyCheckInterval) * time.Millisecond,
			loadEvaluator: GetLoadEvaluator(),
			fallbackLimit: config.UserBaseConcurrentLimit,
		}
	})
	return limiter
}

// 获取并发限制器单例
func GetLimiter() *UserConcurrencyLimiter {
	return limiter
}

// 获取Redis key
func getConcurrentKey(userId int) string {
	return fmt.Sprintf("concurrent:%d", userId)
}

// 获取当前并发数
func (l *UserConcurrencyLimiter) GetCurrentConcurrent(userId int) int {
	if l.redis == nil {
		return 0
	}
	key := getConcurrentKey(userId)
	val, err := l.redis.Get(context.Background(), key).Int()
	if err != nil {
		logger.Warnf(nil, "获取用户并发数失败: %v", err)
		return 0
	}
	return val
}

// 尝试增加并发计数
func (l *UserConcurrencyLimiter) tryIncrement(userId int) bool {
	if l.redis == nil {
		return true
	}
	key := getConcurrentKey(userId)
	success, err := l.redis.SetNX(context.Background(), key, 1, 0).Result()
	if err != nil {
		logger.Warnf(nil, "Redis SetNX失败: %v", err)
		return true // Redis失败时放行
	}
	if success {
		return true // 首次设置成功
	}
	// 已存在，增加计数
	_, err = l.redis.Incr(context.Background(), key).Result()
	if err != nil {
		logger.Warnf(nil, "Redis Incr失败: %v", err)
		return true
	}
	return true
}

// 减少并发计数
func (l *UserConcurrencyLimiter) Decrement(userId int) {
	if l.redis == nil {
		return
	}
	key := getConcurrentKey(userId)
	val, err := l.redis.Decr(context.Background(), key).Result()
	if err != nil {
		logger.Warnf(nil, "Redis Decr失败: %v", err)
		return
	}
	// 如果计数小于等于0，删除key
	if val <= 0 {
		l.redis.Del(context.Background(), key)
	}
}

// 获取用户的并发限制
func (l *UserConcurrencyLimiter) getUserLimit(userId int) int {
	userConfig, err := model.GetOrCreateUserConcurrencyConfig(userId)
	if err != nil || userConfig == nil {
		return config.UserBaseConcurrentLimit
	}
	return userConfig.MaxConcurrent
}

// 检查用户是否启用了自动限制
func (l *UserConcurrencyLimiter) isAutoLimitEnabled(userId int) bool {
	config, err := model.GetUserConcurrencyConfig(userId)
	if err != nil || config == nil {
		return true // 默认启用
	}
	return config.EnableAutoLimit
}

// 计算动态限制
func (l *UserConcurrencyLimiter) calculateLimit(userId int) int {
	// 刷新负载指标
	l.loadEvaluator.RefreshMetrics()

	// 获取基础限制
	baseLimit := l.getUserLimit(userId)

	// 如果未启用自动限制，直接返回基础限制
	if !l.isAutoLimitEnabled(userId) {
		return baseLimit
	}

	// 使用负载评估器计算动态限制
	return l.loadEvaluator.CalculateDynamicLimit(baseLimit)
}

// 获取限制（用于日志和调试）
func (l *UserConcurrencyLimiter) GetLimit(userId int) int {
	return l.calculateLimit(userId)
}

// 获取用户配置（用于日志和调试）
func (l *UserConcurrencyLimiter) GetUserConfig(userId int) map[string]interface{} {
	config, err := model.GetUserConcurrencyConfig(userId)
	if err != nil || config == nil {
		return map[string]interface{}{
			"user_id":          userId,
			"max_concurrent":   5,
			"enable_auto_limit": true,
			"status":           1,
			"current":          l.GetCurrentConcurrent(userId),
			"dynamic_limit":    l.calculateLimit(userId),
		}
	}
	return map[string]interface{}{
		"user_id":          config.UserId,
		"max_concurrent":   config.MaxConcurrent,
		"enable_auto_limit": config.EnableAutoLimit,
		"status":           config.Status,
		"current":          l.GetCurrentConcurrent(userId),
		"dynamic_limit":    l.calculateLimit(userId),
	}
}

// Acquire 获取并发许可
// 如果当前并发数已达上限，会等待直到获得许可或超时
func (l *UserConcurrencyLimiter) Acquire(ctx context.Context, userId int) error {
	// 检查是否启用
	if !config.EnableConcurrencyLimit {
		return nil
	}

	// 检查用户是否被暂停
	if !model.IsUserConcurrencyEnabled(userId) {
		return nil
	}

	// 计算限制
	limit := l.calculateLimit(userId)
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			logger.Warnf(ctx, "用户 %d 并发请求等待超时", userId)
			return ErrWaitTimeout
		default:
			// 获取当前并发数
			current := l.GetCurrentConcurrent(userId)

			if current < limit {
				// 有空位，尝试获取
				if l.tryIncrement(userId) {
					waitTime := time.Since(startTime)
					if waitTime > time.Second {
						logger.Debugf(ctx, "用户 %d 等待 %v 后获得并发许可 (current=%d, limit=%d)",
							userId, waitTime, current+1, limit)
					}
					return nil
				}
			}

			// 等待后重试
			time.Sleep(l.checkInterval)
		}
	}
}

// Release 释放并发许可
func (l *UserConcurrencyLimiter) Release(ctx context.Context, userId int) {
	if !config.EnableConcurrencyLimit {
		return
	}
	l.Decrement(userId)
	logger.Debugf(ctx, "用户 %d 释放并发许可", userId)
}

// 强制设置用户并发数（用于调试）
func (l *UserConcurrencyLimiter) SetConcurrent(userId int, count int) error {
	if l.redis == nil {
		return errors.New("Redis不可用")
	}
	key := getConcurrentKey(userId)
	return l.redis.Set(context.Background(), key, count, 0).Err()
}

// 获取所有用户的并发数统计
func (l *UserConcurrencyLimiter) GetAllStats() (map[int]int, error) {
	if l.redis == nil {
		return nil, errors.New("Redis不可用")
	}

	pattern := "concurrent:*"
	keys, err := l.redis.Keys(context.Background(), pattern).Result()
	if err != nil {
		return nil, err
	}

	stats := make(map[int]int)
	for _, key := range keys {
		userIdStr := key[len("concurrent:"):]
		userId, _ := strconv.Atoi(userIdStr)
		val, _ := l.redis.Get(context.Background(), key).Int()
		stats[userId] = val
	}

	return stats, nil
}