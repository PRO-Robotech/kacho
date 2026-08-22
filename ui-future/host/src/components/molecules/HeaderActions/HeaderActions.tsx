import type { Dispatch, FC, SetStateAction } from "react";
import { Button, Tooltip } from "antd";
import { Moon, Sun } from "lucide-react";

export const HeaderActions: FC<{
  dark: boolean;
  setDark: Dispatch<SetStateAction<boolean>>;
}> = ({ dark, setDark }) => {
  return (
    <div className="header-actions">
      {/* Высоту кнопка берёт из темы (36) — как всякое действие продукта.
          Уменьшенный размер делал её вдвое ниже шапки и читался как
          второстепенная, хотя это единственное действие в шапке. */}
      <Tooltip title={dark ? "Светлая тема" : "Тёмная тема"}>
        <Button
          type="text"
          icon={dark ? <Sun size={16} /> : <Moon size={16} />}
          aria-label={dark ? "Включить светлую тему" : "Включить тёмную тему"}
          onClick={() => setDark((v) => !v)}
        />
      </Tooltip>
    </div>
  );
};
