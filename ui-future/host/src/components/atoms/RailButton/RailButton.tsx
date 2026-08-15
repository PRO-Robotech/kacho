import type { FC, ReactNode } from "react";
import { Tooltip } from "antd";

export const RailButton: FC<{
  active?: boolean;
  disabled?: boolean;
  disabledLabel?: string;
  /**
   * Раздел есть, но его модуль не загрузился (#371). Кнопка остаётся на месте
   * под своим именем: исчезнувший раздел неотличим от «такого сервиса нет».
   */
  unavailable?: boolean;
  unavailableReason?: string;
  icon: ReactNode;
  label: string;
  onClick?: () => void;
}> = ({ active, disabled, disabledLabel, unavailable, unavailableReason, icon, label, onClick }) => {
  const blocked = disabled || unavailable;
  const hint = unavailable ? (unavailableReason ?? "Раздел недоступен") : blocked ? (disabledLabel ?? label) : label;
  return (
    <Tooltip title={hint} placement="right" mouseEnterDelay={0.4}>
      <button
        type="button"
        className="rail-button"
        data-active={active || undefined}
        data-unavailable={unavailable || undefined}
        // Причина видна и без наведения (screen reader, отладка стенда): подсказка
        // antd живёт только в наведённом состоянии и в разметке кнопки её нет.
        title={unavailable ? hint : undefined}
        disabled={blocked}
        onClick={onClick}
        aria-label={label}
      >
        {icon}
      </button>
    </Tooltip>
  );
};
