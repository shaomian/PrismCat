#!/bin/bash
# macOS 双击入口；构建与进程管理统一在 scripts/build-and-run.mjs。
set -eo pipefail

PROJECT_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$PROJECT_ROOT"

# Finder 启动的终端可能尚未配置 Homebrew、Go 或 nvm 的路径。
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$PATH"
if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
    if [ -s "${NVM_DIR:-$HOME/.nvm}/nvm.sh" ]; then
        . "${NVM_DIR:-$HOME/.nvm}/nvm.sh"
    fi
fi

pause_on_error() {
    local status=$?
    if [ "$status" -ne 0 ] && [ "$status" -ne 130 ] && [ "$status" -ne 143 ] && [ -t 0 ]; then
        printf '\n运行失败，请查看上面的错误信息。\n'
        read -r -p '按回车关闭窗口…' _ || true
    fi
    exit "$status"
}
trap pause_on_error EXIT

if ! command -v node >/dev/null 2>&1; then
    printf '缺少 Node.js，请安装 Node.js（建议 24）后重试。\n' >&2
    exit 1
fi

node "$PROJECT_ROOT/scripts/build-and-run.mjs" "$@"
