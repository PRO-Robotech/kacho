import type { FC } from "react";

/**
 * Рисунок пустого списка таблиц маршрутов.
 *
 * ЧТО НАРИСОВАНО. Трафик приходит слева, доходит до узла решения (ромб) и
 * уходит одним из направлений — вверх или вниз. Справа те же направления
 * выписаны строками правил: слева от стрелки — куда, справа — через кого.
 *
 * ПОЧЕМУ ИМЕННО ЭТО. Таблица маршрутов — не «список записей», а набор
 * ответов на вопрос «куда направить». Поэтому нарисован САМ ВЫБОР: без развилки
 * те же строки читались бы как список любого другого ресурса, а с развилкой
 * видно, что строка правила и есть одно из направлений.
 *
 * ЦВЕТНОЕ ПЯТНО ОДНО — направление, которого ещё нет: пунктирная ветвь от того
 * же узла и пустая строка под неё. Тон первичный, а не предупреждающий: это
 * приглашение завести первое правило, а не сообщение об отказе.
 *
 * Встроенный SVG, а не файл и не внешний адрес: экран показывается и в модуле,
 * поднятом отдельно от каркаса, где путей к ассетам каркаса нет. Цвета — только
 * токены: литерал не меняется вместе с темой, и на светлой теме рисунок читался
 * бы выключенным.
 *
 * Движения нет вовсе: анимация на пустом экране читается как продолжающаяся
 * загрузка, то есть обещает строки, которых не будет.
 */

// Две ведущие ветви и их строки правил — перечнем, а не циклом по индексу:
// координаты видны глазом, и правка одной ветви не сдвигает вторую.
const BRANCHES = [
  { id: "up", d: "M70 68 L80 68 L80 40 L98 40", tip: 40 },
  { id: "down", d: "M70 68 L80 68 L80 96 L98 96", tip: 96 },
];

export const RouteTableArt: FC = () => (
  // Смысл несёт текст под рисунком; озвучивать схему нечего.
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
    {/* Опора: без неё развилка висит в пустоте и рисунок читается обрезанным. */}
    <line x1="10" y1="118" x2="198" y2="118" stroke="var(--kc-border)" strokeWidth="1.25" />

    {/* Откуда приходит трафик. Тон второстепенный — источник не предмет списка. */}
    <rect x="16" y="60" width="16" height="16" rx="4" stroke="var(--kc-border-secondary)" strokeWidth="1.25" />
    <circle cx="24" cy="68" r="2" stroke="var(--kc-text-tertiary)" strokeWidth="1.25" />
    <line x1="32" y1="68" x2="50" y2="68" stroke="var(--kc-border-secondary)" strokeWidth="1.25" />

    {/* Узел решения: ромб — общепринятая форма выбора, и она отличает этот
        рисунок от распределения нагрузки, где узел один и круглый. */}
    <path d="M50 68 L60 58 L70 68 L60 78 Z" stroke="var(--kc-border)" strokeWidth="1.25" />

    {/* Ветви: колено под прямым углом плюс наконечник у входа в свою строку. */}
    {BRANCHES.map((branch) => (
      <g key={branch.id}>
        <path d={branch.d} stroke="var(--kc-border-secondary)" strokeWidth="1.25" />
        <line
          x1="95"
          y1={branch.tip - 3.5}
          x2="98"
          y2={branch.tip}
          stroke="var(--kc-text-tertiary)"
          strokeWidth="1.25"
        />
        <line
          x1="95"
          y1={branch.tip + 3.5}
          x2="98"
          y2={branch.tip}
          stroke="var(--kc-text-tertiary)"
          strokeWidth="1.25"
        />
      </g>
    ))}

    {/* Строки правил: слева префикс назначения, стрелка, справа следующий узел.
        Ритм строки узнаётся без подписей, которых на рисунке быть не должно. */}
    {BRANCHES.map((branch) => (
      <g key={`rule-${branch.id}`}>
        <rect
          x="104"
          y={branch.tip - 9}
          width="84"
          height="18"
          rx="5"
          stroke="var(--kc-border)"
          strokeWidth="1.25"
        />
        <line x1="112" y1={branch.tip} x2="132" y2={branch.tip} stroke="var(--kc-text-tertiary)" strokeWidth="1.25" />
        <line x1="138" y1={branch.tip} x2="148" y2={branch.tip} stroke="var(--kc-border-secondary)" strokeWidth="1.25" />
        <line
          x1="145"
          y1={branch.tip - 3.5}
          x2="148"
          y2={branch.tip}
          stroke="var(--kc-border-secondary)"
          strokeWidth="1.25"
        />
        <line
          x1="145"
          y1={branch.tip + 3.5}
          x2="148"
          y2={branch.tip}
          stroke="var(--kc-border-secondary)"
          strokeWidth="1.25"
        />
        <line x1="154" y1={branch.tip} x2="180" y2={branch.tip} stroke="var(--kc-text-tertiary)" strokeWidth="1.25" />
      </g>
    ))}

    {/* Намеченное направление — предмет экрана. Ветвь продолжает тот же излом
        прямо, а строка под неё пуста: правила ещё нет, место под него есть. */}
    <line
      x1="80"
      y1="68"
      x2="98"
      y2="68"
      stroke="var(--kc-primary)"
      strokeWidth="1.25"
      strokeDasharray="4 4"
    />
    <rect
      x="104"
      y="59"
      width="84"
      height="18"
      rx="5"
      fill="var(--kc-primary-bg)"
      stroke="var(--kc-primary)"
      strokeWidth="1.25"
      strokeDasharray="4 4"
    />
  </svg>
);
