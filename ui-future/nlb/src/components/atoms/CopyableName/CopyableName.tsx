// CopyableName — bold-text имя ресурса; click копирует имя в clipboard.
// Применяется в Networks/Subnets/SG list, чтобы не уйти в detail при клике по тексту имени.
// Аналог CopyableId, но без monospaced font и без visible icon.

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { toast } from "@/lib/toast";
import { cn } from "@/lib/utils";

interface Props {
  name: string;
  /** Fallback значение, если name пустое (например — id ресурса). */
  fallback?: string;
  className?: string;
}

export function CopyableName({ name, fallback, className }: Props) {
  const [copied, setCopied] = useState(false);

  // Используем name если есть, иначе fallback (id), иначе empty placeholder.
  const value = name || fallback || "";
  const isFallback = !name && !!fallback;

  if (!value) return <span className="text-muted-foreground italic">(unnamed)</span>;

  const onCopy = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      toast.success(isFallback ? "ID скопирован" : "Имя скопировано");
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("Не удалось скопировать");
    }
  };

  return (
    <button
      type="button"
      onClick={onCopy}
      title={copied ? "Скопировано" : isFallback ? "Имя не задано — скопировать ID" : "Скопировать имя"}
      className={cn(
        "group inline-flex items-center gap-1 font-medium text-sm",
        "text-foreground hover:text-primary transition-colors cursor-pointer text-left",
        isFallback && "font-mono text-xs",
        className,
      )}
    >
      <span className="break-all">{value}</span>
      {copied ? (
        <Check className="h-3 w-3 text-emerald-500 shrink-0" />
      ) : (
        <Copy className="h-3 w-3 opacity-0 group-hover:opacity-100 shrink-0 transition-opacity" />
      )}
    </button>
  );
}
