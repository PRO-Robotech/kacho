import { useEffect, useState } from "react";
import type { Dispatch, FC, SetStateAction } from "react";
import { Typography, theme } from "antd";
import { ChevronRight } from "lucide-react";
import { ScopePicker } from "../ScopePicker";
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
import { ENTITIES, SERVICES } from "../../../lib/entity-names";

// Метки модулей и ресурсов для хлебных крошек в шапке (как в kacho-ui):
// «<Модуль> / <ресурс>» выводится из URL. Хост держит собственную карту, т.к. по
// Module Federation не импортирует реестры remote'ов.
/**
 * Подписи разделов и ресурсов. Экспортированы, чтобы проба утверждала о САМИХ
 * картах как о значениях, а не разбирала исходник этого файла как текст:
 * текстовый разбор говорит о форме записи и переживает любую её смену молча.
 *
 * Сами подписи ВЫВОДЯТСЯ из зеркала канона (`host/src/lib/entity-names`), а не
 * выписываются здесь: до этого обработчик балансировщика назывался у хоста
 * «Слушатели», в меню модуля «Обработчики», в агрегатном меню «Listeners» — три
 * подписи одного предмета в одном продукте. Зеркало сверяется с каноном пробой
 * `HostBreadcrumb.names.test.tsx`.
 *
 * Ниже к выведенным добавлены подписи, которые сущность НЕ именуют и потому в
 * каноне не живут: разделы администрирования и агрегатная страница доступа.
 */
export const MODULE_LABELS: Record<string, string> = Object.fromEntries(
  Object.entries(SERVICES).map(([key, value]) => [key, value.title]),
);

export const RESOURCE_LABELS: Record<string, string> = {
  ...Object.fromEntries(Object.entries(ENTITIES).map(([key, value]) => [key, value.plural])),
  // Подписи не-сущностей: agregate-страница прав и разделы администрирования.
  // `/system/cluster/admins` и `/system/tokens/user-tokens` подписывает их
  // РАЗДЕЛ, потому что крошка строится по ПЕРВОМУ сегменту после `/system/`.
  //
  // `access` подписывает `/iam/access/grant` (у самой `/iam/access` звено снято
  // как последнее — см. ниже).
  //
  // ЗДЕСЬ СТОЯЛО `search: "Поиск"` — подпись без единого адреса, который её
  // произвёл бы. Звено ресурса рисуется, только когда за именем ресурса есть
  // ещё сегмент; у `/system/search` его нет ни одного (единственный маршрут —
  // `<Route path="search">`, вложенных нет), поэтому подпись не показывалась бы
  // никогда. Страница называет себя сама — `PageHead title="Поиск"`.
  access: "Управление доступом",
  cluster: "Администраторы кластера",
  tokens: "Токены и ключи",
};

// deriveCrumb — «<Модуль> / <ресурс>» из pathname. Поддержаны /iam/<res>,
// /projects/<pid>/<module>/<res>, /system/<res>. Иначе null → «Все сервисы».
//
// ─────────────────────────────────────────────────────────────────────────────
// НА СТРАНИЦЕ СПИСКА ПОСЛЕДНЕЕ ЗВЕНО НЕ ПОКАЗЫВАЕТСЯ (решение владельца)
//
// Крошки называют ПУТЬ, заголовок — ПРЕДМЕТ. На списке они говорили одно и то
// же слово: «… / Облачные сети» в крошках и «Облачные сети» заголовком двадцатью
// точками ниже и вчетверо крупнее. Пока у списка заголовка не было, крошки
// оставались единственным местом, где раздел назывался; теперь он назван.
//
// На КАРТОЧКЕ звено остаётся: там заголовок — имя экземпляра, повторения нет, а
// раздел в пути ведёт назад, к списку.
//
// Различаем по адресу: список — путь, оканчивающийся именем ресурса; карточка,
// форма и вкладка — то же плюс сегмент дальше.
function deriveCrumb(path: string): { module: string; resource?: string } | null {
  const tail = (rest: string | undefined) => (rest && rest !== "/" ? true : false);
  let m = path.match(/^\/iam\/([^/]+)(\/.*)?$/);
  if (m)
    return {
      module: SERVICES.iam.title,
      resource: tail(m[2]) ? (RESOURCE_LABELS[m[1]] ?? "Раздел") : undefined,
    };
  m = path.match(/^\/projects\/[^/]+\/([^/]+)\/([^/]+)(\/.*)?$/);
  if (m)
    return {
      module: MODULE_LABELS[m[1]] ?? m[1].toUpperCase(),
      resource: tail(m[3]) ? (RESOURCE_LABELS[m[2]] ?? "Раздел") : undefined,
    };
  m = path.match(/^\/system\/([^/]+)(\/.*)?$/);
  if (m)
    return {
      module: SERVICES.system.title,
      resource: tail(m[2]) ? (RESOURCE_LABELS[m[1]] ?? "Раздел") : undefined,
    };
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

  const pickAccount = (next: { id: string; name: string }) => {
    // Смена аккаунта ОБНУЛЯЕТ проект: проект принадлежит аккаунту, и оставить
    // прежний означало бы показать чужой. Раньше это было незаметно — второе
    // поле просто пустело после клика по первому.
    setHostContext(onChange, { account: next, project: null });
    loadProjects(next.id);
  };

  const pickProject = (next: { id: string; name: string }) => {
    if (!account) return;
    setHostContext(onChange, { account, project: { ...next, accountId: account.id } });
    void navigate(`/projects/${next.id}/dashboard`);
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
    // Крошки — 12 px и приглушённый тон: это адрес, а не заголовок. Кегль
    // проставлен здесь, а не в листе каркаса, чтобы шапка и её содержимое
    // объявлялись одним местом.
    <div className="context-breadcrumb" style={{ color: token.colorTextSecondary, fontSize: 12 }}>
      {!onDashboard && (
        <>
          {/* ОДИН выбор области вместо двух полей подряд. Два поля лгали о
              независимости выбора: проект принадлежит аккаунту, и смена
              аккаунта обнуляет проект — по двум отдельным полям этого не видно
              до клика. Панель показывает связь целиком. */}
          <ScopePicker
            account={account ? { id: account.id, name: account.name || account.id } : null}
            project={project ? { id: project.id, name: project.name || project.id } : null}
            accounts={accounts.map((a) => ({ id: a.id, name: a.name || a.id }))}
            projects={projects.map((pr) => ({ id: pr.id, name: pr.name || pr.id }))}
            // Раздел IAM — область АККАУНТА: у его ресурсов проекта нет, и
            // колонка проектов там показывала бы выбор, которого не существует.
            accountOnly={onIam}
            onAccountPick={pickAccount}
            onProjectPick={pickProject}
            onOpen={loadProjects}
          />
          {sep}
        </>
      )}
      {crumb ? (
        crumb.resource === undefined ? (
          // Список: звено раздела снято, потому что его называет заголовок
          // страницы. Модуль становится текущим звеном — полновесным.
          <Typography.Text className="breadcrumb-current" style={{ fontSize: 12, color: token.colorText }}>
            {crumb.module}
          </Typography.Text>
        ) : (
          <>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {crumb.module}
            </Typography.Text>
            {sep}
            {/* Текущее звено — единственное полновесное: цвет основного текста и
                вес 600 (из класса), остальные звенья приглушены. */}
            <Typography.Text className="breadcrumb-current" style={{ fontSize: 12, color: token.colorText }}>
              {crumb.resource}
            </Typography.Text>
          </>
        )
      ) : (
        <Typography.Text className="breadcrumb-current" style={{ fontSize: 12, color: token.colorText }}>
          Все сервисы
        </Typography.Text>
      )}
    </div>
  );
};
