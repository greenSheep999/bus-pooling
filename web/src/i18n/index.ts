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
  "dispatch", "docs", "prices", "status",
  // vendor · 只 Market 一条(中英对应)· 前 6 家是上游品牌名 / 匿名编号 · 不翻译
  "vendor",
] as const;

export const LANGUAGES = [
  { code: "en", label: "English" },
  { code: "zh-CN", label: "简体中文" },
] as const;

export type LangCode = (typeof LANGUAGES)[number]["code"];

/** 清掉之前跑坏了的 localStorage 值（曾有一版 load:"languageOnly" 把 zh-CN 写成 zh）
 *  只有精确等于 LANGUAGES.code 的值才留下，其他一律清 —— 让 detector 重新走 navigator */
if (typeof window !== "undefined") {
  const stored = window.localStorage.getItem("bp:lang");
  const codes = LANGUAGES.map((l) => l.code) as string[];
  if (stored && !codes.includes(stored)) {
    window.localStorage.removeItem("bp:lang");
  }
}

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
    // Vite 的 import.meta.glob 返 Record<unknown>，跟 i18next 的 Resource 类型对不上但结构一致 · 断言
    resources: resources as never,
    // 默认英语 · 浏览器识别不到 / 识别到不支持的语言就是英文（车主要求）
    // ⚠️ 别用 { default: [...] } 的 object 形式 —— i18next 24 里那种语法会把
    //    supportedLngs 里的 zh-CN 也判定为"不支持"，resolvedLanguage 变 en。
    //    简单的字符串 fallback + supportedLngs 白名单最稳。
    fallbackLng: "en",
    supportedLngs: LANGUAGES.map((l) => l.code),
    ns: NAMESPACES as unknown as string[],
    defaultNS: "common",
    interpolation: { escapeValue: false }, // React 已经防 XSS
    detection: {
      // 顺序：先看用户手动切过没（localStorage）· 没有就用浏览器语言
      order: ["localStorage", "navigator"],
      lookupLocalStorage: "bp:lang",
      caches: ["localStorage"],
      // 把 detector 拿到的 en-US 之类先规整成 en / zh-CN 再存，避免 supportedLngs 判"不支持"
      convertDetectedLanguage: (lng: string) => {
        if (lng.startsWith("zh")) return "zh-CN";
        return "en";
      },
    },
  });

/** 把 i18n.language（可能是 en-US / zh 之类）规整到我们真正支持的 LANGUAGES 里
 *  用于语言切换器 UI 高亮当前项 · 不影响翻译 lookup */
export function resolveLang(current: string): LangCode {
  // 精确命中（zh-CN / en）直接返回
  const codes = LANGUAGES.map((l) => l.code) as string[];
  if (codes.includes(current)) return current as LangCode;
  // 中文各分区都算 zh-CN
  if (current.startsWith("zh")) return "zh-CN";
  // 其他一律 en
  return "en";
}

/** 同步 <html lang="..."> · 无障碍 + CJK 字体切换靠这个 · onChanged 一次性绑 */
if (typeof document !== "undefined") {
  const applyLang = (l: string) => {
    document.documentElement.lang = resolveLang(l);
  };
  applyLang(i18n.language);
  i18n.on("languageChanged", applyLang);
}

export default i18n;
