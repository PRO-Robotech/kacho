// CidrSection — управление CIDR-блоками подсети (одно семейство v4/v6) в блоке
// «Обзор». Add и remove — РАЗНЫЕ методы (:add-cidr-blocks / :remove-cidr-blocks),
// поэтому БЕЗ read/edit-режима с batch-save: каждое действие применяется сразу
// своим RPC (immediate). Без кнопки «Редактировать».
//
// Вид — общая табличная стилистика (как «Статические маршруты»): шапка с бейджем
// IPv4/IPv6 + «CIDR (N)», таблица [CIDR-блок | ⌫], строки mono, удаление сразу
// (со spinner на удаляемой строке), снизу строка ввода + «Добавить».
//
// Backend (kacho-vpc/internal/service/subnet.go) запрещает менять CIDR через
// PATCH (immutable after Subnet.Create) — только эти verb'ы; возвращают Operation.
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button, Input, Space, Spin, Typography } from "antd";
import { DeleteOutlined, LoadingOutlined, PlusOutlined } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { OperationToastWatcher } from "@shared/components/molecules/OperationToastWatcher";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { SectionHeader } from "@shared/components/molecules/SectionHeader";
import { toast } from "@shared/lib/toast";

type CidrKind = "v4" | "v6";

const FIELD_BY_KIND: Record<CidrKind, "v4_cidr_blocks" | "v6_cidr_blocks"> = {
  v4: "v4_cidr_blocks",
  v6: "v6_cidr_blocks",
};

const SUBNETS_API = "/vpc/v1/subnets";
const MONO_FONT = "ui-monospace, monospace";
const ROW_H = 41;

function validateCidr(kind: CidrKind, cidr: string): string | null {
  if (!cidr) return "Введите CIDR.";
  if (!cidr.includes("/")) return "CIDR должен содержать префикс (например /24).";
  if (kind === "v6" && !cidr.includes(":")) return "Похоже не на IPv6-адрес.";
  return null;
}

// Бейдж семейства в плитке шапки — «IPv4» / «IPv6» (mono, мелко чтобы влезло).
const familyTile = (text: string) => (
  <span style={{ fontSize: 10.5, fontWeight: 700, fontFamily: MONO_FONT, letterSpacing: "-0.04em" }}>{text}</span>
);

interface SectionProps {
  subnetId: string;
  kind: CidrKind;
  blocks: string[];
  /** Не используется (operation invalidate по query-key), оставлен для совместимости. */
  projectId?: string | null;
}

export function CidrSection({ subnetId, kind, blocks }: SectionProps) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState("");
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("");
  const [pendingCidr, setPendingCidr] = useState<string | null>(null);

  const family = kind === "v4" ? "IPv4" : "IPv6";
  const placeholder = kind === "v4" ? "10.0.1.0/24" : "fd00:1234::/64";
  const field = FIELD_BY_KIND[kind];

  const mutate = useMutation({
    mutationFn: async (params: { verb: "add" | "remove"; cidr: string }) => {
      return api.action(`${SUBNETS_API}/${subnetId}:${params.verb}-cidr-blocks`, {
        [field]: [params.cidr],
      });
    },
    onSuccess: (resp, vars) => {
      const id = extractOperationId(resp);
      if (id) {
        setOpTitle(`${vars.verb === "add" ? "Добавление" : "Удаление"} ${family} CIDR ${vars.cidr}`);
        setOpId(id);
        setPendingCidr(vars.cidr);
      } else {
        // Широкий prefix-инвалидейт: ["subnets"] матчит и detail-страницу
        // (["subnets","shell-detail",uid]), и list — иначе detail показывает
        // старый CIDR (узкие ключи не совпадали с ключом ResourceShell).
        qc.invalidateQueries({ queryKey: ["subnets"] });
        setPendingCidr(null);
      }
    },
    onError: (err, vars) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`${family} CIDR ${vars.verb === "add" ? "добавление" : "удаление"}: ${m}`);
      setPendingCidr(null);
    },
  });

  const inputDisabled = mutate.isPending || opId !== null;

  const onAdd = () => {
    const cidr = draft.trim();
    const verr = validateCidr(kind, cidr);
    if (verr) {
      toast.error(verr);
      return;
    }
    if (blocks.includes(cidr)) {
      toast.error("Этот CIDR уже добавлен.");
      return;
    }
    setPendingCidr(cidr);
    mutate.mutate({ verb: "add", cidr });
    setDraft("");
  };

  const onRemove = (cidr: string) => {
    setPendingCidr(cidr);
    mutate.mutate({ verb: "remove", cidr });
  };

  return (
    <div style={{ marginTop: 24, maxWidth: 760 }}>
      <SectionHeader
        icon={familyTile(family)}
        eyebrow="Список"
        title={
          <span>
            CIDR <Typography.Text type="secondary">({blocks.length})</Typography.Text>
          </span>
        }
      />

      <div
        style={{
          border: "1px solid var(--kc-border)",
          borderRadius: 8,
          overflow: "hidden",
          background: "var(--kc-page)",
        }}
      >
        <table className="w-full text-sm kc-grid-table" style={{ tableLayout: "fixed" }}>
          <colgroup>
            <col style={{ width: "calc(100% - 48px)" }} />
            <col style={{ width: 48 }} />
          </colgroup>
          <thead>
            <tr style={{ background: "var(--kc-container)" }}>
              <th
                className="text-left"
                style={{
                  padding: "7px 12px",
                  fontSize: 11,
                  fontWeight: 600,
                  letterSpacing: "0.02em",
                  color: "var(--kc-text-tertiary)",
                }}
              >
                CIDR-блок
              </th>
              <th style={{ padding: "7px 4px" }} />
            </tr>
          </thead>
          <tbody>
            {blocks.length === 0 && (
              <tr style={{ height: ROW_H, borderTop: "1px solid var(--kc-border-secondary)" }}>
                <td
                  colSpan={2}
                  style={{
                    textAlign: "center",
                    verticalAlign: "middle",
                    fontSize: 12,
                    color: "var(--kc-text-tertiary)",
                  }}
                >
                  CIDR-блоков нет
                </td>
              </tr>
            )}
            {blocks.map((cidr, i) => {
              const busy = pendingCidr === cidr && (mutate.isPending || opId !== null);
              return (
                <tr
                  key={i}
                  className="kc-kv-row"
                  style={{ height: ROW_H, borderTop: "1px solid var(--kc-border-secondary)" }}
                >
                  <td className="px-3 font-mono text-xs" style={{ verticalAlign: "middle" }}>
                    {cidr}
                  </td>
                  <td className="px-1 text-center" style={{ verticalAlign: "middle" }}>
                    {busy ? (
                      <Spin indicator={<LoadingOutlined style={{ fontSize: 12 }} spin />} />
                    ) : (
                      <Button
                        type="text"
                        danger
                        size="small"
                        icon={<DeleteOutlined />}
                        aria-label="Удалить CIDR"
                        onClick={() => onRemove(cidr)}
                        disabled={inputDisabled}
                      />
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
          <tfoot>
            <tr style={{ borderTop: "1px solid var(--kc-border-secondary)" }}>
              <td style={{ padding: "8px 10px" }} colSpan={2}>
                <Space.Compact style={{ width: "100%" }}>
                  <Input
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    placeholder={placeholder}
                    disabled={inputDisabled}
                    style={{ fontFamily: MONO_FONT, fontSize: 12.5 }}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.preventDefault();
                        onAdd();
                      }
                    }}
                  />
                  <Button
                    type="dashed"
                    icon={<PlusOutlined />}
                    onClick={onAdd}
                    disabled={!draft.trim() || inputDisabled}
                  >
                    Добавить
                  </Button>
                </Space.Compact>
              </td>
            </tr>
          </tfoot>
        </table>
      </div>

      <OperationToastWatcher
        opId={opId}
        title={opTitle}
        onDone={() => {
          setOpId(null);
          setPendingCidr(null);
          // Широкий prefix — обновляет detail (shell-detail) + list + любые
          // subnet-вью, где видны CIDR-блоки.
          qc.invalidateQueries({ queryKey: ["subnets"] });
        }}
      />
    </div>
  );
}
