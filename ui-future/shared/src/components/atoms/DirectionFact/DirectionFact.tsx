// DirectionFact — направление правила, названное словом и глифом, а не цветом.
//
// Прежде направление рисовалось цветным тегом (зелёный INGRESS / синий
// EGRESS). Цвет здесь ничего не значит: оба направления одинаково штатны, и
// зелёный читался как «хорошо», хотя разрешающее входящее правило — ровно то,
// что чаще всего и открывает лишнее. Тот же приём, что у булевых свойств
// (`BoolFact`): смысл несёт текст, глиф лишь помогает взгляду, — и по той же
// причине направление не рисуется пилюлей состояния: оба направления штатны,
// рамка и заливка вокруг них были бы сигналом без предмета.
import type { ReactNode } from "react";
import { LoginOutlined, LogoutOutlined } from "@ant-design/icons";

export interface DirectionFactProps {
  /** "INGRESS" | "EGRESS"; иное значение показывается как есть. */
  value: string | null | undefined;
}

export function DirectionFact({ value }: DirectionFactProps): ReactNode {
  const dir = (value ?? "").toUpperCase();
  if (dir !== "INGRESS" && dir !== "EGRESS") {
    return <span className="text-muted-foreground">{value || "—"}</span>;
  }
  const ingress = dir === "INGRESS";
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>
      {ingress ? (
        <LoginOutlined style={{ color: "var(--kc-text-tertiary)", fontSize: 13 }} aria-hidden />
      ) : (
        <LogoutOutlined style={{ color: "var(--kc-text-tertiary)", fontSize: 13 }} aria-hidden />
      )}
      <span>{ingress ? "Входящий" : "Исходящий"}</span>
    </span>
  );
}
