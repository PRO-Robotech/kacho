// IamLayout — обёртка для /iam/* страниц. Горизонтальные табы между IAM
// ресурсами + Account-селектор секции (account-scoped ресурсы — Project /
// ServiceAccount — фильтруются по нему, аналог project-селектора у VPC).
//
// На E0: без auth-interceptor; UI шлёт запросы анонимно.

import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { Space, Tabs, Typography, Alert, Select } from "antd";
import { useQuery } from "@tanstack/react-query";
import { iamApi, type Account } from "@shared/api/iam";
import { contextApi, useContext } from "@shared/lib/context-store";

const TABS = [
  { key: "/iam/accounts", label: "Accounts" },
  { key: "/iam/projects", label: "Projects" },
  { key: "/iam/users", label: "Users" },
  { key: "/iam/service-accounts", label: "Service Accounts" },
  { key: "/iam/groups", label: "Groups" },
  { key: "/iam/roles", label: "Roles" },
  { key: "/iam/access-bindings", label: "Access Bindings" },
];

export function IamLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  const account = useContext((s) => s.account);

  const active = TABS.find((t) => location.pathname.startsWith(t.key))?.key ?? TABS[0].key;

  const accounts = useQuery({
    queryKey: ["iam", "accounts", "list"],
    queryFn: () => iamApi.listAccounts({ pageSize: "1000" }),
    staleTime: 30_000,
  });
  const accountList = accounts.data?.accounts ?? [];

  return (
    <Space direction="vertical" size={16} style={{ width: "100%" }}>
      <div
        style={{
          display: "flex",
          alignItems: "flex-start",
          justifyContent: "space-between",
          gap: 16,
          flexWrap: "wrap",
        }}
      >
        <div>
          <Typography.Title level={3} className="t-page-title" style={{ margin: 0 }}>
            Identity and Access Management
          </Typography.Title>
          <Typography.Text type="secondary" style={{ fontSize: 13 }}>
            Управление доступом: Accounts, Projects, Users, Service Accounts, Groups, Roles, Access Bindings.
          </Typography.Text>
        </div>

        <Space size={8} align="center">
          <Typography.Text type="secondary">Account:</Typography.Text>
          <Select
            style={{ width: 280 }}
            placeholder="Выберите Account"
            value={account?.id}
            onChange={(id) => {
              const a = accountList.find((x: Account) => x.id === id);
              contextApi.setAccount(a ? { id: a.id, name: a.name } : null);
            }}
            options={accountList.map((a: Account) => ({
              value: a.id,
              label: `${a.name} · ${a.id}`,
            }))}
            loading={accounts.isLoading}
            showSearch
            optionFilterProp="label"
            allowClear
            onClear={() => contextApi.setAccount(null)}
            notFoundContent="Account'ов нет"
          />
        </Space>
      </div>

      <Alert
        type="info"
        showIcon
        message="Auth-flow не активирован (E0)"
        description="На текущей фазе UI ходит в api-gateway анонимно. Account-селектор задаёт scope для Project / ServiceAccount."
        style={{ marginBottom: 0 }}
      />

      <Tabs
        activeKey={active}
        onChange={(k) => navigate(k)}
        items={TABS.map((t) => ({ key: t.key, label: t.label }))}
        size="middle"
        style={{ marginBottom: 0 }}
      />

      <Outlet />
    </Space>
  );
}
