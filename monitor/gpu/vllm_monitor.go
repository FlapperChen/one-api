package gpu

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/songquanpeng/one-api/common/config"
	"github.com/songquanpeng/one-api/common/logger"
)

// VLLM 指标结构
type VLLMMetrics struct {
	RunningRequests      int     `json:"running_requests"`
	WaitingRequests      int     `json:"waiting_requests"`
	GPUKVCacheUsage      float64 `json:"gpu_kv_cache_usage"`       // 百分比
	PromptThroughput     float64 `json:"prompt_throughput"`        // tokens/s
	GenerationThroughput float64 `json:"generation_throughput"`    // tokens/s
	PrefixCacheHitRate   float64 `json:"prefix_cache_hit_rate"`    // 百分比
	Timestamp            int64   `json:"timestamp"`
}

// GPU 监控器
type VLLMMonitor struct {
	mu               sync.RWMutex
	metrics          *VLLMMetrics
	logPath          string
	apiURL           string
	refreshInterval  time.Duration
	stopChan         chan struct{}
	enabled          bool
}

var (
	monitor     *VLLMMonitor
	monitorOnce sync.Once
)

// GetAPIURL 获取当前配置的 API URL（每次从 OptionMap 读取）
func GetAPIURL() string {
	vllmAPIURL := config.OptionMap["VLLMAPIURL"]
	if vllmAPIURL == "" {
		vllmAPIURL = config.VLLMAPIURL
	}
	return vllmAPIURL
}

// IsEnabled 检查 GPU 监控是否启用
func IsEnabled() bool {
	if val, ok := config.OptionMap["EnableGPUMonitoring"]; ok {
		return val == "true"
	}
	return config.EnableGPUMonitoring
}

// VLLM 日志正则
var (
	vllmLogPattern = regexp.MustCompile(`Running:\s*(\d+)\s*reqs,\s*Waiting:\s*(\d+)\s*reqs,\s*GPU KV cache usage:\s*([\d.]+)%`)
	metricsPattern = regexp.MustCompile(`Avg prompt throughput:\s*([\d.]+)\s*tokens/s,\s*Avg generation throughput:\s*([\d.]+)\s*tokens/s`)
	prefixPattern  = regexp.MustCompile(`Prefix cache hit rate:\s*([\d.]+)%`)
)

// 获取监控器单例
func GetVLLMMonitor() *VLLMMonitor {
	monitorOnce.Do(func() {
		// 从 OptionMap 读取配置（前端保存的值）
		vllmAPIURL := config.OptionMap["VLLMAPIURL"]
		if vllmAPIURL == "" {
			vllmAPIURL = config.VLLMAPIURL // 降级到环境变量
		}

		monitor = &VLLMMonitor{
			metrics: &VLLMMetrics{},
			logPath: config.VLLMLogPath,
			apiURL:  vllmAPIURL,
			refreshInterval: time.Duration(config.VLLMRefreshInterval) * time.Second,
			stopChan:  make(chan struct{}),
			enabled:   config.EnableGPUMonitoring,
		}
	})
	return monitor
}

// Start 启动监控
func (m *VLLMMonitor) Start() {
	if !m.enabled {
		logger.SysLog("GPU monitoring is disabled")
		return
	}

	logger.SysLogf("GPU monitoring enabled, log path: %s, API: %s", m.logPath, m.apiURL)

	// 定期刷新指标
	go m.refreshLoop()
}

// Stop 停止监控
func (m *VLLMMonitor) Stop() {
	if m.stopChan != nil {
		close(m.stopChan)
	}
}

// refreshLoop 刷新循环
func (m *VLLMMonitor) refreshLoop() {
	ticker := time.NewTicker(m.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.Refresh()
		}
	}
}

// Refresh 刷新指标（每次从 OptionMap 获取最新配置）
func (m *VLLMMonitor) Refresh() {
	// 每次刷新时从 OptionMap 获取最新配置
	apiURL := GetAPIURL()
	m.apiURL = apiURL
	m.enabled = IsEnabled()

	if !m.enabled {
		return
	}

	// 优先从 API 获取
	if apiURL != "" {
		if err := m.fetchFromAPI(); err == nil {
			return
		}
	}

	// 降级到日志解析
	if m.logPath != "" {
		m.parseFromLog()
	}
}

// fetchFromAPI 从 vLLM API 获取指标
func (m *VLLMMonitor) fetchFromAPI() error {
	if m.apiURL == "" {
		return fmt.Errorf("API URL not configured")
	}

	resp, err := http.Get(m.apiURL + "/metrics")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 解析 Prometheus 格式指标
	scanner := bufio.NewScanner(resp.Body)
	var metrics VLLMMetrics
	metrics.Timestamp = time.Now().Unix()

	for scanner.Scan() {
		line := scanner.Text()

		// 解析 vllm:num_requests_running
		if strings.Contains(line, "vllm:num_requests_running") {
			if parts := strings.Split(line, " "); len(parts) >= 2 {
				if v, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
					metrics.RunningRequests = v
				}
			}
		}

		// 解析 vllm:num_requests_waiting
		if strings.Contains(line, "vllm:num_requests_waiting") {
			if parts := strings.Split(line, " "); len(parts) >= 2 {
				if v, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
					metrics.WaitingRequests = v
				}
			}
		}

		// 解析 vllm:kv_cache_usage_perc (vLLM 标准指标，值为 0-1)
		if strings.Contains(line, "vllm:kv_cache_usage_perc") {
			if parts := strings.Split(line, " "); len(parts) >= 2 {
				if v, err := strconv.ParseFloat(parts[1], 64); err == nil {
					metrics.GPUKVCacheUsage = v * 100 // 转换为百分比
				}
			}
		}
	}

	m.updateMetrics(&metrics)
	return nil
}

// parseFromLog 从日志文件解析指标
func (m *VLLMMonitor) parseFromLog() {
	if m.logPath == "" {
		return
	}

	// 查找最新的日志文件
	logFiles, err := getLogFiles(m.logPath)
	if err != nil || len(logFiles) == 0 {
		return
	}

	// 读取最新的日志文件
	latestLog := logFiles[len(logFiles)-1]
	file, err := os.Open(latestLog)
	if err != nil {
		return
	}
	defer file.Close()

	var metrics VLLMMetrics
	metrics.Timestamp = time.Now().Unix()

	// 从末尾读取最后几行
	scanner := bufio.NewScanner(file)
	var lastLines []string
	maxLines := 100

	for scanner.Scan() {
		line := scanner.Text()
		lastLines = append(lastLines, line)
		if len(lastLines) > maxLines {
			lastLines = lastLines[1:]
		}
	}

	// 解析最后几行
	for _, line := range lastLines {
		// 解析主要指标
		if matches := vllmLogPattern.FindStringSubmatch(line); len(matches) == 4 {
			if v, err := strconv.Atoi(matches[1]); err == nil {
				metrics.RunningRequests = v
			}
			if v, err := strconv.Atoi(matches[2]); err == nil {
				metrics.WaitingRequests = v
			}
			if v, err := strconv.ParseFloat(matches[3], 64); err == nil {
				metrics.GPUKVCacheUsage = v
			}
		}

		// 解析吞吐量
		if matches := metricsPattern.FindStringSubmatch(line); len(matches) == 3 {
			if v, err := strconv.ParseFloat(matches[1], 64); err == nil {
				metrics.PromptThroughput = v
			}
			if v, err := strconv.ParseFloat(matches[2], 64); err == nil {
				metrics.GenerationThroughput = v
			}
		}

		// 解析 Prefix Cache
		if matches := prefixPattern.FindStringSubmatch(line); len(matches) == 2 {
			if v, err := strconv.ParseFloat(matches[1], 64); err == nil {
				metrics.PrefixCacheHitRate = v
			}
		}
	}

	if metrics.RunningRequests > 0 || metrics.GPUKVCacheUsage > 0 {
		m.updateMetrics(&metrics)
	}
}

// getLogFiles 获取日志文件列表
func getLogFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") {
			files = append(files, dir+"/"+entry.Name())
		}
	}
	return files, nil
}

// updateMetrics 更新指标
func (m *VLLMMonitor) updateMetrics(newMetrics *VLLMMetrics) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = newMetrics
}

// GetMetrics 获取当前指标
func (m *VLLMMonitor) GetMetrics() *VLLMMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metrics
}

// GetGPUKVCacheUsage 获取 GPU KV Cache 使用率
func (m *VLLMMonitor) GetGPUKVCacheUsage() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metrics.GPUKVCacheUsage
}

// GetRunningRequests 获取运行中的请求数
func (m *VLLMMonitor) GetRunningRequests() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metrics.RunningRequests
}

// GetLoadLevel 获取负载等级
func (m *VLLMMonitor) GetLoadLevel() int {
	usage := m.GetGPUKVCacheUsage()
	running := m.GetRunningRequests()

	if usage >= 95 || running >= 20 {
		return 3 // Critical
	}
	if usage >= 85 || running >= 15 {
		return 2 // High
	}
	if usage >= 70 || running >= 10 {
		return 1 // Medium
	}
	return 0 // Low
}

// IsOverloaded 检查是否过载
func (m *VLLMMonitor) IsOverloaded() bool {
	return m.GetGPUKVCacheUsage() >= config.GPUKVCacheMaxThreshold
}

// GetLoadFactor 获取负载因子 (0-1)
func (m *VLLMMonitor) GetLoadFactor() float64 {
	usage := m.GetGPUKVCacheUsage()
	if usage >= 95 {
		return 0.1
	}
	if usage >= 85 {
		return 0.3
	}
	if usage >= 70 {
		return 0.5
	}
	if usage >= 50 {
		return 0.7
	}
	return 1.0
}

// VLLM 日志文件配置
type VLLMLogConfig struct {
	LogPath string `json:"log_path"`
	APIURL  string `json:"api_url"`
}

// ToJSON 转换为 JSON
func (m *VLLMMetrics) ToJSON() string {
	data, _ := json.Marshal(m)
	return string(data)
}