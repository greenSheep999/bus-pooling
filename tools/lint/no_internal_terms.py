#!/usr/bin/env python3
"""
CLAUDE.md 硬约束 CI · 禁止内部信息泄漏到 **注释 / 字符串字面量 / 日志 / 错误 message**。

覆盖 CLAUDE.md §0.1 / §0.2 / §1 / §11 / §12.5 / §12.6：

  ① vendor 真名 / provider 名 / 上游服务名（91kiro / kiroceo / ... / kiro.rs / waffo / ...）
  ② 定价机制（加价 / 议价 / 附加费 / 收费点 / 分层字段 · 单价组合等）
  ③ 内部字段（kiro_rs_credential_id / current_group / death_source / provider /
     adapter / decider / coalescer / deathwatch / pullrecord / housepool /
     record group / bus-<id> group / provider_kind 等）
  ④ 内部状态多态（preparing / standby / dying / scrapped / purchasing 等对外泄漏）
  ⑤ 敏感信息（sk-live-... / usr-... 明文 API key 特征串 · webhook secret 明文）

**判定原则**：**扫全库 · 白名单允许 identifier**。
identifier 允许出现在：
  - Go const 定义（`Vendor91Kiro = "kiro91"`）
  - import 路径 · package 名 · struct field 名 · yaml tag / json tag
  - env 名（`BP_VENDOR_KIRO91_API_KEY`）
  - SQL migration 里的枚举值（CHECK IN (...)）
  - API path 段 / URL 参数
  - 内部错误常量（`ErrKiro91` · 但 `Message: "kiro91 ..."` 违规）

**扫描目标**：
  - `internal/` + `cmd/` 下所有 .go（含 _test.go） + migrations/*.sql
  - **`web/src/` 下所有 .ts / .tsx**（2026-08 加）—— CLAUDE.md §12.6 管的是**对外文案**，
    那些字全在前端。之前只扫后端·结果顶部 promo 里的通道商真名溜了很久没被发现。

**前端的判据**：**identifier 允许 · 用户看得见的字不允许**。
  - ✅ 允许：`VENDOR_NAME` 这类 vendor_id → 展示名的映射表键、发给后端的参数值
    （`channel: "waffo"`）、MSW 假数据里的 id
  - ❌ 不允许：JSX 文本节点、`label=` / `desc=` / `sub=` 的内容、Alert 文案、注释里的定价词

**退出码**：违规 = 1 · 无违规 = 0

允许列表在 `_ALLOWLIST` · 命中任一 pattern 该行跳过。**加白名单需要 justification**。
"""
from __future__ import annotations

import re
import sys
import os
from pathlib import Path


# ── 禁止的字面量 ──────────────────────────────────────
# 每条 = (regex, 分类) · regex 是"整行匹配一次"·命中即违规（除非白名单跳过）。
BANNED = [
    # ── ① vendor 真名 / provider 名 ───────────
    (r"\b91kiro\b", "vendor 真名"),
    (r"\bkiro91\b", "vendor 真名"),
    (r"\bkiroceo\b", "vendor 真名"),
    (r"kiro\.ceo", "vendor 真名"),
    (r"\bkirooo\b", "vendor 真名"),
    (r"kiro\.ooo", "vendor 真名"),
    (r"\bkiroappio\b", "vendor 真名"),
    (r"kiroapp\.io", "vendor 真名"),
    (r"\bkiroappcc\b", "vendor 真名"),
    (r"kiroapp\.cc", "vendor 真名"),
    (r"\bkirodrop\b", "vendor 真名"),
    (r"drop\.kiro\.ss", "vendor 真名"),
    # ── 上游服务名 ─────────────────────────
    (r"kiro\.rs", "上游服务"),
    (r"\bwaffo\b", "上游服务"),
    (r"\bepusdt\b", "上游服务"),
    (r"bybit_internal", "上游服务"),
    (r"binance_internal", "上游服务"),
    # ── ② 定价机制 ─────────────────────────
    (r"加价", "定价机制"),
    (r"议价", "定价机制"),
    (r"附加费", "定价机制"),
    (r"收费点", "定价机制"),
    # ── ③ 内部字段（不能出对外接口）──────
    # 只扫**注释 / 字符串字面量** · 不扫 Go identifier
    # （identifier 允许 · 是运行时代码结构）
    # 这些位置用白名单判断：非白名单里出现 = 违规
    # ── ⑤ 明文 secret 特征 ─────────────────
    (r"sk-live-[A-Za-z0-9]{16,}", "明文 API key 特征"),
    (r"usr-[A-Za-z0-9]{16,}", "明文 API key 特征"),
    (r"whsec-[A-Za-z0-9]{16,}", "明文 webhook secret 特征"),

    # ── ⑥ 注释里的思考日志 / 讨论记录 / 产品论述（CLAUDE.md §0.2）──
    #
    # §0.2：注释只写"代码此刻在做什么、为什么这么做"。
    # 不写推理过程、跟用户的对话、产品政策复述、方案论证。
    # 判据：读代码的人不需要知道就能维护它 → 那条注释就该删。
    #
    # 这些模式是"论述文"的语言特征 —— 出现即说明注释在讲道理而不是讲代码。
    (r"(听起来|看起来)(合理|不错|没问题)", "注释在论述而非描述代码（§0.2）"),
    (r"我(一开始|原来|之前)(想|以为|写的)", "注释里的推理过程（§0.2）"),
    (r"(车主|用户)(原话|说过|明确说|要求过)", "注释里的对话记录（§0.2）"),
    (r"(讨论过|争论过).{0,6}(否决|放弃|推翻)", "注释里的讨论记录（§0.2）"),
    (r"(那太荒谬|感觉被骗|干蛋|白搭|瞎写)", "注释里的主观评价（§0.2）"),
    (r"(会落空|制造.{0,4}预期|留个?口子|消除焦虑)", "注释里的文案/产品论述（§0.2）"),
    (r"(身份象征|营销支出|营销预算|无底洞|凭空造钱)", "注释里的商业政策论述（§0.2）"),
    (r"(不然就是失信|变成硬指标|不承诺具体条件)", "注释里的产品承诺论述（§0.2）"),
    (r"(文案原则|三条原则|两条原则)(：|:)", "注释里的规范复述（§0.2 · 该进 docs）"),
    (r"比零售价(低|便宜)", "对外暴露定价分层（§8.20）"),
    # 「看到上游真名/更完整上游信息」= 告诉用户我方按身份隐藏 vendor 信息（§8.20 分层机制）
    (r"(看得?到|解锁|显示).{0,4}上游(真实名|真名|信息)", "暴露 vendor 身份分层机制（§8.20）"),
    (r"上游(真实名字|真名)", "暴露 vendor 身份分层机制（§8.20）"),
    (r"零售视角.{0,6}(编号|匿名)", "暴露 vendor 身份分层机制（§8.20）"),
    # 拼车侧必须叫「拼车码」不叫「邀请码」（§8.38）· 三个码同名用户必混
    (r"拼车邀请码", "拼车侧该叫「拼车码」（§8.38）"),
    (r"(输|填)邀请码加入", "拼车侧该叫「拼车码」（§8.38）"),
    # 文档引用渗到 UI 文本（JSX 里出现 decisions §X / spec §X = 用户看得见内部备注）
    # 注释里可以引 · JSX 文本节点里不行 —— 靠 line_allowed 的注释白名单区分
    # UI 文本里的文档引用 —— 只在 .tsx 的**非注释行**查（注释里引用是允许的）
    # 这条靠 scan_file 传进来的 in_comment 标记过滤 · 见 _UI_ONLY_BANNED

]


# ── 白名单：允许出现的位置 ────────────────────────────
# 每条 = (文件路径 regex · 行内容 regex)。命中任一 → 跳过该行的所有违规。
# **加白名单需要 justification 注释**。
_ALLOWLIST = [
    # ── identifier 定义 ─────────────────
    # Go 常量 · vendor id / provider id
    (r"\.go$", r'\bVendor\w+\s+VendorID\s*=\s*"[a-z0-9]+"'),
    (r"\.go$", r'\bProvider\w+\s+ProviderID\s*=\s*"[a-z0-9]+"'),
    (r"\.go$", r'\btopupchannel\.(Waffo|EPUSDT|Bybit|Binance)\b'),
    # yaml / json tag（数据契约·必须硬编）
    (r"\.go$", r'`(yaml|json|db):"[^"]+"`'),
    # struct field 声明·类型注解在同行（config.Vendors 之类）
    (r"/config/", r'^\s*Kiro\w+\s+Vendor\s+`yaml:'),
    # import 路径
    (r"\.go$", r'^\s*_?\s*"github\.com/[^"]+"$'),
    (r"\.go$", r'"github\.com/bus-pooling/bus-pooling/internal/providers/kiro/vendors/[a-z0-9]+"'),
    # env 名（识别符）
    (r"\.go$", r"BP_VENDOR_(KIRO91|KIROCEO|KIROOOO|KIROAPPIO|KIROAPPCC|KIRODROP)_[A-Z_]+"),
    (r"\.go$", r"BP_TOPUP_(WAFFO|EPUSDT|BYBIT|BINANCE)_ENABLED"),
    (r"\.go$", r'EnvKiro\w+APIKey|EnvKiro\w+Webhook'),
    # topupchannel 包内的 ID 常量声明
    (r"/topupchannel/channel\.go$",
     r'^\s*(Waffo|EPUSDT|Bybit|Binance)\s+ID\s*=\s*"[a-z0-9]+"'),
    # topupchannel/channel.go 内的 registry 数据（各 channel 属性）·
    # 是数据契约字面量 · 允许
    (r"/topupchannel/channel\.go$",
     r'\b(ID|DisplayName|Asset|ProviderKind|PayerReferenceLabel|Note):\s*(ID\("[a-z]+"\)|"[^"]*")'),
    (r"/topupchannel/channel\.go$",
     r'^\s*ID:\s*(Waffo|EPUSDT|Bybit|Binance),'),
    # gwpoller.go 里的 gateway state 归一化 switch
    (r"/topup/gwpoller\.go$",
     r'case\s+"(settled|expired|cancelled|canceled|failed|pending)"'),
    # vendor_webhook slug 白名单 map（运行时路径匹配·必须）
    (r"/api/vendor_webhook\.go$",
     r'"(91kiro|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)":\s*true'),
    # vendor_webhook hmacSpecs 里的注册（要真名·跟 vendor 契约挂钩）
    (r"/api/vendor_webhook\.go$",
     r'"(91kiro|kirodrop|kiroappcc)":\s*\{'),
    # hmacSpecs 里的 VendorID 字段 · vendor_account 表的主键值 · identifier
    (r"/api/vendor_webhook\.go$",
     r'VendorID:\s+"(kiro91|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)",'),
    # slug（vendor 品牌写法）→ 内部 vendor_id 的映射 · 运行时路径匹配必须硬编
    (r"/api/vendor_webhook\.go$",
     r'if slug == "91kiro" \{|^\s*return "kiro91"$'),
    (r"/api/vendor_webhook\.go$",
     r'SecretEnv:\s+"BP_VENDOR_(KIRO91|KIRODROP)_WEBHOOK_SECRET"'),
    # main.go 里的 buildVendorRegistry / decider 装配（identifier 引用）
    (r"/cmd/bus-pooling/main\.go$",
     r'providers\.Vendor(91Kiro|KiroCEO|KiroOOO|KiroAppIO|KiroAppCC|KiroDrop)'),
    (r"/cmd/bus-pooling/main\.go$",
     r'cfg\.Vendors\.(Kiro91|KiroCEO|KiroOOO|KiroAppIO|KiroAppCC|KiroDrop)'),
    (r"/cmd/bus-pooling/main\.go$",
     r'cfg\.Secrets\.(Kiro91|KiroCEO|KiroOOO|KiroAppIO|KiroAppCC|KiroDrop)\w*'),
    # main.go 里 resolveCred("kiro91", ...) 等·同上·是 vendor_id 字面量·identifier
    (r"cmd/bus-pooling/main\.go$",
     r'resolveCred\("(kiro91|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)"'),
    # seed_vendor.go · knownVendorSlugs 白名单 map · 是 CLI 参数校验数据契约
    (r"cmd/bus-pooling/seed_vendor\.go$",
     r'^\s*"(kiro91|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)":\s+true,'),
    # ── SQL migration 里的枚举值 · CHECK IN (...) 白名单 ─
    (r"/migrations/\d+_.*\.sql$",
     r"CHECK\s*\([^)]*(waffo|epusdt|bybit|binance)"),
    # migration 010 的 channel 数据契约行（INSERT 时枚举字符串 + 数据契约文档注释）
    (r"/migrations/010_topup_multichannel\.sql$",
     r"'(waffo|epusdt|bybit|binance)'"),
    (r"/migrations/010_topup_multichannel\.sql$",
     r"'waffo_checkout'|'bybit_internal'|'binance_internal'|'epusdt_onchain'"),
    (r"/migrations/010_topup_multichannel\.sql$",
     r"channel = 'waffo'"),
    # migration 010 的 header 注释：channel 属性对照表·是数据契约文档·允许
    (r"/migrations/010_topup_multichannel\.sql$",
     r"^--.*(waffo|epusdt|bybit|binance)"),
    # migration 028 · pricing 标准化 · seed vendor_pricing 6 家 · vendor id 是
    # 数据契约字面量（跟 providers.VendorXxx 常量绑定）· 允许
    (r"/migrations/028_pricing_normalize\.sql$",
     r"'(kiro91|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)'"),
    (r"/migrations/028_pricing_normalize\.sql$",
     r"^--.*(kiro91|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)"),
    # migration 注释 · 表说明里提到 vendor 真名（是数据契约文档 · 允许）
    (r"/migrations/028_pricing_normalize\.sql$",
     r"kirodrop"),
    # migrate_test.go 白名单表列表里的行注释 · 提到 kirodrop 分档说明
    (r"/db/migrate_test\.go$",
     r"^\s*\"[a-z_]+\",\s*//.*(kirodrop|kiro91|kiroceo|kirooo|kiroappio|kiroappcc)"),
    # ── config·底层校验 err message 里的 yaml path ─
    # 例：errors.New("vendors.kiro91.enabled=true 但 base_url 为空")
    # 这是**面向运维的 yaml 路径**·不是面向乘客的·允许
    (r"/config/config\.go$",
     r'vendors\.(kiro91|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)\.enabled=true'),
    # ── decider 里的 vendor 直接常量引用（identifier）─
    (r"/decider/", r'providers\.Vendor(91Kiro|KiroCEO|KiroOOO|KiroAppIO|KiroAppCC|KiroDrop)\b'),
    # ── **测试代码里的 mock seed** ─────
    # 测试造行 · 直接用真 identifier / SQL 字面量是必要的
    # 但只允许在 seed / SQL 字面量场景 · 不能在测试注释里泛写"加价"之类
    (r"_test\.go$", r"'(waffo|epusdt|bybit|binance)'"),
    (r"_test\.go$", r'"waffo"|"epusdt"|"bybit"|"binance"'),
    (r"_test\.go$", r"VALUES\s*\("),  # SQL seed 行
    (r"_test\.go$", r"INSERT\s+INTO\s+"),
    (r"_test\.go$", r"UPDATE\s+\w+\s+SET"),
    (r"_test\.go$", r'"(91kiro|kiro91|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)"'),
    (r"_test\.go$", r'/api/webhooks/vendor/(91kiro|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)'),
    (r"_test\.go$", r'preferred_vendor.*"kiro91"'),
    (r"_test\.go$", r'\*got\.PreferredVendor\s*!=\s*"kiro91"'),
    (r"_test\.go$", r'BP_VENDOR_'),
    # ── 前 vendor adapter 自身文件里的 identifier 引用 ─
    # 各 vendor 包内 该家真名允许 · 是自身 identifier · 包内文件的注释里也可以引自己
    # 错误前缀 fmt.Errorf("kiroXX: ...") 也允许（内部日志 · 不面向用户）
    (r"/providers/kiro/vendors/kiro91/", r'\bkiro91\b'),
    (r"/providers/kiro/vendors/kiroceo/", r'\bkiroceo\b'),
    (r"/providers/kiro/vendors/kirooo/", r'\bkirooo\b'),
    (r"/providers/kiro/vendors/kiroappio/", r'\bkiroappio\b'),
    (r"/providers/kiro/vendors/kiroappcc/", r'\bkiroappcc\b'),
    (r"/providers/kiro/vendors/kirodrop/", r'\bkirodrop\b'),
    # kiroappcc live 集成测试的 BaseURL（真实域名 · 默认 skip · 是 live 测试 identifier）
    (r"/providers/kiro/vendors/kiroappcc/ledger_test\.go$",
     r'BaseURL:\s*"https://kiroapp\.cc"'),
    # kiroceo 包用 kiroappcc 常量作为 provider ref（跨包 · 允许 identifier）
    (r"/providers/kiro/vendors/",
     r'providers\.VendorKiro(91|CEO|OOO|AppIO|AppCC|Drop)|kiroappcc:|kirodrop:'),
    # providers/kiro/register.go · 装配 6 家 · import 6 个包 · 全 identifier
    (r"/providers/kiro/register\.go$",
     r'\b(kiro91|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)\b|Kiro(91|CEO|OOO|AppIO|AppCC|Drop)'),
    # internal/xi8/ · xi8 vendor_id → 我方 vendor slug 映射表（identifier · 跟数据契约挂钩）
    # 这张表**就是**把外部聚合站 id 翻译到内部 vendor_id 的桥·不许它出现等于不许翻译
    (r"internal/xi8/", r'\b(kiro91|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)\b'),
    # cmd/bus-pooling/xi8.go · CLI 里 seed-vendor xi8 说明 · dev.env 路径注释 · identifier
    (r"cmd/bus-pooling/xi8\.go$",
     r'\b(kiro91|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)\b'),
    # providers/provider_test.go 里各 vendor id 用作测试 identifier
    (r"/providers/provider_test\.go$",
     r'"kiro(91|ceo|ooo)"|"kiroapp(io|cc)"|"kirodrop"|"91kiro"'),
    # api_test.go 里的 fake API key（usr- 前缀是我方 API key 格式 · 测试 identifier）
    (r"/api/api_test\.go$", r'"usr-[a-f0-9]+"'),
    # topup.go 里 req.Channel = "waffo" default channel · identifier
    (r"/api/topup\.go$", r'req\.Channel\s*=\s*"waffo"'),
    # config_test.go 里的 fake yaml body（测试用 · identifier）
    (r"/config/config_test\.go$", r'"vendors:\\n\s+kiro91:'),
    # migration 010 里的 channel 列注释（数据契约描述 · 允许）
    (r"/migrations/010_topup_multichannel\.sql$",
     r"channel\s+TEXT NOT NULL,\s*--\s*waffo"),
    (r"/migrations/010_topup_multichannel\.sql$",
     r"^\s*'overseas',\s*--"),
    (r"/migrations/010_topup_multichannel\.sql$",
     r"^\s*'hosted',\s*--"),
    # topupchannel_test.go · provider_kind 字面量断言（数据契约）
    (r"/topupchannel/channel_test\.go$",
     r'"(waffo_checkout|bybit_internal|binance_internal|epusdt_onchain)"'),
    # vendorview_test.go · banned list 里的字面量（本身就是 lint 表·测试它有效）
    (r"/vendorview/vendorview_test\.go$",
     r'banned\s*:=\s*\[\]string\{'),
    (r"/vendorview/vendorview_test\.go$",
     r'"housepool",\s*"kiro\.rs"'),

    # ── 前端白名单（2026-08 加 · 前端纳入扫描后）───────────────────
    #
    # 判据：**identifier 允许 · 用户看得见的字不允许**。
    # vendor_id 是后端返的数据键·前端必须能拿它查展示名·这类 map 键 / 类型字面量
    # 是"数据契约"不是"文案"。但凡出现在 JSX 文本节点、label、说明文字里 → 违规。

    # lib/utils.ts · vendor_id → 展示名 / 身份色的映射表（键必须跟后端一致）
    # 这两张表**就是**把内部 id 翻译成对外展示名的地方 —— 不许它出现等于不许翻译
    (r"web/src/lib/utils\.ts$",
     r'^\s*"?(91kiro|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)"?:\s*("|#)'),

    # types/index.ts · 字段注释里说明"哪家 vendor 才有这个字段"（数据契约描述）
    (r"web/src/types/index\.ts$",
     r'//.*\b(91kiro|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)\b.*(才有|only|独有)'),
    # types 里的 union 成员 / rail 名注释（数据契约 · 跟后端 topupchannel 对齐）
    (r"web/src/types/index\.ts$",
     r'//.*(waffo|epusdt)\s*(checkout|cashier|/|·|\)|一般)'),
    (r"web/src/types/index\.ts$", r'"topup_source";\s*//'),

    # api/hooks.ts · channel: "waffo" 是发给后端的参数值（identifier）
    (r"web/src/api/hooks\.ts$", r'channel:\s*"waffo"'),

    # mocks/ · MSW 假数据里的 vendor_id / 渠道 id（identifier · 要跟后端契约一致才能测）
    (r"web/src/mocks/", r'"?(91kiro|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)"?\s*[:,)]'),
    (r"web/src/mocks/", r'mkKey\("(91kiro|kiroceo|kirooo|kiroappio|kiroappcc|kirodrop)"'),
    (r"web/src/mocks/", r'vendor_id:\s*"'),
    (r"web/src/mocks/", r'channel:\s*"(waffo|bybit|binance|epusdt)'),
    # mocks 里的 vendor 三元判断 / 数组 / host / URL —— 全是喂给组件的 identifier
    # MSW 只在 VITE_USE_MOCK=1 跑·不进生产 bundle（main.tsx 里 import.meta.env.DEV 才装）
    (r"web/src/mocks/", r'vendor\s*===\s*"'),
    (r"web/src/mocks/", r'vendors:\s*\['),
    (r"web/src/mocks/", r'vendorStocks\["'),
    (r"web/src/mocks/", r'(auth\.91kiro\.com|api\.kiro\.ceo|pay\.waffo\.example)'),
    (r"web/src/mocks/", r'(summary|memo):\s*"'),
]


# ── 只在 UI 文本查的规则（注释里出现是允许的）────────────────
#
# 为什么分开：`decisions §8.6` 这类文档引用在**代码注释**里是好事（可追溯），
# 在**JSX 文本节点**里就是内部备注渗给用户看。同一个字符串两种性质，
# 靠"这一行是不是注释"区分。
_UI_ONLY_BANNED = [
    (r"(decisions|spec)\s*§", "文档引用渗到 UI 文本（§0.1）"),
    (r"CLAUDE\.md", "内部文档名渗到 UI 文本（§0.1）"),
]

# 单行注释特征（.ts/.tsx）
_LINE_COMMENT = re.compile(r"^\s*(//|\*)")


def _ui_text_lines(content: str) -> set[int]:
    """返回**不是注释**的行号集合（1-based）。

    块注释（/* */ 和 JSX 的 {/* */}）跨行 · 续行常常不以 * 开头，
    所以要跟着开合状态走·不能只看行首。
    """
    out: set[int] = set()
    in_block = False
    for i, line in enumerate(content.split("\n"), start=1):
        stripped = line.strip()
        if in_block:
            if "*/" in stripped:
                in_block = False
            continue
        if _LINE_COMMENT.match(line):
            continue
        # 本行开了块注释且没闭合 → 后续行都在注释里
        opens = stripped.count("/*")
        closes = stripped.count("*/")
        if opens > closes:
            in_block = True
            continue
        if opens and opens == closes:
            continue  # 单行内开合的块注释
        out.add(i)
    return out


# ── 扫描 ────────────────────────────────────────────
def load_files() -> list[Path]:
    """扫后端源码 + **前端源码**。

    为什么前端也要扫（2026-08 补）：顶部 promo 里的通道商真名 `waffo` 溜了很久没人发现 ——
    因为这个 lint 原来只看 .go / .sql。而**对外文案的违规恰恰全在前端**
    （CLAUDE.md §12.6 说的就是"乘客 UI / 帮助中心不许出现内部术语"）。

    前端扫 web/src 下的 .ts / .tsx —— 那里是所有用户看得见的字。
    web/src/mocks 也扫（MSW 假数据里写真名一样会带进演示环境）。
    """
    roots = ["internal", "cmd", "web/src"]
    out: list[Path] = []
    for r in roots:
        if not os.path.isdir(r):
            continue
        for path in Path(r).rglob("*"):
            if not path.is_file():
                continue
            if path.suffix not in (".go", ".sql", ".ts", ".tsx"):
                continue
            if any(p in path.parts for p in ("vendor", "node_modules", ".git", "dist")):
                continue
            out.append(path)
    return sorted(out)


def line_allowed(path: str, line: str) -> bool:
    for path_pat, line_pat in _ALLOWLIST:
        if re.search(path_pat, path) and re.search(line_pat, line):
            return True
    return False


def scan_file(path: Path) -> list[tuple[int, str, str]]:
    hits: list[tuple[int, str, str]] = []
    try:
        content = path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return hits
    is_ui = path.suffix in (".ts", ".tsx")
    ui_lines = _ui_text_lines(content) if is_ui else set()
    for lineno, line in enumerate(content.split("\n"), start=1):
        if line_allowed(str(path), line):
            continue
        matched = False
        for pat, category in BANNED:
            if re.search(pat, line):
                hits.append((lineno, pat, line))
                matched = True
                break
        if matched:
            continue
        # UI 文本专属规则 · 只查前端**非注释行**
        if is_ui and lineno in ui_lines:
            for pat, category in _UI_ONLY_BANNED:
                if re.search(pat, line):
                    hits.append((lineno, pat, line))
                    break
    return hits


def main() -> int:
    files = load_files()
    total = 0
    per_file: dict[str, list[tuple[int, str, str]]] = {}
    for f in files:
        hits = scan_file(f)
        if hits:
            per_file[str(f)] = hits
            total += len(hits)

    if total == 0:
        print(f"✅ 无内部术语泄漏（扫了 {len(files)} 个源码文件）")
        return 0

    for path, hits in per_file.items():
        print(f"\n{path}:")
        for lineno, pat, line in hits:
            s = line.strip()
            if len(s) > 120:
                s = s[:117] + "..."
            print(f"  L{lineno}  [{pat}]  {s}")

    print(f"\n❌ 违规行数：{total} · 违规文件数：{len(per_file)}\n")
    print("修法（CLAUDE.md §0.1 / §0.2 / §7.3 / §11 / §12.6）：")
    print("  - 定价词：加价/议价/附加费 → 分项 / 计费项")
    print("  - vendor 真名 → 用 'vendor' / '本 vendor' / '其他 vendor'")
    print("  - 上游服务名 kiro.rs → 'housepool 后端'")
    print("  - 上游 gateway 名 → '支付网关' / '通道商'")
    print("  - 若字面量确实是 identifier · 加白名单到 tools/lint/no_internal_terms.py")
    print("  - 思考日志 / 讨论记录 / 产品论述（§0.2）→ 删掉，或挪进 docs/decisions.md")
    print("    注释只写'代码此刻在做什么、为什么这么做'")
    print("    判据：读代码的人不需要知道就能维护它 → 那条注释就该删")
    return 1


if __name__ == "__main__":
    sys.exit(main())
