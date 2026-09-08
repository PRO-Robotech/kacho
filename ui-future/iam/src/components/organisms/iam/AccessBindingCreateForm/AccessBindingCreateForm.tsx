// AccessBindingCreateForm — тело SCOPE-FIRST формы создания/актуализации привязок
// доступа (AccessBinding) под explicit-RBAC модель.
//
// Модель: грант = subjects[] + role + АНКЕР области + цель под этим анкером.
// Область — first-class измерение (явный селектор «Область действия»), НЕ скрытый
// «тип ресурса»; в форме её тир называется GLOBAL/ACCOUNT/PROJECT, на wire это
// dotted `iam.cluster|iam.account|iam.project` (GLOBAL ≡ анкер cluster_root).
//
// Тело запроса (buildCreateAccessBindingBody): {subjects[], role_id, scope_type,
// scope_id, target}, один POST на роль. Все три координаты — обязательные поля
// Create: `scope_type`/`scope_id` помечены required в proto, а `target` отвергается
// синхронно, если не задан ни один арм (INVALID_ARGUMENT «target is required…»,
// least-privilege spine F8). Прежнее имя пары анкера в CreateAccessBindingRequest
// ОТСУТСТВУЕТ — координата переименована в `scope_type`/`scope_id` (теги 4/5), а не
// выведена в reserved (там перечислены другие имена). Разница видна только в
// стволе: край распаковывает тело с DiscardUnknown, то есть неизвестное имя
// выбрасывает МОЛЧА и отвечает успехом, — поэтому форма собирает ровно каноничные
// имена, а здесь прежние не называются: мёртвая координата в шапке читается
// следующим как рабочая.
//
// Умолчание есть — но у ФОРМЫ, не у сервера, и это разные вещи. Сервер широкого
// гранта по умолчанию не выдаёт: без явного арма Create падает. Форма же
// преднабирает дискриминатор `_target_kind` в allInScope (см. setFieldsValue при
// монтировании), поэтому тело всегда уходит с явным армом, и «Точечно» —
// осознанное сужение, а не включение проверки. Если преднабор когда-нибудь снимут,
// это утверждение обязано поменяться вместе с ним.
//
// Что здесь ДЕЙСТВИТЕЛЬНО заперто пробой, а что нет — названо честно: форму тела
// по каждому арму держат `src/registry-iam1.test.ts` и
// `@shared/api/iam.wire.test.ts` (утверждают ЗНАЧЕНИЕ, которое возвращает
// сборщик тела). Сам преднабор дискриминатора отдельной пробы не имеет. Прежде
// здесь стояла ссылка на замок, который читал ЭТОТ файл как текст и искал в нём
// подстроки: он говорил о символах, а не о поведении, и снят вместе с классом.
//
// Селекторы (all/names/labels) живут в rules РОЛИ (единый источник истины) —
// форма биндинга их НЕ собирает.
//
// Переиспользуется в двух контекстах:
//   1. standalone full-page (AccessBindingCreatePage) — additive-only: N-create по
//      выбранным ролям, без pre-load и revoke;
//   2. embedded в зону-3 detail-страницы субъекта (ResourceShell child-create,
//      lockedSubject) — РЕКОНСАЙЛ: субъект ЗАЛОЧЕН, форма подгружает текущие
//      привилегии субъекта и АКТУАЛИЗИРУЕТ их для выбранного scope (added →
//      create, removed → revoke).
//
// Компонент НЕ зовёт page-level хуки (useHeaderRight/useBreadcrumb/FormShell) —
// они остаются в page-обёртке. Навигация — через колбэки onSuccess/onCancel.
//
// Структура (scope-first):
//   Секция «Субъект»  — тип субъекта + multi-id picker (или залоченный single).
//   Секция «Область»  — scope-tier (GLOBAL/ACCOUNT/PROJECT) + anchor-ресурс.
//   Секция «Роли»     — backend-driven assignable роли по scope_group; disabled
//                       пока scope не выбран. GLOBAL inline-guard: GLOBAL + не-
//                       cluster-admin роль → подсказка + блок submit.
//
// Роли: форма НЕ грузит весь listRoles и НЕ делает клиентскую scope-фильтрацию.
// После выбора scope делает ОДИН вызов iamApi.listAssignableRoles → рендерит РОВНО
// серверный набор, сгруппированный по scope_group. Смена scope → ре-фетч + сброс
// ставших невалидными ролей.
//
// Submit:
//   • standalone (additive-only) — за КАЖДУЮ выбранную роль один POST.
//   • lockedSubject (reconcile) — diff selected vs текущие (DIRECT) роли scope:
//     added → create, removed → revoke (DELETE по binding_id). Реконсайл касается
//     ТОЛЬКО DIRECT-привязок текущего scope; GROUP-derived и привязки на ДРУГИХ
//     scope НИКОГДА не трогаются.
// Все create+revoke — через Promise.allSettled: 409 ALREADY_EXISTS на create →
// успех (идемпотентно); часть упала → форма открыта, inline-Alert называет
// проблемные роли.

import { useEffect, useMemo, useRef, useState } from "react";
import { Alert, Button, Form, Input, Radio, Select, Space, Tag, Typography } from "antd";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@shared/api/client";
import { FormSection } from "@/components/organisms/form/FormSection";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { useContext } from "@shared/lib/context-store";
import {
  buildCreateAccessBindingBody,
  iamApi,
  IAM,
  CLUSTER_SCOPE_ID,
  SUBJECT_TYPE_ENUM,
  type AccessBindingScopeTier,
  type ResourceRef,
  type User,
  type ServiceAccount,
  type Group,
  type Account,
  type Project,
  type AssignableRole,
  type ScopeGroup,
  type SubjectPrivilege,
  type Subject,
} from "@shared/api/iam";
import { isAlreadyExistsError, mapApiErrorToMessage } from "@shared/lib/permissions";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScope, type PickerScope } from "@shared/lib/picker-search";

/** Русская плюрализация слова «роль» для счётчика в сообщении об ошибке. */
function pluralRole(n: number): string {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return "роль";
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20)) return "роли";
  return "ролей";
}

export type SubjectType = "user" | "service_account" | "group";
// UI-уровень scope-измерения: GLOBAL / ACCOUNT / PROJECT. На проводе GLOBAL ≡ tier
// CLUSTER.
export type ScopeTier = "GLOBAL" | "ACCOUNT" | "PROJECT";
// Back-compat alias для page/preset (deep-link сохраняет имена resource_type).
export type ResourceType = "account" | "project" | "cluster";

const SUBJECT_TYPES: SubjectType[] = ["user", "service_account", "group"];

/**
 * Чем сужается КАЖДЫЙ список этой формы у своего владельца (#528).
 *
 * Раньше ввод не покидал вкладку ни в одном из полей: список читался ОДНОЙ
 * страницей (`pageSize: 1000`), а сужение шло по загруженной метке. Тысяча
 * первый субъект был недостижим никаким вводом, а поле отвечало «нет
 * совпадений» — то есть утверждало об отсутствии человека, учётки или якоря то,
 * чего не спрашивало. У выпадающего списка нет «показать ещё» by construction,
 * поэтому узнать правду пользователю было неоткуда.
 *
 * Ключей два, и подставить один вместо другого нельзя: iam отвергает `CONTAINS`
 * на пользователе ЯВНО (`InvalidArgument` на всю страницу, с текстом, называющим
 * правильное написание), а слова `search` не знает никто, кроме пользователя.
 * Причина различия не в стиле: у пользователя имени нет вовсе — его узнают по
 * почте, поэтому владелец завёл выделенное слово, смотрящее на почту И на
 * идентификатор сразу.
 */
const SUBJECT_SCOPE: Record<SubjectType, PickerScope> = {
  user: pickerScope({ serverTerm: "search" }),
  // Group и ServiceAccount владелец сужает подстрокой по настоящему полю `name`:
  // белый список выражения — ровно `name`, разобранный узел применяется через
  // `ToSQL`, то есть оператор доезжает до SQL, а не схлопывается в равенство.
  service_account: pickerScope({ serverSearchField: "name" }),
  group: pickerScope({ serverSearchField: "name" }),
};

/** Якорь области: Account и Project владелец сужает тем же `name CONTAINS "…"`. */
const ANCHOR_SCOPE = pickerScope({ serverSearchField: "name" });

/**
 * Роли назначаемости сервер НЕ сужает — и выдумывать поле запроса нельзя.
 * `ListAssignableRolesRequest` несёт `resource_type`/`resource_id`/`scope_type`/
 * `scope_id` и страничную пару; поля выражения фильтра в нём нет вовсе, а
 * незнакомое имя — не «фильтр без эффекта», а отказ на всю страницу. Значит
 * остаётся второй законный исход: сузить в браузере и НАЗВАТЬ область в пустом
 * ответе, а не выдать её за отсутствие роли.
 */
const ROLE_SCOPE = pickerScope(undefined);

/**
 * Метки уже выбранного, пережившие сужение.
 *
 * Сервер отвечает по ВВОДУ, и сделанный ранее выбор в этот ответ попадать не
 * обязан: набрал второе имя — первое из ответа ушло. Без запоминания метки
 * выбранный субъект показался бы тегом `usr-…`, а якорь области — голым
 * идентификатором, то есть ровно тем, что канон консоли (правило 2) и
 * запрещает. Тот же приём, что в `RefSelect`.
 */
function keepChosenLabels(
  memo: { current: Map<string, string> },
  options: { value: string; label: string }[],
  selected: string[],
): { value: string; label: string }[] {
  for (const o of options) memo.current.set(o.value, o.label);
  const shown = new Set(options.map((o) => o.value));
  return selected
    .filter((v) => !!v && !shown.has(v) && memo.current.has(v))
    .map((v) => ({ value: v, label: memo.current.get(v) as string }));
}

/**
 * Маппинг UI-строки SubjectType → имя proto-enum `SubjectType`
 * (`SUBJECT_TYPE_USER` / `SUBJECT_TYPE_SERVICE_ACCOUNT` / `SUBJECT_TYPE_GROUP`).
 *
 * Поле `Subject.type` в proto — enum. grpc-gateway/protojson с
 * `DiscardUnknown:true` ТИХО схлопывает неизвестную enum-строку (нижне-регистровую
 * "user") в `SUBJECT_TYPE_UNSPECIFIED` — backend затем валит её `Illegal argument
 * subject_type ""`. Поэтому на проводе subjects[].type — enum-ИМЯ; нижний регистр —
 * только внутренний UI-тип.
 */
// SUBJECT_TYPE_ENUM живёт рядом с телом запроса (@shared/api/iam) — один словарь
// на все формы гранта.

/** Cluster singleton id для scope=GLOBAL (на проводе тир CLUSTER). */
const CLUSTER_RESOURCE_ID = CLUSTER_SCOPE_ID;

const SCOPE_TIERS: ScopeTier[] = ["GLOBAL", "ACCOUNT", "PROJECT"];
const SCOPE_TIER_LABEL: Record<ScopeTier, string> = {
  GLOBAL: "GLOBAL",
  ACCOUNT: "ACCOUNT",
  PROJECT: "PROJECT",
};
const SCOPE_TIER_HINT: Record<ScopeTier, string> = {
  GLOBAL: "На весь кластер. Допустим только для роли cluster-admin (*.*.*).",
  ACCOUNT: "Право будет действовать в границах выбранного аккаунта и его проектов.",
  PROJECT: "Право будет действовать в границах выбранного проекта.",
};

/** Anchor-тир запроса из UI scope-измерения. GLOBAL → CLUSTER. */
const WIRE_TIER_BY_SCOPE: Record<ScopeTier, AccessBindingScopeTier> = {
  GLOBAL: "CLUSTER",
  ACCOUNT: "ACCOUNT",
  PROJECT: "PROJECT",
};

/** Аргумент resource_type для listAssignableRoles из UI scope. GLOBAL → cluster. */
const ASSIGNABLE_RESOURCE_TYPE: Record<ScopeTier, ResourceType> = {
  GLOBAL: "cluster",
  ACCOUNT: "account",
  PROJECT: "project",
};

/** UI scope-измерение из legacy preset resource_type (deep-link back-compat). */
function scopeFromResourceType(rt?: ResourceType): ScopeTier | undefined {
  if (rt === "cluster") return "GLOBAL";
  if (rt === "account") return "ACCOUNT";
  if (rt === "project") return "PROJECT";
  return undefined;
}

// Порядок и заголовки секций picker'а — РОВНО по серверному scope_group.
const SCOPE_GROUP_ORDER: ScopeGroup[] = ["SYSTEM", "ACCOUNT", "PROJECT"];
const SCOPE_GROUP_LABEL: Record<ScopeGroup, string> = {
  SYSTEM: "Системные",
  ACCOUNT: "Роли аккаунта",
  PROJECT: "Роли проекта",
  SCOPE_GROUP_UNSPECIFIED: "Прочие",
};

/**
 * Является ли assignable-роль cluster-admin ролью (`*.*.*`).
 *
 * Backend-норматив: `GLOBAL + selector all` легален ТОЛЬКО для cluster-admin роли;
 * для прочих на GLOBAL обязателен names/labels-селектор. assignable-проекция роли
 * НЕ несёт rules[], поэтому UI распознаёт cluster-admin по каноническому имени
 * системной роли `admin` (роль `*.*.*`). Это inline-подсказка/guard — авторитетная
 * валидация остаётся за backend.
 */
function isClusterAdminRole(r: AssignableRole | undefined): boolean {
  if (!r) return false;
  return !!r.is_system && (r.name === "admin" || r.name === "*.*.*");
}

export interface AccessBindingPreset {
  subject_type?: SubjectType;
  subject_id?: string;
  role_id?: string;
  resource_type?: ResourceType;
  resource_id?: string;
}

interface Props {
  /** Залоченный субъект (embedded-режим с detail субъекта): subject_type/
   *  subject_id предзаполнены и disabled. Включает РЕКОНСАЙЛ-режим. */
  lockedSubject?: { type: SubjectType; id: string };
  /** Home-account субъекта — в lockedSubject-режиме scope по умолчанию =
   *  ACCOUNT:<subjectAccountId>, чтобы типичный кейс предзаполнился сразу. */
  subjectAccountId?: string | null;
  /** Deep-link presets (cluster-admin grant и т.п.). */
  preset?: AccessBindingPreset;
  onSuccess: () => void;
  onCancel: () => void;
}

export function AccessBindingCreateForm({ lockedSubject, subjectAccountId, preset, onSuccess, onCancel }: Props) {
  const qc = useQueryClient();
  const account = useContext((s) => s.account);
  const [form] = Form.useForm();

  const presetSubjectType = lockedSubject?.type ?? preset?.subject_type;
  const presetSubjectId = lockedSubject?.id ?? preset?.subject_id;
  const lockSubject = !!lockedSubject;
  const reconcile = lockSubject;
  const homeAccountId = reconcile ? (subjectAccountId ?? null) : null;

  // Стартовый scope (scope-first). reconcile → ACCOUNT:<homeAccount>; preset
  // (deep-link) → из resource_type; иначе НЕ выбран (поле «Роли» disabled).
  const presetScope = scopeFromResourceType(preset?.resource_type);
  const initialScope: ScopeTier | undefined = reconcile ? "ACCOUNT" : (presetScope ?? undefined);
  // Какой anchor-picker рендерить (для дефолтного случая — account-ветка).
  const initialScopeForPicker: ScopeTier = initialScope ?? "ACCOUNT";
  const initialAnchorId: string | undefined = reconcile
    ? (homeAccountId ?? undefined)
    : presetScope === "GLOBAL"
      ? CLUSTER_RESOURCE_ID
      : (preset?.resource_id ?? undefined);

  const [subjectType, setSubjectType] = useState<SubjectType>(presetSubjectType ?? "user");
  const [scope, setScope] = useState<ScopeTier>(initialScopeForPicker);

  const [inlineError, setInlineError] = useState<{
    type: "warning" | "error";
    message: string;
  } | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    form.setFieldsValue({
      subject_type: presetSubjectType ?? "user",
      subject_id: presetSubjectId ?? undefined,
      subject_ids: reconcile || !presetSubjectId ? [] : [presetSubjectId],
      role_ids: reconcile ? [] : preset?.role_id ? [preset.role_id] : [],
      scope: initialScope,
      scope_ref_id: initialAnchorId,
      // IAM-1 F8: у сервера target REQUIRED и умолчания не имеет; преднабор здесь —
      // умолчание ФОРМЫ, чтобы тело всегда несло явный арм (см. шапку файла).
      _target_kind: "allInScope",
      target_resources: [],
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ── Subject data ──
  // Ввод субъекта уходит запросом ТОМУ владельцу, чей тип сейчас выбран, и
  // только ему. Разносить по типам обязательно: иначе набранное имя группы
  // уезжало бы ещё и в список пользователей — запрос, которого никто не просил,
  // и сброс чужого кэша от чужого ввода.
  const subjectScope = SUBJECT_SCOPE[subjectType];
  const [subjectTerm, setSubjectTerm] = useState("");
  const debouncedSubjectTerm = useDebouncedValue(subjectTerm, subjectScope.asksServer ? 250 : 0);
  const subjectQueryFor = (t: SubjectType): Record<string, string> =>
    subjectType === t ? SUBJECT_SCOPE[t].query(debouncedSubjectTerm) : {};
  const userQuery = subjectQueryFor("user");
  const saQuery = subjectQueryFor("service_account");
  const groupQuery = subjectQueryFor("group");

  const users = useQuery({
    // Ключ несёт ввод: без него react-query отдал бы прежний ответ на новый
    // вопрос, и сужение выглядело бы сломанным именно там, где оно работает.
    queryKey: ["iam", "users", "list", userQuery.filter ?? ""],
    queryFn: () => iamApi.listUsers({ pageSize: "1000", ...userQuery }),
    staleTime: 30_000,
  });
  const sas = useQuery({
    queryKey: ["iam", "service-accounts", "all", saQuery.filter ?? ""],
    queryFn: async () => {
      // Перечисление аккаунтов вводом НЕ сужается: набранное относится к имени
      // служебной учётки, а не аккаунта. Сузив внешний список, мы потеряли бы
      // аккаунты, в которых искомая учётка как раз и лежит.
      const accs = await iamApi.listAccounts({ pageSize: "1000" });
      const all: ServiceAccount[] = [];
      for (const a of accs.accounts) {
        const r = await iamApi.listServiceAccounts({
          account_id: a.id,
          pageSize: "1000",
          ...saQuery,
        });
        all.push(...(r.service_accounts ?? []));
      }
      return all;
    },
    enabled: subjectType === "service_account",
    staleTime: 30_000,
  });
  const groups = useQuery({
    queryKey: ["iam", "groups", "all", groupQuery.filter ?? ""],
    queryFn: async () => {
      // То же, что у служебных учёток: внешний список — перечисление аккаунтов,
      // ввод относится к имени группы внутри каждого из них.
      const accs = await iamApi.listAccounts({ pageSize: "1000" });
      const all: Group[] = [];
      for (const a of accs.accounts) {
        const r = await iamApi.listGroups({ account_id: a.id, pageSize: "1000", ...groupQuery });
        all.push(...(r.groups ?? []));
      }
      return all;
    },
    enabled: subjectType === "group",
    staleTime: 30_000,
  });
  const subjectListLoading = users.isLoading || sas.isLoading || groups.isLoading;

  const subjectOptions = useMemo(() => {
    switch (subjectType) {
      case "user":
        return (users.data?.users ?? []).map((u: User) => ({
          value: u.id,
          label: `${u.email || u.display_name || u.id} · ${u.id}`,
        }));
      case "service_account":
        return (sas.data ?? []).map((sa) => ({
          value: sa.id,
          label: `${sa.name} · ${sa.id}`,
        }));
      case "group":
        return (groups.data ?? []).map((g) => ({
          value: g.id,
          label: `${g.name} · ${g.id}`,
        }));
    }
  }, [subjectType, users.data, sas.data, groups.data]);

  // ── Scope-anchor data (account/project; GLOBAL — singleton, без picker'а) ──
  // У каждого якорного поля свой ввод: они рендерятся в разных ветках scope, и
  // общий ввод означал бы, что набранное для проекта сужает ещё и аккаунты.
  const headerAccountId = account?.id ?? "";
  const [accountTerm, setAccountTerm] = useState("");
  const debouncedAccountTerm = useDebouncedValue(accountTerm, ANCHOR_SCOPE.asksServer ? 250 : 0);
  const accountQuery = ANCHOR_SCOPE.query(debouncedAccountTerm);
  const [projectTerm, setProjectTerm] = useState("");
  const debouncedProjectTerm = useDebouncedValue(projectTerm, ANCHOR_SCOPE.asksServer ? 250 : 0);
  const projectQuery = ANCHOR_SCOPE.query(debouncedProjectTerm);

  const accounts = useQuery({
    queryKey: ["iam", "accounts", "list", accountQuery.filter ?? ""],
    queryFn: () => iamApi.listAccounts({ pageSize: "1000", ...accountQuery }),
    enabled: scope === "ACCOUNT",
    staleTime: 30_000,
  });
  const projects = useQuery({
    queryKey: ["iam", "projects", "by-account", headerAccountId, projectQuery.filter ?? ""],
    queryFn: () =>
      iamApi.listProjects(
        headerAccountId
          ? { account_id: headerAccountId, pageSize: "1000", ...projectQuery }
          : { pageSize: "1000", ...projectQuery },
      ),
    enabled: scope === "PROJECT",
    staleTime: 30_000,
  });

  const accountOptions = useMemo(
    () =>
      (accounts.data?.accounts ?? []).map((a: Account) => ({
        value: a.id,
        label: `${a.name || a.id} · ${a.id}`,
      })),
    [accounts.data],
  );
  const projectOptions = useMemo(
    () =>
      (projects.data?.projects ?? []).map((p: Project) => ({
        value: p.id,
        label: `${p.name || p.id} · ${p.id}`,
      })),
    [projects.data],
  );

  // Текущий выбранный scope/anchor (watch формы). useWatch вызываем БЕЗУСЛОВНО
  // (правило хуков) — GLOBAL-ветку резолвим ниже.
  const watchedScope = Form.useWatch("scope", form) as ScopeTier | undefined;
  // IAM-1 F8 target-дискриминатор: allInScope (широкий opt-in) vs resources[] (least-priv).
  const watchedTargetKind = (Form.useWatch("_target_kind", form) as string | undefined) ?? "allInScope";
  const watchedScopeRefId = Form.useWatch("scope_ref_id", form) as string | undefined;
  // GLOBAL: anchor фиксирован (singleton) — не зависит от поля scope_ref_id.
  const watchedAnchorId = watchedScope === "GLOBAL" ? CLUSTER_RESOURCE_ID : watchedScopeRefId;

  // Scope «выбран» (для GLOBAL anchor фиксирован — singleton). До выбора поле
  // «Роли» disabled и assignable не фетчится.
  const scopeSelected = !!watchedScope && !!watchedAnchorId;

  // Выбранное обязано пережить сужение — см. `keepChosenLabels`.
  const watchedSubjectId = Form.useWatch("subject_id", form) as string | undefined;
  const watchedSubjectIds = (Form.useWatch("subject_ids", form) as string[] | undefined) ?? [];
  const subjectLabelMemo = useRef(new Map<string, string>());
  const anchorLabelMemo = useRef(new Map<string, string>());
  const subjectOptionList = subjectOptions ?? [];
  const keptSubjects = keepChosenLabels(
    subjectLabelMemo,
    subjectOptionList,
    reconcile ? [watchedSubjectId ?? ""] : watchedSubjectIds,
  );
  const subjectSelectOptions = keptSubjects.length > 0 ? [...keptSubjects, ...subjectOptionList] : subjectOptionList;
  const anchorOptionList = scope === "PROJECT" ? projectOptions : accountOptions;
  const keptAnchor = keepChosenLabels(anchorLabelMemo, anchorOptionList, [watchedScopeRefId ?? ""]);
  const anchorSelectOptions = keptAnchor.length > 0 ? [...keptAnchor, ...anchorOptionList] : anchorOptionList;

  // listAssignableRoles по resource_type (account/project/cluster) и anchor.
  const assignableResourceType: ResourceType | undefined = watchedScope
    ? ASSIGNABLE_RESOURCE_TYPE[watchedScope]
    : undefined;

  const assignableQ = useQuery({
    queryKey: ["iam", "assignable-roles", assignableResourceType ?? "", watchedAnchorId ?? ""],
    queryFn: () => iamApi.listAssignableRoles(assignableResourceType ?? "", watchedAnchorId ?? ""),
    enabled: scopeSelected,
    staleTime: 0,
  });
  const assignableRoles = useMemo<AssignableRole[]>(() => assignableQ.data?.roles ?? [], [assignableQ.data]);
  const roleById = useMemo(() => {
    const m = new Map<string, AssignableRole>();
    for (const r of assignableRoles) m.set(r.role_id, r);
    return m;
  }, [assignableRoles]);
  const roleNameById = useMemo(() => {
    const m = new Map<string, string>();
    for (const r of assignableRoles) m.set(r.role_id, r.name);
    return m;
  }, [assignableRoles]);
  const assignableIdSet = useMemo(() => new Set(assignableRoles.map((r) => r.role_id)), [assignableRoles]);

  // Опции Select'а, сгруппированные по серверному scope_group.
  const roleOptions = useMemo(() => {
    const byGroup = new Map<ScopeGroup, AssignableRole[]>();
    for (const r of assignableRoles) {
      const g = r.scope_group ?? "SCOPE_GROUP_UNSPECIFIED";
      const arr = byGroup.get(g) ?? [];
      arr.push(r);
      byGroup.set(g, arr);
    }
    const orderedGroups: ScopeGroup[] = [
      ...SCOPE_GROUP_ORDER.filter((g) => byGroup.has(g)),
      ...Array.from(byGroup.keys()).filter((g) => !SCOPE_GROUP_ORDER.includes(g)),
    ];
    return orderedGroups.map((g) => ({
      label: SCOPE_GROUP_LABEL[g],
      title: SCOPE_GROUP_LABEL[g],
      options: (byGroup.get(g) ?? []).map((r) => ({
        value: r.role_id,
        label: r.name,
        title: r.name,
      })),
    }));
  }, [assignableRoles]);

  const selectedRoleIds: string[] = Form.useWatch("role_ids", form) ?? [];

  // ── reconcile (lockedSubject): подгрузка текущих привилегий субъекта ──
  const privilegesQ = useQuery({
    queryKey: ["iam", "subject-privileges", "reconcile", presetSubjectType, presetSubjectId],
    queryFn: () =>
      iamApi.listSubjectPrivileges(presetSubjectType ?? "user", presetSubjectId ?? "", {
        page_size: "1000",
      }),
    enabled: reconcile && !!presetSubjectId,
    staleTime: 0,
  });
  const allPrivileges = useMemo<SubjectPrivilege[]>(() => privilegesQ.data?.privileges ?? [], [privilegesQ.data]);

  // pre-selected = role_id ПРЯМЫХ (DIRECT) привязок субъекта на текущем scope.
  // GROUP-derived и привязки других scope в карту НЕ попадают.
  const { currentRoleIds, roleToBindingId, privRoleName } = useMemo(() => {
    const ids: string[] = [];
    const map = new Map<string, string>();
    const names = new Map<string, string>();
    if (!reconcile || !watchedScope || !watchedAnchorId) {
      return { currentRoleIds: ids, roleToBindingId: map, privRoleName: names };
    }
    const wantResourceType = ASSIGNABLE_RESOURCE_TYPE[watchedScope];
    for (const p of allPrivileges) {
      const deriv = p.derivation ?? "DIRECT";
      if (deriv !== "DIRECT") continue;
      if (p.resource_type !== wantResourceType) continue;
      if ((p.resource_id ?? "") !== watchedAnchorId) continue;
      if (map.has(p.role_id)) continue;
      ids.push(p.role_id);
      map.set(p.role_id, p.binding_id);
      if (p.role_name) names.set(p.role_id, p.role_name);
    }
    return { currentRoleIds: ids, roleToBindingId: map, privRoleName: names };
  }, [reconcile, allPrivileges, watchedScope, watchedAnchorId]);

  const displayName = (roleId: string): string => roleNameById.get(roleId) ?? privRoleName.get(roleId) ?? roleId;

  const selectedExtraOptions = useMemo(() => {
    const known = new Set(assignableRoles.map((r) => r.role_id));
    return selectedRoleIds
      .filter((id) => !known.has(id))
      .map((id) => ({ value: id, label: displayName(id), title: displayName(id) }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRoleIds, assignableRoles, privRoleName]);

  const finalRoleOptions = useMemo(
    () =>
      selectedExtraOptions.length > 0
        ? [
            ...roleOptions,
            {
              label: SCOPE_GROUP_LABEL.SCOPE_GROUP_UNSPECIFIED,
              title: SCOPE_GROUP_LABEL.SCOPE_GROUP_UNSPECIFIED,
              options: selectedExtraOptions,
            },
          ]
        : roleOptions,
    [roleOptions, selectedExtraOptions],
  );

  const preselectKey = useMemo(
    () => `${watchedScope ?? ""}|${watchedAnchorId ?? ""}|${currentRoleIds.slice().sort().join(",")}`,
    [watchedScope, watchedAnchorId, currentRoleIds],
  );
  const appliedPreselectRef = useRef<string | null>(null);
  useEffect(() => {
    if (!reconcile) return;
    if (privilegesQ.isLoading) return;
    if (appliedPreselectRef.current === preselectKey) return;
    appliedPreselectRef.current = preselectKey;
    form.setFieldValue("role_ids", currentRoleIds.slice());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [preselectKey, privilegesQ.isLoading, reconcile]);

  // Смена scope → сброс ставших невалидными выбранных ролей.
  const prunedKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (!scopeSelected) return;
    if (!assignableQ.isSuccess) return;
    const pruneKey = `${watchedScope ?? ""}|${watchedAnchorId ?? ""}|${assignableRoles
      .map((r) => r.role_id)
      .sort()
      .join(",")}`;
    if (prunedKeyRef.current === pruneKey) return;
    prunedKeyRef.current = pruneKey;
    const cur = (form.getFieldValue("role_ids") as string[] | undefined) ?? [];
    const keepReconcile = new Set(reconcile ? currentRoleIds : []);
    const next = cur.filter((id) => assignableIdSet.has(id) || keepReconcile.has(id));
    if (next.length !== cur.length) {
      form.setFieldValue("role_ids", next);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assignableQ.isSuccess, watchedScope, watchedAnchorId, assignableRoles, scopeSelected]);

  // Дельта для подсказки (Добавить N · Отозвать M) — только reconcile.
  const addedCount = useMemo(
    () => selectedRoleIds.filter((id) => !currentRoleIds.includes(id)).length,
    [selectedRoleIds, currentRoleIds],
  );
  const removedCount = useMemo(
    () => currentRoleIds.filter((id) => !selectedRoleIds.includes(id)).length,
    [selectedRoleIds, currentRoleIds],
  );

  // ── GLOBAL inline-guard: GLOBAL + не-cluster-admin роль ──
  // На GLOBAL легальна только cluster-admin роль (*.*.*); прочим нужен
  // names/labels-селектор в rules. Если выбрана хоть одна не-cluster-admin роль на
  // GLOBAL — предупреждаем и блокируем submit (backend всё равно отклонит).
  const globalGuardRoles = useMemo(() => {
    if (watchedScope !== "GLOBAL") return [];
    return selectedRoleIds.filter((id) => !isClusterAdminRole(roleById.get(id)));
  }, [watchedScope, selectedRoleIds, roleById]);
  const globalGuardActive = globalGuardRoles.length > 0;

  // ── submit ──
  const onFinish = async (v: Record<string, unknown>) => {
    const roleIds = (v.role_ids as string[] | undefined) ?? [];
    setInlineError(null);

    const uiScope = v.scope as ScopeTier;
    const anchorId = uiScope === "GLOBAL" ? CLUSTER_RESOURCE_ID : (v.scope_ref_id as string);
    // GLOBAL guard: блокируем submit, если GLOBAL + не-cluster-admin роль выбрана.
    if (uiScope === "GLOBAL") {
      const offending = roleIds.filter((id) => !isClusterAdminRole(roleById.get(id)));
      if (offending.length > 0) return;
    }
    // Anchor гранта — dotted scope_type + scope_id. GLOBAL → тир CLUSTER + singleton.
    // `scope_ref` — снятое имя (тег 10 tombstoned вместе с именем): край его
    // выбрасывает, а обязательные scope_type/scope_id остаются пустыми.
    const scopeTier = WIRE_TIER_BY_SCOPE[uiScope];

    // thin binding несёт subjects[] (1..32). standalone → multi-subject из формы;
    // reconcile → один залоченный субъект.
    const subjectIds = reconcile
      ? [v.subject_id as string].filter(Boolean)
      : ((v.subject_ids as string[] | undefined) ?? []).filter(Boolean);
    const subjects: Subject[] = subjectIds.map((id) => ({
      // proto enum wire-form: enum-имя, не нижне-регистровая строка.
      type: SUBJECT_TYPE_ENUM[subjectType],
      id,
    }));

    // IAM-1 F8: target REQUIRED (least-priv). resources → закрытый {type,id};
    // пустой resources-набор — инвалиден (нет sentinel-по-умолчанию; для «всё под
    // anchor'ом» есть явный allInScope{}).
    const targetKindVal = (v._target_kind as string | undefined) ?? "allInScope";
    let targetResources: ResourceRef[] | undefined;
    if (targetKindVal === "resources") {
      targetResources = ((v.target_resources as Array<{ type?: string; id?: string }> | undefined) ?? [])
        .filter((r): r is { type: string; id: string } => !!(r && r.type && r.id))
        .map((r) => ({ type: r.type, id: r.id }));
      if (targetResources.length === 0) {
        setInlineError({
          type: "error",
          message:
            "Укажите хотя бы один объект (ResourceRef) или выберите «Вся область» (allInScope): цель обязательна.",
        });
        return;
      }
    }

    const added = roleIds.filter((id) => !currentRoleIds.includes(id));
    const removed = reconcile ? currentRoleIds.filter((id) => !roleIds.includes(id)) : [];

    if (added.length === 0 && removed.length === 0) {
      if (reconcile) onSuccess();
      return;
    }

    // Тела собираются ДО перехода в submitting: buildCreateAccessBindingBody
    // бросает на гранте, который нечем адресовать (нет субъекта / роли / anchor'а),
    // и такой отказ должен стать видимым сообщением, а не зависшей кнопкой.
    let addBodies: { roleId: string; body: Record<string, unknown> }[];
    try {
      addBodies = added.map((roleId) => ({
        roleId,
        body: buildCreateAccessBindingBody({ subjects, roleId, scopeTier, scopeId: anchorId, targetResources }),
      }));
    } catch (e) {
      setInlineError({ type: "error", message: mapApiErrorToMessage(e) });
      return;
    }

    setSubmitting(true);

    type Op = { kind: "add" | "remove"; roleId: string; promise: Promise<unknown> };
    const ops: Op[] = [
      ...addBodies.map(({ roleId, body }) => ({
        kind: "add" as const,
        roleId,
        promise: api.create(IAM.accessBindings, body),
      })),
      ...removed.map((roleId) => ({
        kind: "remove" as const,
        roleId,
        promise: api.delete(`${IAM.accessBindings}/${roleToBindingId.get(roleId)}`),
      })),
    ];

    const results = await Promise.allSettled(ops.map((o) => o.promise));

    const failedAdd: { roleId: string; message: string }[] = [];
    const failedRemove: { roleId: string; message: string }[] = [];
    results.forEach((res, i) => {
      if (res.status === "fulfilled") return;
      if (ops[i].kind === "add" && isAlreadyExistsError(res.reason)) return;
      const entry = { roleId: ops[i].roleId, message: mapApiErrorToMessage(res.reason) };
      (ops[i].kind === "add" ? failedAdd : failedRemove).push(entry);
    });

    void qc.invalidateQueries({ queryKey: ["iam", "access-bindings"] });
    void qc.invalidateQueries({ queryKey: ["cluster-admins"] });
    void qc.invalidateQueries({ queryKey: ["iam", "subject-privileges"] });

    setSubmitting(false);

    const anyFailed = failedAdd.length + failedRemove.length > 0;
    if (!anyFailed) {
      onSuccess();
      return;
    }

    const failedAddIds = failedAdd.map((f) => f.roleId);
    const failedRemoveIds = failedRemove.map((f) => f.roleId);
    let nextSelection: string[];
    if (reconcile) {
      nextSelection = Array.from(new Set([...roleIds, ...failedRemoveIds]));
    } else {
      nextSelection = failedAddIds;
    }
    form.setFieldValue("role_ids", nextSelection);

    const lines = [
      ...failedAdd.map((f) => `Не добавлена ${displayName(f.roleId)}: ${f.message}`),
      ...failedRemove.map((f) => `Не отозвана ${displayName(f.roleId)}: ${f.message}`),
    ].join("\n");
    const totalFailed = failedAdd.length + failedRemove.length;
    setInlineError({
      type: "error",
      message: `Не удалось применить ${totalFailed} ${pluralRole(totalFailed)}:\n${lines}`,
    });
  };

  return (
    <div style={{ maxWidth: 720 }}>
      {inlineError && (
        <Alert
          type={inlineError.type}
          showIcon
          style={{ marginBottom: 12, whiteSpace: "pre-line" }}
          message={inlineError.message}
          closable
          onClose={() => setInlineError(null)}
          data-testid="access-bindings-create-error"
        />
      )}
      <Form
        form={form}
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "auto" }}
        labelAlign="left"
        colon={false}
        size="middle"
        onFinish={onFinish}
        data-testid="access-bindings-create-form"
      >
        {/* ── Секция «Субъект» ── */}
        <FormSection title="Субъект">
          <Form.Item label="Тип субъекта" name="subject_type" required>
            <Select
              disabled={lockSubject}
              data-testid="access-bindings-subject-type"
              options={SUBJECT_TYPES.map((t) => ({ value: t, label: t }))}
              onChange={(val) => {
                setSubjectType(val as SubjectType);
                form.setFieldValue("subject_id", undefined);
                form.setFieldValue("subject_ids", []);
              }}
            />
          </Form.Item>

          {reconcile ? (
            <Form.Item
              label="Субъект"
              name="subject_id"
              required
              rules={[{ required: true, message: "Выберите субъект" }]}
            >
              <Select
                disabled
                data-testid="access-bindings-subject-id"
                placeholder={`Выберите ${subjectType}`}
                options={subjectSelectOptions}
                showSearch
                onSearch={setSubjectTerm}
                // Сузил сервер — клиент НЕ пересеивает: владелец смотрит на почту
                // и идентификатор (у групп и учёток — на `name`), а метка варианта
                // склеена из имени и идентификатора, и повторное сужение вычло бы
                // из ответа строки, присланные именно по этому вводу.
                {...(subjectScope.asksServer
                  ? { filterOption: false as const }
                  : { optionFilterProp: "label" as const })}
                title={subjectScope.notice}
                // Пустой ответ обязан называть свою ОБЛАСТЬ. Именно здесь жила
                // ложь: «нет совпадений» на месте «нет среди загруженных».
                notFoundContent={subjectListLoading ? undefined : subjectScope.emptyText}
                loading={subjectListLoading}
              />
            </Form.Item>
          ) : (
            <Form.Item
              label="Субъекты"
              name="subject_ids"
              required
              rules={[
                {
                  validator: (_r, value: string[] | undefined) =>
                    value && value.length > 0
                      ? Promise.resolve()
                      : Promise.reject(new Error("Выберите хотя бы одного субъекта")),
                },
              ]}
            >
              <Select
                mode="multiple"
                data-testid="access-bindings-subject-ids"
                placeholder={`Выберите ${subjectType} (можно несколько, до 32)`}
                options={subjectSelectOptions}
                showSearch
                onSearch={setSubjectTerm}
                {...(subjectScope.asksServer
                  ? { filterOption: false as const }
                  : { optionFilterProp: "label" as const })}
                title={subjectScope.notice}
                notFoundContent={subjectListLoading ? undefined : subjectScope.emptyText}
                maxCount={32}
                loading={subjectListLoading}
              />
            </Form.Item>
          )}
        </FormSection>

        {/* ── Секция «Область действия» (scope-first) ── */}
        <FormSection title="Область действия">
          <Form.Item
            label="Область"
            name="scope"
            required
            rules={[{ required: true, message: "Выберите область действия" }]}
          >
            <Select
              data-testid="access-bindings-scope"
              placeholder="GLOBAL / ACCOUNT / PROJECT"
              options={SCOPE_TIERS.map((t) => ({
                value: t,
                label: SCOPE_TIER_LABEL[t],
                title: SCOPE_TIER_LABEL[t],
              }))}
              onChange={(val) => {
                const next = val as ScopeTier;
                setScope(next);
                // Смена scope сбрасывает anchor; GLOBAL — singleton (поле скрыто).
                form.setFieldValue("scope_ref_id", next === "GLOBAL" ? CLUSTER_RESOURCE_ID : undefined);
              }}
            />
          </Form.Item>

          {/* Anchor-ресурс scope: ACCOUNT → Account-picker; PROJECT → Project-
              picker; GLOBAL — singleton (поле скрыто, anchor фиксирован). */}
          {watchedScope === "GLOBAL" ? (
            <Form.Item label="Объект области">
              <Typography.Text code data-testid="access-bindings-scope-anchor-global">
                {CLUSTER_RESOURCE_ID}
              </Typography.Text>
            </Form.Item>
          ) : (
            <Form.Item
              label={scope === "PROJECT" ? "Проект" : "Аккаунт"}
              name="scope_ref_id"
              required
              rules={[{ required: true, message: "Выберите объект области" }]}
            >
              {scope === "PROJECT" ? (
                <Select
                  data-testid="access-bindings-scope-ref"
                  placeholder={
                    headerAccountId ? "Выберите проект" : "Выберите аккаунт в шапке — тогда подгрузятся проекты"
                  }
                  options={anchorSelectOptions}
                  showSearch
                  onSearch={setProjectTerm}
                  {...(ANCHOR_SCOPE.asksServer
                    ? { filterOption: false as const }
                    : { optionFilterProp: "label" as const })}
                  title={ANCHOR_SCOPE.notice}
                  loading={projects.isLoading}
                  // Три разных факта, и путать их нельзя: аккаунт в шапке не
                  // выбран (спрашивать некого) · ответ ещё едет · ответ пуст —
                  // и вот последний обязан назвать свою область.
                  notFoundContent={
                    !headerAccountId
                      ? "Сначала выберите аккаунт в шапке секции"
                      : projects.isLoading
                        ? undefined
                        : ANCHOR_SCOPE.emptyText
                  }
                />
              ) : (
                <Select
                  data-testid="access-bindings-scope-ref"
                  placeholder="Выберите аккаунт"
                  options={anchorSelectOptions}
                  showSearch
                  onSearch={setAccountTerm}
                  {...(ANCHOR_SCOPE.asksServer
                    ? { filterOption: false as const }
                    : { optionFilterProp: "label" as const })}
                  title={ANCHOR_SCOPE.notice}
                  notFoundContent={accounts.isLoading ? undefined : ANCHOR_SCOPE.emptyText}
                  loading={accounts.isLoading}
                />
              )}
            </Form.Item>
          )}

          {watchedScope && (
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 4, marginLeft: 200 }}>
              {SCOPE_TIER_HINT[watchedScope]}
            </Typography.Paragraph>
          )}
        </FormSection>

        {/* ── Секция «Цель (target)» — IAM-1 F8 least-priv spine ── */}
        <FormSection title="Цель">
          <Form.Item
            label="Тип цели"
            name="_target_kind"
            tooltip="allInScope — все объекты под anchor'ом, включая будущие (широкий явный opt-in); resources — только перечисленные объекты (least-privilege)."
          >
            <Radio.Group data-testid="access-bindings-target-kind">
              <Radio.Button value="allInScope">Вся область</Radio.Button>
              <Radio.Button value="resources">Точечно (перечень объектов)</Radio.Button>
            </Radio.Group>
          </Form.Item>
          {watchedTargetKind === "resources" && (
            <Form.Item label="Объекты (ResourceRef)" required>
              <Form.List name="target_resources">
                {(rows, { add, remove }) => (
                  <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                    {rows.map((r) => (
                      <Space key={r.key} align="baseline">
                        <Form.Item name={[r.name, "type"]} rules={[{ required: true, message: "type" }]} noStyle>
                          <Input placeholder="compute.instance" style={{ width: 220 }} data-testid="target-res-type" />
                        </Form.Item>
                        <Form.Item name={[r.name, "id"]} rules={[{ required: true, message: "id" }]} noStyle>
                          <Input placeholder="ins-…" style={{ width: 220 }} data-testid="target-res-id" />
                        </Form.Item>
                        <Button type="text" danger onClick={() => remove(r.name)}>
                          Удалить
                        </Button>
                      </Space>
                    ))}
                    <Button
                      type="dashed"
                      onClick={() => add({ type: "", id: "" })}
                      data-testid="target-res-add"
                      style={{ alignSelf: "flex-start" }}
                    >
                      Добавить объект
                    </Button>
                  </div>
                )}
              </Form.List>
            </Form.Item>
          )}
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0, marginLeft: 200 }}>
            Цель обязательна (минимально необходимые права): «Вся область» — явно выбранная широкая выдача; «Точечно» —
            доступ только на перечисленные объекты (ResourceRef {"{type,id}"}, закрытый список типов).
          </Typography.Paragraph>
        </FormSection>

        {/* ── Секция «Роли» ── */}
        <FormSection title="Роли">
          {globalGuardActive && (
            <Alert
              type="warning"
              showIcon
              data-testid="access-bindings-global-guard"
              style={{ marginBottom: 12 }}
              message="GLOBAL допустим только для роли cluster-admin"
              description={
                <>
                  На область <b>GLOBAL</b> с селектором «все объекты» можно выдать только роль{" "}
                  <Typography.Text code>cluster-admin</Typography.Text> (<Typography.Text code>*.*.*</Typography.Text>).
                  Для обычных ролей на GLOBAL роль обязана задавать селектор по именам или меткам (в правилах роли).
                  Снимите {globalGuardRoles.map((id) => displayName(id)).join(", ")} или выберите область
                  ACCOUNT/PROJECT.
                </>
              }
            />
          )}
          <Form.Item
            label="Роли"
            name="role_ids"
            className="kc-role-formitem"
            required={!reconcile}
            rules={
              reconcile
                ? []
                : [
                    {
                      validator: (_r, value: string[] | undefined) =>
                        value && value.length > 0
                          ? Promise.resolve()
                          : Promise.reject(new Error("Выберите хотя бы одну роль")),
                    },
                  ]
            }
          >
            <Select
              mode="multiple"
              className="kc-role-select"
              data-testid="access-bindings-role-select"
              disabled={!scopeSelected}
              placeholder={scopeSelected ? "Выберите роли" : "Сначала выберите область действия"}
              options={finalRoleOptions}
              optionFilterProp="label"
              tagRender={({ value, closable, onClose }) => (
                <Tag
                  color="blue"
                  closable={closable}
                  onClose={onClose}
                  style={{ marginInlineEnd: 4, whiteSpace: "normal" }}
                >
                  <span className="ant-select-selection-item-content">{displayName(String(value))}</span>
                </Tag>
              )}
              loading={assignableQ.isLoading}
              title={ROLE_SCOPE.notice}
              // Три состояния — три РАЗНЫХ факта, и прежняя редакция сводила два
              // последних в одно: «нет ролей, доступных для этой области» стояло
              // и тогда, когда роли есть, а введённое просто не совпало ни с
              // одной загруженной меткой. Сервер этот список не сужает (см.
              // ROLE_SCOPE), поэтому пустота ввода честно называется областью.
              notFoundContent={
                assignableQ.isLoading
                  ? "Загрузка ролей…"
                  : assignableRoles.length === 0
                    ? "Нет ролей, доступных для этой области"
                    : ROLE_SCOPE.emptyText
              }
              style={{ width: "100%" }}
            />
          </Form.Item>

          {/* Подсказка (Добавить N · Отозвать M / Будет создано). */}
          {reconcile ? (
            addedCount + removedCount > 0 ? (
              <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 12, marginLeft: 200 }}>
                Добавить: {addedCount} · Отозвать: {removedCount}
                <Tag color="blue" style={{ marginLeft: 8 }}>
                  +{addedCount}
                </Tag>
                <Tag color="volcano">−{removedCount}</Tag>
              </Typography.Paragraph>
            ) : (
              <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 12, marginLeft: 200 }}>
                Изменений нет — текущие привилегии области актуальны.
              </Typography.Paragraph>
            )
          ) : selectedRoleIds.length > 0 ? (
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 12, marginLeft: 200 }}>
              Будет создано привязок: {selectedRoleIds.length}{" "}
              <Tag color="blue" style={{ marginLeft: 4 }}>
                {selectedRoleIds.length} {pluralRole(selectedRoleIds.length)}
              </Tag>
            </Typography.Paragraph>
          ) : null}

          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 12, marginLeft: 200 }}>
            {reconcile ? (
              <>
                Актуализация привилегий субъекта на выбранной области: выбор задаёт желаемый набор ролей. Добавленные
                роли будут выданы, снятые — отозваны. Привилегии через группу и на других областях не затрагиваются.
              </>
            ) : (
              <>
                Каждая выбранная роль создаёт отдельную привязку для выбранных субъектов на выбранной области. Какие
                именно объекты затрагивает роль — определяется её правилами (селектор all / по именам / по меткам).
              </>
            )}
          </Typography.Paragraph>
        </FormSection>

        <FormFooter
          submitLabel={reconcile ? "Сохранить привилегии" : "Создать"}
          submitting={submitting}
          submitDisabled={globalGuardActive}
          onSubmit={() => form.submit()}
          onCancel={onCancel}
        />
      </Form>
    </div>
  );
}
