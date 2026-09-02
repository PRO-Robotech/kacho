import { useMemo, useState } from "react";
import type { FC } from "react";
import { Dropdown, Empty, Input } from "antd";
import { Check, ChevronRight, Search } from "lucide-react";

export interface ScopeRef {
  id: string;
  name: string;
}

interface Props {
  account: ScopeRef | null;
  project: ScopeRef | null;
  accounts: ScopeRef[];
  projects: ScopeRef[];
  /** Проект в этой области не выбирается (раздел IAM — область аккаунта). */
  accountOnly?: boolean;
  onAccountPick: (account: ScopeRef) => void;
  onProjectPick: (project: ScopeRef) => void;
  /** Зовётся при раскрытии — потребитель подгружает проекты аккаунта. */
  onOpen?: (accountId: string) => void;
}

/**
 * Единый выбор рабочей области: аккаунт и проект в ОДНОЙ панели.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ОДИН СЕЛЕКТОР, А НЕ ДВА
 *
 * Прежде их было два подряд, и они лгали о независимости выбора: проект
 * принадлежит аккаунту, поэтому смена аккаунта обнуляет проект — но по двум
 * отдельным полям этого не видно, и пользователь узнавал об этом уже после
 * клика, обнаружив второе поле пустым.
 *
 * Одна панель показывает ЦЕЛОЕ: слева аккаунты, справа проекты выбранного.
 * Связь «аккаунт → проект» видна до выбора, а не выясняется опытным путём.
 *
 * ПОЧЕМУ ПОИСК ПОЯВЛЯЕТСЯ НЕ ВСЕГДА
 *
 * Поле поиска над списком из трёх строк — лишний шаг: искать в нём нечего, а
 * место и внимание оно занимает. Порог назван числом (`SEARCH_FROM`), а не
 * «когда список длинный».
 */
const SEARCH_FROM = 7;
const COLUMN_WIDTH = 232;

export const ScopePicker: FC<Props> = ({
  account,
  project,
  accounts,
  projects,
  accountOnly = false,
  onAccountPick,
  onProjectPick,
  onOpen,
}) => {
  const [open, setOpen] = useState(false);
  const [accountQuery, setAccountQuery] = useState("");
  const [projectQuery, setProjectQuery] = useState("");

  const shownAccounts = useMemo(() => filter(accounts, accountQuery), [accounts, accountQuery]);
  const shownProjects = useMemo(() => filter(projects, projectQuery), [projects, projectQuery]);

  const panel = (
    <div
      // Панель — поверхность продукта, а не «выпадашка библиотеки»: та же
      // заливка, линия и радиус, что у прочих поверхностей.
      style={{
        display: "flex",
        background: "var(--kc-elevated)",
        border: "1px solid var(--kc-border)",
        borderRadius: 11,
        boxShadow: "var(--kc-shadow-md)",
        overflow: "hidden",
      }}
    >
      <Column
        title="Аккаунт"
        width={COLUMN_WIDTH}
        query={accountQuery}
        onQuery={accounts.length >= SEARCH_FROM ? setAccountQuery : undefined}
        empty="Аккаунтов нет"
        items={shownAccounts}
        selectedId={account?.id ?? null}
        // Стрелка у аккаунта означает «ведёт дальше, к его проектам».
        trailing={accountOnly ? undefined : "chevron"}
        onPick={(item) => {
          onAccountPick(item);
          setProjectQuery("");
          onOpen?.(item.id);
          // Панель НЕ закрывается: выбран аккаунт — теперь выбирают проект.
          // Закрытие здесь заставляло бы открывать её второй раз подряд.
          if (accountOnly) setOpen(false);
        }}
      />
      {!accountOnly && (
        <Column
          title="Проект"
          width={COLUMN_WIDTH}
          bordered
          query={projectQuery}
          onQuery={projects.length >= SEARCH_FROM ? setProjectQuery : undefined}
          empty={account ? "Проектов нет" : "Сначала выберите аккаунт"}
          items={account ? shownProjects : []}
          selectedId={project?.id ?? null}
          onPick={(item) => {
            onProjectPick(item);
            setOpen(false);
          }}
        />
      )}
    </div>
  );

  // ПОДПИСЬ КНОПКИ СОБИРАЕТСЯ ЗДЕСЬ, И БОЛЬШЕ НИГДЕ.
  //
  // Рядом стояло второе её выражение — экспортированная функция с объяснением
  // «вынесено ради потребителя: подпись собирают и пробы». Потребителей у неё за
  // всю жизнь было НОЛЬ: во всём отслеживаемом дереве имя встречалось только в
  // собственном объявлении (#1441 п.1). То есть комментарий назначал
  // потребителя, которого не существовало, а выражений подписи стало два.
  //
  // Одноимённое `scopeLabel` в дереве осталось — ДВА, и оба ЛОКАЛЬНЫЕ, то есть
  // потребителем здешнего быть не могли by construction: строка «Проект: …» в
  // правом слоте шапки сводки (`dashboard`) и функция области ПРЕДЕЛА на
  // странице пределов (`shared`). Здесь стояло «одноимённые функции двух других
  // пакетов — про область предела»: верно ровно для второго из двух, а первое —
  // не функция и не про предел. Перемеряется `git grep -n scopeLabel -- ui-future`.
  //
  // Пробе экспорт и не нужен: сквозная проба утверждает НАБЛЮДАЕМОЕ — что
  // человек читает на кнопке, — а не то, чем консоль эту строку собрала
  // (`ui.md` правило 12). Понадобится второму месту — оно назовётся здесь.
  const label = account
    ? accountOnly || !project
      ? account.name
      : `${account.name} / ${project.name}`
    : "Выберите область";

  return (
    <Dropdown
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next && account) onOpen?.(account.id);
        if (!next) {
          setAccountQuery("");
          setProjectQuery("");
        }
      }}
      trigger={["click"]}
      placement="bottomLeft"
      popupRender={() => panel}
    >
      <button type="button" className="scope-picker-trigger" aria-haspopup="menu" aria-expanded={open}>
        <span className="scope-picker-label">{label}</span>
        <ChevronRight size={13} className="scope-picker-chevron" aria-hidden />
      </button>
    </Dropdown>
  );
};

function filter(items: ScopeRef[], query: string): ScopeRef[] {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  // Ищем и по имени, и по идентификатору: имя меняется, идентификатор нет, и
  // человек, пришедший по ссылке из тикета, знает именно его.
  return items.filter((i) => i.name.toLowerCase().includes(q) || i.id.toLowerCase().includes(q));
}

const Column: FC<{
  title: string;
  width: number;
  bordered?: boolean;
  query: string;
  /** Не задан — поиска у колонки нет (список короткий). */
  onQuery?: (value: string) => void;
  empty: string;
  items: ScopeRef[];
  selectedId: string | null;
  trailing?: "chevron";
  onPick: (item: ScopeRef) => void;
}> = ({ title, width, bordered, query, onQuery, empty, items, selectedId, trailing, onPick }) => (
  <div
    style={{
      width,
      minWidth: width,
      display: "flex",
      flexDirection: "column",
      borderLeft: bordered ? "1px solid var(--kc-border)" : undefined,
    }}
  >
    <div className="scope-picker-title">{title}</div>
    {onQuery && (
      <div style={{ padding: "0 8px 8px" }}>
        <Input
          size="small"
          allowClear
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder="Имя или идентификатор"
          prefix={<Search size={13} />}
        />
      </div>
    )}
    <div className="scope-picker-list">
      {items.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={empty} style={{ margin: "12px 0" }} />
      ) : (
        items.map((item) => (
          <button
            key={item.id}
            type="button"
            className="scope-picker-item"
            data-selected={item.id === selectedId ? "true" : undefined}
            onClick={() => onPick(item)}
          >
            <span className="scope-picker-item-name">{item.name}</span>
            {item.id === selectedId ? (
              <Check size={14} aria-hidden />
            ) : (
              trailing === "chevron" && <ChevronRight size={13} className="scope-picker-item-more" aria-hidden />
            )}
          </button>
        ))
      )}
    </div>
  </div>
);
