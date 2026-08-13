// BoolFact — булево свойство ресурса, названное СЛЕДСТВИЕМ, а не ответом «да».
//
// «Да» отвечает на вопрос, которого пользователь не задавал: рядом с подписью
// «Защита от удаления» оно не говорит ни что защита включена, ни что удалить
// нельзя — читателю приходится достраивать смысл самому. Поэтому оба исхода
// формулируются словами предмета: «Удаление запрещено» / «Удаление разрешено».
//
// Иконка — вторична и лишь помогает взгляду: смысл несёт текст. Цветом
// выделяется только то, что действительно требует внимания (`accent`), а
// нейтральные свойства остаются нейтральными — иначе зелёный теряет значение.
import type { ReactNode } from "react";
import { CheckCircleOutlined, MinusCircleOutlined } from "@ant-design/icons";
import { Typography } from "antd";

export interface BoolFactProps {
  value: unknown;
  /** Что означает истина — фразой предмета, не «Да». */
  yes: string;
  /** Что означает ложь — фразой предмета, не «Нет». */
  no: string;
  /** Выделить истину цветом: свойство, о котором стоит знать (защита, признак
   *  «по умолчанию»). Для нейтральных свойств не задаётся. */
  accent?: boolean;
}

export function BoolFact({ value, yes, no, accent = false }: BoolFactProps): ReactNode {
  const on = !!value;
  const color = on && accent ? "var(--kc-primary)" : "var(--kc-text-tertiary)";
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      {on ? (
        <CheckCircleOutlined style={{ color, fontSize: 13 }} aria-hidden />
      ) : (
        <MinusCircleOutlined style={{ color: "var(--kc-text-tertiary)", fontSize: 13 }} aria-hidden />
      )}
      {on ? (
        <span style={accent ? { color: "var(--kc-primary)" } : undefined}>{yes}</span>
      ) : (
        <Typography.Text type="secondary">{no}</Typography.Text>
      )}
    </span>
  );
}
