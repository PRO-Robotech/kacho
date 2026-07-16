// FormSection — группа полей с заголовком + тонким divider. Best-practice:
// разбивать инфра-форму на секции (Идентичность → Конфигурация → Сеть →
// Расширенное). collapsible+defaultOpen=false — для optional/advanced-блоков.
import { useState, type ReactNode } from "react";
import { DownOutlined, RightOutlined } from "@ant-design/icons";

interface Props {
  title: string;
  collapsible?: boolean;
  defaultOpen?: boolean;
  children: ReactNode;
}

export function FormSection({ title, collapsible, defaultOpen = true, children }: Props) {
  const [open, setOpen] = useState(defaultOpen);
  const toggle = () => collapsible && setOpen((v) => !v);
  return (
    <div style={{ marginBottom: 8 }}>
      <div
        role={collapsible ? "button" : undefined}
        tabIndex={collapsible ? 0 : undefined}
        aria-expanded={collapsible ? open : undefined}
        onClick={toggle}
        onKeyDown={
          collapsible
            ? (e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  toggle();
                }
              }
            : undefined
        }
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          cursor: collapsible ? "pointer" : "default",
          margin: "4px 0 12px",
          borderBottom: "1px solid var(--kc-border-secondary)",
          paddingBottom: 6,
        }}
      >
        {collapsible && (open ? <DownOutlined /> : <RightOutlined />)}
        <span
          style={{
            textTransform: "uppercase",
            fontSize: 12,
            fontWeight: 600,
            letterSpacing: 0.4,
            color: "var(--kc-text-tertiary)",
          }}
        >
          {title}
        </span>
      </div>
      {open && children}
    </div>
  );
}
