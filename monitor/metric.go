package monitor

import (
	"github.com/songquanpeng/one-api/common/config"
)

var store = make(map[int][]bool)
var metricSuccessChan = make(chan int, config.MetricSuccessChanSize)
var metricFailChan = make(chan int, config.MetricFailChanSize)

func consumeSuccess(channelId int) {
	if len(store[channelId]) > config.MetricQueueSize {
		store[channelId] = store[channelId][1:]
	}
	store[channelId] = append(store[channelId], true)
}

func consumeFail(channelId int) (bool, float64) {
	if len(store[channelId]) > config.MetricQueueSize {
		store[channelId] = store[channelId][1:]
	}
	store[channelId] = append(store[channelId], false)
	successCount := 0
	for _, success := range store[channelId] {
		if success {
			successCount++
		}
	}
	successRate := float64(successCount) / float64(len(store[channelId]))
	if len(store[channelId]) < config.MetricQueueSize {
		return false, successRate
	}
	if successRate < config.MetricSuccessRateThreshold {
		store[channelId] = make([]bool, 0)
		return true, successRate
	}
	return false, successRate
}

func metricSuccessConsumer() {
	for {
		select {
		case channelId := <-metricSuccessChan:
			consumeSuccess(channelId)
		}
	}
}

func metricFailConsumer() {
	for {
		select {
		case channelId := <-metricFailChan:
			disable, successRate := consumeFail(channelId)
			if disable {
				go MetricDisableChannel(channelId, successRate)
			}
		}
	}
}

func init() {
	if config.EnableMetric {
		go metricSuccessConsumer()
		go metricFailConsumer()
	}
}

func Emit(channelId int, success bool) {
	if !config.EnableMetric {
		return
	}
	go func() {
		if success {
			metricSuccessChan <- channelId
		} else {
			metricFailChan <- channelId
		}
	}()
}

// GetSystemFailRate 获取系统级失败率
func GetSystemFailRate() float64 {
	if !config.EnableMetric {
		return 0.0
	}
	totalSuccess := 0
	totalCount := 0
	for _, history := range store {
		for _, success := range history {
			totalCount++
			if success {
				totalSuccess++
			}
		}
	}
	if totalCount == 0 {
		return 0.0
	}
	return 1.0 - float64(totalSuccess)/float64(totalCount)
}

// GetTotalRequestCount 获取系统总请求数
func GetTotalRequestCount() int64 {
	if !config.EnableMetric {
		return 0
	}
	totalCount := 0
	for _, history := range store {
		totalCount += len(history)
	}
	return int64(totalCount)
}

// GetChannelSuccessRate 获取指定渠道的成功率
func GetChannelSuccessRate(channelId int) float64 {
	if !config.EnableMetric {
		return 1.0
	}
	history, ok := store[channelId]
	if !ok || len(history) == 0 {
		return 1.0
	}
	successCount := 0
	for _, success := range history {
		if success {
			successCount++
		}
	}
	return float64(successCount) / float64(len(history))
}
