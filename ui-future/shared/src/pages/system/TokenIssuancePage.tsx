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
//   3. «Выпустить» → форма (описание + опц. TTL) → POST Issue → Operation →
//      poll GET /operations/{id} до done → Operation.response несёт one-time
//      private_key_pem → OneTimeSecretModal (показать ОДИН раз).
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
import { OneTimeSecretModal } from "@shared/components/organisms/system/OneTimeSecretModal";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { CopyableMonoId, fmtTs } from "@shared/components/organisms/iam/IamCommon";
import { useAuth } from "@shared/contexts/AuthContext";
import { useOperation } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";

/** Унифицированная строка credential'а (общая форма SAKey и UserToken). */
export interface CredentialRow {
  id: string;
  description?: string;
  created_at?: string;
  expires_at?: string;
  last_used_at?: string;
}

/** Опция выбора субъекта (ServiceAccount / User). */
export interface SubjectOption {
  value: string;
  label: string;
}

export interface TokenKindConfig {
  /** Discriminator для query-ключей. */
  kind: "sa" | "user";
  /** Заголовок страницы. */
  pageTitle: string;
  /** Подзаголовок страницы. */
  pageSubtitle: string;
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
  /** Загрузка списка субъектов (best-effort). */
  listSubjects: () => Promise<SubjectOption[]>;
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
  const hay = `${err.code} ${err.message}`.toLowerCase();
  return ["acr", "step-up", "step up", "stepup", "mfa", "assurance", "aal2"].some((n) => hay.includes(n));
}

const STEP_UP_MESSAGE =
  "Действие требует усиленной аутентификации (step-up MFA, ACR≥2). Подтвердите вход через passkey (Touch ID / Windows Hello / security key) и повторите выпуск.";

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
  const [form] = Form.useForm<{ description?: string; ttl_seconds?: number }>();

  // ---- Субъекты (best-effort) ----
  const subjectsQ = useQuery({
    queryKey: [config.kind, "token-subjects"],
    queryFn: config.listSubjects,
    retry: false,
    staleTime: 30_000,
  });
  const subjectOptions = subjectsQ.data ?? [];
  const subjectLabelById = useMemo(() => {
    const m = new Map<string, string>();
    for (const o of subjectOptions) m.set(o.value, o.label);
    return m;
  }, [subjectOptions]);
  const currentSubjectLabel = subjectLabelById.get(subjectId) ?? subjectId;

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
      const opId = resp.operation?.id;
      if (opId) {
        setIssueOpId(opId);
      } else {
        toast.error("Backend не вернул operation id");
      }
    },
    onError: (err) => {
      if (isStepUpError(err)) {
        setStepUpNotice(STEP_UP_MESSAGE);
      } else {
        toast.error(err instanceof Error ? err.message : "Не удалось выпустить credential");
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
        invalidateCreds();
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
      invalidateCreds();
    }
    setRevokeOpId(null);
    setRevokingId(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [revokeOp?.done, revokeOp?.error, revokeOpId]);

  const handleRevoke = async (row: CredentialRow) => {
    setRevokingId(row.id);
    try {
      const resp = await config.revoke(subjectId, row.id);
      const opId = resp.operation?.id;
      if (opId) setRevokeOpId(opId);
      else {
        invalidateCreds();
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
      render: (v?: string) => (v ? fmtTs(v) : <Typography.Text type="secondary">бессрочный</Typography.Text>),
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
    <Space direction="vertical" size={16} style={{ width: "100%" }} data-testid={`token-page-${config.kind}`}>
      <div>
        <Typography.Title level={3} style={{ margin: 0 }}>
          {config.pageTitle}
        </Typography.Title>
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          {config.pageSubtitle}
        </Typography.Text>
      </div>

      <Space size={8} wrap style={{ width: "100%" }}>
        <Select
          showSearch
          style={{ minWidth: 360 }}
          placeholder={`Выберите ${config.subjectSingular}`}
          value={subjectId || undefined}
          onChange={(v) => setSubjectId(v)}
          loading={subjectsQ.isLoading}
          options={subjectOptions}
          optionFilterProp="label"
          notFoundContent={subjectsQ.isError ? "Список недоступен — введите ID вручную ниже" : "Ничего не найдено"}
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
          message="Список сервисных аккаунтов недоступен без выбранного Account"
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
          description="Выпуск требует авторизованной сессии (created_by_user_id). Войдите, чтобы выпускать credential'ы."
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
  onCancel,
  onSubmit,
}: {
  open: boolean;
  title: string;
  form: FormInstance<{ description?: string; ttl_seconds?: number }>;
  issuing: boolean;
  stepUpNotice: string | null;
  onCancel: () => void;
  onSubmit: () => void;
}) {
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
        <Form.Item
          name="description"
          label="Описание"
          rules={[{ max: 256, message: "Не более 256 символов" }]}
        >
          <Input placeholder="Например: CI runner prod" maxLength={256} />
        </Form.Item>
        <Form.Item
          name="ttl_seconds"
          label="Срок действия (секунды, необязательно)"
          tooltip="Пусто или 0 — бессрочный. Максимум 63 072 000 (2 года)."
          rules={[{ type: "number", min: 0, max: 63072000, message: "0…63072000" }]}
        >
          <InputNumber style={{ width: "100%" }} min={0} max={63072000} placeholder="бессрочный" />
        </Form.Item>
        {stepUpNotice && <Alert type="warning" showIcon message={stepUpNotice} style={{ marginTop: 4 }} />}
      </Form>
    </Modal>
  );
}

function cap(s: string): string {
  return s.length ? s[0].toUpperCase() + s.slice(1) : s;
}
