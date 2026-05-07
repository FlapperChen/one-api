package concurrency

import (
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/monitor"
	"github.com/songquanpeng/one-api/monitor/gpu"
	"github.com/songquanpeng/one-api/model"
)

// 配置读取辅助函数
func getConfigInt(key string, defaultVal int) int {
	if val, ok := config.OptionMap[key]; ok {
		if v, err := strconv.Atoi(val); err == nil {
			return v
		}
	}
	return defaultVal
}

func getConfigFloat(key string, defaultVal float64) float64 {
	if val, ok := config.OptionMap[key]; ok {
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			return v
		}
	}
	return defaultVal
}

// 获取当前配置值
func GetCurrentConfig() map[string]interface{} {
	return map[string]interface{}{
		"user_base_concurrent_limit":  getConfigInt("UserBaseConcurrentLimit", config.UserBaseConcurrentLimit),
		"concurrency_wait_timeout":    getConfigInt("ConcurrencyWaitTimeout", config.ConcurrencyWaitTimeout),
		"concurrency_check_interval":  getConfigInt("ConcurrencyCheckInterval", config.ConcurrencyCheckInterval),
		"duration_factor_weight":      getConfigInt("DurationFactorWeight", config.DurationFactorWeight),
		"concurrent_factor_weight":    getConfigInt("ConcurrentFactorWeight", config.ConcurrentFactorWeight),
		"trend_factor_weight":         getConfigInt("TrendFactorWeight", config.TrendFactorWeight),
		"gpu_factor_weight":           getConfigInt("GPUFactorWeight", config.GPUFactorWeight),
		"gpu_kv_cache_warn":           getConfigFloat("GPUKVCacheWarn", config.GPUKVCacheWarnThreshold),
		"gpu_kv_cache_max":            getConfigFloat("GPUKVCacheMax", config.GPUKVCacheMaxThreshold),
	}
}

// 负载等级
type LoadLevel int

const (
	LoadLevelLow      LoadLevel = iota // 0: 正常负载
	LoadLevelMedium                     // 1: 中等负载
	LoadLevelHigh                       // 2: 高负载
	LoadLevelCritical                   // 3: 临界负载
)

// 基础指标
type EnhancedLoadMetrics struct {
	ResponseTime       int64
	SuccessRate        float64
	RequestCount       int64
	ErrorCount         int64
	GPUKVCacheUsage    float64
	RunningRequests    int
	AvgRequestDuration int64
}

// 负载评估器
type LoadEvaluator struct {
	mu                 sync.RWMutex
	metrics            *EnhancedLoadMetrics           // 系统级指标
	userWindows        map[int]*UserWindow            // 用户级别滑动窗口
	lastUpdateTime     int64
}

// 用户滑动窗口
type UserWindow struct {
	mu         sync.RWMutex
	window     []int  // 并发数滑动窗口
	index      int    // 当前索引
	lastUpdate int64  // 最后更新时间
}

var (
	evaluator     *LoadEvaluator
	evaluatorOnce sync.Once
)

// 获取负载评估器单例
func GetLoadEvaluator() *LoadEvaluator {
	evaluatorOnce.Do(func() {
		evaluator = &LoadEvaluator{
			metrics: &EnhancedLoadMetrics{
				ResponseTime:       1000,
				SuccessRate:        1.0,
				RequestCount:       0,
				ErrorCount:         0,
				GPUKVCacheUsage:    0,
				AvgRequestDuration: 2000,
				RunningRequests:    0,
			},
			userWindows: make(map[int]*UserWindow),
		}
	})
	return evaluator
}

// GetMetrics 获取系统级指标
func (e *LoadEvaluator) GetMetrics() *EnhancedLoadMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.metrics
}

// UpdateMetrics 更新系统级指标
func (e *LoadEvaluator) UpdateMetrics(metrics *EnhancedLoadMetrics) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics = metrics
	e.lastUpdateTime = time.Now().Unix()
}

// 获取用户滑动窗口
func (e *LoadEvaluator) getUserWindow(userId int) *UserWindow {
	e.mu.RLock()
	window, exists := e.userWindows[userId]
	e.mu.RUnlock()

	if !exists {
		e.mu.Lock()
		// 双重检查
		if window, exists = e.userWindows[userId]; !exists {
			window = &UserWindow{
				window:     make([]int, 60), // 60秒窗口
				index:      0,
				lastUpdate: time.Now().Unix(),
			}
			e.userWindows[userId] = window
		}
		e.mu.Unlock()
	}
	return window
}

// 更新用户滑动窗口
func (e *LoadEvaluator) UpdateUserWindow(userId int, count int) {
	window := e.getUserWindow(userId)
	window.mu.Lock()
	defer window.mu.Unlock()
	window.window[window.index] = count
	window.index = (window.index + 1) % len(window.window)
	window.lastUpdate = time.Now().Unix()
}

// UpdateWindowOnAcquire 请求获取时更新滑动窗口（单用户）
func UpdateWindowOnAcquire(userId int, count int) {
	if evaluator := GetLoadEvaluator(); evaluator != nil {
		evaluator.UpdateUserWindow(userId, count)
	}
}

// UpdateWindowOnRelease 请求释放时更新滑动窗口（单用户）
func UpdateWindowOnRelease(userId int, count int) {
	if evaluator := GetLoadEvaluator(); evaluator != nil {
		evaluator.UpdateUserWindow(userId, count)
	}
}

// CalculateUserTrendFactor 计算用户级别的趋势因子
func (e *LoadEvaluator) CalculateUserTrendFactor(userId int) float64 {
	window := e.getUserWindow(userId)
	window.mu.RLock()
	defer window.mu.RUnlock()

	windowSize := getConfigInt("ConcurrencyWaitTimeout", config.ConcurrencyWaitTimeout) / 3
	if windowSize < 5 {
		windowSize = 5
	}
	if windowSize > 30 {
		windowSize = 30
	}

	// 计算最近窗口的平均值
	var recentSum int
	idx := window.index
	for i := 0; i < windowSize; i++ {
		pos := (idx - windowSize + i + len(window.window)) % len(window.window)
		recentSum += window.window[pos]
	}
	avgRecent := float64(recentSum) / float64(windowSize)

	// 计算之前窗口的平均值
	var oldSum int
	for i := windowSize; i < windowSize*2; i++ {
		pos := (idx - windowSize * 2 + i + len(window.window)) % len(window.window)
		oldSum += window.window[pos]
	}
	avgOld := float64(oldSum) / float64(windowSize)

	if avgOld == 0 {
		return 1.0
	}

	ratio := avgRecent / avgOld
	if ratio > 2.0 {
		return 0.3
	} else if ratio > 1.5 {
		return 0.5
	} else if ratio > 1.2 {
		return 0.7
	} else if ratio < 0.5 {
		return 0.3
	} else if ratio < 0.7 {
		return 0.5
	}
	return 1.0
}

// CalculateUserDynamicLimit 计算用户级别的动态限制
func (e *LoadEvaluator) CalculateUserDynamicLimit(userId int, baseLimit int, currentConcurrent int) int {
	metrics := e.GetMetrics()

	// 从 OptionMap 获取权重
	durationWeight := float64(getConfigInt("DurationFactorWeight", config.DurationFactorWeight)) / 100.0
	concurrentWeight := float64(getConfigInt("ConcurrentFactorWeight", config.ConcurrentFactorWeight)) / 100.0
	trendWeight := float64(getConfigInt("TrendFactorWeight", config.TrendFactorWeight)) / 100.0
	gpuWeight := float64(getConfigInt("GPUFactorWeight", config.GPUFactorWeight)) / 100.0

	// 计算因子
	durationFactor := CalculateDurationFactor(metrics.AvgRequestDuration)
	concurrentFactor := CalculateConcurrentFactor(currentConcurrent, baseLimit)
	trendFactor := e.CalculateUserTrendFactor(userId)
	gpuFactor := CalculateGPUFactor(metrics.GPUKVCacheUsage)

	// 加权计算
	limit := float64(baseLimit)
	limit *= math.Pow(durationFactor, durationWeight)
	limit *= math.Pow(concurrentFactor, concurrentWeight)
	limit *= math.Pow(trendFactor, trendWeight)
	limit *= math.Pow(gpuFactor, gpuWeight)

	if limit < 1 {
		limit = 1
	}
	return int(limit)
}

// CalculateDynamicLimit 计算系统级别的动态限制（兼容旧代码）
func (e *LoadEvaluator) CalculateDynamicLimit(baseLimit int) int {
	return e.CalculateUserDynamicLimit(0, baseLimit, 0)
}

// GetLoadLevel 获取系统负载等级
func (e *LoadEvaluator) GetLoadLevel() LoadLevel {
	m := e.GetMetrics()
	maxTimeout := float64(getConfigInt("ConcurrencyWaitTimeout", config.ConcurrencyWaitTimeout)) * 1000
	if maxTimeout <= 0 {
		maxTimeout = 30000
	}

	// 从 OptionMap 读取 GPU 阈值
	warnThreshold := config.GPUKVCacheWarnThreshold
	maxThreshold := config.GPUKVCacheMaxThreshold
	if val, ok := config.OptionMap["GPUKVCacheWarn"]; ok {
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			warnThreshold = v
		}
	}
	if val, ok := config.OptionMap["GPUKVCacheMax"]; ok {
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			maxThreshold = v
		}
	}

	if m.GPUKVCacheUsage >= maxThreshold || m.ResponseTime > int64(maxTimeout*0.67) || m.SuccessRate < 0.7 {
		return LoadLevelCritical
	}
	if m.GPUKVCacheUsage >= warnThreshold || m.ResponseTime > int64(maxTimeout*0.33) || m.SuccessRate < 0.8 {
		return LoadLevelHigh
	}
	if m.ResponseTime > int64(maxTimeout*0.1) || m.SuccessRate < 0.9 {
		return LoadLevelMedium
	}
	return LoadLevelLow
}

// RefreshMetrics 刷新系统级指标
func (e *LoadEvaluator) RefreshMetrics() {
	failRate := monitor.GetSystemFailRate()
	successRate := 1.0 - failRate

	var avgResponseTime int64 = 1000
	channels, err := model.GetAllChannels(0, 1000, "all")
	if err == nil && len(channels) > 0 {
		var totalRT int64
		var validCount int
		for _, ch := range channels {
			if ch.Status == model.ChannelStatusEnabled && ch.ResponseTime > 0 {
				totalRT += int64(ch.ResponseTime)
				validCount++
			}
		}
		if validCount > 0 {
			avgResponseTime = totalRT / int64(validCount)
		}
	}

	var gpuKVCacheUsage float64
	if gpu.IsEnabled() {
		vllmMonitor := gpu.GetVLLMMonitor()
		vllmMonitor.Refresh()
		gpuKVCacheUsage = vllmMonitor.GetGPUKVCacheUsage()
	}

	e.UpdateMetrics(&EnhancedLoadMetrics{
		AvgRequestDuration: avgResponseTime,
		GPUKVCacheUsage:    gpuKVCacheUsage,
		ResponseTime:       avgResponseTime,
		SuccessRate:        successRate,
		RequestCount:       monitor.GetTotalRequestCount(),
		ErrorCount:         int64(float64(monitor.GetTotalRequestCount()) * failRate),
		RunningRequests:    0,
	})
}

// 获取用户级别的负载信息
func (e *LoadEvaluator) GetUserLoadInfo(userId int, baseLimit int, currentConcurrent int) map[string]interface{} {
	e.RefreshMetrics()
	metrics := e.GetMetrics()
	limit := e.CalculateUserDynamicLimit(userId, baseLimit, currentConcurrent)
	trendFactor := e.CalculateUserTrendFactor(userId)

	return map[string]interface{}{
		"dynamic_limit":       limit,
		"current_concurrent":  currentConcurrent,
		"gpu_usage":           metrics.GPUKVCacheUsage,
		"avg_duration":        metrics.AvgRequestDuration,
		"success_rate":        metrics.SuccessRate,
		"load_level":          e.GetLoadLevel(),
		"factors": gin.H{
			"duration":   CalculateDurationFactor(metrics.AvgRequestDuration),
			"concurrent": CalculateConcurrentFactor(currentConcurrent, baseLimit),
			"trend":      trendFactor,
			"gpu":        CalculateGPUFactor(metrics.GPUKVCacheUsage),
		},
	}
}

// 请求时长因子
func CalculateDurationFactor(avgDurationMs int64) float64 {
	maxThreshold := float64(getConfigInt("ConcurrencyWaitTimeout", config.ConcurrencyWaitTimeout)) * 1000
	if maxThreshold <= 0 {
		maxThreshold = 30000
	}
	ratio := float64(avgDurationMs) / maxThreshold
	switch {
	case ratio < 0.1:
		return 1.0
	case ratio < 0.33:
		return 0.8
	case ratio < 0.67:
		return 0.5
	default:
		return 0.1
	}
}

// 并发因子
func CalculateConcurrentFactor(current, baseLimit int) float64 {
	if baseLimit == 0 {
		baseLimit = 5
	}
	ratio := float64(current) / float64(baseLimit)
	switch {
	case ratio < 0.5:
		return 1.0
	case ratio < 0.8:
		return 0.8
	case ratio < 1.0:
		return 0.6
	case ratio < 1.5:
		return 0.4
	default:
		return 0.2
	}
}

// GPU 因子
func CalculateGPUFactor(gpuUsage float64) float64 {
	if gpuUsage <= 0 {
		return 1.0
	}

	// 从 OptionMap 读取阈值（支持运行时修改）
	warnThreshold := config.GPUKVCacheWarnThreshold
	maxThreshold := config.GPUKVCacheMaxThreshold
	if val, ok := config.OptionMap["GPUKVCacheWarn"]; ok {
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			warnThreshold = v
		}
	}
	if val, ok := config.OptionMap["GPUKVCacheMax"]; ok {
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			maxThreshold = v
		}
	}

	switch {
	case gpuUsage < 50.0:
		return 1.0
	case gpuUsage < 70.0:
		return 0.8
	case gpuUsage < warnThreshold:
		return 0.5
	case gpuUsage < maxThreshold:
		return 0.3
	default:
		return 0.1
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}