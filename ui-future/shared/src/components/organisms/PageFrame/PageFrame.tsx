// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { CSSProperties, ReactNode } from "react";
import { PAGE_PADDING } from "../DetailShell/PageHead";

// Рамка страницы, у которой нет своей оболочки списка или карточки: параметры
// учётной записи, доступность API, раздел без страницы.
//
// ПРОКРУТКА ОДНА, И ОНА ЗДЕСЬ. Рабочая область каркаса (`.app-content`) сама не
// прокручивается — поверхность заполняет её и прокручивает содержимое внутри
// себя; страница, положенная в неё без такой поверхности, обрезалась снизу, и
// колесо мыши её не двигало (#1274). Шапка стоит над областью прокрутки и с
// содержимым не уезжает.
//
// Имя класса `kc-surface` — то же, что у списка и карточки: им считают
// экземпляры страницы (см. `shared/src/index.css`).

interface Props {
  /** Шапка — над областью прокрутки. Нет шапки — нет и строки под неё. */
  head?: ReactNode;
  children: ReactNode;
}

const frame: CSSProperties = {
  flex: 1,
  height: "100%",
  minHeight: 0,
  minWidth: 0,
  display: "flex",
  flexDirection: "column",
  overflow: "hidden",
};

export function PageFrame({ head, children }: Props) {
  return (
    <section className="kc-surface" style={frame}>
      {head !== undefined && <div style={{ flexShrink: 0, padding: PAGE_PADDING, paddingBottom: 0 }}>{head}</div>}
      <div
        style={{
          flex: 1,
          minHeight: 0,
          overflowY: "auto",
          padding: PAGE_PADDING,
          ...(head !== undefined ? { paddingTop: 0 } : {}),
        }}
      >
        {children}
      </div>
    </section>
  );
}
