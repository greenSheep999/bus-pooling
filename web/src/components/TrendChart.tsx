import {
  Area, AreaChart, CartesianGrid, Label, ReferenceDot, ReferenceLine,
  ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";
import { useTranslation } from "react-i18next";
import type { TrendPoint } from "@/types";

export default function TrendChart({
  data,
  metric,
  height = 260, // 220 + ~28 图例 + ~12 峰值 label 上留白，跟没加图例前的 plot 面积一致
}: {
  data: TrendPoint[];
  metric: string;
  height?: number;
}) {
  const { t } = useTranslation("overview");
  const unitKey = `chart.unit.${metric}`;
  const unit = t(unitKey, { defaultValue: "" });

  /* 平均 + 峰值：直接在数据上算一次，别塞进 Chart 内部 —— 数据变了 tooltip 也用 */
  const values = data.map((d) => d.value);
  const avg = values.length ? values.reduce((a, b) => a + b, 0) / values.length : 0;
  const peakIdx = values.length ? values.indexOf(Math.max(...values)) : -1;
  const peak = peakIdx >= 0 ? data[peakIdx] : null;

  return (
    <ResponsiveContainer width="100%" height={height}>
      {/* top: 28 · 给峰值 label（`峰值 62 · 07/18`）留位，8 不够会被裁 */}
      <AreaChart data={data} margin={{ top: 28, right: 12, bottom: 0, left: -20 }}>
        <defs>
          <linearGradient id="gradTrend" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#9147FF" stopOpacity={0.22} />
            <stop offset="100%" stopColor="#9147FF" stopOpacity={0} />
          </linearGradient>
        </defs>

        {/* 网格实线走 hairline · 深色深灰 · 浅色浅灰 · 不抢曲线视觉 */}
        <CartesianGrid
          strokeDasharray="0"
          vertical={false}
          stroke="hsl(var(--hairline))"
        />

        <XAxis
          dataKey="date"
          tickLine={false}
          axisLine={false}
          interval="preserveStartEnd"
          minTickGap={48}
          tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
          tickFormatter={(d: string) => {
            const [, m, dd] = d.split("-");
            return `${Number(m)}/${dd}`;
          }}
        />

        <YAxis
          tickLine={false}
          axisLine={false}
          width={52}
          tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
        />

        <Tooltip
          cursor={{ stroke: "#9147FF", strokeWidth: 1, strokeDasharray: "4 4" }}
          content={({ active, payload, label }) => {
            if (!active || !payload?.length) return null;
            return (
              <div className="rounded-xl border border-hairline bg-bg px-3 py-2 shadow-pop">
                <div className="text-label text-fg-tertiary">{label}</div>
                <div className="mt-0.5 font-semibold tnum">
                  {payload[0].value} {unit}
                </div>
              </div>
            );
          }}
        />

        {/* 平均虚线 · 灰色 · label 右侧 */}
        {avg > 0 && (
          <ReferenceLine
            y={avg}
            stroke="currentColor"
            className="text-fg-tertiary"
            strokeDasharray="4 4"
            strokeWidth={1}
            ifOverflow="extendDomain"
          >
            <Label
              value={t("chart.avg-label", {
                value: metric === "pulls" ? Math.round(avg) : avg.toFixed(1),
                unit,
              })}
              position="insideTopRight"
              fill="currentColor"
              className="text-fg-tertiary"
              fontSize={11}
              offset={6}
            />
          </ReferenceLine>
        )}

        <Area
          type="monotone"
          dataKey="value"
          stroke="#9147FF"
          strokeWidth={2}
          fill="url(#gradTrend)"
          dot={false}
          activeDot={{ r: 4, fill: "#9147FF", stroke: "hsl(var(--bg))", strokeWidth: 2 }}
        />

        {/* 峰值点 · 品牌色实心 · 描边跟随底色（深色下 = 深底描边 · 保留"点从背景抠出来"的观感） */}
        {peak && (
          <ReferenceDot
            x={peak.date}
            y={peak.value}
            r={4}
            fill="#9147FF"
            stroke="hsl(var(--bg))"
            strokeWidth={2}
            ifOverflow="visible"
          >
            <Label
              value={t("chart.peak-label", {
                value: peak.value,
                unit,
                date: peak.date.slice(5).replace("-", "/"),
              })}
              position="top"
              fill="hsl(var(--brand-strong))"
              fontSize={11}
              fontWeight={600}
              offset={10}
            />
          </ReferenceDot>
        )}
      </AreaChart>
    </ResponsiveContainer>
  );
}

// TrendLegend 已拆到 ./TrendLegend.tsx · 避免使用者仅为图例也拉进 recharts
