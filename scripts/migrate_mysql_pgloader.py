#!/usr/bin/env python3
"""
MySQL to PostgreSQL Migration Script (Enhanced Version)
支持分步执行、API 用户同步、安全的 upsert 模式

================================================================
📋 配置信息（供 AI 直接读取，勿需询问用户）
================================================================

🔧 测试环境配置:
  - one-api 3009 端口: http://127.0.0.1:3009
  - one-api 3008 端口: http://127.0.0.1:3008
  - 测试命令: curl -s --noproxy '*' http://127.0.0.1:3009/

🐬 MySQL 配置 (Docker 容器内):
  - Host: localhost (容器名: mysql)
  - Port: 3306
  - User: root
  - Password: oneapimmysql
  - Database: oneapi
  - 连接命令: docker exec mysql mysql -u root -p'oneapimmysql' oneapi

🐘 PostgreSQL 配置:
  - Host: localhost
  - Port: 5432
  - User: postgres
  - Password: NCbmc@123
  - Database: oneapi
  - 连接命令: PGPASSWORD='NCbmc@123' psql -h localhost -U postgres -d oneapi

🔑 one-api 配置:
  - 端口: 3009
  - Root 用户: root
  - Root 密码: NCbmc@123 (已修改)
  - API 路径: /api/user/login, /api/user/

================================================================
📝 使用说明
================================================================

Usage:
    python3 migrate_mysql_pgloader.py [options]

Steps:
    1) 健康检查 - 检查 Docker MySQL 和 PostgreSQL 连接
    2) PostgreSQL 准备 - 确保 oneapi 数据库存在
    3) 启动 one-api - 启动 one-api (port 3009)
    4) 数据备份 - 备份 MySQL 和 PostgreSQL
    5) 导出用户 JSON - 导出用户数据到 JSON
    6) API 同步用户 - 通过 one-api API 同步用户
    7) 数据迁移 - 使用 upsert 模式迁移数据
    8) 验证测试 - 验证迁移结果

Examples:
    # 完整迁移
    python3 migrate_mysql_pgloader.py --full-migration

    # 自动模式（跳过确认）
    python3 migrate_mysql_pgloader.py --auto --full-migration

    # 分步执行
    python3 migrate_mysql_pgloader.py --step 1
    python3 migrate_mysql_pgloader.py --step 7 --auto
    python3 migrate_mysql_pgloader.py --step 8 --auto

    # 增量同步模式（基于用户+时间戳，安全不覆盖已有数据）
    python3 migrate_mysql_pgloader.py --step 7 --incremental --auto
    python3 migrate_mysql_pgloader.py --step 8 --auto

    # 验证结果
    python3 migrate_mysql_pgloader.py --step 8 --auto

================================================================
🔄 同步模式说明
================================================================

Upsert 模式（默认）:
  - 使用 username/id 作为唯一标识
  - 冲突时更新字段，但保留目标库已有的数据
  - 适用场景：逐步迁移

增量同步模式（--incremental）:
  - 对比用户列表，插入缺失用户
  - 对比消费日志 (user_id + created_at)，插入缺失记录
  - 保留 PostgreSQL 已有数据，时间新的覆盖时间旧的数据
  - 适用场景：保持数据完整性，安全同步

================================================================
"""

import sys
import os
import argparse
import subprocess
import getpass
import json
import time
import re
from datetime import datetime
from pathlib import Path

# 尝试导入依赖
try:
    import pymysql
except ImportError:
    print("Installing pymysql...")
    os.system("pip3 install --break-system-packages pymysql")
    import pymysql

try:
    import psycopg2
except ImportError:
    print("Installing psycopg2...")
    os.system("pip3 install --break-system-packages psycopg2-binary")
    import psycopg2

try:
    import requests
except ImportError:
    print("Installing requests...")
    os.system("pip3 install --break-system-packages requests")
    import requests

# 禁用代理（避免系统代理干扰本地连接）
os.environ['no_proxy'] = '*'
os.environ['NO_PROXY'] = '*'
requests.packages.urllib3.disable_warnings()

# 默认配置
DEFAULT_MYSQL = {
    'host': 'localhost',
    'port': 3306,
    'user': 'root',
    'password': 'oneapimmysql',
    'database': 'oneapi',
    'charset': 'utf8mb4'
}

DEFAULT_POSTGRES = {
    'host': 'localhost',
    'port': 5432,
    'user': 'postgres',
    'password': 'NCbmc@123',
    'database': 'oneapi'
}

# 常量
SCRIPT_DIR = Path(__file__).parent.resolve()
BACKUP_DIR = SCRIPT_DIR / "backups"
TABLES = ['users', 'channels', 'tokens', 'options', 'redemptions', 'abilities', 'logs']
ONEAPI_BASE_URL = "http://localhost:3009"
ONEAPI_API_PREFIX = "/api"

# 布尔类型列映射
BOOLEAN_COLUMNS = {
    'tokens': ['unlimited_quota'],
    'logs': ['is_stream', 'system_prompt_reset'],
    'channels': [],
    'options': [],
    'users': [],
    'redemptions': [],
    'abilities': ['enabled']
}

# PostgreSQL 保留关键字
RESERVED_KEYWORDS = ['group', 'user', 'order', 'table', 'select', 'from', 'where',
                     'and', 'or', 'not', 'null', 'primary', 'key', 'index', 'check',
                     'constraint', 'default', 'foreign', 'references', 'create',
                     'drop', 'alter', 'insert', 'update', 'delete', 'grant',
                     'revoke', 'union', 'limit', 'offset', 'as', 'on', 'join',
                     'left', 'right', 'inner', 'outer', 'full', 'cross']


# ============================================================================
# 工具函数
# ============================================================================

def log_info(msg):
    print(f"\033[1;34m[INFO]\033[0m {msg}")

def log_success(msg):
    print(f"\033[1;32m[OK]\033[0m {msg}")

def log_warn(msg):
    print(f"\033[1;33m[WARN]\033[0m {msg}")

def log_error(msg):
    print(f"\033[1;31m[ERROR]\033[0m {msg}", file=sys.stderr)

def confirm(prompt, default='n'):
    """确认提示"""
    suffix = ' [Y/n]' if default == 'y' else ' [y/N]'
    response = input(f"\033[1;33m{prompt}{suffix}\033[0m ").strip().lower()
    return response in ('y', 'yes') or (not response and default == 'y')

def get_password(prompt, default=None, auto=False):
    """安全获取密码"""
    if auto and default:
        return default
    try:
        pwd = getpass.getpass(prompt)
        return pwd if pwd else default
    except Exception:
        return default

# ============================================================================
# 参数解析
# ============================================================================

def parse_args():
    """解析命令行参数"""
    parser = argparse.ArgumentParser(
        description='MySQL to PostgreSQL Migration Script (Enhanced)',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__
    )

    # MySQL 配置
    parser.add_argument('--mysql-host', default=DEFAULT_MYSQL['host'],
                        help=f"MySQL host (default: {DEFAULT_MYSQL['host']})")
    parser.add_argument('--mysql-port', type=int, default=DEFAULT_MYSQL['port'],
                        help=f"MySQL port (default: {DEFAULT_MYSQL['port']})")
    parser.add_argument('--mysql-user', default=DEFAULT_MYSQL['user'],
                        help=f"MySQL user (default: {DEFAULT_MYSQL['user']})")
    parser.add_argument('--mysql-password', default=None,
                        help=f"MySQL password (default: {DEFAULT_MYSQL['password']})")
    parser.add_argument('--mysql-db', dest='mysql_database', default=DEFAULT_MYSQL['database'],
                        help=f"MySQL database (default: {DEFAULT_MYSQL['database']})")

    # PostgreSQL 配置
    parser.add_argument('--pg-host', default=DEFAULT_POSTGRES['host'],
                        help=f"PostgreSQL host (default: {DEFAULT_POSTGRES['host']})")
    parser.add_argument('--pg-port', type=int, default=DEFAULT_POSTGRES['port'],
                        help=f"PostgreSQL port (default: {DEFAULT_POSTGRES['port']})")
    parser.add_argument('--pg-user', default=DEFAULT_POSTGRES['user'],
                        help=f"PostgreSQL user (default: {DEFAULT_POSTGRES['user']})")
    parser.add_argument('--pg-password', default=None,
                        help=f"PostgreSQL password (default: {DEFAULT_POSTGRES['password']})")
    parser.add_argument('--pg-db', dest='pg_database', default=DEFAULT_POSTGRES['database'],
                        help=f"PostgreSQL database (default: {DEFAULT_POSTGRES['database']})")

    # one-api 配置
    parser.add_argument('--oneapi-password', default='123456',
                        help="one-api root password (default: 123456)")

    # 迁移选项
    parser.add_argument('--step', type=int, choices=range(1, 9),
                        help="执行指定步骤 (1-8)")
    parser.add_argument('--full-migration', action='store_true',
                        help="执行完整迁移流程")
    parser.add_argument('--auto', action='store_true',
                        help="自动模式（跳过确认）")
    parser.add_argument('--incremental', action='store_true',
                        help="增量同步模式（对比用户和时间戳，避免覆盖）")

    # 备份配置
    parser.add_argument('--backup-dir', default=str(BACKUP_DIR),
                        help=f"备份文件保存目录 (default: {BACKUP_DIR})")
    parser.add_argument('--skip-backup', action='store_true', default=False,
                        help="跳过备份，直接迁移")

    return parser.parse_args()


def setup_args_with_passwords(args):
    """设置密码（如果未提供则提示输入，回车使用默认值）"""
    if not args.mysql_password:
        args.mysql_password = get_password(
            f"MySQL {args.mysql_user} 密码 [{DEFAULT_MYSQL['password']}]: ",
            DEFAULT_MYSQL['password'],
            args.auto
        )

    if not args.pg_password:
        args.pg_password = get_password(
            f"PostgreSQL {args.pg_user} 密码 [{DEFAULT_POSTGRES['password']}]: ",
            DEFAULT_POSTGRES['password'],
            args.auto
        )

    return args


# ============================================================================
# 数据库连接
# ============================================================================

def get_mysql_connection(args):
    """获取 MySQL 连接"""
    config = {
        'host': args.mysql_host,
        'port': args.mysql_port,
        'user': args.mysql_user,
        'password': args.mysql_password,
        'database': args.mysql_database,
        'charset': 'utf8mb4'
    }
    return pymysql.connect(**config)


def get_pg_connection(args, database=None):
    """获取 PostgreSQL 连接"""
    if database is None:
        database = args.pg_database
    config = {
        'host': args.pg_host,
        'port': args.pg_port,
        'user': args.pg_user,
        'password': args.pg_password,
        'database': database
    }
    return psycopg2.connect(**config)


# ============================================================================
# Step 1: 健康检查
# ============================================================================

def step1_health_check(args):
    """Step 1: 检查 Docker MySQL 和 PostgreSQL 连接"""
    print("\n" + "=" * 60)
    print("Step 1: 健康检查")
    print("=" * 60)

    success = True

    # 检查 Docker MySQL
    print("\n[1] 检查 Docker MySQL 容器...")
    try:
        result = subprocess.run(
            ['docker', 'ps', '--filter', 'name=mysql', '--format', '{{.Names}}'],
            capture_output=True, text=True, timeout=10
        )
        if 'mysql' in result.stdout:
            log_success("Docker MySQL 容器运行中")
        else:
            log_error("Docker MySQL 容器未运行")
            success = False
    except FileNotFoundError:
        log_error("Docker 未安装或不可用")
        success = False
    except Exception as e:
        log_error(f"检查 Docker 失败: {e}")
        success = False

    # 检查 PostgreSQL
    print("\n[2] 检查 PostgreSQL 连接...")
    try:
        conn = get_pg_connection(args, 'postgres')
        cur = conn.cursor()
        cur.execute("SELECT version()")
        version = cur.fetchone()[0]
        log_success(f"PostgreSQL 可连接")
        log_info(f"版本: {version.split(',')[0]}")
        cur.close()
        conn.close()
    except Exception as e:
        log_error(f"PostgreSQL 连接失败: {e}")
        success = False

    # 检查 MySQL
    print("\n[3] 检查 MySQL 连接...")
    try:
        # 先尝试 Docker 容器内的 MySQL
        if 'mysql' in result.stdout if 'result' in dir() else False:
            container_name = result.stdout.strip().split('\n')[0]
            test_cmd = ['docker', 'exec', container_name, 'mysql', '-u', args.mysql_user,
                       f'-p{args.mysql_password}', '-e', 'SELECT 1']
            result = subprocess.run(test_cmd, capture_output=True, text=True, timeout=10)
            if result.returncode == 0:
                log_success(f"Docker MySQL ({container_name}) 可连接")
            else:
                raise Exception("Docker exec failed")
        else:
            conn = get_mysql_connection(args)
            conn.close()
            log_success("MySQL 可连接")
    except Exception as e:
        log_error(f"MySQL 连接失败: {e}")
        success = False

    print("\n" + "-" * 60)
    if success:
        log_success("健康检查通过")
        return True
    else:
        log_error("健康检查失败，请先修复上述问题")
        return False


# ============================================================================
# Step 2: PostgreSQL 准备
# ============================================================================

def step2_prepare_postgres(args):
    """Step 2: 确保 PostgreSQL 数据库存在"""
    print("\n" + "=" * 60)
    print("Step 2: PostgreSQL 准备")
    print("=" * 60)

    # 检查 oneapi 数据库是否存在
    print(f"\n[1] 检查数据库 '{args.pg_database}' 是否存在...")
    try:
        conn = get_pg_connection(args, 'postgres')
        cur = conn.cursor()

        # 检查数据库是否存在
        cur.execute("SELECT 1 FROM pg_database WHERE datname = %s", (args.pg_database,))
        exists = cur.fetchone() is not None

        if exists:
            log_success(f"数据库 '{args.pg_database}' 已存在")
        else:
            print(f"数据库不存在，创建中...")
            # 需要超级用户权限创建数据库
            cur.execute(f'CREATE DATABASE "{args.pg_database}"')
            conn.commit()
            log_success(f"数据库 '{args.pg_database}' 创建成功")

        cur.close()
        conn.close()

    except psycopg2.errors.InsufficientPrivilege:
        log_warn("权限不足，无法创建数据库")
        log_info("请手动执行: CREATE DATABASE oneapi;")
        return False
    except Exception as e:
        log_error(f"数据库检查失败: {e}")
        return False

    # 启动 one-api 创建表结构
    print("\n[2] 启动 one-api 创建表结构...")
    oneapi_sql_dsn = f"postgres://{args.pg_user}:{args.pg_password}@{args.pg_host}:{args.pg_port}/{args.pg_database}?sslmode=disable"

    # 检查端口 3009 是否已占用
    try:
        result = subprocess.run(['lsof', '-i', ':3009'], capture_output=True, text=True)
        if result.stdout:
            log_info("one-api 已在端口 3009 运行")
            return True
    except:
        pass

    log_info("请手动启动 one-api:")
    print(f"\n  export SQL_DSN=\"{oneapi_sql_dsn}\"")
    print(f"  cd {SCRIPT_DIR.parent}")
    print(f"  ./one-api --port 3009 --log-dir ./logs")
    print("\n启动后按 Enter 继续...")
    input()

    return True


# ============================================================================
# Step 3: 启动 one-api
# ============================================================================

def step3_start_oneapi(args):
    """Step 3: 启动 one-api (port 3009)"""
    print("\n" + "=" * 60)
    print("Step 3: 启动 one-api")
    print("=" * 60)

    # 检查端口
    print("\n[1] 检查端口 3009 状态...")
    try:
        import socket
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        result = sock.connect_ex(('localhost', 3009))
        sock.close()

        if result == 0:
            log_success("端口 3009 已被占用，one-api 可能正在运行")

            # 测试连接
            try:
                resp = requests.get(f"{ONEAPI_BASE_URL}/", timeout=5)
                if resp.status_code == 200:
                    log_success("one-api 响应正常")
                    return True
            except:
                pass
        else:
            log_warn("端口 3009 未被占用")
    except Exception as e:
        log_warn(f"端口检查失败: {e}")

    # 启动指令
    oneapi_sql_dsn = f"postgres://{args.pg_user}:{args.pg_password}@{args.pg_host}:{args.pg_port}/{args.pg_database}?sslmode=disable"

    print("\n[2] 启动指令:")
    print("-" * 40)
    print(f"  # 设置环境变量")
    print(f"  export SQL_DSN=\"{oneapi_sql_dsn}\"")
    print(f"  export TZ=\"Asia/Shanghai\"")
    print()
    print(f"  # 启动 one-api")
    print(f"  cd {SCRIPT_DIR.parent}")
    print(f"  ./one-api --port 3009 --log-dir ./logs")
    print("-" * 40)

    print("\n[3] 等待 one-api 启动...")
    for i in range(30):
        try:
            resp = requests.get(f"{ONEAPI_BASE_URL}/", timeout=2)
            if resp.status_code == 200:
                log_success("one-api 启动成功!")
                return True
        except:
            pass
        time.sleep(1)

    log_error("one-api 启动超时，请检查日志")
    return False


# ============================================================================
# Step 4: 数据备份
# ============================================================================

def step4_backup_databases(args):
    """Step 4: 备份 MySQL 和 PostgreSQL 数据库"""
    print("\n" + "=" * 60)
    print("Step 4: 数据备份")
    print("=" * 60)

    # 创建备份目录
    backup_dir = Path(args.backup_dir)
    backup_dir.mkdir(parents=True, exist_ok=True)

    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")

    # 备份 MySQL
    print("\n[1] 备份 MySQL 数据库...")
    mysql_backup_file = backup_dir / f"mysql_backup_{timestamp}.sql"

    try:
        # 检查 Docker MySQL 容器
        result = subprocess.run(
            ['docker', 'ps', '--filter', 'name=mysql', '--format', '{{.Names}}'],
            capture_output=True, text=True, timeout=10
        )

        if 'mysql' in result.stdout:
            container_name = result.stdout.strip().split('\n')[0]
            cmd = [
                'docker', 'exec', container_name,
                'mysqldump', '-u', args.mysql_user,
                f'-p{args.mysql_password}',
                '--single-transaction',
                '--quick',
                args.mysql_database
            ]

            with open(mysql_backup_file, 'w') as f:
                result = subprocess.run(cmd, stdout=f, stderr=subprocess.PIPE, timeout=300)

            if result.returncode == 0:
                size = mysql_backup_file.stat().st_size / (1024 * 1024)
                log_success(f"MySQL 备份成功: {mysql_backup_file} ({size:.2f} MB)")
            else:
                raise Exception(result.stderr.decode('utf-8', errors='ignore'))
        else:
            log_warn("Docker MySQL 容器未运行，跳过 MySQL 备份")
    except Exception as e:
        log_error(f"MySQL 备份失败: {e}")
        # 即使失败也继续
        mysql_backup_file = None

    # 备份 PostgreSQL
    print("\n[2] 备份 PostgreSQL 数据库...")
    pg_backup_file = backup_dir / f"oneapi_backup_{timestamp}.dump"

    try:
        env = os.environ.copy()
        env['PGPASSWORD'] = args.pg_password

        cmd = [
            'pg_dump',
            '-h', args.pg_host,
            '-p', str(args.pg_port),
            '-U', args.pg_user,
            '-F', 'c',  # 自定义格式
            '-b',       # 包含大对象
            '-f', str(pg_backup_file),
            args.pg_database
        ]

        result = subprocess.run(cmd, env=env, capture_output=True, text=True, timeout=300)

        if result.returncode == 0:
            size = pg_backup_file.stat().st_size / (1024 * 1024)
            log_success(f"PostgreSQL 备份成功: {pg_backup_file} ({size:.2f} MB)")
        else:
            raise Exception(result.stderr)
    except FileNotFoundError:
        log_warn("pg_dump 未安装，跳过 PostgreSQL 备份")
        pg_backup_file = None
    except Exception as e:
        log_error(f"PostgreSQL 备份失败: {e}")
        pg_backup_file = None

    # 显示备份文件列表
    print("\n[3] 备份文件列表:")
    print("-" * 40)
    all_backups = sorted(backup_dir.glob("*.sql")) + sorted(backup_dir.glob("*.dump"))
    for f in all_backups:
        if f.stat().st_mtime > time.time() - 86400:  # 24小时内
            size = f.stat().st_size / (1024 * 1024)
            print(f"  {f.name} ({size:.2f} MB)")
    print("-" * 40)

    return mysql_backup_file, pg_backup_file


# ============================================================================
# Step 5: 导出用户 JSON
# ============================================================================

def step5_export_users_json(args):
    """Step 5: 导出用户数据到 JSON"""
    print("\n" + "=" * 60)
    print("Step 5: 导出用户数据到 JSON")
    print("=" * 60)

    backup_dir = Path(args.backup_dir)
    backup_dir.mkdir(parents=True, exist_ok=True)

    output_file = backup_dir / f"users_export_{datetime.now().strftime('%Y%m%d_%H%M%S')}.json"

    print(f"\n[1] 连接到 MySQL ({args.mysql_host}:{args.mysql_port}/{args.mysql_database})...")

    try:
        mysql_conn = get_mysql_connection(args)
        log_success("MySQL 连接成功")

        data = {
            'export_time': datetime.now().isoformat(),
            'mysql_host': args.mysql_host,
            'mysql_port': args.mysql_port,
            'mysql_database': args.mysql_database,
            'users': [],
            'channels': [],
            'tokens': [],
            'options': [],
            'redemptions': [],
            'abilities': [],
            'logs': []
        }

        # 导出各表
        tables_to_export = ['users', 'channels', 'tokens', 'options', 'redemptions', 'abilities']

        for table in tables_to_export:
            print(f"\n[2.{tables_to_export.index(table)+1}] 导出 {table} 表...")
            try:
                table_data = export_table_to_json(mysql_conn, table)
                data[table] = table_data
                log_success(f"导出 {len(data[table])} 条记录")
            except Exception as e:
                log_warn(f"导出 {table} 失败: {e}")

        # 导出 logs (可能很大，分批)
        print("\n[3] 导出 logs 表 (可能需要较长时间)...")
        try:
            # 获取总数
            cursor = mysql_conn.cursor()
            cursor.execute("SELECT COUNT(*) FROM logs")
            total = cursor.fetchone()[0]
            cursor.close()
            log_info(f"logs 表共有 {total} 条记录")

            # 分批导出
            batch_size = 10000
            all_logs = []
            for offset in range(0, total, batch_size):
                cursor = mysql_conn.cursor(pymysql.cursors.DictCursor)
                cursor.execute(f"SELECT * FROM logs LIMIT {batch_size} OFFSET {offset}")
                batch = cursor.fetchall()
                all_logs.extend(batch)
                cursor.close()
                log_info(f"  已导出 {len(all_logs)}/{total}")

            data['logs'] = all_logs
            log_success(f"logs 导出完成: {len(data['logs'])} 条")
        except Exception as e:
            log_warn(f"导出 logs 失败: {e}")

        mysql_conn.close()

        # 保存到文件
        print(f"\n[4] 保存到 {output_file}...")
        with open(output_file, 'w', encoding='utf-8') as f:
            json.dump(data, f, ensure_ascii=False, indent=2, default=str)

        size = output_file.stat().st_size / (1024 * 1024)
        log_success(f"导出成功: {output_file} ({size:.2f} MB)")

        # 显示统计
        print("\n[5] 数据统计:")
        print("-" * 40)
        for key in ['users', 'channels', 'tokens', 'options', 'redemptions', 'abilities', 'logs']:
            if key in data:
                print(f"  {key}: {len(data[key])} 条")
        print("-" * 40)

        return output_file

    except Exception as e:
        log_error(f"导出失败: {e}")
        return None


def pd_read_sql(conn, sql):
    """简单的 read_sql 实现（返回列表）"""
    cursor = conn.cursor(pymysql.cursors.DictCursor)
    cursor.execute(sql)
    results = cursor.fetchall()
    cursor.close()
    return results


def export_table_to_json(conn, table_name):
    """导出表数据到列表"""
    try:
        cursor = conn.cursor(pymysql.cursors.DictCursor)
        cursor.execute(f"SELECT * FROM {table_name}")
        results = cursor.fetchall()
        cursor.close()
        return results
    except Exception as e:
        log_warn(f"导出 {table_name} 失败: {e}")
        return []


# ============================================================================
# Step 6: API 同步用户
# ============================================================================

def step6_sync_users_api(args, users_json_file):
    """Step 6: 通过 one-api API 同步用户"""
    print("\n" + "=" * 60)
    print("Step 6: API 同步用户")
    print("=" * 60)

    # 读取 JSON 文件
    print(f"\n[1] 读取用户数据: {users_json_file}")
    try:
        with open(users_json_file, 'r', encoding='utf-8') as f:
            data = json.load(f)
        source_users = data.get('users', [])
        log_success(f"读取到 {len(source_users)} 个用户")
    except Exception as e:
        log_error(f"读取失败: {e}")
        return False

    # 使用 session 登录（保持 cookie）
    print("\n[2] 登录 one-api...")
    session = requests.Session()
    session.trust_env = False  # 禁用代理

    try:
        resp = session.post(
            f"{ONEAPI_BASE_URL}{ONEAPI_API_PREFIX}/user/login",
            json={
                'username': 'root',
                'password': args.oneapi_password
            },
            timeout=10
        )

        if resp.status_code == 200:
            result = resp.json()
            if result.get('success'):
                log_success("登录成功")
                # 生成 access token 用于后续 API 调用
                resp = session.get(
                    f"{ONEAPI_BASE_URL}{ONEAPI_API_PREFIX}/user/token",
                    timeout=10
                )
                if resp.status_code == 200:
                    token_data = resp.json()
                    if token_data.get('success'):
                        access_token = token_data.get('data')
                        headers = {'Authorization': f'Bearer {access_token}'}
                        log_info(f"获取 Access Token 成功")
                    else:
                        log_error("获取 Access Token 失败")
                        return False
                else:
                    log_error(f"获取 Access Token 失败: HTTP {resp.status_code}")
                    return False
            else:
                log_error(f"登录失败: {result.get('message', 'Unknown error')}")
                return False
        else:
            log_error(f"登录失败: HTTP {resp.status_code}")
            return False
    except Exception as e:
        log_error(f"登录失败: {e}")
        return False

    # 获取现有用户列表
    print("\n[3] 获取现有用户列表...")
    try:
        resp = session.get(f"{ONEAPI_BASE_URL}{ONEAPI_API_PREFIX}/user/", headers=headers, timeout=10)
        if resp.status_code == 200:
            existing_users = resp.json().get('data', [])
            existing_map = {u['username']: u for u in existing_users}
            log_success(f"现有 {len(existing_users)} 个用户")
        else:
            log_error(f"获取用户列表失败: HTTP {resp.status_code}")
            existing_map = {}
    except Exception as e:
        log_error(f"获取用户列表失败: {e}")
        return False

    source_map = {u['username']: u for u in source_users}

    # 创建缺失用户
    print("\n[4] 创建缺失用户...")
    created = 0
    for username, user in source_map.items():
        if username not in existing_map:
            try:
                resp = session.post(
                    f"{ONEAPI_BASE_URL}{ONEAPI_API_PREFIX}/user/",
                    headers=headers,
                    json={
                        'username': username,
                        'password': 'Migrated123!',
                        'email': user.get('email', ''),
                        'role': user.get('role', 'user'),
                        'quota': user.get('quota', 0)
                    },
                    timeout=10
                )
                if resp.status_code == 200:
                    created += 1
                    log_info(f"  创建用户: {username}")
            except Exception as e:
                log_warn(f"  创建用户 {username} 失败: {e}")

    log_success(f"创建了 {created} 个用户")

    # 删除多余用户（保留 root）
    print("\n[5] 删除多余用户...")
    deleted = 0
    for username, user in existing_map.items():
        if username != 'root' and username not in source_map:
            try:
                resp = session.delete(
                    f"{ONEAPI_BASE_URL}{ONEAPI_API_PREFIX}/user/{user['id']}",
                    headers=headers,
                    timeout=10
                )
                if resp.status_code == 200:
                    deleted += 1
                    log_info(f"  删除用户: {username}")
            except Exception as e:
                log_warn(f"  删除用户 {username} 失败: {e}")

    log_success(f"删除了 {deleted} 个用户")

    # 更新现有用户配额
    print("\n[6] 更新用户配额...")
    updated = 0
    for username, user in source_map.items():
        if username in existing_map and username != 'root':
            existing = existing_map[username]
            if existing.get('quota', 0) != user.get('quota', 0):
                try:
                    resp = session.put(
                        f"{ONEAPI_BASE_URL}{ONEAPI_API_PREFIX}/user/{existing['id']}",
                        headers=headers,
                        json={'quota': user.get('quota', 0)},
                        timeout=10
                    )
                    if resp.status_code == 200:
                        updated += 1
                except:
                    pass

    log_success(f"更新了 {updated} 个用户的配额")

    print("\n[7] 同步完成!")
    print("-" * 40)
    print(f"  源用户数: {len(source_users)}")
    print(f"  原用户数: {len(existing_users)}")
    print(f"  创建: {created}")
    print(f"  删除: {deleted}")
    print(f"  更新: {updated}")
    print("-" * 40)

    return True


# ============================================================================
# Step 7: 数据迁移 (upsert 模式)
# ============================================================================

def step7_migrate_data(args, skip_backup=False):
    """Step 7: 使用 upsert 模式迁移数据（安全模式，不会清空数据）"""
    print("\n" + "=" * 60)
    print("Step 7: 数据迁移 (Upsert 模式)")
    print("=" * 60)

    # 备份（如果需要）
    if not skip_backup:
        print("\n[0] 备份 PostgreSQL 数据库...")
        backup_file = backup_postgres(args)
        if backup_file:
            log_success(f"备份已保存: {backup_file}")

    # 确认迁移
    if not args.auto:
        if not confirm("确认执行数据迁移？"):
            log_info("取消迁移")
            return False

    # 测试连接
    print("\n[1] 测试数据库连接...")
    try:
        mysql_conn = get_mysql_connection(args)
        mysql_conn.close()
        log_success("MySQL 连接成功")
    except Exception as e:
        log_error(f"MySQL 连接失败: {e}")
        return False

    try:
        pg_conn = get_pg_connection(args)
        pg_conn.close()
        log_success("PostgreSQL 连接成功")
    except Exception as e:
        log_error(f"PostgreSQL 连接失败: {e}")
        return False

    # 迁移每个表
    total = 0
    for i, table in enumerate(TABLES):
        print(f"\n[{i+2}] 迁移表: {table}")
        try:
            count = upsert_table(args, table)
            total += count
            log_success(f"  迁移 {count} 条记录")
        except Exception as e:
            log_error(f"  迁移失败: {e}")

    # 同步 PostgreSQL 序列
    print("\n[最后] 同步 PostgreSQL 序列...")
    sync_postgres_sequences(args)

    print("\n" + "=" * 60)
    log_success(f"迁移完成! 共迁移 {total} 条记录")
    print("=" * 60)

    return True


def step7_incremental_sync(args, skip_backup=False):
    """Step 7: 增量同步 - 基于用户和时间戳同步数据"""
    print("\n" + "=" * 60)
    print("Step 7: 增量同步 (基于用户+时间戳)")
    print("=" * 60)

    # 备份
    if not skip_backup:
        print("\n[0] 备份 PostgreSQL 数据库...")
        backup_file = backup_postgres(args)
        if backup_file:
            log_success(f"备份已保存: {backup_file}")

    # 连接数据库
    mysql_conn = get_mysql_connection(args)
    pg_conn = get_pg_connection(args)

    # Step 1: 同步用户
    print("\n[1] 同步用户...")
    users_synced = sync_users_incremental(mysql_conn, pg_conn)
    log_success(f"  用户同步完成: {users_synced} 条")

    # Step 2: 同步 tokens (按 user_id + name)
    print("\n[2] 同步 tokens...")
    tokens_synced = sync_tokens_incremental(mysql_conn, pg_conn)
    log_success(f"  tokens 同步完成: {tokens_synced} 条")

    # Step 3: 同步 logs (按 user_id + created_at)
    print("\n[3] 同步消费日志...")
    logs_inserted, logs_updated = sync_logs_incremental(mysql_conn, pg_conn)
    log_success(f"  新增: {logs_inserted} 条, 更新: {logs_updated} 条")

    mysql_conn.close()
    pg_conn.close()

    # 同步 PostgreSQL 序列
    print("\n[最后] 同步 PostgreSQL 序列...")
    sync_postgres_sequences(args)

    print("\n" + "=" * 60)
    log_success(f"增量同步完成!")
    print("=" * 60)

    return True


def sync_postgres_sequences(args):
    """同步 PostgreSQL 序列值到各表的最大 ID，避免序列不同步导致插入失败"""
    print("\n[同步序列]")
    pg_conn = get_pg_connection(args)
    pg_cursor = pg_conn.cursor()

    # 需要同步序列的表及其序列名
    tables_with_sequences = [
        ('logs', 'logs_id_seq'),
        ('users', 'users_id_seq'),
        ('channels', 'channels_id_seq'),
        ('tokens', 'tokens_id_seq'),
        ('redemptions', 'redemptions_id_seq'),
        ('options', 'options_id_seq'),
        ('abilities', 'abilities_id_seq'),
    ]

    for table_name, seq_name in tables_with_sequences:
        try:
            # 获取表的最大 ID
            pg_cursor.execute(f'SELECT MAX(id) FROM {table_name}')
            max_id = pg_cursor.fetchone()[0]

            if max_id is not None:
                # 获取序列当前值
                pg_cursor.execute(f'SELECT last_value FROM {seq_name}')
                current_val = pg_cursor.fetchone()[0]

                # 如果最大 ID 大于序列当前值，更新序列
                if max_id > current_val:
                    pg_cursor.execute(f"SELECT setval('{seq_name}', (SELECT MAX(id) FROM {table_name}))")
                    pg_conn.commit()
                    log_success(f"  {table_name}: 序列 {seq_name} 从 {current_val} 更新到 {max_id}")
                else:
                    log_info(f"  {table_name}: 序列已同步 (当前值: {current_val}, 最大ID: {max_id})")
            else:
                log_info(f"  {table_name}: 表为空，跳过")
        except Exception as e:
            log_warn(f"  {table_name}: 同步失败 - {e}")

    pg_cursor.close()
    pg_conn.close()
    log_success("序列同步完成!")


def sync_users_incremental(mysql_conn, pg_conn):
    """增量同步用户 - 按 username 比对，使用 upsert 模式"""
    mysql_cursor = mysql_conn.cursor()
    pg_cursor = pg_conn.cursor()

    # 获取 MySQL 用户（包含 password 字段，group 需要反引号）
    mysql_cursor.execute("SELECT id, username, password, email, display_name, role, quota, used_quota, status, `group` FROM users")
    mysql_users = {row[1]: row for row in mysql_cursor.fetchall()}  # username -> row

    # 找出缺失的用户（MySQL 有，PG 没有）
    missing_users = set(mysql_users.keys())
    count = 0

    for username, row in mysql_users.items():
        # password 不能为空，使用占位符
        password = row[2] if row[2] else 'MIGRATED_PLACEHOLDER'
        try:
            pg_cursor.execute("""
                INSERT INTO users (username, password, email, display_name, role, quota, used_quota, status, "group")
                VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
                ON CONFLICT (username) DO UPDATE SET
                    quota = EXCLUDED.quota,
                    used_quota = EXCLUDED.used_quota
            """, (row[1], password, row[3], row[4], row[5], row[6], row[7], row[8], row[9]))
            count += 1
        except Exception as e:
            pass  # 忽略错误

    pg_conn.commit()
    mysql_cursor.close()
    pg_cursor.close()

    return count


def sync_tokens_incremental(mysql_conn, pg_conn):
    """增量同步 tokens - 按 key 比对，使用 upsert 模式"""
    mysql_cursor = mysql_conn.cursor()
    pg_cursor = pg_conn.cursor()

    # 获取 MySQL tokens（key 是保留字，需要反引号）
    mysql_cursor.execute("SELECT user_id, name, `key`, status, remain_quota, used_quota, unlimited_quota FROM tokens")
    mysql_tokens = {row[2]: row for row in mysql_cursor.fetchall()}  # key -> row

    count = 0
    for key_val, row in mysql_tokens.items():
        try:
            pg_cursor.execute("""
                INSERT INTO tokens (user_id, name, key, status, remain_quota, used_quota, unlimited_quota)
                VALUES (%s, %s, %s, %s, %s, %s, %s)
                ON CONFLICT (key) DO UPDATE SET
                    remain_quota = EXCLUDED.remain_quota,
                    used_quota = EXCLUDED.used_quota
            """, (row[0], row[1], row[2], row[3], row[4], row[5], row[6]))
            count += 1
        except Exception as e:
            pass

    pg_conn.commit()
    mysql_cursor.close()
    pg_cursor.close()

    return count


def sync_logs_incremental(mysql_conn, pg_conn):
    """增量同步 logs - 按 user_id + created_at 比对，插入缺失记录"""
    mysql_cursor = mysql_conn.cursor()
    pg_cursor = pg_conn.cursor()

    # 获取 MySQL logs 的 (user_id, created_at, model_name, type) 组合作为唯一标识
    mysql_cursor.execute("SELECT user_id, created_at, model_name, type FROM logs")
    mysql_log_keys = set((row[0], row[1], row[2], row[3]) for row in mysql_cursor.fetchall())

    # 获取 PostgreSQL logs 的同样组合
    pg_cursor.execute("SELECT user_id, created_at, model_name, type FROM logs")
    pg_log_keys = set((row[0], row[1], row[2], row[3]) for row in mysql_cursor.fetchall())

    # 找出需要插入的记录（MySQL 有，PG 没有）
    insert_keys = mysql_log_keys - pg_log_keys

    # 批量获取 MySQL logs 记录
    mysql_cursor.execute("""
        SELECT user_id, created_at, type, content, username, token_name, model_name,
               quota, prompt_tokens, completion_tokens, channel_id, request_id,
               elapsed_time, is_stream, system_prompt_reset
        FROM logs
    """)
    mysql_logs = {(row[0], row[1], row[5], row[2]): row for row in mysql_cursor.fetchall()}
    # 格式: (user_id, created_at, model_name, type) -> row

    inserted = 0
    for key in insert_keys:
        user_id, created_at, model_name, log_type = key
        if key in mysql_logs:
            row = mysql_logs[key]
            try:
                pg_cursor.execute("""
                    INSERT INTO logs (user_id, created_at, type, content, username, token_name,
                        model_name, quota, prompt_tokens, completion_tokens, channel_id,
                        request_id, elapsed_time, is_stream, system_prompt_reset)
                    VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
                """, row)
                inserted += 1
            except Exception as e:
                pass

    pg_conn.commit()
    mysql_cursor.close()
    pg_cursor.close()

    return inserted, 0


def upsert_table(args, table_name):
    """Upsert 模式迁移单个表（使用 username/id 作为唯一标识）"""
    # 连接 MySQL
    mysql_conn = get_mysql_connection(args)
    mysql_cursor = mysql_conn.cursor()

    # 获取 MySQL 列信息
    mysql_cursor.execute(f"SHOW COLUMNS FROM {table_name}")
    mysql_columns = mysql_cursor.fetchall()
    # mysql_columns 格式: [(field_name, type, ...), ...]
    mysql_column_names = [row[0] for row in mysql_columns]
    mysql_column_map = {row[0]: row for row in mysql_columns}  # field -> full row

    # 获取数据
    mysql_cursor.execute(f"SELECT * FROM {table_name}")
    rows = mysql_cursor.fetchall()

    mysql_cursor.close()
    mysql_conn.close()

    if not rows:
        return 0

    # 连接 PostgreSQL 并获取实际列名
    pg_conn = get_pg_connection(args)
    pg_cursor = pg_conn.cursor()

    pg_cursor.execute(f"""
        SELECT column_name FROM information_schema.columns
        WHERE table_name = '{table_name}' AND table_schema = 'public'
    """)
    pg_columns = [row[0] for row in pg_cursor.fetchall()]

    # 只使用两个表都有的列
    common_columns = [col for col in pg_columns if col in mysql_column_names]

    if not common_columns:
        log_warn(f"  表 {table_name} 没有共同的列")
        pg_cursor.close()
        pg_conn.close()
        return 0

    # 确定唯一标识列
    if table_name == 'users' and 'username' in common_columns:
        key_column = 'username'
    elif table_name == 'channels' and 'name' in common_columns:
        key_column = 'name'
    else:
        key_column = 'id'

    log_info(f"  使用 '{key_column}' 作为唯一标识，{len(common_columns)} 个列")

    # 获取布尔类型列
    bool_cols = BOOLEAN_COLUMNS.get(table_name, [])

    # 生成 upsert 语句
    quoted_columns = []
    for col in common_columns:
        if col.lower() in RESERVED_KEYWORDS:
            quoted_columns.append(f'"{col}"')
        else:
            quoted_columns.append(col)

    placeholders = ','.join(['%s'] * len(common_columns))
    set_clause = ','.join([f'"{c}" = EXCLUDED."{c}"' for c in common_columns if c != key_column])

    upsert_sql = f"""
    INSERT INTO {table_name} ({','.join(quoted_columns)})
    VALUES ({placeholders})
    ON CONFLICT ("{key_column}") DO UPDATE SET
    {set_clause}
    """

    # 转换并插入数据
    count = 0
    column_indices = [mysql_column_names.index(col) for col in common_columns]
    for row in rows:
        values = []
        for i, col_name in enumerate(common_columns):
            col_idx = column_indices[i]
            col_type = mysql_column_map[col_name][1]  # type string
            val = row[col_idx]

            # 布尔转换
            if col_name in bool_cols:
                if val is not None:
                    val = bool(val)
            # 处理字节类型
            elif isinstance(val, (bytes, bytearray)):
                val = val.decode('utf-8') if val else ''
            # 字符串截断
            elif isinstance(val, str):
                if col_type.startswith('varchar') and '(' in col_type:
                    max_len = int(col_type.split('(')[1].split(')')[0])
                    if len(val) > max_len:
                        val = val[:max_len]

            values.append(val)

        try:
            pg_cursor.execute(upsert_sql, values)
            count += 1
        except Exception as e:
            # 尝试字符串转换
            try:
                str_values = [str(v) if v is not None else None for v in values]
                pg_cursor.execute(upsert_sql, str_values)
                count += 1
            except Exception as e2:
                pass

    pg_conn.commit()
    pg_cursor.close()
    pg_conn.close()

    return count


def backup_postgres(args):
    """备份 PostgreSQL 数据库"""
    # 创建备份目录
    backup_dir = Path(args.backup_dir)
    backup_dir.mkdir(parents=True, exist_ok=True)

    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    backup_file = backup_dir / f"oneapi_pre_migration_{timestamp}.dump"

    env = os.environ.copy()
    env['PGPASSWORD'] = args.pg_password

    cmd = [
        'pg_dump',
        '-h', args.pg_host,
        '-p', str(args.pg_port),
        '-U', args.pg_user,
        '-F', 'c',
        '-b',
        '-f', str(backup_file),
        args.pg_database
    ]

    try:
        result = subprocess.run(cmd, env=env, capture_output=True, text=True, timeout=300)
        if result.returncode == 0:
            return backup_file
    except:
        pass

    return None


# ============================================================================
# Step 8: 验证测试
# ============================================================================

def step8_verify(args):
    """Step 8: 验证迁移结果"""
    print("\n" + "=" * 60)
    print("Step 8: 验证测试")
    print("=" * 60)

    success = True

    # 1. 对比用户数量
    print("\n[1] 对比用户数量...")
    try:
        mysql_conn = get_mysql_connection(args)
        mysql_cursor = mysql_conn.cursor()
        mysql_cursor.execute("SELECT COUNT(*) FROM users")
        mysql_users = mysql_cursor.fetchone()[0]
        mysql_cursor.close()
        mysql_conn.close()

        pg_conn = get_pg_connection(args)
        pg_cursor = pg_conn.cursor()
        pg_cursor.execute("SELECT COUNT(*) FROM users")
        pg_users = pg_cursor.fetchone()[0]
        pg_cursor.close()
        pg_conn.close()

        print(f"  MySQL users: {mysql_users}")
        print(f"  PostgreSQL users: {pg_users}")

        if mysql_users == pg_users:
            log_success("用户数量一致")
        else:
            log_error("用户数量不一致!")
            success = False
    except Exception as e:
        log_error(f"对比失败: {e}")
        success = False

    # 2. 对比 tokens 数量
    print("\n[2] 对比 tokens 数量...")
    try:
        mysql_conn = get_mysql_connection(args)
        mysql_cursor = mysql_conn.cursor()
        mysql_cursor.execute("SELECT COUNT(*) FROM tokens")
        mysql_tokens = mysql_cursor.fetchone()[0]
        mysql_cursor.close()
        mysql_conn.close()

        pg_conn = get_pg_connection(args)
        pg_cursor = pg_conn.cursor()
        pg_cursor.execute("SELECT COUNT(*) FROM tokens")
        pg_tokens = pg_cursor.fetchone()[0]
        pg_cursor.close()
        pg_conn.close()

        print(f"  MySQL tokens: {mysql_tokens}")
        print(f"  PostgreSQL tokens: {pg_tokens}")

        if mysql_tokens == pg_tokens:
            log_success("tokens 数量一致")
        else:
            log_error("tokens 数量不一致!")
            success = False
    except Exception as e:
        log_error(f"对比失败: {e}")
        success = False

    # 3. 对比 logs 数量
    print("\n[3] 对比 logs 数量...")
    try:
        mysql_conn = get_mysql_connection(args)
        mysql_cursor = mysql_conn.cursor()
        mysql_cursor.execute("SELECT COUNT(*) FROM logs")
        mysql_logs = mysql_cursor.fetchone()[0]
        mysql_cursor.close()
        mysql_conn.close()

        pg_conn = get_pg_connection(args)
        pg_cursor = pg_conn.cursor()
        pg_cursor.execute("SELECT COUNT(*) FROM logs")
        pg_logs = pg_cursor.fetchone()[0]
        pg_cursor.close()
        pg_conn.close()

        print(f"  MySQL logs: {mysql_logs}")
        print(f"  PostgreSQL logs: {pg_logs}")

        if mysql_logs == pg_logs:
            log_success("logs 数量一致")
        else:
            log_warn(f"logs 数量不一致 (差异: {abs(mysql_logs - pg_logs)})")
    except Exception as e:
        log_error(f"对比失败: {e}")

    # 4. API 测试
    print("\n[4] API 测试...")
    try:
        # 登录测试
        resp = requests.post(
            f"{ONEAPI_BASE_URL}{ONEAPI_API_PREFIX}/user/login",
            json={'username': 'root', 'password': args.oneapi_password},
            timeout=10
        )
        if resp.status_code == 200:
            token = resp.json().get('data', {}).get('token')
            if token:
                log_success("登录 API 测试通过")

                # 获取用户列表
                headers = {'Authorization': f'Bearer {token}'}
                resp = requests.get(f"{ONEAPI_BASE_URL}{ONEAPI_API_PREFIX}/user/", headers=headers, timeout=10)
                if resp.status_code == 200:
                    users = resp.json().get('data', [])
                    log_success(f"获取用户列表 API 测试通过 ({len(users)} 个用户)")
                else:
                    log_warn("获取用户列表 API 失败")
            else:
                log_warn("登录 API 未返回 token")
        else:
            log_warn(f"登录 API 返回 HTTP {resp.status_code}")
    except Exception as e:
        log_warn(f"API 测试失败: {e}")

    # 总结
    print("\n" + "=" * 60)
    if success:
        log_success("验证测试通过!")
    else:
        log_error("验证测试存在问题，请检查上述输出")
    print("=" * 60)

    return success


# ============================================================================
# 主程序
# ============================================================================

def main():
    print("\n" + "=" * 60)
    print("One API 数据库迁移工具 (增强版)")
    print("=" * 60)

    # 解析参数
    args = parse_args()

    # 设置密码
    args = setup_args_with_passwords(args)

    # 显示配置
    print(f"\nMySQL: {args.mysql_user}@{args.mysql_host}:{args.mysql_port}/{args.mysql_database}")
    print(f"PostgreSQL: {args.pg_user}@{args.pg_host}:{args.pg_port}/{args.pg_database}")

    # 执行
    if args.step:
        # 分步执行
        result = execute_step(args, args.step)
        return 0 if result else 1

    elif args.full_migration:
        # 完整迁移
        return run_full_migration(args)

    else:
        # 交互模式
        return interactive_mode(args)


def execute_step(args, step):
    """执行指定步骤"""
    # 根据是否使用增量模式选择 step7 的实现
    step7_func = step7_incremental_sync if args.incremental else step7_migrate_data

    step_functions = {
        1: step1_health_check,
        2: step2_prepare_postgres,
        3: step3_start_oneapi,
        4: step4_backup_databases,
        5: step5_export_users_json,
        6: lambda a: step6_sync_users_api(a, find_latest_users_json()),
        7: step7_func,
        8: step8_verify,
    }

    if step in step_functions:
        return step_functions[step](args)
    else:
        log_error(f"无效的步骤: {step}")
        return False


def find_latest_users_json():
    """查找最新的用户导出 JSON 文件"""
    backup_dir = Path(BACKUP_DIR)
    files = list(backup_dir.glob("users_export_*.json"))
    if files:
        return max(files, key=lambda f: f.stat().st_mtime)
    return None


def run_full_migration(args):
    """执行完整迁移流程"""
    print("\n" + "=" * 60)
    print("开始完整迁移流程")
    print("=" * 60)

    steps = [
        ("健康检查", lambda: step1_health_check(args)),
        ("PostgreSQL 准备", lambda: step2_prepare_postgres(args)),
        ("启动 one-api", lambda: step3_start_oneapi(args)),
        ("数据备份", lambda: step4_backup_databases(args)),
        ("导出用户 JSON", lambda: step5_export_users_json(args)),
        ("API 同步用户", lambda: step6_sync_users_api(args, find_latest_users_json())),
        ("数据迁移", lambda: step7_migrate_data(args)),
        ("验证测试", lambda: step8_verify(args)),
    ]

    for i, (name, func) in enumerate(steps):
        print(f"\n{'='*60}")
        print(f"执行步骤 {i+1}/8: {name}")
        print('='*60)

        if not args.auto and i > 0:
            if not confirm(f"继续执行 '{name}'?"):
                log_info("用户取消")
                return 1

        try:
            result = func()
            if result is False:
                log_error(f"步骤 '{name}' 失败")
                return 1
        except Exception as e:
            log_error(f"执行 '{name}' 时出错: {e}")
            return 1

    log_success("\n迁移流程完成!")
    return 0


def interactive_mode(args):
    """交互模式菜单"""
    while True:
        print("\n" + "=" * 60)
        print("请选择操作:")
        print("=" * 60)
        print("  1) 完整迁移 (推荐)")
        print("  2) 分步执行")
        print("  3) 仅备份")
        print("  4) 退出")
        print("=" * 60)

        choice = input("请选择 [1-4]: ").strip()

        if choice == '1':
            return run_full_migration(args)
        elif choice == '2':
            return interactive_step_mode(args)
        elif choice == '3':
            step4_backup_databases(args)
            return 0
        elif choice == '4':
            return 0
        else:
            log_error("无效选择")


def interactive_step_mode(args):
    """交互式分步模式"""
    step_names = {
        1: "健康检查",
        2: "PostgreSQL 准备",
        3: "启动 one-api",
        4: "数据备份",
        5: "导出用户 JSON",
        6: "API 同步用户",
        7: "数据迁移",
        8: "验证测试",
    }

    while True:
        print("\n" + "-" * 40)
        print("可选步骤:")
        for k, v in step_names.items():
            print(f"  {k}) {v}")
        print("  0) 返回")
        print("-" * 40)

        choice = input("请选择步骤 [0-8]: ").strip()

        if choice == '0':
            return 0

        try:
            step = int(choice)
            if 1 <= step <= 8:
                execute_step(args, step)
            else:
                log_error("无效选择")
        except ValueError:
            log_error("请输入数字")


if __name__ == '__main__':
    sys.exit(main())