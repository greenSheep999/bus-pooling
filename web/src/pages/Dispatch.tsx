import { Cloud, FileCheck2, Send, Timer } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Card, Chip } from "@/components/ui/primitives";

/** 3 张能力卡 · 阶段 3b/3c 真做的时候这页直接填内容，结构不推翻（decisions §8.3） */
const FEATURES: { icon: LucideIcon; title: string; desc: string }[] = [
  {
    icon: Cloud,
    title: "号开在你自己的 AWS",
    desc: "账号是你的、额度是你的 · 我方只负责转发开号请求，不碰你的凭证",
  },
  {
    icon: FileCheck2,
    title: "过程可查",
    desc: "每一次开号都有记录：什么时候、开了几个、走的哪家 · 账单对得上",
  },
  {
    icon: Timer,
    title: "寿命最长",
    desc: "自己的号不跟别人共享额度，也不会被别人的用量拖死",
  },
];

export default function Dispatch() {
  return (
    <div className="space-y-section">
      {/* Hero · focal 光晕 · 这页没有数据，唯一的内容就是"将来会有什么" */}
      <Card focal focalTone="brand" className="p-10 text-center sm:p-14">
        <div className="mx-auto max-w-[560px] space-y-5">
          <span className="grid size-14 place-items-center rounded-2xl bg-brand-subtle mx-auto">
            <Send className="size-7 text-brand-strong" />
          </span>

          {/* 标题到描述恒 8px（space-y-2）· 跟其他页一致，见 13-design-principles §5.2b */}
          <div className="space-y-2">
            <Chip tone="brand">阶段 3 开放</Chip>
            <h1 className="text-hero font-semibold">我的发车</h1>
            <p className="text-fg-tertiary">
              把号开在你自己的 AWS 账户上 · 我方帮你转发到上游，号和额度都归你
            </p>
          </div>

          <div className="flex flex-wrap items-center justify-center gap-2 pt-1">
            <Button variant="ghost" asChild>
              <Link to="/buses">先去拼车</Link>
            </Button>
            <Button variant="ghost" asChild>
              <Link to="/docs">看对接文档</Link>
            </Button>
          </div>
        </div>
      </Card>

      {/* 3 张能力卡 */}
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-3">
        {FEATURES.map((f) => (
          <Card key={f.title} className="flex flex-col gap-3 p-6">
            <span className="grid size-9 place-items-center rounded-xl bg-bg-elevated">
              <f.icon className="size-4 text-fg-secondary" />
            </span>
            <div className="space-y-1">
              <div className="font-semibold">{f.title}</div>
              <p className="text-label text-fg-tertiary">{f.desc}</p>
            </div>
          </Card>
        ))}
      </div>

      <p className="text-center text-label text-fg-tertiary">
        现在能用的是<Link to="/buses" className="font-semibold text-brand-strong hover:underline">拼车</Link>
        和<Link to="/extract" className="font-semibold text-brand-strong hover:underline">提取 key</Link>
        {" · "}发车开放时会在站内通知你
      </p>
    </div>
  );
}
