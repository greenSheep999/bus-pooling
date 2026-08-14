import { useState } from "react";
import {
  AlertTriangle, Check, Copy, KeyRound, Loader2, Plus, Ban,
} from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useApiKeys, useCreateApiKey, useRevokeApiKey } from "@/api/hooks";
import { EmptyState } from "@/components/ui/empty-state";
import { SkeletonTable } from "@/components/ui/skeleton";
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
  const { t } = useTranslation("settings");
  const { data: keys, isLoading } = useApiKeys();
  const items = keys ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);

  const active = items.filter((k) => !k.revoked);

  return (
    <div className="space-y-section">
      <SettingsHead
        crumb={t("api-keys.crumb")}
        title={t("api-keys.title")}
        desc={
          <>
            {t("api-keys.desc.prefix")}<Em>{active.length}</Em>{t("api-keys.desc.suffix")}<Link to="/docs" className="font-semibold text-brand-strong hover:underline">{t("api-keys.desc.docs-link")}</Link>
          </>
        }
        right={
          <Button variant="brand" onClick={() => setCreateOpen(true)}>
            <Plus />
            {t("api-keys.create")}
          </Button>
        }
      />

      <Card className="p-7">
        <SectionHead
          title={t("api-keys.list.title")}
          sub={
            items.length === 0
              ? t("api-keys.list.sub-empty")
              : <>{t("api-keys.list.sub.prefix")}<Em>{items.length}</Em>{t("api-keys.list.sub.middle")}<Em>{items.length - active.length}</Em>{t("api-keys.list.sub.suffix")}</>
          }
        />

        {isLoading && !keys ? (
          <SkeletonTable rows={3} cols={["w-1/3", "w-32", "w-20", "w-16"]} />
        ) : items.length === 0 ? (
          <EmptyState
            icon={KeyRound}
            title={t("api-keys.empty.title")}
            desc={t("api-keys.empty.desc")}
            action={
              <Button variant="brand" size="sm" onClick={() => setCreateOpen(true)}>
                <Plus />
                {t("api-keys.create")}
              </Button>
            }
          />
        ) : (
          <div className="mt-4 overflow-x-auto">
            <div className="min-w-[680px]">
              <BareHead>
                <span className="min-w-0 flex-1">{t("api-keys.table.name")}</span>
                <span className="w-[150px] shrink-0">{t("api-keys.table.prefix")}</span>
                <span className="w-[92px] shrink-0">{t("api-keys.table.created")}</span>
                <span className="w-[110px] shrink-0">{t("api-keys.table.last-used")}</span>
                <span className="w-[104px] shrink-0 text-right">{t("api-keys.table.actions")}</span>
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
          {t("api-keys.warn")}
        </Alert>
      </Card>

      <CreateKeyModal open={createOpen} onClose={() => setCreateOpen(false)} />
      <RevokeKeyModal k={revoking} onClose={() => setRevoking(null)} />
    </div>
  );
}

function KeyRow({ k, onRevoke }: { k: ApiKey; onRevoke: () => void }) {
  const { t } = useTranslation("settings");
  const [copied, setCopied] = useState(false);

  return (
    <BareRow className={cn(k.revoked && "opacity-55")}>
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span className={cn("truncate font-semibold", k.revoked && "line-through")}>
          {k.name}
        </span>
        {k.revoked && <Chip tone="neutral">{t("api-keys.row.revoked")}</Chip>}
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
          aria-label={t("api-keys.row.copy-prefix")}
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
            {t("api-keys.row.revoke")}
          </Button>
        )}
      </span>
    </BareRow>
  );
}

/** 新建 · 明文只显示这一次（跟 handoff 同语义） */
function CreateKeyModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation("settings");
  const [name, setName] = useState("");
  const create = useCreateApiKey();
  const [plaintext, setPlaintext] = useState<string | null>(null);

  const close = () => {
    setName("");
    setPlaintext(null);
    onClose();
  };

  const submit = async () => {
    const trimmed = name.trim();
    if (!trimmed || create.isPending) return;
    const r = await create.mutateAsync(trimmed);
    setPlaintext(r.key);
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent className="max-w-[480px]">
        {plaintext ? (
          <>
            <DialogHeader>
              <DialogTitle>{t("api-keys.create-modal.done.title")}</DialogTitle>
              <p className="text-label text-fg-tertiary">{t("api-keys.create-modal.done.desc")}</p>
            </DialogHeader>
            <DialogBody>
              <Alert tone="danger" icon={AlertTriangle} title={t("api-keys.create-modal.done.warn-title")}>
                {t("api-keys.create-modal.done.warn-body")}
              </Alert>
              <div className="mt-3">
                <SecretField plaintext={plaintext} />
              </div>
            </DialogBody>
            <DialogFooter>
              <Button variant="brand" onClick={close}>{t("api-keys.create-modal.done.confirm")}</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>{t("api-keys.create-modal.new.title")}</DialogTitle>
              <p className="text-label text-fg-tertiary">{t("api-keys.create-modal.new.desc")}</p>
            </DialogHeader>
            <DialogBody>
              <Field label={t("api-keys.create-modal.new.label")} hint={t("api-keys.create-modal.new.hint")}>
                <Input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key !== "Enter" || !name.trim()) return;
                    e.preventDefault();
                    void submit();
                  }}
                  placeholder={t("api-keys.create-modal.new.placeholder")}
                  autoFocus
                />
              </Field>
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={close}>{t("common:action.cancel")}</Button>
              <Button
                variant="brand"
                disabled={!name.trim() || create.isPending}
                onClick={() => void submit()}
              >
                {create.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
                {create.isPending ? t("api-keys.create-modal.new.submitting") : t("api-keys.create-modal.new.submit")}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

function RevokeKeyModal({ k, onClose }: { k: ApiKey | null; onClose: () => void }) {
  const { t } = useTranslation("settings");
  const revoke = useRevokeApiKey();

  return (
    <Dialog open={!!k} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[440px]">
        <DialogHeader>
          <DialogTitle>{t("api-keys.revoke-modal.title", { name: k?.name })}</DialogTitle>
          <p className="text-label text-fg-tertiary">{t("api-keys.revoke-modal.desc")}</p>
        </DialogHeader>
        <DialogBody>
          <Alert tone="danger" icon={AlertTriangle} title={t("api-keys.revoke-modal.warn-title")}>
            {t("api-keys.revoke-modal.warn-body")}
          </Alert>
          <p className="mt-3 text-label text-fg-tertiary">
            {t("api-keys.revoke-modal.note")}
          </p>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>{t("common:action.cancel")}</Button>
          <Button
            variant="danger"
            disabled={revoke.isPending}
            onClick={async () => {
              if (!k) return;
              await revoke.mutateAsync(k.id);
              onClose();
            }}
          >
            {revoke.isPending ? t("api-keys.revoke-modal.submitting") : t("api-keys.revoke-modal.submit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
