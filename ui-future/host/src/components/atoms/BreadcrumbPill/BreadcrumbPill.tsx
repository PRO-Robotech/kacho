import type { ButtonHTMLAttributes, FC, Ref } from "react";
import type { theme } from "antd";
import { ChevronRight } from "lucide-react";

type BreadcrumbPillProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  ref?: Ref<HTMLButtonElement>;
  token: ReturnType<typeof theme.useToken>["token"];
  active: boolean;
  placeholder: string;
  chevron?: boolean;
};

export const BreadcrumbPill: FC<BreadcrumbPillProps> = ({
  children,
  token,
  active,
  placeholder,
  chevron,
  disabled,
  ref,
  ...rest
}) => {
  return (
    <button
      ref={ref}
      type="button"
      disabled={disabled}
      {...rest}
      className="breadcrumb-pill"
      style={{
        color: active ? token.colorText : token.colorTextTertiary,
      }}
    >
      <span>{active ? children : placeholder}</span>
      {chevron ? <ChevronRight size={13} strokeWidth={2} className="breadcrumb-pill-chevron" aria-hidden /> : null}
    </button>
  );
};
