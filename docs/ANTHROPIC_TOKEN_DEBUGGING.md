# Anthropic 兼容渠道 Token 统计问题定位方案

## 一、问题描述

使用 Anthropic 兼容渠道（AnthropicCompatible）发起请求后，没有 token 消耗记录。

**测试命令：**
```bash
curl -X POST http://10.31.133.114:3009/v1/messages \
  -H "Authorization: Bearer Cp1kKrXYYALHyALJ81AbF520E1F847D2Bd291388443a1c59" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "/models/coder/minimax/MiniMax-M2",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "hello"}]
  }' \
  -v 2>&1
```

## 二、定位步骤

### 步骤 1：确认请求是否成功到达 relay 层

**检查点：** 查看日志中是否有 relay 请求处理记录

**预期日志：**
```
[relay] Request received for channel: AnthropicCompatible (56)
[relay] API Type: AnthropicCompatible (24)
```

### 步骤 2：确认预消费（PreConsume）是否执行

**检查点：** 用户配额是否在请求前被扣除

**预期日志：**
```
[billing] Pre-consumed quota: XXX for user X
```

### 步骤 3：确认 DoResponse 返回的 usage 是否有效

**检查点：** 这是最关键的检查点

**预期日志：**
```
[relay] Usage from response: prompt_tokens=X, completion_tokens=Y, total_tokens=Z
```

**如果返回 nil：**
```
[relay] usage is nil, which is unexpected
```

### 步骤 4：确认后消费（PostConsume）是否执行

**检查点：** 根据实际 usage 调整配额

**预期日志：**
```
[billing] Post-consumed quota: XXX for user X (prompt: X, completion: Y)
```

### 步骤 5：检查数据库使用记录

**检查点：** 查看 `user_quotas` 或 `log` 表是否有记录

**SQL 查询：**
```sql
SELECT * FROM user_quotas ORDER BY created_at DESC LIMIT 10;
SELECT * FROM logs WHERE type = 'consume' ORDER BY created_at DESC LIMIT 10;
```

## 三、代码流程分析

### Token 统计完整流程

```
1. relay/controller/text.go:RelayTextHelper()
   │
   ├─→ preConsumeQuota()           // 步骤 2: 预消费
   │   位置: relay/billing/quota.go
   │   日志: "[billing] Pre-consumed quota"
   │
   ├─→ adaptor.DoRequest()         // 发送请求到上游
   │
   ├─→ adaptor.DoResponse()        // 步骤 3: 获取 usage (关键！)
   │   位置: relay/adaptor/anthropiccompatible/adaptor.go:79-139
   │   返回值: (*model.Usage, *model.ErrorWithStatusCode)
   │
   └─→ postConsumeQuota()          // 步骤 4: 后消费
       位置: relay/controller/helper.go:97-141
       关键代码:
       ```go
       if usage == nil {
           logger.Error(ctx, "usage is nil, which is unexpected")
           return  // ← 如果 usage 是 nil，直接跳过！
       }
       ```
```

### AnthropicCompatible 适配器当前实现

**文件：** `relay/adaptor/anthropiccompatible/adaptor.go`

**DoResponse 函数逻辑：**
1. 读取响应体 (`io.ReadAll`)
2. 解析 JSON 为 `Response` 结构体
3. 提取 `Response.Usage.InputTokens` 和 `Response.Usage.OutputTokens`
4. 返回 `*model.Usage` 对象

## 四、可能的问题原因

### 问题 1：DoResponse 返回 nil usage（已修复）

**原因：** 适配器未正确解析响应体提取 usage

**状态：** 已在最新提交中修复

### 问题 2：上游响应格式不匹配

**原因：** 上游返回的 JSON 格式与 Anthropic 标准格式不同

**排查：**
```bash
# 捕获上游实际响应
curl -X POST http://10.31.133.114:3009/v1/messages \
  -H "Authorization: Bearer xxx" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "/models/coder/minimax/MiniMax-M2",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "hello"}]
  }' 2>&1 | jq .
```

**预期格式：**
```json
{
  "id": "msg_xxx",
  "type": "message",
  "model": "claude-3-5-sonnet-20241022",
  "usage": {
    "input_tokens": 10,
    "output_tokens": 20
  },
  "content": [...]
}
```

### 问题 3：响应未被正确读取

**原因：** 响应体在到达 DoResponse 之前已被读取

**排查：** 检查 middleware 是否有提前读取响应体的逻辑

### 问题 4：流式响应未处理

**原因：** 如果使用流式模式，需要实现 StreamHandler

**排查：** 检查请求是否包含 `stream: true`

## 五、日志级别设置

查看当前日志配置，确认日志级别是否足够详细：

**位置：** `common/logger/logger.go`

**关键日志点：**
- `relay/controller/helper.go` - 预消费/后消费日志
- `relay/adaptor/anthropiccompatible/adaptor.go` - usage 提取日志

## 六、快速验证清单

| 步骤 | 检查项 | 命令/方法 |
|------|--------|----------|
| 1 | 服务是否运行 | `ps aux \| grep one-api` |
| 2 | 请求是否成功 | curl 请求查看 HTTP 状态码 |
| 3 | 日志文件位置 | `tail -f logs/one-api.log` |
| 4 | 查看关键日志 | `grep -i "usage" logs/one-api.log` |
| 5 | 查看消费日志 | `grep -i "consume" logs/one-api.log` |
| 6 | 数据库记录 | SQL 查询 user_quotas 表 |

## 七、修复后的预期行为

修复后，日志应该显示：

```
[2026/04/18 10:00:00] [relay] Request received for channel: AnthropicCompatible (56)
[2026/04/18 10:00:00] [billing] Pre-consumed quota: 1000 for user 1
[2026/04/18 10:00:00] [relay] Usage from response: prompt_tokens=10, completion_tokens=20, total_tokens=30
[2026/04/18 10:00:00] [billing] Post-consumed quota: 35 for user 1 (prompt: 10, completion: 20)
```

**说明：**
- 预消费扣除 1000（预估配额）
- 后消费根据实际 usage (10 + 20*ratio) 调整
- 差额返还给用户

---

## 八、2026-04-18 最新诊断结果

### 测试发现的问题

1. **日志记录已正常工作**
   - 使用 AnthropicCompatible 渠道的请求已正确记录到 logs 表
   - `channel_id = 4` 对应 AnthropicCompatible 渠道

2. **Token 统计为 0 的原因**
   - 上游响应包含正确的 usage：`{"input_tokens": 39, "output_tokens": 36}`
   - 但 logs 表中 `prompt_tokens=0, completion_tokens=0, quota=0`
   - 原因：**运行中的 one-api 是旧版本，未包含修复代码**

3. **定位到的根本原因**
   - 在 `relay/controller/helper.go` 第 111-114 行：
   ```go
   if totalTokens == 0 {
       quota = 0  // ← 当 usage 为 nil 时，quota 被设为 0
   }
   ```
   - 旧版本的 `DoResponse` 返回 `nil, nil`，导致 usage 为 nil

### 需要的操作

1. **重新编译代码**
   ```bash
   cd /home/bmc/sd1/CODE/one-api
   go build -o one-api .
   ```

2. **停止旧服务**
   ```bash
   pkill -f "one-api.*--port 3009"
   ```

3. **启动新服务**
   ```bash
   export SQL_DSN="postgres://postgres:NCbmc%40123@localhost:5432/oneapi"
   export SESSION_SECRET="your-secret-key-change-this"
   ./one-api --port 3009
   ```

4. **验证修复**
   - 重新发起测试请求
   - 检查日志：`tail -f logs/oneapi-20260418.log`
   - 预期日志显示 `prompt_tokens>0, completion_tokens>0`