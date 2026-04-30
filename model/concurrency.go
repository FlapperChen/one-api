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
	ID               int    `json:"id" gorm:"primaryKey"`
	UserId           int    `json:"user_id" gorm:"uniqueIndex;not null"`
	MaxConcurrent    int    `json:"max_concurrent" gorm:"default:5"`
	EnableAutoLimit  bool   `json:"enable_auto_limit" gorm:"default:true"`
	Status           int    `json:"status" gorm:"default:1"`
	CreatedTime      int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime      int64  `json:"updated_time" gorm:"bigint"`
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
	var config UserConcurrencyConfig
	err := DB.First(&config, "user_id = ?", userId).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
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
		UserId:          userId,
		MaxConcurrent:   5,
		EnableAutoLimit: true,
		Status:          UserConcurrencyStatusNormal,
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