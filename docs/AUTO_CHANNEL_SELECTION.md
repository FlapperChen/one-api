# 自动渠道选择功能

## 概述

此功能根据请求 URL 路径自动选择合适的渠道类型：

- `/v1/messages`（Anthropic 原生格式）→ 路由到 **AnthropicCompatible** 渠道
- `/v1/chat/completions`（OpenAI 格式）→ 路由到 **OpenAI 兼容** 渠道

## 配置方式

在**令牌管理**中创建或编辑令牌时，勾选「启用自动渠道选择」选项。

## 工作原理

```
请求流程：
1. TokenAuth 中间件：验证令牌，检查是否启用了自动渠道选择
2. Distribute 中间件：
   - 如果令牌启用了自动渠道选择，根据 URL 路径检测请求格式
   - 根据检测到的格式按渠道类型筛选
   - 使用优先级/随机逻辑选择渠道
   - 如无匹配渠道，回退到无类型筛选的选择
3. Relay 控制器：使用选中的渠道处理请求
```

## 渠道类型映射

| URL 路径 | 渠道类型 | 匹配的渠道类型 |
|----------|----------|----------------|
| `/v1/messages` | Anthropic | AnthropicCompatible (type=56) |
| `/v1/chat/completions` | OpenAI | OpenAI、Custom、OpenAICompatible、GeminiOpenAICompatible |

## 回退行为

如果找不到符合格式要求的渠道：

1. 令牌绑定渠道：尝试所有绑定的渠道（忽略类型筛选）
2. 自动选择：尝试无类型筛选的选择作为回退
3. 如果仍然没有可用渠道，返回 503 错误

## 错误信息

| 场景 | 错误信息 |
|------|----------|
| 没有符合格式要求的渠道 | `当前分组 {group} 下对于模型 {model} 没有配置支持该请求格式的渠道` |
| 没有任何可用渠道 | `当前分组 {group} 下对于模型 {model} 无可用渠道` |

## 使用场景

当同一个模型配置了**多个不同类型**的渠道时，此功能可以自动根据请求格式选择对应类型的渠道。

**示例：**
- 模型 `claude-3-sonnet` 配置了两个渠道：
  - 渠道1: AnthropicCompatible
  - 渠道2: OpenAI 类型
- 使用同一令牌发送请求：
  - `/v1/messages` 请求 → 自动选择 AnthropicCompatible 渠道
  - `/v1/chat/completions` 请求 → 自动选择 OpenAI 渠道

## 测试

### 前提条件

1. 配置渠道：
   - 一个 `AnthropicCompatible` 渠道，配置模型 `claude-3-sonnet`
   - 一个 `OpenAI` 渠道，配置模型 `claude-3-sonnet`
2. 创建令牌，并勾选「启用自动渠道选择」

### 测试用例

```bash
# 测试 1：Anthropic 原生格式（应路由到 AnthropicCompatible 渠道）
curl -X POST http://localhost:3000/v1/messages \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-sonnet",
    "messages": [{"role": "user", "content": "你好"}],
    "max_tokens": 1024
  }'

# 测试 2：OpenAI 格式（应路由到 OpenAI 渠道）
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-sonnet",
    "messages": [{"role": "user", "content": "你好"}],
    "max_tokens": 1024
  }'
```

### 验证

查看服务端日志，确认渠道选择情况：

```
user id 1, user group: default, request model: claude-3-sonnet, using channel #1, type filter: 2
```

type filter 值：
- `0`: 无过滤
- `1`: OpenAI 类型
- `2`: Anthropic 类型

## 修改的文件

| 文件 | 变更 |
|------|------|
| `model/token.go` | 添加 `AutoChannelSelect` 字段和 `IsAutoChannelSelectEnabled()` 方法 |
| `common/ctxkey/key.go` | 添加 `TokenAutoChannelSelect` 上下文键 |
| `middleware/auth.go` | 在 TokenAuth 中设置自动渠道选择标记到上下文 |
| `middleware/distributor.go` | 添加格式检测、类型过滤、回退逻辑 |
| `model/cache.go` | 添加 `ChannelTypeFilter`、`IsChannelTypeMatch()`、`CacheGetRandomSatisfiedChannelByType()` |
| `model/ability.go` | 添加 `GetRandomSatisfiedChannelByType()`、`buildChannelTypeCondition()` |
| `web/default/src/pages/Token/EditToken.js` | 添加自动渠道选择开关 UI |
| `web/default/src/locales/zh/translation.json` | 添加中文翻译 |
| `web/default/src/locales/en/translation.json` | 添加英文翻译 |