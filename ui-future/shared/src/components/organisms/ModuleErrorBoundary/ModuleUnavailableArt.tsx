import type { FC } from "react";

/**
 * Рисунок экрана «раздел недоступен».
 *
 * ВСТРОЕННЫЙ SVG, а не файл и не внешний адрес: этот экран показывается ровно
 * тогда, когда что-то НЕ ЗАГРУЗИЛОСЬ, — картинка, которую саму надо тянуть по
 * сети, на нём отказала бы вместе с разделом и оставила пустое место.
 *
 * Цвет берётся ТОЛЬКО из токенов: литерал вроде "#aab6ca" не меняется вместе с
 * темой, и на светлом фоне рисунок читался бы выключенным (за это уже чинили
 * вкладки и второй сайдбар).
 *
 * Движения нет вовсе. Вращающаяся шестерня на экране отказа читается как
 * продолжающаяся загрузка — то есть обещает, что вот-вот дорисуется, а страница
 * говорит обратное.
 */

// Зубцы шестерни — восемь лучей через 45°. Перечень, а не цикл по индексу:
// координаты видны глазом, и правка одного зубца не сдвигает остальные.
const GEAR_TEETH = [0, 45, 90, 135, 180, 225, 270, 315].map((deg) => {
  const rad = (deg * Math.PI) / 180;
  return {
    deg,
    x1: 172 + Math.cos(rad) * 9.5,
    y1: 92 + Math.sin(rad) * 9.5,
    x2: 172 + Math.cos(rad) * 13,
    y2: 92 + Math.sin(rad) * 13,
  };
});

export const ModuleUnavailableArt: FC = () => (
  // Смысл несёт текст под рисунком, поэтому для чтения с экрана рисунок скрыт:
  // озвучивать «схема из трёх полок» пользователю нечего.
  <svg
    viewBox="0 0 208 136"
    width="208"
    height="136"
    fill="none"
    strokeLinecap="round"
    aria-hidden="true"
    focusable="false"
    style={{ maxWidth: "100%" }}
  >
    {/* Опора: без неё рамка висит в пустоте и рисунок читается как обрезанный. */}
    <line x1="10" y1="118" x2="198" y2="118" stroke="var(--kc-border)" strokeWidth="1.25" />

    {/* Стойка раздела: три полки на общей шине — так же, как раздел собран из
        своих ресурсов. Средняя отсутствует: это и есть предмет экрана. */}
    <rect x="24" y="16" width="132" height="102" rx="12" stroke="var(--kc-border)" strokeWidth="1.25" />
    <line x1="44" y1="36" x2="44" y2="98" stroke="var(--kc-border-secondary)" strokeWidth="1.25" />

    {[42, 64, 86].map((y) => (
      <line key={y} x1="44" y1={y} x2="58" y2={y} stroke="var(--kc-border-secondary)" strokeWidth="1.25" />
    ))}

    {/* Целые полки: рамка плюс короткий штрих содержимого. */}
    {[34, 78].map((y) => (
      <g key={y}>
        <rect x="58" y={y} width="84" height="16" rx="4" stroke="var(--kc-border)" strokeWidth="1.25" />
        <line x1="68" y1={y + 8} x2="104" y2={y + 8} stroke="var(--kc-border-secondary)" strokeWidth="1.25" />
      </g>
    ))}

    {/* Отсутствующая полка: пунктир и предупреждающий тон — единственное цветное
        пятно рисунка. Насыщенный цвет стоит там, где сообщает состояние. */}
    <rect
      x="58"
      y="56"
      width="84"
      height="16"
      rx="4"
      fill="var(--status-warn-bg)"
      stroke="var(--status-warn-border)"
      strokeWidth="1.25"
      strokeDasharray="4 4"
    />

    {/* Узлы шины: два обычных, средний — под тон отсутствующей полки. */}
    {[42, 64, 86].map((y) => (
      <circle
        key={y}
        cx="44"
        cy={y}
        r="2.5"
        stroke={y === 64 ? "var(--status-warn-fg)" : "var(--kc-text-tertiary)"}
        strokeWidth="1.25"
      />
    ))}

    {/* Шестерня — «ведутся работы». Стоит СНАРУЖИ стойки: работы идут не внутри
        полки, а вокруг раздела. */}
    <circle cx="172" cy="92" r="9.5" stroke="var(--status-warn-fg)" strokeWidth="1.25" />
    <circle cx="172" cy="92" r="3.5" stroke="var(--status-warn-fg)" strokeWidth="1.25" />
    {GEAR_TEETH.map((tooth) => (
      <line
        key={tooth.deg}
        x1={tooth.x1}
        y1={tooth.y1}
        x2={tooth.x2}
        y2={tooth.y2}
        stroke="var(--status-warn-fg)"
        strokeWidth="1.25"
      />
    ))}
  </svg>
);
