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
  ContainerOutlined,
  DatabaseOutlined,
  DeploymentUnitOutlined,
  DesktopOutlined,
  FileImageOutlined,
  FolderOpenOutlined,
  GatewayOutlined,
  GlobalOutlined,
  HddOutlined,
  KeyOutlined,
  NodeIndexOutlined,
  ProjectOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  SafetyOutlined,
  TagOutlined,
  TagsOutlined,
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
  // Набор префиксов — именованный список, на который ссылаются правила: глиф
  // ярлыков, а не сети. Отличен от всех соседних по домену.
  "cidr-groups": <TagsOutlined />,
  // compute / storage (сайдбар: DesktopOutlined / HddOutlined / FileImageOutlined
  // / CameraOutlined). Ключ — идентификатор спеки, а не название раздела: здесь
  // стояли `instances` (спека называется `compute-instances`, поэтому машина
  // оставалась без иконки), `disks` и `operations` — за последними двумя нет
  // спеки ни в одном реестре консоли, а публичного `/compute/v1/disks` в стволе
  // нет вовсе. Соответствие держит ResourceIcon.registry.test.ts.
  "compute-instances": <DesktopOutlined />,
  // Группа размещения — правило взаимного размещения машин, а не сама машина:
  // глиф контейнера отличает её от инстанса и от каталога типов.
  "placement-groups": <ContainerOutlined />,
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
  // registry (OCI). Три ключа, ради которых модуль держал собственную копию
  // карты: без них реестр, репозиторий и тег получали умолчание, то есть один
  // глиф на троих — и тот же, каким помечено «иконки нет».
  //
  // Глифы выбраны РАЗНЫМИ и не отобраны у соседей: копия модуля предлагала
  // `TagsOutlined` тегу и `ContainerOutlined` репозиторию, но первый занят
  // набором префиксов, второй — группой размещения, и перенос копии как есть
  // сделал бы две пары ресурсов неразличимыми.
  registries: <DatabaseOutlined />,
  repositories: <FolderOpenOutlined />,
  tags: <TagOutlined />,
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
