import type { FC } from "react";
import type { HostContext } from "../../../utils";
import {
  activeSection,
  antdIconBySpec,
  fallbackIcon,
  iconByName,
  loadRemoteNavigation,
  remotePath,
  specIdFromPath,
  useModuleSections,
} from "../../../lib/module-navigation";
import { DOC_LINKS_BY_SPEC, MODULE_DOC_LINKS } from "../../../lib/doc-links";

/** Ширина второго уровня. Как в эталоне: рейл 62 + панель 238. */
export const MODULE_NAV_WIDTH = 238;

/**
 * Второй уровень сайдбара — ТИПЫ РЕСУРСОВ активного модуля, списком и с подписями.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЭТА ПАНЕЛЬ ЗАМЕНИЛА И ПОЧЕМУ
 *
 * Раньше её место занимали ВКЛАДКИ одного открытого ресурса (Обзор, Подсети,
 * Операции, JSON). Из-за этого перечень типов модуля жил в рейле безымянными
 * иконками, а сама панель появлялась только внутри карточки — то есть колонка,
 * на которую смотрят всё время, показывала то, что относится к одной странице.
 *
 * Теперь порядок соответствует иерархии продукта:
 *
 *   рейл  →  эта панель  →  таблица типа  →  карточка ресурса
 *   модуль   тип ресурса     строки          горизонтальные вкладки
 *
 * Вкладки ресурса переехали внутрь карточки и стали горизонтальными: они
 * принадлежат ОДНОМУ ресурсу, а не модулю, и в колонке навигации им не место.
 *
 * ПОЧЕМУ ДОКУМЕНТАЦИЯ ЗДЕСЬ
 *
 * Ссылки на документацию относятся к ТИПУ ресурса, а не к экземпляру, поэтому
 * жить им рядом с выбранным типом, а не в карточке: открыв список подсетей,
 * пользователь видит те же ссылки, что и открыв конкретную подсеть. Прежде они
 * показывались только в карточке — то есть ровно там, где человек уже разобрался,
 * и не показывались там, где он ещё выбирает.
 */
export const ModuleNav: FC<{
  context?: HostContext;
  currentPath?: string;
  navigate?: (path: string) => void | Promise<void>;
  loadNavigation?: (remote: string) => Promise<unknown>;
}> = ({
  context,
  currentPath = window.location.pathname,
  navigate = (path) => window.location.assign(path),
  loadNavigation = loadRemoteNavigation,
}) => {
  const projectId = context?.project?.id ?? null;
  const sections = useModuleSections(loadNavigation);
  const section = activeSection(sections, currentPath);

  // Модуль не выбран (дашборд, поиск) — панели нет вовсе. Пустая колонка с
  // заголовком «ничего не выбрано» занимала бы 238px, ничего не сообщая.
  if (!section || section.unavailable || section.items.length === 0) return null;

  const activeItem = section.items.find((item) => isActive(currentPath, item.path));
  const specId = activeItem ? specIdFromPath(activeItem.path) : "";
  // Порядок предпочтения — от частного к общему: ссылки самого типа (приезжают
  // из спецификации ресурса своего модуля), затем ссылки host'а по типу, затем
  // ссылки уровня модуля. Последнее — не подмена первого, а ответ на другой
  // вопрос: что читать, когда тип ещё не выбран.
  const docs = activeItem?.docs?.length
    ? activeItem.docs
    : (DOC_LINKS_BY_SPEC[specId] ?? MODULE_DOC_LINKS[section.segment] ?? []);

  return (
    <nav
      aria-label={`Ресурсы: ${section.label}`}
      style={{
        width: MODULE_NAV_WIDTH,
        minWidth: MODULE_NAV_WIDTH,
        flexShrink: 0,
        display: "flex",
        flexDirection: "column",
        minHeight: 0,
        // Фон рейла: два уровня навигации читаются одной тёмной полосой,
        // разделённой внутри линией, а не двумя разными блоками с зазором.
        background: "var(--kc-rail)",
        borderRight: "1px solid var(--kc-border)",
        padding: "20px 14px 14px",
      }}
    >
      {/* ЗДЕСЬ СТОЯЛО ИМЯ МОДУЛЯ прописными («VIRTUAL PRIVATE CLOUD») с линией
          под ним. Снято решением владельца: модуль назван уже дважды — иконкой
          рейла слева, которая подсвечена, и крошками в шапке, — а здесь имя
          набрано разрядкой и в верхнем регистре, то есть привлекает внимание
          сильнее, чем сам перечень ресурсов, ради которого колонка и заведена.

          Линия ушла вместе с ним: она отделяла заголовок от перечня, а без
          заголовка отделять нечего. Отступ сверху панель держит своим полем. */}

      <div style={{ display: "grid", gap: 3, minHeight: 0, overflowY: "auto" }}>
        {section.items.map((item) => {
          const active = isActive(currentPath, item.path);
          const disabled = !!item.requiresProject && !projectId;
          const icon = antdIconBySpec[specIdFromPath(item.path)] ?? iconByName[item.icon] ?? fallbackIcon;
          return (
            <button
              key={item.key}
              type="button"
              aria-current={active ? "page" : undefined}
              disabled={disabled}
              title={disabled ? "Выберите проект" : undefined}
              onClick={() => {
                if (!disabled) void navigate(remotePath(projectId, item.path));
              }}
              className="module-nav-link"
              data-active={active ? "true" : undefined}
            >
              <span className="module-nav-icon">{icon}</span>
              <span className="module-nav-label">{item.label}</span>
            </button>
          );
        })}
      </div>

      {docs.length > 0 && (
        <div style={{ marginTop: "auto", paddingTop: 14, paddingInline: 6, borderTop: "1px solid var(--kc-border)" }}>
          <p
            style={{
              margin: "0 0 9px",
              color: "var(--kc-text-tertiary)",
              fontSize: 9,
              fontWeight: 700,
              letterSpacing: "0.12em",
              textTransform: "uppercase",
            }}
          >
            Документация
          </p>
          {/* ТЕМЫ, А НЕ ССЫЛКИ — пока у документации нет адресов.
              
              Здесь рисовался живой якорь со стрелкой внешнего перехода, а
              адрес у всех был один и тот же — «#». Сверка дерева нашла 11
              подтверждённых рендером таких якорей (vpc 3, compute 2, storage 2,
              nlb 2, iam 2) при 44 объявлениях в дереве. Ссылка, ведущая на ту
              же страницу, обещает переход, которого не существует, и
              обнаруживает это только кликом.
              
              Побочно снят и второй дефект: ключ списка брался из адреса
              (`key={doc.href}`), а адрес у всех одинаков — то есть ключи внутри
              списка совпадали. Ключом стало название темы, а сама пара
              «подпись + адрес» схлопнута в тему: адрес не читал никто, и он
              лишь приглашал «починить» его одной живой строкой (#1611).
              
              Появится адрес сайта документации — темы станут ссылками здесь и в
              экране пустого списка, то есть в двух местах, а не в сорока
              четырёх объявлениях. */}
          {docs.map((doc) => (
            <span key={doc} className="module-nav-doc">
              {doc}
            </span>
          ))}
        </div>
      )}
    </nav>
  );
};

/** Пункт активен, когда адрес указывает на его тип — включая карточку ресурса.
 *
 *  Именно «включая»: открыв конкретную подсеть, пользователь по-прежнему
 *  находится в разделе подсетей, и подсветка обязана это показывать. Точное
 *  совпадение гасило бы её на карточке, и колонка выглядела бы так, будто
 *  ничего не выбрано. */
function isActive(pathname: string, path: string): boolean {
  if (path.startsWith("/")) return pathname === path || pathname.startsWith(`${path}/`);
  return new RegExp(`^/projects/[^/]+/${path.replace(/\//g, "\\/")}(?:/|$)`).test(pathname);
}
