# MySQL 到 PostgreSQL 数据库迁移优化方案

> 创建日期: 2026-04-22
> 更新日期: 2026-04-23
> 状态: 基本完成（需数据对齐）

---

## 一、背景

用户需要将 Docker MySQL 中的 one-api 数据迁移到本地 PostgreSQL，要求：
1. 确保原数据稳定运行
2. 确保 PostgreSQL 正常，创建 oneapi 数据库
3. 参考 README 手动部署 one-api (port 3009)
4. 备份 MySQL 和 PostgreSQL 数据库
5. 导出 MySQL 用户信息到 JSON
6. 通过 one-api API 同步用户
7. 同步 token 额度消耗历史到 PostgreSQL
8. 验证测试

---

## 二、完成情况

### 2.1 代码优化

**文件**: [scripts/migrate_mysql_pgloader.py](scripts/migrate_mysql_pgloader.py)

| 步骤 | 命令 | 功能 | 状态 |
|------|------|------|------|
| 1 | `--step 1` | 健康检查（Docker MySQL + PostgreSQL） | ✅ 通过 |
| 2 | `--step 2` | PostgreSQL 准备（创建 oneapi 数据库） | ✅ 通过 |
| 3 | `--step 3` | 启动 one-api (port 3009) | ✅ 通过 |
| 4 | `--step 4` | 数据备份（MySQL + PostgreSQL） | ✅ 通过 |
| 5 | `--step 5` | 导出用户数据到 JSON | ✅ 通过 |
| 6 | `--step 6` | API 同步用户 | ✅ 通过 |
| 7 | `--step 7` | 数据迁移（upsert 模式） | ✅ 通过 |
| 8 | `--step 8` | 验证测试 | ✅ 执行 |

### 2.2 关键改进

1. **Upsert 模式**: 用 `INSERT ... ON CONFLICT DO UPDATE` 替代 `TRUNCATE`，避免数据丢失
2. **智能唯一标识**: users 表用 username，channels 表用 name，其他表用 id
3. **禁用代理**: `os.environ['no_proxy'] = '*'` 避免系统代理干扰
4. **Session 认证**: 使用 session 保持 cookie 获取 access token
5. **密码交互**: 使用 `getpass` 安全输入密码

---

## 三、迁移结果

### 3.1 数据对比（2026-04-23）

| 表 | MySQL | PostgreSQL | 差异 | 说明 |
|-----|-------|------------|------|------|
| users | 14 | 13 | -1 | 1 个用户已存在未更新 |
| tokens | 28 | 30 | +2 | 旧数据中多 2 个 token |
| logs | 54,725 | 54,708 | -17 | 17 条日志 ID 冲突 |

### 3.2 问题原因

1. **Upsert 特性**: `ON CONFLICT` 只在键冲突时更新，如果数据相同则不处理
2. **ID 不一致**: MySQL 和 PostgreSQL 的用户/日志 ID 不同
3. **保留策略**: upsert 默认保留目标数据库中已有的数据

### 3.3 Step 6 API 同步结果

- 创建了 5 个缺失用户: van_gao, luca_jiang, guoln, Marco, luca
- 保留了 PostgreSQL 中已有的 9 个用户

---

## 四、备选迁移策略

### 方案 A: 完全覆盖（推荐用于测试）

```bash
# 1. 备份 PostgreSQL
# 2. TRUNCATE 所有表
# 3. 重新执行迁移
```

### 方案 B: 增量同步

```sql
-- 仅同步 MySQL 中更新的数据
INSERT INTO users (...)
SELECT ... FROM mysql_users
WHERE updated_at > (SELECT MAX(updated_at) FROM users)
ON CONFLICT (username) DO UPDATE SET ...
```

---

## 五、使用说明

### 5.1 基本用法

```bash
cd /home/bmc/sd1/CODE/one-api/scripts

# 完整迁移（自动模式）
python3 migrate_mysql_pgloader.py --auto --full-migration --oneapi-password "NCbmc@123"

# 分步执行
python3 migrate_mysql_pgloader.py --step 1  # 健康检查
python3 migrate_mysql_pgloader.py --step 4  # 数据备份
python3 migrate_mysql_pgloader.py --step 5  # 导出数据
python3 migrate_mysql_pgloader.py --step 6  # API 同步
python3 migrate_mysql_pgloader.py --step 7  # 数据迁移
python3 migrate_mysql_pgloader.py --step 8  # 验证测试
```

### 5.2 备份文件位置

```
scripts/backups/
├── mysql_backup_20260423_*.sql        # MySQL 备份
├── oneapi_backup_20260423_*.dump      # PostgreSQL 备份
└── users_export_20260423_*.json       # 用户数据导出
```

---

## 六、后续工作

- [ ] 决定是否需要完全覆盖迁移（清空 PostgreSQL 重迁）
- [ ] 同步用户配额信息（目前只同步了用户基础信息）
- [ ] 更新 [scripts/MIGRATION_README.md](scripts/MIGRATION_README.md)

---

## 七、相关文件

| 文件 | 说明 |
|------|------|
| [scripts/migrate_mysql_pgloader.py](scripts/migrate_mysql_pgloader.py) | 增强版迁移脚本 |
| [scripts/backups/](scripts/backups/) | 备份文件目录 |

---

## 八、备注

- one-api root 密码: `NCbmc@123`
- 默认端口: 3009
- PostgreSQL oneapi 数据库
- Docker MySQL 容器正常运行中