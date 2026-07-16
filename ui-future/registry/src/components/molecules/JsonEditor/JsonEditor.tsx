import { useState } from "react";
import { cn } from "@/lib/utils";

interface Props {
  value: string;
  onChange: (v: string) => void;
  rows?: number;
  className?: string;
  placeholder?: string;
}

export function JsonEditor({ value, onChange, rows = 18, className, placeholder }: Props) {
  const [parseErr, setParseErr] = useState<string | null>(null);

  const handle = (s: string) => {
    onChange(s);
    if (!s.trim()) {
      setParseErr(null);
      return;
    }
    try {
      JSON.parse(s);
      setParseErr(null);
    } catch (e) {
      setParseErr((e as Error).message);
    }
  };

  return (
    <div className="space-y-1">
      <textarea
        rows={rows}
        spellCheck={false}
        className={cn(
          "w-full font-mono text-xs rounded-md border border-border bg-zinc-950 text-zinc-100 p-3",
          "focus:outline-none focus:ring-1 focus:ring-primary",
          parseErr && "ring-1 ring-rose-500",
          className,
        )}
        value={value}
        onChange={(e) => handle(e.target.value)}
        placeholder={placeholder}
      />
      {parseErr && <div className="text-xs text-rose-600">JSON: {parseErr}</div>}
    </div>
  );
}
