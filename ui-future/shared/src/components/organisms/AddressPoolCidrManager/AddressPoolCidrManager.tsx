// AddressPoolCidrManager — управление CIDR-блоками пула адресов (IPv4/IPv6)
// через отдельные RPC, по образцу SubnetCidrManager. Add и remove — РАЗНЫЕ
// методы, поэтому БЕЗ read/edit-режима с batch-save: каждое действие
// применяется сразу своим RPC (immediate).
//
// Wire-format (sync — AddressPool возвращается напрямую, НЕ Operation, паритет
// с тем, что Create пула тоже sync):
//   POST /vpc/v1/addressPools/{id}:addCidrBlocks
//        { address_pool_id, v4_cidr_blocks:[string], v6_cidr_blocks:[string] }
//   POST /vpc/v1/addressPools/{id}:removeCidrBlocks   { … }
//
// KAC-269: AddressPool.Update БОЛЬШЕ НЕ меняет CIDR (proto убрал
// v4/v6_cidr_blocks + replace_* из UpdateAddressPoolRequest). Единственный путь
// изменения CIDR пула — эти verb'ы. Remove запрещён, если в удаляемом блоке есть
// allocated IP → FailedPrecondition ("CIDR has allocated addresses"), тогда chip
// не удаляется и показывается toast.error.
//
// Визуально идентичен SubnetCidrChips (AntD Card «IPv4/IPv6 CIDR blocks N
// блок(ов)» + Tag-chip-list blue/geekblue + input + Add), но мутирует через RPC
// со spinner на chip во время запроса.

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button, Card, Input, Space, Spin, Tag, Typography } from "antd";
import { CloseOutlined, LoadingOutlined, PlusOutlined } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { toast } from "@shared/lib/toast";

type CidrKind = "v4" | "v6";

const FIELD_BY_KIND: Record<CidrKind, "v4_cidr_blocks" | "v6_cidr_blocks"> = {
  v4: "v4_cidr_blocks",
  v6: "v6_cidr_blocks",
};

const POOLS_API = "/vpc/v1/addressPools";
const MONO_FONT = "ui-monospace, monospace";

function validateCidr(kind: CidrKind, cidr: string): string | null {
  if (!cidr) return "Введите CIDR.";
  if (!cidr.includes("/")) return "CIDR должен содержать префикс (например /24).";
  if (kind === "v6" && !cidr.includes(":")) return "Похоже не на IPv6-адрес.";
  return null;
}

interface SectionProps {
  poolId: string;
  kind: CidrKind;
  blocks: string[];
}

function CidrSection({ poolId, kind, blocks }: SectionProps) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState("");
  const [pendingCidr, setPendingCidr] = useState<string | null>(null);

  const label = kind === "v4" ? "IPv4 CIDR blocks" : "IPv6 CIDR blocks";
  const placeholder = kind === "v4" ? "198.51.100.0/24" : "2001:db8::/64";
  const tagColor = kind === "v4" ? "blue" : "geekblue";
  const field = FIELD_BY_KIND[kind];
  const family = kind === "v4" ? "IPv4" : "IPv6";

  const mutate = useMutation({
    // RPC sync → AddressPool напрямую (api.post, не Operation envelope).
    mutationFn: (params: { verb: "add" | "remove"; cidr: string }) =>
      api.post<Record<string, unknown>>(`${POOLS_API}/${poolId}:${params.verb}CidrBlocks`, {
        address_pool_id: poolId,
        [field]: [params.cidr],
      }),
    onSuccess: () => {
      // Широкий prefix-инвалидейт: ["address-pools"] матчит detail-страницу
      // (["address-pools","detail",uid]) и list; плюс utilization-виджет.
      qc.invalidateQueries({ queryKey: ["address-pools"] });
      qc.invalidateQueries({ queryKey: ["pool-util", poolId] });
      setPendingCidr(null);
    },
    onError: (err, vars) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`${family} CIDR ${vars.verb === "add" ? "добавление" : "удаление"}: ${m}`);
      setPendingCidr(null);
    },
  });

  const busyAny = mutate.isPending;

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
    if (busyAny) return;
    setPendingCidr(cidr);
    mutate.mutate({ verb: "remove", cidr });
  };

  return (
    <Card
      size="small"
      title={
        <Space size={8}>
          <Typography.Text strong>{label}</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>
            {blocks.length} блок(ов)
          </Typography.Text>
        </Space>
      }
    >
      <Space direction="vertical" size={8} style={{ width: "100%" }}>
        <div style={{ minHeight: 24 }}>
          {blocks.length === 0 ? (
            <Typography.Text type="secondary" italic style={{ fontSize: 12 }}>
              — пусто —
            </Typography.Text>
          ) : (
            <Space size={[6, 6]} wrap>
              {blocks.map((cidr) => {
                const busy = pendingCidr === cidr && mutate.isPending;
                return (
                  <Tag
                    key={cidr}
                    color={tagColor}
                    closable={!busy}
                    closeIcon={
                      busy ? (
                        <Spin indicator={<LoadingOutlined style={{ fontSize: 10 }} spin />} />
                      ) : (
                        <CloseOutlined style={{ fontSize: 10 }} />
                      )
                    }
                    onClose={(e) => {
                      e.preventDefault();
                      onRemove(cidr);
                    }}
                    style={{ fontFamily: MONO_FONT, fontSize: 12, margin: 0 }}
                  >
                    {cidr}
                  </Tag>
                );
              })}
            </Space>
          )}
        </div>
        <Space.Compact style={{ width: "100%" }}>
          <Input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={placeholder}
            disabled={busyAny}
            style={{ fontFamily: MONO_FONT, fontSize: 12 }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                onAdd();
              }
            }}
          />
          <Button type="primary" ghost onClick={onAdd} disabled={!draft.trim() || busyAny} icon={<PlusOutlined />}>
            Add
          </Button>
        </Space.Compact>
      </Space>
    </Card>
  );
}

interface Props {
  poolId: string;
  v4Blocks: string[];
  v6Blocks: string[];
}

export function AddressPoolCidrManager({ poolId, v4Blocks, v6Blocks }: Props) {
  return (
    <div className="space-y-3">
      <CidrSection poolId={poolId} kind="v4" blocks={v4Blocks} />
      <CidrSection poolId={poolId} kind="v6" blocks={v6Blocks} />
    </div>
  );
}
