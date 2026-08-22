// LabelsCell — метки ресурса: пары «ключ → значение», где КЛЮЧ ВИДЕН ОТДЕЛЬНО.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ КЛЮЧ ВЫДЕЛЕН, А НЕ СЛИТ СО ЗНАЧЕНИЕМ (решение владельца)
//
// Прежде метка была одной строкой `env=dev` в общем теге. Читается такая строка
// целиком, и глаз не отличает, где кончается ключ: `team=networking` и
// `teamnet=working` на беглый взгляд одинаковы. А ищут метки именно по КЛЮЧУ —
// «что тут по env», «есть ли owner», — и ключ обязан находиться сразу.
//
// Теперь метка — двухчастная пилюля: слева ключ на своей, чуть плотной заливке,
// справа значение. Разделяет их линия, а не знак равенства: знак приходится
// читать, линия — нет. Ключ набран весом и тоном текста, значение — тоном
// вторичным: сначала «про что», потом «какое».
//
// Копирование осталось прежним: клик по метке кладёт в буфер `ключ=значение` —
// то есть ровно то, что вставляют в фильтр или в вызов, а не то, что видно.
//
// Форма ряда осталась прежней: одна строка без переноса, лишнюю ширину метки
// отбирают у себя (перенос дал бы вторую строку у ОДНОЙ строки таблицы, и
// список пошёл бы лесенкой).

import type { CSSProperties } from "react";
import { toast } from "@shared/lib/toast";
import { copyText } from "@shared/lib/clipboard";

interface Props {
  labels?: Record<string, string> | null;
  /** Максимум видимых меток; остальные сворачиваются в «+N». */
  max?: number;
}

/** Оболочка метки. Рамка и радиус — общие с прочими пилюлями продукта; заливки
 *  у половин РАЗНЫЕ, и в этом весь смысл: ключ и значение — разные вещи. */
const CHIP: CSSProperties = {
  display: "inline-flex",
  alignItems: "stretch",
  minWidth: 0,
  flexShrink: 1,
  border: "1px solid var(--kc-border)",
  borderRadius: 7,
  overflow: "hidden",
  cursor: "pointer",
  // Метка — КНОПКА: она совершает действие (кладёт машинную форму в буфер), и
  // потому обязана быть достижима с клавиатуры. Прежде это был `span` с
  // обработчиком клика — то есть действие, которого для клавиатуры не
  // существовало вовсе.
  //
  // Собственное оформление кнопки снимается, чтобы вид не менялся: браузер
  // приносит свою рамку, заливку, поля и выравнивание текста.
  padding: 0,
  margin: 0,
  background: "none",
  font: "inherit",
  textAlign: "left",
  appearance: "none",
  // Шрифт объявляется ПОСЛЕ сброса: `font: inherit` выше перебил бы эти три
  // строки, и метка потеряла бы моноширинность вместе с кеглем.
  fontFamily: "var(--font-mono)",
  fontSize: 10,
  lineHeight: 1,
  fontVariantNumeric: "tabular-nums",
};

/** Ключ — первая зона: плотнее заливка, тон основного текста, вес выше.
 *  Он не ужимается: ключ и есть то, по чему метку узнают. */
const KEY_PART: CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  padding: "4px 7px",
  background: "var(--kc-field)",
  borderRight: "1px solid var(--kc-border)",
  color: "var(--kc-text)",
  fontWeight: 650,
  flexShrink: 0,
  whiteSpace: "nowrap",
};

/** Значение — вторая зона: без заливки, тоном ниже. Ужимается именно оно:
 *  обрезанное значение остаётся понятным, обрезанный ключ — нет. */
const VALUE_PART: CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  padding: "4px 7px",
  color: "var(--kc-text-secondary)",
  fontWeight: 520,
  minWidth: 0,
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};

/** Счётчик скрытых меток НЕ ужимается: он короток и он и есть ответ на вопрос
 *  «сколько ещё». Ужавшись, он превратился бы в «+…» — то есть перестал бы
 *  отвечать. Подпись остаётся «+N»: её закрепляет проба. */
const COUNTER: CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  padding: "4px 7px",
  border: "1px solid var(--kc-border)",
  borderRadius: 7,
  background: "var(--kc-field)",
  color: "var(--kc-text-tertiary)",
  fontFamily: "var(--font-mono)",
  fontSize: 10,
  fontWeight: 540,
  lineHeight: 1,
  flexShrink: 0,
};

export function LabelsCell({ labels, max = 4 }: Props) {
  const entries = Object.entries(labels ?? {});
  if (entries.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  const shown = entries.slice(0, max);
  const hiddenCount = entries.length - shown.length;

  const copy = (e: React.MouseEvent, kv: string) => {
    e.stopPropagation();
    // Через общий помощник: `navigator.clipboard` существует только в
    // защищённом контексте, а консоль открывают и по `http://` на имени
    // хоста — там прямое обращение роняло обработчик до всякого `catch`.
    void copyText(kv).then((ok) =>
      ok ? toast.success(`Скопировано: ${kv}`) : toast.error("Не удалось скопировать"),
    );
  };

  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 5, flexWrap: "nowrap", maxWidth: 320 }}>
      {shown.map(([k, v]) => {
        const kv = `${k}=${v}`;
        return (
          <button
            key={k}
            type="button"
            style={CHIP}
            onClick={(e) => copy(e, kv)}
            // Подсказка несёт то, что ляжет в буфер, — а туда идёт `ключ=значение`,
            // машинная форма, годная для фильтра и для вызова.
            title={`Скопировать ${kv}`}
          >
            <span style={KEY_PART}>{k}</span>
            <span style={VALUE_PART}>{v}</span>
          </button>
        );
      })}
      {hiddenCount > 0 && <span style={COUNTER}>+{hiddenCount}</span>}
    </span>
  );
}
