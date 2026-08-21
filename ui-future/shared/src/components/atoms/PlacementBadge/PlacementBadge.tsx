// PlacementBadge — openForPlacement° for a Region or a Zone.
//
// Three answers, not two. `true` and `false` are what the server said; an absent
// field is what it did not say, and that is rendered as "—" rather than as
// "закрыт" — a missing signal is not a refusal.
//
// A Zone additionally carries placementBlockedReason, resolved by the service in
// the same call (zone down wins over region down). It is only meaningful for a
// closed row, and only for a reason this build knows: an unrecognised one is
// left unsaid rather than printed raw.
//
// Форма пилюли берётся у значка состояния (`statusPillStyle`), а не повторяется
// здесь своим набором классов: открытость площадки и статус ресурса — одно и то
// же по виду, и держать это двумя описаниями значит расходиться на первой же
// правке формы.

import { openForPlacementLabel, placementBlockedText, type PlacementBlockedReason } from "@shared/api/geo";
import { statusPillStyle } from "@shared/components/atoms/StatusBadge";

/** Ответ каталога → тон продукта. Открыта — здоровье, закрыта — предупреждение
 *  (не ошибка: закрытие площадки штатно), не сказано — нейтральный тон. */
const TONE_OF_ANSWER = {
  open: "ok",
  closed: "warn",
  unknown: "muted",
} as const;

interface Props {
  open: boolean | undefined;
  reason?: PlacementBlockedReason;
}

export function PlacementBadge({ open, reason }: Props) {
  const label = openForPlacementLabel(open);
  const cause = open === false ? placementBlockedText(reason) : null;
  // Ответ каталога — закрытое множество из трёх значений, поэтому запасной
  // ветки здесь нет: она была бы недостижимой.
  const tone = TONE_OF_ANSWER[label.tone];

  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <span style={statusPillStyle(tone)}>{label.text}</span>
      {cause && <span className="text-[11px] text-muted-foreground">{cause}</span>}
    </span>
  );
}
