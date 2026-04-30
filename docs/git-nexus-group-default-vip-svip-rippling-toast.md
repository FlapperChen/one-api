# 用户分组管理功能实现计划

## Context

当前 one-api 系统已经有基础的用户分组功能，但存在以下问题：
1. 分组信息硬编码在 `relay/billing/ratio/group.go` 中，无法动态管理
2. 无法通过界面创建新分组
3. 用户分组编辑前端使用了 `allowAdditions`，允许随意输入，与现有分组不一致
4. 没有分组管理界面

本方案旨在运营设置下增加完整的分组管理功能，支持创建自定义分组，同时保护 default/vip/svip 三个系统保留分组。

## 方案概述

### 核心设计

1. **新建分组管理表** (`groups`)：存储分组元数据，包括是否系统保留分组
2. **保持现有数据模型**：用户和渠道的 `Group` 字段仍为逗号分隔字符串
3. **前端单选限制**：用户编辑时只显示单个分组下拉框
4. **API 权限**：分组管理 API 仅限 Root 用户访问

---

## 详细实现步骤

### 阶段一：后端模型与API

#### 1.1 新建 `model/group.go`

```go
type Group struct {
    ID           int       `json:"id" gorm:"primaryKey"`
    Name         string    `json:"name" gorm:"uniqueIndex;size:32;not null"`
    Description  string    `json:"description" gorm:"size:255"`
    IsDefault    bool      `json:"is_default" gorm:"default:false"`
    CreatedAt    int64     `json:"created_at"`
    UpdatedAt    int64     `json:"updated_at"`
}
```

实现方法：
- `GetAllGroups()` - 获取所有分组（包含 is_default）
- `GetGroupById(id int)` - 按ID获取
- `CreateGroup(group *Group)` - 创建（检查名称唯一性）
- `UpdateGroup(group *Group)` - 更新（不允许修改 is_default=true 的分组）
- `DeleteGroup(id int)` - 删除（检查关联、检查是否为默认分组）
- `SyncGroupRatio()` - 同步到 billingratio.GroupRatio

#### 1.2 更新 `model/main.go`

在数据库迁移函数中添加：
```go
// 创建 groups 表
DB.AutoMigrate(&Group{})

// 初始化默认分组
initDefaultGroups()
```

#### 1.3 更新 `controller/group.go`

扩展现有控制器，添加完整CRUD：

```go
// GET /api/group/ - 获取所有分组（包含 is_default）
// POST /api/group/ - 创建分组（Root权限）
// PUT /api/group/ - 更新分组（Root权限）
// DELETE /api/group/:id - 删除分组（Root权限，检查是否默认分组）
// GET /api/group/models - 获取分组可用模型（Admin权限）
```

#### 1.4 更新 `router/api.go`

```go
groupRoute := apiRouter.Group("/group")
groupRoute.Use(middleware.RootAuth())  // 改为 RootAuth
{
    groupRoute.GET("/", controller.GetAllGroups)
    groupRoute.POST("/", controller.CreateGroup)
    groupRoute.PUT("/", controller.UpdateGroup)
    groupRoute.DELETE("/:id", controller.DeleteGroup)
}
```

#### 1.5 更新 `relay/billing/ratio/group.go`

修改为从数据库加载分组倍率，同时保持配置文件兼容：
```go
var GroupRatio = map[string]float64{
    "default": 1,
    "vip":     1,
    "svip":    1,
}

func SyncFromDatabase(groups []*model.Group, ratioJSON string) {
    // 从数据库同步分组列表
    // 从 GroupRatio 配置同步倍率
}
```

#### 1.6 更新 `model/cache.go`

添加分组变更时的缓存刷新逻辑：
```go
func RefreshGroupCache() {
    // 清除分组相关缓存
    // 重新加载分组列表
}
```

---

### 阶段二：前端 - 默认主题

#### 2.1 新建 `web/default/src/components/GroupManagement.js`

功能：
- 表格展示所有分组（ID、名称、描述、是否默认、操作）
- 默认分组行显示禁用状态的编辑/删除按钮
- 新建分组按钮
- 模态框：新建/编辑分组
- 删除确认对话框

组件结构：
```jsx
const GroupManagement = () => {
  const { t } = useTranslation();
  const [groups, setGroups] = useState([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState(null);
  // ... CRUD 逻辑
}
```

#### 2.2 更新 `web/default/src/components/OperationSetting.js`

在现有内容后添加分组管理区域：
```jsx
<Divider />
<Header as='h3'>{t('setting.operation.group_management.title')}</Header>
<p>{t('setting.operation.group_management.description')}</p>
<GroupManagement />
```

#### 2.3 更新 `web/default/src/pages/User/EditUser.js`

修改分组选择组件：
```jsx
// 移除 allowAdditions
// 改为单选模式
<Form.Select
  label={t('user.edit.group')}
  name='group'
  fluid
  search
  selection
  onChange={handleInputChange}
  value={inputs.group}
  options={groupOptions}
/>
```

#### 2.4 更新 `web/default/src/helpers/render.js`

更新 `renderGroup` 函数以支持从 API 获取的分组列表样式。

#### 2.5 添加国际化

`web/default/src/locales/zh/translation.json` 和 `en/translation.json` 添加：
```json
{
  "group": {
    "title": "分组管理",
    "table": { "id": "ID", "name": "名称", "description": "描述", "is_default": "默认", "actions": "操作" },
    "buttons": { "add": "新建分组", "edit": "编辑", "delete": "删除" },
    "form": { "name": "分组名称", "name_placeholder": "请输入分组名称", "description": "描述", "cancel": "取消", "submit": "提交" },
    "messages": { "create_success": "分组创建成功", "cannot_delete_default": "默认分组不能删除", "name_required": "分组名称不能为空" }
  },
  "setting": { "operation": { "group_management": { "title": "分组管理", "description": "管理系统中的用户分组，默认分组不可删除" } } }
}
```

---

### 阶段三：前端 - Berry 主题

#### 3.1 新建 `web/berry/src/views/Setting/component/GroupManagement.jsx`

使用 MUI 组件实现相同功能：
- `DataGrid` 或 `Table` 展示
- `Dialog` 模态框
- `Button`, `IconButton`
- `Chip` 显示默认分组标签

#### 3.2 更新 `web/berry/src/views/Setting/OperationSetting.jsx`

集成分组管理组件。

#### 3.3 更新 `web/berry/src/views/User/EditUser.jsx`

修改分组选择为单选下拉框。

#### 3.4 添加国际化

`web/berry/src/locales/` 下添加翻译。

---

## 文件修改清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `model/group.go` | 新建 | 分组数据模型 |
| `model/main.go` | 修改 | 添加迁移和初始化 |
| `model/cache.go` | 修改 | 添加分组缓存逻辑 |
| `controller/group.go` | 修改 | 扩展CRUD API |
| `router/api.go` | 修改 | 添加新路由 |
| `relay/billing/ratio/group.go` | 修改 | 支持数据库同步 |
| `web/default/src/components/GroupManagement.js` | 新建 | 默认主题分组管理组件 |
| `web/default/src/components/OperationSetting.js` | 修改 | 添加分组管理入口 |
| `web/default/src/pages/User/EditUser.js` | 修改 | 分组单选 |
| `web/default/src/helpers/render.js` | 修改 | 更新渲染函数 |
| `web/default/src/locales/zh/translation.json` | 修改 | 中文翻译 |
| `web/default/src/locales/en/translation.json` | 修改 | 英文翻译 |
| `web/berry/src/views/Setting/component/GroupManagement.jsx` | 新建 | Berry主题分组管理 |
| `web/berry/src/views/Setting/OperationSetting.jsx` | 修改 | 添加入口 |
| `web/berry/src/views/User/EditUser.jsx` | 修改 | 分组单选 |
| `web/berry/src/locales/*.json` | 修改 | 国际化 |

---

## 验证方案

### 测试场景

1. **分组管理功能**
   - [ ] 登录 Root 账号，可看到分组管理区域
   - [ ] 新建自定义分组（如 "pro"），保存成功
   - [ ] 编辑自定义分组名称，保存成功
   - [ ] 删除自定义分组，提示关联检查（如有渠道使用则阻止）
   - [ ] 尝试编辑/删除 default/vip/svip，显示"默认分组不能xxx"

2. **用户分组设置**
   - [ ] 用户管理页面，编辑用户分组下拉框只显示已有分组
   - [ ] 选择分组后保存，用户分组更新成功
   - [ ] 无法手动输入新分组名

3. **渠道分组设置**
   - [ ] 渠道编辑页面，分组多选框显示所有分组
   - [ ] 分配渠道到新创建的分组成功

4. **权限验证**
   - [ ] 非 Root 用户无法访问 /api/group/ 的 POST/PUT/DELETE 接口
   - [ ] 非 Admin 用户无法访问 /api/group/models 接口

5. **数据一致性**
   - [ ] 分组变更后，渠道分配逻辑正常工作
   - [ ] 用户请求路由正确使用用户分组