import { useEffect, useRef, type FC } from "react";
import { ACCOUNT_SETTINGS_ADDRESS } from "@shared/pages/auth/ceremony-addresses";
import { Button, Typography } from "antd";
import type { SessionIdentity } from "@shared/api/login-lane";
import { BoolFact } from "@shared/components/atoms/BoolFact";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { useLogout } from "@shared/pages/auth/use-logout";

/**
 * Учётная запись человека в каркасе: кто вошёл, подтверждён ли адрес, путь к
 * параметрам и выход (приёмка F8, F8-17 и F8-18).
 *
 * Признак подтверждённости — из ответа края о сессии, а не догадка каркаса:
 * поля нет в ответе — признака нет, «не подтверждён» по умолчанию не рисуется
 * (условие C8).
 * Действия «подтвердить адрес» здесь нет: производителя письма на посадке нет,
 * и обещать действие без исполнения хуже, чем промолчать.
 *
 * Выход — на месте: на отказе службы панель показывает её текст, и адрес
 * страницы не меняется — экран не делает вид, что вышли (F8-19).
 */
export const AccountPanel: FC<{
  identity: SessionIdentity;
  onClose: () => void;
  navigate: (path: string) => void | Promise<void>;
  leave?: (to: string) => void;
}> = ({ identity, onClose, navigate, leave }) => {
  const { logout, busy, refusal } = useLogout(leave);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    const onPointer = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onPointer);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onPointer);
    };
  }, [onClose]);

  return (
    <div
      ref={ref}
      role="dialog"
      aria-label="Учётная запись"
      className="account-panel"
      style={{
        position: "fixed",
        left: 70,
        bottom: 12,
        zIndex: 1050,
        width: 300,
        padding: 16,
        background: "var(--kc-elevated)",
        border: "1px solid var(--kc-border)",
        borderRadius: 10,
        boxShadow: "var(--kc-shadow-lg)",
      }}
    >
      <Typography.Text strong style={{ display: "block", marginBottom: 4, wordBreak: "break-all" }}>
        {identity.user.email || identity.user.displayName}
      </Typography.Text>
      {typeof identity.session?.emailVerified === "boolean" && (
        <div style={{ marginBottom: 12 }}>
          <BoolFact value={identity.session.emailVerified} yes="Адрес подтверждён" no="Адрес не подтверждён" />
        </div>
      )}
      {refusal && (
        <div style={{ marginBottom: 12 }}>
          <LaneRefusalAlert refusal={refusal} />
        </div>
      )}
      <div style={{ display: "flex", gap: 8 }}>
        <Button
          onClick={() => {
            onClose();
            void navigate(ACCOUNT_SETTINGS_ADDRESS);
          }}
        >
          Параметры учётной записи
        </Button>
        <Button danger loading={busy} onClick={() => void logout()}>
          Выйти
        </Button>
      </div>
    </div>
  );
};
