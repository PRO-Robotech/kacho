// IamScopedListShell — обёртка для account-scoped IAM-ресурсов (Project,
// ServiceAccount). Backend ListProjects / ListServiceAccounts требует
// account_id, поэтому список показывается только когда в IAM-секции выбран
// Account (context-store). Аналог project-scope у VPC-страниц.
//
// Заглушка «аккаунт не выбран» — ОДНА на весь раздел
// (`ScopeRequiredEmpty`): прежде здесь стоял `Empty` от antd, прижатый к
// верхнему краю, и таких форм у одного вопроса было шесть.

import { ScopeRequiredEmpty } from "@/components/molecules/ScopeRequiredEmpty";
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
    return <ScopeRequiredEmpty purpose={`увидеть ${spec.plural}`} />;
  }

  return (
    // IAM-секция регистрирует /iam/<resource>/create и /:id/edit.
    <ResourceListPage
      spec={spec}
      parentField="account_id"
      parentValue={account.id}
      disableChildRoute={disableChildRoute}
      panelForms
    />
  );
}
