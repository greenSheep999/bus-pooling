import { Clock3, CloudCheck } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useMe } from "@/api/hooks";
import { Card, Chip, Meter, Stat } from "./ui/primitives";
import {
  QUOTA_COLOR, fmtK, fmtLifespan, quotaLevel, quotaMaxFor,
  toCredits, vendorColor, vendorLabel,
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
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  const alive = cred.status === "alive";
  // 优先用**当前**用量（号池 5min 采样 · 实时）· 死号时后端会把最后一次快照回落到 credits_used
  // 老数据没 usage_current 时也回落到 credits_used
  const usedRaw = cred.usage_current || cred.credits_used;
  const used = toCredits(usedRaw);
  // 用量上限 · 按 subscription 分档 · 后端下发 usage_limit 优先 · 否则按档兜底 ·
  // 都没有走全局 QUOTA_MAX（docs/24 §5 + docs/06-db-schema §12.5a）
  const max = cred.usage_limit
    ? toCredits(cred.usage_limit)
    : quotaMaxFor(cred.subscription);
  const level = quotaLevel(used, max);
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
                  ? "font-semibold"
                  : "font-semibold text-fg-tertiary"
              }
            >
              {vendorLabel(cred.vendor_id, me?.tier)}
            </span>
          </div>

          {!alive ? (
            <Chip tone="danger">{t("credentials.status.dead")}</Chip>
          ) : cred.pushed_at ? (
            <CloudCheck className="size-3.5 text-ok-fg" />
          ) : null}
        </div>

        {/* 额度 · max 走真值(usage_limit) · 老数据兜底 QUOTA_MAX */}
        <div className="space-y-2">
          <Stat
            value={fmtK(used)}
            unit={t("credentials.card.quota-unit", {
              max: max / 1000,
              defaultValue: `k / ${max / 1000}k credits`,
            })}
            size="stat"
            tone={level === "danger" ? color : undefined}
          />
          <Meter value={used} max={max} color={color} />
        </div>

        {/* 寿命 + ID */}
        <div className="flex items-center justify-between">
          <span className="flex items-center gap-1.5 text-label font-medium text-fg-secondary">
            <Clock3 className="size-3 text-fg-tertiary" />
            {fmtLifespan(cred.lifespan_seconds)}
          </span>
          <span className="font-mono text-label text-fg-tertiary">
            …{cred.id.slice(-3)}
          </span>
        </div>
      </div>
    </Card>
  );
}
