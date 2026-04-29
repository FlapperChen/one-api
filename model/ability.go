package model

import (
	"context"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/utils"
)

type Ability struct {
	Group     string `json:"group" gorm:"type:varchar(32);primaryKey;autoIncrement:false"`
	Model     string `json:"model" gorm:"primaryKey;autoIncrement:false"`
	ChannelId int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool   `json:"enabled"`
	Priority  *int64 `json:"priority" gorm:"bigint;default:0;index"`
}

func GetRandomSatisfiedChannel(group string, model string, ignoreFirstPriority bool) (*Channel, error) {
	ability := Ability{}
	groupCol := "`group`"
	trueVal := "1"
	if common.UsingPostgreSQL {
		groupCol = `"group"`
		trueVal = "true"
	}

	var err error = nil
	var channelQuery *gorm.DB
	if ignoreFirstPriority {
		channelQuery = DB.Where(groupCol+" = ? and model = ? and enabled = "+trueVal, group, model)
	} else {
		maxPrioritySubQuery := DB.Model(&Ability{}).Select("MAX(priority)").Where(groupCol+" = ? and model = ? and enabled = "+trueVal, group, model)
		channelQuery = DB.Where(groupCol+" = ? and model = ? and enabled = "+trueVal+" and priority = (?)", group, model, maxPrioritySubQuery)
	}
	if common.UsingSQLite || common.UsingPostgreSQL {
		err = channelQuery.Order("RANDOM()").First(&ability).Error
	} else {
		err = channelQuery.Order("RAND()").First(&ability).Error
	}
	if err != nil {
		return nil, err
	}
	channel := Channel{}
	channel.Id = ability.ChannelId
	err = DB.First(&channel, "id = ?", ability.ChannelId).Error
	return &channel, err
}

func GetRandomSatisfiedChannelByType(group string, model string, ignoreFirstPriority bool, typeFilter ChannelTypeFilter) (*Channel, error) {
	ability := Ability{}
	groupCol := "`group`"
	trueVal := "1"
	if common.UsingPostgreSQL {
		groupCol = `"group"`
		trueVal = "true"
	}

	var err error = nil
	var channelQuery *gorm.DB

	// Build channel type condition
	channelTypeCondition := buildChannelTypeCondition(typeFilter)

	if ignoreFirstPriority {
		channelQuery = DB.Where(groupCol+" = ? and model = ? and enabled = "+trueVal, group, model)
	} else {
		maxPrioritySubQuery := DB.Model(&Ability{}).Select("MAX(priority)").Where(groupCol+" = ? and model = ? and enabled = "+trueVal, group, model)
		channelQuery = DB.Where(groupCol+" = ? and model = ? and enabled = "+trueVal+" and priority = (?)", group, model, maxPrioritySubQuery)
	}

	if channelTypeCondition != "" {
		// Subquery to get channel ids that match the type filter
		channelIdsSubQuery := DB.Table("channels").Select("id").Where(channelTypeCondition)
		channelQuery = channelQuery.Where("channel_id IN (?)", channelIdsSubQuery)
	}

	if common.UsingSQLite || common.UsingPostgreSQL {
		err = channelQuery.Order("RANDOM()").First(&ability).Error
	} else {
		err = channelQuery.Order("RAND()").First(&ability).Error
	}
	if err != nil {
		return nil, err
	}
	channel := Channel{}
	channel.Id = ability.ChannelId
	err = DB.First(&channel, "id = ?", ability.ChannelId).Error
	return &channel, err
}

// buildChannelTypeCondition returns a SQL condition for filtering by channel type
// Channel types reference (relay/channeltype/define.go):
//   Unknown=0, OpenAI=1, API2D=2, Azure=3, CloseAI=4, OpenAISB=5, OpenAIMax=6, OhMyGPT=7,
//   Custom=8, Ails=9, AIProxy=10, PaLM=11, API2GPT=12, AIGC2D=13, Anthropic=14, Baidu=15,
//   Zhipu=16, Ali=17, Xunfei=18, AI360=19, OpenRouter=20, AIProxyLibrary=21, FastGPT=22,
//   Tencent=23, Gemini=24, Moonshot=25, Baichuan=26, Minimax=27, Mistral=28, Groq=29,
//   Ollama=30, LingYiWanWu=31, StepFun=32, AwsClaude=33, Coze=34, Cohere=35, DeepSeek=36,
//   Cloudflare=37, DeepL=38, TogetherAI=39, Doubao=40, Novita=41, VertextAI=42, Proxy=43,
//   SiliconFlow=44, XAI=45, Replicate=46, BaiduV2=47, XunfeiV2=48, AliBailian=49,
//   OpenAICompatible=50, GeminiOpenAICompatible=51, AnthropicCompatible=52, Dummy=53
func buildChannelTypeCondition(typeFilter ChannelTypeFilter) string {
	switch typeFilter {
	case ChannelTypeFilterOpenAI:
		// OpenAICompatible=50
		return "type = 50"
	case ChannelTypeFilterAnthropic:
		// AnthropicCompatible=52
		return "type = 52"
	}
	return ""
}

func (channel *Channel) AddAbilities() error {
	models_ := strings.Split(channel.Models, ",")
	models_ = utils.DeDuplication(models_)
	groups_ := strings.Split(channel.Group, ",")
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == ChannelStatusEnabled,
				Priority:  channel.Priority,
			}
			abilities = append(abilities, ability)
		}
	}
	return DB.Create(&abilities).Error
}

func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities() error {
	// A quick and dirty way to update abilities
	// First delete all abilities of this channel
	err := channel.DeleteAbilities()
	if err != nil {
		return err
	}
	// Then add new abilities
	err = channel.AddAbilities()
	if err != nil {
		return err
	}
	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	return DB.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
}

func GetGroupModels(ctx context.Context, group string) ([]string, error) {
	groupCol := "`group`"
	trueVal := "1"
	if common.UsingPostgreSQL {
		groupCol = `"group"`
		trueVal = "true"
	}
	var models []string
	err := DB.Model(&Ability{}).Distinct("model").Where(groupCol+" = ? and enabled = "+trueVal, group).Pluck("model", &models).Error
	if err != nil {
		return nil, err
	}
	sort.Strings(models)
	return models, err
}
