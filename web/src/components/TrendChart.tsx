import {
  Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis,
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
  height = 220,
}: {
  data: TrendPoint[];
  metric: string;
  height?: number;
}) {
  const unit = UNIT[metric] ?? "";

  return (
    <ResponsiveContainer width="100%" height={height}>
      <AreaChart data={data} margin={{ top: 8, right: 4, bottom: 0, left: -20 }}>
        <defs>
          <linearGradient id="gradTrend" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#9147FF" stopOpacity={0.22} />
            <stop offset="100%" stopColor="#9147FF" stopOpacity={0} />
          </linearGradient>
        </defs>

        <CartesianGrid
          strokeDasharray="0"
          vertical={false}
          stroke="currentColor"
          className="text-hairline"
        />

        <XAxis
          dataKey="date"
          tickLine={false}
          axisLine={false}
          interval="preserveStartEnd"
          minTickGap={48}
          tick={{ fontSize: 11, fill: "currentColor" }}
          className="text-fg-tertiary"
          tickFormatter={(d: string) => {
            const [, m, dd] = d.split("-");
            return `${Number(m)}/${dd}`;
          }}
        />

        <YAxis
          tickLine={false}
          axisLine={false}
          width={52}
          tick={{ fontSize: 11, fill: "currentColor" }}
          className="text-fg-tertiary"
        />

        <Tooltip
          cursor={{ stroke: "#9147FF", strokeWidth: 1, strokeDasharray: "4 4" }}
          content={({ active, payload, label }) => {
            if (!active || !payload?.length) return null;
            return (
              <div className="rounded-xl border border-hairline bg-bg px-3 py-2 shadow-pop">
                <div className="text-micro text-fg-tertiary">{label}</div>
                <div className="mt-0.5 text-body font-semibold tnum">
                  {payload[0].value} {unit}
                </div>
              </div>
            );
          }}
        />

        <Area
          type="monotone"
          dataKey="value"
          stroke="#9147FF"
          strokeWidth={2}
          fill="url(#gradTrend)"
          dot={false}
          activeDot={{ r: 4, fill: "#9147FF", stroke: "#fff", strokeWidth: 2 }}
        />
      </AreaChart>
    </ResponsiveContainer>
  );
}
