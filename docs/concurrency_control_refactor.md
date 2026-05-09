# 并发控制与负载管理模块重构文档

## 修改日期
2026-05-08 ~ 2026-05-09

## 背景

### 问题 1：启用请求去重开关无法同步全局配置
- **现象**：在个人配置模式下选择一个没有个人配置的用户，"启用请求去重"开关不会同步显示全局配置的值
- **原因**：`EnableRequestDeduplication` 使用环境变量 `config.EnableRequestDeduplication`（默认为 `false`），而其他配置项都从 `OptionMap`（数据库）读取

### 问题 2：用户输入被自动刷新覆盖
- **现象**：在个人配置模式下编辑输入框时，每 5 秒的自动刷新会用全局值覆盖用户正在编辑的内容
- **原因**：自动刷新调用 `fetchAutoStatusForUser()` 时无条件更新 `inputs` 状态

---

## 后端修改

### 1. common/concurrency/load_evaluator.go

**新增函数**：`GetCurrentConfigBool` 和 `getConfigBool`

```go
// GetCurrentConfigBool 从 OptionMap 获取布尔配置值（导出版本）
func GetCurrentConfigBool(key string, defaultVal bool) bool {
	return getConfigBool(key, defaultVal)
}

// getConfigBool 内部函数：从 OptionMap 获取布尔配置值
func getConfigBool(key string, defaultVal bool) bool {
	if val, ok := config.OptionMap[key]; ok {
		return val == "true"
	}
	return defaultVal
}
```

**目的**：提供与 `GetCurrentConfigInt` 类似的布尔配置读取功能，从数据库 `OptionMap` 读取而非使用环境变量默认值。

### 2. controller/misc.go

#### GetAutoConcurrencyLimit 函数 (第 274 行)
```go
// 修改前
dedupEnabled := config.EnableRequestDeduplication

// 修改后
dedupEnabled := concurrency.GetCurrentConfigBool("EnableRequestDeduplication", config.EnableRequestDeduplication)
```

#### GetAutoConcurrencyLimitForUser 函数 (第 492 行)
```go
// 修改前
dedupEnabled := config.EnableRequestDeduplication

// 修改后
dedupEnabled := concurrency.GetCurrentConfigBool("EnableRequestDeduplication", config.EnableRequestDeduplication)
```

**目的**：确保无个人配置时正确返回数据库中的全局配置值。

### 3. common/concurrency/limiter.go

#### GetEffectiveConcurrencyConfig 方法 (第 275 行)
```go
// 修改前
result.EnableRequestDedup = config.EnableRequestDeduplication

// 修改后
result.EnableRequestDedup = getConfigBool("EnableRequestDeduplication", config.EnableRequestDeduplication)
```

**目的**：保持与上面相同的逻辑一致性。

---

## 前端修改

### web/default/src/components/OperationSetting.js

#### 1. 新增状态 (第 90 行)
```javascript
let [globalConcurrencyInputs, setGlobalConcurrencyInputs] = useState(null); // 全局并发配置缓存，用于无个人配置时显示
```

**用途**：缓存全局配置，用于无个人配置时显示和切换用户时恢复。

#### 2. fetchAutoStatusForUser 函数 (第 212-265 行)

```javascript
// 修改前
const fetchAutoStatusForUser = async (userId) => {
  // ... 无条件更新 inputs
};

// 修改后
const fetchAutoStatusForUser = async (userId, updateInputs = true) => {
  // ...
  // 只有在需要更新 inputs 时才更新（切换用户时，自动刷新时不更新）
  if (updateInputs) {
    if (data.has_personal_config) {
      // 使用个人配置更新 inputs
      setInputs({...});
    } else if (globalConcurrencyInputs) {
      // 无个人配置时，使用全局配置缓存更新 inputs
      setInputs({...});
    }
  }
};
```

**用途**：
- 切换用户时 (`updateInputs=true`)：更新 inputs 表单
- 自动刷新时 (`updateInputs=false`)：只更新实时状态，不更新表单

#### 3. fetchAutoStatus 函数 (第 267-315 行)

```javascript
// 新增：缓存全局并发配置
const globalConfig = {
  EnableManualConcurrencyLimit: data.enabled?.manual ? 'true' : 'false',
  EnableAutoConcurrencyLimit: data.enabled?.auto ? 'true' : 'false',
  EnableRequestDeduplication: data.enable_request_dedup ? 'true' : 'false',
  UserBaseConcurrentLimit: data.manual_limit || 5,
  ConcurrencyWaitTimeout: data.wait_timeout || 30,
  ConcurrencyCheckInterval: data.check_interval || 100,
  RequestCacheTTL: data.cache_ttl || 30,
};
setGlobalConcurrencyInputs(globalConfig);
```

**用途**：在获取全局配置时同步更新缓存，供无个人配置的用户使用。

#### 4. 自动刷新 useEffect (第 362-376 行)

```javascript
// 修改前
fetchAutoStatusForUser(targetUserId);

// 修改后
fetchAutoStatusForUser(targetUserId, false);  // 不更新 inputs
```

**用途**：自动刷新只更新实时状态显示，不覆盖用户正在编辑的内容。

#### 5. fetchConfigMode 函数 (第 160-171 行)

```javascript
// 修改前（个人模式）
await fetchAutoStatus();
await fetchAutoStatusForUser(savedTargetUserId);

// 修改后（个人模式）
await getOptions();        // 先初始化 inputs
await fetchAutoStatus();   // 再缓存全局配置
await fetchAutoStatusForUser(savedTargetUserId);
```

**用途**：刷新页面进入个人模式时，确保 inputs 正确初始化。

#### 6. setConfigModeAPI 函数 (第 184-209 行)

```javascript
// 修改前（个人模式）
await fetchAutoStatus();
await fetchAutoStatusForUser(savedTargetUserId);

// 修改后（个人模式）
await getOptions();        // 先初始化 inputs
await fetchAutoStatus();   // 再缓存全局配置
await fetchAutoStatusForUser(savedTargetUserId);
```

**用途**：切换到个人模式时，确保 inputs 正确初始化。

---

## 数据流示意

### 个人模式初始化流程
```
1. getOptions()          → 从 /api/option/ 获取所有配置，初始化 inputs
2. fetchAutoStatus()     → 从 /api/system/auto-concurrency-limit 获取全局配置
                          → 缓存到 globalConcurrencyInputs
3. fetchAutoStatusForUser() →
   - 有个人配置 (has_personal_config=true) → 更新 inputs 为个人配置值
   - 无个人配置 (has_personal_config=false) → 更新 inputs 为 globalConcurrencyInputs
```

### 切换用户流程
```
selectUser(userId) → fetchAutoStatusForUser(userId, true) →
  - 有个人配置 → inputs = 个人配置
  - 无个人配置 → inputs = globalConcurrencyInputs (已缓存的全局值)
```

### 自动刷新流程
```
setInterval(5s) → fetchAutoStatusForUser(userId, false) →
  - 只更新 autoStatus (实时状态)
  - 不更新 inputs (用户正在编辑的内容保持不变)
```

---

## 配置优先级逻辑

| 配置项 | 有个人配置 | 无个人配置 |
|--------|-----------|-----------|
| EnableManualConcurrencyLimit | 个人配置 | 全局配置 (OptionMap) |
| EnableAutoConcurrencyLimit | 个人配置 | 全局配置 (OptionMap) |
| UserBaseConcurrentLimit | 个人配置 | 全局配置 (OptionMap) |
| ConcurrencyWaitTimeout | 个人配置 | 全局配置 (OptionMap) |
| ConcurrencyCheckInterval | 个人配置 | 全局配置 (OptionMap) |
| RequestCacheTTL | 个人配置 | 全局配置 (OptionMap) |
| EnableRequestDeduplication | 个人配置 | 全局配置 (OptionMap) |
| 因子权重 | - | 全局配置 (OptionMap) |
| GPU监控配置 | - | 全局配置 (OptionMap) |

---

## 验证测试清单

### 后端测试
1. 修改全局"启用请求去重"配置后，在个人模式下选择无个人配置用户
   - 验证开关状态与全局配置一致

### 前端测试
1. **刷新页面测试**
   - 全局模式下刷新 → inputs 显示全局配置
   - 个人模式下刷新 → inputs 显示正确的配置值

2. **输入保护测试**
   - 个人模式下选择无个人配置用户
   - 修改输入框值（如"用户最大并发数"）
   - 等待 5 秒自动刷新
   - 验证输入框值保持不变（只有顶部实时状态更新）

3. **用户切换测试**
   - 在无个人配置用户和有个人配置用户之间切换
   - 验证 inputs 正确显示各自的配置值
   - 验证切换到无个人配置用户时，显示最新的全局配置

---

## 涉及文件清单

### 后端
- `common/concurrency/load_evaluator.go` - 新增布尔配置读取函数
- `controller/misc.go` - 修改全局配置读取方式
- `common/concurrency/limiter.go` - 修改全局配置读取方式

### 前端
- `web/default/src/components/OperationSetting.js` - 完善状态管理和刷新逻辑