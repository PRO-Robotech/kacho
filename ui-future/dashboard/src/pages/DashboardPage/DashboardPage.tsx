import { useCallback, useEffect, useMemo, useRef, useState, type FC, type ReactNode } from "react";
import { Card, Col, Input, Row, Space, Tree } from "antd";
import type { DataNode } from "antd/es/tree";
import { ArrowRight, FolderClosed, LockKeyhole, Search } from "lucide-react";
import { SERVICE_MODULES } from "../../lib/service-modules";
import type { ServiceModule } from "../../lib/service-modules";
import { useModuleCounts } from "../../hooks/use-module-counts";
import { apiList, loadHostContext } from "../../utils";
import type { AccountRef, HostContext, ProjectRef } from "../../utils";
import { clientScope, narrowingTitle, noMatchesText, scopeSuffix } from "@shared/lib/list-scope";
// Шапка и поля страницы — ОДНА конструкция на всю консоль (канон §1, §8).
// Здесь стоял свой заголовок (`Typography.Title level={3}` плюс подпись под
// ним): кегль, вес, межбуквенное расстояние и высота блока приходили не оттуда,
// откуда у списка и карточки, и переход «главная → раздел» читался как переход
// в другой продукт.
import { PageHead, PAGE_PADDING } from "@shared/components/organisms/DetailShell/PageHead";

export interface DashboardPageProps {
  context?: HostContext;
  navigate?: (path: string) => void | Promise<void>;
}

export const DashboardPage: FC<DashboardPageProps> = ({ context, navigate = defaultNavigate }) => {
  const ctx = context ?? loadHostContext();
  const projectId = ctx.project?.id ?? null;
  const accountId = ctx.account?.id ?? null;

  // Дерево «аккаунт → проекты» c ленивой загрузкой: на старте грузятся ТОЛЬКО
  // аккаунты (быстрый первый рендер), проекты аккаунта — по раскрытию узла
  // (AntD loadData). Выбор проекта навигирует на /projects/:id/dashboard —
  // host берёт контекст из URL.
  const [accounts, setAccounts] = useState<AccountRef[]>([]);
  const [accountsLoaded, setAccountsLoaded] = useState(false);
  // Отказ чтения ОТЛИЧИМ от пустого ответа. Прежде обе ветки сходились в
  // `accounts = []`, и дерево говорило «ничего не найдено» там, где на самом
  // деле ничего не прочитано: исход «не выполнилось» зачитывался в «пусто».
  const [accountsFailed, setAccountsFailed] = useState(false);
  const [projectsByAccount, setProjectsByAccount] = useState<Record<string, ProjectRef[]>>({});
  const loadedAccounts = useRef<Set<string>>(new Set());
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<string[]>([]);
  // Область строки поиска (#373). Аккаунты и проекты читаются одной страницей
  // на тысячу строк каждый, продолжения у дерева нет — поиск судит о
  // прочитанном. `truncated` поднимается, как только хотя бы один из этих
  // ответов оставил за собой курсор.
  const [truncated, setTruncated] = useState(false);
  const scope = clientScope(truncated);

  // loadProjects — догружает проекты одного аккаунта (идемпотентно: повторно не
  // ходит). Вызывается из loadData (раскрытие) и при поиске (догрузка всех).
  const loadProjects = useCallback(async (accId: string) => {
    if (loadedAccounts.current.has(accId)) return;
    loadedAccounts.current.add(accId);
    try {
      const pr = await apiList<{
        projects?: Array<{ id: string; name?: string; accountId?: string }>;
        next_page_token?: string;
      }>("/iam/v1/projects", { account_id: accId, pageSize: "1000" });
      if (pr.next_page_token) setTruncated(true);
      const projects = (pr.projects ?? []).map((p) => ({
        id: p.id,
        name: p.name || p.id,
        accountId: p.accountId || accId,
      }));
      setProjectsByAccount((cur) => ({ ...cur, [accId]: projects }));
    } catch {
      setProjectsByAccount((cur) => ({ ...cur, [accId]: [] }));
    }
  }, []);

  // Старт — только список аккаунтов; проекты текущего аккаунта подгружаем сразу
  // (чтобы выбранный проект был виден в раскрытом узле).
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const accResp = await apiList<{
          accounts?: Array<{ id: string; name?: string }>;
          next_page_token?: string;
        }>("/iam/v1/accounts", { pageSize: "1000" });
        if (accResp.next_page_token) setTruncated(true);
        const accs = (accResp.accounts ?? []).map((a) => ({ id: a.id, name: a.name || a.id }));
        if (cancelled) return;
        setAccounts(accs);
        setAccountsLoaded(true);
        const cur = accountId ?? accs[0]?.id ?? "";
        if (cur) {
          setExpanded((prev) => (prev.length ? prev : [`acc:${cur}`]));
          void loadProjects(cur);
        }
      } catch {
        if (!cancelled) {
          setAccounts([]);
          setAccountsFailed(true);
          setAccountsLoaded(true);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const q = search.trim().toLowerCase();

  // Поиск требует проектов всех аккаунтов — догружаем их один раз, когда
  // пользователь начал искать (стартовую загрузку это не замедляет).
  useEffect(() => {
    if (!q) return;
    accounts.forEach((a) => void loadProjects(a.id));
  }, [q, accounts, loadProjects]);

  // loadData — коллбэк AntD Tree: при раскрытии узла аккаунта тянет его проекты.
  const onLoadData = useCallback(
    async (node: DataNode) => {
      const key = String(node.key);
      if (key.startsWith("acc:")) await loadProjects(key.slice(4));
    },
    [loadProjects],
  );

  // treeData для AntD Tree + авто-раскрытие совпадений при поиске. Узел аккаунта
  // без загруженных проектов оставляет children undefined (ленивый — стрелка +
  // loadData); загруженный — рендерит проекты (отфильтрованные поиском).
  const { treeData, searchExpanded } = useMemo(() => {
    const autoExpand: string[] = [];
    const data: DataNode[] = accounts
      .map((account) => {
        const accMatch = !q || account.name.toLowerCase().includes(q);
        const projects = projectsByAccount[account.id];
        const loaded = projects !== undefined;
        let children: DataNode[] | undefined;
        if (loaded) {
          const shown = projects.filter((p) => !q || accMatch || p.name.toLowerCase().includes(q));
          children = shown.map((p) => ({
            key: `prj:${p.id}`,
            isLeaf: true,
            icon: <FolderClosed size={13} />,
            title: <span className="dash-tree-prj">{highlight(p.name, q)}</span>,
          }));
          if (q && shown.length > 0) autoExpand.push(`acc:${account.id}`);
        }
        // При поиске убираем аккаунт, если ни имя, ни его (загруженные) проекты
        // не совпали.
        if (q && !accMatch && loaded && (children?.length ?? 0) === 0) return null;
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-type-assertion -- утверждение НЕСУЩЕЕ: без него узел выводится с `key: string`, тогда как `DataNode.key` — это `Key` (string | number), и предикат `.filter((n): n is DataNode …)` ниже становится недопустимым (проверено tsc: удаление даёт TS2322 и TS2677). Правило смотрит только на непосредственного получателя и о предикате ничего не знает.
        return {
          key: `acc:${account.id}`,
          selectable: false,
          isLeaf: false,
          title: <span className="dash-tree-acc">{highlight(account.name, q)}</span>,
          children,
        } as DataNode;
      })
      .filter((n): n is DataNode => n !== null);
    return { treeData: data, searchExpanded: autoExpand };
  }, [accounts, projectsByAccount, q]);

  // Загрузчик заводится КАЖДОЙ плитке витрины: без своей записи в
  // countsByModule плитка рисуется целиком, но на месте каждого числа стоит
  // прочерк — навсегда и при любом состоянии облака, неотличимо от «ресурсов
  // нет». Список поимённый, потому что это хуки: их нельзя звать циклом по
  // витрине. Полноту держит проба DashboardPage.counts.test.tsx — она проходит
  // по SERVICE_MODULES и требует числа в каждой плитке.
  const vpcCounts = useModuleCounts(findModule("vpc"), projectId);
  const computeCounts = useModuleCounts(findModule("compute"), projectId);
  const storageCounts = useModuleCounts(findModule("storage"), projectId);
  const registryCounts = useModuleCounts(findModule("registry"), projectId);
  const nlbCounts = useModuleCounts(findModule("nlb"), projectId);
  const iamCounts = useModuleCounts(findModule("iam"), accountId ?? "all", "");
  const countsByModule: Record<string, Record<string, number | null>> = {
    vpc: vpcCounts,
    compute: computeCounts,
    storage: storageCounts,
    registry: registryCounts,
    nlb: nlbCounts,
    iam: iamCounts,
  };

  const tileDisabled = (module: ServiceModule) => module.landing(projectId, accountId) == null;
  const openModule = (module: ServiceModule) => {
    const target = module.landing(projectId, accountId);
    if (target != null) void navigate(target);
  };

  // Правый слот шапки называет ОБЛАСТЬ, в границах которой показаны числа.
  const scopeLabel = ctx.project ? `Проект: ${ctx.project.name || ctx.project.id}` : "Проект не выбран";
  // ПОЛОЖЕНИЙ ТРИ, А НЕ ДВА, и третье — положение НОВОГО клиента (#1613).
  //
  // Здесь стояло деление «проект выбран · проект не выбран», и во втором прятались
  // два разных положения с разными следующими шагами. Человек, у которого нет ни
  // одного аккаунта, читал «Выберите проект в дереве слева» — а дерево слева
  // сообщало «Аккаунтов нет». Продукт велел сделать то, чего клиент сделать не
  // может, и молчал о настоящем первом шаге; пять плиток под замком читались при
  // этом как «мне сюда не выдали прав», а не как «мне надо завести аккаунт».
  //
  // Четвёртое состояние — «список ещё не прочитан» — намеренно НЕ сливается с
  // «аккаунтов нет»: пока чтение идёт, утверждать отсутствие нельзя, это было бы
  // утверждение, которого никто не проверял (та же причина, что у `navEmptyText`).
  const noAccounts = accountsLoaded && !accountsFailed && accounts.length === 0;

  // Куда ведёт первый шаг — берётся у ТОГО ЖЕ объявления, что и плитка раздела,
  // а не выписывается вторым адресом. Выписанный адрес был бы второй копией
  // координаты, которую владелец раздела уже назвал: разойдись они — плитка
  // вела бы в одно место, подсказка в другое, и заметить это можно только
  // кликом. Заодно это единственная форма, при которой переход исполним:
  // приложение, не обслуживающее `/iam`, открыть его само не может, и путь
  // существует только потому, что раздел объявил свою посадочную страницу.
  const firstStepTarget = SERVICE_MODULES.find((m) => m.segment === "iam")?.landing(null, null);

  // Подсказка под шапкой стоит ВСЕГДА и держит свою высоту (см. `.dashboard-hint`).
  // Пустая, она резервирует место: иначе выбор проекта поднимал бы всю витрину
  // на строку, и переход читался бы как прыжок.
  const hint = ctx.project
    ? ""
    : noAccounts
      ? "Аккаунт — верхний уровень Kachō: проекты, пользователи и роли живут внутри него. Начните с него."
      : "Выберите проект в дереве слева — модули открываются в его границах. IAM доступен и без проекта.";

  // Отключённая плитка ОБЪЯСНЯЕТ, почему она отключена. Замок без объяснения
  // сообщает «сюда нельзя» и умалчивает о том, что нужно сделать, чтобы стало
  // можно, — а сделать нужно ровно одно. Какое именно — зависит от положения:
  // «выберите проект» на пустом дереве неисполнимо.
  const lockReason = noAccounts
    ? "Модуль работает в границах проекта — сначала создайте аккаунт, затем проект."
    : "Модуль работает в границах проекта — выберите проект в дереве слева.";

  // Строка состояния дерева. Три исхода, и они РАЗНЫЕ: не прочитано · прочитано
  // и пусто · прочитано, но сужение ничего не дало. Последний берёт слова у
  // общего источника (`noMatchesText`): над недочитанным списком «ничего не
  // найдено» — утверждение, которого никто не проверял (#373).
  const navEmptyText = !accountsLoaded
    ? "Загрузка…"
    : accountsFailed
      ? "Список аккаунтов не загрузился"
      : accounts.length === 0
        ? // Строка дерева называет СОСТОЯНИЕ и только его. Следующий шаг живёт в
          // подсказке под шапкой, в одном месте: сказав его дважды, мы завели бы
          // два места об одном предмете, и они разошлись бы при первой правке.
          "Аккаунтов нет"
        : noMatchesText(scope);

  return (
    <section className="dashboard-console" data-testid="dashboard-page">
      <aside className="dashboard-nav">
        {/* Ручка сужения — той же геометрии, что ручки строки инструментов
            списка: высота 32, радиус 8 (канон §3). Прежде она была `small`
            (24 и 6) — единственная ручка консоли своего размера. Значок берёт
            тусклую роль палитры, а не непрозрачность: непрозрачность не меняется
            вместе с темой. */}
        <Input
          allowClear
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={`Поиск аккаунта или проекта ${scopeSuffix(scope)}`}
          title={narrowingTitle(scope)}
          prefix={<Search size={13} style={{ color: "var(--kc-text-tertiary)" }} />}
          className="dash-tree-search"
        />
        {treeData.length === 0 ? (
          <div className="dash-nav-empty">{navEmptyText}</div>
        ) : (
          <Tree
            showIcon
            blockNode
            className="dash-tree"
            treeData={treeData}
            loadData={onLoadData}
            selectedKeys={projectId ? [`prj:${projectId}`] : []}
            expandedKeys={q ? searchExpanded : expanded}
            onExpand={(keys) => setExpanded(keys as string[])}
            onSelect={(_keys, info) => {
              const key = String(info.node.key);
              if (key.startsWith("prj:")) void navigate(`/projects/${key.slice(4)}/dashboard`);
            }}
          />
        )}
      </aside>

      {/* Поля страницы — общие (`PAGE_PADDING`), а не свои. Пока их было двое,
          заголовок главной стоял на другой вертикали, чем заголовок раздела, и
          в переходе текст дёргался. */}
      <main className="dashboard-main" style={{ padding: PAGE_PADDING }}>
        <PageHead title="Сервисы облака" right={<span className="dashboard-scope">{scopeLabel}</span>} />
        <p className="dashboard-hint" data-testid={noAccounts ? "dashboard-first-step" : undefined}>
          {hint}
          {/* ХОД, А НЕ ТОЛЬКО СЛОВА. Назвать первый шаг и оставить клиента его
              искать — половина ответа: путь существует (раздел IAM открыт и без
              проекта), но с главной он назван не был. */}
          {noAccounts && firstStepTarget && (
            <>
              {" "}
              <a
                data-testid="dashboard-first-step-action"
                href={firstStepTarget}
                onClick={(e) => {
                  e.preventDefault();
                  void navigate(firstStepTarget);
                }}
              >
                Создать аккаунт
              </a>
            </>
          )}
        </p>

        {/* Здесь стояла карточка «Нет доступных проектов». Показать её было
            НЕЛЬЗЯ ни при каком состоянии: её условие требовало пустого дерева
            при непустом списке аккаунтов и пустом поиске, а дерево при пустом
            поиске строится по аккаунту на узел — то есть пусто ровно тогда,
            когда пуст сам список. Форма пустого состояния была, содержания у
            неё не было. Настоящая пустота — свойство ДЕРЕВА, и о ней говорит
            строка состояния в самом дереве (`navEmptyText`). */}

        <Row gutter={[16, 16]}>
          {SERVICE_MODULES.map((module) => {
            const disabled = tileDisabled(module);
            return (
              <Col key={module.key} xs={24} sm={24} md={12} lg={12} style={{ display: "flex" }}>
                <Card
                  hoverable={!disabled}
                  data-testid={`dashboard-tile-${module.key}`}
                  data-disabled={disabled ? "true" : "false"}
                  onClick={() => openModule(module)}
                  styles={{ body: { padding: 16 } }}
                  className={disabled ? "dashboard-tile dashboard-tile-disabled" : "dashboard-tile"}
                  title={
                    <Space>
                      <span className="dashboard-tile-icon" style={{ color: module.color }}>
                        {module.icon}
                      </span>
                      <span>{module.label}</span>
                    </Space>
                  }
                  extra={
                    disabled ? (
                      <span className="dashboard-tile-lock" title={lockReason} aria-label={lockReason}>
                        <LockKeyhole size={16} />
                      </span>
                    ) : (
                      <ArrowRight size={16} />
                    )
                  }
                >
                  {/* Чем владеет домен — ВИДНО, а не объявлено в реестре.
                      Описание жило в `SERVICE_MODULES` и не имело ни одного
                      читателя в продукте: его утверждала проба (компания compute
                      не обещает блочного хранения), а человек его не видел
                      никогда. Область фиксирована по самому длинному описанию
                      реестра — иначе ряды плиток вставали лесенкой. */}
                  <p className="dashboard-tile-about">{module.description}</p>
                  <div className="dashboard-tile-stats">
                    {module.stats.map((stat) => (
                      <div key={stat.key} className="dashboard-metric">
                        <span className="dashboard-metric-value">{countsByModule[module.key]?.[stat.key] ?? "—"}</span>
                        <span className="dashboard-metric-label" title={stat.label}>
                          {stat.label}
                        </span>
                      </div>
                    ))}
                  </div>
                </Card>
              </Col>
            );
          })}
        </Row>
      </main>
    </section>
  );
};

// highlight — подсветка совпадения поиска в названии узла.
function highlight(text: string, q: string): ReactNode {
  if (!q) return text;
  const idx = text.toLowerCase().indexOf(q);
  if (idx < 0) return text;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="dash-tree-mark">{text.slice(idx, idx + q.length)}</mark>
      {text.slice(idx + q.length)}
    </>
  );
}

function findModule(key: string): ServiceModule {
  const module = SERVICE_MODULES.find((item) => item.key === key);
  if (!module) throw new Error(`Missing service module: ${key}`);
  return module;
}

function defaultNavigate(path: string) {
  window.location.assign(path);
}

export default DashboardPage;
