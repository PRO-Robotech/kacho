// BoolFact — булево свойство ресурса, названное СЛЕДСТВИЕМ, а не ответом «да».
//
// «Да» отвечает на вопрос, которого пользователь не задавал: рядом с подписью
// «Защита от удаления» оно не говорит ни что защита включена, ни что удалить
// нельзя — читателю приходится достраивать смысл самому. Поэтому оба исхода
// формулируются словами предмета: «Удаление запрещено» / «Удаление разрешено».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЦВЕТ СЛЕДУЕТ ЗА СМЫСЛОМ, А НЕ ЗА ИСТИННОСТЬЮ (решение владельца)
//
// Прежде цвет назначался булевым значением: истина с пометкой `accent` красилась
// первичным тоном, ложь всегда уходила в приглушённый. Из-за этого тон вступал
// в спор со смыслом ровно там, где смысл важен:
//
//   • «Свободен» выглядел выключенным состоянием, хотя свободный адрес — это
//     ресурс, готовый к работе, а вовсе не что-то потухшее;
//   • «Удаление разрешено» тоже приглушалось, хотя это единственная из двух
//     сторон, о которой стоит знать: защиты нет, и ресурс можно снести;
//   • «Удаление запрещено» красилось тем же первичным тоном, что и «занят», —
//     то есть охрана и занятость выглядели одним и тем же событием.
//
// Теперь тон объявляется КАЖДОЙ стороне отдельно (`yesTone` / `noTone`):
//
//   neutral   — штатное положение, ничего не сообщает (приглушённый тон);
//   active    — связь установлена, ресурс задействован (тон телеметрии);
//   good      — свойство защищает или подтверждает готовность (здоровый тон);
//   attention — стоит знать: защиты нет, возможность закрыта (тон внимания).
//
// Глиф тоже принадлежит стороне, а не истинности: у защиты это замок, у
// занятости — связь. Смысл по-прежнему несёт ТЕКСТ; цвет и глиф лишь помогают
// взгляду найти строку, о которой стоит подумать.
//
// ПИЛЮЛЕЙ ЭТО НЕ РИСУЕТСЯ, и это не пропущенная унификация. Пилюля состояния
// несёт рамку и заливку — то есть добавляет на экран цвет и линию. Здесь такой
// ценой оплачивалось бы большинство значений, внимания не требующих.
import type { ReactNode } from "react";
import {
  CheckCircleOutlined,
  LinkOutlined,
  LockOutlined,
  MinusCircleOutlined,
  SafetyCertificateOutlined,
  UnlockOutlined,
  WarningOutlined,
} from "@ant-design/icons";

/** Тон стороны — по смыслу, а не по истинности. */
export type FactTone = "neutral" | "active" | "good" | "attention";

/** Глиф стороны. Имя называет ПРЕДМЕТ, а не картинку: замок — про защиту, связь
 *  — про занятость. Не задан — берётся по тону. */
export type FactGlyph = "lock" | "unlock" | "link" | "shield" | "check" | "dash" | "warn";

export interface BoolFactProps {
  value: unknown;
  /** Что означает истина — фразой предмета, не «Да». */
  yes: string;
  /** Что означает ложь — фразой предмета, не «Нет». */
  no: string;
  /** Тон истины. По умолчанию нейтральный. */
  yesTone?: FactTone;
  /** Тон лжи. По умолчанию нейтральный. */
  noTone?: FactTone;
  yesGlyph?: FactGlyph;
  noGlyph?: FactGlyph;
  /** Прежняя форма: «выделить истину». Оставлена, чтобы не переписывать разом
   *  все двенадцать мест — она означает ровно `yesTone="active"`. Новые вызовы
   *  объявляют тон явно: он говорит, ПОЧЕМУ выделено, а не только что выделено. */
  accent?: boolean;
}

const TONE_COLOR: Record<FactTone, string> = {
  neutral: "var(--kc-text-tertiary)",
  active: "var(--kc-cyan)",
  good: "var(--status-ok-fg)",
  attention: "var(--status-warn-fg)",
};

const GLYPH_BY_TONE: Record<FactTone, FactGlyph> = {
  neutral: "dash",
  active: "link",
  good: "check",
  attention: "warn",
};

function Glyph({ kind, color }: { kind: FactGlyph; color: string }) {
  const style = { color, fontSize: 13 };
  switch (kind) {
    case "lock":
      return <LockOutlined style={style} aria-hidden />;
    case "unlock":
      return <UnlockOutlined style={style} aria-hidden />;
    case "link":
      return <LinkOutlined style={style} aria-hidden />;
    case "shield":
      return <SafetyCertificateOutlined style={style} aria-hidden />;
    case "warn":
      return <WarningOutlined style={style} aria-hidden />;
    case "check":
      return <CheckCircleOutlined style={style} aria-hidden />;
    default:
      return <MinusCircleOutlined style={style} aria-hidden />;
  }
}

export function BoolFact({
  value,
  yes,
  no,
  yesTone,
  noTone = "neutral",
  yesGlyph,
  noGlyph,
  accent = false,
}: BoolFactProps): ReactNode {
  const on = !!value;
  // Прежняя форма отображается в тон, а не живёт рядом со своей заменой: два
  // способа сказать одно расходятся при первой же правке.
  const tone: FactTone = on ? (yesTone ?? (accent ? "active" : "neutral")) : noTone;
  const color = TONE_COLOR[tone];
  const glyph = (on ? yesGlyph : noGlyph) ?? GLYPH_BY_TONE[tone];
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>
      <Glyph kind={glyph} color={color} />
      <span style={{ color: tone === "neutral" ? "var(--kc-text-secondary)" : color }}>{on ? yes : no}</span>
    </span>
  );
}
