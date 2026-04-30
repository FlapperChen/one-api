# 并发控制设置 Web 界面调试文档

## 问题描述

**症状**：运营设置页面中"并发控制与负载管理"区块显示不完整：
- 请求去重、用户最大并发数等文本显示正常
- 但开关按钮无法点击/选择
- 自动并发限制的详细配置不显示
- GPU监控配置区域不显示

---

## 已完成的工作

### 1. 功能实现 (后端)

| 文件 | 说明 |
|------|------|
| `model/concurrency.go` | 用户并发配置模型 |
| `model/request_cache.go` | 请求缓存模型 + 去重算法 |
| `common/concurrency/limiter.go` | 并发限制核心 |
| `common/concurrency/load_evaluator.go` | 4因子负载评估算法 |
| `monitor/gpu/vllm_monitor.go` | GPU KV Cache监控 |
| `middleware/request_dedup.go` | 请求去重中间件 |
| `controller/misc.go` | 新增 GetAutoConcurrencyLimit API |
| `router/api.go` | 注册 /api/system/auto-concurrency-limit |
| `model/option.go` | 配置项管理 |
| `common/config/config.go` | 因子权重等配置 |

### 2. 功能实现 (前端)

| 文件 | 说明 |
|------|------|
| `web/default/src/components/OperationSetting.js` | 重写并发区块 |
| `web/default/src/locales/zh/translation.json` | 中文翻译 |
| `web/default/src/locales/en/translation.json` | 英文翻译 |

### 3. 修复记录

| 日期 | 问题 | 修复 |
|------|------|------|
| 04-30 | 翻译key冲突（auto_enable重复） | 改为 unique key：manual_limit_enable, auto_limit_enable |
| 04-30 | Checkbox状态更新异常 | 修复 updateOption 函数逻辑 |
| 04-30 | 配置默认值被API覆盖 | 添加默认配置合并逻辑 |
| 04-30 | 翻译key缺失 | 添加 cache_ttl_placeholder |

---

## 可能的问题原因

### ✅ 已排除：旧服务进程

**结论**：3008是生产环境，永远不在上面操作。问题与旧服务无关。

### 原因1：前端构建未成功

### 原因2：Semantic UI Checkbox label 使用 JSX 元素

**已确认**：组件已渲染，但 label 元素为 undefined

**Console 诊断结果**：
```
并发区块 Checkbox 0-3: hasLabel: false ❌
其他区块 Checkbox 4-9: hasLabel: true  ✅
```

**根因**：Semantic UI React (v2.1.3) 对 `label` prop 使用 JSX 元素（如 `<b>{t(...)}</b>`）处理有问题，导致 label 元素不渲染。

**问题代码**：
```jsx
// ❌ 错误写法
<Form.Checkbox
  label={<b>{t('setting.operation.concurrency.dedup_enable')}</b>}
/>

// ✅ 正确写法
<Form.Checkbox
  label={t('setting.operation.concurrency.dedup_enable')}
/>
```

**解决方案**：
1. 将所有 `label={<b>{...}</b>}` 改为 `label={...}`
2. 如需加粗效果，使用其他方式实现（如在 label 后单独显示加粗文字）

### ✅ 已解决

**验证**：
```bash
# 检查构建文件时间戳
ls -la /home/bmc/sd1/CODE/one-api/web/build/default/static/js/main.*.js

# 检查是否包含新代码
grep -o "manual_limit_enable\|auto_limit_enable" /home/bmc/sd1/CODE/one-api/web/build/default/static/js/main.*.js
```

### 原因3：浏览器缓存

**验证**：按 F12 打开开发者工具 → Network → 勾选 "Disable cache" → 刷新页面

---

## 诊断计划

### 步骤1：确认访问的端口

1. 打开浏览器，按 F12 打开开发者工具
2. 在 Console 中输入：
   ```javascript
   // 查看当前页面URL
   console.log(window.location.href)

   // 查看页面加载的JS文件
   performance.getEntriesByType('resource').filter(r => r.name.includes('.js')).forEach(r => console.log(r.name))
   ```

3. 如果 URL 是 `http://localhost:3008/...`，说明访问的是旧服务

### 步骤2：检查前端构建

```bash
cd /home/bmc/sd1/CODE/one-api/web/default
# 清理并重新构建
rm -rf build
npm run build
```

### 步骤3：停止旧服务

```bash
# 停止旧服务（需要root权限）
sudo fuser -k 3008/tcp

# 只保留新服务运行
ps aux | grep one-api | grep -v grep
```

### 步骤4：清除浏览器缓存

1. Chrome: F12 → Network → 勾选 "Disable cache"
2. 或者: 设置 → 隐私 → 清除缓存

---

## 需要用户收集的信息

请在浏览器中按 F12 打开开发者工具，依次执行以下操作：

### 1. Console 中输入

```javascript
// 1. 查看API响应
fetch('/api/option/').then(r => r.json()).then(d => {
  console.log('Options count:', d.data.length);
  // 查找并发相关配置
  d.data.filter(item => 
    item.key.includes('Concurrency') || 
    item.key.includes('Dedup') ||
    item.key.includes('GPU')
  ).forEach(item => console.log(item.key, '=', item.value));
});
```

### 2. Network 中查找

1. 刷新页面
2. 查找 `/api/option/` 请求
3. 查看响应中是否包含以下 key：
   - `EnableRequestDeduplication`
   - `EnableManualConcurrencyLimit`
   - `EnableAutoConcurrencyLimit`
   - `EnableGPUMonitoring`
   - `DurationFactorWeight`

### 3. 检查页面渲染

在 Console 中输入：
```javascript
// 查看页面中的Checkbox数量
document.querySelectorAll('.ui.checkbox').length

// 查看并发区块是否存在
document.querySelectorAll('h3').forEach(h => {
  if (h.textContent.includes('并发')) console.log('Found:', h.textContent);
});
```

### 4. 查看当前端口

```javascript
window.location.port
```

---

## 预期结果 vs 实际结果

### 预期结果（设计图）

```
┌─────────────────────────────────────────┐
│ 并发控制与负载管理                       │
├─────────────────────────────────────────┤
│ [x] 启用请求去重                         │
│     说明：识别客户端重试...              │
│     缓存TTL: [30]                        │
├─────────────────────────────────────────┤
│ [ ] 启用手动并发限制                      │
│     用户最大并发: [5] 等待超时: [30]...  │
├─────────────────────────────────────────┤
│ [ ] 启用自动并发限制                      │
│     当前: 3  实际限制: 3  负载: 正常     │
│     [权重配置区域]                       │
├─────────────────────────────────────────┤
│ [ ] 启用GPU监控                          │
│     vLLM API: [...]  KV Cache: [85][95] │
└─────────────────────────────────────────┘
```

### 实际结果（当前）

```
┌─────────────────────────────────────────┐
│ 并发控制与负载管理                       │
├─────────────────────────────────────────┤
│ 启用请求去重                             │
│ 说明：识别客户端重试...                  │
│ 缓存TTL: [36]                            │
├─────────────────────────────────────────┤
│ 启用手动并发限制                          │
│ 用户最大并发数: [5]                      │
│ 排队等待超时: [30]                       │
│ 检查间隔: [100]                          │
├─────────────────────────────────────────┤
│ 启用自动并发限制                          │
│ 启用GPU监控                              │
└─────────────────────────────────────────┘
```

**差异**：开关按钮和详细配置区域没有显示

---

## 剩余工作清单

| 优先级 | 任务 | 状态 |
|--------|------|------|
| P0 | 确认问题根因 | ✅ 已完成 |
| P1 | 修复 Checkbox label JSX 问题 | ✅ 已完成 |
| P2 | 前端重新构建 | ✅ 已完成 |
| P3 | 部署验证 | ⏳ 待验证 |
| P4 | 测试功能正常 | ⏳ 待验证 |

---

## 文档更新记录

| 日期 | 更新内容 |
|------|----------|
| 2026-04-30 | 创建调试文档 |
| 2026-04-30 | 记录已完成工作和问题 |
| 2026-04-30 | 添加诊断计划 |
| 2026-04-30 | 发现根因：label 使用 JSX 元素导致不渲染 |
| 2026-04-30 | 修复：改为字符串 label，前端已重新构建 |
