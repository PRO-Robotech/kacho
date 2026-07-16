import { Modal, Typography } from "antd";
import { InfoCircleOutlined } from "@ant-design/icons";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  resourceLabel: string;
  name: string;
  apiPath: string;
}

export function MoveStubDialog({ open, onOpenChange, resourceLabel, name, apiPath }: Props) {
  return (
    <Modal
      open={open}
      onCancel={() => onOpenChange(false)}
      onOk={() => onOpenChange(false)}
      okText="Понятно"
      cancelButtonProps={{ style: { display: "none" } }}
      title={
        <span>
          <InfoCircleOutlined style={{ color: "#3D8DF5", marginRight: 8 }} />
          Перемещение через UI пока не реализовано
        </span>
      }
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <div>
          <Typography.Text type="secondary">{resourceLabel}: </Typography.Text>
          <Typography.Text strong>{name}</Typography.Text>
        </div>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          UI пока не имеет picker&apos;а целевого Project. Используйте REST API:
        </Typography.Text>
        <Typography.Text code copyable style={{ display: "block", whiteSpace: "pre-wrap" }}>
          {`POST ${apiPath}:move\n{ "destination_project_id": "<project-id>" }`}
        </Typography.Text>
      </div>
    </Modal>
  );
}
