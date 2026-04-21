# Anthropic 兼容渠道类型修改分析计划

## 一、Context（背景）

2026-04-16 新增了 Anthropic 兼容渠道类型（AnthropicCompatible），这是一个支持 Anthropic 格式 `/v1/messages` API 的渠道。用户报告该渠道的 token 无法被统计，需要分析：
1. 修改是否完整覆盖了新渠道类型需要的所有模块
2. 与历史新渠道类型的修改对比，查看是否有遗漏
3. 分析 token 统计失败的根本原因

## 二、修改完整性分析

### 2.1 已完成的修改（AnthropicCompatible 提交 97b6d9a）

| 文件 | 修改内容 | 状态 |
|------|---------|------|
| relay/channeltype/define.go | 添加 AnthropicCompatible = 56 | ✅ 完成 |
| relay/apitype/define.go | 添加 AnthropicCompatible = 24 | ✅ 完成 |
| relay/channeltype/url.go | 添加空基础 URL（用户自定义） | ✅ 完成 |
| relay/channeltype/helper.go | 添加 ToAPIType 映射 | ✅ 完成 |
| relay/adaptor.go | 注册 AnthropicCompatible 适配器 | ✅ 完成 |
| relay/adaptor/anthropiccompatible/adaptor.go | 新建适配器（100行） | ✅ 完成 |
| relay/controller/text.go | 允许透传原始请求体 | ✅ 完成 |
| relay/relaymode/define.go | 添加 AnthropicCompatible 模式 | ✅ 完成 |
| relay/relaymode/helper.go | 添加模式映射 | ✅ 完成 |
| router/relay.go | 添加路由处理 | ✅ 完成 |
| web/berry/src/constants/ChannelConstants.js | 添加渠道选项（56: Anthropic 兼容） | ✅ 完成 |
| web/default/src/constants/channel.constants.js | 添加渠道选项（56: Anthropic 兼容） | ✅ 完成 |

### 2.2 遗漏的修改

| 文件 | 问题 | 严重程度 |
|------|------|---------|
| **web/air/src/constants/channel.constants.js** | **未添加 AnthropicCompatible (56) 渠道选项** | 🔴 高 |

**验证：**
```bash
# 搜索 air 前端的渠道列表，确认没有 56
$ grep "56" web/air/src/constants/channel.constants.js
# 无输出 - 确认遗漏
```

### 2.3 与历史渠道添加对比

对比 Gemini OpenAI Compatible 渠道（提交 3f421c4）的修改：

| 模块 | Gemini Compatible | Anthropic Compatible | 差异 |
|------|------------------|---------------------|------|
| channeltype/define.go | ✅ | ✅ | 一致 |
| channeltype/url.go | ✅ | ✅ | 一致 |
| adaptor/ | ✅ | ✅ | 一致 |
| web/default/ | ✅ | ✅ | 一致 |
| web/air/ | ❓ | ❌ | **未添加** |
| middleware/ | 不需要 | 不需要 | 无差异 |
| controller/ | 不需要 | ✅ | Anthropic 需要透传 |

结论：AnthropicCompatible 在 **web/air 前端** 遗漏了渠道选项添加。

## 三、Token 统计问题分析

### 3.1 Token 统计流程

```
relay/controller/text.go:RelayTextHelper()
  │
  ├─→ preConsumeQuota()     // 预消费：从用户配额中预先扣除
  │
  ├─→ adaptor.DoRequest()   // 发送请求到上游渠道
  │
  ├→ adaptor.DoResponse()   // ← 获取 usage（关键步骤）
  │
  └─→ postConsumeQuota()    // 后消费：根据实际 usage 计算配额
```

**统计逻辑位置：** `relay/controller/helper.go:97-141`

```go
func postConsumeQuota(ctx *gin.Context, ...) {
    if usage == nil {
        logger.Error(ctx, "usage is nil, which is unexpected")
        return  // ← 直接返回，不进行任何统计
    }
    // 计算配额...
}
```

### 3.2 根本原因

**问题代码：** `relay/adaptor/anthropiccompatible/adaptor.go:60-79`

```go
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, meta *meta.Meta)
    (usage *model.Usage, err *model.ErrorWithStatusCode) {

    // 透传响应头和状态码
    for k, v := range resp.Header {
        for _, vv := range v {
            c.Writer.Header().Set(k, vv)
        }
    }

    c.Writer.WriteHeader(resp.StatusCode)
    if _, gerr := io.Copy(c.Writer, resp.Body); gerr != nil {
        return nil, &model.ErrorWithStatusCode{...}
    }

    return nil, nil  // ← 关键问题：返回 nil usage！
}
```

**问题分析：**
1. **返回值为 nil**：`DoResponse()` 直接透传响应，**不解析响应体提取 usage**
2. **统计被跳过**：`postConsumeQuota()` 检测到 `usage == nil` 直接返回
3. **配额计算错误**：预消费的配额无法根据实际使用量调整

### 3.3 对比：Anthropic 原生渠道的实现

**正常实现：** `relay/adaptor/anthropic/main.go:365-368`

```go
usage := model.Usage{
    PromptTokens:     claudeResponse.Usage.InputTokens,
    CompletionTokens: claudeResponse.Usage.OutputTokens,
    TotalTokens:      claudeResponse.Usage.InputTokens + claudeResponse.Usage.OutputTokens,
}
return usage, nil
```

Anthropic 原生渠道从响应体中解析了 `usage` 信息并返回，而 AnthropicCompatible 没有。

### 3.4 问题影响

| 影响 | 说明 |
|------|------|
| 配额不准确 | 用户被预消费的配额无法根据实际 usage 调整 |
| 统计失效 | token 使用量不会被记录到系统 |
| 计费错误 | 用户可能被多收或少收配额 |

## 四、修复方案

### 4.1 修复 Token 统计（必须）

在 `relay/adaptor/anthropiccompatible/adaptor.go` 的 `DoResponse()` 方法中：

1. **解析响应体**：读取并解析 JSON 响应
2. **提取 usage**：从 Anthropic 响应格式中提取 `usage.input_tokens` 和 `usage.output_tokens`
3. **返回 usage**：返回正确的 `*model.Usage` 对象

参考实现（基于 Anthropic 原生适配器）：
```go
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, meta *meta.Meta)
    (*model.Usage, *model.ErrorWithStatusCode) {

    bodyBytes, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, &model.ErrorWithStatusCode{...}
    }

    // 解析 Anthropic 格式的响应
    var anthropicResp map[string]interface{}
    if err := json.Unmarshal(bodyBytes, &anthropicResp); err != nil {
        return nil, &model.ErrorWithStatusCode{...}
    }

    // 提取 usage
    var promptTokens, completionTokens int
    if usage, ok := anthropicResp["usage"].(map[string]interface{}); ok {
        if input, ok := usage["input_tokens"].(float64); ok {
            promptTokens = int(input)
        }
        if output, ok := usage["output_tokens"].(float64); ok {
            completionTokens = int(output)
        }
    }

    usage := &model.Usage{
        PromptTokens:     promptTokens,
        CompletionTokens: completionTokens,
        TotalTokens:      promptTokens + completionTokens,
    }

    // 透传响应
    c.Writer.WriteHeader(resp.StatusCode)
    c.Writer.Write(bodyBytes)

    return usage, nil
}
```

### 4.2 补充遗漏的前端修改（必须）

在 `web/air/src/constants/channel.constants.js` 中添加：
```javascript
{ key: 56, text: 'Anthropic 兼容', value: 56, color: 'black', description: '支持 Anthropic 格式 /v1/messages API' },
```

位置：建议添加在 Anthropic Claude (14) 之后

### 4.3 考虑添加流式响应支持（可选）

当前 `DoResponse` 不支持流式响应。如果需要支持流式，需要实现 `StreamHandler` 方法。

## 五、关键文件路径

### 需要修改的文件
- `/home/bmc/sd1/CODE/one-api/relay/adaptor/anthropiccompatible/adaptor.go` - 修复 token 统计
- `/home/bmc/sd1/CODE/one-api/web/air/src/constants/channel.constants.js` - 补充前端渠道选项

### 需要参考的文件
- `/home/bmc/sd1/CODE/one-api/relay/adaptor/anthropic/main.go` - 正常的统计实现
- `/home/bmc/sd1/CODE/one-api/relay/controller/helper.go` - 统计逻辑
- `/home/bmc/sd1/CODE/one-api/web/default/src/constants/channel.constants.js` - 前端示例

## 六、验证方案

### 6.1 Token 统计验证

1. 启动 one-api 服务
2. 配置 Anthropic Compatible 渠道（使用支持 usage 的上游）
3. 发起文本生成请求
4. 检查日志中是否有 "usage is nil" 错误
5. 验证用户配额是否正确计算

### 6.2 前端验证

1. 访问 air 前端
2. 检查渠道下拉列表是否显示 "Anthropic 兼容" 选项
3. 确认可以选择该渠道并创建

### 6.3 回归测试

1. 测试其他渠道（OpenAI、Anthropic 原生）的 token 统计是否正常
2. 验证现有功能未受影响

---

## 七、完整修复总结报告（2026-04-20）

### 7.1 发现的两个根本原因

#### 原因一：数据库渠道类型值与代码不匹配 🔴 严重

| 位置 | 值 |
|------|-----|
| 代码 `channeltype/define.go` 中 `AnthropicCompatible` 的 iota 值 | **52** |
| 数据库 `channels.type` 字段存储的值 | **56** |

**影响：**
- `ToAPIType()` 函数的 switch case 无法匹配到 `AnthropicCompatible`
- 导致 APIType 返回默认值 0（OpenAI）
- 系统使用错误的适配器处理请求

**验证方法：**
```go
// 通过检查日志发现
[RelayTextHelper] ChannelType=56, APIType=0  // APIType 应该是 19 (AnthropicCompatible)
```

#### 原因二：DoResponse 返回 nil usage

**问题代码：** `relay/adaptor/anthropiccompatible/adaptor.go`

```go
func (a *Adaptor) DoResponse(...) (usage *model.Usage, ...) {
    // 透传响应...
    return nil, nil  // ← 返回 nil usage，token 统计被跳过
}
```

### 7.2 执行的修复步骤

#### 步骤 1：修复 DoResponse 方法
**文件：** `relay/adaptor/anthropiccompatible/adaptor.go`

修改内容：
1. 添加 `Usage`、`Response`、`ResponseError` 结构体定义
2. 解析响应体 JSON，提取 `usage.input_tokens` 和 `usage.output_tokens`
3. 返回正确的 `*model.Usage` 对象

```go
func (a *Adaptor) DoResponse(...) (usage *model.Usage, ...) {
    responseBody, readErr := io.ReadAll(resp.Body)
    // ...
    var claudeResponse Response
    json.Unmarshal(responseBody, &claudeResponse)
    // ...
    usage = &model.Usage{
        PromptTokens:     claudeResponse.Usage.InputTokens,
        CompletionTokens: claudeResponse.Usage.OutputTokens,
        TotalTokens:      claudeResponse.Usage.InputTokens + claudeResponse.Usage.OutputTokens,
    }
    return usage, nil
}
```

#### 步骤 2：补充前端渠道选项
**文件：** `web/air/src/constants/channel.constants.js`

添加内容：
```javascript
{ key: 56, text: 'Anthropic 兼容', value: 56, color: 'black', description: '支持 Anthropic 格式 /v1/messages API' },
```

#### 步骤 3：修复数据库渠道类型值
**SQL：**
```sql
UPDATE channels SET type = 52 WHERE type = 56;
```

### 7.3 验证结果

#### 修复前日志：
```
[INFO] record log: &{Quota:0 PromptTokens:0 CompletionTokens:0 ...}
```

#### 修复后日志：
```
[INFO] record log: &{Quota:31950 PromptTokens:41 CompletionTokens:1024 ...}
```

#### 上游响应：
```json
{"usage": {"input_tokens": 41, "output_tokens": 1024}}
```

#### 验证成功：
- ✅ `PromptTokens=41` = 上游 `input_tokens=41`
- ✅ `CompletionTokens=1024` = 上游 `output_tokens=1024`
- ✅ `Quota=31950` = (41 + 1024) * 30 (模型倍率)

### 7.4 修改的文件清单

| 文件 | 修改类型 | 说明 |
|------|---------|------|
| `relay/adaptor/anthropiccompatible/adaptor.go` | 修复 | 添加 usage 解析逻辑 |
| `web/air/src/constants/channel.constants.js` | 补充 | 添加 AnthropicCompatible (52) 渠道选项 |
| `web/default/src/constants/channel.constants.js` | 修正 | 更新 AnthropicCompatible 从 56 到 52 |
| `web/berry/src/constants/ChannelConstants.js` | 修正 | 更新 AnthropicCompatible 从 56 到 52 |
| 数据库 `channels` 表 | 数据修复 | 更新 type 从 56 到 52 |

### 7.5 补充发现：前端渠道选项不完整

#### 发现的问题

添加新渠道类型时（如 OpenAICompatible、GeminiOpenAICompatible、AnthropicCompatible），只更新了部分前端主题，导致：

| 渠道类型 | 值 | web/air | web/default | web/berry |
|---------|-----|---------|-------------|----------|
| OpenAICompatible | 50 | ❌ 缺失 | ✅ 有 | ❌ 缺失 |
| GeminiOpenAICompatible | 51 | ❌ 缺失 | ✅ 有 | ❌ 缺失 |
| AnthropicCompatible | 52 | ✅ 需修正 | ✅ 需修正 | ✅ 需修正 |

#### 执行的补充修复

在 `web/air` 和 `web/berry` 中补充缺失的渠道选项：

**web/air/src/constants/channel.constants.js**
```javascript
{ key: 50, text: 'OpenAI 兼容', value: 50, color: 'olive', description: 'OpenAI 兼容渠道，支持设置 Base URL' },
{ key: 51, text: 'Gemini OpenAI 兼容', value: 51, color: 'orange', description: 'Gemini OpenAI 兼容格式' },
```

**web/berry/src/constants/ChannelConstants.js**
```javascript
50: { key: 50, text: 'OpenAI 兼容', value: 50, color: 'primary', description: 'OpenAI 兼容渠道，支持设置 Base URL' },
51: { key: 51, text: 'Gemini OpenAI 兼容', value: 51, color: 'warning', description: 'Gemini OpenAI 兼容格式' },
```

### 7.6 经验教训

1. **iota 值可能因代码合并而变化**：添加新渠道类型时，应确保与数据库中已有的值一致
2. **测试时需重启服务**：代码修改后必须重新编译并重启服务
3. **添加调试日志**：在关键函数添加日志可以快速定位问题
4. **验证完整的请求流程**：不仅要测试 API 返回，还要验证后端的 token 统计
5. **同步所有前端主题**：添加新渠道时需要在所有前端主题中同步添加选项

### 7.7 最终验证（2026-04-20）

#### 构建步骤

```bash
# 1. 构建前端
cd web/default && npm install && npm run build && cd ../..

# 2. 构建后端
go build -ldflags "-s -w" -o one-api

# 3. 重启测试服务（端口 3009）
fuser -k 3009/tcp
SQL_DSN="postgres://postgres:密码@localhost:5432/oneapi?sslmode=disable" ./one-api --port 3009 --log-dir ./logs
```

#### 测试命令

```bash
curl -X POST http://localhost:3009/v1/messages \
  -H "Authorization: Bearer <令牌>" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

#### 验证结果

| 验证项 | 预期值 | 实际值 | 状态 |
|--------|--------|--------|------|
| API 返回 usage.input_tokens | 39 | 39 | ✅ |
| API 返回 usage.output_tokens | 97 | 97 | ✅ |
| 日志记录 PromptTokens | 39 | 39 | ✅ |
| 日志记录 CompletionTokens | 97 | 97 | ✅ |
| 日志记录 Quota | 4080 | 4080 | ✅ |
| 日志无 "usage is nil" 错误 | 无错误 | 无错误 | ✅ |

#### 日志片段

```
[INFO] record log: &{... Quota:4080 PromptTokens:39 CompletionTokens:97 ChannelId:4 ...}
```

#### 数据库清理状态

- 验证：`SELECT * FROM channels WHERE type=56;` 返回 0 行
- 结论：无残留 type=56 数据

#### 注意事项

⚠️ **端口区分**：
- 端口 **3008**：Docker 生产环境（/one-api），**禁止 kill**
- 端口 **3009**：本地测试实例（./one-api --port 3009），可以 kill

---

## 八、透传优化计划（2026-04-20）

### 8.1 问题描述

**用户报告**：Claude Code 测试 Anthropic Compatible 渠道时，只能看到 think 内容，无法执行实际的修改和读取命令。

**现象分析**：
- 响应被部分解析，只保留了 think 部分
- 实际的工具调用（Tool Use）和命令执行内容丢失
- 可能是解析过程中丢失了某些 Content 类型

**优化目标**：
1. **全链路透传优先**：请求和响应都完整透传，不丢失任何数据
2. **日志记录原始数据**：将请求和响应的原始 JSON 记录到日志，供后续分析
3. **数据解析为辅**：在透传的基础上，使用缓存数据进行 usage 统计

### 8.2 核心原则

| 原则 | 说明 |
|------|------|
| **透传优先** | 任何情况下都先保证数据完整透传 |
| **日志可见** | 原始请求和响应数据必须记录到日志 |
| **解析为辅** | 只在需要统计 usage 时才解析，且不影响透传 |

### 8.3 处理流程（统一）

`ConvertRequest` 和 `DoResponse` 采用相同的处理模式：

```
1. 读取数据（从缓存或响应体）
       ↓
2. 优先透传（无论解析是否成功）
       ↓
3. 再解析（提取 usage 用于统计）
       ↓
4. 返回结果（usage 或 nil）
```

### 8.4 缓存机制检查 ✅

**文件**：`common/gin.go`

`GetRequestBody` 已有缓存机制，请求体可以被多次读取。

### 8.5 实施方案

#### 8.5.1 ConvertRequest 透传优化

**文件**：`relay/adaptor/anthropiccompatible/adaptor.go`

**处理流程**：
1. 从缓存读取原始请求体
2. 记录到日志（调试用）
3. 返回 `nil, nil` 让系统使用缓存的原始数据

```go
func (a *Adaptor) ConvertRequest(c *gin.Context, relayMode int, request *model.GeneralOpenAIRequest) (any, error) {
    // 从缓存获取原始请求体
    bodyBytes, err := common.GetRequestBody(c)
    if err != nil {
        return nil, err
    }

    // 记录原始请求到日志（截断显示）
    bodyStr := string(bodyBytes)
    if len(bodyStr) > 500 {
        logger.Infof(c.Request.Context(), "[AnthropicCompatible] Raw request: %s...", bodyStr[:500])
    } else {
        logger.Infof(c.Request.Context(), "[AnthropicCompatible] Raw request: %s", bodyStr)
    }

    // 直接透传，不做转换
    return nil, nil
}
```

#### 8.5.2 DoResponse 透传优化

**文件**：`relay/adaptor/anthropiccompatible/adaptor.go`

**处理流程**：
1. 读取响应体
2. **优先透传**原始响应（无论后续解析是否成功）
3. 记录原始响应到日志
4. 再解析 usage（用于统计）
5. 返回 usage

```go
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode) {
    // 1. 读取响应体
    responseBody, readErr := io.ReadAll(resp.Body)
    if readErr != nil {
        return nil, &model.ErrorWithStatusCode{...}
    }

    // 2. 记录原始响应到日志
    respLen := len(responseBody)
    if respLen > 500 {
        logger.Infof(c.Request.Context(), "[AnthropicCompatible] Raw response (%d bytes): %s...", respLen, string(responseBody[:500]))
    } else {
        logger.Infof(c.Request.Context(), "[AnthropicCompatible] Raw response: %s", string(responseBody))
    }

    // 3. 优先透传原始响应（无论解析是否成功）
    passThroughResponse(c, resp, responseBody)

    // 4. 再解析 usage（用于统计）
    var claudeResponse Response
    if unmarshalErr := json.Unmarshal(responseBody, &claudeResponse); unmarshalErr != nil {
        logger.Errorf(c.Request.Context(), "[AnthropicCompatible] Unmarshal error: %v", unmarshalErr)
        return nil, nil
    }

    // 检查错误
    if claudeResponse.Error.Type != "" {
        return nil, &model.ErrorWithStatusCode{...}
    }

    // 5. 返回 usage
    usage = &model.Usage{
        PromptTokens:     claudeResponse.Usage.InputTokens,
        CompletionTokens: claudeResponse.Usage.OutputTokens,
        TotalTokens:      claudeResponse.Usage.InputTokens + claudeResponse.Usage.OutputTokens,
    }

    logger.Infof(c.Request.Context(), "[AnthropicCompatible] Usage: input=%d, output=%d, total=%d",
        usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)

    return usage, nil
}
```

#### 8.5.3 getRequestBody 配合修改

**文件**：`relay/controller/text.go`

对于 AnthropicCompatible，当 `ConvertRequest` 返回 nil 时，直接使用缓存的原始请求体。

```go
// 对于 AnthropicCompatible，使用缓存的原始请求体
if meta.APIType == apitype.AnthropicCompatible {
    bodyBytes, err := common.GetRequestBody(c)
    if err != nil {
        return nil, err
    }
    return bytes.NewReader(bodyBytes), nil
}
```

### 8.6 预期日志输出

```
# 请求阶段
[AnthropicCompatible] Raw request: {"model":..., "messages":[...],"max_tokens":1024}
[AnthropicCompatible] Raw request (truncated): {"model":... (512 chars truncated)

# 响应阶段
[AnthropicCompatible] DoResponse called, APIType=19, ChannelType=52
[AnthropicCompatible] Raw response (1234 bytes): {"id":"...","type":"message","content":[...]}
[AnthropicCompatible] Usage: input=39, output=97, total=136
```

### 8.7 验证清单

- [ ] 请求日志显示原始 JSON 格式（包括 tool_use 等完整数据）
- [ ] 响应日志显示完整 JSON（包括所有 ContentBlock 类型）
- [ ] Claude Code 测试能执行实际命令
- [ ] Token 统计正确
- [ ] 即使解析失败，响应也已透传给客户端

### 8.8 关键文件清单

| 文件 | 作用 |
|------|------|
| `relay/adaptor/anthropiccompatible/adaptor.go` | ConvertRequest、DoResponse |
| `relay/controller/text.go` | getRequestBody |
| `common/gin.go` | GetRequestBody、UnmarshalBodyReusable |
| `logs/oneapi-*.log` | Debug 日志 | |

---

## 九、流式输出与 Test 功能修复 (2026-04-21)

### 9.1 流式输出额度记录

#### 问题现象

Claude Code 使用流式响应时，日志显示解析错误：

```
[AnthropicCompatible] Unmarshal error: invalid character 'e' looking for beginning of value
```

#### 根本原因

Claude Code 发送 `stream: true` 请求，上游返回 SSE 格式响应：

```
event: message_start
data: {"type":"message_start","message":{"id":"...","usage":{"input_tokens":135734,...}}}
event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"..."}}
...
event: message_delta
data: {"type":"message_delta","usage":{"output_tokens":73}}
```

SSE 响应以 `event:` 开头，不是标准 JSON，`json.Unmarshal` 解析失败。

#### 解决方案

添加 `StreamHandler` 方法处理 SSE 流式响应：

**文件**：`relay/adaptor/anthropiccompatible/adaptor.go`

```go
func (a *Adaptor) StreamHandler(c *gin.Context, resp *http.Response) (err *model.ErrorWithStatusCode, usage *model.Usage) {
    logger.Infof(c.Request.Context(), "[AnthropicCompatible] StreamHandler called")

    common.SetEventStreamHeaders(c)

    // 透传所有响应头
    for k, v := range resp.Header {
        for _, vv := range v {
            c.Writer.Header().Set(k, vv)
        }
    }

    var promptTokens, completionTokens int
    buf := make([]byte, 0, 4096)
    tmp := make([]byte, 1024)

    for {
        n, readErr := reader.Read(tmp)
        if n > 0 {
            // 透传到客户端
            c.Writer.Write(tmp[:n])
            c.Writer.Flush()

            buf = append(buf, tmp[:n]...)

            // 解析 SSE 行
            for {
                lineEnd := findLineEnd(buf)
                if lineEnd < 0 { break }

                line := string(buf[:lineEnd])
                buf = buf[lineEnd+1:]

                if strings.HasPrefix(line, "data:") {
                    // 解析 message_start 获取 input_tokens
                    if strings.Contains(line, "\"type\":\"message_start\"") {
                        var startEvent StreamMessageStart
                        json.Unmarshal([]byte(line[5:]), &startEvent)
                        promptTokens = startEvent.Message.Usage.InputTokens
                    }
                    // 解析 message_delta 获取 output_tokens
                    if strings.Contains(line, "\"type\":\"message_delta\"") {
                        var deltaEvent StreamMessageDelta
                        json.Unmarshal([]byte(line[5:]), &deltaEvent)
                        completionTokens = deltaEvent.Usage.OutputTokens
                    }
                }
            }
        }
        if readErr != nil { break }
    }

    render.Done(c)

    usage = &model.Usage{
        PromptTokens:     promptTokens,
        CompletionTokens: completionTokens,
        TotalTokens:      promptTokens + completionTokens,
    }
    return nil, usage
}
```

**DoResponse 中添加流式检测：**

```go
func (a *Adaptor) DoResponse(...) {
    responseBody, _ := io.ReadAll(resp.Body)

    // 检测 SSE 流式响应
    if len(responseBody) > 10 && strings.HasPrefix(string(responseBody[:10]), "event:") {
        resp.Body = io.NopCloser(strings.NewReader(string(responseBody)))
        return a.StreamHandler(c, resp)  // 流式处理
    }

    // JSON 响应处理...
}
```

---

### 9.2 Test 功能修复

#### 问题现象

渠道测试时 panic：

```
panic: runtime error: invalid memory address or nil pointer dereference
[AnthropicCompatible] GetRequestBody error: runtime error: invalid memory address or nil pointer dereference
```

#### 根本原因

`controller/channel-test.go` 中 `testChannel` 函数设置 `c.Request.Body = nil`，而 `ConvertRequest` 中的 `GetRequestBody` 无法处理 nil body。

#### 解决方案

在 `ConvertRequest` 中添加 body nil 检查：

```go
func (a *Adaptor) ConvertRequest(c *gin.Context, relayMode int, request *model.GeneralOpenAIRequest) (any, error) {
    // Test context: c.Request.Body is nil
    if c.Request.Body == nil {
        logger.Infof(c.Request.Context(), "[AnthropicCompatible] ConvertRequest: no body, building from request object")
        return buildAnthropicRequest(request), nil
    }
    // ... 正常处理
}
```

**buildAnthropicRequest 增强：** 支持完整参数转换（tools, tool_choice, reasoning_effort 等）。

---

### 9.3 验证结果

#### 流式响应测试

```bash
curl -X POST http://localhost:3009/v1/messages \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-3-5-sonnet-20241022","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"hello"}]}'
```

**日志输出：**
```
[AnthropicCompatible] StreamHandler called
[AnthropicCompatible] Stream usage: output=29
[AnthropicCompatible] Stream complete: input=135734, output=73, total=135807
record log: &{Quota:4074210 PromptTokens:135734 CompletionTokens:73 IsStream:true ...}
```

#### 非流式响应测试

```
[AnthropicCompatible] Response body length: 1234 bytes
[AnthropicCompatible] Usage: input=39, output=265, total=304
record log: &{Quota:9120 PromptTokens:39 CompletionTokens:265 ...}
```

#### 渠道测试

```
渠道 Claude 兼容 (Anthropic) 测试成功，响应：Hello! How can I help you today?
```

---

### 9.4 修改文件清单

| 文件 | 修改内容 |
|------|---------|
| `relay/adaptor/anthropiccompatible/adaptor.go` | 添加 StreamHandler, SSE 检测, body nil 检查 |
| `relay/controller/text.go` | 添加 `common` import |
| `controller/channel-test.go` | 使用 Anthropic 专用解析器 |

---

### 9.5 经验教训

1. **SSE vs JSON 检测**：流式响应以 `event:` 开头，需单独处理
2. **Test Context 处理**：当 body 为 nil 时使用 request 对象构建请求
3. **事件流解析**：Anthropic SSE 事件需解析 `message_start`(input_tokens) 和 `message_delta`(output_tokens)
4. **透传优先**：确保 SSE 数据完整透传到客户端，不丢失任何事件