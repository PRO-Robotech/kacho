// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { createContext, useCallback, useContext, useState, type ReactNode } from "react";

// Исход действия экрана «Безопасность» — ОДНОЙ областью `role="status"` на
// экран (приёмка F8-S7, Р9): ключ добавлен, фактор отключён, коды выпущены,
// сессии завершены. Текстом, а не только цветом; отказ — у своего раздела
// областью `role="alert"`.
//
// Область одна на экран: у человека с диктором исход не должен звучать дважды,
// а две области об одном предмете разошлись бы — одна сказала бы «удалён»,
// другая промолчала бы. Тексты исхода — общие (`mutation-signal.ts`).

type Announce = (text: string) => void;

const AnnouncerContext = createContext<Announce>(() => undefined);

/** Объявить исход действия экрана. Вне экрана — ничего: объявлять некому. */
export function useAnnounce(): Announce {
  return useContext(AnnouncerContext);
}

export function SecurityAnnouncer({ children }: { children: ReactNode }) {
  const [text, setText] = useState("");
  const announce = useCallback<Announce>((t) => setText(t), []);
  return (
    <AnnouncerContext.Provider value={announce}>
      <p role="status" aria-live="polite" style={{ margin: text ? "0 0 12px" : 0, minHeight: 0 }}>
        {text}
      </p>
      {children}
    </AnnouncerContext.Provider>
  );
}
