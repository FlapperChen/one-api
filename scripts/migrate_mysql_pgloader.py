#!/usr/bin/env python3
"""
MySQL to PostgreSQL Migration Script using Python
Handles longtext and other problematic MySQL types

Usage:
    python3 migrate_mysql_pgloader.py [options]

Options:
    --mysql-host HOST      MySQL host (default: localhost)
    --mysql-port PORT      MySQL port (default: 3306)
    --mysql-user USER      MySQL user (default: root)
    --mysql-password PASS  MySQL password (required)
    --mysql-db DB          MySQL database (default: oneapi)

    --pg-host HOST         PostgreSQL host (default: localhost)
    --pg-port PORT         PostgreSQL port (default: 5432)
    --pg-user USER         PostgreSQL user (default: postgres)
    --pg-password PASS     PostgreSQL password (required)
    --pg-db DB             PostgreSQL database (default: oneapi)

    -h, --help             Show this help message

Examples:
    # 使用默认配置 (Docker MySQL -> 本地 PostgreSQL)
    python3 migrate_mysql_pgloader.py

    # 自定义 MySQL 配置
    python3 migrate_mysql_pgloader.py --mysql-host 192.168.1.100 --mysql-user root --mysql-password mypass

    # 自义定所有配置
    python3 migrate_mysql_pgloader.py \\
        --mysql-host localhost --mysql-port 3306 --mysql-user root --mysql-password oneapimmysql --mysql-db oneapi \\
        --pg-host localhost --pg-port 5432 --pg-user postgres --pg-password NCbmc@123 --pg-db oneapi

This script migrates data from Docker MySQL to PostgreSQL.
Default: MySQL: root:oneapimmysql@localhost:3306/oneapi
         PostgreSQL: postgres:NCbmc@123@localhost:5432/oneapi
"""

import sys
import os
import argparse

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

# Tables to migrate
TABLES = ['users', 'channels', 'tokens', 'options', 'redemptions', 'abilities', 'logs']

# Type mappings for specific columns that need conversion
# MySQL tinyint(1) -> PostgreSQL boolean
BOOLEAN_COLUMNS = {
    'tokens': ['unlimited_quota'],
    'logs': ['is_stream', 'system_prompt_reset'],
    'channels': [],
    'options': [],
    'users': [],
    'redemptions': [],
    'abilities': ['enabled']
}


def parse_args():
    """解析命令行参数"""
    parser = argparse.ArgumentParser(
        description='MySQL to PostgreSQL Migration Script',
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
    parser.add_argument('--mysql-password', default=DEFAULT_MYSQL['password'],
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
    parser.add_argument('--pg-password', default=DEFAULT_POSTGRES['password'],
                        help=f"PostgreSQL password (default: {DEFAULT_POSTGRES['password']})")
    parser.add_argument('--pg-db', dest='pg_database', default=DEFAULT_POSTGRES['database'],
                        help=f"PostgreSQL database (default: {DEFAULT_POSTGRES['database']})")

    return parser.parse_args()


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


def get_pg_connection(args):
    """获取 PostgreSQL 连接"""
    config = {
        'host': args.pg_host,
        'port': args.pg_port,
        'user': args.pg_user,
        'password': args.pg_password,
        'database': args.pg_database
    }
    return psycopg2.connect(**config)


def migrate_table(args, table_name):
    """迁移单个表"""
    print(f"\n{'='*50}")
    print(f"Migrating table: {table_name}")
    print('='*50)

    # 连接 MySQL
    mysql_conn = get_mysql_connection(args)
    mysql_cursor = mysql_conn.cursor(pymysql.cursors.DictCursor)

    # 获取列信息
    mysql_cursor.execute(f"SHOW COLUMNS FROM {table_name}")
    columns = mysql_cursor.fetchall()
    column_names = [col['Field'] for col in columns]

    print(f"Columns: {column_names}")

    # 获取数据
    mysql_cursor.execute(f"SELECT * FROM {table_name}")
    rows = mysql_cursor.fetchall()

    if not rows:
        print(f"  No data to migrate")
        mysql_cursor.close()
        mysql_conn.close()
        return 0

    print(f"  Found {len(rows)} rows")

    # 连接 PostgreSQL
    pg_conn = get_pg_connection(args)
    pg_cursor = pg_conn.cursor()

    # 清空表
    pg_cursor.execute(f"TRUNCATE TABLE {table_name} CASCADE")

    # 获取布尔类型列
    bool_cols = BOOLEAN_COLUMNS.get(table_name, [])

    # 生成插入语句 - 处理保留关键字
    quoted_columns = []
    for col in column_names:
        # 列出 PostgreSQL 保留关键字
        reserved_keywords = ['group', 'user', 'order', 'table', 'select', 'from', 'where',
                            'and', 'or', 'not', 'null', 'primary', 'key', 'index', 'check',
                            'constraint', 'default', 'foreign', 'references', 'create',
                            'drop', 'alter', 'insert', 'update', 'delete', 'grant',
                            'revoke', 'union', 'limit', 'offset', 'as', 'on', 'join',
                            'left', 'right', 'inner', 'outer', 'full', 'cross']
        if col.lower() in reserved_keywords:
            quoted_columns.append(f'"{col}"')
        else:
            quoted_columns.append(col)

    placeholders = ','.join(['%s'] * len(column_names))
    insert_sql = f"INSERT INTO {table_name} ({','.join(quoted_columns)}) VALUES ({placeholders})"

    # 转换数据
    for row in rows:
        values = []
        for col in columns:
            col_name = col['Field']
            val = row[col_name]

            # 布尔转换
            if col_name in bool_cols:
                if val is not None:
                    val = bool(val)

            # 处理字节类型
            elif isinstance(val, (bytes, bytearray)):
                val = val.decode('utf-8') if val else ''

            # 处理 None
            elif val is None:
                val = None

            # 字符串截断
            elif isinstance(val, str):
                if col['Type'].startswith('varchar') and '(' in col['Type']:
                    max_len = int(col['Type'].split('(')[1].split(')')[0])
                    if len(val) > max_len:
                        val = val[:max_len]

            values.append(val)

        try:
            pg_cursor.execute(insert_sql, values)
        except Exception as e:
            print(f"  Error inserting row: {e}")
            try:
                values = [str(v) if v is not None else None for v in values]
                pg_cursor.execute(insert_sql, values)
            except Exception as e2:
                print(f"  Still failing: {e2}")

    pg_conn.commit()

    # 验证
    pg_cursor.execute(f"SELECT COUNT(*) FROM {table_name}")
    count = pg_cursor.fetchone()[0]
    print(f"  Successfully migrated: {count} rows")

    # 清理
    pg_cursor.close()
    pg_conn.close()
    mysql_cursor.close()
    mysql_conn.close()

    return count


def main():
    print("MySQL to PostgreSQL Migration")
    print("=" * 50)

    # 解析参数
    args = parse_args()

    # 显示配置
    print(f"\nMySQL: {args.mysql_user}@{args.mysql_host}:{args.mysql_port}/{args.mysql_database}")
    print(f"PostgreSQL: {args.pg_user}@{args.pg_host}:{args.pg_port}/{args.pg_database}")

    # 测试连接
    print("\nTesting MySQL connection...")
    try:
        mysql_conn = get_mysql_connection(args)
        mysql_conn.close()
        print("  MySQL: OK")
    except Exception as e:
        print(f"  MySQL: FAILED - {e}")
        return 1

    print("Testing PostgreSQL connection...")
    try:
        pg_conn = get_pg_connection(args)
        pg_conn.close()
        print("  PostgreSQL: OK")
    except Exception as e:
        print(f"  PostgreSQL: FAILED - {e}")
        return 1

    # 迁移每个表
    total = 0
    for table in TABLES:
        try:
            count = migrate_table(args, table)
            total += count
        except Exception as e:
            print(f"  ERROR: {e}")

    print("\n" + "=" * 50)
    print(f"Migration complete! Total rows migrated: {total}")
    print("=" * 50)

    return 0


if __name__ == '__main__':
    sys.exit(main())