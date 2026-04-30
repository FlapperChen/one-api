package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/songquanpeng/one-api/common/helper"
	"gorm.io/gorm"
)

const (
	RequestCacheStatusProcessing = 0
	RequestCacheStatusCompleted  = 1
	RequestCacheStatusFailed     = 2
)

type RequestCache struct {
	Id            int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId        int       `json:"user_id" gorm:"index"`
	Fingerprint   string    `json:"fingerprint" gorm:"index;size:64"` // 请求指纹
	RequestHash   string    `json:"request_hash" gorm:"size:64"`      // 完整请求Hash
	RequestType   string    `json:"request_type" gorm:"size:32"`      // chat/completion
	Model         string    `json:"model" gorm:"index;size:128"`
	Status        int       `json:"status" gorm:"default:0"`
	RequestBody   string    `json:"request_body" gorm:"type:text"`       // 原始请求体
	ResponseBody  string    `json:"response_body" gorm:"type:text"`     // 响应内容
	ErrorMessage  string    `json:"error_message" gorm:"type:text"`      // 错误信息
	CreatedTime   int64     `json:"created_time" gorm:"bigint"`
	CompletedTime int64     `json:"completed_time" gorm:"bigint"`
	DuplicateCount int      `json:"duplicate_count" gorm:"default:0"`    // 重复请求数
	TTL           int       `json:"ttl" gorm:"default:30"`              // 缓存过期秒数
}

func (RequestCache) TableName() string {
	return "request_cache"
}

func (r *RequestCache) BeforeCreate(tx *gorm.DB) error {
	r.CreatedTime = helper.GetTimestamp()
	return nil
}

// RequestFingerprint 计算请求指纹
type RequestFingerprint struct {
	Model      string                 `json:"model"`
	Messages   []FingerprintMessage   `json:"messages,omitempty"`
	Prompt     string                 `json:"prompt,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

type FingerprintMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CalculateFingerprint 计算请求指纹
// 指纹由 model + 消息内容(去序) + 参数(去随机) 组成
func CalculateFingerprint(reqBody map[string]interface{}) string {
	var fp RequestFingerprint

	// 提取 model
	if model, ok := reqBody["model"].(string); ok {
		fp.Model = model
	}

	// 提取 messages (chat completions)
	if messages, ok := reqBody["messages"].([]interface{}); ok {
		fp.Messages = make([]FingerprintMessage, 0, len(messages))
		for _, m := range messages {
			if msg, ok := m.(map[string]interface{}); ok {
				fm := FingerprintMessage{}
				if role, ok := msg["role"].(string); ok {
					fm.Role = role
				}
				if content, ok := msg["content"].(string); ok {
					fm.Content = content
				}
				fp.Messages = append(fp.Messages, fm)
			}
		}
	}

	// 提取 prompt (completions)
	if prompt, ok := reqBody["prompt"].(string); ok {
		fp.Prompt = prompt
	}

	// 提取参数 (去除随机参数)
	params := make(map[string]interface{})
	for k, v := range reqBody {
		if k == "model" || k == "messages" || k == "prompt" {
			continue
		}
		// 跳过随机参数
		if k == "seed" || k == "user" || k == "stream" {
			continue
		}
		params[k] = v
	}
	fp.Parameters = params

	// 序列化并计算 Hash
	content := serializeFingerprint(fp)
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

func serializeFingerprint(fp RequestFingerprint) string {
	var parts []string

	// Model
	parts = append(parts, fp.Model)

	// Messages (排序确保顺序一致)
	if len(fp.Messages) > 0 {
		var msgParts []string
		for _, m := range fp.Messages {
			msgParts = append(msgParts, fmt.Sprintf("%s:%s", m.Role, m.Content))
		}
		sort.Strings(msgParts)
		parts = append(parts, strings.Join(msgParts, "|"))
	}

	// Prompt
	if fp.Prompt != "" {
		parts = append(parts, fp.Prompt)
	}

	// Parameters (排序键)
	if len(fp.Parameters) > 0 {
		var paramKeys []string
		for k := range fp.Parameters {
			paramKeys = append(paramKeys, k)
		}
		sort.Strings(paramKeys)
		for _, k := range paramKeys {
			parts = append(parts, fmt.Sprintf("%s:%v", k, fp.Parameters[k]))
		}
	}

	return strings.Join(parts, "\n")
}

// CalculateFullHash 计算完整请求Hash (用于精确去重)
func CalculateFullHash(reqBody map[string]interface{}) string {
	content, _ := json.Marshal(reqBody)
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// GetRequestCache 获取请求缓存
func GetRequestCache(userId int, fingerprint string) (*RequestCache, error) {
	var cache RequestCache
	err := DB.Where("user_id = ? AND fingerprint = ? AND status = ? AND created_time > ?",
		userId, fingerprint, RequestCacheStatusCompleted,
		helper.GetTimestamp()-300). // 5分钟内
		Order("created_time DESC").
		First(&cache).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &cache, nil
}

// GetProcessingRequest 获取正在处理的请求
func GetProcessingRequest(userId int, fingerprint string) (*RequestCache, error) {
	var cache RequestCache
	err := DB.Where("user_id = ? AND fingerprint = ? AND status = ? AND created_time > ?",
		userId, fingerprint, RequestCacheStatusProcessing,
		helper.GetTimestamp()-300). // 5分钟内
		First(&cache).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &cache, nil
}

// CreateRequestCache 创建请求缓存
func CreateRequestCache(cache *RequestCache) error {
	return DB.Create(cache).Error
}

// UpdateRequestCache 更新请求缓存
func UpdateRequestCache(cache *RequestCache) error {
	return DB.Save(cache).Error
}

// CompleteRequestCache 完成请求缓存
func CompleteRequestCache(id int64, responseBody string) error {
	return DB.Model(&RequestCache{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":         RequestCacheStatusCompleted,
			"completed_time": helper.GetTimestamp(),
			"response_body":  responseBody,
		}).Error
}

// FailRequestCache 标记请求缓存失败
func FailRequestCache(id int64, errorMsg string) error {
	return DB.Model(&RequestCache{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":         RequestCacheStatusFailed,
			"completed_time": helper.GetTimestamp(),
			"error_message":  errorMsg,
		}).Error
}

// IncrementDuplicateCount 增加重复计数
func IncrementDuplicateCount(id int64) error {
	return DB.Model(&RequestCache{}).
		Where("id = ?", id).
		UpdateColumn("duplicate_count", gorm.Expr("duplicate_count + 1")).Error
}

// GetPendingRequests 获取用户待处理的请求
func GetPendingRequests(userId int, limit int) ([]*RequestCache, error) {
	var caches []*RequestCache
	err := DB.Where("user_id = ? AND status = ?", userId, RequestCacheStatusProcessing).
		Order("created_time ASC").
		Limit(limit).
		Find(&caches).Error
	return caches, err
}

// CleanupExpiredCache 清理过期缓存
func CleanupExpiredCache() error {
	// 清理超过TTL或24小时的缓存
	cutoff := helper.GetTimestamp() - 86400 // 24小时
	return DB.Where("created_time < ?", cutoff).Delete(&RequestCache{}).Error
}

// NotifyWaitingRequests 通知等待的重复请求
func NotifyWaitingRequests(ctx interface{}, userId int, fingerprint string, response interface{}) {
	// 查找所有等待中的相同指纹请求
	var caches []*RequestCache
	DB.Where("user_id = ? AND fingerprint = ? AND status = ? AND response_body != ''",
		userId, fingerprint, RequestCacheStatusCompleted).
		Order("completed_time DESC").
		Limit(10).
		Find(&caches)
	// 响应会通过Channel发送给等待的请求
}

// IsTimeoutRetry 判断是否为超时重试
func IsTimeoutRetry(newReqTime, oldReqTime int64) bool {
	retryInterval := newReqTime - oldReqTime
	return retryInterval > 3 && retryInterval < 120 // 3-120秒内认为是重试
}

// GetAverageRequestDuration 获取用户平均请求时长
func GetAverageRequestDuration(userId int) (int64, error) {
	var result struct {
		AvgDuration float64
	}
	err := DB.Model(&RequestCache{}).
		Select("AVG(completed_time - created_time) as avg_duration").
		Where("user_id = ? AND status = ? AND completed_time > ?",
			userId, RequestCacheStatusCompleted, helper.GetTimestamp()-3600).
		Scan(&result).Error
	if err != nil {
		return 0, err
	}
	return int64(result.AvgDuration), nil
}

// GetRequestCountInWindow 获取时间窗口内的请求数
func GetRequestCountInWindow(userId int, windowSeconds int) (int, error) {
	var count int64
	cutoff := helper.GetTimestamp() - int64(windowSeconds)
	err := DB.Model(&RequestCache{}).
		Where("user_id = ? AND created_time > ?", userId, cutoff).
		Count(&count).Error
	return int(count), err
}

// StartRequestCacheCleanupJob 启动缓存清理任务
func StartRequestCacheCleanupJob(intervalSeconds int) {
	go func() {
		ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
		for range ticker.C {
			CleanupExpiredCache()
		}
	}()
}