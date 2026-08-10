import { useTranslation } from "react-i18next";

// 独立文件让页面即使暂时没渲染 TrendChart 也能显示图例壳 · 而不必先加载 recharts
export function TrendLegend() {
  const { t } = useTranslation("overview");
  return (
    <div className="flex items-center justify-center gap-5 pt-3 text-label text-fg-tertiary">
      <span className="flex items-center gap-1.5">
        <span className="inline-block h-[2px] w-4 bg-brand" />
        <span>{t("chart.legend.current")}</span>
      </span>
      <span className="flex items-center gap-1.5">
        <span
          className="inline-block h-[2px] w-4"
          style={{
            background:
              "repeating-linear-gradient(to right,#A3A3A3 0,#A3A3A3 3px,transparent 3px,transparent 6px)",
          }}
        />
        <span>{t("chart.legend.average")}</span>
      </span>
      <span className="flex items-center gap-1.5">
        <span className="inline-block size-2 rounded-full bg-brand ring-2 ring-white" />
        <span>{t("chart.legend.peak")}</span>
      </span>
    </div>
  );
}
