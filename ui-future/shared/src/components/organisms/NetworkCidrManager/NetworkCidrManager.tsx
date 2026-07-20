// NetworkCidrManager — управление declared-супернетом сети (IPv4/IPv6) через
// verb-действия, по образцу SubnetCidrManager / AddressPoolCidrManager. Add и
// remove — РАЗНЫЕ методы, поэтому БЕЗ batch-save: каждое действие применяется
// сразу своим RPC (immediate).
//
// VPC-1 wire-format (op-in-response — Network statusless, Operation{done:true}
// приходит сразу + полное тело в .response):
//   POST /vpc/v1/networks/{id}:add-cidr-blocks     { ipv4_cidr_blocks:[string] }
//   POST /vpc/v1/networks/{id}:remove-cidr-blocks  { ipv6_cidr_blocks:[string] }
//
// Супернет неизменяем через Update — эти verb'ы единственный путь его изменения.
// Remove последнего блока, покрывающего живую подсеть → FAILED_PRECONDITION
// ("network CIDR block … still contains subnets") — тогда chip не удаляется и
// показывается toast.error.

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button, Card, Input, Space, Spin, Tag, Typography } from "antd";
import { CloseOutlined, LoadingOutlined, PlusOutlined } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { OperationToastWatcher } from "@shared/components/molecules/OperationToastWatcher";
import { toast } from "@shared/lib/toast";

type CidrKind = "v4" | "v6";

const FIELD_BY_KIND: Record<CidrKind, "ipv4_cidr_blocks" | "ipv6_cidr_blocks"> = {
  v4: "ipv4_cidr_blocks",
  v6: "ipv6_cidr_blocks",
};

const NETWORKS_API = "/vpc/v1/networks";
const MONO_FONT = "ui-monospace, monospace";

function validateCidr(kind: CidrKind, cidr: string): string | null {
  if (!cidr) return "Введите CIDR.";
  if (!cidr.includes("/")) return "CIDR должен содержать префикс (например /16).";
  if (kind === "v6" && !cidr.includes(":")) return "Похоже не на IPv6-адрес.";
  return null;
}

interface SectionProps {
  networkId: string;
  kind: CidrKind;
  blocks: string[];
}

function CidrSection({ networkId, kind, blocks }: SectionProps) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState("");
  const [pendingCidr, setPendingCidr] = useState<string | null>(null);
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("");

  const label = kind === "v4" ? "Супернет IPv4" : "Супернет IPv6";
  const placeholder = kind === "v4" ? "10.30.0.0/16" : "fd00:30::/48";
  const tagColor = kind === "v4" ? "blue" : "geekblue";
  const field = FIELD_BY_KIND[kind];
  const family = kind === "v4" ? "IPv4" : "IPv6";

  const mutate = useMutation({
    mutationFn: (params: { verb: "add" | "remove"; cidr: string }) =>
      api.action(`${NETWORKS_API}/${networkId}:${params.verb}-cidr-blocks`, {
        [field]: [params.cidr],
      }),
    onSuccess: (resp, vars) => {
      const id = extractOperationId(resp);
      if (id) {
        setOpTitle(`${vars.verb === "add" ? "Добавление" : "Удаление"} ${family} супернет-блока ${vars.cidr}`);
        setOpId(id);
      } else {
        qc.invalidateQueries({ queryKey: ["networks"] });
        setPendingCidr(null);
      }
    },
    onError: (err, vars) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`${family} супернет ${vars.verb === "add" ? "добавление" : "удаление"}: ${m}`);
      setPendingCidr(null);
    },
  });

  const busyAny = mutate.isPending || opId !== null;

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
                const busy = pendingCidr === cidr && busyAny;
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

      <OperationToastWatcher
        opId={opId}
        title={opTitle}
        onDone={() => {
          setOpId(null);
          setPendingCidr(null);
          qc.invalidateQueries({ queryKey: ["networks"] });
        }}
      />
    </Card>
  );
}

interface Props {
  networkId: string;
  v4Blocks: string[];
  v6Blocks: string[];
}

export function NetworkCidrManager({ networkId, v4Blocks, v6Blocks }: Props) {
  return (
    <div className="space-y-3">
      <CidrSection networkId={networkId} kind="v4" blocks={v4Blocks} />
      <CidrSection networkId={networkId} kind="v6" blocks={v6Blocks} />
    </div>
  );
}
