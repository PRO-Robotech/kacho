import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@shared/lib/utils";

// Геометрия — та же, что у кнопки AntD (`SHAPE` в lib/theme.ts): высота 36,
// радиус 8 (`rounded-lg` = var(--radius)), отступ 13, кегль 12/610, зазор 8.
// Держать в консоли два разных вида кнопки не за чем: пользователю всё равно,
// какой библиотекой она нарисована.
//
// `focus-visible:outline-none` СНЯТ: он гасил единственную обводку фокуса,
// которую продукт рисует глобально (`:focus-visible` в index.css), а взамен
// здесь ничего не ставилось — кнопка, до которой дошли клавиатурой, переставала
// быть видимой вовсе. У поля ввода иначе: там обводку заменяют граница сигнала
// и ореол, и гасить её там осознанно.
const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-lg text-xs font-[610] transition-colors disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/90",
        destructive: "bg-destructive text-destructive-foreground hover:bg-destructive/90",
        // Кнопка в линии: заливки в покое нет, она появляется наведением —
        // «скупой сигнал» целевого оформления.
        outline:
          "border border-[var(--kc-border)] bg-transparent hover:border-[var(--kc-line-strong)] hover:bg-[var(--kc-hover-fill)]",
        secondary: "bg-secondary text-secondary-foreground hover:bg-secondary/80",
        ghost: "hover:bg-[var(--kc-hover-fill)]",
        link: "text-primary underline-offset-[3px] hover:underline",
      },
      size: {
        default: "h-9 px-[13px]",
        sm: "h-8 rounded-lg px-3",
        lg: "h-10 rounded-lg px-8",
        icon: "h-[30px] w-[30px] rounded-md",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...props} />;
  },
);
Button.displayName = "Button";
