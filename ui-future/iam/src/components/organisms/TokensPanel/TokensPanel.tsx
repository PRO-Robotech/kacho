// TokensPanel — вкладка «Токены» субъекта: список OAuth-клиентов + выпуск токена
// с TTL + одноразовый показ секрета + отзыв. Секрет (private_key_pem) приходит
// один раз в Operation.response — показываем его немедленно в отдельной модалке
// (копировать/скачать), после закрытия он безвозвратно теряется. Все мутации —
// async через Operation.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОДНА РЕАЛИЗАЦИЯ НА ДВА СУБЪЕКТА (канон консоли, правило 9)
//
// Этот вид жил ДВУМЯ копиями по 368 строк: `SaKeysPanel` (ключи сервисного
// аккаунта) и `UserTokensPanel` (личные токены пользователя). Различались они
// ровно тремя вещами — именем поля субъекта, путём коллекции и ключом полезной
// нагрузки списка; всё остальное, включая тексты подтверждений, форму выпуска и
// окно секрета, совпадало дословно. Форк отстаёт молча: правка одной копии до
// другой не доезжает, и в одном продукте живут два поведения одного экрана.
//
// Что своё у субъекта — объявляет ВЛАДЕЛЕЦ (тонкая обёртка рядом со своим
// путём); всё остальное здесь, в одном месте. Обёртки же импортируют
// `@shared/api/iam` — этот файл о крае не знает вовсе, поэтому его можно
// монтировать в пробах, не подменяя клиент.

import { useEffect, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Segmented,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { CopyOutlined, DeleteOutlined, DownloadOutlined, PlusOutlined } from "@ant-design/icons";

import type { Operation } from "@shared/api/types";
import { HeaderSlotPortal } from "@shared/components/organisms/DetailShell";
import { CopyableMonoId, fmtTs, useIamMutation } from "@shared/components/organisms/iam/IamCommon";
import { useTableScrollY } from "@/components/organisms/iam/IamListShell";
import { toast } from "@shared/lib/toast";
import { MAX_TTL_DAYS, TTL_PRESETS, expiryState, ttlDaysToSeconds } from "@shared/lib/tokens-util";
import { MONO_FONT } from "@shared/components/organisms/form/editor-surface";

/** Строка списка. Форма у обоих субъектов одна — различаются только имена типов
 *  в контракте, а не поля. */
export interface TokenRow {
  id: string;
  description?: string;
  expires_at?: string;
  last_used_at?: string;
  created_by_user_id?: string;
  created_at?: string;
}

/** Ответ Issue-операции: одноразовый секрет и опознавательные идентификаторы. */
export interface IssuedSecret {
  client_id?: string;
  private_key_pem?: string;
  algorithm?: string;
  key_id?: string;
  key?: { id?: string };
}

/** Тело Issue-запроса. `created_by_user_id` проставляет край из принципала. */
export interface IssueTokenBody {
  description?: string;
  ttl_seconds?: number;
}

export interface TokensPanelProps {
  /** Идентификатор субъекта — сервисного аккаунта либо пользователя. */
  subjectId: string;
  /** REST-путь коллекции токенов этого субъекта. */
  collectionPath: (subjectId: string) => string;
  /** Ключ кэша списка: `["iam", "sa-keys" | "user-tokens", subjectId]`. */
  queryKind: string;
  /** Чтение списка у края. Ключ полезной нагрузки у субъектов разный, поэтому
   *  разворачивает его владелец, а не эта панель. */
  list: (subjectId: string) => Promise<TokenRow[]>;
  /** Имя файла при сохранении секрета, когда идентификаторы пусты. */
  fallbackFileName: string;
  /** Пример назначения в подсказке поля «Описание». */
  descriptionExample: string;
  /** Опознавательный признак таблицы для проб. */
  tableTestId: string;
}

// Бейдж срока действия токена: «Бессрочный» / «Истек» / «истекает через X».
function ExpiryBadge({ expiresAt }: { expiresAt?: string }) {
  const st = expiryState(expiresAt);
  const color = st.kind === "expired" ? "red" : st.kind === "none" ? "default" : "green";
  return (
    <Tag color={color} style={{ margin: 0 }}>
      {st.label}
    </Tag>
  );
}

// CreateTokenModal — модалка выпуска токена. Описание (≤256) + TTL (пресеты либо
// «Свой срок» в днях). Клиентская валидация диапазона ДО submit; ошибка мутации
// не закрывает модалку (toast от useIamMutation). На success — секрет отдается
// наверх (onIssued) и модалка закрывается.
function CreateTokenModal({
  open,
  subjectId,
  collectionPath,
  queryKind,
  descriptionExample,
  onClose,
  onIssued,
}: {
  open: boolean;
  subjectId: string;
  collectionPath: (subjectId: string) => string;
  queryKind: string;
  descriptionExample: string;
  onClose: () => void;
  onIssued: (resp: IssuedSecret) => void;
}) {
  const [description, setDescription] = useState("");
  const [ttlKey, setTtlKey] = useState<string>("90d");
  const [customDays, setCustomDays] = useState<number | null>(90);

  const resetForm = () => {
    setDescription("");
    setTtlKey("90d");
    setCustomDays(90);
  };

  const issue = useIamMutation({
    method: "POST",
    path: collectionPath(subjectId),
    invalidateKeys: [["iam", queryKind, subjectId]],
    onSuccess: (op: Operation) => {
      const resp = (op.response ?? undefined) as unknown as IssuedSecret | undefined;
      onIssued(resp ?? {});
      resetForm();
    },
  });

  const handleClose = () => {
    if (issue.submitting) return; // не закрываем во время выпуска
    resetForm();
    onClose();
  };

  const customInvalid = ttlKey === "custom" && (customDays == null || customDays < 1 || customDays > MAX_TTL_DAYS);

  const submit = () => {
    if (description.length > 256) {
      toast.error("Описание не длиннее 256 символов");
      return;
    }
    if (customInvalid) {
      toast.error(`Срок в днях — от 1 до ${MAX_TTL_DAYS}`);
      return;
    }
    const ttlSeconds =
      ttlKey === "custom"
        ? ttlDaysToSeconds(customDays ?? 0)
        : (TTL_PRESETS.find((p) => p.key === ttlKey)?.seconds ?? 0);
    const body: IssueTokenBody = { description: description.trim(), ttl_seconds: ttlSeconds };
    // Ошибка submit/операции не закрывает модалку — useIamMutation покажет toast.
    void issue.run(body).catch(() => undefined);
  };

  const segmentOptions = [
    ...TTL_PRESETS.map((p) => ({ label: p.label, value: p.key })),
    { label: "Свой срок", value: "custom" },
  ];

  return (
    <Modal
      title="Создать токен"
      open={open}
      onCancel={handleClose}
      maskClosable={false}
      okText="Создать"
      cancelText="Отмена"
      confirmLoading={issue.submitting}
      onOk={submit}
      okButtonProps={{ disabled: customInvalid }}
    >
      <Form layout="vertical">
        <Form.Item label="Описание" help={`Например: ${descriptionExample}. Не более 256 символов.`}>
          <Input.TextArea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            maxLength={256}
            showCount
            autoSize={{ minRows: 1, maxRows: 3 }}
            placeholder="Назначение токена"
          />
        </Form.Item>
        <Form.Item label="Срок действия">
          <Segmented value={ttlKey} onChange={(v) => setTtlKey(String(v))} options={segmentOptions} />
        </Form.Item>
        {ttlKey === "custom" && (
          <Form.Item
            label="Срок в днях"
            validateStatus={customInvalid ? "error" : undefined}
            help={customInvalid ? `От 1 до ${MAX_TTL_DAYS} дней` : `Максимум ${MAX_TTL_DAYS} дней (~2 года)`}
          >
            <InputNumber
              value={customDays ?? undefined}
              onChange={(v) => setCustomDays(typeof v === "number" ? v : null)}
              min={1}
              max={MAX_TTL_DAYS}
              style={{ width: 160 }}
            />
          </Form.Item>
        )}
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0, fontSize: 12 }}>
          «Без срока» — токен действует бессрочно. Секрет будет показан один раз после создания.
        </Typography.Paragraph>
      </Form>
    </Modal>
  );
}

// SecretModal — одноразовый показ секрета выпущенного токена. Держит private_key_pem
// в памяти до явного закрытия; фоновая ошибка (clipboard/скачивание) секрет не теряет.
function SecretModal({
  resp,
  fallbackFileName,
  onClose,
}: {
  resp: IssuedSecret;
  fallbackFileName: string;
  onClose: () => void;
}) {
  const pem = resp.private_key_pem ?? "";
  const keyId = resp.key_id ?? resp.key?.id ?? "";
  const clientId = resp.client_id ?? "";

  const copyPem = async () => {
    try {
      await navigator.clipboard.writeText(pem);
      toast.success("Приватный ключ скопирован");
    } catch {
      toast.error("Не удалось скопировать. Скопируйте вручную из поля ниже.");
    }
  };

  const downloadPem = () => {
    try {
      const blob = new Blob([pem], { type: "application/x-pem-file" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${keyId || clientId || fallbackFileName}.pem`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      toast.success("Файл ключа сохранен");
    } catch {
      toast.error("Не удалось скачать файл. Скопируйте ключ вручную.");
    }
  };

  return (
    <Modal
      title="Токен создан"
      open
      onCancel={onClose}
      maskClosable={false}
      width={640}
      footer={[
        <Button key="close" type="primary" onClick={onClose}>
          Я сохранил ключ
        </Button>,
      ]}
    >
      <Alert
        type="warning"
        showIcon
        style={{ marginBottom: 16 }}
        message="Сохраните ключ — он больше не будет показан"
        description="Приватный ключ выдается один раз и нигде не хранится. После закрытия окна восстановить его будет невозможно — потребуется выпустить новый токен."
      />
      <Descriptions column={1} size="small" bordered style={{ marginBottom: 16 }}>
        <Descriptions.Item label="Идентификатор ключа">
          <CopyableMonoId id={keyId} />
        </Descriptions.Item>
        <Descriptions.Item label="Идентификатор клиента">
          <CopyableMonoId id={clientId} />
        </Descriptions.Item>
        <Descriptions.Item label="Алгоритм">{resp.algorithm || "ES256"}</Descriptions.Item>
      </Descriptions>
      <Typography.Text strong style={{ display: "block", marginBottom: 6 }}>
        Приватный ключ (PEM)
      </Typography.Text>
      <Input.TextArea
        readOnly
        value={pem}
        autoSize={{ minRows: 6, maxRows: 14 }}
        style={{ fontFamily: MONO_FONT, fontSize: 12 }}
      />
      <Space style={{ marginTop: 12 }}>
        <Button icon={<CopyOutlined />} onClick={copyPem}>
          Скопировать
        </Button>
        <Button icon={<DownloadOutlined />} onClick={downloadPem}>
          Скачать
        </Button>
      </Space>
    </Modal>
  );
}

// TokensPanel — таблица токенов + CTA «Создать токен» (в слоте шапки таба) +
// per-row отзыв (Popconfirm). Список рефетчится после выпуска/отзыва.
export function TokensPanel({
  subjectId,
  collectionPath,
  queryKind,
  list: listFn,
  fallbackFileName,
  descriptionExample,
  tableTestId,
}: TokensPanelProps): ReactNode {
  const [createOpen, setCreateOpen] = useState(false);
  const [secret, setSecret] = useState<IssuedSecret | null>(null);
  const [revokingId, setRevokingId] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ["iam", queryKind, subjectId],
    queryFn: () => listFn(subjectId),
    enabled: !!subjectId,
    staleTime: 0,
  });

  const revoke = useIamMutation({
    method: "DELETE",
    path: (body) => `${collectionPath(subjectId)}/${encodeURIComponent((body as { id: string }).id)}`,
    invalidateKeys: [["iam", queryKind, subjectId]],
    successText: "Токен отозван",
  });

  useEffect(() => {
    if (!revoke.submitting) setRevokingId(null);
  }, [revoke.submitting]);

  const rows = list.data ?? [];
  const { wrapRef, scrollY } = useTableScrollY();

  const columns: ColumnsType<TokenRow> = [
    {
      title: "Описание",
      dataIndex: "description",
      key: "description",
      render: (v?: string) => v || <Typography.Text type="secondary">—</Typography.Text>,
    },
    {
      title: "Истекает",
      dataIndex: "expires_at",
      key: "expires_at",
      width: 190,
      render: (v?: string) => <ExpiryBadge expiresAt={v} />,
    },
    {
      title: "Последнее использование",
      dataIndex: "last_used_at",
      key: "last_used_at",
      width: 210,
      render: (v?: string) => fmtTs(v),
    },
    { title: "Создан", dataIndex: "created_at", key: "created_at", width: 200, render: (v?: string) => fmtTs(v) },
    {
      title: "Кем создан",
      dataIndex: "created_by_user_id",
      key: "created_by_user_id",
      render: (v?: string) => <CopyableMonoId id={v} />,
    },
    { title: "Идентификатор", dataIndex: "id", key: "id", render: (v: string) => <CopyableMonoId id={v} /> },
    {
      title: "",
      key: "actions",
      width: 130,
      render: (_v, row) => (
        <Popconfirm
          title="Отозвать токен?"
          description="Токен перестанет действовать безвозвратно."
          okText="Отозвать"
          okButtonProps={{ danger: true }}
          cancelText="Отмена"
          onConfirm={() => {
            setRevokingId(row.id);
            void revoke.run({ id: row.id }).catch(() => undefined);
          }}
        >
          <Button
            danger
            size="small"
            type="text"
            icon={<DeleteOutlined />}
            loading={revoke.submitting && revokingId === row.id}
          >
            Отозвать
          </Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <div style={{ height: "100%", minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>
      <HeaderSlotPortal>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          Создать токен
        </Button>
      </HeaderSlotPortal>

      <div ref={wrapRef} className="kc-table-fill" style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
        <Table<TokenRow>
          rowKey="id"
          size="small"
          className="kc-table"
          loading={list.isLoading}
          dataSource={rows}
          columns={columns}
          pagination={false}
          scroll={{ x: "max-content", y: scrollY }}
          locale={{ emptyText: "Токенов нет. Создайте первый токен." }}
          data-testid={tableTestId}
        />
      </div>

      <CreateTokenModal
        open={createOpen}
        subjectId={subjectId}
        collectionPath={collectionPath}
        queryKind={queryKind}
        descriptionExample={descriptionExample}
        onClose={() => setCreateOpen(false)}
        onIssued={(resp) => {
          setCreateOpen(false);
          setSecret(resp);
        }}
      />
      {secret && <SecretModal resp={secret} fallbackFileName={fallbackFileName} onClose={() => setSecret(null)} />}
    </div>
  );
}
