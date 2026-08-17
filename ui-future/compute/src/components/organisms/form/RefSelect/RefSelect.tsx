// RefSelect — выбор ресурса по ID из выпадающего списка.
// Загружает список через GET <apiPath>?project_id=<id> (+ опц. динамический
// query-параметр от другого поля формы, напр. ?subnet_id=<form.subnet_id>).
// apiPath уже содержит полный путь (e.g. "/iam/v1/projects"),
// никакого "/v1/" префикса не добавляем.
// Flat API: ресурсы имеют поля id и name.
//
// Если задан createResource — в списке появляется «+ Создать …» entry,
// открывающая InlineResourceCreateForm в модалке (паттерн inline-create
// related-resource, как на NetworkDetailPage / SubnetDetailPage). На success
// id созданного ресурса подставляется в это поле.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Modal, Select } from "antd";
import { api } from "@/api/client";
import { getResource } from "@/lib/resource-registry";
import { useProjectStore } from "@/lib/context-store";
import { ErrorResult } from "@/components/molecules/ErrorResult";
import { InlineResourceCreateForm } from "@/components/organisms/InlineResourceCreateForm";
import { FormBareProvider } from "@/components/organisms/form/FormShell";
import { extraInfoFor, headLabelFor } from "./refOptionLabel";
import { createActionLabel } from "@shared/lib/resource-label";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScopeOfSpec } from "@shared/lib/picker-search";
import { useKeptLabel } from "@shared/lib/kept-choice";

interface Props {
  refResource: string;
  refProjectScoped?: boolean;
  value?: string;
  onChange: (uid: string) => void;
  placeholder?: string;
  id?: string;
  disabled?: boolean;
  // Динамический query-параметр от другого поля формы.
  refQueryFromField?: { param: string; field: string };
  // Клиентский фильтр-предикат поверх загруженного candidate-list.
  refFilter?: (row: Record<string, unknown>) => boolean;
  // Текущее значение всей формы (для refQueryFromField / createPresetFields).
  formValue?: Record<string, unknown>;
  // Inline-create related-resource.
  createResource?: string;
  createPresetFields?: (form: Record<string, unknown>) => Record<string, unknown>;
  createTitle?: string;
}

export function RefSelect({
  refResource,
  refProjectScoped,
  value,
  onChange,
  placeholder,
  id,
  disabled,
  refQueryFromField,
  refFilter,
  formValue,
  createResource,
  createPresetFields,
  createTitle,
}: Props) {
  const project = useProjectStore((s) => s.project);
  const spec = getResource(refResource);
  const createSpec = createResource ? getResource(createResource) : undefined;

  const [creating, setCreating] = useState(false);

  // Динамический query-параметр (e.g. subnet_id) — берём из текущего значения формы.
  const dynParamValue =
    refQueryFromField && formValue ? (formValue[refQueryFromField.field] as string | undefined) : undefined;
  const needsDynParam = !!refQueryFromField;

  const enabled = !!spec && (!refProjectScoped || !!project) && (!needsDynParam || !!dynParamValue);

  // Введённое пользователем. Область поиска решает, что с ним делать: спросить
  // сервер либо честно сказать, что сужаются только загруженные варианты (#528).
  // Раньше ввод не покидал браузер НИКОГДА: список читался одной страницей, а
  // поле отвечало «нет совпадений» — то есть утверждало об отсутствии ресурса
  // то, чего не спрашивало.
  const scope = pickerScopeOfSpec(spec);
  const [term, setTerm] = useState("");
  const debouncedTerm = useDebouncedValue(term, scope.asksServer ? 250 : 0);
  const serverQuery = scope.asksServer ? scope.query(debouncedTerm) : {};
  // Ключ запроса несёт ввод ТОЛЬКО когда сужает сервер: иначе каждое нажатие
  // клавиши сбрасывало бы кэш и перечитывало один и тот же список.
  const queryTermKey = scope.asksServer ? (serverQuery.filter ?? "") : "";

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: [
      "ref",
      refResource,
      refProjectScoped ? project?.id : null,
      needsDynParam ? (dynParamValue ?? null) : null,
      queryTermKey,
    ],
    queryFn: () => {
      const q: Record<string, string> = { ...serverQuery };
      if (refProjectScoped && project) q["project_id"] = project.id;
      if (refQueryFromField && dynParamValue) q[refQueryFromField.param] = dynParamValue;
      return api.list<Record<string, Array<{ id: string; name: string } & Record<string, unknown>>>>(spec!.apiPath, q);
    },
    enabled,
    staleTime: 30_000,
  });

  // Состав вариантов считается ДО раннего возврата: ниже стоит хук памяти
  // выбора, а хук, стоящий за условным `return`, вызывается не на каждом
  // рендере — это нарушение правил хуков, а не стилистика.
  const candidates = ((spec ? data?.[spec.payloadKey] : undefined) ?? []).filter((it) =>
    refFilter ? refFilter(it as Record<string, unknown>) : true,
  );
  const options = candidates.map((it) => ({
    uid: it.id,
    name: headLabelFor(refResource, it as Record<string, unknown>),
    extra: extraInfoFor(refResource, it as Record<string, unknown>),
  }));


  const labelOf = (o: { uid: string; name: string; extra?: string }) =>
    `${o.name || o.uid}${o.extra ? ` · ${o.extra}` : ""}`;

  // Выбранное значение обязано пережить сужение: сервер отвечает по ВВОДУ, и
  // уже сделанный выбор в этот ответ не обязан попадать. Без запоминания метки
  // поле показывало бы вместо имени идентификатор — ровно то, что канон консоли
  // (правило 2) и запрещает.
  const chosen = options.find((o) => o.uid === value);
  const keptLabel = useKeptLabel(value, chosen ? labelOf(chosen) : null);
  const keptChoice = value && keptLabel ? [{ value, label: keptLabel }] : [];

  if (!spec) return <div className="text-xs text-rose-600">Неизвестная ссылка: {refResource}</div>;

  const CREATE_SENTINEL = "__create__";

  return (
    <div className="space-y-1">
      <Select
        id={id}
        showSearch
        allowClear
        value={value || undefined}
        placeholder={placeholder ?? `Выбрать ${spec.singular}…`}
        disabled={disabled || !enabled}
        style={{ width: "100%" }}
        title={scope.notice}
        onSearch={setTerm}
        // Сузил сервер — клиент НЕ пересеивает: повторное сужение по загруженной
        // метке вычло бы из ответа края строки, которые он прислал именно по
        // этому вводу (у ресурса имя может не совпасть с меткой варианта).
        {...(scope.asksServer ? { filterOption: false as const } : { optionFilterProp: "label" as const })}
        // Пустой ответ обязан называть свою ОБЛАСТЬ. Именно здесь жила ложь:
        // «нет совпадений» на месте «нет среди загруженных».
        notFoundContent={enabled && !isLoading ? scope.emptyText : undefined}
        // CREATE_SENTINEL — действие (открыть inline-create modal), не значение:
        // не зовём onChange (value не меняется → controlled Select откатывается).
        onChange={(v) => {
          if (v === CREATE_SENTINEL) {
            setCreating(true);
            return;
          }
          onChange(v || "");
        }}
        options={[
          ...keptChoice,
          ...options.map((o) => ({ value: o.uid as string, label: labelOf(o) })),
          ...(createSpec ? [{ value: CREATE_SENTINEL, label: `+ ${createActionLabel(createSpec)}…` }] : []),
        ]}
      />
      {refProjectScoped && !project && <p className="text-xs text-amber-600">Выберите проект в шапке для загрузки.</p>}
      {needsDynParam && !dynParamValue && (
        <p className="text-xs text-amber-600">Сначала выберите «{refQueryFromField!.field}» выше.</p>
      )}
      {isLoading && <p className="text-xs text-muted-foreground">Загрузка списка {spec.plural}…</p>}
      {error && <ErrorResult error={error} />}
      {/* Предупреждение «нет в списке» судит о ЗАГРУЖЕННОМ, поэтому оно молчит,
          пока список сужен вводом: там отсутствие выбранного — норма, а не
          признак удалённого ресурса. Иначе поле пугало бы пользователя ровно в
          тот момент, когда он ищет другое значение. */}
      {value && !chosen && !debouncedTerm && options.length > 0 && (
        <p className="text-xs text-amber-600">ID не найден среди загруженных (возможно ресурс удалён или вне фильтра).</p>
      )}

      {creating && createSpec && (
        <Modal
          open
          footer={null}
          onCancel={() => setCreating(false)}
          width={720}
          destroyOnClose
          title={null}
          styles={{ body: { padding: "12px 24px 20px" } }}
        >
          <FormBareProvider>
            <InlineResourceCreateForm
              spec={createSpec}
              ctx={{
                projectId: project?.id,
                accountId: project?.accountId,
              }}
              presetFields={createPresetFields && formValue ? createPresetFields(formValue) : undefined}
              projectId={project?.id ?? null}
              title={createTitle}
              onCancel={() => setCreating(false)}
              onSuccess={() => {
                // refetch candidate-list — новый ресурс должен появиться;
                // затем подхватываем последний созданный по имени-эвристике
                // (InlineResourceCreateForm не отдаёт id наверх — после refetch
                // diff'им список). Простой best-effort: перезапрашиваем и
                // оставляем выбор пользователю, если не смогли определить.
                void refetch().then((r) => {
                  const after = (r.data?.[spec.payloadKey] ?? []) as Array<{ id: string }>;
                  const before = new Set(options.map((o) => o.uid));
                  const fresh = after.find((it) => !before.has(it.id));
                  if (fresh) onChange(fresh.id);
                });
              }}
            />
          </FormBareProvider>
        </Modal>
      )}
    </div>
  );
}
