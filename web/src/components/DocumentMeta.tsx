import { useEffect } from "react";
import { useTranslation } from "react-i18next";

/** 同步 document.title / meta[description] 到当前 i18n 语言
 *  index.html 首屏用英文兜底，App boot 起来后这个组件按用户语言覆盖
 *  只需要挂一次（Landing / AppLayout 各挂一个 · 切页/切语言都会重跑 effect） */
export function DocumentMeta({
  titleKey = "meta.title",
  descriptionKey = "meta.description",
}: {
  titleKey?: string;
  descriptionKey?: string;
}) {
  const { t } = useTranslation();
  useEffect(() => {
    const title = t(titleKey);
    if (title) document.title = title;
    const desc = t(descriptionKey);
    if (desc) {
      const el = document.querySelector('meta[name="description"]');
      if (el) el.setAttribute("content", desc);
    }
  }, [t, titleKey, descriptionKey]);
  return null;
}
