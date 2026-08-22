// DetailShell — обёртка карточки ресурса под единый вид.
//
// Раскладка (внутри Content; навигацию по модулю рисует оболочка узла):
//   ┌─────────────────────────────────────────────────────────────────────┐
//   │  ТИП РЕСУРСА (надзаголовок)                                         │
//   │  Имя ресурса                     [действия ресурса · фильтры таба]  │
//   │  Обзор │ JSON │ Подсети …        ← вкладки, ГОРИЗОНТАЛЬНО           │
//   │  ───────────────────────────────────────────────────────────────────│
//   │  Содержимое активной вкладки — ЛИБО форма (`mainOverride`)          │
//   └─────────────────────────────────────────────────────────────────────┘
//
// Здесь была раскладка в ДВЕ зоны: слева вертикальный рейл вкладок со своей
// шапкой, справа содержимое. Рейл снят — колонка рядом с навигацией принадлежит
// НАВИГАЦИИ ПО МОДУЛЮ, а вкладки относятся к одному открытому ресурсу. Диаграмма
// пережила эту правку и продолжала описывать зону 2, которой оболочка не рисует.
//
// Вкладок НЕТ, когда тело занято формой (`mainOverride`) — см. примечание у
// самого набора.
//
// Tab выбирается через ?tab=<id>. Дефолт — первый tab.

import { createContext, useContext, useId, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useSearchParams } from "react-router";
import { Menu, Badge } from "antd";
import type { GetProp, MenuProps } from "antd";
import { PageHead } from "./PageHead";

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
  /** Зона-2 заголовок (тип/название предмета таба).
   *  (тип мастер-ресурса). Напр. связанный таб «Подсети» → plural ребёнка. */
  headerTitle?: string;
  /** Зона-2 иконка предмета таба. Default: иконка мастер-ресурса (ctxIcon).
   *  Напр. связанный таб → иконка дочернего ресурса. */
  headerIcon?: ReactNode;
  /** true — контент таба заполняет область зоны-3 и скроллит СЕБЯ (таблица с
   *  фиксированной шапкой колонок + h/v-скролл тела), а не всю зону-3. Для
   *  related-таблиц. Content-табы (Обзор/JSON) — false: скроллится вся зона-3. */
  fill?: boolean;
  /** CTA в ШАПКЕ страницы (правый верхний угол), показывается когда этот таб
   *  активен. Напр. таб «Привилегии» → кнопка «Выдать доступ». Рендерится через
   *  useHeaderRight в ResourceShell, не в зоне-2. */
  headerAction?: ReactNode;
}

export interface DocLink {
  label: string;
  href: string;
}

interface Props {
  resourceName: string;
  badges?: ReactNode;
  tabs: DetailTab[];
  /** Опциональный ряд кнопок-secondary actions над content в main pane.
   *  Используется для domain-specific действий (Subnet «Перенести в зону» и т.п.). */
  secondaryActions?: ReactNode;
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
  /** ЗДЕСЬ БЫЛИ `nameEyebrow` и `headerEyebrow` — надзаголовок над именем
   *  ресурса («ОБЛАЧНАЯ СЕТЬ», «Изменение»). Надзаголовки сняты по всему
   *  продукту решением владельца, и пропы сняты ВМЕСТЕ с ними, а не оставлены
   *  «на будущее»: принятое и никем не читаемое значение обещает вызывающему
   *  влияние, которого нет.
   *
   *  Режим правки называет теперь сама форма — её заголовок несёт и действие, и
   *  предмет («Изменение подсети»), поэтому сказанное здесь не потерялось. */
  headerTitle?: string;
  headerIcon?: ReactNode;
}



/** Пункт рейла — ВКЛАДКА, а не пункт меню.
 *
 *  `role`/`id`/`aria-*` объявление `items` у antd не описывает (там `key`,
 *  `label`, `disabled`, `data-*`), однако пункт кладёт остаток props на свой
 *  `<li>` — то есть поведение есть, а типа под него нет. Тип дополняется здесь и
 *  ровно на те атрибуты, которыми пользуется рейл: приведение всего набора
 *  (`as MenuProps["items"]`) сняло бы проверку заодно с `key` и `label`, и
 *  опечатка в имени атрибута уехала бы молча — вместе с ролью вкладки. */
type RailTab = GetProp<MenuProps, "items">[number] & {
  role: "tab";
  id: string;
  "aria-selected": boolean;
  "aria-controls"?: string;
};

export function DetailShell({
  resourceName,
  badges,
  tabs,
  secondaryActions,
  defaultTab,
  mainOverride,
  activeTabId,
  onTabSelect,
  nameActions,
  headerTitle,
}: Props) {
  const [slotEl, setSlotEl] = useState<HTMLElement | null>(null);
  const [params, setParams] = useSearchParams();
  const fallback = defaultTab ?? tabs[0]?.id ?? "overview";
  const controlled = onTabSelect !== undefined;
  const activeId = controlled ? (activeTabId ?? fallback) : (params.get("tab") ?? fallback);
  const active = tabs.find((t) => t.id === activeId) ?? tabs[0];

  // Шапка страницы (зона 3): заголовок — ПРЕДМЕТ, надзаголовок — действие или
  // контекст над ним. Оба берутся у активной вкладки, если оболочке не названы
  // свои (режим формы: «Редактирование» над типом ресурса). До этого `headerTitle`
  // был объявлен пропом и не читался ни одной строкой: вызывающий его передавал,
  // а шапка показывала вместо него надзаголовок — принято и проигнорировано.
  // Заголовок страницы — ИМЯ РЕСУРСА, и оно не меняется при смене вкладки:
  // предмет страницы — сам ресурс, а вкладка лишь выбирает, что о нём показать.
  // Прежде здесь стояло имя активной вкладки, и страница называла себя «Обзор» —
  // то есть сообщала способ просмотра вместо того, на что смотрят. Переопределение
  // (`headerTitle`) остаётся за формами: у «Создания» ресурса ещё нет имени.
  //
  // Пустое имя подписывается ЯВНО. Имени, которого нет, у ресурса не бывает
  // (сервер проставляет производное от `id`), но ответ края читается ДО того,
  // как это стало нормой, и на старой строке имя приходит пустым. Пустой
  // заголовок читается как «не загрузилось»: страница выглядит сломанной там,
  // где сломан один ресурс. Подстановка стоит ПОСЛЕ переопределения формы —
  // у «Создания» имени ещё нет, и там заголовок называет тип, а не прочерк.
  const headTitle = headerTitle ?? (resourceName || "(без имени)");
  // Связь «вкладка ↔ её панель» — по идентификаторам узлов. Префикс уникален на
  // экземпляр оболочки: карточка на странице одна, но в пробах их рисуют рядом, и
  // совпавшие идентификаторы связали бы вкладку с чужой панелью.
  const domPrefix = useId();
  const tabDomId = (id: string) => `${domPrefix}tab-${id}`;
  const panelDomId = `${domPrefix}panel`;

  const setTab = (id: string) => {
    if (controlled) {
      onTabSelect(id);
      return;
    }
    const next = new URLSearchParams(params);
    if (id === fallback) next.delete("tab");
    else next.set("tab", id);
    setParams(next, { replace: true });
  };


  return (
    <div
      className="kc-surface"
      style={{
        display: "flex",
        flexDirection: "column",
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

      <main
        style={{
          flex: 1,
          minWidth: 0,
          minHeight: 0,
          padding: "20px 24px",
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
        }}
      >
        {/* Шапка страницы — ОБЩАЯ с листом ресурсов (`PageHead`): один и тот же
            предмет (имя того, на что смотрят) обязан выглядеть одинаково, где бы
            на него ни смотрели. Довод про равную высоту с зоной 2 снят вместе с
            самой зоной: сшивать больше нечего. */}
        <PageHead
          title={headTitle}
          badges={badges}
          // Линия под именем — ТОЛЬКО когда вкладок нет.
          //
          // При вкладках её роль исполняет их полоса: две линии в двух десятках
          // точек друг от друга читаются как пустая полоса между ними, и имя
          // ресурса отрывается от своих же вкладок.
          //
          // А вот когда тело занято формой (`mainOverride`), вкладок нет — и без
          // линии заголовок оставался висеть над полями, а всё содержимое
          // поднималось на высоту снятой полосы. В переходе «карточка → правка»
          // это читалось как прыжок заголовка: сам он стоит на месте, съезжает
          // то, что под ним. Линия занимает место полосы и держит ритм страницы.
          divider={!!mainOverride}
          right={
            <>
              {/* ПОРЯДОК ТОТ ЖЕ, ЧТО В СТРОКЕ ИНСТРУМЕНТОВ СПИСКА: сузить →
                  выбрать, что показывать → сделать. Слот фильтров активной
                  вкладки идёт первым, действия — последними.

                  Прежде действия стояли ПЕРЕД слотом, и кнопка «Создать» на
                  вкладке дочернего ресурса оказывалась в начале ряда фильтров —
                  тогда как на странице списка того же ресурса она стоит в
                  конце. Один и тот же набор ручек читался как два разных. */}
              <div ref={setSlotEl} style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "nowrap" }} />
              {nameActions}
            </>
          }
        />

        {/* Вкладки ресурса — ГОРИЗОНТАЛЬНЫЕ и внутри карточки.
            Прежде они стояли вертикальным рейлом слева, занимая колонку, которая
            в иерархии продукта принадлежит НАВИГАЦИИ ПО МОДУЛЮ: вкладки относятся
            к одному открытому ресурсу, а колонка рядом с рейлом — ко всему
            разделу. Из-за этого перечень типов модуля жил безымянными иконками в
            рейле, а колонка появлялась только внутри карточки.

            Набор объявлен вкладками, а не меню (#627): он ПЕРЕКЛЮЧАЕТ ВИД в теле
            карточки, а не запускает команды, и выбранный вид обязан быть помечен
            состоянием, а не только подсветкой. Пока роли не было, читающий
            страницу не глазами получал список команд без единого признака
            открытого вида, а сквозная проба не находила на карточке ни одной
            вкладки — при том что вкладка строилась и рисовалась. */}
        {/* Вкладок НЕТ, когда тело занято формой (`mainOverride`).
            Вкладка переключает ВИД на ресурс, а форма — не вид: она правит его.
            Оставленные рядом, вкладки предлагали уйти с недозаполненной формы,
            ничего не сказав о судьбе введённого, и при этом ни одна из них не
            была выбрана — набор без выбранного пункта читается как сломанный. */}
        {!mainOverride && (
        <Menu
          mode="horizontal"
          role="tablist"
          aria-orientation="horizontal"
          selectedKeys={active ? [active.id] : []}
          onClick={({ key }) => setTab(key)}
          className="kc-detail-tabs"
          style={{ background: "transparent", fontSize: 12, lineHeight: "38px" }}
          items={tabs.map((t): RailTab => ({
            key: t.id,
            role: "tab",
            id: tabDomId(t.id),
            "aria-selected": t.id === active?.id,
            "aria-controls": panelDomId,
            // Панель у вкладки есть ВСЕГДА, пока сама вкладка нарисована: набор
            // рисуется только там, где тело карточки показывает содержимое
            // вкладки (см. условие выше). Здесь стояла ветвь «в режиме формы
            // ссылки нет» — она пережила свой предмет: с тех пор как вкладки
            // сняты со страницы формы целиком, `mainOverride` в этой точке ложен
            // by construction, и ветвь описывала состояние, которого код не
            // производит.
            label: (
              <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
                <span>{t.label}</span>
                {typeof t.count === "number" && t.count > 0 && (
                  // Форма счётчика — тег: поле, линия, моноширинная цифра.
                  <Badge
                    count={t.count}
                    overflowCount={9999}
                    style={{
                      background: "var(--kc-field)",
                      border: "1px solid var(--kc-border)",
                      color: "var(--kc-text-secondary)",
                      boxShadow: "none",
                      borderRadius: 5,
                      minWidth: 20,
                      height: 18,
                      lineHeight: "16px",
                      padding: "0 5px",
                      fontFamily: "var(--font-mono)",
                      fontSize: 10,
                      fontWeight: 540,
                    }}
                  />
                )}
              </span>
            ),
          }))}
        />
        )}

        {/* Тело карточки: fill-таб (related-таблица) заполняет область и
            скроллит СЕБЯ (thead фиксирован), content-таб (Обзор/JSON/форма) —
            скроллится целиком. Внешний контейнер overflow:hidden + flex-column,
            скролл живёт во внутренней обёртке per-case. */}
        <div
          // Панель активной вкладки — и названа она своей вкладкой, чтобы
          // «что я сейчас читаю» отвечалось без разглядывания подсветки.
          // В режиме формы зона 3 занята НЕ содержимым вкладки: роли панели у неё
          // нет (иначе форма выдавала бы себя за вид вкладки), и ссылки на панель
          // у вкладок в этом режиме тоже нет — см. `aria-controls` выше.
          {...(mainOverride
            ? {}
            : { role: "tabpanel", id: panelDomId, "aria-labelledby": active ? tabDomId(active.id) : undefined })}
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

