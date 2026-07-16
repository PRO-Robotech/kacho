// ResourceFormModal — generic модалка Create/Edit для любого VPC-ресурса.
// Открывается через query:
//   ?modal=<spec.id>-create[&...preset]   ← создание
//   ?modal=<spec.id>-edit&id=<uid>        ← редактирование
//
// Внутри модалки используется:
//   * Custom-инлайн форма (если ресурс её имеет — InlineSubnetCreateForm,
//     InlineSubnetEditForm, InlineSecurityGroupEditForm и т.п.).
//   * Иначе — generic InlineResourceCreateForm / InlineResourceEditForm
//     по spec.fields.
//
// Mount: один экземпляр на каждой странице, где могут открываться модалки
// (List / Detail). Сама модалка — fragment-noop пока в URL нет ?modal=.

import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Modal } from "antd";
import { InlineResourceForm } from "@shared/components/organisms/InlineResourceForm";
import { FORM_WIDTH } from "@shared/components/organisms/form/FormShell";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useContext } from "@shared/lib/context-store";
import { api } from "@shared/api/client";

interface Props {
  projectId: string;
}

export function ResourceFormModal({ projectId }: Props) {
  const [searchParams, setSearchParams] = useSearchParams();
  const modal = searchParams.get("modal") ?? "";
  // Account-scoped IAM-ресурсы (Project / ServiceAccount) берут account_id из
  // выбранного в IAM-секции Account — пробрасываем в ctx.template.
  const accountId = useContext((s) => s.account?.id);

  // Парсим `<spec-id>-(create|edit)`.
  const match = modal.match(/^([a-z-]+)-(create|edit)$/);
  const specId = match?.[1];
  const action = match?.[2] as "create" | "edit" | undefined;
  const spec = specId ? REGISTRY[specId] : undefined;
  const id = searchParams.get("id") ?? undefined;

  const close = useCallback(() => {
    const params = new URLSearchParams(searchParams);
    params.delete("modal");
    params.delete("id");
    // Сохраняем networkId / subnetId preset как контекст-параметры (могут
    // быть нужны parent-странице) — НЕ удаляем.
    setSearchParams(params, { replace: true });
  }, [searchParams, setSearchParams]);

  // Для Edit: загружаем ресурс (нужно для InlineResourceEditForm — она
  // принимает data, а не id).
  const { data: editData } = useQuery({
    queryKey: [specId, "detail", id],
    queryFn: () => api.get<Record<string, unknown>>(`${spec?.apiPath}/${id}`),
    enabled: action === "edit" && !!spec && !!id,
  });

  // Preset-fields для Create: пробрасываем все query-params кроме служебных.
  const presetFields = useMemo(() => {
    if (action !== "create") return undefined;
    const fields: Record<string, unknown> = {};
    for (const [k, v] of searchParams.entries()) {
      if (k === "modal" || k === "id") continue;
      // network_id / subnet_id и т.п. — preset (как из формы).
      fields[k.replace(/([A-Z])/g, "_$1").toLowerCase()] = v;
    }
    return fields;
  }, [action, searchParams]);

  // Единая ширина для ВСЕХ модалок ресурсов — visual unity (= FORM_WIDTH,
  // тот же стандарт, что и у page-форм).
  const width = FORM_WIDTH;

  if (!spec || !action) return null;
  if (action === "edit" && (!id || !editData)) {
    // Открыли edit-модалку, но id/данные ещё не загружены — пустая.
    return null;
  }

  const title = action === "create" ? `Создание: ${spec.singular}` : `Редактирование: ${spec.singular}`;

  // Диспетч кастомных/generic форм вынесен в InlineResourceForm (тот же
  // компонент использует form-panel ResourceShell в зоне 3 detail-страницы).
  const formNode = (
    <InlineResourceForm
      spec={spec}
      action={action}
      id={id}
      data={editData}
      projectId={projectId}
      accountId={accountId}
      presetFields={presetFields}
      networkId={searchParams.get("networkId") ?? undefined}
      subnetId={searchParams.get("subnetId") ?? searchParams.get("subnet_id") ?? undefined}
      onCancel={close}
      onSuccess={close}
    />
  );

  return (
    <Modal
      open
      onCancel={close}
      footer={null}
      width={width}
      destroyOnClose
      // Клик по маске вне модалки → закрытие (user UX-запрос).
      maskClosable={true}
      title={null}
      // FormShell сам рендерит band-шапку + подложку-карточку. Modal content/
      // body — тёмный фон (--kc-page) через .kc-form-modal (см. index.css),
      // чтобы карточка формы «всплывала» как на welcome-странице «первый
      // ресурс». Паддинг 16 — небольшая рамка вокруг карточки.
      className="kc-form-modal"
      styles={{ body: { padding: 16 } }}
      // Anim — стандартная Antd (zoom from center). Без transform-origin
      // override модалка появляется из центра, не от точки клика.
    >
      {/* Унифицирующая обёртка: maxWidth → forms not slip past modal width;
          Inline-форма сама рендерит Title level=4 + content. */}
      <div style={{ width: "100%" }}>{formNode}</div>
      <span style={{ position: "absolute", left: -10000 }} aria-hidden>
        {title}
      </span>
    </Modal>
  );
}
