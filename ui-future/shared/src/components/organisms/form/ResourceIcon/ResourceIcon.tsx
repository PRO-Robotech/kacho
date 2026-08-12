// ResourceIcon — иконка ресурса для заголовков модалок Create/Edit.
// Mapping синхронизирован с навигацией в сайдбаре (см. src/lib/service-modules.tsx)
// — те же AntD Outlined-иконки, чтобы пользователь узнавал ресурс в обоих местах.

import {
  ApartmentOutlined,
  ApiOutlined,
  AppstoreOutlined,
  BankOutlined,
  CameraOutlined,
  CloudServerOutlined,
  ClusterOutlined,
  DeploymentUnitOutlined,
  DesktopOutlined,
  FileImageOutlined,
  GatewayOutlined,
  GlobalOutlined,
  HddOutlined,
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
  // vpc (сайдбар: ApartmentOutlined / ClusterOutlined / GlobalOutlined /
  // NodeIndexOutlined / SafetyOutlined / ApiOutlined / GatewayOutlined)
  networks: <ApartmentOutlined />,
  subnets: <ClusterOutlined />,
  addresses: <GlobalOutlined />,
  "route-tables": <NodeIndexOutlined />,
  "security-groups": <SafetyOutlined />,
  "network-interfaces": <ApiOutlined />,
  gateways: <GatewayOutlined />,
  // compute / storage (сайдбар: DesktopOutlined / HddOutlined / FileImageOutlined
  // / CameraOutlined). Ключ — идентификатор спеки, а не название раздела: здесь
  // стояли `instances` (спека называется `compute-instances`, поэтому машина
  // оставалась без иконки), `disks` и `operations` — за последними двумя нет
  // спеки ни в одном реестре консоли, а публичного `/compute/v1/disks` в стволе
  // нет вовсе. Соответствие держит ResourceIcon.registry.test.ts.
  "compute-instances": <DesktopOutlined />,
  volumes: <HddOutlined />,
  images: <FileImageOutlined />,
  snapshots: <CameraOutlined />,
  // admin / system
  "address-pools": <AppstoreOutlined />,
  // Регион и зона — РАЗНЫЕ координаты размещения, поэтому и глифы разные:
  // прежде оба несли AppstoreOutlined, который вдобавок служит умолчанием
  // для незнакомого ресурса — то есть по иконке они не отличались ни друг
  // от друга, ни от «иконки нет». Регион — совокупность площадок,
  // зона — одна площадка внутри него.
  regions: <DeploymentUnitOutlined />,
  zones: <CloudServerOutlined />,
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
