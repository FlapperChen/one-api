package concurrency

import (
	"sync"
	"time"

	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/monitor"
	"github.com/songquanpeng/one-api/monitor/gpu"
	"github.com/songquanpeng/one-api/model"
)

// 负载等级
type LoadLevel int

const (
	LoadLevelLow      LoadLevel = iota // 0: 正常负载
	LoadLevelMedium                     // 1: 中等负载
	LoadLevelHigh                       // 2: 高负载
	LoadLevelCritical                   // 3: 临界负载
)

// 增强版负载指标
type EnhancedLoadMetrics struct {
	// 因素1: 单次模型平均请求时长 (ms)
	AvgRequestDuration int64

	// 因素2: 当前总并发数量
	CurrentConcurrent int

	// 因素3: 一段时间内并发数量 (滑动窗口)
	ConcurrentWindow []int

	// 因素4: GPU KV Cache 占用百分比
	GPUKVCacheUsage float64

	// 基础指标
	ResponseTime    int64
	SuccessRate     float64
	RequestCount    int64
	ErrorCount      int64
	RunningRequests int // vLLM 运行中的请求数
}

// 负载评估器 (增强版)
type LoadEvaluator struct {
	mu               sync.RWMutex
	metrics          *EnhancedLoadMetrics
	concurrentWindow []int // 60秒滑动窗口，每秒一个采样
	windowIndex      int
	lastUpdateTime   int64
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
				ResponseTime:    1000,
				SuccessRate:     1.0,
				RequestCount:    0,
				ErrorCount:      0,
				GPUKVCacheUsage: 0,
				AvgRequestDuration: 2000,
				CurrentConcurrent:  0,
				RunningRequests: 0,
			},
			concurrentWindow: make([]int, 60), // 60秒窗口
			windowIndex:      0,
		}
	})
	return evaluator
}

// 获取当前负载指标
func (e *LoadEvaluator) GetMetrics() *EnhancedLoadMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.metrics
}

// 更新负载指标
func (e *LoadEvaluator) UpdateMetrics(metrics *EnhancedLoadMetrics) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics = metrics
	e.lastUpdateTime = time.Now().Unix()
}

// 获取系统负载等级
func (e *LoadEvaluator) GetLoadLevel() LoadLevel {
	m := e.GetMetrics()

	// 综合判断
	if m.GPUKVCacheUsage >= config.GPUKVCacheMaxThreshold || m.ResponseTime > 5000 || m.SuccessRate < 0.7 {
		return LoadLevelCritical
	}
	if m.GPUKVCacheUsage >= config.GPUKVCacheWarnThreshold || m.ResponseTime > 3000 || m.SuccessRate < 0.8 {
		return LoadLevelHigh
	}
	if m.ResponseTime > 2000 || m.SuccessRate < 0.9 {
		return LoadLevelMedium
	}
	return LoadLevelLow
}

// UpdateConcurrentWindow 更新并发滑动窗口
func (e *LoadEvaluator) UpdateConcurrentWindow(count int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.concurrentWindow[e.windowIndex] = count
	e.windowIndex = (e.windowIndex + 1) % len(e.concurrentWindow)
	e.metrics.CurrentConcurrent = count
}

// 计算增强版动态并发限制
// 考虑四个因素: 请求时长, 当前并发, 并发趋势, GPU资源
func (e *LoadEvaluator) CalculateDynamicLimit(baseLimit int) int {
	metrics := e.GetMetrics()
	limit := float64(baseLimit)

	// 1. 请求时长因子 (权重 25%)
	// 响应时间越长，应降低并发
	durationFactor := CalculateDurationFactor(metrics.AvgRequestDuration)
	limit *= durationFactor

	// 2. 当前并发因子 (权重 25%)
	// 当前并发越高，降低后续并发
	concurrentFactor := CalculateConcurrentFactor(metrics.CurrentConcurrent, baseLimit)
	limit *= concurrentFactor

	// 3. 并发趋势因子 (权重 20%)
	// 并发快速增长时，降低限制
	trendFactor := e.CalculateTrendFactor()
	limit *= trendFactor

	// 4. GPU KV Cache 因子 (权重 30%)
	// KV Cache 使用率越高，应降低并发
	gpuFactor := CalculateGPUFactor(metrics.GPUKVCacheUsage)
	limit *= gpuFactor

	// 确保最小值为1
	if limit < 1 {
		limit = 1
	}

	return int(limit)
}

// 请求时长因子
// < 1s: 1.0, 1-3s: 0.8, 3-5s: 0.5, 5-10s: 0.3, > 10s: 0.1
func CalculateDurationFactor(avgDurationMs int64) float64 {
	switch {
	case avgDurationMs < 1000:
		return 1.0
	case avgDurationMs < 3000:
		return 0.8
	case avgDurationMs < 5000:
		return 0.5
	case avgDurationMs < 10000:
		return 0.3
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

// 并发趋势因子
func (e *LoadEvaluator) CalculateTrendFactor() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	window := e.concurrentWindow
	windowLen := len(window)

	if windowLen < 10 {
		return 1.0
	}

	// 计算最近10秒和之前的平均值
	var recentSum, oldSum int
	recentCount := 10
	oldCount := 10

	if e.windowIndex >= recentCount {
		for i := 0; i < recentCount; i++ {
			idx := (e.windowIndex - recentCount + i + windowLen) % windowLen
			recentSum += window[idx]
		}
	} else {
		recentSum = window[e.windowIndex] * recentCount
	}

	if e.windowIndex >= recentCount+oldCount {
		for i := 0; i < oldCount; i++ {
			idx := (e.windowIndex - recentCount - oldCount + i + windowLen) % windowLen
			oldSum += window[idx]
		}
	} else {
		oldSum = recentSum // 数据不足时使用相同值
	}

	avgRecent := float64(recentSum) / float64(recentCount)
	avgOld := float64(oldSum) / float64(max(oldCount, 1))

	if avgOld == 0 {
		return 1.0
	}

	ratio := avgRecent / avgOld
	if ratio > 2.0 {
		return 0.3 // 快速增长
	} else if ratio > 1.5 {
		return 0.5 // 温和增长
	} else if ratio > 1.2 {
		return 0.7 // 轻微增长
	}
	return 1.0
}

// GPU KV Cache 因子
// < 50%: 1.0, 50-70%: 0.8, 70-85%: 0.5, 85-95%: 0.3, > 95%: 0.1
func CalculateGPUFactor(gpuUsage float64) float64 {
	// 如果GPU监控未启用，返回1.0
	if gpuUsage <= 0 {
		return 1.0
	}
	switch {
	case gpuUsage < 50.0:
		return 1.0
	case gpuUsage < 70.0:
		return 0.8
	case gpuUsage < config.GPUKVCacheWarnThreshold:
		return 0.5
	case gpuUsage < config.GPUKVCacheMaxThreshold:
		return 0.3
	default:
		return 0.1
	}
}

// max 辅助函数
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RefreshMetrics 刷新负载指标
func (e *LoadEvaluator) RefreshMetrics() {
	// 获取系统级指标
	failRate := monitor.GetSystemFailRate()
	successRate := 1.0 - failRate

	// 获取渠道平均响应时间
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

	// 获取GPU指标
	var gpuKVCacheUsage float64
	var runningRequests int
	if gpu.IsEnabled() {
		vllmMonitor := gpu.GetVLLMMonitor()
		vllmMonitor.Refresh() // 刷新 GPU 数据
		gpuKVCacheUsage = vllmMonitor.GetGPUKVCacheUsage()
		runningRequests = vllmMonitor.GetRunningRequests()
	}

	// 获取总请求数
	totalRequests := monitor.GetTotalRequestCount()

	// 获取当前并发数
	currentConcurrent := 0
	if limiter := GetLimiter(); limiter != nil {
		// 尝试获取系统总并发数
		stats, _ := limiter.GetAllStats()
		for _, count := range stats {
			currentConcurrent += count
		}
	}

	// 更新并发滑动窗口
	e.UpdateConcurrentWindow(currentConcurrent)

	metrics := &EnhancedLoadMetrics{
		AvgRequestDuration: avgResponseTime,
		CurrentConcurrent:  currentConcurrent,
		GPUKVCacheUsage:    gpuKVCacheUsage,
		ResponseTime:       avgResponseTime,
		SuccessRate:        successRate,
		RequestCount:       totalRequests,
		ErrorCount:         int64(float64(totalRequests) * failRate),
		RunningRequests:    runningRequests,
	}

	e.UpdateMetrics(metrics)
}

// GetLoadInfo 获取负载信息 (用于调试和监控)
func (e *LoadEvaluator) GetLoadInfo(baseLimit int) map[string]interface{} {
	m := e.GetMetrics()
	limit := e.CalculateDynamicLimit(baseLimit)

	return map[string]interface{}{
		"base_limit":             baseLimit,
		"dynamic_limit":          limit,
		"current_concurrent":     m.CurrentConcurrent,
		"gpu_kv_cache_usage":     m.GPUKVCacheUsage,
		"avg_request_duration":   m.AvgRequestDuration,
		"response_time":          m.ResponseTime,
		"success_rate":           m.SuccessRate,
		"running_requests":       m.RunningRequests,
		"load_level":             e.GetLoadLevel(),
		"duration_factor":        CalculateDurationFactor(m.AvgRequestDuration),
		"concurrent_factor":      CalculateConcurrentFactor(m.CurrentConcurrent, baseLimit),
		"gpu_factor":             CalculateGPUFactor(m.GPUKVCacheUsage),
	}
}