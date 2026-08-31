import type { ReactNode } from "react";
import { Boxes, Cloud, HardDrive, KeyRound, Network, Scale } from "lucide-react";

export interface ModuleStat {
  key: string;
  label: string;
  listPath: string;
  payloadKey: string;
  /**
   * Идентификатор спеки в карте предметов потока (`@shared/lib/subscription/subjects`).
   *
   * Назван ЗДЕСЬ, а не выведен из `key`: ключ счётчика — имя колонки витрины
   * (`sgs`, `instances`), а идентификатор спеки — имя ресурса консоли
   * (`security-groups`, `compute-instances`), и совпадают они у девяти из
   * одиннадцати случайно. Деривация одного из другого молча промахнулась бы
   * мимо двух — а промах здесь тихий: поток откроется, словаря этого вида не
   * принесёт, покрытие не объявится, и счётчик навсегда останется на опросе,
   * выглядя при этом исправным.
   *
   * Отсутствие — не «забыли»: у домена может не быть журнала вовсе (iam), и
   * тогда счётчик остаётся на опросе ОСОЗНАННО. Обе стороны держит проба
   * `service-modules.stream.test.ts`.
   */
  specId?: string;
}

export interface ServiceModule {
  key: string;
  segment: string;
  label: string;
  icon: ReactNode;
  color: string;
  description: string;
  requiresProject?: boolean;
  landing: (projectId: string | null, accountId: string | null) => string | null;
  stats: ModuleStat[];
}

const iconSize = 16;

export const SERVICE_MODULES: ServiceModule[] = [
  {
    key: "vpc",
    segment: "vpc",
    label: "Virtual Private Cloud",
    icon: <Network size={iconSize} />,
    color: "#3D8DF5",
    description: "Облачные сети, подсети, группы безопасности, публичные IP, таблицы маршрутов.",
    requiresProject: true,
    landing: (projectId) => (projectId ? `/projects/${projectId}/vpc/networks` : null),
    stats: [
      { key: "networks", label: "Сетей", listPath: "/vpc/v1/networks", payloadKey: "networks", specId: "networks" },
      { key: "subnets", label: "Подсетей", listPath: "/vpc/v1/subnets", payloadKey: "subnets", specId: "subnets" },
      {
        key: "sgs",
        label: "Групп безопасности",
        listPath: "/vpc/v1/securityGroups",
        payloadKey: "securityGroups",
        specId: "security-groups",
      },
    ],
  },
  {
    key: "compute",
    segment: "compute",
    label: "Compute",
    icon: <Cloud size={iconSize} />,
    color: "#36CFC9",
    // Блочное хранение (тома, образы, снимки) — домен storage, не compute:
    // маршрутов /compute/v1/{disks,images,snapshots,diskTypes} в контракте нет.
    // Плитка, обещавшая их под именем Compute, отправляла человека искать
    // раздел, которого у этого домена больше нет. Сам раздел никуда не делся —
    // он ниже, отдельной плиткой Storage: обещание снято ВМЕСТЕ с выдачей
    // нового адреса, иначе живой раздел остался бы недостижим с главной.
    description: "Виртуальные машины и типы машин.",
    requiresProject: true,
    landing: (projectId) => (projectId ? `/projects/${projectId}/compute/instances` : null),
    stats: [
      {
        key: "instances",
        label: "Машин",
        listPath: "/compute/v1/instances",
        payloadKey: "instances",
        specId: "compute-instances",
      },
    ],
  },
  {
    key: "storage",
    segment: "storage",
    label: "Storage",
    icon: <HardDrive size={iconSize} />,
    color: "#13C2C2",
    description: "Блочное хранение: тома, снимки, образы и типы дисков.",
    requiresProject: true,
    landing: (projectId) => (projectId ? `/projects/${projectId}/storage/volumes` : null),
    stats: [
      { key: "volumes", label: "Томов", listPath: "/storage/v1/volumes", payloadKey: "volumes", specId: "volumes" },
      {
        key: "snapshots",
        label: "Снимков",
        listPath: "/storage/v1/snapshots",
        payloadKey: "snapshots",
        specId: "snapshots",
      },
      { key: "images", label: "Образов", listPath: "/storage/v1/images", payloadKey: "images", specId: "images" },
    ],
  },
  {
    key: "registry",
    segment: "registry",
    label: "Registry",
    icon: <Boxes size={iconSize} />,
    color: "#EB2F96",
    description: "Реестры OCI-образов: реестры, репозитории и теги.",
    requiresProject: true,
    landing: (projectId) => (projectId ? `/projects/${projectId}/registry/registries` : null),
    // Счётчик один, и это не упущение: репозитории и теги адресуются ТОЛЬКО
    // внутри конкретного реестра (/registry/v1/registries/{registryId}/...) —
    // плоского списка по проекту у них в контракте нет, а счётчик по несуществующему
    // адресу дал бы вечный прочерк, неотличимый от «их нет».
    stats: [
      {
        key: "registries",
        label: "Реестров",
        listPath: "/registry/v1/registries",
        payloadKey: "registries",
        specId: "registries",
      },
    ],
  },
  {
    key: "nlb",
    segment: "nlb",
    label: "Load Balancer",
    icon: <Scale size={iconSize} />,
    color: "#FA8C16",
    description: "Балансировка трафика TCP/UDP на четвёртом уровне: балансировщики, обработчики, целевые группы.",
    requiresProject: true,
    landing: (projectId) => (projectId ? `/projects/${projectId}/nlb/load-balancers` : null),
    stats: [
      {
        key: "load-balancers",
        label: "Балансировщиков",
        listPath: "/nlb/v1/networkLoadBalancers",
        payloadKey: "networkLoadBalancers",
        specId: "load-balancers",
      },
      {
        key: "listeners",
        label: "Обработчиков",
        listPath: "/nlb/v1/listeners",
        payloadKey: "listeners",
        specId: "listeners",
      },
      {
        key: "target-groups",
        label: "Целевых групп",
        listPath: "/nlb/v1/targetGroups",
        payloadKey: "targetGroups",
        specId: "target-groups",
      },
    ],
  },
  {
    key: "iam",
    segment: "iam",
    label: "Identity and Access Management",
    icon: <KeyRound size={iconSize} />,
    color: "#9B59F6",
    description: "Аккаунты, проекты, пользователи, сервисные аккаунты, группы, роли и связки прав.",
    landing: () => "/iam/accounts",
    // ЕДИНСТВЕННАЯ плитка без `specId` — и это решение, а не пропуск: журнала у
    // iam нет ни для одного вида, поэтому подписываться тут не на что, и её три
    // счётчика остаются на опросе при любом исходе #1632. Перечень владельцев
    // журнала объявляет `JOURNAL_OWNERS` (@shared/lib/subscription/subjects), и
    // `iam` в него не входит: назови мы предмет здесь — владелец отверг бы поток,
    // а счётчик замер бы навсегда, что со стороны неотличимо от «ресурсов нет».
    // Появится журнал у iam — три `specId` дописываются сюда, и опрос уходит
    // отсюда сам, без единой правки загрузчика.
    stats: [
      { key: "accounts", label: "Аккаунтов", listPath: "/iam/v1/accounts", payloadKey: "accounts" },
      { key: "projects", label: "Проектов", listPath: "/iam/v1/projects", payloadKey: "projects" },
      { key: "roles", label: "Ролей", listPath: "/iam/v1/roles", payloadKey: "roles" },
    ],
  },
];
