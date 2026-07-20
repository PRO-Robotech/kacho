// DetailShell — обёртка detail-страницы под YC look-and-feel.
//
// Layout (внутри Content; глобальный ServiceSidebar w=56 рисует Layout.tsx):
//   ┌─ Sub-pane w=240 ────────┬─ Main pane ────────────────────────────────┐
//   │  RESOURCE LABEL (caps)  │  [secondary action row]                    │
//   │  Name + status badges   │                                            │
//   │  ──────                 │  Active tab content (Обзор / IP-адреса …)  │
//   │  Tabs (vertical menu)   │                                            │
//   │                         │                                            │
//   │  ──────                 │                                            │
//   │  ДОКУМЕНТАЦИЯ           │                                            │
//   │  · ссылки               │                                            │
//   └─────────────────────────┴────────────────────────────────────────────┘
//
// Tab выбирается через ?tab=<id>. Дефолт — первый tab.

import { createContext, useContext, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useSearchParams } from "react-router-dom";
import { Menu, Typography, Badge } from "antd";
import { useDetailHeaderIcon } from "@/components/molecules/PanelHeader";

// Слот в правой части строки-имени (зона 3): активный таб может «поднять» свой
// тулбар (поиск/колонки/фильтры) на уровень имени ресурса через HeaderSlotPortal.
const HeaderSlotContext = createContext<HTMLElement | null>(null);

/** Рендерит children в правый слот строки-имени (зона 3) detail-страницы.
 *  Вне DetailShell (нет слота) — graceful: ничего не рендерит. Используется
 *  related-таблицами / OperationsTab, чтобы их фильтры были на уровне имени. */
export function HeaderSlotPortal({ children }: { children: ReactNode }) {
  const el = useContext(HeaderSlotContext);
  return el ? createPortal(children, el) : null;
}

export interface DetailTab {
  id: string;
  label: string;
  count?: number;
  render: () => ReactNode;
  /** Зона-2 «действие» (eyebrow) для этого таба — НЕ обязано совпадать с label
   *  меню. Default: label. Напр. json → «Информация», связанный таб → «Список». */
  eyebrow?: string;
  /** Зона-2 заголовок (тип/название предмета таба). Default: resourceLabel
   *  (тип мастер-ресурса). Напр. связанный таб «Подсети» → plural ребёнка. */
  headerTitle?: string;
  /** Зона-2 иконка предмета таба. Default: иконка мастер-ресурса (ctxIcon).
   *  Напр. связанный таб → иконка дочернего ресурса. */
  headerIcon?: ReactNode;
  /** true — контент таба заполняет область зоны-3 и скроллит СЕБЯ (таблица с
   *  фиксированной шапкой колонок + h/v-скролл тела), а не всю зону-3. Для
   *  related-таблиц. Content-табы (Обзор/JSON) — false: скроллится вся зона-3. */
  fill?: boolean;
}

export interface DocLink {
  label: string;
  href: string;
}

interface Props {
  resourceLabel: string;
  resourceName: string;
  badges?: ReactNode;
  tabs: DetailTab[];
  /** Опциональный ряд кнопок-secondary actions над content в main pane.
   *  Используется для domain-specific действий (Subnet «Перенести в зону» и т.п.). */
  secondaryActions?: ReactNode;
  docLinks?: DocLink[];
  defaultTab?: string;
  /** KAC-232: если задан — main pane (zone 3) рендерит это вместо контента
   *  активного таба. Используется для form-panel (edit / create связного
   *  ресурса разворачивается в правой зоне, табы остаются для контекста). */
  mainOverride?: ReactNode;
  /** KAC-233: controlled-режим табов (path-based вместо ?tab=). Когда задан
   *  `onTabSelect` — активный таб = `activeTabId`, клик по табу зовёт
   *  `onTabSelect(id)` (caller навигирует по path → уникальный URI на таб,
   *  и переключение таба выходит из form-panel). Иначе — legacy ?tab=. */
  activeTabId?: string;
  onTabSelect?: (id: string) => void;
  /** Действия рядом с именем ресурса в зоне 3 (Редактировать/Удалить/Создать). */
  nameActions?: ReactNode;
  /** Caps-eyebrow над именем (тип ресурса) — зеркалит eyebrow зоны-2 → симметрия. */
  nameEyebrow?: string;
  /** Override зоны-2 шапки (для форм edit/create: «Редактирование»/«Создание» +
   *  тип + иконка ресурса формы). Иначе eyebrow = label активного таба. */
  headerEyebrow?: string;
  headerTitle?: string;
  headerIcon?: ReactNode;
}

// Рейл табов: фиксированная ширина под самый длинный label/zone-2-заголовок
// (после сокращения route-table longest = «Сетевые интерфейсы»/«Группы
// безопасности» ≈175px@16 + иконка 42 + отступы). Жёстко пинуется (min=max),
// иначе в `min-width:max-content` обёртке длинный заголовок распирал бы aside →
// ширина рейла «прыгала» при смене таба (KAC-246).
const SUB_PANE_WIDTH = 288;

export function DetailShell({
  resourceName,
  badges,
  tabs,
  secondaryActions,
  docLinks,
  defaultTab,
  mainOverride,
  activeTabId,
  onTabSelect,
  nameActions,
  nameEyebrow,
  headerEyebrow,
}: Props) {
  const ctxIcon = useDetailHeaderIcon();
  const [slotEl, setSlotEl] = useState<HTMLElement | null>(null);
  const [params, setParams] = useSearchParams();
  const fallback = defaultTab ?? tabs[0]?.id ?? "overview";
  const controlled = onTabSelect !== undefined;
  const activeId = controlled ? (activeTabId ?? fallback) : (params.get("tab") ?? fallback);
  const active = tabs.find((t) => t.id === activeId) ?? tabs[0];

  const setTab = (id: string) => {
    if (controlled) {
      onTabSelect!(id);
      return;
    }
    const next = new URLSearchParams(params);
    if (id === fallback) next.delete("tab");
    else next.set("tab", id);
    setParams(next, { replace: true });
  };

  const docs = docLinks ?? DEFAULT_VPC_DOCS;

  return (
    <div
      className="kc-surface"
      style={{
        display: "flex",
        alignItems: "stretch",
        overflow: "hidden",
        // Detail-поверхность заполняет ограниченную content-область host'а
        // (header + content + footer в 100vh; .app-content overflow:hidden →
        // .vpc-remote-content flex:1 → .kc-surface height:100%). Рейл табов
        // (зона-2) и шапка зоны-3 не двигаются, скроллится только контент
        // зоны-3. Единый размер с list-поверхностью (обе height:100%).
        height: "100%",
        maxHeight: "100%",
      }}
    >
      {/* KAC-246: рейл табов — часть единой detail-поверхности. Без своего
          фона/рамки/радиуса/тени; от main отделён ТОЛЬКО вертикальным
          border-secondary. «Встроен», а не «плавает». */}
      <aside
        style={{
          width: SUB_PANE_WIDTH,
          minWidth: SUB_PANE_WIDTH,
          maxWidth: SUB_PANE_WIDTH,
          flexGrow: 0,
          flexShrink: 0,
          overflowX: "hidden",
          overflowY: "auto",
          display: "flex",
          flexDirection: "column",
          borderRight: "1px solid var(--kc-border-secondary)",
          // padding 20 — как у list kc-surface, чтобы блок [иконка+действие+тип]
          // был на той же позиции (20,20) от kc-surface и НЕ прыгал list↔detail.
          padding: 20,
        }}
      >
        {/* Зона 2 (рейл) — ИДЕНТИЧНОСТЬ ресурса: [иконка осн. ресурса] +
            ТИП(eyebrow) + имя. (Поменяно местами с контекстом таба в зоне 3.) */}
        <div
          style={{
            paddingBottom: 14,
            marginBottom: 18,
            borderBottom: "1px solid var(--kc-border-secondary)",
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 12, minWidth: 0 }}>
            {/* Бейдж основного ресурса — та же плитка-иконка, что у ContextBadge. */}
            {ctxIcon && (
              <div
                style={{
                  width: 42,
                  height: 42,
                  borderRadius: 12,
                  flexShrink: 0,
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  fontSize: 19,
                  color: "var(--kc-primary)",
                  background: "linear-gradient(135deg, rgba(61,141,245,0.16), rgba(61,141,245,0.05))",
                  border: "1px solid rgba(61,141,245,0.22)",
                }}
              >
                {ctxIcon}
              </div>
            )}
            <div style={{ minWidth: 0 }}>
              {nameEyebrow && (
                <div
                  style={{
                    fontSize: 11,
                    fontWeight: 600,
                    letterSpacing: "0.06em",
                    textTransform: "uppercase",
                    color: "var(--kc-primary)",
                    marginBottom: 2,
                    whiteSpace: "nowrap",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                  }}
                >
                  {nameEyebrow}
                </div>
              )}
              <div style={{ display: "flex", alignItems: "center", gap: 10, minWidth: 0 }}>
                {/* Размер/вес синхронизированы с ContextBadge-title зоны-3
                    (16/600/lh1.25) → типографика рейла и main идентична. */}
                <Typography.Title
                  level={3}
                  ellipsis={{ tooltip: resourceName || undefined }}
                  style={{
                    margin: 0,
                    fontSize: 16,
                    fontWeight: 600,
                    lineHeight: 1.25,
                    color: "var(--kc-text)",
                  }}
                >
                  {resourceName || "(без имени)"}
                </Typography.Title>
                {badges}
              </div>
            </div>
          </div>
        </div>

        <Menu
          mode="inline"
          selectedKeys={active ? [active.id] : []}
          onClick={({ key }) => setTab(key)}
          className="kc-detail-rail-menu"
          style={{ borderRight: "none", background: "transparent" }}
          items={tabs.map((t) => ({
            key: t.id,
            label: (
              <span
                style={{
                  display: "inline-flex",
                  justifyContent: "space-between",
                  width: "100%",
                  alignItems: "center",
                }}
              >
                <span>{t.label}</span>
                {typeof t.count === "number" && t.count > 0 && (
                  <Badge count={t.count} color="rgba(255,255,255,0.12)" overflowCount={9999} />
                )}
              </span>
            ),
          }))}
        />

        {docs.length > 0 && (
          <div
            style={{
              marginTop: "auto",
              padding: "16px 8px 8px 8px",
              borderTop: "1px solid var(--kc-border-secondary)",
            }}
          >
            <Typography.Text
              type="secondary"
              style={{
                fontSize: 11,
                textTransform: "uppercase",
                letterSpacing: 0.5,
                fontWeight: 500,
              }}
            >
              Документация
            </Typography.Text>
            <ul
              style={{
                listStyle: "none",
                padding: 0,
                margin: "8px 0 0 0",
                display: "flex",
                flexDirection: "column",
                gap: 6,
              }}
            >
              {docs.map((d) => (
                <li key={d.href}>
                  <Typography.Link
                    href={d.href}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{ fontSize: 12, lineHeight: 1.4 }}
                  >
                    {d.label}
                  </Typography.Link>
                </li>
              ))}
            </ul>
          </div>
        )}
      </aside>

      <main
        style={{
          flex: 1,
          minWidth: 0,
          padding: "20px 24px",
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
        }}
      >
        {/* Зона 3 (main) верх — ТОЛЬКО название активного таба (дубль). Структура
            ЗЕРКАЛИТ зону-2: невидимый eyebrow-спейсер (та же высота, что caps-тип
            в рейле) → title встаёт ровно на строку ИМЕНИ зоны-2 (req3). minHeight
            42 + paddingBottom 14 → нижняя линия на той же y, что у рейла (req2).
            Всё nowrap → высота фиксирована, текст/линия НЕ прыгают при смене таба
            (req1). Справа — слот фильтров. */}
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            gap: 16,
            flexWrap: "nowrap",
            // 56 (border-box) = высота блока зоны-2 (иконка 42 + paddingBottom 14)
            // → нижние линии зоны-2/зоны-3 на одной y (req2). Контент-область 42,
            // текст центрируется в ней как у зоны-2 → title на строке имени (req3).
            minHeight: 56,
            // Шапка зоны-3 фиксирована (не скроллится) — flexShrink:0.
            flexShrink: 0,
            paddingBottom: 14,
            marginBottom: 18,
            borderBottom: "1px solid var(--kc-border-secondary)",
          }}
        >
          <div style={{ minWidth: 0 }}>
            {/* невидимый eyebrow-спейсер = caps-тип зоны-2 по высоте */}
            <div
              aria-hidden
              style={{
                fontSize: 11,
                fontWeight: 600,
                letterSpacing: "0.06em",
                textTransform: "uppercase",
                marginBottom: 2,
                visibility: "hidden",
                whiteSpace: "nowrap",
              }}
            >
              {" "}
            </div>
            <Typography.Title
              level={3}
              ellipsis={{ tooltip: undefined }}
              style={{
                margin: 0,
                fontSize: 16,
                fontWeight: 600,
                lineHeight: 1.25,
                color: "var(--kc-text)",
              }}
            >
              {headerEyebrow ?? active?.headerTitle ?? active?.label}
            </Typography.Title>
          </div>
          <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "nowrap", flexShrink: 0 }}>
            {nameActions}
            {/* Слот для фильтров активного таба. */}
            <div ref={setSlotEl} style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "nowrap" }} />
          </div>
        </div>

        {/* Зона-3 контент: fill-таб (related-таблица) заполняет область и
            скроллит СЕБЯ (thead фиксирован), content-таб (Обзор/JSON/форма) —
            скроллится целиком. Внешний контейнер overflow:hidden + flex-column,
            скролл живёт во внутренней обёртке per-case. */}
        <div
          style={{ flex: 1, minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column", overflow: "hidden" }}
        >
          {mainOverride ? (
            <div style={{ flex: 1, minHeight: 0, minWidth: 0, overflow: "auto" }}>{mainOverride}</div>
          ) : (
            <>
              {secondaryActions && (
                <div
                  style={{
                    flexShrink: 0,
                    display: "flex",
                    gap: 8,
                    flexWrap: "wrap",
                    marginBottom: 16,
                    paddingBottom: 12,
                    borderBottom: "1px solid var(--kc-border-secondary)",
                  }}
                >
                  {secondaryActions}
                </div>
              )}
              <HeaderSlotContext.Provider value={slotEl}>
                {active?.fill ? (
                  <div style={{ flex: 1, minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>
                    {active.render()}
                  </div>
                ) : (
                  <div style={{ flex: 1, minHeight: 0, minWidth: 0, overflow: "auto" }}>{active?.render()}</div>
                )}
              </HeaderSlotContext.Provider>
            </>
          )}
        </div>
      </main>
    </div>
  );
}

// Дефолтные ссылки для VPC ресурсов (Kachō docs; конкретные ссылки на тип
// мастер-ресурса передаёт ResourceShell через docLinks).
const DEFAULT_VPC_DOCS: DocLink[] = [
  { label: "Начать работу с сетями и подсетями", href: "#" },
  { label: "Облачные сети и подсети", href: "#" },
  { label: "Группы безопасности", href: "#" },
  { label: "Адреса облачных ресурсов", href: "#" },
  { label: "Получить статический публичный IP-адрес", href: "#" },
];
