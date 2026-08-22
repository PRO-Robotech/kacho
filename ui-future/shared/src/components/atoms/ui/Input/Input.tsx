import * as React from "react";
import { cn } from "@shared/lib/utils";

export type InputProps = React.InputHTMLAttributes<HTMLInputElement>;

export const Input = React.forwardRef<HTMLInputElement, InputProps>(({ className, type, ...props }, ref) => (
  <input
    ref={ref}
    type={type ?? "text"}
    // Форма поля — общая с AntD (`SHAPE` в lib/theme.ts): высота 38, радиус 8,
    // отступ 11, заливка `--kc-field`. Тени нет: глубина делается тоном, и
    // статичное поле над страницей не всплывает.
    className={cn(
      "flex h-[38px] w-full rounded-lg border border-[var(--kc-border)] bg-[var(--kc-field)] px-[11px] text-[13px]",
      "transition-colors hover:border-[var(--kc-line-strong)]",
      "placeholder:text-muted-foreground/60",
      "focus-visible:outline-none focus-visible:border-[var(--kc-primary)] focus-visible:shadow-[shadow:var(--kc-focus-ring)]",
      "disabled:cursor-not-allowed disabled:opacity-50",
      className,
    )}
    {...props}
  />
));
Input.displayName = "Input";

export const Textarea = React.forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement>>(
  ({ className, ...props }, ref) => (
    <textarea
      ref={ref}
      className={cn(
        "flex w-full rounded-lg border border-[var(--kc-border)] bg-[var(--kc-field)] px-[11px] py-2 text-[13px]",
        "transition-colors hover:border-[var(--kc-line-strong)]",
        "placeholder:text-muted-foreground/60",
        "focus-visible:outline-none focus-visible:border-[var(--kc-primary)] focus-visible:shadow-[shadow:var(--kc-focus-ring)]",
        "disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  ),
);
Textarea.displayName = "Textarea";

export function Label({
  htmlFor,
  children,
  required,
  description,
  className,
}: {
  htmlFor?: string;
  children: React.ReactNode;
  required?: boolean;
  description?: string;
  className?: string;
}) {
  return (
    <div className={cn("space-y-0.5", className)}>
      <label htmlFor={htmlFor} className="text-sm font-medium leading-none">
        {children}
        {/* Признак обязательности — тон опасности продукта: розовый из палитры
            Tailwind не участвовал ни в одной теме и на светлой уходил в цвет
            подписи рядом. */}
        {required && (
          <span className="ml-0.5" style={{ color: "var(--kc-danger)" }}>
            *
          </span>
        )}
      </label>
      {description && <p className="text-xs text-muted-foreground">{description}</p>}
    </div>
  );
}
