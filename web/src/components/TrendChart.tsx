import {
  Area, AreaChart, CartesianGrid, Label, ReferenceDot, ReferenceLine,
  ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";
import type { TrendPoint } from "@/types";

const UNIT: Record<string, string> = {
  credits: "积分",
  pulls: "次",
  lifespan: "h",
};

export function TrendChart({
  data,
  metric,
  height = 260, // 220 + ~28 图例 + ~12 峰值 label 上留白，跟没加图例前的 plot 面积一致
}: {
  data: TrendPoint[];
  metric: string;
  height?: number;
}) {
  const unit = UNIT[metric] ?? "";

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

        {/* 网格实线要比平均虚线更浅，不然俩灰打架 · currentColor 在 SVG 不稳，硬色值 */}
        <CartesianGrid
          strokeDasharray="0"
          vertical={false}
          stroke="#F2F2F2"
        />

        <XAxis
          dataKey="date"
          tickLine={false}
          axisLine={false}
          interval="preserveStartEnd"
          minTickGap={48}
          tick={{ fontSize: 11, fill: "#A3A3A3" }}
          tickFormatter={(d: string) => {
            const [, m, dd] = d.split("-");
            return `${Number(m)}/${dd}`;
          }}
        />

        <YAxis
          tickLine={false}
          axisLine={false}
          width={52}
          tick={{ fontSize: 11, fill: "#A3A3A3" }}
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
              value={`平均 ${metric === "pulls" ? Math.round(avg) : avg.toFixed(1)} ${unit}`}
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
          activeDot={{ r: 4, fill: "#9147FF", stroke: "#fff", strokeWidth: 2 }}
        />

        {/* 峰值点 · 品牌色实心 · 附日期 label */}
        {peak && (
          <ReferenceDot
            x={peak.date}
            y={peak.value}
            r={4}
            fill="#9147FF"
            stroke="#fff"
            strokeWidth={2}
            ifOverflow="visible"
          >
            <Label
              value={`峰值 ${peak.value} ${unit} · ${peak.date.slice(5).replace("-", "/")}`}
              position="top"
              fill="#6420C7"
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

/** 图例：图表下方居中，解释图上标注的语义 */
export function TrendLegend() {
  return (
    <div className="flex items-center justify-center gap-5 pt-3 text-label text-fg-tertiary">
      <span className="flex items-center gap-1.5">
        <span className="inline-block h-[2px] w-4 bg-brand" />
        <span>当期用量</span>
      </span>
      <span className="flex items-center gap-1.5">
        <span
          className="inline-block h-[2px] w-4"
          style={{
            background:
              "repeating-linear-gradient(to right,#A3A3A3 0,#A3A3A3 3px,transparent 3px,transparent 6px)",
          }}
        />
        <span>期间平均</span>
      </span>
      <span className="flex items-center gap-1.5">
        <span className="inline-block size-2 rounded-full bg-brand ring-2 ring-white" />
        <span>期间峰值</span>
      </span>
    </div>
  );
}
