// RulesEditor — controlled per-rule редактор политики роли (RBAC rules-model).
// Роль авторится и рендерится из `rules[]` (источник истины), НЕ из `permissions[]`
// (внутренняя compiled-форма, в API-ответе пустая).
//
// Каждое Rule — однородный грант `{verbs}` над декартовым `module × resources`,
// опц. суженный `resource_names[]` (pin-by-id) XOR `match_labels{}` (AND-equality).
// Арм правила (ANCHOR / NAMES / LABELS) выводится из формы (наличие resource_names /
// match_labels) — отдельного поля нет.
//
// module / resource / verb опции + wildcard-политика — из ЖИВОГО backend-каталога
// (usePermissionCatalog → GET /iam/v1/permissionCatalog). resource_names arm —
// picker реальных инстансов через публичный per-object filtered List<Resource>
// (has_list_endpoint=true) либо free-text fallback.
//
// Wildcard-политика (каталожные флаги wildcard_policy):
//   • verb-`*` РАЗРЕШЁН в custom-роли («все verbs типа», bounded);
//   • module-`*` / resource-`*` — SYSTEM-ONLY → в custom-editor DISABLED;
//   • `*` — только как единственный элемент; `*` + selector → INVALID_ARGUMENT
//     (backend), UI не предлагает `*` в комбинации с resource_names/match_labels.
//
// Контролируемый компонент: `value: Rule[]` + `onChange`. Валидность набора
// считается `rulesInvalid()` — submit блокируется вызывающим кодом.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, Card, Input, Radio, Select, Space, Spin, Tag, Tooltip, Typography } from "antd";
import { CloseOutlined, DeleteOutlined, PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import type { PermissionCatalog, Rule, RuleArm } from "@shared/api/iam";
import { ruleArm } from "@shared/api/iam";
import { api } from "@shared/api/client";
import {
  WILDCARD,
  catalogModules,
  isLabelSelectable,
  resourcesForModule,
  usePermissionCatalog,
  verbOptions,
} from "@shared/api/usePermissionCatalog";
import { instanceFetcherFor, type InstanceFetcher } from "@shared/lib/resourceInstanceFetchers";
import { useContext } from "@shared/lib/context-store";

// Re-export WILDCARD для обратной совместимости импортёров RulesEditor.
export { WILDCARD };

/** Пустое правило для «Добавить правило» (ARM_ANCHOR по умолчанию). Scalar
 *  `module` (ровно один модуль на правило). */
export function emptyRule(): Rule {
  return { module: "", resources: [], verbs: [] };
}

/** Опции валидации правила. catalog — для labelSelectable-gating (опц.; без него
 *  labels-gating пропускается — back-compat для чистого unit-вызова). */
export interface RuleInvalidOpts {
  isSystem: boolean;
  catalog?: PermissionCatalog;
}

/** Список ошибок правила (человекочитаемые сообщения) — для подсветки + submit-gate. */
export function ruleInvalid(rule: Rule, opts: RuleInvalidOpts): string[] {
  const errs: string[] = [];
  const nonEmpty = (xs: string[] | undefined) => (xs ?? []).filter((s) => s.trim());
  // Scalar module (один модуль на правило).
  const module = (rule.module ?? "").trim();
  const ress = nonEmpty(rule.resources);
  const verbs = nonEmpty(rule.verbs);
  if (module === "") errs.push("Укажите модуль");
  if (ress.length === 0) errs.push("Укажите хотя бы один тип ресурса");
  if (verbs.length === 0) errs.push("Укажите хотя бы один глагол");

  const hasNames = (rule.resource_names ?? []).length > 0;
  const hasLabels = Object.keys(rule.match_labels ?? {}).length > 0;
  if (hasNames && hasLabels) {
    errs.push("resourceNames и matchLabels взаимоисключающи");
  }

  // wildcard-политика. module/resource-`*` — system-only.
  const wildIn = (xs: string[]) => xs.includes(WILDCARD);
  const moduleWild = module === WILDCARD;
  if (!opts.isSystem) {
    if (moduleWild) errs.push("Wildcard '*' в модуле доступен только системным ролям");
    if (wildIn(ress)) errs.push("Wildcard '*' в типах ресурсов доступен только системным ролям");
  }
  // `*` (resources/verbs) — только как единственный элемент списка.
  const wildSole = (xs: string[]) => !wildIn(xs) || (xs.length === 1 && xs[0] === WILDCARD);
  if (!wildSole(ress)) errs.push("Wildcard '*' в типах ресурсов — только единственным элементом");
  if (!wildSole(verbs)) errs.push("Wildcard '*' в глаголах — только единственным элементом");
  // `*` + selector → INVALID_ARGUMENT.
  if ((moduleWild || wildIn(ress) || wildIn(verbs)) && (hasNames || hasLabels)) {
    errs.push("Wildcard '*' нельзя комбинировать с resourceNames или matchLabels");
  }

  // labels-arm gating: в match_labels-арме каждый выбранный ресурс обязан быть
  // labelSelectable (есть resource-feed). Иначе backend вернёт INVALID_ARGUMENT
  // "type <module>.<resource> is not selectable (no resource feed)" — блокируем
  // submit заранее. Проверяем только при наличии каталога и в labels-арме.
  if (hasLabels && opts.catalog && module && module !== WILDCARD) {
    for (const r of ress) {
      if (r === WILDCARD) continue;
      if (!isLabelSelectable(opts.catalog, module, r)) {
        errs.push(
          `Тип «${module}.${r}» нельзя выбирать по меткам (нет resource feed) — снимите его или смените способ выбора инстансов`,
        );
      }
    }
  }
  return errs;
}

/** Невалидные правила набора (индекс + ошибки). Пустой набор → отдельная ошибка. */
export function rulesInvalid(rules: Rule[], opts: RuleInvalidOpts): { index: number; errors: string[] }[] {
  return rules.map((r, index) => ({ index, errors: ruleInvalid(r, opts) })).filter((x) => x.errors.length > 0);
}

const ARM_LABEL: Record<RuleArm, string> = {
  ARM_ANCHOR: "Все инстансы в scope",
  ARM_NAMES: "По именам (resourceNames)",
  ARM_LABELS: "По меткам (matchLabels)",
};
const ARM_TAG_COLOR: Record<RuleArm, string> = {
  ARM_ANCHOR: "default",
  ARM_NAMES: "geekblue",
  ARM_LABELS: "purple",
};

// Тип арм-селектора правила (radio): anchor / names / labels.
type ArmMode = "anchor" | "names" | "labels";

function armToMode(rule: Rule): ArmMode {
  const arm = ruleArm(rule);
  return arm === "ARM_NAMES" ? "names" : arm === "ARM_LABELS" ? "labels" : "anchor";
}

export function RulesEditor({
  value,
  onChange,
  isSystem = false,
}: {
  value: Rule[];
  onChange: (rules: Rule[]) => void;
  /** system-роль — wildcard module/resource разрешён (read-only seed-path; UI это
   *  обычно не редактирует, но флаг держим для parity). custom (default) — disabled. */
  isSystem?: boolean;
}) {
  // Backend-driven каталог опций. Loading/error/empty — явные состояния (не
  // silent-пустые dropdown'ы), редактор правил показывается только после успешной
  // загрузки.
  const catalogQuery = usePermissionCatalog();
  const catalog = catalogQuery.data;

  // Явный per-rule arm-mode override. Арм правила обычно выводится из данных
  // (resource_names/match_labels), но при переключении на «По именам»/«По меткам»
  // селектор ещё пуст — без override mode откатился бы обратно в anchor и picker
  // не показался бы. Override живёт до первого ввода (тогда данные совпадут).
  const [armOverrides, setArmOverrides] = useState<Record<number, ArmMode>>({});

  const invalid = useMemo(() => rulesInvalid(value, { isSystem, catalog }), [value, isSystem, catalog]);

  const moduleOptions = useMemo(() => catalogModules(catalog), [catalog]);

  if (catalogQuery.isLoading) {
    return (
      <div data-testid="role-rules-catalog-loading" style={{ padding: 16, textAlign: "center" }}>
        <Spin size="small" />{" "}
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          Загрузка каталога ресурсов…
        </Typography.Text>
      </div>
    );
  }

  if (catalogQuery.isError) {
    return (
      <Alert
        data-testid="role-rules-catalog-error"
        type="error"
        showIcon
        message="Не удалось загрузить каталог ресурсов"
        description="Редактор правил недоступен, пока каталог не загружен. Повторите попытку."
        action={
          <Button size="small" icon={<ReloadOutlined />} onClick={() => catalogQuery.refetch()}>
            Повторить
          </Button>
        }
      />
    );
  }

  if (moduleOptions.length === 0) {
    return (
      <Alert
        data-testid="role-rules-catalog-empty"
        type="warning"
        showIcon
        message="Каталог ресурсов пуст"
        description="Backend не вернул ни одного grantable-модуля — редактор правил недоступен."
      />
    );
  }

  const patchRule = (i: number, patch: Partial<Rule>) => {
    onChange(value.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  };

  const setArmMode = (i: number, mode: ArmMode) => {
    // Запоминаем явный выбор арма (override), чтобы picker/labels-редактор остался
    // виден даже при пустом селекторе.
    setArmOverrides((prev) => ({ ...prev, [i]: mode }));
    // Переключение арма очищает чужой селектор (XOR-инвариант).
    if (mode === "anchor") {
      patchRule(i, { resource_names: [], match_labels: {} });
    } else if (mode === "names") {
      patchRule(i, { match_labels: {}, resource_names: value[i].resource_names ?? [] });
    } else {
      patchRule(i, { resource_names: [], match_labels: value[i].match_labels ?? {} });
    }
  };

  const addRule = () => onChange([...value, emptyRule()]);
  const removeRule = (i: number) => {
    setArmOverrides((prev) => {
      // Сдвигаем override-индексы после удалённого правила.
      const next: Record<number, ArmMode> = {};
      for (const [k, v] of Object.entries(prev)) {
        const idx = Number(k);
        if (idx < i) next[idx] = v;
        else if (idx > i) next[idx - 1] = v;
      }
      return next;
    });
    onChange(value.filter((_, idx) => idx !== i));
  };

  return (
    <div data-testid="role-rules-editor">
      <Space direction="vertical" size={12} style={{ width: "100%" }}>
        {value.map((rule, i) => {
          // Tag отражает фактический арм (по данным); mode (radio + видимость
          // селектора) — явный override, иначе derived из данных.
          const mode = armOverrides[i] ?? armToMode(rule);
          const arm: RuleArm = mode === "names" ? "ARM_NAMES" : mode === "labels" ? "ARM_LABELS" : "ARM_ANCHOR";
          const errs = invalid.find((x) => x.index === i)?.errors ?? [];
          // cascade `module → resources` по СКАЛЯРУ. В labels-арме оставляем только
          // labelSelectable ресурсы (match_labels по non-selectable типу backend
          // reject'ит).
          const moduleResources = resourcesForModule(catalog, rule.module);
          const ruleResourceOptions =
            mode === "labels"
              ? moduleResources.filter((r) => isLabelSelectable(catalog, rule.module, r))
              : moduleResources;
          return (
            <Card
              key={i}
              size="small"
              data-testid={`role-rule-${i}`}
              title={
                <Space size={8}>
                  <Typography.Text strong>Правило {i + 1}</Typography.Text>
                  <Tag color={ARM_TAG_COLOR[arm]} data-testid={`role-rule-${i}-arm`}>
                    {ARM_LABEL[arm]}
                  </Tag>
                </Space>
              }
              extra={
                <Button
                  size="small"
                  type="text"
                  danger
                  icon={<DeleteOutlined />}
                  data-testid={`role-rule-${i}-remove`}
                  onClick={() => removeRule(i)}
                  title="Удалить правило"
                />
              }
            >
              <Space direction="vertical" size={8} style={{ width: "100%" }}>
                <ModuleSelect
                  testid={`role-rule-${i}-module`}
                  // Опции каталога (backend ground-truth). module-`*` — system-only:
                  // опция `*` появляется только для system-роли. Ровно ОДИН модуль на
                  // правило (single-select scalar).
                  value={rule.module}
                  options={isSystem ? [WILDCARD, ...moduleOptions] : moduleOptions}
                  systemOnlyWildcardHint={!isSystem}
                  onChange={(module) => {
                    // Смена module — отсеять resources, выпавшие из cascade (resource
                    // другого модуля превратился бы в no-op rule).
                    const allowed = new Set(resourcesForModule(catalog, module));
                    const resources = rule.resources.filter((r) => allowed.has(r) || r === WILDCARD);
                    patchRule(i, { module, resources });
                  }}
                />
                <CatalogSelect
                  label="Типы ресурсов"
                  testid={`role-rule-${i}-resources`}
                  placeholder={
                    rule.module === ""
                      ? "Сначала выберите модуль"
                      : mode === "labels"
                        ? "Выберите тип(ы), выбираемые по меткам"
                        : "Выберите тип(ы) ресурсов"
                  }
                  value={rule.resources}
                  // Cascade: опции — только resources выбранного module. resource-`*`
                  // — system-only. В labels-арме — только labelSelectable
                  // (отфильтровано в ruleResourceOptions).
                  options={isSystem ? [WILDCARD, ...ruleResourceOptions] : ruleResourceOptions}
                  systemOnlyWildcardHint={!isSystem}
                  onChange={(resources) => patchRule(i, { resources })}
                />
                <CatalogSelect
                  label="Глаголы"
                  testid={`role-rule-${i}-verbs`}
                  placeholder="Выберите глагол(ы)"
                  value={rule.verbs}
                  // verb-`*` РАЗРЕШЁН в custom-роли — если каталог разрешает.
                  options={verbOptions(catalog, isSystem)}
                  systemOnlyWildcardHint={false}
                  onChange={(verbs) => patchRule(i, { verbs })}
                />

                <div>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    Селекция инстансов
                  </Typography.Text>
                  <div style={{ marginTop: 4 }}>
                    <Radio.Group
                      data-testid={`role-rule-${i}-arm-mode`}
                      value={mode}
                      onChange={(e) => setArmMode(i, e.target.value as ArmMode)}
                      optionType="button"
                      buttonStyle="solid"
                      size="small"
                    >
                      <Radio.Button value="anchor" data-testid={`role-rule-${i}-arm-anchor`}>
                        Все в scope
                      </Radio.Button>
                      <Radio.Button value="names" data-testid={`role-rule-${i}-arm-names`}>
                        По именам
                      </Radio.Button>
                      <Radio.Button value="labels" data-testid={`role-rule-${i}-arm-labels`}>
                        По меткам
                      </Radio.Button>
                    </Radio.Group>
                  </div>
                </div>

                {mode === "names" && (
                  <ResourceNamesPicker
                    testid={`role-rule-${i}-resourceNames`}
                    catalog={catalog}
                    module={rule.module}
                    resources={rule.resources}
                    value={rule.resource_names ?? []}
                    onChange={(resource_names) => patchRule(i, { resource_names })}
                  />
                )}

                {mode === "labels" && (
                  <MatchLabelsEditor
                    testid={`role-rule-${i}-matchLabels`}
                    value={rule.match_labels ?? {}}
                    onChange={(match_labels) => patchRule(i, { match_labels })}
                  />
                )}

                {errs.length > 0 && (
                  <Alert
                    type="error"
                    showIcon
                    data-testid={`role-rule-${i}-invalid`}
                    message={
                      <ul style={{ margin: 0, paddingLeft: 18, fontSize: 12 }}>
                        {errs.map((e, j) => (
                          <li key={j}>{e}</li>
                        ))}
                      </ul>
                    }
                  />
                )}
              </Space>
            </Card>
          );
        })}

        <Button type="dashed" icon={<PlusOutlined />} onClick={addRule} data-testid="role-rule-add" block>
          Добавить правило
        </Button>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          Каждое правило — грант глаголов над «модуль × типы ресурсов»: ровно <b>один модуль</b> на правило (типы
          отфильтрованы по нему). Чтобы охватить несколько модулей — добавьте <b>отдельное правило</b> на каждый. Модуль,
          тип ресурса и глагол выбираются из выпадающих списков (опции — из платформенного каталога ресурсов). Сужение —
          по именам (resourceNames) <b>или</b> по меткам (matchLabels): по меткам выбираются только типы с поддержкой
          меток. Без сужения правило покрывает все инстансы в scope. Wildcard <code>*</code> в глаголах допустим; в
          модуле/типах — только системным ролям.
        </Typography.Text>
      </Space>
    </div>
  );
}

// CatalogSelect — AntD multi-select из каталога опций (backend ground-truth). Не
// free-text: `mode="multiple"` (НЕ `tags`) — пользователь выбирает только из
// предложенного `options`-списка, ручной набор строк недоступен. Используется для
// module / resource / verb. wildcard-`*` появляется опцией только когда он разрешён
// (для resource/verb — см. вызовы; module-`*` system-only).
function CatalogSelect({
  label,
  testid,
  placeholder,
  value,
  options,
  onChange,
  systemOnlyWildcardHint,
}: {
  label: string;
  testid: string;
  placeholder: string;
  value: string[];
  options: readonly string[];
  onChange: (tokens: string[]) => void;
  /** Показать бейдж «* — system-only» рядом с label (module/resource в custom). */
  systemOnlyWildcardHint: boolean;
}) {
  // Опции каталога. Если в `value` есть legacy/unknown-токен (из существующего
  // rule, отсутствующий в каталоге) — добавляем его опцией, чтобы Select его
  // отрисовал, а не молча выкинул (не теряем уже сохранённый грант).
  const optSet = new Set(options);
  const merged = [...options, ...value.filter((v) => !optSet.has(v))];
  return (
    <div>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {label}
        {systemOnlyWildcardHint && (
          <Tooltip title="Wildcard '*' здесь доступен только системным ролям">
            {" "}
            <Tag style={{ marginLeft: 4 }}>* — system-only</Tag>
          </Tooltip>
        )}
      </Typography.Text>
      <Select
        mode="multiple"
        data-testid={testid}
        placeholder={placeholder}
        value={value}
        options={merged.map((o) => ({ value: o, label: o }))}
        // Поиск по подстроке среди опций; ручной ввод новых токенов недоступен
        // (mode=multiple, не tags) — выбор только из каталога.
        optionFilterProp="label"
        style={{ width: "100%" }}
        onChange={(raw) => onChange(Array.from(new Set(raw as string[])))}
      />
    </div>
  );
}

// ModuleSelect — SINGLE-select модуля из каталога. Ровно ОДИН модуль на правило
// (scalar `module: string`), не multi-chips: при выборе нового значения старое
// замещается. `mode` НЕ задан → одиночный Select. wildcard-`*` появляется опцией
// только в system-роли (module-`*` system-only). Уже сохранённый legacy/unknown-
// токен добавляется опцией (не теряем грант).
function ModuleSelect({
  testid,
  value,
  options,
  onChange,
  systemOnlyWildcardHint,
}: {
  testid: string;
  value: string;
  options: readonly string[];
  onChange: (token: string) => void;
  systemOnlyWildcardHint: boolean;
}) {
  const merged = value && !options.includes(value) ? [...options, value] : [...options];
  return (
    <div>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        Модуль
        {systemOnlyWildcardHint && (
          <Tooltip title="Wildcard '*' здесь доступен только системным ролям">
            {" "}
            <Tag style={{ marginLeft: 4 }}>* — system-only</Tag>
          </Tooltip>
        )}
      </Typography.Text>
      <Select
        data-testid={testid}
        placeholder="Выберите модуль"
        // single-select scalar: один модуль на правило. value="" → не выбрано.
        value={value || undefined}
        options={merged.map((o) => ({ value: o, label: o }))}
        optionFilterProp="label"
        showSearch
        allowClear
        style={{ width: "100%" }}
        onChange={(raw) => onChange((raw as string | undefined) ?? "")}
      />
    </div>
  );
}

// ResourceNamesPicker — resource_names arm (pin-by-id):
//   • has_list_endpoint=true И есть фетчер → Select РЕАЛЬНЫХ инстансов (display-name
//     option, value=opaque id) + произвольный id вручную (mode=tags);
//   • has_list_endpoint=false / нет фетчера → free-text (hand-typed opaque id):
//     НИКОГДА Select, бэкенящийся несуществующим публичным List.
//
// Picker рендерится для (module,resource)-пар выбранного правила. Модуль скалярный
// (один на правило) → пары = {module} × resources. Если ХОТЯ БЫ одна пара НЕ
// picker-able (или пары не выбраны) → free-text (консервативно: не светим частичный
// список как полный).
function ResourceNamesPicker({
  testid,
  catalog,
  module,
  resources,
  value,
  onChange,
}: {
  testid: string;
  catalog: PermissionCatalog | undefined;
  module: string;
  resources: string[];
  value: string[];
  onChange: (tokens: string[]) => void;
}) {
  // (module,resource) пары текущего правила — один (scalar) module × resources.
  const pairs = useMemo(() => {
    if (!module) return [] as { module: string; resource: string }[];
    return resources.map((r) => ({ module, resource: r }));
  }, [module, resources]);

  // Picker-able, только если КАЖДАЯ пара: (a) has_list_endpoint=true в каталоге, (b)
  // имеет фетчер в registry-map. Иначе — free-text (не светим Select без реального
  // публичного List).
  const fetchers = useMemo(() => {
    if (pairs.length === 0) return null;
    const byModule = new Map((catalog?.modules ?? []).map((mm) => [mm.module, mm.resources ?? []]));
    const list: InstanceFetcher[] = [];
    for (const { module, resource } of pairs) {
      const catRes = byModule.get(module)?.find((rr) => rr.resource === resource);
      if (!catRes?.has_list_endpoint) return null; // нет публичного List → free-text
      const f = instanceFetcherFor(module, resource);
      if (!f) return null; // нет фетчера → free-text
      list.push(f);
    }
    return list;
  }, [pairs, catalog]);

  const pickerable = fetchers !== null && fetchers.length > 0;

  if (!pickerable) {
    return <ResourceNamesFreeText testid={testid} value={value} onChange={onChange} />;
  }
  return <ResourceNamesInstanceSelect testid={testid} fetchers={fetchers} value={value} onChange={onChange} />;
}

// Free-text режим: tags-Select без dropdown'а. resource_names — opaque object-id
// (case-sensitive!), вводятся вручную через Enter/запятую/пробел. `*` запрещён.
function ResourceNamesFreeText({
  testid,
  value,
  onChange,
}: {
  testid: string;
  value: string[];
  onChange: (tokens: string[]) => void;
}) {
  return (
    <div data-testid={testid}>
      <div data-testid={`${testid}-freetext`}>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          resourceNames (ручной ввод id)
        </Typography.Text>
        <Select
          mode="tags"
          aria-label="resourceNames"
          placeholder="opaque-id объектов (Enter после каждого)"
          value={value}
          // Свободный ввод токенов — dropdown не нужен (нет публичного List).
          open={false}
          tokenSeparators={[",", " "]}
          style={{ width: "100%" }}
          onChange={(raw) => onChange(sanitizeNames(raw as string[]))}
        />
      </div>
    </div>
  );
}

// Instance-Select: реальные инстансы выбранных (module,resource) через публичный
// List<Resource> (display-name option, value=opaque id). mode=tags → можно ещё и
// вписать произвольный id (pin невидимого). List-ошибка → пустые опции, но свободный
// ввод остаётся.
function ResourceNamesInstanceSelect({
  testid,
  fetchers,
  value,
  onChange,
}: {
  testid: string;
  fetchers: InstanceFetcher[];
  value: string[];
  onChange: (tokens: string[]) => void;
}) {
  const projectId = useContext((s) => s.project?.id ?? "");
  const accountId = useContext((s) => s.account?.id ?? "");

  // Запрос инстансов по каждому фетчеру. Ключ включает scope-id, чтобы при смене
  // контекста перезапрашивать. Ошибка/пусто → [] (свободный ввод сохраняется).
  const queries = fetchers.map((f) => {
    const q: Record<string, string> = { page_size: "500" };
    if (f.needsProject && projectId) q.project_id = projectId;
    if (f.needsAccount && accountId) q.account_id = accountId;
    return { fetcher: f, query: q };
  });

  const queryKey = ["role-rule-instances", fetchers.map((f) => f.spec.id).join(","), projectId, accountId];

  const { data: instanceRows } = useQuery({
    queryKey,
    queryFn: async () => {
      const rows: { id: string; name: string }[] = [];
      for (const { fetcher, query } of queries) {
        try {
          const resp = await api.list<Record<string, unknown>>(fetcher.spec.apiPath, query);
          const arr = (resp[fetcher.spec.payloadKey] as Record<string, unknown>[] | undefined) ?? [];
          for (const r of arr) {
            const id = (r.id as string) ?? "";
            if (!id) continue;
            const name = (r.name as string) || id;
            rows.push({ id, name });
          }
        } catch {
          // List недоступен для этого типа — деградируем (без опций), но не крэшим:
          // свободный ввод id остаётся доступным.
        }
      }
      return rows;
    },
    staleTime: 30_000,
  });

  const options = useMemo(() => {
    const seen = new Set<string>();
    const opts: { value: string; label: string }[] = [];
    for (const r of instanceRows ?? []) {
      if (seen.has(r.id)) continue;
      seen.add(r.id);
      // label = display-name (виден пользователю); value = opaque id (на провод).
      opts.push({ value: r.id, label: r.name });
    }
    return opts;
  }, [instanceRows]);

  return (
    <div data-testid={testid}>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        resourceNames (выберите инстанс или впишите id)
      </Typography.Text>
      <Select
        mode="tags"
        aria-label="resourceNames"
        placeholder="Выберите инстанс по имени или впишите opaque-id"
        value={value}
        options={options}
        // Поиск по display-name; выбор кладёт id (value). mode=tags разрешает ввести
        // произвольный id, которого нет в списке (pin невидимого).
        optionFilterProp="label"
        tokenSeparators={[",", " "]}
        style={{ width: "100%" }}
        onChange={(raw) => onChange(sanitizeNames(raw as string[]))}
      />
    </div>
  );
}

// Нормализация resource_names: trim, dedup, выкинуть пустые и `*` (валидным resource
// name им быть не может).
function sanitizeNames(raw: string[]): string[] {
  const tokens = raw
    .map((t) => t.trim())
    .filter(Boolean)
    .filter((t) => t !== WILDCARD);
  return Array.from(new Set(tokens));
}

// MatchLabelsEditor — key=value пары (AND-equality). Chip-list + add-row.
function MatchLabelsEditor({
  testid,
  value,
  onChange,
}: {
  testid: string;
  value: Record<string, string>;
  onChange: (labels: Record<string, string>) => void;
}) {
  const [k, setK] = useState("");
  const [v, setV] = useState("");
  const add = () => {
    const key = k.trim();
    if (!key) return;
    onChange({ ...value, [key]: v.trim() });
    setK("");
    setV("");
  };
  const remove = (key: string) => {
    const next = { ...value };
    delete next[key];
    onChange(next);
  };
  return (
    <div data-testid={testid}>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        matchLabels (AND-equality)
      </Typography.Text>
      <Space direction="vertical" size={6} style={{ width: "100%", marginTop: 4 }}>
        {Object.keys(value).length > 0 && (
          <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
            {Object.entries(value).map(([key, val]) => (
              <Tag
                key={key}
                color="purple"
                closable
                closeIcon={<CloseOutlined />}
                onClose={(e) => {
                  e.preventDefault();
                  remove(key);
                }}
                style={{ fontFamily: "ui-monospace, monospace" }}
              >
                {key}={val}
              </Tag>
            ))}
          </div>
        )}
        <Space.Compact style={{ width: "100%" }}>
          <Input
            placeholder="ключ"
            value={k}
            onChange={(e) => setK(e.target.value)}
            onPressEnter={add}
            data-testid={`${testid}-key`}
          />
          <Input
            placeholder="значение"
            value={v}
            onChange={(e) => setV(e.target.value)}
            onPressEnter={add}
            data-testid={`${testid}-value`}
          />
          <Button icon={<PlusOutlined />} onClick={add} disabled={!k.trim()} data-testid={`${testid}-add`}>
            Добавить
          </Button>
        </Space.Compact>
      </Space>
    </div>
  );
}
