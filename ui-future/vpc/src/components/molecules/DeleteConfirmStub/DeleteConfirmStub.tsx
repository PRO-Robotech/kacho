import { Modal, Typography } from "antd";
import { ExclamationCircleOutlined } from "@ant-design/icons";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  resourceLabel: string;
  name: string;
  apiPath: string;
}

export function DeleteConfirmStub({ open, onOpenChange, resourceLabel, name, apiPath }: Props) {
  return (
    <Modal
      open={open}
      onCancel={() => onOpenChange(false)}
      onOk={() => onOpenChange(false)}
      okText="Понятно"
      cancelButtonProps={{ style: { display: "none" } }}
      title={
        <span>
          <ExclamationCircleOutlined style={{ color: "#fa8c16", marginRight: 8 }} />
          Удаление через UI отключено
        </span>
      }
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <div>
          <Typography.Text type="secondary">{resourceLabel}: </Typography.Text>
          <Typography.Text strong>{name}</Typography.Text>
        </div>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          На текущей итерации UI не выполняет destructive-операции. Удаляйте через REST API:
        </Typography.Text>
        <Typography.Text code copyable style={{ display: "block", wordBreak: "break-all" }}>
          DELETE {apiPath}
        </Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          или через <code>kachoctl</code>.
        </Typography.Text>
      </div>
    </Modal>
  );
}
