# 用户并发请求动态限制功能设计方案

> 基于渠道负载动态限制每个用户同时发送的请求数量

## 背景

当渠道响应时间过长或负载较大时，需要限制每个用户同时发送的请求数量，防止上游过载雪崩。

---

## 需求确认

- **超限处理**: 排队等待
- **限制范围**: 按用户
- **配置管理**: 前端管理 + 数据库持久化
  - 支持设置最大并发数
  - 支持开关自动动态限制
  - 显示当前并发数

---

## 数据模型

### 用户并发配置表: user_concurrency_config

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INT | 主键 |
| user_id | INT | 用户ID，唯一索引 |
| max_concurrent | INT | 最大并发数，默认5 |
| enable_auto_limit | BOOLEAN | 是否启用自动动态限制，默认true |
| status | INT | 状态：1=正常，2=暂停 |
| created_at | BIGINT | 创建时间 |
| updated_at | BIGINT | 更新时间 |

---

## 执行流程

### 手动模式 (enable_auto_limit = false)

```
用户请求 → 检查用户当前并发数 → 超限? → 排队等待
                ↓
         执行请求 → 完成 → 开放下一个请求
```

### 自动模式 (enable_auto_limit = true)

```
用户请求 → 渠道负载评估
                ↓
         计算动态并发上限
                ↓
         检查用户当前并发数 → 超限? → 排队等待
                ↓
         限制并发计数发送给渠道方
                ↓
         执行请求 → 完成 → 开放下一个请求
```

---

## 自动渠道负载评估算法

### 1. 评估指标

| 指标 | 数据来源 | 更新频率 |
|------|----------|----------|
| 渠道平均响应时间 | channel.response_time | 渠道测试时更新 |
| 渠道请求成功率 | monitor.store | 每次请求后 |
| 渠道当前负载 | 计算得出 | 实时 |
| 系统失败率 | monitor.GetSystemFailRate() | 实时 |
| 渠道错误类型分布 | monitor.ShouldDisableChannel() | 每次请求后 |

### 2. 负载等级划分

```go
type LoadLevel int

const (
    LoadLevelLow    LoadLevel = iota  // 0: 正常负载
    LoadLevelMedium                    // 1: 中等负载
    LoadLevelHigh                      // 2: 高负载
    LoadLevelCritical                  // 3: 临界负载
)

type ChannelLoadMetrics struct {
    ResponseTime    int64   // 响应时间(ms)
    SuccessRate     float64 // 成功率 (0-1)
    RequestCount    int64   // 请求数
    ErrorCount      int64   // 错误数
    LastResponseTime int64  // 上次响应时间
}
```

### 3. 动态上限计算算法

#### 算法一: 线性衰减模型 (推荐)

```go
func CalculateDynamicLimit(baseLimit int, metrics *ChannelLoadMetrics) int {
    limit := float64(baseLimit)

    // 1. 响应时间因子 (权重 40%)
    responseTimeFactor := calculateResponseTimeFactor(metrics.ResponseTime)
    limit *= responseTimeFactor

    // 2. 成功率因子 (权重 40%)
    successRateFactor := calculateSuccessRateFactor(metrics.SuccessRate)
    limit *= successRateFactor

    // 3. 请求密度因子 (权重 20%)
    requestDensityFactor := calculateRequestDensityFactor(metrics)
    limit *= requestDensityFactor

    // 确保最小值为1
    if limit < 1 {
        limit = 1
    }

    return int(limit)
}

// 响应时间因子计算
// 响应时间 < 1s: 1.0
// 响应时间 1-2s: 0.8
// 响应时间 2-3s: 0.5
// 响应时间 3-5s: 0.3
// 响应时间 > 5s: 0.1
func calculateResponseTimeFactor(responseTimeMs int64) float64 {
    switch {
    case responseTimeMs < 1000:
        return 1.0
    case responseTimeMs < 2000:
        return 0.8
    case responseTimeMs < 3000:
        return 0.5
    case responseTimeMs < 5000:
        return 0.3
    default:
        return 0.1
    }
}

// 成功率因子计算
// 成功率 > 95%: 1.0
// 成功率 90-95%: 0.8
// 成功率 80-90%: 0.5
// 成功率 70-80%: 0.3
// 成功率 < 70%: 0.1
func calculateSuccessRateFactor(successRate float64) float64 {
    switch {
    case successRate > 0.95:
        return 1.0
    case successRate > 0.90:
        return 0.8
    case successRate > 0.80:
        return 0.5
    case successRate > 0.70:
        return 0.3
    default:
        return 0.1
    }
}

// 请求密度因子 (基于时间窗口内请求量)
// 高密度请求时降低限制
func calculateRequestDensityFactor(metrics *ChannelLoadMetrics) float64 {
    // 获取最近N秒内的请求数
    requestCount := metrics.RequestCount
    timeWindow := 60 // 60秒窗口

    if requestCount < 100 {
        return 1.0
    } else if requestCount < 500 {
        return 0.8
    } else if requestCount < 1000 {
        return 0.6
    } else {
        return 0.4
    }
}
```

#### 算法二: 指数加权移动平均 (EWMA)

```go
type EWMA struct {
    value      float64
    decayFactor float64  // 衰减因子，通常 0.9-0.99
}

// 初始化EWMA
func NewEWMA(decayFactor float64) *EWMA {
    return &EWMA{value: 0, decayFactor: decayFactor}
}

// 添加新样本
func (e *EWMA) Add(sample float64) {
    e.value = e.decayFactor*e.value + (1-e.decayFactor)*sample
}

// 获取当前估计值
func (e *EWMA) Value() float64 {
    return e.value
}

// 使用EWMA计算动态限制
func CalculateDynamicLimitEWMA(baseLimit int, metrics *ChannelLoadMetrics) int {
    // 响应时间EWMA
    rtEWMA := NewEWMA(0.9)
    rtEWMA.Add(float64(metrics.ResponseTime))

    // 成功率EWMA
    srEWMA := NewEWMA(0.9)
    srEWMA.Add(metrics.SuccessRate)

    // 计算综合负载分数 (0-1，越高负载越重)
    loadScore := 0.0

    // 响应时间负载 (归一化到0-1)
    // 假设正常响应时间1s，临界5s
    rtLoad := min(rtEWMA.Value()/5000.0, 1.0)
    loadScore += rtLoad * 0.5  // 权重50%

    // 成功率负载 (归一化到0-1)
    srLoad := 1.0 - srEWMA.Value()  // 成功率越低，负载越高
    loadScore += srLoad * 0.5  // 权重50%

    // 根据负载分数计算限制
    if loadScore < 0.2 {
        return baseLimit
    } else if loadScore < 0.4 {
        return int(float64(baseLimit) * 0.8)
    } else if loadScore < 0.6 {
        return int(float64(baseLimit) * 0.5)
    } else if loadScore < 0.8 {
        return int(float64(baseLimit) * 0.3)
    } else {
        return 1
    }
}
```

### 4. 推荐算法选择

| 算法 | 适用场景 | 优点 | 缺点 |
|------|----------|------|------|
| 线性衰减 | 简单场景，快速实现 | 直观，易调试 | 响应较慢 |
| EWMA | 需要平滑处理 | 抗噪声，平滑过渡 | 需要预热 |
| 回归模型 | 精确控制 | 精确，可训练 | 需要数据积累 |

**推荐**: 先使用**算法一 (线性衰减)**快速实现，后续可升级到 EWMA。

### 5. 算法参数建议

```go
// 推荐配置参数
const (
    ResponseTimeWeight   = 0.4  // 响应时间权重
    SuccessRateWeight    = 0.4  // 成功率权重
    RequestDensityWeight = 0.2  // 请求密度权重

    // 响应时间阈值 (ms)
    RTExcellent = 1000  // 优秀: < 1s
    RTGood      = 2000  // 良好: 1-2s
    RTMedium    = 3000  // 中等: 2-3s
    RTBad       = 5000  // 较差: 3-5s

    // 成功率阈值
    SRExcellent = 0.95  // 优秀: > 95%
    SRGood      = 0.90  // 良好: 90-95%
    SRMedium    = 0.80  // 中等: 80-90%
    SRBad       = 0.70  // 较差: 70-80%
)
```

### 6. 执行流程 (更新)

#### 手动模式 (enable_auto_limit = false)

```
用户请求 → 检查用户当前并发数 → 超限? → 排队等待
                ↓
         执行请求 → 完成 → 开放下一个请求
```

#### 自动模式 (enable_auto_limit = true)

```
用户请求
   │
   ├─→ [Web设置: enable_auto_limit?]
   │         │
   │         ├─ NO (手动模式) ──→ 检查并发数 ──→ 超限? ──→ 排队等待
   │         │                                    ↓
   │         │                              执行请求
   │         │                                    ↓
   │         └─ YES (自动模式) ──→ 渠道负载评估 ──→ 计算动态上限
   │                                                        ↓
   │                                                  超限? ──→ 排队等待
   │                                                    ↓
   │                                              限制并发计数
   │                                              发送给渠道方
   │                                                    ↓
   └──────────────────────────────────────────────────→ 执行请求
                                                            ↓
                                                      完成 → 开放下一个请求
```

---

## 实现文件清单

| 文件 | 类型 | 说明 |
|------|------|------|
| `model/concurrency.go` | 新增 | UserConcurrencyConfig 模型 |
| `common/concurrency/limiter.go` | 新增 | 并发限制核心 |
| `common/concurrency/load_evaluator.go` | 新增 | 负载评估算法 |
| `common/ctxkey/key.go` | 修改 | 添加上下文键 |
| `middleware/auth.go` | 修改 | Acquire/Release |
| `controller/relay.go` | 修改 | defer Release |
| `controller/user_concurrency.go` | 新增 | 配置 API |
| `router/dashboard.go` | 修改 | 注册路由 |
| `model/channel.go` | 修改 | 添加负载查询方法 |
| `monitor/metric.go` | 修改 | 扩展指标收集 |
| `web/default/src/pages/User/ConcurrencyConfig.js` | 新增 | 前端配置页 |

---

## 数据库迁移

```sql
CREATE TABLE user_concurrency_config (
    id INT PRIMARY KEY AUTO_INCREMENT,
    user_id INT NOT NULL UNIQUE,
    max_concurrent INT DEFAULT 5,
    enable_auto_limit BOOLEAN DEFAULT TRUE,
    status INT DEFAULT 1,
    created_at BIGINT,
    updated_at BIGINT,
    INDEX idx_user_id (user_id)
);
```

---

## 验证方式

1. **手动测试**:
   ```bash
   for i in {1..10}; do
     curl -X POST http://localhost:3000/v1/chat/completions \
       -H "Authorization: Bearer $TOKEN" \
       -d '{"model":"gpt-3.5-turbo","messages":[{"role":"user","content":"test'$i'"}]}' &
   done
   ```

2. **前端测试**:
   - 设置最大并发=2，观察请求排队
   - 开启自动限制，观察高负载时自动降低

---

## 更新日志

- **2026-04-29**: 初始方案设计