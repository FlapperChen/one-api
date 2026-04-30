# One API 数据库迁移方案

## 概述

提供从 Docker MySQL 迁移到 PostgreSQL 的完整解决方案。

## 文件结构

```
scripts/
├── migrate_mysql_pgloader.py   # 实际迁移执行脚本 (可独立运行)
├── migration.py                # 主脚本 (菜单流程控制)
├── MIGRATION_README.md         # 文档
└── offline_packages/           # 离线安装包
    ├── apt/                    # 系统包
    └── python/                 # Python 包
```

## 快速开始

### 方式一：使用主脚本 (推荐)

```bash
cd /home/bmc/sd1/CODE/one-api/scripts

# 检查依赖
python3 migration.py check

# 创建虚拟环境
python3 migration.py venv

# 安装依赖
python3 migration.py install

# 自动执行 (检查 -> 虚拟环境 -> 安装 -> 迁移)
python3 migration.py auto
```

### 方式二：手动执行迁移

如果环境已配置好，可以直接运行：

```bash
cd /home/bmc/sd1/CODE/one-api/scripts
python3 migrate_mysql_pgloader.py
```

## 使用流程

### 1. 依赖检查

```bash
python3 migration.py check
```

检查内容：
- 系统命令: bash, curl, wget
- Python: python3, pip3
- Python 模块: pymysql, psycopg2
- Docker (可选)
- PostgreSQL 客户端 (可选)

### 2. 创建虚拟环境 (可选)

```bash
python3 migration.py venv
```

创建 `venv_migration` 虚拟环境，隔离依赖。

### 3. 安装依赖

```bash
python3 migration.py install
```

安装 Python 依赖：
- pymysql (MySQL 驱动)
- psycopg2-binary (PostgreSQL 驱动)

### 4. 执行迁移

```bash
python3 migration.py migrate
# 或
python3 migration.py auto
```

在确认环节会显示：
```
你可以选择:
  1) 直接执行迁移 (自动调用 migrate_mysql_pgloader.py)
  2) 手动执行: python3 migrate_mysql_pgloader.py
  3) 取消
```

## 手动迁移

如果不想使用主脚本，可以直接调用迁移脚本：

```bash
cd /home/bmc/sd1/CODE/one-api/scripts

# 确保依赖已安装
pip3 install pymysql psycopg2-binary

# 执行迁移
python3 migrate_mysql_pgloader.py
```

## 离线使用

### 1. 在有网络的环境下载离线包

```bash
python3 migration.py download
```

### 2. 复制到目标机器

```bash
scp offline_packages.tar.gz user@target:/path/
```

### 3. 在目标机器执行

```bash
# 解包
python3 migration.py unpack

# 自动执行
python3 migration.py auto
```

## 依赖要求

### 必须

- Python 3.8+
- pymysql
- psycopg2-binary

### 可选

- Docker (从 Docker 容器迁移时需要)
- PostgreSQL 客户端 (psql)

### 离线包内容

```
offline_packages/
├── apt/
│   ├── postgresql-client_16+257build1.1_all.deb
│   └── pgloader_3.6.10-1build2_amd64.deb
└── python/
    ├── pymysql-1.1.2-py3-none-any.whl
    └── psycopg2_binary-2.9.11-...manylinux2014_x86_64.whl
```

## 数据库配置

脚本中的默认配置：

```python
POSTGRES_CONFIG = {
    'user': 'postgres',
    'password': 'NCbmc@123',
    'host': 'localhost',
    'port': 5432,
    'database': 'oneapi'
}
```

## 迁移后启动 one-api

```bash
# 设置环境变量
export SQL_DSN="postgres://postgres:NCbmc@123@localhost:5432/oneapi"

# 启动 one-api
cd /home/bmc/sd1/CODE/one-api
./one-api --port 3009
```

## 支持的操作系统

- ✅ Ubuntu 22.04 (Jammy)
- ✅ Ubuntu 24.04 (Noble)

## 故障排除

### 问题: import 错误

```bash
# 重新安装依赖
pip3 install --break-system-packages pymysql psycopg2-binary
```

### 问题: PostgreSQL 连接失败

检查 PostgreSQL 服务状态：
```bash
sudo systemctl status postgresql
```

### 问题: Docker 容器无法访问

检查 Docker 容器状态：
```bash
docker ps | grep mysql
```

### 问题: 迁移后消费记录无法保存 (duplicate key error)

PostgreSQL 序列不同步导致。迁移脚本会在迁移结束时自动同步序列，但以下情况可能导致序列再次不同步：

- 通过 web UI 或 API 新增用户、令牌等数据
- 手动导入数据后
- 增量同步后新增的数据

**自动修复**：重新运行迁移脚本的验证步骤（会自动同步序列）：

```bash
cd /home/bmc/sd1/CODE/one-api/scripts
python3 migrate_mysql_pgloader.py --step 8 --auto
```

**手动修复**：执行以下 SQL 同步所有表的序列：

```bash
PGPASSWORD='NCbmc@123' psql -h localhost -U postgres -d oneapi -c "
SELECT setval('logs_id_seq', (SELECT MAX(id) FROM logs));
SELECT setval('users_id_seq', (SELECT MAX(id) FROM users));
SELECT setval('channels_id_seq', (SELECT MAX(id) FROM channels));
SELECT setval('tokens_id_seq', (SELECT MAX(id) FROM tokens));
SELECT setval('redemptions_id_seq', (SELECT MAX(id) FROM redemptions));
SELECT setval('options_id_seq', (SELECT MAX(id) FROM options));
SELECT setval('abilities_id_seq', (SELECT MAX(id) FROM abilities));
"
```

### 问题: 新建令牌时报 "duplicate key value violates unique constraint"

这是 tokens 表序列不同步导致的。执行以下命令修复：

```bash
PGPASSWORD='NCbmc@123' psql -h localhost -U postgres -d oneapi -c "SELECT setval('tokens_id_seq', (SELECT MAX(id) FROM tokens));"
```

### 问题: 创建用户后无法正常使用

可能是 users 或 abilities 表序列不同步：

```bash
PGPASSWORD='NCbmc@123' psql -h localhost -U postgres -d oneapi -c "
SELECT setval('users_id_seq', (SELECT MAX(id) FROM users));
SELECT setval('abilities_id_seq', (SELECT MAX(id) FROM abilities));
"
```

## 迁移结果示例

```
=== 依赖检查 ===
  ✓ bash
  ✓ python3
  ✓ pymysql (1.4.6)
  ✓ psycopg2 (2.9.11)

=== 迁移完成 ===
数据统计:
  users: 9
  channels: 1
  tokens: 19
  options: 2
  redemptions: 0
  abilities: 3
  logs: 28831

启动 one-api:
  export SQL_DSN="postgres://postgres:NCbmc@123@localhost:5432/oneapi"
  cd /home/bmc/sd1/CODE/one-api
  ./one-api --port 3009
```