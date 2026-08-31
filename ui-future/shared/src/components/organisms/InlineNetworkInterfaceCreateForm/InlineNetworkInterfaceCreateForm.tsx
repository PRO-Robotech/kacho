// InlineNetworkInterfaceCreateForm — NIC create модалка. Зеркально к
// InlineNetworkInterfaceEditForm: тот же состав полей и тот же порядок, иначе
// одно и то же место продукта читается как два разных.
//
// Подсеть выбирается своим списком (не `RefSelect`): её кандидатов надо
// прочитать здесь же — по выбранной подсети фильтруются адреса, а её сеть
// решает, какие группы безопасности показывать. Адреса и группы — `RefMultiSelect`.
//
// Геометрия (ширина колонки подписи) берётся из `FormGrid`, одна на все формы
// консоли; здесь её не объявляют. Прежняя редакция этой шапки называла и
// компонент подсети, и число 140 — ни то, ни другое дереву не отвечало.

import { useEffect, useMemo, useState } from "react";
import { addressIsFree } from "@shared/lib/address-availability";
import { FORM_DIVIDER_STYLE } from "@shared/components/organisms/form/editor-surface";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Form, Input, Select, Space, Tooltip } from "antd";
import { QuestionCircleOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScopeOfSpec } from "@shared/lib/picker-search";
import { useKeptLabel } from "@shared/lib/kept-choice";
import { RefMultiSelect } from "@shared/components/organisms/form/RefSelect";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { FieldError } from "@shared/components/organisms/form/FieldError";
import { LabelsEditor, labelsToMap, type LabelEntry } from "@shared/components/organisms/LabelsEditor";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useInvalidateResourceList, useOperation } from "@shared/lib/use-operation";
import { resolveMutationResponse } from "@shared/lib/operation-outcome";
import { toast } from "@shared/lib/toast";
import { errorText } from "@shared/lib/error-presentation";

interface Props {
  projectId: string;
  /** subnet_id preset из контекста (например, из subnet detail). Если задан —
   *  Subnet locked; иначе — RefSelect. */
  subnetId?: string;
  onCancel: () => void;
  onSuccess?: () => void;
}

const labelWithInfo = (text: string, info: string) => (
  <Space size={4}>
    {text}
    <Tooltip title={info}>
      <QuestionCircleOutlined style={{ color: "var(--kc-text-tertiary)" }} />
    </Tooltip>
  </Space>
);

function autoName(): string {
  return `nic-${Math.floor(100000 + Math.random() * 900000)}`;
}

export function InlineNetworkInterfaceCreateForm({ projectId, subnetId: presetSubnetId, onCancel, onSuccess }: Props) {
  const invalidate = useInvalidateResourceList();
  const spec = REGISTRY["network-interfaces"];
  const subnetSpec = REGISTRY["subnets"];

  const [name, setName] = useState(() => autoName());
  const [description, setDescription] = useState("");
  const [labels, setLabels] = useState<LabelEntry[]>([]);
  const [subnetId, setSubnetId] = useState<string | undefined>(presetSubnetId);
  const subnetLocked = !!presetSubnetId;

  const [v4, setV4] = useState<string[]>([]);
  const [v6, setV6] = useState<string[]>([]);
  const [sgs, setSgs] = useState<string[]>([]);

  // Введённое в селекторе подсети. Область поиска решает, что с ним делать:
  // спросить владельца (vpc сужает по имени) либо честно сказать, что сужаются
  // только загруженные варианты (#528). Прежде ввод не покидал вкладку: список
  // читался одной страницей в 500 строк, а поле отвечало «нет совпадений» —
  // утверждение об отсутствии подсети, которого никто не проверял. Продолжения
  // («показать ещё») у выпадающего списка нет, поэтому проверить это нечем и
  // самому пользователю.
  const subnetScope = pickerScopeOfSpec(subnetSpec);
  const [subnetTerm, setSubnetTerm] = useState("");
  const debouncedSubnetTerm = useDebouncedValue(subnetTerm, subnetScope.asksServer ? 250 : 0);
  const subnetServerQuery = subnetScope.asksServer ? subnetScope.query(debouncedSubnetTerm) : {};
  // Ключ запроса несёт ввод ТОЛЬКО когда сужает сервер: иначе каждое нажатие
  // клавиши сбрасывало бы кэш и перечитывало один и тот же список.
  const subnetTermKey = subnetScope.asksServer ? (subnetServerQuery.filter ?? "") : "";

  // Subnets для RefSelect.
  const { data: subnetList, isLoading: subnetsLoading } = useQuery({
    queryKey: ["subnets", "list", projectId, subnetTermKey],
    queryFn: () =>
      api.list<{ subnets: Array<{ id: string; name?: string }> }>(subnetSpec.apiPath, {
        ...subnetServerQuery,
        project_id: projectId,
        pageSize: "500",
      }),
    enabled: !subnetLocked,
    staleTime: 30_000,
  });
  const subnetOptions = useMemo(
    () =>
      (subnetList?.subnets ?? []).map((s) => ({
        value: s.id,
        label: s.name || s.id,
      })),
    [subnetList],
  );

  // Выбранная подсеть обязана пережить сужение: сервер отвечает по ВВОДУ, и уже
  // сделанный выбор в этот ответ не обязан попадать. Без запоминания метки поле
  // показало бы `sub-…` вместо имени — идентификатор вместо имени, ровно то,
  // что канон консоли (правило 2) и запрещает.
  const chosenSubnet = subnetOptions.find((o) => o.value === subnetId);
  const keptSubnetLabel = useKeptLabel(subnetId, chosenSubnet ? chosenSubnet.label : null);
  const keptSubnet = subnetId && keptSubnetLabel ? [{ value: subnetId, label: keptSubnetLabel }] : [];

  // Выбранная подсеть → network_id: SG фильтруем по сети подсети.
  const { data: selectedSubnet } = useQuery({
    queryKey: ["subnets", "for-nic-filter", subnetId],
    queryFn: () => api.get<{ network_id?: string }>(`${subnetSpec.apiPath}/${subnetId}`),
    enabled: !!subnetId,
    staleTime: 60_000,
  });
  const sgNetworkId = selectedSubnet?.network_id;

  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.create(spec.apiPath, item),
    onSuccess: (resp) => {
      const resolved = resolveMutationResponse(resp, spec.mutationsReturnOperation !== false);
      if (resolved.kind === "operation") setPendingOpId(resolved.opId);
      else if (resolved.kind === "violation") toast.error(`Создать NIC: ${resolved.message}`);
      else {
        invalidate(spec.id, projectId);
        toast.success(`NIC ${name} создан`);
        onSuccess?.();
        onCancel();
      }
    },
    onError: (err) => {
      const m = errorText(err);
      toast.error(`Создать NIC: ${m}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      toast.error(`Создать NIC: ${op.error.message ?? "ошибка"}`);
      setPendingOpId(null);
      return;
    }
    invalidate(spec.id, projectId);
    toast.success(`NIC ${name} создан`);
    setPendingOpId(null);
    onSuccess?.();
    onCancel();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  // Отказ подсети стоит У ПОЛЯ, а не всплывашкой в углу: подсеть — первое поле
  // формы, и сообщение о ней читается там же, где её выбирают.
  const [submitAttempted, setSubmitAttempted] = useState(false);
  const subnetError =
    submitAttempted && !subnetId
      ? "«Подсеть»: поле обязательное — интерфейс создаётся внутри подсети."
      : undefined;

  const submit = () => {
    setSubmitAttempted(true);
    if (!subnetId) return;
    mutation.mutate({
      project_id: projectId,
      subnet_id: subnetId,
      name,
      description: description || undefined,
      labels: labelsToMap(labels),
      v4_address_ids: v4,
      v6_address_ids: v6,
      security_group_ids: sgs,
    });
  };

  return (
    <FormShell specId="network-interfaces" mode="create" singular={spec.singular}>
      <FormGrid>
        <Form.Item label={labelWithInfo("Имя", "Имя интерфейса в пределах проекта.")}>
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </Form.Item>

        <Form.Item label={labelWithInfo("Описание", "Опциональное описание для людей.")}>
          <Input.TextArea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
        </Form.Item>

        <Form.Item label={labelWithInfo("Метки", "Пары ключ=значение для группировки/фильтрации.")}>
          <LabelsEditor value={labels} onChange={setLabels} />
        </Form.Item>

        {/* ПОРЯДОК ОДИН НА ВСЕ ФОРМЫ (решение владельца): имя → описание →
            метки → черта → поля самого ресурса. Форма начиналась с выбора
            подсети; он не потерялся — стоит первым за чертой, среди полей
            ресурса, где ему и место. */}
        <div style={FORM_DIVIDER_STYLE} aria-hidden />

        <Form.Item
          label={labelWithInfo("Подсеть", "Подсеть, в которой создаётся NIC. После Create иммутабельно.")}
          required
        >
          <Select
            showSearch
            value={subnetId}
            onChange={setSubnetId}
            onSearch={setSubnetTerm}
            options={[...keptSubnet, ...subnetOptions]}
            placeholder="Выберите подсеть"
            title={subnetScope.notice}
            // Сузил сервер — клиент НЕ пересеивает: повторное сужение по метке
            // вычло бы из ответа края строки, которые он прислал именно по
            // этому вводу.
            {...(subnetScope.asksServer ? { filterOption: false as const } : { optionFilterProp: "label" as const })}
            // Пустой ответ обязан называть свою ОБЛАСТЬ. Здесь и жила ложь:
            // «нет совпадений» на месте «нет среди загруженных».
            notFoundContent={subnetsLoading ? undefined : subnetScope.emptyText}
            disabled={subnetLocked}
            status={subnetError ? "error" : undefined}
            aria-required
            aria-invalid={subnetError ? true : undefined}
          />
          <FieldError message={subnetError} />
        </Form.Item>

        <Form.Item label={labelWithInfo("IPv4 адрес", "Один Address-ресурс с internal_ipv4. KAC-55: максимум один.")}>
          <RefMultiSelect
            refResource="addresses"
            projectId={projectId}
            value={v4}
            onChange={setV4}
            maxItems={1}
            disabled={!subnetId}
            disabledHint="Сначала выберите подсеть"
            refFilter={(row) =>
              (row.internal_ipv4_address as { subnet_id?: string } | undefined)?.subnet_id === subnetId &&
              addressIsFree(row)
            }
            createResource="addresses"
            createPresetFields={{
              _address_kind: "internal",
              ...(subnetId ? { "internal_ipv4_address_spec.subnet_id": subnetId } : {}),
            }}
            createTitle="Создание внутреннего IPv4-адреса"
          />
        </Form.Item>

        <Form.Item
          label={labelWithInfo("IPv6 адрес", "Internal или external IPv6 Address-ресурс. KAC-55: максимум один.")}
        >
          <RefMultiSelect
            refResource="addresses"
            projectId={projectId}
            value={v6}
            onChange={setV6}
            maxItems={1}
            disabled={!subnetId}
            disabledHint="Сначала выберите подсеть"
            refFilter={(row) =>
              (row.internal_ipv6_address as { subnet_id?: string } | undefined)?.subnet_id === subnetId &&
              addressIsFree(row)
            }
            createResource="addresses"
            createEditablePresetFields={{ _address_kind: "internal_v6" }}
            createPresetFields={subnetId ? { "internal_ipv6_address_spec.subnet_id": subnetId } : undefined}
            createTitle="Создание IPv6-адреса"
          />
        </Form.Item>

        <Form.Item label={labelWithInfo("Группы безопасности", "Группы безопасности, привязанные к этому интерфейсу.")}>
          <RefMultiSelect
            refResource="security-groups"
            projectId={projectId}
            value={sgs}
            onChange={setSgs}
            disabled={!subnetId}
            disabledHint="Сначала выберите подсеть"
            refFilter={(row) => !!sgNetworkId && row.network_id === sgNetworkId}
            createResource="security-groups"
            createTitle="Создание группы безопасности"
          />
        </Form.Item>
        <FormFooter
          submitLabel="Создать"
          submitting={mutation.isPending || !!pendingOpId}
          onSubmit={submit}
          onCancel={onCancel}
        />
      </FormGrid>
    </FormShell>
  );
}
