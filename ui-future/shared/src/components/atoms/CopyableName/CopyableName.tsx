// CopyableName — bold-text имя ресурса; click копирует имя в clipboard.
// Применяется в Networks/Subnets/SG list, чтобы не уйти в detail при клике по тексту имени.
// Аналог CopyableId, но без monospaced font и без visible icon.

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { toast } from "@shared/lib/toast";
import { cn } from "@shared/lib/utils";

interface Props {
  name: string;
  /** Fallback значение, если name пустое (например — id ресурса). */
  fallback?: string;
  /** Только иконка, без текста. Нужен там, где сам текст уже занят ССЫЛКОЙ на
   *  карточку: кнопка вокруг текста перехватывала бы клик и вместо перехода
   *  копировала — при том что ссылка есть и выглядит рабочей.
   *
   *  Вызывающий ОБЯЗАН обернуть кнопку вместе с текстом в элемент с классом
   *  `group`: значок не виден в покое и раскрывается наведением на этот
   *  элемент. Без обёртки якорем остаётся сама кнопка — раскрыть значок можно
   *  будет только наведением на него самого, то есть на двенадцать пикселей,
   *  которых в покое не видно (#480). */
  iconOnly?: boolean;
  className?: string;
}

export function CopyableName({ name, fallback, className, iconOnly }: Props) {
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
        "group inline-flex items-center gap-1 transition-colors cursor-pointer text-left",
        "text-foreground hover:text-primary",
        // Кнопка без текста не несёт и текстового блока: `text-sm` поставил бы
        // вокруг значка в 12px строку в 20px, и кнопка читалась бы заметно
        // крупнее самого значка — при том что показывать ей нечего (#480).
        iconOnly ? "leading-none" : "font-medium text-sm",
        !iconOnly && isFallback && "font-mono text-xs",
        className,
      )}
    >
      {!iconOnly && <span className="break-all">{value}</span>}
      {copied ? (
        <Check className="h-3 w-3 text-emerald-500 shrink-0" />
      ) : (
        // В покое значка не видно вовсе — копирование вспомогательно и не
        // спорит за внимание с тем, к чему приставлено. Раскрывает его
        // наведение на ЛЮБОГО `group`-предка: у формы с текстом это сама
        // кнопка, у `iconOnly` — обёртка вызывающего (см. `iconOnly` выше).
        // `group-focus-within` добавлен к прежнему правилу не ради заметности:
        // без него кнопка, до которой дошли клавиатурой, оставалась бы
        // невидимой — в форме с текстом такого не бывало, там виден текст.
        <Copy className="h-3 w-3 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 shrink-0 transition-opacity" />
      )}
    </button>
  );
}
