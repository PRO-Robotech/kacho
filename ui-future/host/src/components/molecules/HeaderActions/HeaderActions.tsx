import type { Dispatch, FC, SetStateAction } from "react";
import { Button, Tooltip } from "antd";
import { Moon, Sun } from "lucide-react";

export const HeaderActions: FC<{
  dark: boolean;
  setDark: Dispatch<SetStateAction<boolean>>;
}> = ({ dark, setDark }) => {
  return (
    <div className="header-actions">
      <Tooltip title={dark ? "Светлая тема" : "Тёмная тема"}>
        <Button
          type="text"
          size="small"
          icon={dark ? <Sun size={16} /> : <Moon size={16} />}
          aria-label={dark ? "Включить светлую тему" : "Включить тёмную тему"}
          onClick={() => setDark((v) => !v)}
        />
      </Tooltip>
    </div>
  );
};
