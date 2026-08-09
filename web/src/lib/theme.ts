import { useEffect, useState } from "react";

/** 主题模式 · light / dark / system（跟系统）· 存 localStorage
 *  在 <html> 上加 .dark class · tailwind darkMode:['class'] 生效 */
export type ThemeMode = "light" | "dark" | "system";

const KEY = "bp:theme";

/** 系统当前偏好 · matchMedia 判定 */
function systemPrefersDark(): boolean {
  if (typeof window === "undefined") return false;
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
}

/** 应用主题到 <html>（幂等）· system 时按当前系统偏好定 */
function applyTheme(mode: ThemeMode) {
  const isDark = mode === "dark" || (mode === "system" && systemPrefersDark());
  document.documentElement.classList.toggle("dark", isDark);
}

/** 初次渲染前同步读缓存并落 class · 避免刷新时闪一下浅色
 *  在 main.tsx 顶部调用（React mount 前） */
export function initTheme() {
  if (typeof window === "undefined") return;
  const saved = (localStorage.getItem(KEY) as ThemeMode | null) ?? "system";
  applyTheme(saved);
}

export function useTheme(): [ThemeMode, (m: ThemeMode) => void] {
  const [mode, setMode] = useState<ThemeMode>(() => {
    if (typeof window === "undefined") return "system";
    return (localStorage.getItem(KEY) as ThemeMode | null) ?? "system";
  });

  useEffect(() => {
    localStorage.setItem(KEY, mode);
    applyTheme(mode);
  }, [mode]);

  // system 模式下 · 监听系统主题变化实时切
  useEffect(() => {
    if (mode !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const handler = () => applyTheme("system");
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, [mode]);

  return [mode, setMode];
}
