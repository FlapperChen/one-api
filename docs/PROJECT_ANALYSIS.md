# One API 项目分析

> 生成日期: 2026-04-17
> 项目地址: https://github.com/songquanpeng/one-api

## 项目定位

**One API** 是一个用 Go 语言开发的 LLM（大型语言模型）API 网关与管理系统。通过统一的 OpenAI API 格式，可以访问 30+ 种不同的大模型，实现"开箱即用"。

## 核心功能

| 功能 | 说明 |
|------|------|
| **统一接口** | 标准 OpenAI API 格式，一次接入多模型 |
| **多模型支持** | OpenAI、Claude、Gemini、文心一言、通义千问等 30+ 种 |
| **负载均衡** | 自动选择可用渠道，支持权重/优先级配置 |
| **配额管理** | 精细的用量控制和预扣配额机制 |
| **令牌管理** | 支持多 Token、IP 限制、模型限制 |
| **渠道测试** | 自动检测渠道可用性 |

## 项目架构

```
用户请求 → Router → Middleware(认证/限流/分发) → Controller → Relay → Adaptor → 下游 LLM
                                    ↓
                              Model (数据库)
```

### 关键目录结构

| 目录 | 作用 |
|------|------|
| `/router` | API 路由定义 (Web/API/Relay/Dashboard) |
| `/controller` | 请求处理逻辑 (用户、渠道、令牌、中继等) |
| `/model` | 数据模型 (User/Channel/Token/Log/Ability) |
| `/relay/adaptor` | LLM 提供商适配器 (30+ 种) |
| `/middleware` | 请求处理中间件 (认证、限流、分发、日志) |
| `/common` | 通用工具 (配置、日志、Redis、验证等) |
| `/web` | 前端 React 代码 |
| `/monitor` | 监控与指标 |
| `/scripts` | 脚本工具 |

### 启动流程 (main.go)

```go
1. common.Init()                    # 初始化通用组件
2. logger.SetupLogger()             # 设置日志系统
3. model.InitDB() & InitLogDB()     # 初始化数据库
4. CreateRootAccountIfNeed()        # 创建根账户
5. common.InitRedisClient()         # 初始化 Redis 缓存
6. model.InitOptionMap()            # 加载系统配置
7. InitChannelCache()               # 初始化渠道缓存
8. SyncOptions() & SyncChannelCache()  # 后台同步线程
9. AutomaticallyTestChannels()      # 渠道自动测试
10. openai.InitTokenEncoders()      # 初始化 Token 编码器
11. i18n.Init()                     # 国际化初始化
12. gin.New() & SetRouter()         # 启动 HTTP 服务器
```

## API 路由

### 主要路由组

```
/api/*              → Web 管理 API (用户、渠道、令牌、系统)
/v1/*               → OpenAI 兼容 API (中继请求)
/v1/models          → 模型列表
/v1/chat/completions → 聊天补全 (核心)
/dashboard/*       → 管理面板路由
```

### 请求处理流程

```
用户请求 (Bearer Token)
    ↓
TokenAuth() 中间件 → 验证令牌有效性、获取用户信息
    ↓
Distribute() 中间件 →
  - 获取用户分组
  - 根据模型查找可用渠道 (负载均衡)
  - 设置渠道信息到 Context
    ↓
Relay() 控制器
    ↓
relayHelper() → 根据路由模式选择处理方式:
  - RelayTextHelper()    → 文本/聊天请求
  - RelayImageHelper()   → 图像生成
  - RelayAudioHelper()   → 音频处理
  - RelayProxyHelper()   → 代理模式
    ↓
获取对应的 Adaptor (根据渠道类型)
    ↓
1. ConvertRequest()  → 转换请求格式
2. DoRequest()       → 发送请求到下游 API
3. DoResponse()      → 处理响应、计算用量
4. 返回结果给用户
    ↓
计量 & 配额管理:
  - 预扣配额 (preConsumeQuota)
  - 返回用量 (postConsumeQuota)
  - 失败重试机制
```

## 支持的 LLM 提供商

### 完整列表 (30+)

| 类型 | 提供商 | 适配器目录 |
|------|--------|-----------|
| **OpenAI 系列** | OpenAI, Azure OpenAI | `relay/adaptor/openai/` |
| **Anthropic** | Claude (含 AWS Claude) | `relay/adaptor/anthropic/`, `relay/adaptor/aws/claude/` |
| **Google** | PaLM2, Gemini | `relay/adaptor/gemini/`, `relay/adaptor/palm/`, `relay/adaptor/geminiv2/` |
| **国内厂商** | 百度文心一言, 阿里通义千问, 讯飞星火, 智谱ChatGLM, 腾讯混元, 字节豆包 | `relay/adaptor/baidu/`, `relay/adaptor/baiduv2/`, `relay/adaptor/ali/`, `relay/adaptor/alibailian/`, `relay/adaptor/xunfei/`, `relay/adaptor/xunfeiv2/`, `relay/adaptor/zhipu/`, `relay/adaptor/tencent/`, `relay/adaptor/doubao/` |
| **AI 初创** | Moonshot AI, 百川大模型, MiniMax, 零一万物, 阶跃星辰 | `relay/adaptor/moonshot/`, `relay/adaptor/baichuan/`, `relay/adaptor/minimax/`, `relay/adaptor/lingyiwanwu/`, `relay/adaptor/stepfun/` |
| **开源/本地** | Ollama, DeepSeek, Mistral | `relay/adaptor/ollama/`, `relay/adaptor/deepseek/`, `relay/adaptor/mistral/` |
| **代理服务** | OpenRouter, Cloudflare Workers AI, TogetherAI, SiliconFlow, novita.ai | `relay/adaptor/openrouter/`, `relay/adaptor/cloudflare/`, `relay/adaptor/togetherai/`, `relay/adaptor/siliconflow/`, `relay/adaptor/novita/` |
| **其他** | Groq, Cohere, Coze, DeepL, xAI, Replicate | `relay/adaptor/groq/`, `relay/adaptor/cohere/`, `relay/adaptor/coze/`, `relay/adaptor/deepl/`, `relay/adaptor/xai/`, `relay/adaptor/replicate/` |

### 特殊适配器

- **AWS Claude** (`relay/adaptor/aws/claude/`) - 通过 AWS Bedrock 访问 Claude
- **AWS Llama3** (`relay/adaptor/aws/llama3/`) - 通过 AWS Bedrock 访问 Llama 3
- **AI Proxy** (`relay/adaptor/aiproxy/`) - AI 代理服务
- **360 智脑** (`relay/adaptor/ai360/`) - 360 智能助手

## 中间件

| 中间件 | 功能 |
|--------|------|
| `TokenAuth()` | 验证 API 令牌，获取用户、分组信息 |
| `Distribute()` | 负载均衡 - 为请求分配可用渠道 |
| `RateLimit()` | 限流控制 (API/Web/上传/下载/关键操作) |
| `CORS()` | 跨域资源共享 |
| `GzipDecodeMiddleware()` | Gzip 解压 |
| `RequestId()` | 请求追踪 ID |
| `Language()` | 多语言支持 |
| `Cache()` | 缓存处理 |
| `Recover()` | 错误恢复 |

## 数据模型

### 核心数据表

1. **User** - 用户管理
   - 字段：username, password, role, quota, used_quota, group, email, GitHub/WeChat/Lark ID

2. **Channel** - LLM 渠道配置
   - 字段：type, key, status, base_url, balance, models, group, model_mapping, config, priority

3. **Token** - API 访问令牌
   - 字段：user_id, key, name, quota, remain_quota, expire_time, allowed_ips, allowed_models

4. **Log** - 请求日志
   - 记录每次 API 调用的用量、耗时、状态等

5. **Ability** - 渠道能力映射
   - 记录每个渠道支持的模型列表

6. **Redemption** - 兑换码
   - 批量生成充值码

## 配额与计费系统

```python
配额计算公式:
额度 = 分组倍率 × 模型倍率 × (提示 Token 数 + 补全 Token 数 × 补全倍率)
```

### 特性

- 预扣配额机制 (默认 500)
- 模型倍率 (与官方一致)
- 分组倍率 (可自定义)
- 失败自动重试 (默认 3 次)
- 根据成功率自动禁用渠道

## 高级特性

1. **多机部署**: 主从架构，支持 Redis 缓存同步
2. **负载均衡**: 权重、优先级、智能选择
3. **模型映射**: 重定向/转换模型名称
4. **系统令牌**: 管理 API 可通过令牌扩展功能
5. **第三方登录**: GitHub/Lark/WeChat/OIDC
6. **主题切换**: 支持多种 UI 主题
7. **国际化**: 多语言支持 (i18n)

## 技术栈

- **语言**: Go (Gin 框架)
- **数据库**: SQLite/MySQL/PostgreSQL
- **缓存**: Redis
- **前端**: React (web 目录)
- **部署**: Docker, Docker Compose

## 总结

One API 是一个功能完善的 **LLM API 网关系统**，通过统一的 OpenAI 兼容接口，实现了：

- 多提供商集成 (30+)
- 统一认证与授权
- 智能负载均衡
- 精细的配额管理
- 完整的日志与监控

其架构清晰、模块化程度高，支持灵活的扩展和定制，非常适合需要统一管理多个 LLM 服务的场景。

## 相关文件

- [README.md](../README.md) - 项目主文档
- [API.md](API.md) - API 接口文档
- [main.go](../main.go) - 入口文件
- [relay/adaptor/](../relay/adaptor/) - LLM 适配器源码