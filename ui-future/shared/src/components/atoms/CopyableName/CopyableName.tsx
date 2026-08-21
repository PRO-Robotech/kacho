// CopyableName — bold-text имя ресурса; click копирует имя в clipboard.
// Применяется в Networks/Subnets/SG list, чтобы не уйти в detail при клике по тексту имени.
// Аналог CopyableId, но не моноширинный: у имени нет машинного ряда.
//
// Копирование — ЧАСТЬ ЭТОГО КОМПОНЕНТА, а не отдельная кнопка у края строки
// (решение владельца): значок стоит сразу за значением и относится к нему, а не
// к строке, в которой значение показано.
//
// Значение стоит В ОДНУ СТРОКУ и обрезается многоточием — по той же причине, что
// у идентификатора: прежний `break-all` рвал имя посреди слова и добавлял ячейке
// вторую строку, отчего строка списка становилась выше соседних. Полное значение
// остаётся в подсказке и в буфере, то есть ничего не теряется.

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
   *  копировала — при том что ссылка есть и выглядит рабочей (правило 5 `ui.md`,
   *  этот промах уже стоил дня отладки).
   *
   *  Обёртка вызывающего с классом `group` больше НЕ ОБЯЗАТЕЛЬНА: значок виден
   *  всегда и от наведения не зависит. Она осталась полезной, но по другому
   *  поводу — от неё значок берёт ЯРКИЙ тон, когда курсор идёт к имени, а не
   *  только когда он уже на самом значке. */
  iconOnly?: boolean;
  className?: string;
}

export function CopyableName({ name, fallback, className, iconOnly }: Props) {
  const [copied, setCopied] = useState(false);

  // Используем name если есть, иначе fallback (id), иначе empty placeholder.
  const value = name || fallback || "";
  const isFallback = !name && !!fallback;

  if (!value) return <span className="text-muted-foreground italic">(без имени)</span>;

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
      // `minWidth: 0` — условие работы многоточия: без него flex-элемент не
      // становится уже своего содержимого и обрезка не наступает никогда.
      // Переход задан токенами продукта (160 мс, общая кривая), а не набором
      // Tailwind: движение по консоли читается как одно вещество.
      style={{
        minWidth: 0,
        maxWidth: "100%",
        transition: "color var(--kc-duration) var(--kc-ease)",
      }}
      className={cn(
        "group inline-flex items-center gap-1 cursor-pointer text-left",
        "text-foreground hover:text-primary",
        // Кнопка без текста не несёт и текстового блока: `text-sm` поставил бы
        // вокруг значка в 12px строку в 20px, и кнопка читалась бы заметно
        // крупнее самого значка — при том что показывать ей нечего (#480).
        // Три взаимоисключающие ветки, а не наложение двух наборов: запасное
        // значение — идентификатор, то есть машинная строка, и ему полагается
        // моноширинный ряд продукта (`t-mono`) целиком. Прежде ряд дописывался
        // ПОВЕРХ `text-sm`, а класс кегля Tailwind приезжает позже нашего слоя
        // типографики и перебивал его — то есть ряд применялся наполовину.
        iconOnly ? "leading-none" : isFallback ? "t-mono" : "font-medium text-sm",
        className,
      )}
    >
      {!iconOnly && (
        // Подсказка несёт ЗНАЧЕНИЕ: обрезанное имя перестаёт читаться, и вернуть
        // его глазам важнее, чем повторить название действия — про действие
        // говорят курсор и подсказка самой кнопки.
        <span title={value} style={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {value}
        </span>
      )}
      {copied ? (
        <Check className="h-3 w-3 shrink-0" style={{ color: "var(--status-ok-fg)" }} />
      ) : (
        // Значок виден ВСЕГДА (решение владельца). Проявление по наведению
        // отменено: с сенсорного ввода наведения нет как события, поэтому
        // копирование там было недоступно вовсе, а глазами страницу нельзя было
        // прочесть как список возможностей — о действии знал только тот, кто о
        // нём уже знал.
        //
        // Сдержанность держит ТОН, а не отсутствие: в покое значок тусклый
        // (`--kc-text-tertiary` — тот же тон, каким продукт печатает
        // второстепенное), и становится ярким, когда к нему потянулись. Ярче он
        // делается от наведения или клавиатурного захода на ЛЮБОГО `group`-предка:
        // у формы с текстом это сама кнопка, у `iconOnly` — обёртка вызывающего,
        // то есть тон меняется уже на подходе к имени, а не на двенадцати
        // пикселях самого значка.
        <Copy
          className={cn(
            "h-3 w-3 shrink-0",
            "text-[var(--kc-text-tertiary)] group-hover:text-[var(--kc-text)] group-focus-within:text-[var(--kc-text)]",
          )}
          // Длительность и кривая — токенами продукта, а не набором утилит со
          // своими значениями: движение по консоли читается как одно вещество.
          style={{ transition: "color var(--kc-duration) var(--kc-ease)" }}
        />
      )}
    </button>
  );
}
