// TokenIssuancePage — generic Stage 4 страница выпуска credential'ов
// (SA-ключи / персональные токены пользователя). Конфигурируется TokenKindConfig
// (см. ServiceAccountKeysPage / UserTokensPage).
//
// Flow:
//   1. Выбрать субъект (ServiceAccount / User) — Select со списком + fallback
//      ручной ввод id (list SA требует account_id; глобальный админ может не
//      иметь его под рукой).
//   2. Список существующих credential'ов субъекта (id / описание / создан /
//      истекает / посл. использование) + Revoke per-row.
//   3. «Выпустить» → форма (ВИД + описание + срок) → POST Issue → Operation.
//      Одноразовое значение читается ПО ВИДУ:
//        * SECRET  — выдача завершается на пути запроса, и секрет живёт ТОЛЬКО
//          в теле немедленного ответа: строка операции его не несёт ни в какой
//          момент, поэтому опрос вернул бы тело БЕЗ секрета;
//        * KEYPAIR — асинхронный путь, значение приезжает опросом
//          GET /operations/{id} до done.
//      → OneTimeSecretModal (показать ОДИН раз).
//
// required_acr_min="2": без свежего step-up api-gateway вернёт 401/403 — ловим и
// показываем friendly step-up notice (полноценный replay через StepUpModal не
// подключён к shared api-client; здесь — явное сообщение + подсказка).

import { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Spin,
  Table,
  Tooltip,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import type { FormInstance } from "antd";
import { DeleteOutlined, KeyOutlined, PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import type { Operation } from "@shared/api/types";
import { issuedCredentialFromOperation, type IssuedCredential, type IssueTokenBody } from "@shared/api/tokens";
import {
  CREDENTIAL_KIND_KEYPAIR,
  CREDENTIAL_KIND_SECRET,
  MAX_TTL_SECONDS,
  SECRET_RADIUS_NOTICE,
  SECRET_TTL_CEILING_SECONDS,
  SECRET_TTL_DEFAULT_DAYS,
  credentialKindLabel,
  maxTtlSecondsFor,
  type IssuableCredentialKind,
} from "@shared/lib/tokens-util";
import { OneTimeSecretModal } from "@shared/components/organisms/system/OneTimeSecretModal";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { CopyableMonoId, fmtTs } from "@shared/components/organisms/iam/IamCommon";
import { useAuth } from "@shared/contexts/AuthContext";
import { useOperation } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";
import { resolveMutationResponse } from "@shared/lib/operation-outcome";
import { getResource } from "@shared/lib/resource-registry";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScope, pickerScopeOfSpec } from "@shared/lib/picker-search";
import { useKeptLabels } from "@shared/lib/kept-choice";

/** Унифицированная строка credential'а (общая форма SAKey и UserToken). */
export interface CredentialRow {
  id: string;
  description?: string;
  created_at?: string;
  expires_at?: string;
  last_used_at?: string;
  /** Вид удостоверения. Край, который о видах не говорит, оставляет его пустым. */
  credential_kind?: string;
}

/** Опция выбора субъекта (ServiceAccount / User). */
export interface SubjectOption {
  value: string;
  label: string;
}

export interface TokenKindConfig {
  /** Discriminator для query-ключей. */
  kind: "sa" | "user";
  /** «сервисный аккаунт» / «пользователь». */
  subjectSingular: string;
  /** «Сервисный аккаунт» / «Пользователь» (для label поля). */
  subjectLabel: string;
  /** «ключ» / «токен». */
  credentialSingular: string;
  /** «Ключи» / «Токены». */
  credentialPlural: string;
  /** Заголовок one-time модалки. */
  issuedTitle: string;
  /**
   * Загрузка списка субъектов (best-effort).
   *
   * `query` — параметры сужения, собранные областью поиска поля (#528):
   * реализация ОБЯЗАНА донести их до края. Реализация, объявленная БЕЗ
   * параметра, тем самым говорит, что сужать не умеет, — и поле не станет
   * утверждать обратное (см. `subjectScope`).
   */
  listSubjects: (query?: Record<string, string>) => Promise<SubjectOption[]>;
  /** Загрузка credential'ов субъекта. */
  listCredentials: (subjectId: string) => Promise<CredentialRow[]>;
  /** POST issue → Operation. */
  issue: (subjectId: string, body: IssueTokenBody) => Promise<{ operation: Operation }>;
  /** DELETE revoke → Operation. */
  revoke: (subjectId: string, credentialId: string) => Promise<{ operation: Operation }>;
}

/** step-up (required_acr_min) — эвристика: 401/403 либо ACR/MFA/step в тексте. */
function isStepUpError(err: unknown): boolean {
  if (!(err instanceof ApiError)) return false;
  if (err.status === 401 || err.status === 403) return true;
  const hay = err.message.toLowerCase();
  return ["acr", "step-up", "step up", "stepup", "mfa", "assurance", "aal2"].some((n) => hay.includes(n));
}

// Способ входа назван ТЕМ ЖЕ словом, что и в окне подтверждения
// (`StepUpModal`): один предмет — одно имя. Прежняя редакция говорила
// «passkey (Touch ID / Windows Hello / security key)», тогда как окно рядом
// говорит «ключом доступа … (аппаратный ключ)» — то самое расхождение, ради
// которого заведён гейт языка.
const STEP_UP_MESSAGE =
  "Действие требует усиленной аутентификации (step-up MFA, ACR≥2). Подтвердите вход ключом доступа (Touch ID / Windows Hello / аппаратный ключ) и повторите выпуск.";

/**
 * Ресурс субъекта в реестре — чтобы область поиска читалась ОТТУДА.
 *
 * Чем владелец умеет сужать список, объявлено один раз в реестре и сверено с
 * его деревом (`lib/list-server-search-parity.test.ts`): у пользователя имени
 * нет вовсе (его знают по почте), у сервисного аккаунта — есть. Переписывать
 * это здесь значило бы завести второе место об одном предмете, из которых
 * верно одно.
 */
const SUBJECT_SPEC_ID: Record<TokenKindConfig["kind"], string> = { sa: "service-accounts", user: "users" };

export function TokenIssuancePage({ config }: { config: TokenKindConfig }) {
  const qc = useQueryClient();
  const { user } = useAuth();
  const createdByUserId = user?.id ?? "";

  const [subjectId, setSubjectId] = useState<string>("");
  const [issueOpen, setIssueOpen] = useState(false);
  const [issueOpId, setIssueOpId] = useState<string | null>(null);
  const [revokeOpId, setRevokeOpId] = useState<string | null>(null);
  const [revokingId, setRevokingId] = useState<string | null>(null);
  const [issued, setIssued] = useState<IssuedCredential | null>(null);
  const [stepUpNotice, setStepUpNotice] = useState<string | null>(null);
  // Вид живёт в состоянии страницы, а не в форме: от него зависят и потолок
  // срока, и текст рядом с ним, и предупреждение о радиусе — то есть форма
  // ПЕРЕРИСОВЫВАЕТСЯ от его смены, а не только читается на отправке.
  const [kind, setKind] = useState<IssuableCredentialKind>(CREDENTIAL_KIND_SECRET);
  const [form] = Form.useForm<{ description?: string; ttl_seconds?: number }>();

  // ---- Субъекты (best-effort) ----
  //
  // Область поиска субъекта (#528). Ввод уходит запросом при ДВУХ условиях
  // сразу: владелец умеет сужать (объявление реестра) И получатель списка
  // объявил параметр под запрос. Второе — не педантизм: `listSubjects` без
  // параметра молча выбросит переданное, и поле утверждало бы «искали по всему
  // списку», не спросив никого, — ровно тот класс, ради которого поле и
  // правится. Арность передачу не доказывает, но объявить параметр и не
  // воспользоваться им здесь нельзя: `noUnusedParameters` не даст собраться.
  //
  // Пока страница списка не сужает, поле говорит правду о своей области —
  // «нет среди загруженных», — вместо «Ничего не найдено», которое стояло здесь
  // и утверждало отсутствие пользователя, чью почту никто не спрашивал.
  const subjectScope =
    config.listSubjects.length > 0
      ? pickerScopeOfSpec(getResource(SUBJECT_SPEC_ID[config.kind]))
      : pickerScope(undefined);
  const [subjectTerm, setSubjectTerm] = useState("");
  const debouncedSubjectTerm = useDebouncedValue(subjectTerm, subjectScope.asksServer ? 250 : 0);
  const subjectServerQuery = subjectScope.asksServer ? subjectScope.query(debouncedSubjectTerm) : {};
  // Ключ запроса несёт ввод ТОЛЬКО когда сужает сервер: иначе каждое нажатие
  // клавиши сбрасывало бы кэш и перечитывало один и тот же список.
  const subjectTermKey = subjectScope.asksServer ? (subjectServerQuery.filter ?? "") : "";

  const subjectsQ = useQuery({
    queryKey: [config.kind, "token-subjects", subjectTermKey],
    queryFn: () => config.listSubjects(subjectServerQuery),
    retry: false,
    staleTime: 30_000,
  });
  const subjectOptions = subjectsQ.data ?? [];

  // Метка выбранного субъекта обязана пережить сужение: сервер отвечает по
  // ВВОДУ, и уже выбранный субъект в этот ответ попадать не обязан. Без
  // запоминания заголовок окна выпуска («Выпустить ключ для «…»») и сам
  // селектор показали бы сырой идентификатор вместо почты или имени.
  // Зависимость — ОТВЕТ запроса, а не выражение `?? []`: у пустого литерала
  // каждый рендер своя идентичность, и пересчёт шёл бы всегда.
  const seenSubjects = useMemo(
    () => (subjectsQ.data ?? []).map((o) => [o.value, o.label] as const),
    [subjectsQ.data],
  );
  const subjectLabelOf = useKeptLabels(seenSubjects);
  const currentSubjectLabel = subjectLabelOf(subjectId);
  const keptSubject =
    subjectId && !subjectOptions.some((o) => o.value === subjectId) && currentSubjectLabel !== subjectId
      ? [{ value: subjectId, label: currentSubjectLabel }]
      : [];

  // ---- Credential'ы выбранного субъекта ----
  const credsQ = useQuery({
    queryKey: [config.kind, "credentials", subjectId],
    queryFn: () => config.listCredentials(subjectId),
    enabled: !!subjectId,
    retry: (failureCount, error) => {
      if (error instanceof ApiError && (error.status === 401 || error.status === 403)) return false;
      return failureCount < 1;
    },
    staleTime: 5_000,
  });
  const creds = credsQ.data ?? [];

  const invalidateCreds = () => qc.invalidateQueries({ queryKey: [config.kind, "credentials", subjectId] });

  // ---- Issue ----
  const issueMut = useMutation({
    mutationFn: (body: IssueTokenBody) => config.issue(subjectId, body),
    onSuccess: (resp) => {
      // СНАЧАЛА читаем НЕМЕДЛЕННЫЙ ответ, и только потом заводим опрос.
      //
      // У вида SECRET выдача завершается на пути запроса, а секрет подменяется
      // в теле ответа ПОСЛЕ записи строки: сама строка операции его не несёт
      // ни в какой момент. Опрос вернул бы тело без секрета — то есть при
      // исправной выдаче невосстановимое значение было бы потеряно, а на экране
      // появилось бы «Операция завершена, но секрет не получен».
      const immediate = issuedCredentialFromOperation(resp as unknown as Operation);
      if (immediate) {
        setIssued(immediate);
        setIssueOpen(false);
        form.resetFields();
        toast.success(`${cap(config.credentialSingular)} выпущен`);
        void invalidateCreds();
        return;
      }
      // Единственная из четырёх точек, что уже отказывалась считать «нет
      // операции» успехом; общий разбор нужен ей ради ВТОРОЙ формы конверта —
      // край отдаёт Operation верхним уровнем, и по вложенному ключу выпуск
      // валился в ложный отказ.
      const resolved = resolveMutationResponse(resp, true);
      if (resolved.kind === "operation") {
        setIssueOpId(resolved.opId);
      } else {
        // Объединение обеих линий: общий разбор из ствола (он знает обе формы
        // конверта) плюс конкретика этой ветки — «выпуск», а не безличное
        // «ответ». Выбрать одну сторону значило бы потерять либо разбор, либо
        // то, что сообщение называет ДЕЙСТВИЕ, о котором говорит.
        toast.error(
          resolved.kind === "violation"
            ? "Сервер не вернул операцию — подтвердить выпуск невозможно"
            : "Ответ без операции",
        );
      }
    },
    onError: (err) => {
      if (isStepUpError(err)) {
        setStepUpNotice(STEP_UP_MESSAGE);
      } else {
        toast.error(err instanceof Error ? err.message : "Не удалось выпустить");
      }
    },
  });

  // Poll issue-operation → на done читаем one-time секрет из response.
  const { data: issueOp } = useOperation(issueOpId);
  useEffect(() => {
    if (!issueOp?.done || !issueOpId) return;
    if (issueOp.error) {
      if (issueOp.error.code === 9 /* FAILED_PRECONDITION */ || issueOp.error.code === 7 /* PERMISSION_DENIED */) {
        // step-up мог отразиться и в async-ошибке.
        setStepUpNotice(STEP_UP_MESSAGE);
      }
      toast.error(issueOp.error.message || "Выпуск не удался");
    } else {
      const cred = issuedCredentialFromOperation(issueOp);
      if (cred) {
        setIssued(cred);
        setIssueOpen(false);
        form.resetFields();
        toast.success(`${cap(config.credentialSingular)} выпущен`);
        void invalidateCreds();
      } else {
        toast.error("Операция завершена, но секрет не получен");
      }
    }
    setIssueOpId(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [issueOp?.done, issueOp?.error, issueOpId]);

  // ---- Revoke ----
  const { data: revokeOp } = useOperation(revokeOpId);
  useEffect(() => {
    if (!revokeOp?.done || !revokeOpId) return;
    if (revokeOp.error) {
      toast.error(revokeOp.error.message || "Не удалось отозвать");
    } else {
      toast.success(`${cap(config.credentialSingular)} отозван`);
      void invalidateCreds();
    }
    setRevokeOpId(null);
    setRevokingId(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [revokeOp?.done, revokeOp?.error, revokeOpId]);

  const handleRevoke = async (row: CredentialRow) => {
    setRevokingId(row.id);
    try {
      const resp = await config.revoke(subjectId, row.id);
      const resolved = resolveMutationResponse(resp, true);
      if (resolved.kind === "operation") {
        setRevokeOpId(resolved.opId);
      } else {
        toast.error(resolved.kind === "violation" ? resolved.message : "Ответ без операции");
        setRevokingId(null);
      }
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : e instanceof Error ? e.message : "Ошибка");
      setRevokingId(null);
    }
  };

  const submitIssue = () => {
    setStepUpNotice(null);
    form
      .validateFields()
      .then((vals) => {
        issueMut.mutate({
          description: vals.description?.trim() || undefined,
          ttl_seconds: vals.ttl_seconds && vals.ttl_seconds > 0 ? vals.ttl_seconds : undefined,
          created_by_user_id: createdByUserId,
          // Вид называет КОНСОЛЬ, а не умолчание сервера: не названный вид
          // сервер разрешает прежним поведением и выпускает ключевую пару —
          // ровно тот вид, который докерная полоса больше не принимает.
          credential_kind: kind,
        });
      })
      .catch(() => {
        /* validation errors — уже показаны формой */
      });
  };

  const issuing = issueMut.isPending || issueOpId !== null;

  const columns: ColumnsType<CredentialRow> = [
    {
      title: "Идентификатор",
      dataIndex: "id",
      key: "id",
      width: 220,
      render: (v: string) => <CopyableMonoId id={v} />,
    },
    {
      title: "Описание",
      dataIndex: "description",
      key: "description",
      render: (v?: string) => v || <Typography.Text type="secondary">—</Typography.Text>,
    },
    { title: "Создан", dataIndex: "created_at", key: "created_at", width: 170, render: (v?: string) => fmtTs(v) },
    {
      title: "Истекает",
      dataIndex: "expires_at",
      key: "expires_at",
      width: 170,
      // Пустой срок означает РАЗНОЕ у разных видов: у ключевой пары «бессрочно»,
      // у секрета такой строки не бывает вовсе. Слово подставляется по виду
      // СТРОКИ, а не одно на всех.
      render: (_v: unknown, row: CredentialRow) =>
        row.expires_at ? (
          fmtTs(row.expires_at)
        ) : (
          <Typography.Text type="secondary">
            {row.credential_kind === CREDENTIAL_KIND_SECRET ? "срок не получен" : "бессрочный"}
          </Typography.Text>
        ),
    },
    {
      title: "Посл. использование",
      dataIndex: "last_used_at",
      key: "last_used_at",
      width: 170,
      render: (v?: string) => (v ? fmtTs(v) : <Typography.Text type="secondary">—</Typography.Text>),
    },
    {
      title: "",
      key: "actions",
      width: 60,
      render: (_v, row) => (
        <Popconfirm
          title={`Отозвать ${config.credentialSingular}?`}
          description="Credential перестанет работать немедленно и необратимо."
          okText="Отозвать"
          okButtonProps={{ danger: true }}
          cancelText="Отмена"
          onConfirm={() => void handleRevoke(row)}
        >
          <Button
            size="small"
            type="text"
            danger
            icon={<DeleteOutlined />}
            loading={revokingId === row.id}
            data-testid={`token-revoke-${row.id}`}
          />
        </Popconfirm>
      ),
    },
  ];

  return (
    // Своего заголовка страница НЕ печатает: её называет рейл раздела и шапка
    // общей оболочки — до #447 имя стояло на экране дважды, а под ним висел
    // абзац о внутреннем устройстве выпуска. Единственный факт того абзаца —
    // секрет показывается один раз — сказан в окне выпуска, где он и нужен.
    <Space direction="vertical" size={16} style={{ width: "100%" }} data-testid={`token-page-${config.kind}`}>
      <Space size={8} wrap style={{ width: "100%" }}>
        <Select
          showSearch
          style={{ minWidth: 360 }}
          placeholder={`Выберите ${config.subjectSingular}`}
          value={subjectId || undefined}
          onChange={(v) => setSubjectId(v)}
          onSearch={setSubjectTerm}
          loading={subjectsQ.isLoading}
          options={[...keptSubject, ...subjectOptions]}
          title={subjectScope.notice}
          // Сузил сервер — клиент НЕ пересеивает: метка субъекта склеена из
          // имени и идентификатора, и повторное сужение по ней вычло бы из
          // ответа края строки, которые он прислал именно по этому вводу.
          {...(subjectScope.asksServer ? { filterOption: false as const } : { optionFilterProp: "label" as const })}
          // Пустой ответ обязан называть свою ОБЛАСТЬ. Отказ края — отдельный
          // случай: там списка нет вовсе, и сказать надо про ручной ввод.
          notFoundContent={
            subjectsQ.isError
              ? "Список недоступен — введите ID вручную ниже"
              : subjectsQ.isLoading
                ? undefined
                : subjectScope.emptyText
          }
          data-testid="token-subject-select"
        />
        <Input
          allowClear
          style={{ width: 260 }}
          placeholder={`…или ID ${config.subjectSingular} вручную`}
          value={subjectId}
          onChange={(e) => setSubjectId(e.target.value.trim())}
          data-testid="token-subject-input"
        />
        <Button
          type="primary"
          icon={<PlusOutlined />}
          disabled={!subjectId}
          onClick={() => {
            setStepUpNotice(null);
            setIssueOpen(true);
          }}
          data-testid="token-issue-button"
        >
          Выпустить {config.credentialSingular}
        </Button>
        <Tooltip title="Обновить список">
          <Button icon={<ReloadOutlined />} disabled={!subjectId} onClick={() => void invalidateCreds()} />
        </Tooltip>
      </Space>

      {config.kind === "sa" && subjectsQ.isError && (
        <Alert
          type="info"
          showIcon
          message="Список сервисных аккаунтов недоступен, пока не выбран аккаунт"
          description="Введите ID сервисного аккаунта вручную в поле выше — выпуск и список ключей работают по прямому ID."
        />
      )}

      {stepUpNotice && (
        <Alert
          type="warning"
          showIcon
          closable
          onClose={() => setStepUpNotice(null)}
          message="Требуется усиленная аутентификация"
          description={stepUpNotice}
          data-testid="token-stepup-notice"
        />
      )}

      {!createdByUserId && (
        <Alert
          type="warning"
          showIcon
          message="Не определён текущий пользователь"
          description="Выпуск требует выполненного входа. Войдите и повторите."
        />
      )}

      {!subjectId ? (
        <Alert
          type="info"
          showIcon
          message={`Выберите ${config.subjectSingular}, чтобы увидеть ${config.credentialPlural.toLowerCase()}`}
        />
      ) : credsQ.isLoading ? (
        <div style={{ padding: 32, textAlign: "center" }}>
          <Spin />
        </div>
      ) : credsQ.isError ? (
        <ErrorResult error={credsQ.error} />
      ) : (
        <Table<CredentialRow>
          rowKey="id"
          size="small"
          columns={columns}
          dataSource={creds}
          pagination={false}
          loading={credsQ.isFetching && creds.length === 0}
          locale={{ emptyText: `${config.credentialPlural} не выпущены.` }}
        />
      )}

      <IssueModal
        open={issueOpen}
        title={`Выпустить ${config.credentialSingular} для «${currentSubjectLabel}»`}
        form={form}
        issuing={issuing}
        stepUpNotice={stepUpNotice}
        kind={kind}
        onKindChange={setKind}
        onCancel={() => setIssueOpen(false)}
        onSubmit={submitIssue}
      />

      <OneTimeSecretModal
        open={issued !== null}
        credential={issued}
        title={config.issuedTitle}
        subjectLabel={currentSubjectLabel}
        onClose={() => setIssued(null)}
      />
    </Space>
  );
}

function IssueModal({
  open,
  title,
  form,
  issuing,
  stepUpNotice,
  kind,
  onKindChange,
  onCancel,
  onSubmit,
}: {
  open: boolean;
  title: string;
  form: FormInstance<{ description?: string; ttl_seconds?: number }>;
  issuing: boolean;
  stepUpNotice: string | null;
  kind: IssuableCredentialKind;
  onKindChange: (k: IssuableCredentialKind) => void;
  onCancel: () => void;
  onSubmit: () => void;
}) {
  const isSecret = kind === CREDENTIAL_KIND_SECRET;
  const maxTtl = maxTtlSecondsFor(kind);
  return (
    <Modal
      open={open}
      title={
        <Space>
          <KeyOutlined />
          {title}
        </Space>
      }
      okText="Выпустить"
      cancelText="Отмена"
      confirmLoading={issuing}
      onOk={onSubmit}
      onCancel={onCancel}
      maskClosable={!issuing}
      data-testid="token-issue-modal"
    >
      <Form form={form} layout="vertical" preserve={false}>
        <Form.Item label="Вид удостоверения">
          <Segmented
            value={kind}
            onChange={(v) => onKindChange(v)}
            options={[
              { label: credentialKindLabel(CREDENTIAL_KIND_SECRET), value: CREDENTIAL_KIND_SECRET },
              { label: credentialKindLabel(CREDENTIAL_KIND_KEYPAIR), value: CREDENTIAL_KIND_KEYPAIR },
            ]}
          />
        </Form.Item>
        {/* Радиус называется В ОКНЕ ВЫДАЧИ, а не оставляется умолчанием: секрет
            предъявительский, сужения по адресатам у его полосы нет, и «утёк
            секрет сборочного конвейера» означает не доступ к реестру, а всё,
            что может учётная запись. */}
        {isSecret && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            message="Что открывает этот секрет"
            description={SECRET_RADIUS_NOTICE}
          />
        )}
        <Form.Item name="description" label="Описание" rules={[{ max: 256, message: "Не более 256 символов" }]}>
          <Input placeholder="Например: CI runner prod" maxLength={256} />
        </Form.Item>
        <Form.Item
          name="ttl_seconds"
          label="Срок действия (секунды)"
          // Смысл нуля ЗАВИСИТ ОТ ВИДА, и подсказка обязана говорить о том виде,
          // который выбран: одна фраза на оба означала бы, что о ней не спросили
          // ни у одного.
          tooltip={
            isSecret
              ? `Бессрочного секрета не бывает. Пусто или 0 — платформа поставит ${SECRET_TTL_DEFAULT_DAYS} дней; максимум ${SECRET_TTL_CEILING_SECONDS} (90 дней).`
              : `Пусто или 0 — бессрочный. Максимум ${MAX_TTL_SECONDS} (2 года).`
          }
          rules={[{ type: "number", min: 0, max: maxTtl, message: `0…${maxTtl}` }]}
        >
          <InputNumber
            style={{ width: "100%" }}
            min={0}
            max={maxTtl}
            placeholder={isSecret ? `${SECRET_TTL_DEFAULT_DAYS} дней по умолчанию` : "бессрочный"}
          />
        </Form.Item>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0, fontSize: 12 }}>
          {isSecret
            ? `Срок обязателен: бессрочного секрета не бывает. Не назовёте — платформа поставит ${SECRET_TTL_DEFAULT_DAYS} дней, дольше 90 дней выпустить нельзя.`
            : `Пусто или 0 — ключ бессрочный: действует, пока его не отзовут. Максимум ${MAX_TTL_SECONDS} секунд (2 года).`}
        </Typography.Paragraph>
        {stepUpNotice && <Alert type="warning" showIcon message={stepUpNotice} style={{ marginTop: 4 }} />}
      </Form>
    </Modal>
  );
}

function cap(s: string): string {
  return s.length ? s[0].toUpperCase() + s.slice(1) : s;
}
