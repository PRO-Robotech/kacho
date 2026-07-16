// SecurityGroupDetailPage — обёртка над generic ResourceDetailPage с двумя
// дополнительными табами: Входящий трафик / Исходящий трафик.
//
// Каждый tab фильтрует rules по direction и рендерит resource-specific таблицу:
// Протокол | Диапазон портов | Тип источника | Источник | Описание.

import { useCallback, useMemo } from "react";
import { useParams } from "react-router-dom";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { ResourceFormModal } from "@shared/components/organisms/ResourceFormModal";
import { InlineSecurityGroupEditForm } from "@shared/components/organisms/InlineSecurityGroupEditForm";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useNestedBreadcrumb } from "@shared/lib/use-nested-breadcrumb";
import type { DetailTab } from "@shared/components/organisms/DetailShell";

interface SgRule {
  id?: string;
  direction?: string;
  description?: string;
  protocol_name?: string;
  protocol_number?: number;
  ports?: { from_port?: number | string; to_port?: number | string };
  cidr_blocks?: { v4_cidr_blocks?: string[]; v6_cidr_blocks?: string[] };
  security_group_id?: string;
  predefined_target?: string;
}

function protocolLabel(r: SgRule): string {
  if (r.protocol_name) return r.protocol_name;
  if (typeof r.protocol_number === "number") return `proto ${r.protocol_number}`;
  return "Any";
}

function portsLabel(r: SgRule): string {
  if (!r.ports) return "—";
  const f = r.ports.from_port;
  const t = r.ports.to_port;
  if (f == null && t == null) return "—";
  if (f === t || t == null) return String(f);
  return `${f}–${t}`;
}

function targetParts(r: SgRule): { kind: string; value: string } {
  if (r.cidr_blocks) {
    const v4 = r.cidr_blocks.v4_cidr_blocks ?? [];
    const v6 = r.cidr_blocks.v6_cidr_blocks ?? [];
    return { kind: "CIDR", value: [...v4, ...v6].join(", ") || "—" };
  }
  if (r.security_group_id) return { kind: "SG", value: r.security_group_id };
  if (r.predefined_target) return { kind: "Predefined", value: r.predefined_target };
  return { kind: "—", value: "—" };
}

function RulesTable({ rules }: { rules: SgRule[] }) {
  if (rules.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
        Правил нет — трафик блокируется (default-deny).
      </div>
    );
  }
  return (
    <div className="rounded-lg border border-border overflow-hidden bg-card">
      <table className="w-full text-sm">
        <thead className="bg-muted/40 text-xs uppercase tracking-wide">
          <tr>
            <th className="text-left px-3 py-2">Протокол</th>
            <th className="text-left px-3 py-2">Диапазон портов</th>
            <th className="text-left px-3 py-2">Тип источника</th>
            <th className="text-left px-3 py-2">Источник</th>
            <th className="text-left px-3 py-2">Описание</th>
          </tr>
        </thead>
        <tbody>
          {rules.map((r, i) => {
            const tgt = targetParts(r);
            return (
              <tr key={r.id ?? i} className="border-t border-border hover:bg-muted/20">
                <td className="px-3 py-2">{protocolLabel(r)}</td>
                <td className="px-3 py-2">{portsLabel(r)}</td>
                <td className="px-3 py-2 text-xs text-muted-foreground">{tgt.kind}</td>
                <td className="px-3 py-2 font-mono text-xs">{tgt.value}</td>
                <td className="px-3 py-2 text-xs">{r.description || "—"}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

export function SecurityGroupDetailPage() {
  const spec = REGISTRY["security-groups"];
  const { projectId, networkId } = useParams();

  const { segments: breadcrumbSegments, backHref: backHrefOverride } = useNestedBreadcrumb({
    projectId,
    networkId,
    currentResourcePlural: spec.plural,
  });

  const extraTabs = useMemo(
    () =>
      (data: Record<string, unknown>): DetailTab[] => {
        const allRules = (data.rules as SgRule[] | undefined) ?? [];
        const ingress = allRules.filter((r) => (r.direction ?? "INGRESS").toUpperCase() === "INGRESS");
        const egress = allRules.filter((r) => (r.direction ?? "").toUpperCase() === "EGRESS");
        return [
          {
            id: "ingress",
            label: "Входящий трафик",
            count: ingress.length,
            render: () => <RulesTable rules={ingress} />,
          },
          {
            id: "egress",
            label: "Исходящий трафик",
            count: egress.length,
            render: () => <RulesTable rules={egress} />,
          },
        ];
      },
    [],
  );

  const renderInlineEdit = useCallback(
    (data: Record<string, unknown>, exitEdit: () => void) => {
      const id = (data.id as string | undefined) ?? "";
      const fid = (data.project_id as string | undefined) ?? projectId ?? "";
      if (!id || !fid) return null;
      return <InlineSecurityGroupEditForm projectId={fid} sgId={id} onCancel={exitEdit} />;
    },
    [projectId],
  );

  return (
    <>
      <ResourceDetailPage
        spec={spec}
        extraTabs={extraTabs}
        backHrefOverride={backHrefOverride}
        breadcrumbSegments={breadcrumbSegments}
        renderInlineEdit={renderInlineEdit}
      />
      {projectId && <ResourceFormModal projectId={projectId} />}
    </>
  );
}
