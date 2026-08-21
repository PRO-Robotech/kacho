// RefMultiSelect — выбор НЕСКОЛЬКИХ чужих ресурсов по ссылке одним полем
// (NIC.v4_address_ids / v6_address_ids / security_group_ids).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ФИШКИ ЖИВУТ ВНУТРИ ПОЛЯ, А НЕ НАД НИМ
//
// Прежний вид этого места рисовал выбранное СНАРУЖИ — отдельной строкой фишек
// над выпадающим списком. У такой строки нет ни рамки поля, ни его ширины:
// она растёт вверх и вбок по мере выбора, наезжая на соседнее поле формы, и со
// стороны выглядит как выпавшее из вёрстки содержимое, а не как значение поля.
// Правка владельца 2026-08-21: «теги в дропдауне должны фиксироваться внутри
// дропдауна».
//
// Здесь выбранное несёт САМО поле (`mode="multiple"`): рамка одна, ширина одна,
// а хвост, который в неё не влез, сворачивается в «+N» (`maxTagCount`), а не
// вылезает наружу. Это не косметика: пока выбранное рисовал кто-то другой,
// высота поля не зависела от числа фишек — то есть форма не знала, сколько
// места занимает её собственное значение.
//
// ПОЧЕМУ ФИШКА И ВАРИАНТ СПИСКА ПОДПИСАНЫ ПО-РАЗНОМУ
//
// Список помогает ВЫБРАТЬ — там имя ресурса несёт смысл. Фишка называет уже
// сделанный выбор в узком поле, и для адреса значимым остаётся адрес: интерфейс
// привязывают к нему, а имя ресурса-адреса о выборе не говорит ничего. Обе
// подписи собираются чистыми функциями (`refOptionLabel` / `refTagLabel`) — там
// же, где подписи одиночного поля, чтобы одно и то же значение не читалось в
// двух формах по-разному.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Modal, Select, Tag } from "antd";
import { api } from "@shared/api/client";
import { getResource } from "@shared/lib/resource-registry";
import { useContext } from "@shared/lib/context-store";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScopeOfSpec } from "@shared/lib/picker-search";
import { useKeptLabels } from "@shared/lib/kept-choice";
import { InlineResourceCreateForm } from "@shared/components/organisms/InlineResourceCreateForm";
import { FormBareProvider } from "@shared/components/organisms/form/FormShell";
import { refOptionLabel, refTagLabel } from "./refOptionLabel";
import { createActionLabel } from "@shared/lib/resource-label";

interface Props {
  /** ID ресурса в REGISTRY, на который ссылается поле («addresses», «security-groups»). */
  refResource: string;
  /** Область выдачи — проект, в котором стоит форма. */
  projectId: string;
  value: string[];
  onChange: (next: string[]) => void;
  /** Клиентский предикат поверх загруженных кандидатов. */
  refFilter?: (row: Record<string, unknown>) => boolean;
  /** Предел выбора (KAC-55: не больше одного адреса каждого семейства на NIC). */
  maxItems?: number;
  disabled?: boolean;
  /** Что сказать вместо подсказки, когда поле заперто («Сначала выберите подсеть»). */
  disabledHint?: string;
  /** Inline-create связанного ресурса прямо из списка. */
  createResource?: string;
  createPresetFields?: Record<string, unknown>;
  createEditablePresetFields?: Record<string, unknown>;
  createTitle?: string;
  id?: string;
}

/** Значение-действие: «+ Создать …» открывает форму, а не выбирается. */
const CREATE_SENTINEL = "__create__";

export function RefMultiSelect({
  refResource,
  projectId,
  value,
  onChange,
  refFilter,
  maxItems,
  disabled,
  disabledHint,
  createResource,
  createPresetFields,
  createEditablePresetFields,
  createTitle,
  id,
}: Props) {
  const spec = getResource(refResource);
  const createSpec = createResource ? getResource(createResource) : undefined;
  const account = useContext((s) => s.account);
  const [creating, setCreating] = useState(false);

  // Введённое пользователем. Область поиска решает, что с ним делать: спросить
  // владельца либо честно сказать, что сужаются только загруженные варианты
  // (#528). У выпадающего списка продолжения нет by construction, поэтому
  // «ничего не найдено» на месте «нет среди загруженных» читается как факт о
  // мире, и человек идёт заводить дубликат уже существующего ресурса.
  const scope = pickerScopeOfSpec(spec);
  const [term, setTerm] = useState("");
  const debouncedTerm = useDebouncedValue(term, scope.asksServer ? 250 : 0);
  const serverQuery = scope.asksServer ? scope.query(debouncedTerm) : {};
  // Ключ запроса несёт ввод ТОЛЬКО когда сужает сервер: иначе каждое нажатие
  // клавиши сбрасывало бы кэш и перечитывало один и тот же список.
  const queryTermKey = scope.asksServer ? (serverQuery.filter ?? "") : "";

  const { data, isLoading, refetch } = useQuery({
    queryKey: [refResource, "list", projectId, queryTermKey],
    queryFn: () =>
      api.list<Record<string, unknown>>(spec!.apiPath, {
        ...serverQuery,
        project_id: projectId,
        pageSize: "500",
      }),
    enabled: !!spec,
    staleTime: 30_000,
  });

  const rows = useMemo(() => {
    if (!data || !spec) return [];
    const arr = (data[spec.payloadKey] as Record<string, unknown>[] | undefined) ?? [];
    return refFilter ? arr.filter(refFilter) : arr;
  }, [data, spec, refFilter]);

  // Подпись выбранного обязана пережить сужение: сервер отвечает по ВВОДУ, и
  // уже выбранный адрес в этот ответ попадать не обязан. Без памяти фишки
  // выродились бы в сырые `adr-…` ровно тогда, когда человек ищет ДРУГОЕ
  // значение, — то есть поле показывало бы идентификатор вместо адреса.
  const seen = useMemo(
    () =>
      rows
        .map((r) => [((r.id as string) ?? ""), refTagLabel(refResource, r)] as const)
        .filter(([rid]) => rid !== ""),
    [rows, refResource],
  );
  const tagText = useKeptLabels(seen);

  const options = useMemo(() => {
    const base = rows.map((r) => ({
      value: (r.id as string) ?? "",
      label: refOptionLabel(refResource, r),
    }));
    if (createSpec) {
      base.push({ value: CREATE_SENTINEL, label: `+ ${createActionLabel(createSpec)}…` });
    }
    return base;
  }, [rows, refResource, createSpec]);

  if (!spec) return <div className="text-xs text-rose-600">Неизвестная ссылка: {refResource}</div>;

  const handleChange = (next: string[]) => {
    if (next.includes(CREATE_SENTINEL)) {
      // Sentinel — действие, а не значение: в набор он попасть не должен ни на
      // мгновение, иначе форма отправит на край строку «__create__».
      setCreating(true);
      onChange(next.filter((v) => v !== CREATE_SENTINEL));
      return;
    }
    // Предел объявлен и вводу (`maxCount` гасит остальные варианты — это видит
    // оператор), и значению (обрезка — это то, что уедет на край). Полагаться
    // на одно объявление нельзя: значение формы принадлежит форме, а не полю
    // ввода, и превышение здесь означало бы отказ владельца уже на отправке.
    onChange(maxItems !== undefined ? next.slice(0, maxItems) : next);
  };

  return (
    <>
      <Select
        id={id}
        mode="multiple"
        showSearch
        allowClear
        value={value}
        onChange={handleChange}
        onSearch={setTerm}
        options={options}
        maxCount={maxItems}
        // Хвост, не влезший в ширину поля, сворачивается в «+N» — именно этим
        // выбранное и удерживается ВНУТРИ рамки при любом числе значений.
        maxTagCount="responsive"
        // Фишка называет адрес, а не имя (см. шапку файла). Моноширинное
        // начертание — как у всех адресов консоли, семейство берётся токеном
        // продукта, а не именем шрифта.
        tagRender={({ value: uid, closable, onClose }) => (
          <Tag
            closable={closable}
            onClose={onClose}
            style={{ fontFamily: "var(--font-mono)", marginInlineEnd: 4 }}
          >
            {tagText(String(uid))}
          </Tag>
        )}
        // Подсказка видна ТОЛЬКО при пустом наборе — здесь стояла ещё ветка
        // «Максимум N», и показаться она не могла никогда: предел достигается
        // непустым набором, а при непустом подсказки нет. Достижение предела
        // оператор видит иначе — `maxCount` гасит оставшиеся варианты.
        placeholder={disabled ? (disabledHint ?? "Недоступно") : `${createActionLabel(spec, "Выбрать")}…`}
        title={scope.notice}
        disabled={disabled}
        style={{ width: "100%" }}
        // Сузил сервер — клиент НЕ пересеивает: повторное сужение по подписи
        // варианта вычло бы из ответа края строки, которые он прислал именно по
        // этому вводу (подпись адреса несёт ещё и адрес, а сервер искал по имени).
        {...(scope.asksServer ? { filterOption: false as const } : { optionFilterProp: "label" as const })}
        // Пустой ответ обязан называть свою ОБЛАСТЬ. Здесь и живёт ложь:
        // «ничего не найдено» на месте «нет среди загруженных».
        notFoundContent={isLoading ? undefined : scope.emptyText}
      />
      {creating && createSpec && (
        <Modal
          open
          footer={null}
          onCancel={() => setCreating(false)}
          width={720}
          destroyOnClose
          title={null}
          // Тот же интерьер, что у основной модалки формы: «утопленный» фон и
          // одинаковая рамка.
          className="kc-form-modal"
          styles={{ body: { padding: 18 } }}
        >
          <FormBareProvider>
            <InlineResourceCreateForm
              spec={createSpec}
              ctx={{ projectId, accountId: account?.id }}
              presetFields={createPresetFields}
              editablePresetFields={createEditablePresetFields}
              projectId={projectId}
              title={createTitle}
              onCancel={() => setCreating(false)}
              onSuccess={() => {
                // Созданный ресурс подхватываем сравнением списка ДО и ПОСЛЕ:
                // форма создания не отдаёт id наверх.
                const before = new Set(rows.map((r) => (r.id as string) ?? "").filter(Boolean));
                void refetch().then((r) => {
                  const after = (r.data?.[spec.payloadKey] as Record<string, unknown>[] | undefined) ?? [];
                  const fresh = (refFilter ? after.filter(refFilter) : after).find(
                    (it) => !before.has((it.id as string) ?? ""),
                  );
                  const freshId = (fresh?.id as string) ?? "";
                  if (freshId && !value.includes(freshId) && !(maxItems !== undefined && value.length >= maxItems)) {
                    onChange([...value, freshId]);
                  }
                  setCreating(false);
                });
              }}
            />
          </FormBareProvider>
        </Modal>
      )}
    </>
  );
}
