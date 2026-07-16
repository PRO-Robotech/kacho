// AccessBindingCreateForm — тело SCOPE-FIRST формы создания/актуализации привязок
// доступа (AccessBinding) под explicit-RBAC модель.
//
// Модель: грант = subjects[] + role + scope{GLOBAL|ACCOUNT|PROJECT} + scopeRef.
// Scope — first-class измерение (явный селектор «Область действия»), НЕ скрытый
// «тип ресурса». Wire: GLOBAL ≡ tier CLUSTER (anchor cluster_kacho_root);
// ACCOUNT/PROJECT → tier + id. Payload: {subjects[], role_id, scope_ref{tier,id}},
// один POST на роль. Селекторы (all/names/labels) живут в rules РОЛИ (единый
// источник истины) — форма биндинга их НЕ собирает.
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
import { Alert, Form, Select, Tag, Typography } from "antd";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@shared/api/client";
import { FormSection } from "@/components/organisms/form/FormSection";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { useContext } from "@shared/lib/context-store";
import {
  iamApi,
  IAM,
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
 * Маппинг UI-строки SubjectType → имя proto-enum `SubjectType`
 * (`SUBJECT_TYPE_USER` / `SUBJECT_TYPE_SERVICE_ACCOUNT` / `SUBJECT_TYPE_GROUP`).
 *
 * Поле `Subject.type` в proto — enum. grpc-gateway/protojson с
 * `DiscardUnknown:true` ТИХО схлопывает неизвестную enum-строку (нижне-регистровую
 * "user") в `SUBJECT_TYPE_UNSPECIFIED` — backend затем валит её `Illegal argument
 * subject_type ""`. Поэтому на проводе subjects[].type — enum-ИМЯ; нижний регистр —
 * только внутренний UI-тип.
 */
const SUBJECT_TYPE_ENUM: Record<SubjectType, string> = {
  user: "SUBJECT_TYPE_USER",
  service_account: "SUBJECT_TYPE_SERVICE_ACCOUNT",
  group: "SUBJECT_TYPE_GROUP",
};

/** Cluster singleton id для scope=GLOBAL (на проводе tier CLUSTER). */
const CLUSTER_RESOURCE_ID = "cluster_kacho_root";

const SCOPE_TIERS: ScopeTier[] = ["GLOBAL", "ACCOUNT", "PROJECT"];
const SCOPE_TIER_LABEL: Record<ScopeTier, string> = {
  GLOBAL: "GLOBAL",
  ACCOUNT: "ACCOUNT",
  PROJECT: "PROJECT",
};
const SCOPE_TIER_HINT: Record<ScopeTier, string> = {
  GLOBAL: "На весь кластер. Допустим только для роли cluster-admin (*.*.*).",
  ACCOUNT: "Граница материализации — выбранный Account (и его проекты).",
  PROJECT: "Граница материализации — выбранный Project.",
};

/** Wire scope-tier (ScopeRef.tier) из UI scope-измерения. GLOBAL → CLUSTER. */
const WIRE_TIER_BY_SCOPE: Record<ScopeTier, string> = {
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
  ACCOUNT: "Account-роли",
  PROJECT: "Project-роли",
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

export function AccessBindingCreateForm({
  lockedSubject,
  subjectAccountId,
  preset,
  onSuccess,
  onCancel,
}: Props) {
  const qc = useQueryClient();
  const account = useContext((s) => s.account);
  const [form] = Form.useForm();

  const presetSubjectType = lockedSubject?.type ?? preset?.subject_type;
  const presetSubjectId = lockedSubject?.id ?? preset?.subject_id;
  const lockSubject = !!lockedSubject;
  const reconcile = lockSubject;
  const homeAccountId = reconcile ? subjectAccountId ?? null : null;

  // Стартовый scope (scope-first). reconcile → ACCOUNT:<homeAccount>; preset
  // (deep-link) → из resource_type; иначе НЕ выбран (поле «Роли» disabled).
  const presetScope = scopeFromResourceType(preset?.resource_type);
  const initialScope: ScopeTier | undefined = reconcile ? "ACCOUNT" : presetScope ?? undefined;
  // Какой anchor-picker рендерить (для дефолтного случая — account-ветка).
  const initialScopeForPicker: ScopeTier = initialScope ?? "ACCOUNT";
  const initialAnchorId: string | undefined = reconcile
    ? homeAccountId ?? undefined
    : presetScope === "GLOBAL"
      ? CLUSTER_RESOURCE_ID
      : preset?.resource_id ?? undefined;

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
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ── Subject data ──
  const users = useQuery({
    queryKey: ["iam", "users", "list"],
    queryFn: () => iamApi.listUsers({ pageSize: "1000" }),
    staleTime: 30_000,
  });
  const sas = useQuery({
    queryKey: ["iam", "service-accounts", "all"],
    queryFn: async () => {
      const accs = await iamApi.listAccounts({ pageSize: "1000" });
      const all: ServiceAccount[] = [];
      for (const a of accs.accounts) {
        const r = await iamApi.listServiceAccounts({
          account_id: a.id,
          pageSize: "1000",
        });
        all.push(...(r.service_accounts ?? []));
      }
      return all;
    },
    enabled: subjectType === "service_account",
    staleTime: 30_000,
  });
  const groups = useQuery({
    queryKey: ["iam", "groups", "all"],
    queryFn: async () => {
      const accs = await iamApi.listAccounts({ pageSize: "1000" });
      const all: Group[] = [];
      for (const a of accs.accounts) {
        const r = await iamApi.listGroups({ account_id: a.id, pageSize: "1000" });
        all.push(...(r.groups ?? []));
      }
      return all;
    },
    enabled: subjectType === "group",
    staleTime: 30_000,
  });

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
  const headerAccountId = account?.id ?? "";
  const accounts = useQuery({
    queryKey: ["iam", "accounts", "list"],
    queryFn: () => iamApi.listAccounts({ pageSize: "1000" }),
    enabled: scope === "ACCOUNT",
    staleTime: 30_000,
  });
  const projects = useQuery({
    queryKey: ["iam", "projects", "by-account", headerAccountId],
    queryFn: () =>
      iamApi.listProjects(
        headerAccountId ? { account_id: headerAccountId, pageSize: "1000" } : { pageSize: "1000" },
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
  const watchedScopeRefId = Form.useWatch("scope_ref_id", form) as string | undefined;
  // GLOBAL: anchor фиксирован (singleton) — не зависит от поля scope_ref_id.
  const watchedAnchorId = watchedScope === "GLOBAL" ? CLUSTER_RESOURCE_ID : watchedScopeRefId;

  // Scope «выбран» (для GLOBAL anchor фиксирован — singleton). До выбора поле
  // «Роли» disabled и assignable не фетчится.
  const scopeSelected = !!watchedScope && !!watchedAnchorId;

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

  const displayName = (roleId: string): string =>
    roleNameById.get(roleId) ?? privRoleName.get(roleId) ?? roleId;

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
    // canonical scope_ref {tier, id}. GLOBAL → tier CLUSTER + singleton.
    const scopeRef = {
      tier: WIRE_TIER_BY_SCOPE[uiScope],
      id: anchorId,
    };

    // thin binding несёт subjects[] (1..32). standalone → multi-subject из формы;
    // reconcile → один залоченный субъект.
    const subjectIds = reconcile
      ? [v.subject_id as string].filter(Boolean)
      : ((v.subject_ids as string[] | undefined) ?? []).filter(Boolean);
    const subjects: Subject[] = subjectIds.map((id) => ({
      // proto enum wire-form: enum-имя, не нижне-регистровая строка.
      type: SUBJECT_TYPE_ENUM[subjectType] as Subject["type"],
      id,
    }));
    const baseBody = {
      subjects,
      scope_ref: scopeRef,
    };

    const added = roleIds.filter((id) => !currentRoleIds.includes(id));
    const removed = reconcile ? currentRoleIds.filter((id) => !roleIds.includes(id)) : [];

    if (added.length === 0 && removed.length === 0) {
      if (reconcile) onSuccess();
      return;
    }

    setSubmitting(true);

    type Op = { kind: "add" | "remove"; roleId: string; promise: Promise<unknown> };
    const ops: Op[] = [
      ...added.map((roleId) => ({
        kind: "add" as const,
        roleId,
        promise: api.create(IAM.accessBindings, { ...baseBody, role_id: roleId }),
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
                options={subjectOptions}
                showSearch
                optionFilterProp="label"
                loading={users.isLoading || sas.isLoading || groups.isLoading}
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
                options={subjectOptions}
                showSearch
                optionFilterProp="label"
                maxCount={32}
                loading={users.isLoading || sas.isLoading || groups.isLoading}
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
              label={scope === "PROJECT" ? "Project" : "Account"}
              name="scope_ref_id"
              required
              rules={[{ required: true, message: "Выберите объект области" }]}
            >
              {scope === "PROJECT" ? (
                <Select
                  data-testid="access-bindings-scope-ref"
                  placeholder={
                    headerAccountId
                      ? "Выберите Project"
                      : "Выберите Account в шапке — тогда подгрузятся проекты"
                  }
                  options={projectOptions}
                  showSearch
                  optionFilterProp="label"
                  loading={projects.isLoading}
                  notFoundContent={headerAccountId ? undefined : "Сначала выберите Account в шапке секции"}
                />
              ) : (
                <Select
                  data-testid="access-bindings-scope-ref"
                  placeholder="Выберите Account"
                  options={accountOptions}
                  showSearch
                  optionFilterProp="label"
                  loading={accounts.isLoading}
                />
              )}
            </Form.Item>
          )}

          {watchedScope && (
            <Typography.Paragraph
              type="secondary"
              style={{ fontSize: 12, marginBottom: 4, marginLeft: 200 }}
            >
              {SCOPE_TIER_HINT[watchedScope]}
            </Typography.Paragraph>
          )}
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
                  <Typography.Text code>cluster-admin</Typography.Text>{" "}
                  (<Typography.Text code>*.*.*</Typography.Text>). Для обычных ролей на GLOBAL роль
                  обязана задавать селектор по именам или меткам (в правилах роли). Снимите{" "}
                  {globalGuardRoles.map((id) => displayName(id)).join(", ")} или выберите область
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
              notFoundContent={
                assignableQ.isLoading ? "Загрузка ролей…" : "Нет ролей, доступных для этой области"
              }
              style={{ width: "100%" }}
            />
          </Form.Item>

          {/* Подсказка (Добавить N · Отозвать M / Будет создано). */}
          {reconcile ? (
            addedCount + removedCount > 0 ? (
              <Typography.Paragraph
                type="secondary"
                style={{ fontSize: 12, marginBottom: 12, marginLeft: 200 }}
              >
                Добавить: {addedCount} · Отозвать: {removedCount}
                <Tag color="blue" style={{ marginLeft: 8 }}>
                  +{addedCount}
                </Tag>
                <Tag color="volcano">−{removedCount}</Tag>
              </Typography.Paragraph>
            ) : (
              <Typography.Paragraph
                type="secondary"
                style={{ fontSize: 12, marginBottom: 12, marginLeft: 200 }}
              >
                Изменений нет — текущие привилегии области актуальны.
              </Typography.Paragraph>
            )
          ) : selectedRoleIds.length > 0 ? (
            <Typography.Paragraph
              type="secondary"
              style={{ fontSize: 12, marginBottom: 12, marginLeft: 200 }}
            >
              Будет создано привязок: {selectedRoleIds.length}{" "}
              <Tag color="blue" style={{ marginLeft: 4 }}>
                {selectedRoleIds.length} {pluralRole(selectedRoleIds.length)}
              </Tag>
            </Typography.Paragraph>
          ) : null}

          <Typography.Paragraph
            type="secondary"
            style={{ fontSize: 12, marginBottom: 12, marginLeft: 200 }}
          >
            {reconcile ? (
              <>
                Актуализация привилегий субъекта на выбранной области: выбор задаёт желаемый набор
                ролей. Добавленные роли будут выданы, снятые — отозваны. Привилегии через группу и на
                других областях не затрагиваются.
              </>
            ) : (
              <>
                Каждая выбранная роль создаёт отдельную привязку для выбранных субъектов на выбранной
                области. Какие именно объекты затрагивает роль — определяется её правилами (селектор all
                / по именам / по меткам).
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
