// IamScopedListShell — обёртка для account-scoped IAM-ресурсов (Project,
// ServiceAccount). Backend ListProjects / ListServiceAccounts требует
// account_id, поэтому список показывается только когда в IAM-секции выбран
// Account (context-store). Аналог project-scope у VPC-страниц.

import { Empty } from "antd";
import { ResourceListPage } from "@/components/organisms/ResourceListPage";
import { useContext } from "@shared/lib/context-store";
import type { ResourceSpec } from "@shared/lib/resource-registry";

export function IamScopedListShell({
  spec,
  disableChildRoute = false,
}: {
  spec: ResourceSpec;
  disableChildRoute?: boolean;
}) {
  const account = useContext((s) => s.account);

  if (!account) {
    return (
      <Empty
        description={`Выберите Account вверху секции, чтобы увидеть ${spec.plural}.`}
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        style={{ padding: "48px 0" }}
      />
    );
  }

  return (
    <ResourceListPage
      spec={spec}
      parentField="account_id"
      parentValue={account.id}
      disableChildRoute={disableChildRoute}
    />
  );
}
