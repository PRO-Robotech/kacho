// SubnetDetailPage — расширение generic ResourceDetailPage с utilization-баром
// и tab "IP-адреса", который показывает Address-ресурсы привязанные к этому
// subnet (через internal_ipv4_address.subnet_id), используя те же колонки,
// что и /projects/X/addresses.

import { useCallback, useMemo, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { Button as AntButton, Input, Space, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { Button } from "@shared/components/atoms/ui/Button";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { ResourceTable, type Column } from "@shared/components/organisms/ResourceTable";
import { RowActionsMenu } from "@shared/components/molecules/RowActionsMenu";
import { InlineSubnetEditForm } from "@shared/components/organisms/InlineSubnetEditForm";
import { ResourceFormModal } from "@shared/components/organisms/ResourceFormModal";
import { api } from "@shared/api/client";
import { REGISTRY, getByPath } from "@shared/lib/resource-registry";
import { useNestedBreadcrumb } from "@shared/lib/use-nested-breadcrumb";
import { buildSpecColumns } from "@shared/lib/spec-columns";
import type { DetailTab } from "@shared/components/organisms/DetailShell";

export function SubnetDetailPage() {
  const { uid: subnetId, projectId, networkId } = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const spec = REGISTRY["subnets"];
  const addrSpec = REGISTRY["addresses"];

  // Открыть модалку резервирования IP в контексте текущей подсети
  // (subnetId передаётся через query → ResourceFormModal превратит его в
  // preset internal_ipv4/v6_address_spec.subnet_id, см. ResourceFormModal.tsx).
  const openReserveModal = useCallback(() => {
    if (!subnetId) return;
    const next = new URLSearchParams(searchParams);
    next.set("modal", "addresses-create");
    next.set("subnetId", subnetId);
    setSearchParams(next, { replace: false });
  }, [searchParams, setSearchParams, subnetId]);

  const { segments: breadcrumbSegments, backHref: backHrefOverride } = useNestedBreadcrumb({
    projectId,
    networkId,
    currentResourcePlural: spec.plural,
  });

  // Адреса под subnet всегда nested под subnet'ом (с/без network в цепочке).
  const addressesBasePath =
    projectId && subnetId
      ? networkId
        ? `/projects/${projectId}/vpc/networks/${networkId}/subnets/${subnetId}/addresses`
        : `/projects/${projectId}/vpc/subnets/${subnetId}/addresses`
      : null;

  // Address-ресурсы проекта — будем фильтровать по subnet_id client-side.
  const { data: addrList } = useQuery({
    queryKey: ["addresses", "list", projectId],
    queryFn: () =>
      api.list<{ addresses: Array<Record<string, unknown>> }>(addrSpec.apiPath, {
        project_id: projectId!,
        pageSize: "500",
      }),
    refetchInterval: 5000,
    enabled: !!projectId,
  });

  const subnetAddresses = useMemo(() => {
    const all = addrList?.addresses ?? [];
    return all.filter((a) => {
      const v4 = a.internal_ipv4_address as { subnet_id?: string } | undefined;
      const v6 = a.internal_ipv6_address as { subnet_id?: string } | undefined;
      return v4?.subnet_id === subnetId || v6?.subnet_id === subnetId;
    });
  }, [addrList, subnetId]);

  // Колонки = те же, что у Addresses list, плюс actions.
  const addrColumns = useMemo<Column<Record<string, unknown>>[]>(() => {
    const cols = buildSpecColumns(addrSpec, { projectId });
    if (addressesBasePath) {
      cols.push({
        header: "",
        className: "text-right whitespace-nowrap",
        cell: (row) => (
          <RowActionsMenu spec={addrSpec} row={row} basePath={addressesBasePath} projectId={projectId ?? null} />
        ),
      });
    }
    return cols;
  }, [addrSpec, addressesBasePath, projectId]);

  const extraTabs = useMemo(
    () => (): DetailTab[] => [
      {
        id: "addresses",
        label: "IP-адреса",
        count: subnetAddresses.length,
        render: () => (
          <AddressesSection
            rows={subnetAddresses}
            columns={addrColumns}
            onReserve={subnetId ? openReserveModal : null}
            onClick={(id) => addressesBasePath && navigate(`${addressesBasePath}/${id}`)}
          />
        ),
      },
      // tab "Операции" автоматически добавляется ResourceDetailPage —
      // не дублируем здесь.
    ],
    [subnetAddresses, addrColumns, addressesBasePath, navigate, subnetId, openReserveModal],
  );

  const headerActionsByTab = useCallback(
    (tabId: string) => {
      if (tabId === "addresses" && subnetId) {
        return (
          <AntButton type="primary" size="small" icon={<PlusOutlined />} onClick={openReserveModal}>
            Зарезервировать IP-адрес
          </AntButton>
        );
      }
      return null;
    },
    [subnetId, openReserveModal],
  );

  const renderInlineEdit = useCallback(
    (_data: Record<string, unknown>, exitEdit: () => void) =>
      projectId && subnetId ? (
        <InlineSubnetEditForm projectId={projectId} subnetId={subnetId} onCancel={exitEdit} />
      ) : null,
    [projectId, subnetId],
  );

  return (
    <>
      <ResourceDetailPage
        spec={spec}
        extraTabs={extraTabs}
        headerActionsByTab={headerActionsByTab}
        backHrefOverride={backHrefOverride}
        breadcrumbSegments={breadcrumbSegments}
        renderInlineEdit={renderInlineEdit}
      />
      {projectId && <ResourceFormModal projectId={projectId} />}
    </>
  );
}

// AddressesSection — Title + filter + table для tab "IP-адреса".
function AddressesSection({
  rows,
  columns,
  onReserve,
  onClick,
}: {
  rows: Array<Record<string, unknown>>;
  columns: Column<Record<string, unknown>>[];
  onReserve: (() => void) | null;
  onClick: (id: string) => void;
}) {
  const [query, setQuery] = useState("");
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((row) => {
      const name = (getByPath<string>(row, "name") ?? "").toLowerCase();
      const id = (getByPath<string>(row, "id") ?? "").toLowerCase();
      const ext = ((row.external_ipv4_address as { address?: string } | undefined)?.address ?? "").toLowerCase();
      const int = ((row.internal_ipv4_address as { address?: string } | undefined)?.address ?? "").toLowerCase();
      const int6 = ((row.internal_ipv6_address as { address?: string } | undefined)?.address ?? "").toLowerCase();
      return name.includes(q) || id.includes(q) || ext.includes(q) || int.includes(q) || int6.includes(q);
    });
  }, [rows, query]);

  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Typography.Title level={4} style={{ margin: 0 }}>
        IP-адреса
      </Typography.Title>
      <Input.Search
        placeholder="Фильтр по имени, идентификатору или IP"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        style={{ maxWidth: 360 }}
        allowClear
      />
      {filtered.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-10 text-center space-y-3">
          <div className="text-base font-medium">
            {query ? "По фильтру ничего не найдено" : "У вас пока нет IP-адресов"}
          </div>
          {!query && (
            <div className="text-xs text-muted-foreground">
              Зарезервируйте адрес, чтобы он автоматически использовал CIDR-блок этой подсети.
            </div>
          )}
          {!query && onReserve && (
            <Button onClick={onReserve}>
              <Plus className="h-4 w-4" /> Зарезервировать IP-адрес
            </Button>
          )}
        </div>
      ) : (
        <ResourceTable
          rows={filtered}
          columns={columns}
          rowKey={(r) => getByPath<string>(r, "id") ?? Math.random().toString()}
          onRowClick={(r) => {
            const id = getByPath<string>(r, "id");
            if (id) onClick(id);
          }}
        />
      )}
    </Space>
  );
}
