# 自动渠道选择功能开发总结

> 创建日期: 2026-04-29
> 功能版本: Auto Channel Selection v3

---

## 一、完成的工作

### 1. 修复渠道类型常量值不匹配问题

**问题**: 代码中硬编码的渠道类型值与 `relay/channeltype/define.go` 中的实际 `iota` 值不匹配。

**修改文件**:
- `model/cache.go` - `IsChannelTypeMatch()` 函数
- `model/ability.go` - `buildChannelTypeCondition()` 函数

**修复内容**:
| 类型 | 代码原值 | 正确值 |
|------|----------|--------|
| OpenAICompatible | 54 | 50 |
| GeminiOpenAICompatible | 55 | 51 |
| AnthropicCompatible | 56 | 52 |

### 2. 添加渠道类型备注

**目的**: 防止后续开发时再次数错 iota 值。

**修改文件**:
- `relay/channeltype/define.go` - 每行添加对应的 type 值注释

### 3. 模型配置自动启用自动渠道选择功能

**核心思路**: 复用已有的令牌级别"自动渠道选择"功能，在 auth 中间件中根据模型配置自动设置该标志。

**实现方式**:
- 在 `middleware/auth.go` 中，当令牌未手动开启自动渠道选择时，检查模型是否在配置中
- 如果模型在配置中，自动设置 `ctxkey.TokenAutoChannelSelect = true`
- distributor 原有逻辑不变，检查该标志决定是否启用类型过滤

**修改文件**:
- `model/option.go` - 添加 `AutoChannelSelectModels` 默认配置
- `middleware/auth.go` - 添加 `shouldAutoSelectForModel()`、`matchModelPattern()` 函数
- `middleware/distributor.go` - 保持原有逻辑，复用 `TokenAutoChannelSelect` 标志
- `model/channel.go` - 添加 `GetAllGroups()` 函数
- `controller/model.go` - 添加 `GetAllGroups()`、`GetGroupModels()` API
- `router/api.go` - 注册新路由
- `web/default/src/components/OperationSetting.js` - 添加分组选择、模型选择 UI
- `web/default/src/locales/zh/translation.json` - 添加中文翻译
- `web/default/src/locales/en/translation.json` - 添加英文翻译

**配置格式**:
```json
[
  {"model": "/models/coder/minimax/MiniMax-M2", "groups": ["default"]},
  {"model": "claude-", "groups": ["default", "vip"]}
]
```

---

## 二、技术细节

### 实现原理

```
请求流程:
1. TokenAuth 中间件:
   - 检查令牌是否手动开启 auto_channel_select
   - 如果没有，检查模型是否在 AutoChannelSelectModels 配置中
   - 如果在配置中，设置 ctxkey.TokenAutoChannelSelect = true
   
2. Distribute 中间件:
   - 检查 ctxkey.TokenAutoChannelSelect 是否设置
   - 如果设置，调用 detectChannelTypeFromPath() 检测请求格式
   - 根据类型过滤选择对应类型的渠道
```

### 渠道类型参考

```
Unknown=0, OpenAI=1, API2D=2, Azure=3, CloseAI=4, OpenAISB=5, OpenAIMax=6, OhMyGPT=7,
Custom=8, Ails=9, AIProxy=10, PaLM=11, API2GPT=12, AIGC2D=13, Anthropic=14, Baidu=15,
Zhipu=16, Ali=17, Xunfei=18, AI360=19, OpenRouter=20, AIProxyLibrary=21, FastGPT=22,
Tencent=23, Gemini=24, Moonshot=25, Baichuan=26, Minimax=27, Mistral=28, Groq=29,
Ollama=30, LingYiWanWu=31, StepFun=32, AwsClaude=33, Coze=34, Cohere=35, DeepSeek=36,
Cloudflare=37, DeepL=38, TogetherAI=39, Doubao=40, Novita=41, VertextAI=42, Proxy=43,
SiliconFlow=44, XAI=45, Replicate=46, BaiduV2=47, XunfeiV2=48, AliBailian=49,
OpenAICompatible=50, GeminiOpenAICompatible=51, AnthropicCompatible=52, Dummy=53
```

### URL 路径映射

| URL 路径 | 渠道类型过滤 | 匹配渠道 |
|----------|-------------|----------|
| `/v1/chat/completions` | ChannelTypeFilterOpenAI (1) | type = 50 (OpenAICompatible) |
| `/v1/messages` | ChannelTypeFilterAnthropic (2) | type = 52 (AnthropicCompatible) |

---

## 三、待完成工作

1. **测试验证**
   - 确认 OpenAI 格式 `/v1/chat/completions` 路由到 type=50 渠道
   - 确认 Anthropic 格式 `/v1/messages` 路由到 type=52 渠道
   - 确认令牌绑定渠道时的行为
   - 确认回退机制正常工作

2. **文档更新**
   - 更新 `docs/AUTO_CHANNEL_SELECTION.md`

---

## 四、相关文件列表

### 后端修改
```
model/cache.go              - IsChannelTypeMatch, CacheGetRandomSatisfiedChannelByType
model/ability.go            - GetRandomSatisfiedChannelByType, buildChannelTypeCondition
model/option.go             - AutoChannelSelectModels 配置
model/channel.go            - GetAllGroups
middleware/auth.go          - shouldAutoSelectForModel, matchModelPattern
middleware/distributor.go   - 保持原有逻辑
controller/model.go         - GetAllGroups, GetGroupModels API
router/api.go               - 注册新路由
relay/channeltype/define.go - 添加类型值备注
```

### 前端修改
```
web/default/src/components/OperationSetting.js - 自动渠道选择配置 UI
web/default/src/locales/zh/translation.json - 中文翻译
web/default/src/locales/en/translation.json - 英文翻译
```