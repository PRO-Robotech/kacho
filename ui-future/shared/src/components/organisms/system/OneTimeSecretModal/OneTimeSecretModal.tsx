// OneTimeSecretModal — показывает выпущенное удостоверение РОВНО ОДИН РАЗ, с
// явным предупреждением «показывается один раз — сохраните сейчас», кнопками
// copy/download и acknowledge-checkbox (закрыть можно только осознанно, чтобы
// случайный dismiss не потерял невосстановимое значение).
//
// ─────────────────────────────────────────────────────────────────────────────
// ФОРМ ДВЕ, И ОКНО РАЗЛИЧАЕТ ИХ ВИДОМ (#1235)
//
// Окно писалось под единственную форму — приватный ключ — и говорило о ней
// везде: в подписи поля, в имени скачиваемого файла, в тексте подтверждения.
// Секрет через него показать было НЕЛЬЗЯ: подпись обещала PEM, а поле оставалось
// пустым. Значение невосстановимо, поэтому цена ошибки здесь — потерянный
// доступ, а не неудобство.
//
// Вид — дискриминатор контракта: заполнено РОВНО ОДНО из двух полей ответа
// выдачи. Поэтому здесь ветка по виду, а не «покажем то, что непусто».
//
// Ни ключ, ни секрет нигде не хранятся — потеря значит перевыпуск.

import { useEffect, useMemo, useState } from "react";
import { Alert, Button, Checkbox, Input, Modal, Space, Typography, App } from "antd";
import { CopyOutlined, DownloadOutlined, WarningOutlined } from "@ant-design/icons";
import type { IssuedCredential } from "@shared/api/tokens";
import { copyText } from "@shared/lib/clipboard";
import { SECRET_RADIUS_NOTICE } from "@shared/lib/tokens-util";

const { Text, Paragraph } = Typography;

interface Props {
  open: boolean;
  onClose: () => void;
  credential: IssuedCredential | null;
  /** Заголовок модалки, напр. «Ключ сервисного аккаунта выпущен». */
  title: string;
  /** Человекочитаемая метка субъекта (имя SA / пользователя) — в описании. */
  subjectLabel?: string;
  /** Базовое имя скачиваемого файла ключа (без расширения). */
  fileBaseName?: string;
}

function CopyField({ label, value, mono = true }: { label: string; value: string; mono?: boolean }) {
  const { message } = App.useApp();
  return (
    <div>
      <Text type="secondary" style={{ fontSize: 12 }}>
        {label}
      </Text>
      <Space.Compact style={{ width: "100%" }}>
        <Input
          readOnly
          value={value}
          style={mono ? { fontFamily: "ui-monospace, SFMono-Regular, monospace", fontSize: 12 } : undefined}
        />
        <Button
          icon={<CopyOutlined />}
          onClick={() => {
            // Секрет показывается ОДИН раз: не сработавшее копирование здесь
            // означает потерянный доступ, а не неудобство. См.
            // `@shared/lib/clipboard` — вне защищённого контекста прямое
            // обращение роняло обработчик, и кнопка не делала ничего.
            void copyText(value);
            message.success("Скопировано");
          }}
        />
      </Space.Compact>
    </div>
  );
}

export function OneTimeSecretModal({ open, onClose, credential, title, subjectLabel, fileBaseName }: Props) {
  const { message } = App.useApp();
  const [acknowledged, setAcknowledged] = useState(false);

  // Сбрасываем acknowledge при новом открытии.
  useEffect(() => {
    if (open) setAcknowledged(false);
  }, [open, credential?.key_id]);

  const bundle = useMemo(() => {
    if (!credential) return "";
    if (credential.kind === "secret") {
      return JSON.stringify(
        { client_id: credential.client_id, key_id: credential.key_id, secret: credential.secret },
        null,
        2,
      );
    }
    return JSON.stringify(
      {
        client_id: credential.client_id,
        key_id: credential.key_id,
        algorithm: credential.algorithm,
        private_key_pem: credential.private_key_pem,
        public_key_pem: credential.public_key_pem,
      },
      null,
      2,
    );
  }, [credential]);

  if (!credential) return null;

  const isSecret = credential.kind === "secret";
  // Что именно показано — решает ВИД, а не непустота поля: у контракта заполнено
  // ровно одно из двух, и подпись обязана называть то, что под ней лежит.
  const value = isSecret ? credential.secret : credential.private_key_pem;
  const valueLabel = isSecret ? "Секрет" : "Приватный ключ (PEM, PKCS#8)";
  const valueExt = isSecret ? "txt" : "pem";
  const valueMime = isSecret ? "text/plain" : "application/x-pem-file";
  const base = fileBaseName || credential.key_id || credential.client_id || "kacho-credential";

  const download = (filename: string, content: string, mime: string) => {
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    message.success(`Файл ${filename} сохранён`);
  };

  return (
    <Modal
      open={open}
      title={
        <Space>
          <WarningOutlined style={{ color: "#faad14" }} />
          {title}
        </Space>
      }
      onCancel={onClose}
      maskClosable={false}
      keyboard={false}
      width={640}
      footer={[
        <Button
          key="download-value"
          icon={<DownloadOutlined />}
          onClick={() => download(`${base}.${valueExt}`, value, valueMime)}
        >
          Скачать .{valueExt}
        </Button>,
        <Button
          key="download-json"
          icon={<DownloadOutlined />}
          onClick={() => download(`${base}.json`, bundle, "application/json")}
        >
          Скачать .json
        </Button>,
        <Button
          key="done"
          type="primary"
          disabled={!acknowledged}
          onClick={onClose}
          data-testid="one-time-secret-done"
        >
          Готово
        </Button>,
      ]}
      data-testid="one-time-secret-modal"
    >
      <Space direction="vertical" size={16} style={{ width: "100%" }}>
        <Alert
          type="warning"
          showIcon
          message={
            isSecret
              ? "Секрет показывается один раз — сохраните его сейчас"
              : "Приватный ключ показывается один раз — сохраните его сейчас"
          }
          description={
            isSecret ? (
              <>
                Секрет невозможно восстановить после закрытия окна. Скопируйте или скачайте его прямо сейчас и храните
                в безопасном месте (менеджер секретов). При потере придётся выпустить новый.
              </>
            ) : (
              <>
                Приватный ключ (<Text code>private_key_pem</Text>) невозможно восстановить после закрытия окна.
                Скопируйте или скачайте его прямо сейчас и храните в безопасном месте (менеджер секретов). При потере
                ключ придётся перевыпустить.
              </>
            )
          }
        />

        {/* Радиус называется ТОЛЬКО там, где он таков: секрет предъявительский и
            открывает всё, что может учётная запись. Приписать то же ключевой
            паре значило бы пугать не тем — она предъявляется подписью. */}
        {isSecret && (
          <Alert type="info" showIcon message="Что открывает этот секрет" description={SECRET_RADIUS_NOTICE} />
        )}

        {subjectLabel && (
          <Paragraph style={{ margin: 0 }}>
            <Text type="secondary">Субъект: </Text>
            <Text strong>{subjectLabel}</Text>
          </Paragraph>
        )}

        <CopyField label="Идентификатор клиента" value={credential.client_id} />
        <CopyField label="Идентификатор ключа (kid)" value={credential.key_id} />
        {credential.kind === "keypair" && <CopyField label="Алгоритм" value={credential.algorithm} mono={false} />}

        <div>
          <Space style={{ width: "100%", justifyContent: "space-between" }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {valueLabel}
            </Text>
            <Button
              size="small"
              icon={<CopyOutlined />}
              onClick={() => {
                // Значение показывается ОДИН раз: не сработавшее копирование
                // здесь означает потерянный доступ, а не неудобство. См.
                // `@shared/lib/clipboard` — вне защищённого контекста прямое
                // обращение роняло обработчик, и кнопка не делала ничего.
                void copyText(value);
                message.success(isSecret ? "Секрет скопирован" : "Приватный ключ скопирован");
              }}
            >
              Копировать
            </Button>
          </Space>
          <Input.TextArea
            readOnly
            value={value}
            autoSize={{ minRows: isSecret ? 2 : 6, maxRows: 12 }}
            style={{ fontFamily: "ui-monospace, SFMono-Regular, monospace", fontSize: 12 }}
            data-testid="one-time-secret-pem"
          />
        </div>

        <Checkbox
          checked={acknowledged}
          onChange={(e) => setAcknowledged(e.target.checked)}
          data-testid="one-time-secret-ack"
        >
          {isSecret ? "Я сохранил секрет в надёжном месте" : "Я сохранил приватный ключ в надёжном месте"}
        </Checkbox>
      </Space>
    </Modal>
  );
}
