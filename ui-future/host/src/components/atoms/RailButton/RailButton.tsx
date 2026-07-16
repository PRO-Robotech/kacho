import type { FC, ReactNode } from "react";
import { Tooltip } from "antd";

export const RailButton: FC<{
  active?: boolean;
  disabled?: boolean;
  disabledLabel?: string;
  icon: ReactNode;
  label: string;
  onClick?: () => void;
}> = ({ active, disabled, disabledLabel, icon, label, onClick }) => {
  return (
    <Tooltip title={disabled ? (disabledLabel ?? label) : label} placement="right" mouseEnterDelay={0.4}>
      <button
        type="button"
        className="rail-button"
        data-active={active || undefined}
        disabled={disabled}
        onClick={onClick}
        aria-label={label}
      >
        {icon}
      </button>
    </Tooltip>
  );
};
