// CopyableId — рендерит полный id моноширинно с inline copy-button.
// Click копирует через navigator.clipboard, показывает toast и подменяет
// иконку на ✓ на 1.5s. Используется везде, где раньше был truncated `slice(0,8)…`.

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { toast } from "@/lib/toast";
import { cn } from "@/lib/utils";

interface Props {
  id: string;
  className?: string;
  // false — без иконки рядом (подсказка title всё равно работает; click активен).
  showIcon?: boolean;
}

export function CopyableId({ id, className, showIcon = true }: Props) {
  const [copied, setCopied] = useState(false);

  if (!id) return <span className="text-muted-foreground">—</span>;

  const onCopy = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(id);
      setCopied(true);
      toast.success("ID скопирован");
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("Не удалось скопировать");
    }
  };

  return (
    <button
      type="button"
      onClick={onCopy}
      title={copied ? "Скопировано" : "Скопировать ID"}
      className={cn(
        "group inline-flex items-center gap-1 font-mono text-xs",
        "text-muted-foreground hover:text-foreground transition-colors cursor-pointer",
        "max-w-full",
        className,
      )}
    >
      <span className="break-all text-left">{id}</span>
      {showIcon &&
        (copied ? (
          <Check className="h-3 w-3 text-emerald-500 shrink-0" />
        ) : (
          <Copy className="h-3 w-3 opacity-0 group-hover:opacity-100 shrink-0 transition-opacity" />
        ))}
    </button>
  );
}
