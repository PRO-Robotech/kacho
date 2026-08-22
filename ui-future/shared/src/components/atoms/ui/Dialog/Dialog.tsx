import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "@shared/lib/utils";

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;

export const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>
>(({ className, children, ...props }, ref) => (
  <DialogPrimitive.Portal>
    {/* Затемнение БЕЗ размытия: стеклянные размытия целевое оформление
        запрещает прямо — фокус на окне даётся тоном подложки, а не расфокусом
        страницы под ним. */}
    <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-black/55" />
    <DialogPrimitive.Content
      ref={ref}
      className={cn(
        // Радиус 11 — ряд поверхности; тень остаётся: окно действительно
        // всплывает над страницей, а запрет теней касается статичных панелей.
        "fixed left-1/2 top-1/2 z-50 grid w-full max-w-2xl -translate-x-1/2 -translate-y-1/2 gap-4 rounded-[11px] border border-[var(--kc-border)] bg-[var(--kc-elevated)] p-6 shadow-[shadow:var(--kc-shadow-lg)] max-h-[85vh] overflow-y-auto",
        className,
      )}
      {...props}
    >
      {children}
      <DialogPrimitive.Close className="absolute right-4 top-4 grid h-[30px] w-[30px] place-items-center rounded-md text-[var(--kc-text-tertiary)] transition-colors hover:bg-[var(--kc-hover-fill)] hover:text-[var(--kc-text)]">
        <X className="h-4 w-4" />
        <span className="sr-only">Закрыть окно</span>
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
));
DialogContent.displayName = "DialogContent";

export function DialogHeader({ children, className }: React.PropsWithChildren<{ className?: string }>) {
  return <div className={cn("flex flex-col gap-1", className)}>{children}</div>;
}

export const DialogTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn("text-lg font-semibold", className)} {...props} />
));
DialogTitle.displayName = "DialogTitle";

export const DialogDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description ref={ref} className={cn("text-sm text-muted-foreground", className)} {...props} />
));
DialogDescription.displayName = "DialogDescription";

export function DialogFooter({ children, className }: React.PropsWithChildren<{ className?: string }>) {
  return <div className={cn("flex justify-end gap-2 mt-4", className)}>{children}</div>;
}
