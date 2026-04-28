# 令牌指定渠道功能测试计划

> 创建日期: 2026-04-26
> 功能版本: Token Channel Binding
> 测试环境: http://10.31.133.114:3009

---

## 一、测试目标

验证令牌绑定渠道功能是否正常工作：
1. 用户可以在令牌编辑页面选择允许的渠道
2. 使用带渠道绑定的令牌请求时，优先使用指定渠道
3. 当指定渠道不可用时，自动回退到优先级选择
4. 管理员和普通用户都可以使用此功能

---

## 二、测试准备

### 2.1 前置条件

1. **编译部署**
```bash
# 编译后端
cd /home/bmc/sd1/CODE/one-api
go build ./...

# 停止现有服务
pkill -f "one-api.*3009" 2>/dev/null || true

# 启动服务
export SQL_DSN="postgres://postgres:NCbmc%40123@localhost:5432/oneapi?sslmode=disable"
./one-api --port 3009 --log-dir ./logs &
```

2. **数据库确认**
```bash
# 确认 one-api 表结构已更新（ChannelIds 字段）
psql -h localhost -U postgres -d oneapi -c "\d tokens"
```

### 2.2 测试数据准备

**需要准备两个配置相同模型但不同优先级的渠道：**

| 渠道 | 名称 | 类型 | Base URL | 模型 | Priority | 状态 |
|------|------|------|----------|------|----------|------|
| 渠道A | Test-OpenAI | OpenAI兼容 | https://api.openai.com | gpt-4 | 10 | 启用 |
| 渠道B | Test-Anthropic | Anthropic兼容 | https://api.anthropic.com | gpt-4 | 5 | 启用 |

---

## 三、测试用例

### 3.1 API 测试

#### TC-001: 获取可用渠道列表
**请求**
```bash
curl -s --noproxy '*' http://127.0.0.1:3009/api/token/available_channels \
  -H "Authorization: Bearer sk-你的access-token"
```

**预期结果**
```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "name": "Test-OpenAI",
      "type": 50,
      "status": 1,
      "models": "gpt-4",
      "priority": 10
    },
    {
      "id": 2,
      "name": "Test-Anthropic",
      "type": 52,
      "status": 1,
      "models": "gpt-4",
      "priority": 5
    }
  ]
}
```

---

### 3.2 前端功能测试

#### TC-002: 创建令牌时选择渠道
**步骤**
1. 登录 one-api (root/NCbmc@123)
2. 进入令牌管理页面
3. 点击"新建令牌"
4. 填写名称，选择模型范围
5. 在"允许的渠道"下拉框中选择渠道 A
6. 提交

**预期结果**
- 渠道选择器显示所有可用渠道
- 令牌创建成功
- 令牌详情显示绑定的渠道 ID

---

#### TC-003: 编辑令牌时修改渠道绑定
**步骤**
1. 选择一个已存在的令牌
2. 点击编辑
3. 修改"允许的渠道"
4. 保存

**预期结果**
- 渠道绑定成功更新
- 后续请求使用新绑定的渠道

---

### 3.3 核心功能测试

#### TC-004: 令牌指定渠道生效（单一渠道）
**前置条件**
- 创建令牌 A，只绑定渠道 A
- 禁用渠道 B

**步骤**
```bash
# 使用令牌 A 请求
curl -s --noproxy '*' http://127.0.0.1:3009/v1/chat/completions \
  -H "Authorization: Bearer sk-令牌A的key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

**预期结果**
- 请求使用渠道 A
- logs 表中 channel_id = 渠道A的ID

---

#### TC-005: 令牌指定渠道生效（多渠道）
**前置条件**
- 创建令牌 B，绑定渠道 A 和渠道 B
- 两个渠道都启用

**步骤**
1. 多次使用令牌 B 发送请求
2. 检查 logs 表中使用的渠道

**预期结果**
- 每次请求都在指定的渠道中随机选择
- 如果渠道 A 失败，自动切换到渠道 B

---

#### TC-006: 指定渠道都不可用时回退
**前置条件**
- 创建令牌 C，只绑定渠道 A
- 禁用渠道 A

**步骤**
```bash
curl -s --noproxy '*' http://127.0.0.1:3009/v1/chat/completions \
  -H "Authorization: Bearer sk-令牌C的key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

**预期结果**
- 返回 503 错误（因为没有其他可用渠道）
- 或自动回退使用其他可用渠道

---

#### TC-007: 不限制渠道的令牌
**前置条件**
- 创建令牌 D，不选择任何渠道

**步骤**
```bash
curl -s --noproxy '*' http://127.0.0.1:3009/v1/chat/completions \
  -H "Authorization: Bearer sk-令牌D的key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

**预期结果**
- 按 Priority 优先级自动选择渠道
- 优先使用高优先级渠道

---

### 3.4 边界测试

#### TC-008: 普通用户使用渠道绑定
**步骤**
1. 以普通用户身份登录
2. 创建令牌并绑定渠道
3. 使用该令牌发送请求

**预期结果**
- 普通用户可以正常绑定渠道
- 请求使用绑定的渠道

---

#### TC-009: 绑定不存在的渠道
**步骤**
1. 编辑令牌，手动输入一个不存在的渠道 ID（如 999）
2. 保存

**预期结果**
- 后端验证失败，返回错误信息
- 或前端验证拒绝输入

---

#### TC-010: 绑定其他分组的渠道
**步骤**
1. 以 vip 分组用户登录
2. 尝试绑定 default 分组的渠道

**预期结果**
- 后端验证拒绝，返回"渠道不在您的分组中"

---

## 四、日志验证

### 4.1 查看请求日志
```bash
# 查看 one-api 日志
tail -f logs/one-api.log | grep "channel"

# 或查看指定渠道的请求
grep "using channel #" logs/one-api.log
```

### 4.2 验证数据库记录
```sql
-- 查看令牌绑定的渠道
SELECT id, name, channel_ids FROM tokens WHERE channel_ids IS NOT NULL;

-- 查看请求使用的渠道
SELECT id, channel_id, model_name, created_at FROM logs ORDER BY created_at DESC LIMIT 10;

-- 验证指定渠道的请求
SELECT l.id, l.channel_id, l.model_name, t.name as token_name, t.channel_ids
FROM logs l
JOIN tokens t ON l.token_id = t.id
WHERE t.channel_ids IS NOT NULL
ORDER BY l.created_at DESC;
```

---

## 五、测试检查清单

| 序号 | 检查项 | 状态 |
|------|--------|------|
| 1 | API /api/token/available_channels 正常返回 | [ ] |
| 2 | 创建令牌时可以选择渠道 | [ ] |
| 3 | 编辑令牌时可以修改渠道 | [ ] |
| 4 | 令牌指定渠道生效 | [ ] |
| 5 | 多渠道随机选择 | [ ] |
| 6 | 渠道不可用时回退 | [ ] |
| 7 | 不限制渠道正常走优先级 | [ ] |
| 8 | 普通用户可使用功能 | [ ] |
| 9 | 跨分组渠道被拒绝 | [ ] |
| 10 | 数据库字段正确存储 | [ ] |

---

## 六、预期测试结果

### 6.1 成功标准
- [ ] 所有测试用例通过
- [ ] 前端渠道选择器正常工作
- [ ] 令牌绑定渠道后请求使用指定渠道
- [ ] 渠道不可用时自动回退
- [ ] 数据库正确存储 channel_ids 字段

### 6.2 失败处理
如测试失败，请记录：
1. 失败的测试用例编号
2. 实际结果 vs 预期结果
3. 相关日志片段
4. 环境状态（数据库、渠道配置等）

---

## 七、测试后操作

### 7.1 清理测试数据
```sql
-- 删除测试令牌（可选）
DELETE FROM tokens WHERE name LIKE '测试%';
```

### 7.2 恢复测试环境
```bash
# 如果需要恢复渠道状态
UPDATE channels SET status = 1 WHERE name IN ('Test-OpenAI', 'Test-Anthropic');
```

---

## 八、联系方式

- 测试负责人: _________
- 测试日期: _________
- 测试结果: _________