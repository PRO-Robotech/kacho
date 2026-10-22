// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { ReactNode } from "react";
import { SECURITY_SECTION, type SecuritySectionKey } from "./sections";

/**
 * Раздел экрана «Безопасность» — `section` с заголовком второго уровня, на
 * который ссылается его `aria-labelledby` (приёмка F8-S7, Р1, Р9). Заголовок
 * принимает фокус программно (переход из навигации и из «Настроить»), но в
 * обход клавишей Tab не попадает: он не действие.
 */
export function SecuritySection({ section, children }: { section: SecuritySectionKey; children: ReactNode }) {
  const { id, title } = SECURITY_SECTION[section];
  return (
    <section aria-labelledby={id} style={{ maxWidth: 920, marginBottom: 32 }}>
      <h2
        id={id}
        tabIndex={-1}
        style={{
          margin: "0 0 12px",
          fontSize: 20,
          fontWeight: 600,
          lineHeight: 1.4,
          color: "var(--kc-text)",
          outlineOffset: 2,
        }}
      >
        {title}
      </h2>
      {children}
    </section>
  );
}
