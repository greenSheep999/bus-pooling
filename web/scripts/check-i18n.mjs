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
