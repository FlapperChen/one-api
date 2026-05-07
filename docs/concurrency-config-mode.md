# 并发控制个人配置/全局配置切换功能

## 功能概述

在 root 用户的"运营设置"页面"并发控制与负载管理"区块增加"个人配置"与"全局配置"的切换选项。

### 主要功能

1. **配置模式切换**：用户可选择使用"全局配置"或"个人配置"
2. **个人配置管理**：管理员可查看/编辑指定用户的个人并发配置
3. **智能回退**：当个人配置不存在时，自动使用全局配置并提示用户
4. **数据持久化**：页面刷新后保持当前选择的目标用户

---

## 修改文件清单

### 后端修改

#### 1. `controller/misc.go`

**新增 API**：
- `GetConcurrencyConfigMode` - 获取当前配置模式
- `SetConcurrencyConfigMode` - 设置配置模式
- `GetAutoConcurrencyLimitForUser` - 管理员获取指定用户的并发状态

**修改**：
- `GetAutoConcurrencyLimit` - 支持根据模式返回全局或个人配置，添加 `config_mode` 和 `has_personal_config` 字段

**原因**：需要提供配置模式管理接口和区分全局/个人配置的 API

---

#### 2. `controller/user_concurrency.go`

**新增函数**：
- `DeleteUserConcurrencyConfig` - 删除当前用户个人配置
- `DeleteUserConcurrencyConfigById` - 管理员删除指定用户配置

**原因**：支持删除个人配置，使其恢复使用全局配置

---

#### 3. `router/api.go`

**新增路由**：
```
GET  /api/system/concurrency-config-mode      # 获取配置模式
PUT  /api/system/concurrency-config-mode      # 设置配置模式
GET  /api/system/auto-concurrency-limit/:userId  # 管理员获取指定用户状态
DELETE /api/user/concurrency                  # 删除当前用户个人配置
DELETE /api/user/concurrency/:id              # 管理员删除指定用户配置
```

**原因**：注册新的 API 路由

---

#### 4. `model/concurrency.go`

**修改**：
- `GetUserConcurrencyConfig` - 使用 `Find()` 代替 `First()`，避免"record not found"日志

**原因**：GORM 使用 `First()` 查询不存在记录时会记录 ERROR 日志，影响日志可读性

---

#### 5. `model/user.go`

**修改**：
- `SearchUsers` - 支持按 ID 精确搜索

**原代码问题**：PostgreSQL 版本没有按 ID 搜索的逻辑

**修复内容**：
```go
func SearchUsers(keyword string) (users []*User, err error) {
    // 尝试将 keyword 解析为数字（用户ID）
    if userId, parseErr := strconv.Atoi(keyword); parseErr == nil {
        var idUsers []*User
        idErr := DB.Omit("password").Where("id = ?", userId).Find(&idUsers).Error
        if idErr == nil && len(idUsers) > 0 {
            return idUsers, nil
        }
    }
    // 按用户名、邮箱、显示名称模糊搜索
    // ...
}
```

**原因**：用户输入数字 ID 时需要能搜索到对应用户

---

### 前端修改

#### `web/default/src/components/OperationSetting.js`

**新增状态**：
```javascript
let [configMode, setConfigMode] = useState('global');      // 配置模式
let [targetUserId, setTargetUserId] = useState(() => parseInt(sessionStorage.getItem('targetUserId') || '0'));
let [isAdmin, setIsAdmin] = useState(false);               // 是否为管理员
let [currentUserId, setCurrentUserId] = useState(0);       // 当前用户ID
let [targetUserName, setTargetUserName] = useState(sessionStorage.getItem('targetUserName') || '');
```

**新增函数**：
- `fetchConfigMode` - 获取配置模式，自动加载之前选择的目标用户
- `setConfigModeAPI` - 设置配置模式
- `fetchAutoStatusForUser` - 获取指定用户并发状态
- `searchUsers(keyword)` - 搜索用户（直接使用传入参数避免异步问题）
- `selectUser` - 选择用户并持久化到 sessionStorage

**UI 修改**：
- 在并发控制区块顶部添加配置模式切换（全局配置/个人配置）
- 添加用户搜索输入框，自动搜索并显示下拉列表
- 选择用户后显示提示信息

**修复的问题**：

1. **避免重复提示**：移除 `updateOption` 中的成功提示，改为在 `submitConfig` 中统一显示

2. **选择当前用户时重置目标**：
   ```javascript
   if (user.id === currentUserId) {
     setTargetUserId(0);
     setTargetUserName('');
     sessionStorage.setItem('targetUserId', '0');
     sessionStorage.setItem('targetUserName', '');
   }
   ```

3. **sessionStorage 持久化**：页面刷新后恢复之前选择的目标用户

4. **搜索框立即响应**：使用 `searchUsers(value)` 直接传入新值而非依赖状态

---

## 数据源映射

| 配置项 | 全局配置 (options表) | 个人配置 (user_concurrency_config表) |
|--------|---------------------|-------------------------------------|
| MaxConcurrent | UserBaseConcurrentLimit | max_concurrent |
| EnableAutoLimit | EnableAutoConcurrencyLimit | enable_auto_limit |
| Status | EnableManualConcurrencyLimit | status |
| WaitTimeout | ConcurrencyWaitTimeout | - |
| CheckInterval | ConcurrencyCheckInterval | - |
| 权重/GPU配置 | DurationFactorWeight 等 | - |

**说明**：个人配置表只有 3 个字段，其他配置项始终使用全局值

---

## API 返回字段说明

### `/api/system/auto-concurrency-limit`

```json
{
  "success": true,
  "data": {
    "config_mode": "personal",           // 全局或个人配置模式
    "target_user_id": 1,                 // 目标用户ID
    "has_personal_config": false,        // 是否有个人配置
    "dynamic_limit": 11,                 // 动态限制
    "manual_limit": 11,                  // 手动限制
    "actual_limit": 11,                  // 实际限制
    "load_level": 0,                     // 负载等级
    "current_concurrent": 0,             // 当前并发数
    "factors": {...},                    // 因子值
    "weights": {...},                    // 权重配置
    "metrics": {...},                    // 系统指标
    "enabled": {...}                     // 功能开关
  }
}
```

---

## 使用说明

### 普通用户
1. 切换到"个人配置"模式
2. 修改个人配置并保存
3. 点击"恢复默认参数"会清除个人配置，恢复使用全局配置

### 管理员
1. 切换到"个人配置"模式
2. 在搜索框输入用户 ID 或名称
3. 从下拉列表选择用户
4. 可查看/编辑该用户的个人配置
5. 选择用户ID为1时（当前用户），提示会自动消失

---

## 测试命令

```bash
# 获取配置模式
curl localhost:3009/api/system/concurrency-config-mode -H "Authorization: Bearer TOKEN"

# 设置个人配置模式
curl localhost:3009/api/system/concurrency-config-mode -X PUT -H "Authorization: Bearer TOKEN" -d '{"mode":"personal"}'

# 获取当前用户并发状态
curl localhost:3009/api/system/auto-concurrency-limit -H "Authorization: Bearer TOKEN"

# 管理员获取指定用户并发状态
curl localhost:3009/api/system/auto-concurrency-limit/2 -H "Authorization: Bearer TOKEN"

# 搜索用户
curl localhost:3009/api/user/search?keyword=2 -H "Authorization: Bearer TOKEN"

# 删除个人配置
curl localhost:3009/api/user/concurrency -X DELETE -H "Authorization: Bearer TOKEN"
```

---

## 记录的问题与修复

| 问题 | 原因 | 修复 |
|------|------|------|
| 保存个人配置 404 | 前端 API 路径错误 `/api/admin/concurrency` 应为 `/api/user/concurrency` | 修改前端 API 路径 |
| 搜索用户 404 | 后端路由是 `/api/user/search` 不是 `/api/admin/search` | 修改前端 API 路径 |
| 输入单个数字无法搜索 | 路由不正确导致请求 404 | 修正路由路径 |
| record not found 日志过多 | GORM `First()` 查询不存在记录时记录 ERROR 日志 | 改用 `Find()` |
| 选择用户后提示不消失 | 选择当前用户时 targetUserId 未重置 | 在 selectUser 中重置 |
| 页面刷新后选择用户丢失 | 状态未持久化 | 使用 sessionStorage 持久化 |
| 输入立即搜索不生效 | React 状态异步更新导致使用旧值 | searchUsers(value) 直接传参 |
| 全局配置保存无提示 | updateOption 成功时未显示提示 | 在 submitConfig 统一显示 |
| 切换个人配置自动创建记录 | GetOrCreateUserConcurrencyConfig 会自动创建 | 改用 GetUserConcurrencyConfig 判断 |
| 权重等配置无法个性化 | 个人配置表字段不足 | 明确文档说明这些配置使用全局值 |

---

## 版本信息

- 修改日期：2026-05-07
- 功能状态：已完成