# Anthropic 兼容渠道实施计划

## 一、当前状态总结

### 已完成的工作 ✅

| 序号 | 工作项 | 文件位置 | 状态 |
|------|--------|----------|------|
| 1 | API 类型定义 | `relay/apitype/define.go` (值=24) | ✅ |
| 2 | 渠道类型定义 | `relay/channeltype/define.go` (值=56) | ✅ |
| 3 | 渠道→API类型映射 | `relay/channeltype/helper.go` | ✅ |
| 4 | Base URL 配置 | `relay/channeltype/url.go` (空，用户自定义) | ✅ |
| 5 | Relay Mode 定义 | `relay/relaymode/define.go` (Messages) | ✅ |
| 6 | 路径解析 | `relay/relaymode/helper.go` (/v1/messages) | ✅ |
| 7 | 适配器实现 | `relay/adaptor/anthropiccompatible/adaptor.go` | ✅ |
| 8 | 适配器注册 | `relay/adaptor.go` | ✅ |
| 9 | 请求处理 | `relay/controller/text.go` | ✅ |
| 10 | 路由配置 | `router/relay.go` (/v1/messages) | ✅ |
| 11 | 前端选项(default) | `web/default/src/constants/channel.constants.js` | ✅ |
| 12 | 前端选项(berry) | `web/berry/src/constants/ChannelConstants.js` | ✅ |

### 代码架构

```
Claude Code (Anthropic 格式 /v1/messages)
    ↓
One API /v1/messages 路由 (router/relay.go)
    ↓
Distribute 中间件 → 选择渠道 (Type = 56 AnthropicCompatible)
    ↓
AnthropicCompatible 适配器 (relay/adaptor/anthropiccompatible/adaptor.go)
    ↓
透传到上游 {BaseURL}/v1/messages
    ↓
vLLM 返回响应
    ↓
透传响应给客户端
```

## 二、问题分析

### 问题：前端下拉框未显示 "Anthropic 兼容" 选项

**用户反馈**：在浏览器 Console 执行 `CHANNEL_OPTIONS` 报错 "CHANNEL_OPTIONS is not defined"

**可能原因**：

1. **浏览器缓存未清除** - 最常见原因
2. **前端未正确构建** - 构建后的 JS 文件可能没有包含新选项
3. **使用的是 Berry 主题而非 Default 主题** - 需要确认用户使用的主题

**验证步骤**（需要用户在浏览器执行）：

1. 打开浏览器开发者工具 (F12)
2. 访问渠道编辑页面
3. 在 Console 执行：
   ```javascript
   // 查看使用的常量名称
   window.__REDUX_STATE__ || localStorage
   ```

## 三、下一步计划

### 验证阶段 (Priority 1)

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 1.1 | 在浏览器打开 http://10.31.133.114:3009 并登录 | 页面正常显示 |
| 1.2 | 打开开发者工具 → Console | Console 可用 |
| 1.3 | 执行 `document.body.innerText.includes('Anthropic')` | 返回 true |
| 1.4 | 打开渠道管理页面，点击"新建渠道" | 弹出新建渠道对话框 |
| 1.5 | 检查渠道类型下拉框 | 应显示 "Anthropic 兼容" 选项 |

### 缓存清除（如果选项未显示）

1. **Chrome**: Ctrl+Shift+Delete → 清除缓存 → Ctrl+Shift+R 强制刷新
2. **或使用隐身模式** 访问页面
3. **清除 LocalStorage**: Application → Local Storage → Clear

### 测试阶段 (Priority 2)

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 2.1 | 创建新渠道，选择 "Anthropic 兼容" | 渠道创建成功 |
| 2.2 | 配置 Base URL (vLLM 地址) | 保存成功 |
| 2.3 | 测试 API: `curl -X POST http://localhost:3009/v1/messages` | 返回正常响应 |

### 端到端测试 (Priority 3)

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 3.1 | 配置 Claude Code 使用 One API 的 /v1/messages | 连接成功 |
| 3.2 | 发送测试消息 | 正常响应 |

## 四、待确认事项

1. **确认使用的主题**：用户使用的是 Default 主题还是 Berry 主题？
2. **确认浏览器**：Chrome / Firefox / Edge？
3. **确认服务端口**：当前是 3009 端口吗？

## 五、技术细节

### 适配器关键代码

文件: [`relay/adaptor/anthropiccompatible/adaptor.go`](relay/adaptor/anthropiccompatible/adaptor.go)

- **URL 构建**: `{BaseURL}/v1/messages`
- **Headers**: 透传 `x-api-key`, `anthropic-version`, `anthropic-beta`
- **请求体**: 直接透传，不做转换
- **响应**: 直接透传所有 headers 和 body

### 前端选项定义

- **Default 主题**: [`web/default/src/constants/channel.constants.js:11`](web/default/src/constants/channel.constants.js:11)
  ```javascript
  {key: 56, text: 'Anthropic 兼容', value: 56, color: 'black', description: '支持 Anthropic 格式 /v1/messages API'}
  ```

- **Berry 主题**: [`web/berry/src/constants/ChannelConstants.js:14-20`](web/berry/src/constants/ChannelConstants.js:14-20)
  ```javascript
  56: {
    key: 56,
    text: 'Anthropic 兼容',
    value: 56,
    color: 'primary',
    description: '支持 Anthropic 格式 /v1/messages API'
  }
  ```

---

**建议**: 请先尝试清除浏览器缓存并强制刷新页面（Ctrl+Shift+R），然后检查是否能看到 "Anthropic 兼容" 选项。如果仍有问题，请告诉我你使用的浏览器和主题类型。