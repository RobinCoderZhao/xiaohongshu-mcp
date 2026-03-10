#!/bin/bash
# 小红书 MCP Server 启动脚本
# 路径: /Users/robin/export/xiaohongshu-mcp/

cd /Users/robin/export/xiaohongshu-mcp

case "${1:-start}" in
  start)
    echo "📕 Starting xiaohongshu-mcp server..."
    nohup ./xiaohongshu-mcp > mcp.log 2>&1 &
    echo "PID: $!"
    echo "日志: /Users/robin/export/xiaohongshu-mcp/mcp.log"
    echo "端口: http://localhost:18060/mcp"
    ;;
  stop)
    echo "🛑 Stopping xiaohongshu-mcp..."
    pkill -f "xiaohongshu-mcp" 2>/dev/null
    echo "Done"
    ;;
  status)
    if pgrep -f "xiaohongshu-mcp" > /dev/null; then
      echo "✅ xiaohongshu-mcp is running (PID: $(pgrep -f 'xiaohongshu-mcp'))"
    else
      echo "❌ xiaohongshu-mcp is not running"
    fi
    ;;
  log)
    tail -f mcp.log
    ;;
  login)
    echo "🔑 Starting login tool..."
    ./xiaohongshu-login
    ;;
  *)
    echo "Usage: $0 {start|stop|status|log|login}"
    exit 1
    ;;
esac
