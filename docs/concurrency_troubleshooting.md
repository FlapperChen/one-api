# 并发控制问题定位记录

## 修改时间
2026-05-12

---

## 问题一：请求时长因子计算不正确

### 现象
- API 返回 `avg_duration: 16463` (16.46s)
- API 返回 `wait_timeout: 10000` (10s，个人配置)
- 显示的 `factors.duration: 0.8`
- 但根据 16.46s / 10s = 1.65 > 0.67，因子应该是 **0.1**

### 根本原因
因子计算函数 `CalculateDurationFactor` **内部固定从全局配置读取 waitTimeout**，没有使用 API 返回的 `wait_timeout` 值。

```go
// 原代码 - load_evaluator.go
func CalculateDurationFactor(avgDurationMs int64) float64 {
    // 固定从全局配置读取
    maxThreshold := float64(getConfigInt("ConcurrencyWaitTimeout", config.ConcurrencyWaitTimeout)) * 1000
    ...
}
```

**数据来源不一致**：
- API 返回的 `wait_timeout`: 来自个人/全局配置
- 因子计算使用的阈值: 固定从全局配置读取

### 修复方案

**1. 修改 `CalculateDurationFactor` 函数签名**

添加 `waitTimeoutMs` 参数：

```go
// load_evaluator.go
// 请求时长因子
// avgDurationMs: 平均响应时长（毫秒）
// waitTimeoutMs: 等待超时阈值（毫秒），如果为0则使用全局配置
func CalculateDurationFactor(avgDurationMs int64, waitTimeoutMs int) float64 {
    maxThreshold := float64(waitTimeoutMs)
    if maxThreshold <= 0 {
        // 如果未提供 waitTimeoutMs，从全局配置读取
        maxThreshold = float64(getConfigInt("ConcurrencyWaitTimeout", config.ConcurrencyWaitTimeout)) * 1000
    }
    if maxThreshold <= 0 {
        maxThreshold = 30000
    }
    ratio := float64(avgDurationMs) / maxThreshold
    switch {
    case ratio < 0.1:   return 1.0
    case ratio < 0.33:  return 0.8
    case ratio < 0.67:  return 0.5
    default:            return 0.1
    }
}
```

**2. 修改 controller/misc.go 中的调用**

- `GetAutoConcurrencyLimit` 函数（第332行）：
```go
// 之前
durationFactor := concurrency.CalculateDurationFactor(avgDuration)

// 之后 - 传入用户/配置的 waitTimeout
durationFactor := concurrency.CalculateDurationFactor(avgDuration, waitTimeout*1000)
```

- `GetAutoConcurrencyLimitForUser` 函数（第568行）：
```go
// 之前
durationFactor := concurrency.CalculateDurationFactor(avgDuration)

// 之后 - 传入目标用户的 waitTimeout
durationFactor := concurrency.CalculateDurationFactor(avgDuration, waitTimeout*1000)
```

**3. 内部调用（传0保持兼容）**

load_evaluator.go 内部有两处调用：
```go
// CalculateUserDynamicLimit 和 GetUserLoadInfo 中
durationFactor := CalculateDurationFactor(metrics.AvgRequestDuration, 0) // 0 表示使用全局配置
```

### 验证计算

修改后使用个人配置的 waitTimeout 计算：

```
avg_duration = 16463ms
waitTimeout = 10000ms
ratio = 16463 / 10000 = 1.646 > 0.67
duration_factor = 0.1 ✓
```

---

## 问题二：全局配置下表单值被自动刷新覆盖

### 现象
- 全局模式下，修改"用户最大并发数"、"请求超时"等表单值
- 每5秒自动刷新后，输入的值被覆盖回数据库中的值

### 根本原因
自动刷新调用 `fetchAutoStatus()` 时无条件更新 `inputs` 表单状态。

```javascript
// 原代码 - 自动刷新时
useEffect(() => {
    if (inputs.EnableAutoConcurrencyLimit === 'true') {
        const interval = setInterval(() => {
            fetchAutoStatus(); // 无条件更新 inputs！
        }, 5000);
    }
}, [...]);
```

### 修复方案

**1. 为 `fetchAutoStatus` 添加 `updateInputs` 参数**

```javascript
// 获取自动并发限制状态
// updateInputs: 是否更新 inputs 表单，切换用户/初始化时为 true，自动刷新时为 false
const fetchAutoStatus = async (updateInputs = true) => {
    // ... 获取数据 ...

    // 只有在需要更新 inputs 时才更新
    if (updateInputs) {
        setInputs(prev => ({
            ...prev,
            EnableManualConcurrencyLimit: data.enabled?.manual ? 'true' : 'false',
            // ... 其他字段
        }));
    }
};
```

**2. 修改自动刷新逻辑**

```javascript
useEffect(() => {
    if (inputs.EnableAutoConcurrencyLimit === 'true') {
        const interval = setInterval(() => {
            if (configMode === 'personal' && targetUserId !== 0) {
                // 个人模式：刷新状态，不更新 inputs
                fetchAutoStatusForUser(targetUserId, false);
            } else {
                // 全局模式：刷新状态，不更新 inputs（避免覆盖用户输入）
                fetchAutoStatus(false);
            }
        }, 5000);
    }
}, [...]);
```

### 自动刷新行为定义

| 场景 | updateInputs | 结果 |
|------|--------------|------|
| 初始化/切换模式/切换用户 | `true` | 更新 inputs + 状态显示 |
| **自动刷新（全/个人模式）** | `false` | **只更新状态显示** |

### 应该只刷新的状态值（橙色区域）
- 当前动态限制
- 实际生效限制
- 平均响应时长
- 负载等级
- GPU使用率
- 因子计算值

### 不应自动刷新的表单值
- 用户最大并发数
- 请求超时
- 检查间隔
- 缓存TTL
- 开关状态等

---

## 修改文件清单

| 文件 | 修改内容 |
|------|----------|
| `common/concurrency/load_evaluator.go` | `CalculateDurationFactor` 添加 `waitTimeoutMs` 参数，内部调用传0 |
| `controller/misc.go` | 两处因子计算调用传入 `waitTimeout*1000` |
| `web/default/src/components/OperationSetting.js` | `fetchAutoStatus` 添加 `updateInputs` 参数，自动刷新传 `false` |

---

## 测试验证

### 后端测试

```bash
# 全局模式
curl --noproxy '*' -s "localhost:3009/api/system/auto-concurrency-limit" \
  -H "Authorization: Bearer <token>" | jq '{avg_duration, wait_timeout, duration_factor: .data.factors.duration}'

# 个人模式（指定用户）
curl --noproxy '*' -s "localhost:3009/api/system/auto-concurrency-limit/1" \
  -H "Authorization: Bearer <token>" | jq '{avg_duration, wait_timeout, duration_factor: .data.factors.duration}'
```

### 验证公式

```
ratio = avg_duration / (wait_timeout * 1000)

因子判断:
ratio < 0.1   → 1.0
ratio < 0.33  → 0.8
ratio < 0.67  → 0.5
ratio >= 0.67 → 0.1
```

### 前端测试

1. **因子计算验证**
   - 全局模式：因子使用全局配置的 waitTimeout
   - 个人模式：因子使用个人配置的 waitTimeout

2. **表单保护验证**
   - 全局模式下修改表单值
   - 等待5秒自动刷新
   - 验证表单值**不被覆盖**

3. **状态刷新验证**
   - 观察橙色状态区域是否正常刷新

---

## 数据库访问方法

```python
python3 -c "
import psycopg2
conn = psycopg2.connect('host=localhost dbname=oneapi user=postgres password=<password>')
cur = conn.cursor()
# 查询并发配置
cur.execute(\"SELECT key, value FROM options WHERE key LIKE '%Concurrent%' OR key LIKE '%Timeout%'\")
print(cur.fetchall())
"
```

---

## 经验教训

1. **数据一致性**：函数调用时要确保使用正确的参数，不要在内部偷偷读取其他数据源

2. **UI 反馈 vs 用户输入**：自动刷新应只更新实时状态，不应覆盖用户正在编辑的表单

3. **参数设计**：如果函数需要上下文相关的配置值，应作为参数传入，而非在函数内部获取

4. **测试场景**：修改后要测试各种场景（全局/个人、初始化/刷新、切换用户等）

---

## 相关文档

- [并发控制重构总结](./concurrency_refactor_summary.md)