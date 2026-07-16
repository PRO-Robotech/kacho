// ResourceIcon — иконка ресурса для заголовков модалок Create/Edit.
// Mapping синхронизирован с навигацией в сайдбаре (см. src/lib/service-modules.tsx)
// — те же AntD Outlined-иконки, чтобы пользователь узнавал ресурс в обоих местах.

import {
  ApartmentOutlined,
  ApiOutlined,
  AppstoreOutlined,
  CameraOutlined,
  ClusterOutlined,
  ContainerOutlined,
  DatabaseOutlined,
  DesktopOutlined,
  FileImageOutlined,
  GatewayOutlined,
  GlobalOutlined,
  HddOutlined,
  NodeIndexOutlined,
  SafetyOutlined,
  TagsOutlined,
  UserOutlined,
} from "@ant-design/icons";
import type { ReactNode } from "react";

const ICONS: Record<string, ReactNode> = {
  // iam (сайдбар: ApartmentOutlined / ClusterOutlined / UserOutlined /
  // ApiOutlined / NodeIndexOutlined / SafetyOutlined / GatewayOutlined)
  accounts: <ApartmentOutlined />,
  projects: <ClusterOutlined />,
  users: <UserOutlined />,
  "service-accounts": <ApiOutlined />,
  groups: <NodeIndexOutlined />,
  roles: <SafetyOutlined />,
  "access-bindings": <GatewayOutlined />,
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
  // registry (сайдбар: DatabaseOutlined; репозитории/теги — дочерние табы)
  registries: <DatabaseOutlined />,
  repositories: <ContainerOutlined />,
  tags: <TagsOutlined />,
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
