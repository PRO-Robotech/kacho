// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { Alert, Button } from "antd";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import type { AccessKeySignIn } from "./use-access-key-sign-in";

// Кнопка входа ключом доступа на экране входа и исход её попытки (приёмка
// F8-S4, Р1). Кнопка — `type="button"` ВНЕ формы пароля: клавиша ввода в поле
// пароля отправляет форму пароля, а не начинает церемонию ключа; до кнопки
// человек доходит клавишей перехода и нажимает её клавишей ввода (F8S4-12).
//
// Отказ службы — тем же видом, что у формы пароля (`LaneRefusalAlert`):
// текст службы дословно и то, что нужно для действия. Отказ браузера — один
// текст консоли без подробностей браузера (Р5, условие К2).

/** Текст консоли на отказ церемонии браузером (приёмка F8-S4, Р5) — один на все причины. */
export const ACCESS_KEY_BROWSER_REFUSED = "Вход ключом прерван в браузере — повторите или войдите паролем";

export function AccessKeySignInButton({ signIn, blocked }: { signIn: AccessKeySignIn; blocked: boolean }) {
  if (!signIn.supported) return null;
  const { failure } = signIn;
  return (
    <div style={{ marginTop: 16 }}>
      {failure && (
        <div style={{ marginBottom: 16 }}>
          {failure.kind === "service" ? (
            <LaneRefusalAlert refusal={failure.refusal} />
          ) : (
            <Alert type="error" showIcon message={ACCESS_KEY_BROWSER_REFUSED} />
          )}
        </div>
      )}
      <Button
        htmlType="button"
        block
        loading={signIn.busy}
        disabled={blocked || signIn.lockedFor !== null}
        onClick={signIn.start}
      >
        Войти ключом доступа
      </Button>
    </div>
  );
}
