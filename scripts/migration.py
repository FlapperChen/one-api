#!/usr/bin/env python3
"""
One API 数据库迁移工具 - 统一脚本
功能: 依赖检查 | 虚拟环境 | 安装依赖 | 执行迁移
支持: SQLite -> PostgreSQL, MySQL -> PostgreSQL
"""

import os
import sys
import subprocess
import shutil
import tempfile
from pathlib import Path

# 颜色定义
class Colors:
    RED = '\033[0;31m'
    GREEN = '\033[0;32m'
    YELLOW = '\033[1;33m'
    BLUE = '\033[0;34m'
    CYAN = '\033[0;36m'
    NC = '\033[0m'

# 配置
SCRIPT_DIR = Path(__file__).parent.resolve()
OFFLINE_DIR = SCRIPT_DIR / "offline_packages"
VENV_NAME = "venv_migration"

# PostgreSQL 配置
POSTGRES_CONFIG = {
    'user': 'postgres',
    'password': 'NCbmc@123',
    'host': 'localhost',
    'port': 5432,
    'database': 'oneapi'
}

#==============================================================================
# 工具函数
#==============================================================================

def log_info(msg):
    print(f"{Colors.BLUE}[INFO]{Colors.NC} {msg}")

def log_success(msg):
    print(f"{Colors.GREEN}[OK]{Colors.NC} {msg}")

def log_warn(msg):
    print(f"{Colors.YELLOW}[WARN]{Colors.NC} {msg}")

def log_error(msg):
    print(f"{Colors.RED}[ERROR]{Colors.NC} {msg}", file=sys.stderr)

def confirm(prompt, default='n'):
    """确认提示"""
    suffix = ' [Y/n]' if default == 'y' else ' [y/N]'
    response = input(f"{Colors.YELLOW}{prompt}{suffix}{Colors.NC} ").strip().lower()
    return response in ('y', 'yes') or (not response and default == 'y')

def run_cmd(cmd, check=True, env=None, shell=False):
    """运行命令"""
    try:
        if shell:
            result = subprocess.run(cmd, shell=True, capture_output=True, text=True, env=env)
        else:
            result = subprocess.run(cmd, capture_output=True, text=True, env=env)
        if check and result.returncode != 0:
            log_error(f"命令失败: {cmd}")
            if result.stderr:
                log_error(result.stderr)
            return False
        return result
    except Exception as e:
        if check:
            log_error(f"执行错误: {e}")
        return None

#==============================================================================
# 功能 1: 依赖检查
#==============================================================================

def check_dependencies():
    """检查依赖是否满足"""
    print(f"\n{Colors.GREEN}=== 依赖检查 ==={Colors.NC}\n")

    missing = []

    # 系统命令
    for cmd in ['bash', 'curl', 'wget', 'python3', 'pip3']:
        result = shutil.which(cmd)
        if result:
            print(f"  ✓ {cmd}")
        else:
            print(f"  ✗ {cmd}")
            missing.append(cmd)

    # PostgreSQL 客户端
    result = shutil.which('psql')
    if result:
        run_cmd(['psql', '--version'], check=False)
    else:
        print(f"  ⚠ psql (可选)")

    # Docker
    result = shutil.which('docker')
    if result:
        print(f"  ✓ docker")
        # 检查 Docker 服务
        run_cmd(['systemctl', 'is-active', 'docker'], check=False)
    else:
        print(f"  ⚠ docker (可选)")

    # Python 模块
    try:
        import pymysql
        print(f"  ✓ pymysql ({pymysql.__version__})")
    except ImportError:
        print(f"  ✗ pymysql")
        missing.append('pymysql')

    try:
        import psycopg2
        print(f"  ✓ psycopg2 ({psycopg2.__version__})")
    except ImportError:
        print(f"  ✗ psycopg2")
        missing.append('psycopg2')

    print()
    if missing:
        log_warn(f"缺少依赖: {', '.join(missing)}")
        log_info("运行: python3 migration.py install")
        return False
    else:
        log_success("所有依赖已满足")
        return True

#==============================================================================
# 功能 2: 安装依赖
#==============================================================================

def install_dependencies():
    """安装 Python 依赖"""
    print(f"\n{Colors.GREEN}=== 安装依赖 ==={Colors.NC}\n")

    # 检查 pip
    if not shutil.which('pip3'):
        log_error("pip3 未安装")
        return False

    # 尝试使用虚拟环境
    venv_python = None
    venv_dir = SCRIPT_DIR / VENV_NAME

    if venv_dir.exists():
        venv_python = venv_dir / "bin" / "python"
        log_info(f"使用虚拟环境: {VENV_NAME}")
        pip = venv_dir / "bin" / "pip"
    else:
        venv_python = sys.executable
        pip = shutil.which('pip3')
        log_info(f"使用系统 Python: {venv_python}")

    # 安装 pymysql
    log_info("安装 pymysql...")
    if venv_python:
        result = run_cmd([str(venv_python), '-m', 'pip', 'install', 'pymysql'], check=False)
    else:
        result = run_cmd(['pip3', 'install', 'pymysql'], check=False)

    if result and result.returncode == 0:
        log_success("pymysql 安装成功")
    else:
        log_warn("pymysql 安装失败，尝试其他方法...")

    # 安装 psycopg2-binary
    log_info("安装 psycopg2-binary...")
    if venv_python:
        result = run_cmd([str(venv_python), '-m', 'pip', 'install', 'psycopg2-binary'], check=False)
    else:
        result = run_cmd(['pip3', 'install', 'psycopg2-binary'], check=False)

    if result and result.returncode == 0:
        log_success("psycopg2-binary 安装成功")
    else:
        log_warn("psycopg2-binary 安装失败，尝试其他方法...")

    # 验证
    print()
    try:
        import pymysql
        log_success(f"pymysql ({pymysql.__version__})")
    except ImportError:
        log_error("pymysql 验证失败")

    try:
        import psycopg2
        log_success(f"psycopg2 ({psycopg2.__version__})")
    except ImportError:
        log_error("psycopg2 验证失败")

    return True

#==============================================================================
# 功能 3: 创建虚拟环境
#==============================================================================

def create_venv():
    """创建虚拟环境"""
    print(f"\n{Colors.GREEN}=== 创建虚拟环境 ==={Colors.NC}\n")

    venv_dir = SCRIPT_DIR / VENV_NAME

    # 检查是否已存在
    if venv_dir.exists():
        if confirm(f"虚拟环境 {VENV_NAME} 已存在，是否删除重建?"):
            log_info("删除旧虚拟环境...")
            shutil.rmtree(venv_dir)
        else:
            log_info("使用现有虚拟环境")
            return True

    # 检查 python3-venv
    log_info("检查 python3-venv...")
    result = run_cmd([sys.executable, '-m', 'venv', '--help'], check=False)
    if result is None or result.returncode != 0:
        log_info("需要安装 python3-venv (需要 sudo)...")
        # 尝试使用系统 Python
        log_info("使用系统 Python 替代虚拟环境")
        return True

    # 创建虚拟环境
    log_info(f"创建虚拟环境: {VENV_NAME}")
    result = run_cmd([sys.executable, '-m', 'venv', str(venv_dir)], check=False)

    if result and result.returncode == 0:
        log_success("虚拟环境创建完成")
        print()
        print(f"激活虚拟环境:")
        print(f"  source {venv_dir}/bin/activate")
        return True
    else:
        log_error("虚拟环境创建失败")
        return False

#==============================================================================
# 功能 4: 执行迁移
#==============================================================================

def run_migration():
    """执行数据库迁移"""
    print(f"\n{Colors.GREEN}=== 执行迁移 ==={Colors.NC}\n")

    # 选择 Python
    venv_dir = SCRIPT_DIR / VENV_NAME
    if venv_dir.exists():
        python = venv_dir / "bin" / "python"
    else:
        python = sys.executable

    log_info(f"使用 Python: {python}")

    # 确认
    log_warn("此操作将清空目标 PostgreSQL 数据库的现有数据!")
    if not confirm("确认继续?"):
        log_info("取消迁移")
        return False

    # 检查 Docker MySQL
    docker = shutil.which('docker')
    if docker:
        result = run_cmd([docker, 'ps', '--format', '{{.Names}}'], check=False)
        if result and 'mysql' in result.stdout:
            log_success("Docker MySQL 容器: 运行中")
        else:
            log_warn("Docker MySQL 容器未运行")

    # 检查 PostgreSQL
    try:
        import psycopg2
        conn = psycopg2.connect(**POSTGRES_CONFIG)
        conn.close()
        log_success("PostgreSQL: 可连接")
    except Exception as e:
        log_error(f"无法连接到 PostgreSQL: {e}")
        return False

    # 询问用户是否执行迁移
    print()
    log_info("环境检查完成，所有依赖已满足")
    print()
    log_info("你可以选择:")
    print("  1) 直接执行迁移 (自动调用 migrate_mysql_pgloader.py)")
    print("  2) 手动执行: python3 migrate_mysql_pgloader.py")
    print("  3) 取消")
    print()

    choice = input(f"{Colors.YELLOW}请选择 [1-3]:{Colors.NC} ").strip()

    if choice == '1':
        # 执行迁移
        print()
        log_info("执行迁移...")
        print()

        migration_script = SCRIPT_DIR / "migrate_mysql_pgloader.py"
        if not migration_script.exists():
            log_error(f"迁移脚本不存在: {migration_script}")
            return False

        result = run_cmd([str(python), str(migration_script)], check=False)

        if result and result.returncode == 0:
            print()
            log_success("=== 迁移完成 ===")
            print()

            # 显示统计
            try:
                import psycopg2
                conn = psycopg2.connect(**POSTGRES_CONFIG)
                cur = conn.cursor()

                tables = ['users', 'channels', 'tokens', 'options', 'redemptions', 'abilities', 'logs']
                print("数据统计:")
                for table in tables:
                    cur.execute(f"SELECT COUNT(*) FROM {table}")
                    count = cur.fetchone()[0]
                    print(f"  {table}: {count}")

                cur.close()
                conn.close()
            except Exception as e:
                log_warn(f"无法显示统计: {e}")

            print()
            print("启动 one-api:")
            print(f"  export SQL_DSN=\"postgres://{POSTGRES_CONFIG['user']}:{POSTGRES_CONFIG['password']}@{POSTGRES_CONFIG['host']}:{POSTGRES_CONFIG['port']}/{POSTGRES_CONFIG['database']}\"")
            print("  cd " + str(SCRIPT_DIR.parent))
            print("  ./one-api --port 3009")

            return True
        else:
            log_error("迁移失败")
            if result and result.stderr:
                print(result.stderr)
            return False

    elif choice == '2':
        print()
        log_info("手动执行迁移:")
        print(f"  cd {SCRIPT_DIR}")
        print(f"  python3 migrate_mysql_pgloader.py")
        return True

    else:
        log_info("取消执行")
        return True

#==============================================================================
# 功能 5: 自动执行 (完整流程)
#==============================================================================

def auto_run():
    """自动执行完整流程"""
    print(f"\n{Colors.CYAN}=== One API 迁移工具 - 自动模式 ==={Colors.NC}\n")

    log_info("将执行以下步骤:")
    print("  1. 依赖检查")
    print("  2. 创建虚拟环境")
    print("  3. 安装依赖")
    print("  4. 执行迁移")
    print()

    if not confirm("确认执行完整流程?"):
        log_info("取消执行")
        return False

    # 步骤1: 依赖检查
    print()
    if not check_dependencies():
        log_warn("继续执行，因为某些依赖可以自动安装...")

    # 步骤2: 虚拟环境
    print()
    create_venv()

    # 步骤3: 安装依赖
    print()
    install_dependencies()

    # 步骤4: 执行迁移 (复用 run_migration 函数)
    print()

    # 确认迁移
    log_warn("此操作将清空目标 PostgreSQL 数据库的现有数据!")
    if not confirm("确认执行迁移?"):
        log_info("取消迁移")
        return False

    # 检查环境
    print()

    # 检查 Docker MySQL
    docker = shutil.which('docker')
    if docker:
        result = run_cmd([docker, 'ps', '--format', '{{.Names}}'], check=False)
        if result and 'mysql' in result.stdout:
            log_success("Docker MySQL 容器: 运行中")
        else:
            log_warn("Docker MySQL 容器未运行")

    # 检查 PostgreSQL
    try:
        import psycopg2
        conn = psycopg2.connect(**POSTGRES_CONFIG)
        conn.close()
        log_success("PostgreSQL: 可连接")
    except Exception as e:
        log_error(f"无法连接到 PostgreSQL: {e}")
        return False

    # 执行迁移
    print()
    migration_script = SCRIPT_DIR / "migrate_mysql_pgloader.py"
    if not migration_script.exists():
        log_error(f"迁移脚本不存在: {migration_script}")
        return False

    result = run_cmd([str(python), str(migration_script)], check=False)

    if result and result.returncode == 0:
        print()
        log_success("=== 迁移完成 ===")
        print()

        # 显示统计
        try:
            import psycopg2
            conn = psycopg2.connect(**POSTGRES_CONFIG)
            cur = conn.cursor()

            tables = ['users', 'channels', 'tokens', 'options', 'redemptions', 'abilities', 'logs']
            print("数据统计:")
            for table in tables:
                cur.execute(f"SELECT COUNT(*) FROM {table}")
                count = cur.fetchone()[0]
                print(f"  {table}: {count}")

            cur.close()
            conn.close()
        except Exception as e:
            log_warn(f"无法显示统计: {e}")

        print()
        print("启动 one-api:")
        print(f"  export SQL_DSN=\"postgres://{POSTGRES_CONFIG['user']}:{POSTGRES_CONFIG['password']}@{POSTGRES_CONFIG['host']}:{POSTGRES_CONFIG['port']}/{POSTGRES_CONFIG['database']}\"")
        print("  cd " + str(SCRIPT_DIR.parent))
        print("  ./one-api --port 3009")

        return True
    else:
        log_error("迁移失败")
        if result and result.stderr:
            print(result.stderr)
        return False

#==============================================================================
# 功能 6: 解包离线包
#==============================================================================

def unpack_packages():
    """解包离线包"""
    print(f"\n{Colors.GREEN}=== 解包离线包 ==={Colors.NC}\n")

    archive = SCRIPT_DIR / "offline_packages.tar.gz"

    if not archive.exists():
        log_error(f"离线包不存在: {archive}")
        log_info("请先运行: python3 migration.py download")
        return False

    log_info("解压离线包...")
    if OFFLINE_DIR.exists():
        shutil.rmtree(OFFLINE_DIR)

    import tarfile
    with tarfile.open(archive, 'r:gz') as tar:
        tar.extractall(SCRIPT_DIR)

    log_success(f"解包完成: {OFFLINE_DIR}/")
    return True

#==============================================================================
# 功能 7: 下载离线包
#==============================================================================

def download_packages():
    """下载离线包"""
    print(f"\n{Colors.GREEN}=== 下载离线包 ==={Colors.NC}\n")

    # 创建目录
    OFFLINE_DIR.mkdir(exist_ok=True)
    (OFFLINE_DIR / "apt").mkdir(exist_ok=True)
    (OFFLINE_DIR / "python").mkdir(exist_ok=True)

    log_info("下载系统包...")
    # 注意: 这里需要实际下载，可能需要 sudo
    log_warn("系统包下载需要系统包管理权限")

    log_info("下载 Python 包...")
    # 下载 Python 包到临时目录
    tmpdir = Path(tempfile.mkdtemp())
    result = run_cmd(['pip3', 'download', '-d', str(tmpdir), 'pymysql', 'psycopg2-binary'], check=False)

    if result and result.returncode == 0:
        # 移动文件
        for f in tmpdir.glob('*.whl'):
            shutil.move(str(f), OFFLINE_DIR / "python" / f.name)

        log_success("Python 包下载完成")
    else:
        log_warn("Python 包下载失败")

    # 清理
    shutil.rmtree(tmpdir)

    # 打包
    log_info("创建离线包...")
    archive = SCRIPT_DIR / "offline_packages.tar.gz"
    if archive.exists():
        archive.unlink()

    with tarfile.open(archive, 'w:gz') as tar:
        tar.add(OFFLINE_DIR, arcname='offline_packages')

    log_success(f"离线包创建完成: {archive}")

    return True

#==============================================================================
# 主程序
#==============================================================================

def main():
    if len(sys.argv) < 2:
        # 默认显示帮助
        print(f"\n{Colors.CYAN}=== One API 数据库迁移工具 ==={Colors.NC}\n")
        print("用法: python3 migration.py [命令]\n")
        print("命令:")
        print("  check      - 检查依赖")
        print("  venv       - 创建虚拟环境")
        print("  install    - 安装依赖")
        print("  migrate    - 执行迁移")
        print("  auto       - 自动执行 (推荐)")
        print("  unpack     - 解包离线包")
        print("  download   - 下载离线包")
        print("  help       - 显示帮助")
        print()
        print("示例:")
        print("  python3 migration.py check")
        print("  python3 migration.py auto")
        return 0

    cmd = sys.argv[1]

    if cmd == 'check':
        check_dependencies()
    elif cmd == 'venv':
        create_venv()
    elif cmd == 'install':
        install_dependencies()
    elif cmd == 'migrate':
        run_migration()
    elif cmd == 'auto':
        auto_run()
    elif cmd == 'unpack':
        unpack_packages()
    elif cmd == 'download':
        download_packages()
    elif cmd in ('help', '--help', '-h'):
        print(__doc__)
    else:
        log_error(f"未知命令: {cmd}")
        print("运行: python3 migration.py help")
        return 1

    return 0

if __name__ == '__main__':
    sys.exit(main())