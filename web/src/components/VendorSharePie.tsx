import { Cell, Pie, PieChart, ResponsiveContainer } from "recharts";
import { vendorColor } from "@/lib/utils";

// 独立文件让 router / Overview 用 React.lazy 拆包 · recharts 只在打开概览时才加载
export default function VendorSharePie({
  data,
}: {
  data: Array<{ vendor_id: string; pulls: number }>;
}) {
  const nonZero = data.filter((s) => s.pulls > 0);
  return (
    <ResponsiveContainer width="100%" height="100%">
      <PieChart>
        <Pie
          data={nonZero}
          dataKey="pulls"
          nameKey="vendor_id"
          innerRadius={58}
          outerRadius={84}
          paddingAngle={2}
          strokeWidth={0}
        >
          {nonZero.map((s) => (
            <Cell key={s.vendor_id} fill={vendorColor(s.vendor_id)} />
          ))}
        </Pie>
      </PieChart>
    </ResponsiveContainer>
  );
}
