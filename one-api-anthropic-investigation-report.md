# One API Anthropic 渠道调研报告

## 问题描述

Claude Code 无法通过 One API 访问本地部署的 vLLM 模型，错误信息：
```
There's an issue with the selected model (/models/coder/minimax/MiniMax-M2). It may not exist or you may not have access to it.
```

## 测试结果对比

| 测试 | 请求路径 | 结果 |
|------|---------|------|
| 直接访问 vLLM | `POST /v1/messages` | ✅ HTTP 200 成功 |
| 通过 One API | `POST /v1/chat/completions` | ✅ HTTP 200 成功 |
| 通过 One API | `POST /v1/messages` | ❌ HTTP 404 Not Found |

## 根本原因

**One API 不支持 Anthropic 格式的 `/v1/messages` 端点**

- One API 接收端只实现了 OpenAI 格式的 `/v1/chat/completions`
- Claude Code 默认发送 Anthropic 格式的 `/v1/messages` 请求
- 当 Claude Code 发送 `/v1/messages` 时，One API 返回 404

## One API Anthropic 渠道工作原理

### 架构流程

```
Claude Code (Anthropic 格式)
    ↓
One API 接收 /v1/chat/completions (OpenAI 格式)
    ↓
根据模型选择渠道 (Type = Anthropic)
    ↓
Anthropic 适配器转换为 Anthropic 格式
    ↓
发送到上游 {BaseURL}/v1/messages
    ↓
vLLM 返回 Anthropic 格式响应
    ↓
转换回 OpenAI 格式返回给客户端
```

### 接收格式（入口）- 必须是 OpenAI 格式

```http
POST /v1/chat/completions
Authorization: Bearer {token}
Content-Type: application/json

{
  "model": "/models/coder/minimax/MiniMax-M2",
  "messages": [{"role": "user", "content": "hello"}],
  "max_tokens": 1024
}
```

### 发送格式（上游）- Anthropic 格式

```http
POST {BaseURL}/v1/messages
x-api-key: {channel_key}
anthropic-version: 2023-06-01
Content-Type: application/json

{
  "model": "/models/coder/minimax/MiniMax-M2",
  "messages": [{"role": "user", "content": [{"type": "text", "text": "hello"}]}],
  "max_tokens": 1024
}
```

### 返回格式 - 转换为 OpenAI 格式

```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "model": "/models/coder/minimax/MiniMax-M2",
  "choices": [{
    "message": {"role": "assistant", "content": "Hello!"},
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30}
}
```

## 渠道配置信息

根据用户提供的配置：

| 字段 | 值 |
|------|-----|
| 类型 (Type) | Anthropic (18) |
| Base URL | `http://172.17.167.240:8000` |
| 密钥 (Key) | `sk-SUD87giSLjuXJ4KuFfD752E62a2e453c95CbE04d0f769482` |
| 模型 (Models) | `MiniMax-M2-claude` |

## 关键代码位置

1. **路由定义**: `router/relay.go:17` - 只定义了 `/v1/chat/completions`
2. **适配器选择**: `relay/adaptor.go:33-34` - Anthropic 适配器
3. **请求转换**: `relay/adaptor/anthropic/main.go:39` - OpenAI→Anthropic 格式转换
4. **URL 构建**: `relay/adaptor/anthropic/adaptor.go:24` - `{BaseURL}/v1/messages`
5. **响应转换**: `relay/adaptor/anthropic/main.go:210` - Anthropic→OpenAI 格式转换

## 解决方案

### 方案 1：让 Claude Code 使用 OpenAI 格式（推荐）

修改 `~/.claude/settings.json`：

```json
{
  "env": {
    "OPENAI_API_KEY": "sk-SUD87giSLjuXJ4KuFfD752E62a2e453c95CbE04d0f769482",
    "OPENAI_API_BASE": "http://10.31.133.114:3009/v1"
  }
}
```

启动时使用 `--openai` 标志：
```bash
claude --openai
```

### 方案 2：在 One API 中添加 `/v1/messages` 支持

需要修改代码：
1. 在 `router/relay.go` 添加 `/v1/messages` 路由
2. 实现 Anthropic 格式请求的接收和转换逻辑
3. 使用现有的 Anthropic 适配器进行转换

此方案需要更多开发工作。

## 结论

One API 的 Anthropic 渠道**功能是正常的**，它能够：
- ✅ 接收 OpenAI 格式的 `/v1/chat/completions` 请求
- ✅ 转换为 Anthropic 格式发送到上游 vLLM
- ✅ 将响应转换回 OpenAI 格式返回

**问题是**：One API 不接收 Anthropic 格式的 `/v1/messages` 请求，这是设计上的限制。要让 Claude Code 工作，需要让 Claude Code 使用 OpenAI 格式访问 One API。

---

## 附录：测试用的 curl 命令

### 直接访问 vLLM（成功）

```bash
curl -X POST http://172.17.167.240:8000/v1/messages \
  -H "Authorization: Bearer test" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "/models/coder/minimax/MiniMax-M2",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

### 通过 One API 访问（成功）

```bash
curl -X POST http://10.31.133.114:3009/v1/chat/completions \
  -H "Authorization: Bearer sk-SUD87giSLjuXJ4KuFfD752E62a2e453c95CbE04d0f769482" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "/models/coder/minimax/MiniMax-M2",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

### 通过 One API 访问 /v1/messages（失败）

```bash
curl -X POST http://10.31.133.114:3009/v1/messages \
  -H "Authorization: Bearer sk-SUD87giSLjuXJ4KuFfD752E62a2e453c95CbE04d0f769482" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "/models/coder/minimax/MiniMax-M2",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

返回结果：HTTP 404 Not Found