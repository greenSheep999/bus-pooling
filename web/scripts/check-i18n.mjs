#!/usr/bin/env node
/* check-i18n · 静态查 i18n 漏词条 · 无第三方依赖（只用 node 内置）
 *
 * 起因（2026-08-16）：`t("pull-form.vendor.out-of-stock", { defaultValue: "暂时缺货" })`
 * —— key 两边都不存在 · defaultValue 是中文 · 于是**英文用户看到中文** ·
 * 且 i18next 静默不报。tsc / oxlint 都查不到这类问题。
 *
 * 查三件事：
 *   1. 代码里 t("...") 的字面量 key · 在 en 的任一命名空间里必须存在
 *   2. en / zh-CN 键集必须完全一致（一边加词条另一边忘了 → 回落显示另一语言）
 *   3. defaultValue 里不许出现中日韩字符（那是"看似有兜底、实则漏翻译"的伪装）
 *
 * 限制：只查字面量。模板 key（t(`a.${x}`)）静态查不了 · 会统计数量但不判定 ·
 * 这类要靠人工确认"后端所有可能值都有词条"。
 */
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, basename } from "node:path";

const SRC = "src";
const LOCALES = "src/i18n/locales";
const BASE_LANG = "en";

/** 递归收集文件 */
function walk(dir, exts, out = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, exts, out);
    else if (exts.some((e) => name.endsWith(e))) out.push(p);
  }
  return out;
}

/** 把嵌套 json 拍平成 "a.b.c" 键集 */
function flatten(obj, prefix = "", out = new Set()) {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === "object" && !Array.isArray(v)) flatten(v, key, out);
    else out.add(key);
  }
  return out;
}

function loadLang(lang) {
  const dir = join(LOCALES, lang);
  const byNs = new Map();
  for (const f of readdirSync(dir).filter((f) => f.endsWith(".json"))) {
    byNs.set(basename(f, ".json"), flatten(JSON.parse(readFileSync(join(dir, f), "utf8"))));
  }
  return byNs;
}

const langs = readdirSync(LOCALES).filter((d) => statSync(join(LOCALES, d)).isDirectory());
const loaded = new Map(langs.map((l) => [l, loadLang(l)]));
const base = loaded.get(BASE_LANG);
if (!base) {
  console.error(`✗ 找不到基准语言 ${BASE_LANG}`);
  process.exit(1);
}

const errors = [];

/* ── 1. en / 其他语言 键集一致 ── */
for (const [lang, byNs] of loaded) {
  if (lang === BASE_LANG) continue;
  for (const [ns, keys] of base) {
    const other = byNs.get(ns);
    if (!other) {
      errors.push(`${lang}: 缺整个命名空间 ${ns}.json`);
      continue;
    }
    for (const k of keys) if (!other.has(k)) errors.push(`${lang}/${ns}.json 缺 key: ${k}`);
    for (const k of other) if (!keys.has(k)) errors.push(`${BASE_LANG}/${ns}.json 缺 key: ${k}（${lang} 有）`);
  }
  for (const ns of byNs.keys()) if (!base.has(ns)) errors.push(`${BASE_LANG}: 缺整个命名空间 ${ns}.json`);
}

/* ── 2. 代码里的字面量 key 必须在某个命名空间里存在 ── */
const allBaseKeys = new Set();
for (const keys of base.values()) for (const k of keys) allBaseKeys.add(k);

const files = walk(SRC, [".ts", ".tsx"]).filter((f) => !f.includes("/i18n/locales/"));
let dynamicCount = 0;

for (const f of files) {
  const src = readFileSync(f, "utf8");
  // t("key") / t('key') · 允许后面跟 , 或 )
  for (const m of src.matchAll(/\bt\(\s*["']([^"'`]+)["']/g)) {
    const raw = m[1];
    // 显式命名空间形式 ns:key
    const key = raw.includes(":") ? raw.split(":").slice(1).join(":") : raw;
    if (!allBaseKeys.has(key)) {
      errors.push(`${f}: t("${raw}") —— 所有命名空间里都找不到这个 key`);
    }
  }
  // 模板 key 只统计 · 不判定
  dynamicCount += [...src.matchAll(/\bt\(\s*`/g)].length;
}

/* ── 3. defaultValue 里不许有中日韩字符 ── */
for (const f of files) {
  const src = readFileSync(f, "utf8");
  for (const m of src.matchAll(/defaultValue:\s*["']([^"']*)["']/g)) {
    if (/[㐀-䶿一-鿿぀-ヿ가-힯]/.test(m[1])) {
      errors.push(`${f}: defaultValue "${m[1]}" 含中文 —— 英文用户会看到中文 · 补词条别写兜底`);
    }
  }
}

/* ── 4. 面向用户的字符串里不许硬编码中文 ──
 *
 * 前三条只查"已经走了 t() 的 key" —— 压根没调 t() 的硬编码中文它看不见。
 * 实测漏网（2026-08-17 车主抓出来的）：
 *   - tags.tsx  `function OwnerBadge({ children = "我发起" })` ← 默认值写死中文
 *   - 活动流后端 `Target = "待派"` / `CountUnit = "个号"`   ← 后端塞中文进响应体
 *
 * 只查**会渲染成 UI 的位置**（JSX 文本 / 默认参数值 / 字符串字面量赋给 label 之类）·
 * 注释和 i18n 文件本身不算。误报就往 ALLOW 里加，别把检查关掉。
 */
const CJK = /[㐀-䶿一-鿿぀-ヿ가-힯]/;
const ALLOW_CJK_FILE = [
  /\/i18n\//,          // locales 本身就是中文
  /\/mocks\//,         // mock 数据（假数据里的中文是"数据"不是文案）
  /\/lib\/rank\.ts$/,  // 档名映射表(注释里列了中文档名 · 值走 i18n)
  // Docs 页是**对接文档**：字段说明本身只提供中文（阶段 1 定的 · 不是漏翻译）
  /\/pages\/Docs\.tsx$/,
];

for (const f of files) {
  if (ALLOW_CJK_FILE.some((re) => re.test(f))) continue;
  const src = readFileSync(f, "utf8");
  // 先整文件剥注释（块注释跨行 · 逐行剥会漏掉中间那些以 * 开头的行 —— 实测全是误报）
  const stripped = src
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " ")) // 块注释留换行保行号
    .replace(/^\s*\/\/.*$/gm, "")
    .replace(/([^:])\/\/.*$/gm, "$1");
  stripped.split("\n").forEach((code, i) => {
    if (!CJK.test(code)) return;
    // console.* 是开发日志 · 不是 UI
    if (/console\.(log|info|warn|error|debug)/.test(code)) return;
    // 已经按语言分支了（`zh ? "天" : "d"`）—— 那是本地化过的 · 不算硬编码
    if (/\b(zh|isZh|lang|locale)\b[^?]{0,20}\?/.test(code)) return;
    // 只揪"字符串字面量里的中文"（JSX 文本节点里的中文同样算）
    const inString = /["'`][^"'`]*[㐀-䶿一-鿿぀-ヿ가-힯][^"'`]*["'`]/.test(code);
    const jsxText = />[^<>{]*[㐀-䶿一-鿿぀-ヿ가-힯][^<>{]*</.test(code);
    if (inString || jsxText) {
      errors.push(
        `${f}:${i + 1}: 硬编码中文 —— 面向用户的文案要走 t() · 英文用户会看到中文\n` +
          `      ${code.trim().slice(0, 100)}`,
      );
    }
  });
}

if (errors.length) {
  console.error("✗ i18n 检查失败：\n");
  for (const e of errors) console.error("  " + e);
  console.error(`\n共 ${errors.length} 处`);
  process.exit(1);
}
console.log(
  `✓ i18n OK · ${langs.length} 语言 × ${base.size} 命名空间 · ` +
    `${allBaseKeys.size} 词条 · ${files.length} 文件已扫 · ${dynamicCount} 处模板 key 未静态校验`,
);
