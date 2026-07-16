// LabelsCell — рендер map<string,string> labels как набор chips key=value.
// Пустой / null → "—". Click на chip копирует "key=value".

import { Tag } from "antd";
import { toast } from "@shared/lib/toast";

interface Props {
  labels?: Record<string, string> | null;
  /** Максимум видимых chips; остальные сворачиваются в "+N". */
  max?: number;
}

export function LabelsCell({ labels, max = 4 }: Props) {
  const entries = Object.entries(labels ?? {});
  if (entries.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  const shown = entries.slice(0, max);
  const hiddenCount = entries.length - shown.length;

  const copy = (e: React.MouseEvent, kv: string) => {
    e.stopPropagation();
    navigator.clipboard
      .writeText(kv)
      .then(() => toast.success(`Скопировано: ${kv}`))
      .catch(() => toast.error("Не удалось скопировать"));
  };

  return (
    <span style={{ display: "inline-flex", gap: 4, flexWrap: "wrap", maxWidth: 320 }}>
      {shown.map(([k, v]) => {
        const kv = `${k}=${v}`;
        return (
          <Tag
            key={k}
            onClick={(e) => copy(e, kv)}
            style={{ cursor: "pointer", margin: 0, fontFamily: "ui-monospace, monospace", fontSize: 11 }}
            title={`Click to copy ${kv}`}
          >
            {kv}
          </Tag>
        );
      })}
      {hiddenCount > 0 && <Tag style={{ margin: 0, fontSize: 11 }}>+{hiddenCount}</Tag>}
    </span>
  );
}
