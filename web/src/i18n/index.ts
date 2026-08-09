import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

/** 命名空间划分（按页面 / 模块拆开 · 避免一个大 json）
 *   - common: 顶栏/底栏/按钮/共用词
 *   - landing: 未登录访客首页
 *   - auth: 登录/注册/邀请落地
 *   - overview: 概览
 *   - buses: 拼车列表 / 车详情
 *   - extract: 提取 key
 *   - wallet: 钱包 / 充值
 *   - settings: 设置索引 + 5 张子页
 *   - profile: /me
 *   - community / invite: 社群 / 邀请页
 *   - dispatch / docs / prices: 阶段 3 / 文档 / 价格
 *
 *  未翻译的 key 会 fallback 回默认语言（zh-CN）· 用户看到中文而不是英文 key */
export const NAMESPACES = [
  "common", "landing", "auth", "overview", "buses", "extract",
  "wallet", "settings", "profile", "community", "invite",
  "dispatch", "docs", "prices",
] as const;

export const LANGUAGES = [
  { code: "zh-CN", label: "简体中文" },
  { code: "en", label: "English" },
] as const;

export type LangCode = (typeof LANGUAGES)[number]["code"];

/* Vite import.meta.glob · 编译期把所有 locales/*.json 打进包 · 不用运行时 fetch */
const modules = import.meta.glob("./locales/*/*.json", { eager: true }) as Record<
  string,
  { default: Record<string, unknown> }
>;

// 按语言 · 按命名空间收集资源
const resources: Record<string, Record<string, unknown>> = {};
for (const [path, mod] of Object.entries(modules)) {
  // path 形如 ./locales/zh-CN/common.json
  const m = /\.\/locales\/([^/]+)\/([^/]+)\.json$/.exec(path);
  if (!m) continue;
  const [, lang, ns] = m;
  resources[lang] ??= {};
  resources[lang][ns] = mod.default;
}

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: "zh-CN",
    supportedLngs: LANGUAGES.map((l) => l.code),
    ns: NAMESPACES as unknown as string[],
    defaultNS: "common",
    interpolation: { escapeValue: false }, // React 已经防 XSS
    detection: {
      order: ["localStorage", "navigator"],
      lookupLocalStorage: "bp:lang",
      caches: ["localStorage"],
    },
  });

export default i18n;
