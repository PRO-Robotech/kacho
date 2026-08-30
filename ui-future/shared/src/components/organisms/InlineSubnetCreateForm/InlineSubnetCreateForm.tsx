// InlineSubnetCreateForm — inline-форма создания подсети, встраиваемая в правую
// панель Network detail вместо "Общее"-Descriptions. Раскладка повторяет
// 2-column horizontal layout (label-left / input-right) с полями размещения
// (ZONAL зона / REGIONAL регион), основного CIDR (IPv4/IPv6) и меток.
//
// VPC-1 wire-format (SubnetService.Create):
//   { project_id, network_id, name, description?, labels?,
//     zone_id XOR region_id,               // placement_type° server-derived
//     ipv4_cidr_primary?, ipv6_cidr_primary?,  // immutable anchor, ≥1 required
//     route_table_id? }                    // auto = network.defaultRouteTableId°
//
// placement_type is NOT sent (server rejects it; derived from zone/region).
// Additional CIDR ranges are added post-create via :add-cidr-blocks on the
// subnet detail page. DhcpOptions retired by design.

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Form, Input, Select, Space, Tooltip } from "antd";
import { QuestionCircleOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScopeOfSpec } from "@shared/lib/picker-search";
import { useKeptLabel } from "@shared/lib/kept-choice";
import { resolveMutationResponse } from "@shared/lib/operation-outcome";
import { FORM_DIVIDER_STYLE, MONO_FONT  } from "@shared/components/organisms/form/editor-surface";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { FieldError } from "@shared/components/organisms/form/FieldError";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useInvalidateResourceList, useOperation } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";
import { LabelsEditor, labelsFromEntries, type LabelEntry } from "@shared/components/organisms/LabelsEditor";
import { errorText } from "@shared/lib/error-presentation";

interface Props {
  projectId: string;
  // networkId — preset (locked если задан). Если undefined — форма
  // отображает RefSelect "Сеть" как первое поле, user выбирает в форме
  // (отказались от двухшагового flow).
  networkId?: string;
  onCancel: () => void;
  onSuccess?: () => void;
}

function autoName(): string {
  return `subnet-${Math.floor(100000 + Math.random() * 900000)}`;
}

export function InlineSubnetCreateForm({ projectId, networkId: presetNetworkId, onCancel, onSuccess }: Props) {
  const invalidate = useInvalidateResourceList();
  const subnetSpec = REGISTRY["subnets"];
  const zoneSpec = REGISTRY["zones"];
  const regionSpec = REGISTRY["regions"];
  const rtSpec = REGISTRY["route-tables"];
  const networkSpec = REGISTRY["networks"];

  // Если networkId preset (передан из контекста — например, "Создать подсеть"
  // из NetworkDetailPage), сеть locked. Иначе — selectable в форме.
  const [networkId, setNetworkId] = useState<string | undefined>(presetNetworkId);
  const networkLocked = !!presetNetworkId;

  // Введённое в селекторе сети. Область поиска решает, что с ним делать:
  // спросить владельца (vpc сужает по имени) либо честно сказать, что сужаются
  // только загруженные варианты (#528). Прежде ввод не покидал вкладку: сети
  // читались одной страницей в 500 строк, а поле отвечало «нет совпадений» —
  // то есть утверждало об отсутствии сети то, чего не спрашивало, и у
  // пользователя не было способа это опровергнуть: «показать ещё» у
  // выпадающего списка нет.
  const networkScope = pickerScopeOfSpec(networkSpec);
  const [networkTerm, setNetworkTerm] = useState("");
  const debouncedNetworkTerm = useDebouncedValue(networkTerm, networkScope.asksServer ? 250 : 0);
  const networkServerQuery = networkScope.asksServer ? networkScope.query(debouncedNetworkTerm) : {};
  // Ключ запроса несёт ввод ТОЛЬКО когда сужает сервер: иначе каждое нажатие
  // клавиши сбрасывало бы кэш и перечитывало один и тот же список.
  const networkTermKey = networkScope.asksServer ? (networkServerQuery.filter ?? "") : "";

  // Список Networks для RefSelect (когда preset не задан).
  const { data: netData, isLoading: networksLoading } = useQuery({
    queryKey: ["networks", "list", projectId, networkTermKey],
    queryFn: () =>
      api.list<{ networks: Array<{ id: string; name?: string }> }>(networkSpec.apiPath, {
        ...networkServerQuery,
        project_id: projectId,
        pageSize: "500",
      }),
    enabled: !networkLocked,
    staleTime: 30_000,
  });
  const networkOptions = useMemo(
    () =>
      (netData?.networks ?? []).map((n) => ({
        value: n.id,
        label: n.name || n.id,
      })),
    [netData],
  );

  // Выбранная сеть обязана пережить сужение: сервер отвечает по ВВОДУ, и уже
  // сделанный выбор в этот ответ попадать не обязан. Без запоминания метки поле
  // показало бы `net-…` вместо имени — идентификатор вместо имени, что канон
  // консоли (правило 2) запрещает. Сеть при этом остаётся ВЫБРАННОЙ: значение
  // формы от сужения списка не зависит.
  const chosenNetwork = networkOptions.find((o) => o.value === networkId);
  const keptNetworkLabel = useKeptLabel(networkId, chosenNetwork ? chosenNetwork.label : null);
  const keptNetwork = networkId && keptNetworkLabel ? [{ value: networkId, label: keptNetworkLabel }] : [];

  const [name, setName] = useState(() => autoName());
  const [description, setDescription] = useState("");
  const [labels, setLabels] = useState<LabelEntry[]>([]);
  const [zoneId, setZoneId] = useState<string | undefined>(undefined);
  // Размещение подсети: ZONAL (одна зона) либо REGIONAL (весь регион).
  const [placementType, setPlacementType] = useState<"ZONAL" | "REGIONAL">("ZONAL");
  const [regionId, setRegionId] = useState<string | undefined>(undefined);
  const [routeTableId, setRouteTableId] = useState<string | undefined>(undefined);
  // VPC-1: single immutable primary CIDR anchor per family (≥1 required).
  // Additional ranges are added post-create via :add-cidr-blocks.
  const [v4Primary, setV4Primary] = useState("");
  const [v6Primary, setV6Primary] = useState("");

  // Зоны: глобальный admin-ресурс, без project_id.
  const { data: zoneData } = useQuery({
    queryKey: ["zones", "list"],
    queryFn: () =>
      api.list<{ zones: Array<{ id: string }> }>(zoneSpec.apiPath, {
        pageSize: "500",
      }),
    staleTime: 60_000,
  });
  // Подписью служит сам идентификатор: у каталога размещения отдельного имени
  // нет (#716) — его назначает администратор, и он читаем by construction.
  // Прежний запасной путь (`z.name || z.id`) после снятия поля не выбирался бы
  // НИКОГДА, а тип обещал поле, которого в ответе нет: мёртвая ветка плюс
  // ложное объявление формы ответа.
  const zoneOptions = useMemo(
    () => (zoneData?.zones ?? []).map((z) => ({ value: z.id, label: z.id })),
    [zoneData],
  );
  // Default-zone — первая по списку.
  useEffect(() => {
    if (!zoneId && zoneOptions.length > 0) {
      setZoneId(zoneOptions[0].value);
    }
  }, [zoneId, zoneOptions]);

  // Регионы (для REGIONAL-размещения) — geo admin-ресурс, без project_id.
  const { data: regionData } = useQuery({
    queryKey: ["regions", "list"],
    queryFn: () =>
      api.list<{ regions: Array<{ id: string }> }>(regionSpec.apiPath, {
        pageSize: "500",
      }),
    staleTime: 60_000,
  });
  const regionOptions = useMemo(
    // См. зоны выше: подпись региона снята вместе с полем (#716).
    () => (regionData?.regions ?? []).map((r) => ({ value: r.id, label: r.id })),
    [regionData],
  );
  useEffect(() => {
    if (placementType === "REGIONAL" && !regionId && regionOptions.length > 0) {
      setRegionId(regionOptions[0].value);
    }
  }, [placementType, regionId, regionOptions]);

  // RouteTables: project-scoped, ещё фильтруем по network.
  const { data: rtData } = useQuery({
    queryKey: ["route-tables", "list", projectId, networkId],
    queryFn: () =>
      api.list<{ route_tables: Array<Record<string, unknown>> }>(rtSpec.apiPath, {
        project_id: projectId,
        pageSize: "500",
      }),
    staleTime: 30_000,
  });
  const rtOptions = useMemo(
    () =>
      (rtData?.route_tables ?? [])
        .filter((r) => r.network_id === networkId)
        .map((r) => ({
          value: r.id as string,
          label: ((r.name as string) || (r.id as string)) ?? "",
        })),
    [rtData, networkId],
  );

  // Doppler-flow: ждём op.done через polling вместо banner.
  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.create(subnetSpec.apiPath, item),
    onSuccess: (resp) => {
      const resolved = resolveMutationResponse(resp, subnetSpec.mutationsReturnOperation !== false);
      if (resolved.kind === "operation") {
        setPendingOpId(resolved.opId);
      } else if (resolved.kind === "violation") {
        // Ответ без операции у ресурса, который её объявил: подтверждать
        // выполнение нечем, и закрыть форму как успех значит сказать неправду.
        toast.error(`Создать подсеть: ${resolved.message}`);
      } else {
        invalidate(subnetSpec.id, projectId);
        onSuccess?.();
        onCancel();
      }
    },
    onError: (err) => {
      const m = errorText(err);
      toast.error(`Создать подсеть: ${m}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      // На ошибку — НЕ вызываем onCancel/onSuccess: остаёмся на форме,
      // user видит toast с причиной (например CIDR overlap) и может
      // поправить ввод. Раньше любой результат закрывал форму — баг.
      toast.error(`Создать подсеть: ${op.error.message ?? "ошибка"}`);
      setPendingOpId(null);
      return;
    }
    invalidate(subnetSpec.id, projectId);
    toast.success(`Подсеть ${name} создана`);
    setPendingOpId(null);
    onSuccess?.();
    onCancel();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  // Правила полей — ОДНО объявление, читаемое и отправкой, и разметкой.
  //
  // Прежде каждое из шести правил жило внутри `submit` и заканчивалось
  // всплывающим сообщением в углу экрана. Всплывашка не говорила, к какому полю
  // относится, гасла через несколько секунд и на первом же правиле обрывала
  // проверку, поэтому арендатор узнавал о недочётах ПО ОДНОМУ — за столько
  // попыток отправки, сколько их было.
  // Здесь считаются ВСЕ сразу и каждое приписано своему полю.
  const problems = (): Record<string, string> => {
    const out: Record<string, string> = {};
    if (!networkId) out.network = "«Сеть»: поле обязательное — выберите сеть, в которой создаётся подсеть.";
    if (placementType === "ZONAL" && !zoneId) {
      out.zone = "«Зона доступности»: поле обязательное при размещении ZONAL.";
    }
    if (placementType === "REGIONAL" && !regionId) {
      out.region = "«Регион»: поле обязательное при размещении REGIONAL.";
    }
    // VPC-1: ≥1 primary CIDR anchor required (v4 / v6 / both). Additional
    // ranges are added post-create via :add-cidr-blocks.
    const v4 = v4Primary.trim();
    const v6 = v6Primary.trim();
    // Требование «хотя бы одно семейство» названо у IPv4 — того поля, что
    // помечено обязательным. Назвать его у обоих значило бы утверждать, что
    // обязательны оба.
    if (!v4 && !v6) out.v4 = "«Основной IPv4 CIDR»: нужен основной CIDR хотя бы одного семейства — IPv4 либо IPv6.";
    if (v4 && !v4.includes("/")) {
      out.v4 = "«Основной IPv4 CIDR»: CIDR должен содержать префикс (например 10.20.0.0/24).";
    }
    if (v6 && !(v6.includes("/") && v6.includes(":"))) {
      out.v6 = "«Основной IPv6 CIDR»: CIDR должен содержать префикс (например fd00:20::/64).";
    }
    return out;
  };

  const [submitAttempted, setSubmitAttempted] = useState(false);
  // Отказы считаются на каждом рендере по текущему вводу: поправленное поле
  // выходит из отказа сразу, соседнее остаётся названным.
  const errors = submitAttempted ? problems() : {};

  const submit = () => {
    setSubmitAttempted(true);
    if (Object.keys(problems()).length > 0) return;
    const v4 = v4Primary.trim();
    const v6 = v6Primary.trim();
    const labelMap = labelsFromEntries(labels);

    // placement_type НЕ отправляется — сервер выводит его из zone_id XOR
    // region_id. DhcpOptions сняты by design.
    const payload: Record<string, unknown> = {
      project_id: projectId,
      network_id: networkId,
      zone_id: placementType === "ZONAL" ? zoneId : undefined,
      region_id: placementType === "REGIONAL" ? regionId : undefined,
      name,
      description: description || undefined,
      labels: Object.keys(labelMap).length > 0 ? labelMap : undefined,
      ipv4_cidr_primary: v4 || undefined,
      ipv6_cidr_primary: v6 || undefined,
      route_table_id: routeTableId || undefined,
    };

    mutation.mutate(payload);
  };

  return (
    <FormShell specId="subnets" mode="create" singular={subnetSpec.singular}>
      <FormGrid>
        <Form.Item label="Имя">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="subnet-..." />
        </Form.Item>

        <Form.Item label="Описание">
          <Input.TextArea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
        </Form.Item>

        <Form.Item label="Метки">
          <LabelsEditor value={labels} onChange={setLabels} />
        </Form.Item>

        {/* ПОРЯДОК ОДИН НА ВСЕ ФОРМЫ (решение владельца): имя → описание →
            метки → черта → поля самого ресурса. Здесь форма начиналась с выбора
            сети, и на соседних формах рука шла к разным местам. Выбор сети от
            этого не потерялся: он стоит первым среди полей ресурса, сразу за
            чертой. */}
        <div style={FORM_DIVIDER_STYLE} aria-hidden />

        <Form.Item label="Сеть" required>
          <Select
            showSearch
            value={networkId}
            onChange={(v) => setNetworkId(v)}
            onSearch={setNetworkTerm}
            options={[...keptNetwork, ...networkOptions]}
            placeholder="Выберите сеть"
            title={networkScope.notice}
            // Сузил сервер — клиент НЕ пересеивает: повторное сужение по метке
            // вычло бы из ответа края строки, которые он прислал именно по
            // этому вводу.
            {...(networkScope.asksServer ? { filterOption: false as const } : { optionFilterProp: "label" as const })}
            // Пустой ответ обязан называть свою ОБЛАСТЬ. Здесь и жила ложь:
            // «нет совпадений» на месте «нет среди загруженных».
            notFoundContent={networksLoading ? undefined : networkScope.emptyText}
            disabled={networkLocked}
            status={errors.network ? "error" : undefined}
            aria-required
            aria-invalid={errors.network ? true : undefined}
          />
          <FieldError message={errors.network} />
        </Form.Item>

        <Form.Item label="Размещение" required>
          <Select
            value={placementType}
            onChange={(v) => setPlacementType(v)}
            options={[
              { value: "ZONAL", label: "ZONAL — в одной зоне доступности" },
              { value: "REGIONAL", label: "REGIONAL — во всём регионе" },
            ]}
          />
        </Form.Item>

        {placementType === "ZONAL" ? (
          <Form.Item label="Зона доступности" required>
            <Select
              value={zoneId}
              onChange={setZoneId}
              options={zoneOptions}
              placeholder="Выберите зону"
              status={errors.zone ? "error" : undefined}
              aria-required
              aria-invalid={errors.zone ? true : undefined}
            />
            <FieldError message={errors.zone} />
          </Form.Item>
        ) : (
          <Form.Item label="Регион" required>
            <Select
              value={regionId}
              onChange={setRegionId}
              options={regionOptions}
              placeholder="Выберите регион"
              status={errors.region ? "error" : undefined}
              aria-required
              aria-invalid={errors.region ? true : undefined}
            />
            <FieldError message={errors.region} />
          </Form.Item>
        )}

        <Form.Item label="Таблица маршрутов">
          <Select
            value={routeTableId}
            onChange={(v) => setRouteTableId(v)}
            options={rtOptions}
            allowClear
            placeholder="Выберите таблицу маршрутов (опц.)"
          />
        </Form.Item>

        {/* VPC-1: единственный immutable основной CIDR на семейство (≥1 обязателен).
            Доп. диапазоны добавляются после создания на странице подсети. */}
        <Form.Item
          label={
            <Space size={4}>
              Основной IPv4 CIDR
              <Tooltip title="Неизменяемый основной IPv4 CIDR подсети (⊆ одного CIDR-блока сети), например 10.20.0.0/24. Можно оставить пустым для IPv6-only подсети. Доп. диапазоны добавляются позже.">
                <QuestionCircleOutlined style={{ color: "var(--kc-text-tertiary)" }} />
              </Tooltip>
            </Space>
          }
          required
        >
          <Input
            value={v4Primary}
            onChange={(e) => setV4Primary(e.target.value)}
            placeholder="10.20.0.0/24"
            style={{ fontFamily: MONO_FONT, fontSize: 11, fontWeight: 520 }}
            status={errors.v4 ? "error" : undefined}
            aria-required
            aria-invalid={errors.v4 ? true : undefined}
          />
          <FieldError message={errors.v4} />
        </Form.Item>

        <Form.Item
          label={
            <Space size={4}>
              Основной IPv6 CIDR
              <Tooltip title="Опционально. Неизменяемый основной IPv6 CIDR подсети (⊆ IPv6 CIDR сети), например fd00:20::/64.">
                <QuestionCircleOutlined style={{ color: "var(--kc-text-tertiary)" }} />
              </Tooltip>
            </Space>
          }
        >
          <Input
            value={v6Primary}
            onChange={(e) => setV6Primary(e.target.value)}
            placeholder="fd00:20::/64"
            style={{ fontFamily: MONO_FONT, fontSize: 11, fontWeight: 520 }}
            status={errors.v6 ? "error" : undefined}
            aria-invalid={errors.v6 ? true : undefined}
          />
          <FieldError message={errors.v6} />
        </Form.Item>

        <FormFooter
          submitLabel="Создать"
          submitting={mutation.isPending || pendingOpId !== null}
          onSubmit={submit}
          onCancel={onCancel}
        />
      </FormGrid>
    </FormShell>
  );
}
