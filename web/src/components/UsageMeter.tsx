import { Meter } from "./ui/primitives";
import { QUOTA_COLOR, quotaLevel, quotaMaxFor, toCredits } from "@/lib/utils";
import type { Money } from "@/types";

/** 号用量进度条 · 待派列表 / 车内号列表 / 派发历史共用 —— 一处定口径不漂
 *
 *  用量真值：活号读实时采样(usage_current) · 死号 credits_used 才是终值
 *  上限：usage_limit 真值优先 · 老数据按 subscription 档兜底(quotaMaxFor)
 *  配色：quotaLevel 阈值(ok/warn/danger) · 不新写一套
 *
 *  入参只要"能算用量的那几个字段" —— Credential 和 AssignedKey 都满足 ·
 *  别为了复用去 cast（那会把类型不匹配藏起来）。 */
export function UsageMeter({
  c,
  className,
}: {
  c: {
    usage_current?: Money;
    usage_limit?: Money;
    credits_used: Money;
    subscription?: "power" | "pro" | "pro_plus" | "pro_max";
  };
  className?: string;
}) {
  const used = toCredits(c.usage_current || c.credits_used);
  const max = c.usage_limit ? toCredits(c.usage_limit) : quotaMaxFor(c.subscription);
  const color = QUOTA_COLOR[quotaLevel(used, max)];
  return <Meter value={used} max={max} color={color} className={className} />;
}
