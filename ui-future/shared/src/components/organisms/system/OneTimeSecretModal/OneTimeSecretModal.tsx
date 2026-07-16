// OneTimeSecretModal — Stage 4. Показывает выпущенный credential (private_key_pem
// + client_id + key_id + algorithm) РОВНО ОДИН РАЗ, с явным предупреждением
// «показывается один раз — сохраните сейчас», кнопками copy/download и
// acknowledge-checkbox (закрыть можно только осознанно, чтобы случайный dismiss
// не потерял невосстановимый секрет).
//
// Backend отдаёт private_key_pem единожды в Operation.response (см. api/tokens.ts).
// Ключ нигде не персистится — потеря = перевыпуск.

import { useEffect, useMemo, useState } from "react";
import { Alert, Button, Checkbox, Input, Modal, Space, Typography, App } from "antd";
import { CopyOutlined, DownloadOutlined, WarningOutlined } from "@ant-design/icons";
import type { IssuedCredential } from "@shared/api/tokens";

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
            void navigator.clipboard.writeText(value);
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
          key="download-pem"
          icon={<DownloadOutlined />}
          onClick={() => download(`${base}.pem`, credential.private_key_pem, "application/x-pem-file")}
        >
          Скачать .pem
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
          message="Секрет показывается один раз — сохраните его сейчас"
          description={
            <>
              Приватный ключ (<Text code>private_key_pem</Text>) невозможно восстановить после закрытия окна. Скопируйте
              или скачайте его прямо сейчас и храните в безопасном месте (менеджер секретов). При потере ключ придётся
              перевыпустить.
            </>
          }
        />

        {subjectLabel && (
          <Paragraph style={{ margin: 0 }}>
            <Text type="secondary">Субъект: </Text>
            <Text strong>{subjectLabel}</Text>
          </Paragraph>
        )}

        <CopyField label="Client ID" value={credential.client_id} />
        <CopyField label="Key ID (kid)" value={credential.key_id} />
        <CopyField label="Algorithm" value={credential.algorithm} mono={false} />

        <div>
          <Space style={{ width: "100%", justifyContent: "space-between" }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              Private key (PEM, PKCS#8)
            </Text>
            <Button
              size="small"
              icon={<CopyOutlined />}
              onClick={() => {
                void navigator.clipboard.writeText(credential.private_key_pem);
                message.success("Приватный ключ скопирован");
              }}
            >
              Копировать
            </Button>
          </Space>
          <Input.TextArea
            readOnly
            value={credential.private_key_pem}
            autoSize={{ minRows: 6, maxRows: 12 }}
            style={{ fontFamily: "ui-monospace, SFMono-Regular, monospace", fontSize: 12 }}
            data-testid="one-time-secret-pem"
          />
        </div>

        <Checkbox
          checked={acknowledged}
          onChange={(e) => setAcknowledged(e.target.checked)}
          data-testid="one-time-secret-ack"
        >
          Я сохранил приватный ключ в надёжном месте
        </Checkbox>
      </Space>
    </Modal>
  );
}
