// ResourceIcon — иконка ресурса для заголовков модалок Create/Edit.
// Mapping синхронизирован с навигацией в сайдбаре (см. src/lib/service-modules.tsx)
// — те же AntD Outlined-иконки, чтобы пользователь узнавал ресурс в обоих местах.

import {
  ApartmentOutlined,
  ApiOutlined,
  AppstoreOutlined,
  BankOutlined,
  CameraOutlined,
  ClusterOutlined,
  DesktopOutlined,
  FileImageOutlined,
  GatewayOutlined,
  GlobalOutlined,
  HddOutlined,
  HistoryOutlined,
  KeyOutlined,
  NodeIndexOutlined,
  ProjectOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  SafetyOutlined,
  TeamOutlined,
  UserOutlined,
} from "@ant-design/icons";
import type { ReactNode } from "react";

const ICONS: Record<string, ReactNode> = {
  // iam (набор синхронизирован с сайдбаром: Bank/Project/User/Robot/Team/
  // SafetyCertificate/Key + History для операций)
  accounts: <BankOutlined />,
  projects: <ProjectOutlined />,
  users: <UserOutlined />,
  "service-accounts": <RobotOutlined />,
  groups: <TeamOutlined />,
  roles: <SafetyCertificateOutlined />,
  "access-bindings": <KeyOutlined />,
  operations: <HistoryOutlined />,
  // vpc (сайдбар: ApartmentOutlined / ClusterOutlined / GlobalOutlined /
  // NodeIndexOutlined / SafetyOutlined / ApiOutlined / GatewayOutlined)
  networks: <ApartmentOutlined />,
  subnets: <ClusterOutlined />,
  addresses: <GlobalOutlined />,
  "route-tables": <NodeIndexOutlined />,
  "security-groups": <SafetyOutlined />,
  "network-interfaces": <ApiOutlined />,
  gateways: <GatewayOutlined />,
  // compute (сайдбар: DesktopOutlined / HddOutlined / FileImageOutlined / CameraOutlined)
  instances: <DesktopOutlined />,
  disks: <HddOutlined />,
  images: <FileImageOutlined />,
  snapshots: <CameraOutlined />,
  // admin / system
  "address-pools": <AppstoreOutlined />,
  regions: <AppstoreOutlined />,
  zones: <AppstoreOutlined />,
  // nlb (сайдбар: ApartmentOutlined / ApiOutlined / ClusterOutlined)
  "load-balancers": <ApartmentOutlined />,
  listeners: <ApiOutlined />,
  "target-groups": <ClusterOutlined />,
};

interface Props {
  specId: string;
  className?: string;
}

export function ResourceIcon({ specId, className }: Props) {
  const icon = ICONS[specId] ?? <AppstoreOutlined />;
  return (
    <span className={className} style={{ fontSize: 18, lineHeight: 1 }} aria-hidden>
      {icon}
    </span>
  );
}
