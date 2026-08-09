#!/usr/bin/env bash
# tools/lint/no-internal-terms.sh
#
# CLAUDE.md §0.1 / §0.2 / §7.3 / §11 / §12.6 硬约束 · CI 强制：
#
#   代码里（**注释 + 字符串字面量 + 日志 message + 错误 message**）绝不能出现
#   下列会泄漏内部服务组合 / 上游依赖 / 定价机制 / 资源命名的字面量：
#
#   1. 定价机制词  加价 / 议价 / 附加费 / 收费点 / 加价栈 / 加价链
#   2. vendor 真名  91kiro / kiro91 / kiroceo / kiro.ceo / kirooo / kiro.ooo /
#                   kiroappio / kiroapp.io / kiroappcc / kiroapp.cc /
#                   kirodrop / drop.kiro.ss
#   3. 上游服务名  kiro.rs / waffo / epusdt / bybit_internal / binance_internal
#
# 允许作为 **identifier**（不算违规）：
#   - import 路径 · package 名
#   - struct field 名 · Go 常量值 · yaml tag / json tag
#   - env 名（BP_VENDOR_KIRO91_*）
#   - SQL identifier / API path 段
#   - migration SQL 里的枚举值（CHECK IN (...)）
#
# 判定原则：**扫全文·允许列表白名单**。
# 扫描目标：`internal/` + `cmd/` 下所有 .go 文件（含 _test.go —— 测试也能泄漏）·
# 也扫 `web/src/` 下 .ts .tsx（前端对外 · 更严）。
#
# 退出码：违规 = 1 · 无违规 = 0

set -eu

cd "$(dirname "$0")/../.."

exec python3 tools/lint/no_internal_terms.py "$@"
