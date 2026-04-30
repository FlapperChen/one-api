# 用户并发请求动态限制功能 (V2)

> 基于渠道负载动态限制每个用户同时发送的请求数量，支持请求去重和GPU资源监控

## 功能概述

当渠道响应时间过长或负载较大时，限制每个用户同时发送的请求数量，防止上游过载雪崩。

### 核心能力

1. **手动模式**: 用户可配置固定最大并发数
2. **自动模式**: 根据系统负载动态调整并发限制
3. **请求去重**: 识别客户端重试请求，避免重复处理
4. **GPU监控**: 集成vLLM GPU KV Cache监控，精确控制并发

---

## 架构设计

### 执行流程

```
用户请求
    │
    ▼
┌──────────────────────────────────────────────────────────────┐
│ 1. TokenAuth 中间件                                          │
│    ├─ 验证令牌                                               │
│    └─ 获取用户并发配置                                        │
└──────────────────────────────────────────────────────────────┘
    │
    ▼
┌──────────────────────────────────────────────────────────────┐
│ 2. 请求去重中间件 (RequestDeduplication)                     │
│    ├─ 计算请求指纹 (SHA256)                                  │
│    ├─ 查询 request_cache 表                                  │
│    │    ├─ 命中(完成) → 直接返回缓存响应                      │
│    │    ├─ 命中(处理中) → 加入等待队列                        │
│    │    └─ 未命中 → 正常处理                                 │
│    └─ 设置缓存ID到上下文                                     │
└──────────────────────────────────────────────────────────────┘
    │
    ▼
┌──────────────────────────────────────────────────────────────┐
│ 3. 并发限制检查 (TokenAuth)                                  │
│    └─ UserConcurrencyLimiter.Acquire()                      │
│         ├─ 获取用户配置                                       │
│         ├─ 计算动态限制                                       │
│         │    └─ LoadEvaluator.CalculateDynamicLimit()        │
│         │         ├─ 请求时长因子 (25%)                       │
│         │         ├─ 当前并发因子 (25%)                       │
│         │         ├─ 并发趋势因子 (20%)                       │
│         │         └─ GPU KV Cache因子 (30%)                  │
│         └─ 超限? → 排队等待                                   │
└──────────────────────────────────────────────────────────────┘
    │
    ▼
┌──────────────────────────────────────────────────────────────┐
│ 4. 请求处理 (Relay)                                          │
│    └─ defer Release() → 释放并发许可                          │
└──────────────────────────────────────────────────────────────┘
```

---

## 数据模型

### 1. user_concurrency_config

用户并发配置表。

```go
type UserConcurrencyConfig struct {
    ID               int    `gorm:"primaryKey"`
    UserId           int    `gorm:"uniqueIndex"`
    MaxConcurrent    int    `gorm:"default:5"`
    EnableAutoLimit  bool   `gorm:"default:true"`
    Status           int    `gorm:"default:1"` // 1=正常, 2=暂停
    CreatedTime      int64  `gorm:"bigint"`
    UpdatedTime      int64  `gorm:"bigint"`
}
```

### 2. request_cache

请求缓存表，用于请求去重。

```go
type RequestCache struct {
    Id             int64  `gorm:"primaryKey;autoIncrement"`
    UserId         int    `gorm:"index"`
    Fingerprint    string `gorm:"index;size:64"`    // 请求指纹
    RequestHash    string `gorm:"size:64"`          // 完整请求Hash
    RequestType    string `gorm:"size:32"`          // chat/completion
    Model          string `gorm:"index;size:128"`
    Status         int    `gorm:"default:0"`        // 0=处理中, 1=完成, 2=失败
    RequestBody    string `gorm:"type:text"`
    ResponseBody   string `gorm:"type:longtext"`
    ErrorMessage   string `gorm:"type:text"`
    CreatedTime    int64  `gorm:"bigint"`
    CompletedTime  int64  `gorm:"bigint"`
    DuplicateCount int    `gorm:"default:0"`        // 重复请求数
    TTL            int    `gorm:"default:30"`      // 缓存过期秒数
}
```

---

## 核心算法

### 请求指纹计算

```go
func CalculateFingerprint(reqBody map[string]interface{}) string {
    // 1. 提取关键字段 (model, messages, parameters)
    // 2. 去除随机参数 (seed, user, stream)
    // 3. 排序确保一致性
    // 4. 计算 SHA256 Hash
}
```

**指纹组成**:
- Model 名称
- 消息内容 (按 role:content 格式)
- 参数 (排除随机参数后按 key 排序)

### 增强版动态限制计算

```go
func (e *LoadEvaluator) CalculateDynamicLimit(baseLimit int) int {
    limit := float64(baseLimit)

    // 1. 请求时长因子 (权重 25%)
    limit *= calculateDurationFactor(metrics.AvgRequestDuration)

    // 2. 当前并发因子 (权重 25%)
    limit *= calculateConcurrentFactor(metrics.CurrentConcurrent, baseLimit)

    // 3. 并发趋势因子 (权重 20%)
    limit *= calculateTrendFactor(window)

    // 4. GPU KV Cache 因子 (权重 30%)
    limit *= calculateGPUFactor(metrics.GPUKVCacheUsage)

    return max(1, int(limit))
}
```

**因子表**:

| 响应时间 | 因子值 | GPU使用率 | 因子值 |
|----------|--------|-----------|--------|
| < 1s | 1.0 | < 50% | 1.0 |
| 1-3s | 0.8 | 50-70% | 0.8 |
| 3-5s | 0.5 | 70-85% | 0.5 |
| 5-10s | 0.3 | 85-95% | 0.3 |
| > 10s | 0.1 | > 95% | 0.1 |

---

## GPU 监控

### vLLM 日志格式

```
Engine 000: Avg prompt throughput: 7002.4 tokens/s,
           Avg generation throughput: 131.4 tokens/s,
           Running: 3 reqs, Waiting: 0 reqs,
           GPU KV cache usage: 15.9%,
           Prefix cache hit rate: 55.4%
```

### 数据获取方式

1. **vLLM Metrics API** (推荐): `GET /metrics` → Prometheus 格式
2. **日志文件解析**: 读取 `/var/log/vllm/*.log`

### 监控指标

| 指标 | 说明 |
|------|------|
| GPU KV Cache Usage | GPU 显存使用百分比 |
| Running Requests | 正在运行的请求数 |
| Waiting Requests | 等待队列长度 |
| Prompt Throughput | 提示词处理速度 (tokens/s) |
| Generation Throughput | 生成速度 (tokens/s) |

---

## API 接口

### 用户接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/user/concurrency | 获取用户配置 |
| PUT | /api/user/concurrency | 更新用户配置 |
| GET | /api/user/concurrency/current | 获取当前并发数 |

### 管理员接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/admin/concurrency/stats | 所有用户并发统计 |
| GET | /api/admin/concurrency/:id | 获取指定用户配置 |
| PUT | /api/admin/concurrency/:id | 更新指定用户配置 |

### 系统接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/system/load | 系统负载信息 |

---

## 环境变量

### 并发限制

| 变量 | 默认值 | 说明 |
|------|--------|------|
| ENABLE_CONCURRENCY_LIMIT | false | 启用并发限制 |
| USER_BASE_CONCURRENT_LIMIT | 5 | 用户默认最大并发数 |
| CONCURRENCY_WAIT_TIMEOUT | 30 | 排队等待超时(秒) |
| CONCURRENCY_CHECK_INTERVAL | 100 | 检查间隔(毫秒) |

### GPU 监控

| 变量 | 默认值 | 说明 |
|------|--------|------|
| ENABLE_GPU_MONITORING | false | 启用GPU监控 |
| VLLM_LOG_PATH | /var/log/vllm | vLLM日志路径 |
| VLLM_API_URL | http://localhost:8000 | vLLM Metrics API |
| VLLM_REFRESH_INTERVAL | 5 | 刷新间隔(秒) |
| GPU_KV_CACHE_WARN | 85.0 | KV Cache警告阈值(%) |
| GPU_KV_CACHE_MAX | 95.0 | KV Cache最大阈值(%) |

### 请求去重

| 变量 | 默认值 | 说明 |
|------|--------|------|
| ENABLE_REQUEST_DEDUP | false | 启用请求去重 |
| REQUEST_CACHE_TTL | 30 | 缓存过期时间(秒) |

---

## 前端配置

在 **运营设置** 页面中配置并发限制功能。

### 并发限制设置

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| 用户最大并发数 | 每个用户同时发送的最大请求数 | 5 |
| 排队等待超时 | 排队等待超时时间（秒） | 30 |
| 检查间隔 | 并发检查间隔（毫秒） | 100 |

### 开关选项

| 选项 | 说明 |
|------|------|
| 启用并发限制 | 开启后限制每个用户的并发请求数 |
| 启用GPU监控 | 开启后监控GPU KV Cache使用率 |
| 启用请求去重 | 开启后识别并合并重复请求 |

### GPU监控与请求去重

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| vLLM API地址 | vLLM Metrics API地址 | http://localhost:8000 |
| KV Cache警告阈值 | GPU KV Cache警告阈值(%) | 85 |
| KV Cache最大阈值 | GPU KV Cache最大阈值(%) | 95 |
| 请求缓存TTL | 请求缓存过期时间（秒） | 30 |

### 界面截图位置

```
运营设置 → 并发限制设置
    ├─ 并发限制设置区块
    └─ GPU监控与请求去重区块
```

---

## 代码文件

```
model/
├── concurrency.go      # 用户并发配置模型
└── request_cache.go    # 请求缓存模型 + 去重算法

common/concurrency/
├── limiter.go          # 并发限制核心实现
└── load_evaluator.go   # 增强版负载评估算法

monitor/gpu/
└── vllm_monitor.go     # GPU KV Cache监控器

middleware/
└── request_dedup.go    # 请求去重中间件

controller/
└── user_concurrency.go # 配置API控制器

model/
└── option.go           # 配置项管理（InitOptionMap, updateOptionMap）

web/default/src/
├── components/
│   └── OperationSetting.js  # 运营设置页面（并发限制配置）
└── locales/
    ├── zh/translation.json  # 中文翻译
    └── en/translation.json  # 英文翻译
```

---

## 数据库迁移

表会在应用启动时通过 GORM AutoMigrate 自动创建：

```sql
CREATE TABLE IF NOT EXISTS user_concurrency_config (
    id INT PRIMARY KEY AUTO_INCREMENT,
    user_id INT NOT NULL UNIQUE,
    max_concurrent INT DEFAULT 5,
    enable_auto_limit BOOLEAN DEFAULT TRUE,
    status INT DEFAULT 1,
    created_time BIGINT,
    updated_time BIGINT,
    INDEX idx_user_id (user_id)
);

CREATE TABLE IF NOT EXISTS request_cache (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id INT,
    fingerprint VARCHAR(64),
    request_hash VARCHAR(64),
    request_type VARCHAR(32),
    model VARCHAR(128),
    status INT DEFAULT 0,
    request_body TEXT,
    response_body LONGTEXT,
    error_message TEXT,
    created_time BIGINT,
    completed_time BIGINT,
    duplicate_count INT DEFAULT 0,
    ttl INT DEFAULT 30,
    INDEX idx_user_fingerprint (user_id, fingerprint),
    INDEX idx_model (model)
);
```

---

## 使用示例

### 启用功能

```bash
# 启用并发限制
export ENABLE_CONCURRENCY_LIMIT=true
export USER_BASE_CONCURRENT_LIMIT=5

# 启用GPU监控
export ENABLE_GPU_MONITORING=true
export VLLM_API_URL=http://localhost:8000
export GPU_KV_CACHE_WARN=85.0
export GPU_KV_CACHE_MAX=95.0

# 启用请求去重
export ENABLE_REQUEST_DEDUP=true
export REQUEST_CACHE_TTL=30
```

### 测试去重

```bash
# 发送两次相同请求
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-3.5-turbo","messages":[{"role":"user","content":"hello"}]}'

# 第二次请求应该返回 X-Request-Dedup: hit 响应头
```

### 查看系统负载

```bash
curl http://localhost:3000/api/system/load
# 返回:
# {
#   "success": true,
#   "data": {
#     "enabled": true,
#     "load_level": 1,
#     "response_time": 1500,
#     "success_rate": 0.95,
#     "gpu_kv_cache_usage": 45.5,
#     "running_requests": 3
#   }
# }
```

---

## 更新日志

### V2.1 (2026-04-30)

- 新增前端运营设置页面配置界面
- 支持通过 UI 配置并发限制参数
- 支持通过 UI 配置 GPU 监控参数
- 支持通过 UI 配置请求去重参数
- 后端配置实时生效

### V2.0 (2026-04-30)

- 新增增强版负载评估算法 (4因子模型)
- 新增 GPU KV Cache 监控
- 新增请求去重功能
- 新增并发滑动窗口趋势分析

### V1.0 (2026-04-29)

- 初始版本，实现基础并发限制功能