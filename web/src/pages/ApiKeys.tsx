import { useState } from "react";
import {
  AlertTriangle, Check, Copy, KeyRound, Loader2, Plus, Ban,
} from "lucide-react";
import { Link } from "react-router-dom";
import { useApiKeys, useCreateApiKey, useRevokeApiKey } from "@/api/hooks";
import { SettingsHead } from "@/components/SettingsHead";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead,
} from "@/components/ui/primitives";
import { SecretField } from "@/components/ui/secret-field";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { cn, fmtTime } from "@/lib/utils";
import type { ApiKey } from "@/types";

export default function ApiKeys() {
  const { data: keys } = useApiKeys();
  const items = keys ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);

  const active = items.filter((k) => !k.revoked);

  return (
    <div className="space-y-section">
      <SettingsHead
        crumb="API key"
        title="API key"
        desc={
          <>
            拿去调我方 API · 当前 <Em>{active.length}</Em> 个可用 ·
            用法见 <Link to="/docs" className="font-semibold text-brand-strong hover:underline">对接文档</Link>
          </>
        }
        right={
          <Button variant="brand" onClick={() => setCreateOpen(true)}>
            <Plus />
            新建 key
          </Button>
        }
      />

      <Card className="p-7">
        <SectionHead
          title="我的 key"
          sub={
            items.length === 0
              ? "还没有 key"
              : <>共 <Em>{items.length}</Em> 个 · 已吊销 <Em>{items.length - active.length}</Em> 个</>
          }
        />

        {items.length === 0 ? (
          <div className="grid place-items-center gap-3 py-12 text-center">
            <span className="grid size-10 place-items-center rounded-full bg-bg-elevated">
              <KeyRound className="size-4 text-fg-tertiary" />
            </span>
            <p className="text-label text-fg-tertiary">建一个 key 就能开始调 API</p>
            <Button variant="brand" size="sm" onClick={() => setCreateOpen(true)}>
              <Plus />
              新建 key
            </Button>
          </div>
        ) : (
          <div className="mt-4 overflow-x-auto">
            <div className="min-w-[680px]">
              <BareHead>
                <span className="min-w-0 flex-1">备注名</span>
                <span className="w-[150px] shrink-0">前缀</span>
                <span className="w-[92px] shrink-0">创建</span>
                <span className="w-[110px] shrink-0">最近使用</span>
                <span className="w-[104px] shrink-0 text-right">操作</span>
              </BareHead>
              <BareList>
                {items.map((k) => (
                  <KeyRow key={k.id} k={k} onRevoke={() => setRevoking(k)} />
                ))}
              </BareList>
            </div>
          </div>
        )}

        <Alert tone="neutral" icon={KeyRound} className="mt-4">
          key 只在创建那一次显示明文 · 丢了只能吊销重建 · 别写进前端代码或提交到仓库
        </Alert>
      </Card>

      <CreateKeyModal open={createOpen} onClose={() => setCreateOpen(false)} />
      <RevokeKeyModal k={revoking} onClose={() => setRevoking(null)} />
    </div>
  );
}

function KeyRow({ k, onRevoke }: { k: ApiKey; onRevoke: () => void }) {
  const [copied, setCopied] = useState(false);

  return (
    <BareRow className={cn(k.revoked && "opacity-55")}>
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span className={cn("truncate font-semibold", k.revoked && "line-through")}>
          {k.name}
        </span>
        {k.revoked && <Chip tone="neutral">已吊销</Chip>}
      </span>

      <span className="flex w-[150px] shrink-0 items-center gap-1">
        <code className="truncate font-mono text-label text-fg-secondary">{k.prefix}…</code>
        <button
          type="button"
          onClick={() => {
            navigator.clipboard.writeText(k.prefix);
            setCopied(true);
            setTimeout(() => setCopied(false), 1600);
          }}
          aria-label="复制前缀"
          className="grid size-6 shrink-0 place-items-center rounded-md text-fg-tertiary transition-colors hover:bg-bg-elevated hover:text-fg-secondary"
        >
          {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
        </button>
      </span>

      <span className="w-[92px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(k.created_at)}
      </span>

      <span className="w-[110px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {k.last_used_at ? fmtTime(k.last_used_at) : "-"}
      </span>

      <span className="flex w-[104px] shrink-0 items-center justify-end">
        {k.revoked ? (
          <span className="text-label text-fg-tertiary">-</span>
        ) : (
          <Button variant="ghost" size="sm" onClick={onRevoke}>
            <Ban />
            吊销
          </Button>
        )}
      </span>
    </BareRow>
  );
}

/** 新建 · 明文只显示这一次（跟 handoff 同语义） */
function CreateKeyModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [name, setName] = useState("");
  const create = useCreateApiKey();
  const [plaintext, setPlaintext] = useState<string | null>(null);

  const close = () => {
    setName("");
    setPlaintext(null);
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent className="max-w-[480px]">
        {plaintext ? (
          <>
            <DialogHeader>
              <DialogTitle>key 建好了</DialogTitle>
              <p className="text-label text-fg-tertiary">这是唯一一次可见，请立即复制保存</p>
            </DialogHeader>
            <DialogBody>
              <Alert tone="danger" icon={AlertTriangle} title="关掉这个窗口就再也拿不到明文">
                丢了只能吊销重建 —— 我方不留明文
              </Alert>
              <div className="mt-3">
                <SecretField plaintext={plaintext} />
              </div>
            </DialogBody>
            <DialogFooter>
              <Button variant="brand" onClick={close}>我已保存</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>新建 API key</DialogTitle>
              <p className="text-label text-fg-tertiary">给它起个名字，方便以后认出是哪个用途</p>
            </DialogHeader>
            <DialogBody>
              <Field label="备注名" hint="只给你自己看">
                <Input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && name.trim() && create.mutate(name.trim())}
                  placeholder="例：生产 · N8N 机器人"
                  autoFocus
                />
              </Field>
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={close}>取消</Button>
              <Button
                variant="brand"
                disabled={!name.trim() || create.isPending}
                onClick={async () => {
                  const r = await create.mutateAsync(name.trim());
                  setPlaintext(r.plaintext);
                }}
              >
                {create.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
                {create.isPending ? "创建中…" : "创建"}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

function RevokeKeyModal({ k, onClose }: { k: ApiKey | null; onClose: () => void }) {
  const revoke = useRevokeApiKey();

  return (
    <Dialog open={!!k} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[440px]">
        <DialogHeader>
          <DialogTitle>吊销 {k?.name}？</DialogTitle>
          <p className="text-label text-fg-tertiary">用这个 key 的请求会立刻开始返回 401</p>
        </DialogHeader>
        <DialogBody>
          <Alert tone="danger" icon={AlertTriangle} title="不可恢复">
            吊销后不能再启用 · 记录会保留（能看到它建于何时、最近何时用过），但 key 本身作废
          </Alert>
          <p className="mt-3 text-label text-fg-tertiary">
            先确认没有还在跑的脚本用它 —— 尤其是 CI 和机器人
          </p>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>取消</Button>
          <Button
            variant="danger"
            disabled={revoke.isPending}
            onClick={async () => {
              if (!k) return;
              await revoke.mutateAsync(k.id);
              onClose();
            }}
          >
            {revoke.isPending ? "吊销中…" : "确认吊销"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
