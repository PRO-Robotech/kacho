// TableToolbar — переиспользуемые элементы тулбара таблиц:
//   • поиск по имени/идентификатору (controlled);
//   • подпись к числу показанных строк;
//   • шестерёнка-конфигуратор видимости колонок (persist в localStorage).
//
// Используется во встроенных таблицах дочерних ресурсов (ResourceShell) и
// рассчитан на переиспользование на странице-списке (ResourceListPage).

import { useState } from "react";
import { Button, Checkbox, Dropdown, Input } from "antd";
import { SearchOutlined, SettingOutlined } from "@ant-design/icons";
import { narrowingTitle, searchPlaceholder, type NarrowingScope } from "@shared/lib/list-scope";

export interface ToggleCol {
  key: string;
  label: string;
}

/** Видимость колонок с persist в localStorage по ключу. Возвращает множество
 *  СКРЫТЫХ ключей + toggler. */
export function useHiddenColumns(storageKey: string): [Set<string>, (key: string, hidden: boolean) => void] {
  const [hidden, setHidden] = useState<Set<string>>(() => {
    try {
      const raw = localStorage.getItem(storageKey);
      return new Set(raw ? (JSON.parse(raw) as string[]) : []);
    } catch {
      return new Set<string>();
    }
  });

  const toggle = (key: string, h: boolean) => {
    setHidden((prev) => {
      const next = new Set(prev);
      if (h) next.add(key);
      else next.delete(key);
      try {
        localStorage.setItem(storageKey, JSON.stringify([...next]));
      } catch {
        /* localStorage недоступен — игнорируем persist */
      }
      return next;
    });
  };

  return [hidden, toggle];
}

/**
 * TableSearch — controlled поиск-инпут с иконкой.
 *
 * `scope` ОБЯЗАТЕЛЕН и без умолчания (#373): одна и та же строка ввода означает
 * на разных страницах разное, и молчаливое умолчание выдавало бы сужение
 * прочитанной части за поиск по списку. Пользователь читает пустой ответ как
 * утверждение «такого нет» — а над недочитанным списком это утверждение никем
 * не проверено.
 *
 * `placeholder` остаётся необязательным перекрытием для ресурсов, у которых
 * ищут не по имени (почта, идентификатор): подпись области при этом никуда не
 * девается — она в `title`.
 */
export function TableSearch({
  value,
  onChange,
  scope,
  placeholder,
  width = 260,
}: {
  value: string;
  onChange: (v: string) => void;
  scope: NarrowingScope;
  placeholder?: string;
  width?: number;
}) {
  return (
    <Input
      // Поле ПОИСКА, а не просто текстовое: тип объявляет роль `searchbox`,
      // которую читает программа чтения с экрана, и даёт браузеру его
      // собственную очистку по Escape. Прежде список рисовал `Input.Search`
      // antd — он этот тип производит; при сведении ручек к одной общей
      // семантика потерялась молча, и поле осталось обычным текстовым.
      type="search"
      allowClear
      // Иконка — тусклая роль палитры, а не «серый вообще»: в светлой теме
      // прежний запасной серый был единственным цветом, не менявшимся с темой.
      prefix={<SearchOutlined style={{ color: "var(--kc-text-tertiary)" }} />}
      placeholder={placeholder ?? searchPlaceholder(scope)}
      title={narrowingTitle(scope)}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      style={{ width }}
    />
  );
}

// Счётчик показанных строк снят решением владельца: число над таблицей не
// отвечало ни на один вопрос, ради которого на страницу приходят, а место в
// ряду ручек занимало. Компонент удалён вместе с предметом, а не оставлен
// «на случай»: неиспользуемая форма выглядит работающей и переживает своё
// основание.

/** ColumnSettings — шестерёнка с чек-боксами видимости колонок. */
export function ColumnSettings({
  columns,
  hidden,
  onToggle,
}: {
  columns: ToggleCol[];
  hidden: Set<string>;
  onToggle: (key: string, hidden: boolean) => void;
}) {
  return (
    <Dropdown
      trigger={["click"]}
      placement="bottomRight"
      popupRender={() => (
        <div
          style={{
            padding: 12,
            minWidth: 180,
            // Выпадающий блок ДЕЙСТВИТЕЛЬНО всплывает над страницей — он один из
            // немногих, кому тень положена; статичные панели держат глубину
            // тоном. Роли берутся из палитры продукта: прежние запасные значения
            // (тёмно-серый фон, чёрная тень) не менялись вместе с темой, и в
            // светлой теме блок оставался тёмным.
            background: "var(--kc-elevated)",
            border: "1px solid var(--kc-border)",
            borderRadius: 11,
            boxShadow: "var(--kc-shadow-md)",
          }}
        >
          <div
            style={{
              fontSize: 10,
              fontWeight: 550,
              letterSpacing: "0.12em",
              textTransform: "uppercase",
              color: "var(--kc-text-tertiary)",
            }}
          >
            Колонки
          </div>
          <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 10 }}>
            {columns.map((c) => (
              <Checkbox key={c.key} checked={!hidden.has(c.key)} onChange={(e) => onToggle(c.key, !e.target.checked)}>
                {c.label}
              </Checkbox>
            ))}
          </div>
        </div>
      )}
    >
      {/* Кнопка ПОДПИСАНА: безымянная шестерёнка читается как «настройки»
          вообще, и выбор столбцов рядом с фильтрами попросту не находили —
          владелец дважды сообщал, что его «нет», хотя он был. */}
      <Button icon={<SettingOutlined />} title="Выбрать столбцы таблицы">
        Столбцы
      </Button>
    </Dropdown>
  );
}
