import { useEffect, useState } from "react";
import type { Dispatch, FC, SetStateAction } from "react";
import { Dropdown, Typography, theme } from "antd";
import type { MenuProps } from "antd";
import { ChevronRight } from "lucide-react";
import { BreadcrumbPill } from "../../atoms";
import {
  getAccount,
  getProject,
  listAccounts,
  listProjects,
  setHostContext,
  type AccountRef,
  type HostContext,
  type ProjectRef,
} from "../../../utils";

// Метки модулей и ресурсов для хлебных крошек в шапке (как в kacho-ui):
// «<Модуль> / <ресурс>» выводится из URL. Хост держит собственную карту, т.к. по
// Module Federation не импортирует реестры remote'ов.
const MODULE_LABELS: Record<string, string> = {
  iam: "IAM",
  vpc: "VPC",
  compute: "Compute",
  nlb: "NLB",
  system: "Администрирование",
};

const RESOURCE_LABELS: Record<string, string> = {
  // iam
  accounts: "Аккаунты",
  projects: "Проекты",
  "service-accounts": "Сервисные аккаунты",
  users: "Пользователи",
  groups: "Группы",
  roles: "Роли",
  "access-bindings": "Привязки доступа",
  operations: "Операции",
  access: "Управление доступом",
  // vpc
  networks: "Облачные сети",
  subnets: "Подсети",
  "security-groups": "Группы безопасности",
  "route-tables": "Таблицы маршрутов",
  addresses: "Адреса",
  gateways: "Шлюзы",
  "network-interfaces": "Сетевые интерфейсы",
  "address-pools": "Пулы адресов",
  "anycast-address-pools": "Anycast-пулы",
  // compute
  instances: "Инстансы",
  disks: "Диски",
  images: "Образы",
  snapshots: "Снимки",
  "disk-types": "Типы дисков",
  regions: "Регионы",
  zones: "Зоны",
  // nlb
  "load-balancers": "Балансировщики",
  listeners: "Слушатели",
  "target-groups": "Целевые группы",
};

// deriveCrumb — «<Модуль> / <ресурс>» из pathname. Поддержаны /iam/<res>,
// /projects/<pid>/<module>/<res>, /system/<res>. Иначе null → «Все сервисы».
function deriveCrumb(path: string): { module: string; resource: string } | null {
  let m = path.match(/^\/iam\/([^/]+)/);
  if (m) return { module: "IAM", resource: RESOURCE_LABELS[m[1]] ?? "Раздел" };
  m = path.match(/^\/projects\/[^/]+\/([^/]+)\/([^/]+)/);
  if (m) return { module: MODULE_LABELS[m[1]] ?? m[1].toUpperCase(), resource: RESOURCE_LABELS[m[2]] ?? "Раздел" };
  m = path.match(/^\/system\/([^/]+)/);
  if (m) return { module: "Администрирование", resource: RESOURCE_LABELS[m[1]] ?? "Раздел" };
  return null;
}

export const HostBreadcrumb: FC<{
  context: HostContext;
  onChange: Dispatch<SetStateAction<HostContext>>;
  navigate?: (path: string) => void | Promise<void>;
}> = ({ context, onChange, navigate = (path) => window.history.pushState(null, "", path) }) => {
  const { token } = theme.useToken();
  const account = context.account;
  const project = context.project;
  const [accounts, setAccounts] = useState<AccountRef[]>([]);
  const [projects, setProjects] = useState<ProjectRef[]>([]);
  const [projectsLoadedFor, setProjectsLoadedFor] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const r = await listAccounts({ pageSize: "1000" });
        if (!cancelled) {
          setAccounts((r.accounts ?? []).map((item) => ({ id: item.id, name: item.name || item.id })));
        }
      } catch {
        if (!cancelled) setAccounts([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!context.project?.id || context.project.name) return;
    const currentProject = context.project;
    const currentAccount = context.account;
    let cancelled = false;
    void (async () => {
      try {
        const p = await getProject(currentProject.id);
        if (cancelled) return;
        const accountId = p.account_id ?? p.accountId ?? currentProject.accountId;
        setHostContext(onChange, {
          account:
            currentAccount?.id === accountId ? currentAccount : { id: accountId, name: currentAccount?.name ?? "" },
          project: { id: p.id, name: p.name || p.id, accountId },
        });
      } catch {
        // Hydration is best-effort; dropdown lists will still populate names.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [context.account, context.project, onChange]);

  useEffect(() => {
    if (!context.account?.id || context.account.name) return;
    const currentAccount = context.account;
    const currentProject = context.project;
    let cancelled = false;
    void (async () => {
      try {
        const a = await getAccount(currentAccount.id);
        if (cancelled) return;
        setHostContext(onChange, {
          account: { id: a.id, name: a.name || a.id },
          project: currentProject,
        });
      } catch {
        // Hydration is best-effort; account list load will still populate names.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [context.account, context.project, onChange]);

  const loadProjects = (accountId: string) => {
    if (projectsLoadedFor === accountId) return;
    void (async () => {
      try {
        const r = await listProjects({ account_id: accountId, pageSize: "1000" });
        setProjects(
          (r.projects ?? []).map((item) => ({
            id: item.id,
            name: item.name || item.id,
            accountId: item.account_id ?? item.accountId ?? accountId,
          })),
        );
        setProjectsLoadedFor(accountId);
      } catch {
        setProjects([]);
        setProjectsLoadedFor(accountId);
      }
    })();
  };

  const accountMenu: MenuProps = {
    items: accounts.length
      ? accounts.map((item) => ({ key: item.id, label: item.name }))
      : [{ key: "__empty", label: "Аккаунтов нет", disabled: true }],
    selectedKeys: context.account ? [context.account.id] : [],
    onClick: ({ key }) => {
      const nextAccount = accounts.find((item) => item.id === key);
      if (nextAccount) setHostContext(onChange, { account: nextAccount, project: null });
    },
  };

  const projectMenu: MenuProps = {
    items: projects.length
      ? projects.map((item) => ({ key: item.id, label: item.name }))
      : [{ key: "__empty", label: account ? "Проектов нет" : "Выберите аккаунт", disabled: true }],
    selectedKeys: context.project ? [context.project.id] : [],
    onClick: ({ key }) => {
      const nextProject = projects.find((item) => item.id === key);
      if (nextProject && account) {
        setHostContext(onChange, { account, project: nextProject });
        void navigate(`/projects/${nextProject.id}/dashboard`);
      }
    },
  };

  const sep = <ChevronRight size={14} strokeWidth={2} className="breadcrumb-separator" aria-hidden />;

  // На дашборде account/project выбираются в левой панели лендинга → верхние
  // pill-селекторы не дублируем. На остальных страницах они остаются единственным
  // способом сменить контекст. (re-render на навигации — pathname актуален.)
  const path = typeof window !== "undefined" ? window.location.pathname : "";
  const onDashboard = path === "/dashboard" || /^\/projects\/[^/]+\/dashboard\/?$/.test(path);
  // Раздел IAM — account-scoped (аккаунты/проекты/SA/пользователи/группы/роли/
  // связки/операции), проекта у этих ресурсов нет → project-пилюля не показывается
  // (иначе селектор выглядит «не до конца заполненным»). Остаётся аккаунт.
  const onIam = /^\/iam(\/|$)/.test(path);
  // Хлебные крошки в самой шапке (как в kacho-ui): «<Модуль> / <ресурс>» для всех
  // модулей (IAM / VPC / Compute / NLB / Администрирование), выведено из URL.
  const crumb = deriveCrumb(path);

  return (
    <div className="context-breadcrumb" style={{ color: token.colorTextSecondary }}>
      {!onDashboard && (
        <>
          <Dropdown menu={accountMenu} trigger={["click"]} placement="bottomLeft">
            <BreadcrumbPill token={token} active={!!account} placeholder="Выберите аккаунт" chevron>
              {account?.name || account?.id}
            </BreadcrumbPill>
          </Dropdown>
          {sep}
          {!onIam && (
            <>
              <Dropdown
                menu={projectMenu}
                trigger={["click"]}
                placement="bottomLeft"
                disabled={!account}
                onOpenChange={(open) => {
                  if (open && account) loadProjects(account.id);
                }}
              >
                <BreadcrumbPill token={token} active={!!project} placeholder="Проект" chevron>
                  {project?.name || project?.id}
                </BreadcrumbPill>
              </Dropdown>
              {sep}
            </>
          )}
        </>
      )}
      {crumb ? (
        <>
          <Typography.Text type="secondary">{crumb.module}</Typography.Text>
          {sep}
          <Typography.Text className="breadcrumb-current">{crumb.resource}</Typography.Text>
        </>
      ) : (
        <Typography.Text className="breadcrumb-current">Все сервисы</Typography.Text>
      )}
    </div>
  );
};
