// UserTokensPanel — вкладка «Токены» пользователя: список OAuth-токенов
// (UserTokenService.List) + выпуск токена с TTL + одноразовый показ секрета +
// отзыв. Секрет (private_key_pem) приходит один раз в Operation.response —
// показываем его немедленно в отдельной модалке (копировать/скачать), после
// закрытия он безвозвратно теряется. Все мутации — async через Operation.

import { useEffect, useState } from "react";
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

import { iamApi, userTokensPath } from "@shared/api/iam";
import type { IssueUserTokenBody, IssueUserTokenResponse, UserOAuthClient } from "@shared/api/iam";
import type { Operation } from "@shared/api/types";
import { HeaderSlotPortal } from "@shared/components/organisms/DetailShell";
import { CopyableMonoId, fmtTs, useIamMutation } from "@shared/components/organisms/iam/IamCommon";
import { useTableScrollY } from "@/components/organisms/iam/IamListShell";
import { toast } from "@shared/lib/toast";
import { MAX_TTL_DAYS, TTL_PRESETS, expiryState, ttlDaysToSeconds } from "@shared/lib/tokens-util";

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
  userId,
  onClose,
  onIssued,
}: {
  open: boolean;
  userId: string;
  onClose: () => void;
  onIssued: (resp: IssueUserTokenResponse) => void;
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
    path: userTokensPath(userId),
    invalidateKeys: [["iam", "user-tokens", userId]],
    onSuccess: (op: Operation) => {
      const resp = (op.response ?? undefined) as unknown as IssueUserTokenResponse | undefined;
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
    const body: IssueUserTokenBody = { description: description.trim(), ttl_seconds: ttlSeconds };
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
        <Form.Item label="Описание" help="Например: токен для CI. Не более 256 символов.">
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
function SecretModal({ resp, onClose }: { resp: IssueUserTokenResponse; onClose: () => void }) {
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
      a.download = `${keyId || clientId || "user-token"}.pem`;
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
        <Descriptions.Item label="Client ID">
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
        style={{ fontFamily: "ui-monospace, monospace", fontSize: 12 }}
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

// UserTokensPanel — таблица токенов + CTA «Создать токен» (в слоте шапки таба) +
// per-row отзыв (Popconfirm). Список рефетчится после выпуска/отзыва.
export function UserTokensPanel({ userId }: { userId: string }) {
  const [createOpen, setCreateOpen] = useState(false);
  const [secret, setSecret] = useState<IssueUserTokenResponse | null>(null);
  const [revokingId, setRevokingId] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ["iam", "user-tokens", userId],
    queryFn: () => iamApi.listUserTokens(userId, { page_size: "1000" }),
    enabled: !!userId,
    staleTime: 0,
  });

  const revoke = useIamMutation({
    method: "DELETE",
    path: (body) => `${userTokensPath(userId)}/${encodeURIComponent((body as { tokenId: string }).tokenId)}`,
    invalidateKeys: [["iam", "user-tokens", userId]],
    successText: "Токен отозван",
  });

  useEffect(() => {
    if (!revoke.submitting) setRevokingId(null);
  }, [revoke.submitting]);

  const tokens = list.data?.tokens ?? [];
  const { wrapRef, scrollY } = useTableScrollY();

  const columns: ColumnsType<UserOAuthClient> = [
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
            void revoke.run({ tokenId: row.id }).catch(() => undefined);
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
        <Table<UserOAuthClient>
          rowKey="id"
          size="small"
          className="kc-table"
          loading={list.isLoading}
          dataSource={tokens}
          columns={columns}
          pagination={false}
          scroll={{ x: "max-content", y: scrollY }}
          locale={{ emptyText: "Токенов нет. Создайте первый токен." }}
          data-testid="user-tokens-table"
        />
      </div>

      <CreateTokenModal
        open={createOpen}
        userId={userId}
        onClose={() => setCreateOpen(false)}
        onIssued={(resp) => {
          setCreateOpen(false);
          setSecret(resp);
        }}
      />
      {secret && <SecretModal resp={secret} onClose={() => setSecret(null)} />}
    </div>
  );
}
