// ResourceCreatePage — full-page форма Create (не modal).

import { useMemo, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams, useSearchParams } from "react-router";
import { Alert, Typography } from "antd";
import { ResourceFormBody } from "@shared/components/organisms/form/ResourceFormBody";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { api } from "@shared/api/client";
import { applyFieldDefaults, mutationBasePath, type ResourceSpec } from "@shared/lib/resource-registry";
import { setByPath } from "@shared/lib/path";
import { presetFieldsForSpec } from "@shared/lib/preset-fields";
import { buildCreateBody } from "@shared/lib/update-mask";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";
import { mutationFailureText, subjectNameOf, subjectOfSpec } from "@shared/lib/mutation-signal";
import { useSignalledMutation } from "@shared/lib/use-signalled-mutation";

interface Props {
  spec: ResourceSpec;
  parentField?: string;
  parentParam?: string;
  parentValue?: string | null;
}

export function ResourceCreatePage({ spec, parentField, parentParam, parentValue }: Props) {
  const params = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const filterValue = parentValue ?? (parentParam ? (params[parentParam] ?? null) : null);
  const invalidate = useInvalidateResourceList();

  const ctx = useMemo(
    () => ({
      projectId: parentField === "project_id" ? (filterValue ?? undefined) : undefined,
      accountId: parentField === "account_id" ? (filterValue ?? undefined) : undefined,
    }),
    [parentField, filterValue],
  );

  // Контекст приходит либо из nested-URL params (`/networks/<n>/.../create`),
  // либо из query (`?network_id=...&subnet_id=...&kind=...`) для обратной
  // совместимости и для случая create-из-list-page с pre-selected parent.
  // `presetFields` — заблокированные (immutable) поля из контекста.
  // `softPresetFields` — предзаполненные, но editable (начальное значение,
  // не lock). Пример: `_address_kind` для адреса из контекста подсети — дефолт
  // "internal", но пользователь может переключить на "internal_v6".
  const { presetFields, softPresetFields, fieldOptionsFilter } = useMemo(() => {
    const out: Record<string, unknown> = {};
    const soft: Record<string, unknown> = {};
    const optFilter: Record<string, string[]> = {};
    const subnetId = params.subnetId ?? searchParams.get("subnet_id");
    const networkId = params.networkId ?? searchParams.get("network_id");
    const kind = searchParams.get("kind");
    if (spec.id === "addresses" && subnetId) {
      // Адрес в контексте подсети — только ВНУТРЕННИЙ (internal); привязан к
      // этой подсети, sanitize выкинет неактивную ветку.
      out["internal_ipv4_address_spec.subnet_id"] = subnetId;
      out["internal_ipv6_address_spec.subnet_id"] = subnetId;
      soft["_address_kind"] = kind === "internal_v6" ? "internal_v6" : "internal";
      optFilter["_address_kind"] = ["internal", "internal_v6"];
    } else {
      if (kind) out["_address_kind"] = kind;
      if (subnetId) out["subnet_id"] = subnetId;
      // Глобальное создание адреса — только ВНЕШНИЙ (external); internal задаётся
      // из контекста подсети.
      if (spec.id === "addresses") optFilter["_address_kind"] = ["external", "external_v6"];
    }
    if (networkId) out["network_id"] = networkId;
    // A preset may only name a field this form has. The keys come from the URL, so
    // their set is chosen by whoever built the link — `network_id` above is set for
    // every spec, and most Create messages have no such field. An unmatched preset
    // would ride out as an undeclared request key and be dropped at the edge in
    // silence.
    return {
      presetFields: presetFieldsForSpec(spec.fields, out),
      softPresetFields: presetFieldsForSpec(spec.fields, soft),
      fieldOptionsFilter: optFilter,
    };
  }, [params.subnetId, params.networkId, searchParams, spec.id]);

  const initialObj = useMemo(() => {
    const tpl = spec.template(ctx);
    const baseObj = typeof tpl === "object" && tpl !== null ? { ...(tpl as Record<string, unknown>) } : {};
    let merged: Record<string, unknown> = applyFieldDefaults(spec.fields, baseObj);
    for (const [path, val] of Object.entries(softPresetFields)) {
      merged = setByPath(merged, path, val);
    }
    for (const [path, val] of Object.entries(presetFields)) {
      merged = setByPath(merged, path, val);
    }
    // Auto-name: пустое name + UNIQUE на (project_id, name) → ALREADY_EXISTS
    // на повторе. Генерируем <route>-NNNNNN.
    if (spec.fields?.some((f) => f.name === "name") && (!merged.name || merged.name === "")) {
      const stem = spec.route.replace(/-/g, "");
      merged.name = `${stem}-${Math.floor(100000 + Math.random() * 900000)}`;
    }
    return merged;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [obj, setObj] = useState<Record<string, unknown>>(initialObj);

  const lockedPathsRef = useRef(new Set(Object.keys(presetFields)));

  // Back = текущий path без /create суффикса. Для nested URL вида
  //   /projects/X/vpc/networks/Y/route-tables/create
  // полученный URL `/projects/X/vpc/networks/Y/route-tables` не существует —
  // вместо этого возвращаемся к parent detail (network detail с табом).
  const rawBack = location.pathname.replace(/\/create$/, "") || "/";
  const projectId = params.projectId;
  const networkId = params.networkId;
  const subnetId = params.subnetId;
  const isNestedUnderSubnet = !!(projectId && subnetId);
  const isNestedUnderNetwork = !!(projectId && networkId);
  const backHref = isNestedUnderSubnet
    ? networkId
      ? `/projects/${projectId}/vpc/networks/${networkId}/subnets/${subnetId}?tab=addresses`
      : `/projects/${projectId}/vpc/subnets/${subnetId}?tab=addresses`
    : isNestedUnderNetwork
      ? `/projects/${projectId}/vpc/networks/${networkId}?tab=${spec.route}`
      : rawBack;

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        {spec.serviceTitle && (
          <>
            <Typography.Text type="secondary">{spec.serviceTitle}</Typography.Text>
            <Typography.Text type="secondary">/</Typography.Text>
          </>
        )}
        <Link to={backHref}>
          <Typography.Text type="secondary">{spec.plural}</Typography.Text>
        </Link>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Создать</Typography.Text>
      </span>
    ),
    [backHref, spec.plural, spec.serviceTitle],
  );
  useBreadcrumb(breadcrumb);
  const noHeaderRight = useMemo(() => null, []);
  useHeaderRight(noHeaderRight);

  // Исход мутации — через единый механизм (`use-signalled-mutation`): он же
  // разбирает ответ, поллит операцию и сообщает про все три исхода одной формой.
  // Мутация уходит на admin-плоскость ресурса, если она у него есть: публичный
  // путь geo Region/Zone обслуживает только чтение (POST по нему не
  // смаршрутизирован никем).
  const mutation = useSignalledMutation<Record<string, unknown>>({
    verb: "create",
    subject: (body) => subjectOfSpec(spec, subjectNameOf(body)),
    expectOperation: spec.mutationsReturnOperation !== false,
    mutationFn: (item) => api.create(mutationBasePath(spec), item),
    onSucceeded: () => {
      invalidate(spec.id, filterValue ?? null);
      void navigate(backHref);
    },
  });

  const submit = () => {
    let parsed: Record<string, unknown> = obj;
    // Инвариант спеки — ДО sanitize: он читает UI-дискриминаторы формы (какая
    // ветка oneof выбрана), а sanitize их как раз и срезает. Поле объявлено на
    // спеке и читается инлайн-формой; страница его не читала — то есть один и
    // тот же ресурс проверялся в модалке и не проверялся на странице.
    if (spec.validate) {
      const err = spec.validate(parsed);
      if (err) {
        // Отказ собственной проверки — тот же исход «не создан», что и отказ
        // края: пользователю важно, что ресурс не создан и почему, а не то,
        // на чьей стороне это выяснилось.
        toast.error(mutationFailureText("create", subjectOfSpec(spec, subjectNameOf(obj)), err));
        return;
      }
    }
    if (spec.sanitize) parsed = spec.sanitize(parsed);
    // sanitize shapes the domain payload; this drops what only ever existed for the
    // widget (`_`-prefixed discriminators, incl. inside array items) so it cannot
    // reach the wire as `Placement` / `AddressKind` / `BootSource` and be discarded
    // there in silence.
    mutation.run(buildCreateBody(parsed));
  };

  const fields = spec.fields;
  if (!fields) {
    return <Alert type="warning" message={`У ресурса ${spec.singular} нет form-schema; используйте API напрямую.`} />;
  }

  return (
    // Ширины здесь НЕТ: страница занимает свою область целиком, как список и
    // карточка. Ограничивает ширину только колонка ПОЛЕЙ, внутри оболочки формы,
    // — заголовок и черта под ним обязаны идти на всю ширину страницы.
    //
    // Пока сужена была вся страница, черта под заголовком кончалась на 820-й
    // точке против полутора тысяч у списка, и переход «список → создание» рвал
    // обе линии сразу: и текст, и подчёркивание под ним.
    <div>
      {/* Беклинк убран (req) — путь назад есть в breadcrumb хедера. */}
      <ResourceFormBody
        spec={spec}
        mode="create"
        obj={obj}
        onChange={setObj}
        lockedPaths={lockedPathsRef.current}
        fieldOptionsFilter={fieldOptionsFilter}
        // Предмет назван заголовком формы прямо над кнопкой — здесь короткое
        // «Создать» (решение владельца).
        submitLabel="Создать"
        submitting={mutation.pending}
        onSubmit={submit}
        onCancel={() => navigate(backHref)}
      />
    </div>
  );
}
