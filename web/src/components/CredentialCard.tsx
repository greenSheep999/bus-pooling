import { Clock3, CloudCheck } from "lucide-react";
import { Card, Chip, Meter, Stat } from "./ui/primitives";
import {
  QUOTA_COLOR, QUOTA_MAX, fmtK, fmtLifespan, quotaLevel, toCredits, vendorColor, vendorName,
} from "@/lib/utils";
import type { Credential } from "@/types";

/**
 * 号卡片 · grid 一排 4 个
 * 三层：vendor 身份 → 额度（红绿灯）→ 寿命 + ID
 * 明文 key / 账号 / issuer 不在卡上，点开抽屉看
 */
export function CredentialCard({
  cred,
  onClick,
}: {
  cred: Credential;
  onClick?: () => void;
}) {
  const alive = cred.status === "alive";
  const used = toCredits(cred.credits_used);
  const level = quotaLevel(used);
  const color = QUOTA_COLOR[level];

  return (
    <Card
      hover={!!onClick}
      className={alive ? "p-[18px]" : "bg-bg-elevated p-[18px]"}
    >
      <div onClick={onClick} className="space-y-4">
        {/* vendor + 状态 */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span
              className="size-2 rounded-full"
              style={{ backgroundColor: alive ? vendorColor(cred.vendor_id) : "#A1A1AA" }}
            />
            <span
              className={
                alive
                  ? "text-body font-semibold"
                  : "text-body font-semibold text-fg-tertiary"
              }
            >
              {vendorName(cred.vendor_id)}
            </span>
          </div>

          {!alive ? (
            <Chip tone="danger">已失效</Chip>
          ) : cred.pushed_at ? (
            <CloudCheck className="size-3.5 text-ok-fg" />
          ) : null}
        </div>

        {/* 额度 */}
        <div className="space-y-2">
          <Stat
            value={fmtK(used)}
            unit={`k / ${QUOTA_MAX / 1000}k 积分`}
            size="stat"
            tone={level === "danger" ? color : undefined}
          />
          <Meter value={used} max={QUOTA_MAX} color={color} />
        </div>

        {/* 寿命 + ID */}
        <div className="flex items-center justify-between">
          <span className="flex items-center gap-1.5 text-micro font-medium text-fg-secondary">
            <Clock3 className="size-3 text-fg-tertiary" />
            {fmtLifespan(cred.lifespan_seconds)}
          </span>
          <span className="font-mono text-micro text-fg-tertiary">
            …{cred.id.slice(-3)}
          </span>
        </div>
      </div>
    </Card>
  );
}
