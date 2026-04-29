# 自动渠道选择功能

> 基于请求 URL 路径自动选择对应类型渠道的功能

## 功能概述

根据请求的 URL 路径格式，自动选择对应类型的渠道：

| URL 路径 | 选择渠道类型 | 说明 |
|----------|--------------|------|
| `/v1/messages` | AnthropicCompatible | Anthropic 原生 API 格式 |
| `/v1/chat/completions` | OpenAI 兼容类型 | OpenAI 兼容格式 |
| 其他路径 | 不进行筛选 | 使用默认选择逻辑 |

## 使用方式

在令牌管理中创建或编辑令牌时，勾选「**启用自动渠道选择**」选项。

## 应用场景

当同一个模型配置了**多个不同类型**的渠道时，此功能可以自动根据请求格式选择对应类型的渠道。

**示例配置：**
- 模型 `/models/coder/minimax/MiniMax-M2` 配置了两个渠道：
  - 渠道 1：`AnthropicCompatible` 类型
  - 渠道 2：`OpenAI` 类型
- 使用启用了自动选择的令牌发送请求：
  - 发送 `/v1/messages` → 自动选择 AnthropicCompatible 渠道
  - 发送 `/v1/chat/completions` → 自动选择 OpenAI 渠道

## 技术实现

### 请求处理流程

```
1. TokenAuth 中间件 (middleware/auth.go)
   └─ 验证令牌有效性
   └─ 检查 AutoChannelSelect 字段
   └─ 如启用，设置 ctxkey.TokenAutoChannelSelect 到上下文

2. Distribute 中间件 (middleware/distributor.go)
   └─ 检测 TokenAutoChannelSelect 标记
   └─ 如启用，调用 detectChannelTypeFromPath() 检测格式
   └─ 调用 CacheGetRandomSatisfiedChannelByType() 筛选渠道
   └─ 如无匹配，回退到无筛选选择

3. Relay 控制器
   └─ 使用选中的渠道处理请求
```

### 核心算法

**类型检测：**
```go
func detectChannelTypeFromPath(path string) ChannelTypeFilter {
    if strings.HasPrefix(path, "/v1/messages") {
        return ChannelTypeFilterAnthropic  // 2
    }
    if strings.HasPrefix(path, "/v1/chat/completions") {
        return ChannelTypeFilterOpenAI  // 1
    }
    return ChannelTypeFilterNone  // 0
}
```

**渠道类型匹配：**
```go
func IsChannelTypeMatch(channelType int, filter ChannelTypeFilter) bool {
    switch filter {
    case ChannelTypeFilterOpenAI:
        return channelType == 1 ||  // OpenAI
               channelType == 12 || // Custom
               channelType == 54 || // OpenAICompatible
               channelType == 55    // GeminiOpenAICompatible
    case ChannelTypeFilterAnthropic:
        return channelType == 56  // AnthropicCompatible
    }
    return true
}
```

## 代码变更

### 后端变更

#### 1. model/token.go
```go
// 新增字段
type Token struct {
    // ... 其他字段
    AutoChannelSelect *bool `json:"auto_channel_select" gorm:"default:false"`
}

// 新增方法
func (t *Token) IsAutoChannelSelectEnabled() bool {
    if t == nil || t.AutoChannelSelect == nil {
        return false
    }
    return *t.AutoChannelSelect
}
```

#### 2. common/ctxkey/key.go
```go
// 新增上下文键
const (
    // ... 其他键
    TokenAutoChannelSelect = "token_auto_channel_select"
)
```

#### 3. middleware/auth.go
```go
// TokenAuth 中新增
if token.IsAutoChannelSelectEnabled() {
    c.Set(ctxkey.TokenAutoChannelSelect, true)
}
```

#### 4. middleware/distributor.go
- 新增 `detectChannelTypeFromPath()` 函数
- 新增 `getRandomChannelFromListWithTypeFilter()` 函数
- 修改 `Distribute()` 主函数，集成类型筛选逻辑

#### 5. model/cache.go
```go
// 新增类型
type ChannelTypeFilter int

const (
    ChannelTypeFilterNone     ChannelTypeFilter = iota  // 0
    ChannelTypeFilterOpenAI                            // 1
    ChannelTypeFilterAnthropic                         // 2
)

// 新增函数
func IsChannelTypeMatch(channelType int, filter ChannelTypeFilter) bool
func CacheGetRandomSatisfiedChannelByType(group, model string, ignoreFirstPriority bool, typeFilter ChannelTypeFilter) (*Channel, error)
```

#### 6. model/ability.go
```go
// 新增函数
func GetRandomSatisfiedChannelByType(...) (*Channel, error)
func buildChannelTypeCondition(typeFilter ChannelTypeFilter) string
```

### 前端变更

#### 1. web/default/src/pages/Token/EditToken.js
```jsx
// 新增表单字段
<Form.Field>
  <Form.Checkbox
    label={t('token.edit.auto_channel_select')}
    name='auto_channel_select'
    checked={inputs.auto_channel_select}
    onChange={(e, { checked }) => {
      setInputs({ ...inputs, auto_channel_select: checked });
    }}
  />
  <div style={{ color: '#999', fontSize: '12px', marginTop: '5px', marginLeft: '24px' }}>
    {t('token.edit.auto_channel_select_hint')}
  </div>
</Form.Field>
```

#### 2. 翻译文件
- `web/default/src/locales/zh/translation.json` - 新增中文翻译
- `web/default/src/locales/en/translation.json` - 新增英文翻译

## 测试方法

### 前提条件

1. 配置两个渠道，都支持同一个模型（如 `/models/coder/minimax/MiniMax-M2`）：
   - 渠道 A：`AnthropicCompatible` 类型
   - 渠道 B：`OpenAI` 类型

2. 创建令牌并勾选「启用自动渠道选择」

### 测试命令

```bash
# 测试 1：Anthropic 原生格式
curl -X POST http://localhost:3000/v1/messages \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "/models/coder/minimax/MiniMax-M2",
    "messages": [{"role": "user", "content": "你好"}],
    "max_tokens": 1024
  }'

# 测试 2：OpenAI 格式
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "/models/coder/minimax/MiniMax-M2",
    "messages": [{"role": "user", "content": "你好"}],
    "max_tokens": 1024
  }'
```

### 日志验证

查看服务日志，确认路由选择：

```
# Anthropic 格式请求 - type filter: 2
user id 1, user group: default, request model: /models/coder/minimax/MiniMax-M2, using channel #1, type filter: 2

# OpenAI 格式请求 - type filter: 1
user id 1, user group: default, request model: /models/coder/minimax/MiniMax-M2, using channel #2, type filter: 1
```

### type filter 值说明

| 值 | 含义 |
|----|------|
| 0 | 无过滤（未启用或非适用路径） |
| 1 | 筛选 OpenAI 类型渠道 |
| 2 | 筛选 Anthropic 类型渠道 |

## 错误处理

| 场景 | 错误信息 |
|------|----------|
| 没有符合格式要求的渠道 | `当前分组 {group} 下对于模型 {model} 没有配置支持该请求格式的渠道` |
| 没有任何可用渠道 | `当前分组 {group} 下对于模型 {model} 无可用渠道` |

**回退机制：**
1. 如果类型筛选找不到渠道，自动回退到无筛选选择
2. 如果 Token 绑定了多个渠道，也会尝试所有绑定的渠道

## 相关文件列表

```
model/token.go                           # Token 模型扩展
common/ctxkey/key.go                     # 上下文键定义
middleware/auth.go                       # 认证中间件
middleware/distributor.go                # 渠道分发中间件
model/cache.go                           # 渠道缓存与选择
model/ability.go                         # 渠道能力查询
web/default/src/pages/Token/EditToken.js # 前端令牌编辑页
web/default/src/locales/zh/translation.json
web/default/src/locales/en/translation.json
```

## 更新日志

- **2024-04-28**: 初始实现，支持令牌级别的自动渠道选择