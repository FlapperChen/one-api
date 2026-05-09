package model

import (
	"github.com/songquanpeng/one-api/common/helper"
	"gorm.io/gorm"
)

const (
	UserConcurrencyStatusNormal   = 1
	UserConcurrencyStatusPaused   = 2
)

type UserConcurrencyConfig struct {
	ID                  int    `json:"id" gorm:"primaryKey"`
	UserId              int    `json:"user_id" gorm:"uniqueIndex;not null"`
	MaxConcurrent       int    `json:"max_concurrent" gorm:"default:5"`
	EnableAutoLimit     bool   `json:"enable_auto_limit" gorm:"default:true"`
	Status              int    `json:"status" gorm:"default:1"`
	WaitTimeout         int    `json:"wait_timeout" gorm:"default:30"`         // 请求超时(s)
	CheckInterval       int    `json:"check_interval" gorm:"default:100"`      // 检查间隔(ms)
	CacheTTL            int    `json:"cache_ttl" gorm:"default:30"`            // 缓存TTL(s)
	EnableRequestDedup  bool   `json:"enable_request_dedup" gorm:"default:true"` // 请求去重开关
	CreatedTime         int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime         int64  `json:"updated_time" gorm:"bigint"`
}

func (UserConcurrencyConfig) TableName() string {
	return "user_concurrency_config"
}

func (u *UserConcurrencyConfig) BeforeCreate(tx *gorm.DB) error {
	u.CreatedTime = helper.GetTimestamp()
	u.UpdatedTime = helper.GetTimestamp()
	return nil
}

func (u *UserConcurrencyConfig) BeforeUpdate(tx *gorm.DB) error {
	u.UpdatedTime = helper.GetTimestamp()
	return nil
}

func GetUserConcurrencyConfig(userId int) (*UserConcurrencyConfig, error) {
	var configs []UserConcurrencyConfig
	err := DB.Where("user_id = ?", userId).Limit(1).Find(&configs).Error
	if err != nil {
		return nil, err
	}
	if len(configs) == 0 {
		return nil, nil
	}
	return &configs[0], nil
}

func GetOrCreateUserConcurrencyConfig(userId int) (*UserConcurrencyConfig, error) {
	config, err := GetUserConcurrencyConfig(userId)
	if err != nil {
		return nil, err
	}
	if config != nil {
		return config, nil
	}
	// 创建默认配置
	config = &UserConcurrencyConfig{
		UserId:         userId,
		MaxConcurrent:  5,
		EnableAutoLimit: true,
		Status:         UserConcurrencyStatusNormal,
		WaitTimeout:    30,
		CheckInterval:  100,
		CacheTTL:       30,
	}
	err = DB.Create(config).Error
	if err != nil {
		return nil, err
	}
	return config, nil
}

func UpdateUserConcurrencyConfig(config *UserConcurrencyConfig) error {
	return DB.Save(config).Error
}

func IsUserConcurrencyEnabled(userId int) bool {
	config, err := GetUserConcurrencyConfig(userId)
	if err != nil || config == nil {
		return true // 默认启用
	}
	return config.Status == UserConcurrencyStatusNormal
}