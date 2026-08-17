/** 号 / 车的趣味评价档 · **唯一定义处**（车和 key 共用同一套阈值 · 车主 2026-08-17 定）
 *
 *  语感沿用某上游站的原词（`docs/11-fields §16` 有记录:拉完了 / NPC / 人上人 / 夯）·
 *  我方阈值按存活时长分档:
 *
 *    < 30min   拉      · 刚拉出来就没了 · 亏
 *    ≥ 30min   NPC     · 平平无奇 · 活着但没惊喜
 *    ≥ 1h      人上人  · 超过大多数
 *    ≥ 2h      顶级
 *    ≥ 3h      夯      · 硬得离谱
 *
 *  **活号也评** —— 按"当前已存活多久"给档 · 所以同一个号会随时间升级
 *  （拉 → NPC → 人上人 → 顶级 → 夯）· 用户能看到号在长大。
 *
 *  车（母号）多一档 **装甲车**:整车还没死（一辆车只要还有活号就算）· 比 key 的最高档更高。
 *  个人号没有车（没母号）· 所以只有 key 评价 · 不出车评价。
 */

export type RankKey = "la" | "npc" | "renshangren" | "topji" | "hang" | "armored";

/** 阈值（秒）· 改这里就全站一起改 · 别在组件里另写一套 */
const RANK_STEPS: { min: number; rank: RankKey }[] = [
  { min: 3 * 3600, rank: "hang" },        // ≥3h  夯
  { min: 2 * 3600, rank: "topji" },       // ≥2h  顶级
  { min: 1 * 3600, rank: "renshangren" }, // ≥1h  人上人
  { min: 30 * 60, rank: "npc" },          // ≥30m NPC
  { min: 0, rank: "la" },                 // <30m 拉
];

/** key 的评价 · 按存活秒数给档（活号传"当前已存活" · 死号传最终值） */
export function rankOfLifespan(seconds: number): RankKey {
  const s = Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
  for (const step of RANK_STEPS) {
    if (s >= step.min) return step.rank;
  }
  return "la";
}

/** 车的评价 · 车还活着(有活号)就是装甲车 · 否则按整车最长存活给档
 *
 *  aliveCount>0 = 车没死。这是"车级"概念 —— 个人号没车 · 别对个人号调这个。 */
export function rankOfBus(aliveCount: number, maxLifespanSeconds: number): RankKey {
  if (aliveCount > 0) return "armored";
  return rankOfLifespan(maxLifespanSeconds);
}

/** 档 → 语义色（走设计规范的 tone · 不自定义颜色）
 *  拉=danger（亏了）· NPC=neutral · 人上人/顶级=brand · 夯/装甲车=ok（最稳） */
export const RANK_TONE: Record<RankKey, "ok" | "warn" | "danger" | "brand" | "neutral"> = {
  la: "danger",
  npc: "neutral",
  renshangren: "brand",
  topji: "brand",
  hang: "ok",
  armored: "ok",
};
