// LabelsEditor (form-wrapper) — адаптер общего controlled `LabelsEditor` для
// generic FormFieldRenderer-схемы (storage = obj, в нашем UI — entries).
//
// Чтобы избежать feedback-loop при первом клике «Добавить метку»
// (entries=[{"":""}] → obj={} → parent перерисует value без изменений →
// useEffect не должен сбросить rows), синхронизация с parent — через
// signature-ref: после собственного update мы запоминаем sig нашего obj и не
// реагируем на тот же sig обратно.
//
// External hydrate (например, edit-форма после первого fetch) идёт через
// другую signature → setRows вызывается ровно один раз.
import { useEffect, useRef, useState } from "react";
import { Label } from "@/components/atoms/ui/Input";
import { getByPath, setByPath } from "@/lib/path";
import {
  LabelsEditor as LabelsEditorBase,
  labelsToEntries,
  labelsFromEntries,
  type LabelEntry,
} from "@/components/organisms/LabelsEditor";

interface Props {
  pathPrefix: string;
  path: string;
  label: string;
  description?: string;
  value: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  disabled?: boolean;
}

export function LabelsEditor({ path, label, description, value, onChange, disabled }: Props) {
  const curRaw = getByPath(value, path);
  const cur =
    curRaw && typeof curRaw === "object" && !Array.isArray(curRaw) ? (curRaw as Record<string, string>) : undefined;

  const [rows, setRows] = useState<LabelEntry[]>(() => labelsToEntries(cur));
  const sigRef = useRef<string>(JSON.stringify(cur ?? {}));

  useEffect(() => {
    const incomingSig = JSON.stringify(cur ?? {});
    if (incomingSig === sigRef.current) return;
    sigRef.current = incomingSig;
    setRows(labelsToEntries(cur));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(cur ?? {})]);

  const handleChange = (next: LabelEntry[]) => {
    setRows(next);
    const obj = labelsFromEntries(next);
    sigRef.current = JSON.stringify(obj);
    onChange(setByPath(value, path, obj));
  };

  return (
    <div className={label ? "space-y-1.5" : ""}>
      {label && <Label description={description}>{label}</Label>}
      <LabelsEditorBase value={rows} onChange={handleChange} disabled={disabled} />
    </div>
  );
}
