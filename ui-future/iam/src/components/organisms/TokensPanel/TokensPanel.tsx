// TokensPanel — вкладка «Токены» субъекта: список удостоверений + выпуск с
// названным ВИДОМ и сроком + одноразовый показ секрета + отзыв. Все мутации —
// async через Operation.
//
// ─────────────────────────────────────────────────────────────────────────────
// ВИД УДОСТОВЕРЕНИЯ НАЗЫВАЕТСЯ ЯВНО, И ПО УМОЛЧАНИЮ ЭТО СЕКРЕТ (#1235)
//
// Здесь выпускалась ключевая пара (сервер выдаёт её, когда вид не назван) и
// подписывалась «действует бессрочно». Полоса докера ключевой материал в поле
// пароля больше не принимает — окно перехода закрыто по умолчанию, — поэтому
// арендатор, у которого перестал работать `docker login`, шёл в консоль (а
// непрограммист больше никуда и не пойдёт) и получал ровно то, что платформа
// отвергает. Путь восстановления вёл в тупик.
//
// Теперь вид называет консоль, а не умолчание сервера, и умолчание консоли —
// СЕКРЕТ: это тот вид, который докерная полоса принимает. Ключевая пара
// остаётся выбором для внешней федерации.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОДНОРАЗОВОЕ ЗНАЧЕНИЕ ЧИТАЕТСЯ ИЗ ОТВЕТА ВЫДАЧИ, А НЕ ИЗ ОПРОСА
//
// Выдача секрета завершается НА ПУТИ ЗАПРОСА, и секрет подменяется в теле
// ответа ПОСЛЕ записи строки: сама строка операции его не несёт ни в какой
// момент. Читатель, ждущий `GET /operations/{id}`, получил бы тело БЕЗ секрета
// — то есть потерял бы невосстановимое значение при исправной выдаче. Поэтому
// доставка идёт из НЕМЕДЛЕННОГО ответа, а опрос остаётся запасным путём для
// ключевой пары (она уезжает в асинхронный путь).
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

import { useEffect, useRef, useState, type ReactNode } from "react";
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
import { issuedCredentialFromOperation, type IssuedCredential } from "@shared/api/tokens";
import {
  CREDENTIAL_KIND_KEYPAIR,
  CREDENTIAL_KIND_SECRET,
  SECRET_RADIUS_NOTICE,
  SECRET_TTL_DEFAULT_DAYS,
  credentialKindLabel,
  expiryState,
  maxTtlDaysFor,
  ttlDaysToSeconds,
  ttlPresetsFor,
  type CredentialKind,
  type IssuableCredentialKind,
} from "@shared/lib/tokens-util";
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
  /** Вид удостоверения. Край, который о видах не говорит, оставляет его пустым —
   *  тогда столбца вида нет вовсе (поле без источника не показывается). */
  credential_kind?: CredentialKind;
}

/** Тело Issue-запроса. `created_by_user_id` проставляет край из принципала. */
export interface IssueTokenBody {
  description?: string;
  ttl_seconds?: number;
  credential_kind?: IssuableCredentialKind;
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

// Бейдж срока действия: «Бессрочный» / «Истек» / «истекает через X».
//
// Вид передаётся, потому что от него зависит СМЫСЛ пустого срока: у ключевой
// пары пусто означает «бессрочно», у секрета такой строки не бывает вовсе —
// назвать её бессрочной значило бы утверждать о ресурсе неправду.
function ExpiryBadge({ expiresAt, kind }: { expiresAt?: string; kind?: CredentialKind }) {
  const st = expiryState(expiresAt, Date.now(), kind);
  const color =
    st.kind === "expired" ? "red" : st.kind === "none" ? "default" : st.kind === "unknown" ? "orange" : "green";
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
  onIssued: (cred: IssuedCredential) => void;
}) {
  const [description, setDescription] = useState("");
  const [kind, setKind] = useState<IssuableCredentialKind>(CREDENTIAL_KIND_SECRET);
  const presets = ttlPresetsFor(kind);
  const maxDays = maxTtlDaysFor(kind);
  const defaultTtlKey = presets[presets.length - 1]?.key ?? "custom";
  const [ttlKey, setTtlKey] = useState<string>(defaultTtlKey);
  const [customDays, setCustomDays] = useState<number | null>(90);

  const resetForm = () => {
    setDescription("");
    setKind(CREDENTIAL_KIND_SECRET);
    setTtlKey(ttlPresetsFor(CREDENTIAL_KIND_SECRET).at(-1)?.key ?? "custom");
    setCustomDays(90);
  };

  // Смена вида меняет и НАБОР вариантов срока: у секрета нет «Без срока», у
  // ключевой пары нет «7 дней». Выбор, которого в новом наборе не существует,
  // молча уехал бы в ноль — то есть в «бессрочно» у одного вида и в «умолчание
  // политики» у другого, ни о том ни о другом никого не спросив.
  const switchKind = (next: IssuableCredentialKind) => {
    setKind(next);
    const nextPresets = ttlPresetsFor(next);
    if (ttlKey !== "custom" && !nextPresets.some((p) => p.key === ttlKey)) {
      setTtlKey(nextPresets.at(-1)?.key ?? "custom");
    }
  };

  // Доставка одноразового значения идёт ОДИН раз за выпуск, каким бы путём оно
  // ни пришло: секрет — немедленным ответом, ключевая пара — опросом. Без этой
  // защёлки опрос, приехавший вторым, перекрыл бы показанный секрет пустым
  // телом (строка операции секрета не несёт).
  const deliveredRef = useRef(false);
  const deliver = (resp: unknown): boolean => {
    if (deliveredRef.current) return true;
    const cred = issuedCredentialFromOperation(resp as Operation | undefined);
    if (!cred) return false;
    deliveredRef.current = true;
    onIssued(cred);
    resetForm();
    return true;
  };

  const issue = useIamMutation({
    method: "POST",
    path: collectionPath(subjectId),
    invalidateKeys: [["iam", queryKind, subjectId]],
    // Опрос — ПОСЛЕДНЕЕ слово: если и он не принёс значения, выдача прошла, а
    // одноразовое значение потеряно. Молчать об этом нельзя, но и открывать
    // пустое окно «вот ваш ключ» — тоже: пустая рамка с подписью «приватный
    // ключ» утверждает, что значение показано, тогда как показывать нечего.
    // Здесь стояло именно это, и проба закрепляла фантом как норму.
    onSuccess: (op: Operation) => {
      if (deliver(op)) return;
      toast.error(
        "Токен выпущен, но его значение не пришло — оно невосстановимо. " + "Отзовите этот токен и выпустите новый.",
      );
    },
  });

  const handleClose = () => {
    if (issue.submitting) return; // не закрываем во время выпуска
    resetForm();
    onClose();
  };

  const customInvalid = ttlKey === "custom" && (customDays == null || customDays < 1 || customDays > maxDays);

  const submit = () => {
    if (description.length > 256) {
      toast.error("Описание не длиннее 256 символов");
      return;
    }
    if (customInvalid) {
      toast.error(`Срок в днях — от 1 до ${maxDays}`);
      return;
    }
    const ttlSeconds =
      ttlKey === "custom"
        ? ttlDaysToSeconds(customDays ?? 0, kind)
        : (presets.find((p) => p.key === ttlKey)?.seconds ?? 0);
    const body: IssueTokenBody = {
      description: description.trim(),
      ttl_seconds: ttlSeconds,
      credential_kind: kind,
    };
    deliveredRef.current = false;
    // Ошибка submit/операции не закрывает модалку — useIamMutation покажет toast.
    void issue
      .run(body)
      .then(deliver)
      .catch(() => undefined);
  };

  const segmentOptions = [
    ...presets.map((p) => ({ label: p.label, value: p.key })),
    { label: "Свой срок", value: "custom" },
  ];

  const kindOptions = [
    { label: credentialKindLabel(CREDENTIAL_KIND_SECRET), value: CREDENTIAL_KIND_SECRET },
    { label: credentialKindLabel(CREDENTIAL_KIND_KEYPAIR), value: CREDENTIAL_KIND_KEYPAIR },
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
        <Form.Item
          label="Вид удостоверения"
          help={
            kind === CREDENTIAL_KIND_SECRET
              ? "Секрет предъявляется как есть: строка в поле пароля docker login и в заголовке Authorization. Ни библиотек, ни подписи, ни обмена."
              : "Ключевая пара: вы сами подписываете утверждение и обмениваете его на токен. Нужна внешней федерации; docker login её больше не принимает."
          }
        >
          <Segmented value={kind} onChange={(v) => switchKind(v as IssuableCredentialKind)} options={kindOptions} />
        </Form.Item>
        {kind === CREDENTIAL_KIND_SECRET && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            message="Что открывает этот секрет"
            description={SECRET_RADIUS_NOTICE}
          />
        )}
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
            help={customInvalid ? `От 1 до ${maxDays} дней` : `Максимум ${maxDays} дней`}
          >
            <InputNumber
              value={customDays ?? undefined}
              onChange={(v) => setCustomDays(typeof v === "number" ? v : null)}
              min={1}
              max={maxDays}
              style={{ width: 160 }}
            />
          </Form.Item>
        )}
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0, fontSize: 12 }}>
          {kind === CREDENTIAL_KIND_SECRET
            ? `Срок обязателен: бессрочного секрета не бывает. Не назовёте — платформа поставит ${SECRET_TTL_DEFAULT_DAYS} дней, дольше ${maxDays} дней выпустить нельзя. Значение будет показано один раз после создания.`
            : "«Без срока» — ключ действует, пока его не отзовут. Приватный ключ будет показан один раз после создания."}
        </Typography.Paragraph>
      </Form>
    </Modal>
  );
}

// SecretModal — одноразовый показ выпущенного удостоверения. Держит значение в
// памяти до явного закрытия; фоновая ошибка (буфер обмена / скачивание) его не
// теряет.
//
// ФОРМ ДВЕ, И ОНИ РАЗЛИЧАЮТСЯ ВИДОМ, а не наличием поля: у секрета — одна
// строка, у ключевой пары — PEM. Общее окно с двумя необязательными полями
// показало бы пустую рамку там, где лежит единственный экземпляр
// невосстановимого значения.
function SecretModal({
  cred,
  fallbackFileName,
  onClose,
}: {
  cred: IssuedCredential;
  fallbackFileName: string;
  onClose: () => void;
}) {
  const isSecret = cred.kind === "secret";
  const value = isSecret ? cred.secret : cred.private_key_pem;
  const valueLabel = isSecret ? "Секрет" : "Приватный ключ (PEM)";
  const fileExt = isSecret ? "txt" : "pem";
  const mime = isSecret ? "text/plain" : "application/x-pem-file";
  const keyId = cred.key_id;
  const clientId = cred.client_id;

  const copyValue = async () => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(isSecret ? "Секрет скопирован" : "Приватный ключ скопирован");
    } catch {
      toast.error("Не удалось скопировать. Скопируйте вручную из поля ниже.");
    }
  };

  const downloadValue = () => {
    try {
      const blob = new Blob([value], { type: mime });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${keyId || clientId || fallbackFileName}.${fileExt}`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      toast.success("Файл сохранен");
    } catch {
      toast.error("Не удалось скачать файл. Скопируйте значение вручную.");
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
          Я сохранил значение
        </Button>,
      ]}
    >
      <Alert
        type="warning"
        showIcon
        style={{ marginBottom: 16 }}
        message={`Сохраните значение — оно больше не будет показано`}
        description={`${valueLabel} выдаётся один раз и нигде не хранится. После закрытия окна восстановить его будет невозможно — потребуется выпустить новый токен.`}
      />
      {isSecret && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          message="Что открывает этот секрет"
          description={SECRET_RADIUS_NOTICE}
        />
      )}
      <Descriptions column={1} size="small" bordered style={{ marginBottom: 16 }}>
        <Descriptions.Item label="Идентификатор">
          <CopyableMonoId id={keyId} />
        </Descriptions.Item>
        <Descriptions.Item label="Идентификатор клиента">
          <CopyableMonoId id={clientId} />
        </Descriptions.Item>
        <Descriptions.Item label="Вид">
          {credentialKindLabel(isSecret ? CREDENTIAL_KIND_SECRET : CREDENTIAL_KIND_KEYPAIR)}
        </Descriptions.Item>
        {cred.kind === "keypair" ? (
          <Descriptions.Item label="Алгоритм">{cred.algorithm || "ES256"}</Descriptions.Item>
        ) : null}
      </Descriptions>
      <Typography.Text strong style={{ display: "block", marginBottom: 6 }}>
        {valueLabel}
      </Typography.Text>
      <Input.TextArea
        readOnly
        value={value}
        autoSize={{ minRows: isSecret ? 2 : 6, maxRows: 14 }}
        style={{ fontFamily: MONO_FONT, fontSize: 12 }}
      />
      <Space style={{ marginTop: 12 }}>
        <Button icon={<CopyOutlined />} onClick={copyValue}>
          Скопировать
        </Button>
        <Button icon={<DownloadOutlined />} onClick={downloadValue}>
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
  const [issued, setIssued] = useState<IssuedCredential | null>(null);
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

  // Столбец вида появляется, ТОЛЬКО когда край о видах говорит. Край прежнего
  // выпуска поля не отдаёт вовсе, и колонка сплошных прочерков утверждала бы о
  // ресурсах то, чего никто не спрашивал (канон консоли, правило 9: поле без
  // источника не показывается).
  const kindKnown = rows.some((r) => !!r.credential_kind);

  const columns: ColumnsType<TokenRow> = [
    {
      title: "Описание",
      dataIndex: "description",
      key: "description",
      render: (v?: string) => v || <Typography.Text type="secondary">—</Typography.Text>,
    },
    ...(kindKnown
      ? [
          {
            title: "Вид",
            dataIndex: "credential_kind",
            key: "credential_kind",
            width: 150,
            render: (v?: CredentialKind) =>
              credentialKindLabel(v) || <Typography.Text type="secondary">—</Typography.Text>,
          } as ColumnsType<TokenRow>[number],
        ]
      : []),
    {
      title: "Истекает",
      dataIndex: "expires_at",
      key: "expires_at",
      width: 190,
      render: (_v: unknown, row: TokenRow) => <ExpiryBadge expiresAt={row.expires_at} kind={row.credential_kind} />,
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
        onIssued={(cred) => {
          setCreateOpen(false);
          setIssued(cred);
        }}
      />
      {issued && <SecretModal cred={issued} fallbackFileName={fallbackFileName} onClose={() => setIssued(null)} />}
    </div>
  );
}
