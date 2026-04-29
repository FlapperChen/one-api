#!/bin/bash
set -e

PROJECT_DIR="/home/bmc/sd1/CODE/one-api"

echo "=== 1. 构建前端 ==="
cd $PROJECT_DIR/web/default
npm run build 2>&1 | tail -3

echo "=== 2. 编译后端 ==="
cd $PROJECT_DIR
go build -ldflags "-s -w" -o one-api && echo "编译成功" || { echo "编译失败"; exit 1; }

echo "=== 3. 停止旧服务 ==="
fuser -k 3009/tcp 2>/dev/null && echo "已停止旧服务" || echo "无旧进程"
sleep 1

echo "=== 4. 启动服务 ==="
chmod u+x one-api
SQL_DSN="postgres://postgres:NCbmc%40123@localhost:5432/oneapi?sslmode=disable" ./one-api --port 3009 --log-dir ./logs