import { Globe, Moon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useTheme, type ThemeMode } from "@/lib/theme";
import { LANGUAGES, resolveLang } from "@/i18n";
import {
  Popover, PopoverContent, PopoverItem, PopoverTrigger,
} from "@/components/ui/popover";

/** Landing / Auth 页共用 · 主题 + 语言切换器
 *  未登录也能用 · 图标按钮 + popover · 不套 AppLayout（那个只给已登录）
 *  样式跟顶栏保持一致 · rounded-full 圆按钮 · hover 淡底 */
export function PublicControls() {
  const { t, i18n } = useTranslation();
  const [theme, setTheme] = useTheme();

  const THEMES: { code: ThemeMode; labelKey: string }[] = [
    { code: "system", labelKey: "avatar.theme_system" },
    { code: "light", labelKey: "avatar.theme_light" },
    { code: "dark", labelKey: "avatar.theme_dark" },
  ];

  return (
    <div className="flex items-center gap-1">
      {/* 语言 */}
      <Popover>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="grid size-9 place-items-center rounded-full text-fg-secondary transition-colors hover:bg-bg-elevated hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30"
            aria-label={t("avatar.language")}
          >
            <Globe className="size-4" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-40 p-1">
          {LANGUAGES.map((l) => (
            <PopoverItem
              key={l.code}
              onSelect={() => { void i18n.changeLanguage(l.code); }}
              className={resolveLang(i18n.language) === l.code ? "font-semibold text-fg" : ""}
            >
              {l.label}
            </PopoverItem>
          ))}
        </PopoverContent>
      </Popover>

      {/* 主题 */}
      <Popover>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="grid size-9 place-items-center rounded-full text-fg-secondary transition-colors hover:bg-bg-elevated hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30"
            aria-label={t("avatar.theme")}
          >
            <Moon className="size-4" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-40 p-1">
          {THEMES.map((th) => (
            <PopoverItem
              key={th.code}
              onSelect={() => setTheme(th.code)}
              className={theme === th.code ? "font-semibold text-fg" : ""}
            >
              {t(th.labelKey)}
            </PopoverItem>
          ))}
        </PopoverContent>
      </Popover>
    </div>
  );
}
