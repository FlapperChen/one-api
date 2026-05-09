# 并发控制平均响应时长重构总结

## 修改时间
2026-05-09

---

## 一、问题描述

### 原始问题
"平均响应时长"计算逻辑不正确：
- 原实现：从所有启用渠道的最近一次响应时间的平均值
- 实际需求：
  - **全局模式**：使用所有用户请求时长**前十名**的平均值
  - **个人模式**：
    - 有个人配置：使用**目标用户的最后一次请求时长**
    - 无个人配置：使用全局 Top10 平均值

### 后续发现的问题
前端切换用户时配置显示错误：
- 个人模式切换到无个人配置的用户时，显示的是前一个有配置用户的配置
- 原因：前端依赖 `globalConcurrencyInputs` 缓存判断，缓存为 null 时不更新 inputs

---

## 二、修改文件清单

### 1. model/log.go

**新增两个查询方法**：

```go
// GetTop10AvgElapsedTime 获取Top10用户最近一次请求时长的平均值
// 只统计消费类型日志 (type=LogTypeConsume)，返回毫秒
func GetTop10AvgElapsedTime() (int64, error) {
    type Result struct {
        AvgElapsed float64
    }
    var result Result
    // 子查询：获取每个用户最近一次请求时长
    subQuery := LOG_DB.Model(&Log{}).
        Select("user_id, MAX(created_at) as latest_time").
        Where("type = ? AND elapsed_time > 0", LogTypeConsume).
        Group("user_id")
    // 再关联获取每个用户最近一次请求的具体时长，按时长降序取前10
    latestQuery := LOG_DB.Table("(?) as latest", subQuery).
        Select("l.elapsed_time").
        Joins("JOIN logs l ON l.user_id = latest.user_id AND l.created_at = latest.latest_time AND l.type = ? AND l.elapsed_time > 0", LogTypeConsume).
        Order("l.elapsed_time DESC").
        Limit(10)
    // 外层查询：计算这10条记录的平均值
    err := LOG_DB.Table("(?) as t", latestQuery).
        Select("AVG(elapsed_time) as avg_elapsed").
        Scan(&result).Error
    if err != nil {
        return 0, err
    }
    return int64(result.AvgElapsed), nil
}

// GetUserLastRequestElapsedTime 获取用户最近一次请求的时长（毫秒）
func GetUserLastRequestElapsedTime(userId int) (int64, error) {
    var log Log
    err := LOG_DB.Where("user_id = ? AND type = ? AND elapsed_time > 0", userId, LogTypeConsume).
        Order("created_at DESC").
        Select("elapsed_time").
        First(&log).Error
    if err != nil {
        return 0, err
    }
    return log.ElapsedTime, nil
}
```

### 2. common/concurrency/load_evaluator.go

**修改 RefreshMetrics 方法**：

```go
// RefreshMetrics 刷新系统级指标
func (e *LoadEvaluator) RefreshMetrics() {
    failRate := monitor.GetSystemFailRate()
    successRate := 1.0 - failRate

    // 全局模式：使用所有用户请求时长前十名的平均值
    var avgResponseTime int64 = 1000
    avgResponseTime, _ = model.GetTop10AvgElapsedTime()
    if avgResponseTime == 0 {
        avgResponseTime = 1000 // 默认值
    }
    // ... GPU 监控代码保持不变
}
```

**新增 GetUserAvgElapsedTime 方法**：

```go
// GetUserAvgElapsedTime 获取指定用户的平均响应时长
// 优先使用用户最近一次请求时长，无记录则返回全局平均值
func (e *LoadEvaluator) GetUserAvgElapsedTime(userId int) int64 {
    elapsed, err := model.GetUserLastRequestElapsedTime(userId)
    if err == nil && elapsed > 0 {
        return elapsed
    }
    avgGlobal, _ := model.GetTop10AvgElapsedTime()
    if avgGlobal == 0 {
        return 1000
    }
    return avgGlobal
}
```

### 3. controller/misc.go

**修改 GetAutoConcurrencyLimit 函数**：

```go
// 获取平均响应时长
// 全局模式：使用所有用户请求时长Top10平均值
// 个人模式：有个人配置则使用用户最近一次请求时长，否则与全局模式一致
var avgDuration int64
if configMode == "global" || !hasPersonalConfig {
    // 全局模式或无个人配置：使用Top10平均值
    avgDuration, _ = model.GetTop10AvgElapsedTime()
    if avgDuration == 0 {
        avgDuration = 1000
    }
} else {
    // 个人模式且有个人配置：使用用户最近一次请求时长
    avgDuration = evaluator.GetUserAvgElapsedTime(userId)
}
```

**修改 GetAutoConcurrencyLimitForUser 函数**：

```go
// 获取平均响应时长
// 有个人配置则使用用户最近一次请求时长，否则使用Top10平均值
var avgDuration int64
if hasPersonalConfig {
    avgDuration = evaluator.GetUserAvgElapsedTime(targetUserId)
} else {
    avgDuration, _ = model.GetTop10AvgElapsedTime()
    if avgDuration == 0 {
        avgDuration = 1000
    }
}
```

### 4. web/default/src/components/OperationSetting.js

**问题根因**：前端依赖 `globalConcurrencyInputs` 缓存来判断是否更新 inputs，但缓存可能为 null，导致切换用户时 inputs 不更新，保持前一个用户的配置值。

**修复方案**：直接使用 API 返回值，无需依赖缓存判断。

**修改 fetchAutoStatusForUser**：

```javascript
// 之前：有条件更新，依赖缓存
if (data.has_personal_config) {
    setInputs({...个人配置...});
} else if (globalConcurrencyInputs) {
    setInputs({...缓存配置...});  // 缓存为null时不更新！
}

// 现在：始终使用API返回值
setInputs(prev => ({
    ...prev,
    EnableManualConcurrencyLimit: data.enabled?.manual ? 'true' : 'false',
    EnableAutoConcurrencyLimit: data.enabled?.auto ? 'true' : 'false',
    EnableRequestDeduplication: data.enable_request_dedup ? 'true' : 'false',
    UserBaseConcurrentLimit: data.manual_limit || data.wait_timeout || 5,
    ConcurrencyWaitTimeout: data.wait_timeout || 30,
    ConcurrencyCheckInterval: data.check_interval || 100,
    RequestCacheTTL: data.cache_ttl || 30,
}));
```

**修改 fetchAutoStatus**：

```javascript
// 之前：只在has_personal_config时更新
if (data.has_personal_config) {
    setInputs({...});
}

// 现在：全局模式下始终使用API返回值
setInputs(prev => ({
    ...prev,
    EnableManualConcurrencyLimit: data.enabled?.manual ? 'true' : 'false',
    // ... 其他字段
}));
```

---

## 三、数据流示意

```
全局模式:
  API /api/system/auto-concurrency-limit
  → RefreshMetrics() → GetTop10AvgElapsedTime() → 返回所有用户请求时长前十名平均值
  → avg_duration = Top10平均值

个人模式-有配置:
  API /api/system/auto-concurrency-limit/{userId}
  → has_personal_config = true
  → GetUserAvgElapsedTime(userId) → GetUserLastRequestElapsedTime(userId)
  → avg_duration = 用户最近一次请求时长

个人模式-无配置:
  API /api/system/auto-concurrency-limit/{userId}
  → has_personal_config = false
  → GetTop10AvgElapsedTime()
  → avg_duration = Top10平均值
```

---

## 四、经验教训

### 1. 避免状态依赖陷阱
- **错误做法**：依赖其他状态变量来判断是否更新当前状态
- **正确做法**：直接从数据源获取最新值

### 2. 后端负责返回正确数据
- 前端不应做复杂的状态计算和缓存逻辑
- 后端 API 应根据请求上下文（全局/个人模式）返回正确的值

### 3. 缓存需要考虑初始化情况
- 缓存变量初始化为 null 时，所有依赖该缓存的条件判断都可能失效
- 如果缓存为可选的备用方案，必须有兜底逻辑

### 4. 调试技巧
- 使用 curl 直接调用 API 验证后端逻辑
- 使用数据库查询验证数据正确性
- 前端问题排查时，检查 React 组件的状态更新链

---

## 五、相关数据库表

### logs 表（关键字段）
| 字段 | 类型 | 说明 |
|------|------|------|
| user_id | int | 用户ID |
| type | int | 日志类型（2=消费日志） |
| created_at | bigint | 创建时间戳 |
| elapsed_time | int64 | 请求时长（毫秒） |

### options 表（可能的问题来源）
| key | 正确值 | 说明 |
|-----|--------|------|
| UserBaseConcurrentLimit | 5 | 用户基础并发限制 |
| ConcurrencyWaitTimeout | 30 | 等待超时（秒） |
| ConcurrencyCheckInterval | 100 | 检查间隔（毫秒） |
| RequestCacheTTL | 30 | 缓存TTL（秒） |

---

## 六、测试验证

### 后端测试
```bash
# 全局模式：验证 avg_duration = Top10平均值（约41957ms）
curl localhost:3009/api/system/auto-concurrency-limit

# 个人模式-有配置用户：验证 avg_duration = 用户最近一次请求时长
curl localhost:3009/api/system/auto-concurrency-limit/1

# 个人模式-无配置用户：验证 avg_duration = Top10平均值
curl localhost:3009/api/system/auto-concurrency-limit/2
```

### 前端测试
1. 全局模式：验证显示 Top10 平均值
2. 个人模式切换到无配置用户：验证显示全局配置值
3. 个人模式切换到有配置用户：验证显示个人配置值
4. 模式切换：验证状态正确重置

---

## 七、注意事项

1. **重启服务**：修改后端代码后必须重启服务
2. **构建前端**：修改前端代码后需要重新构建
3. **数据库配置**：确保 `LOG_SQL_DSN` 指向正确的日志数据库
4. **OptionMap 缓存**：`config.OptionMap` 在服务启动时加载，修改数据库后需要重启或等待缓存刷新