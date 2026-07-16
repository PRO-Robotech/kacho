// LabelsEditor — controlled editor для map<string,string> labels. Использовать
// в каждой модалке/форме (Subnet, Network, NIC, SG, AddressPool, ...). Вид —
// единый EditableKVTable (Ключ | Значение | ⌫ + dashed «Добавить метку»), общий
// со «Статическими маршрутами».
//
// Контракт: value — массив пар LabelEntry. State держится в parent, что
// исключает feedback-loop, из-за которого row пропадал при первом клике
// (entries=[{"":""}] → obj={} → useEffect мог сбросить локальный state).
//
// Утилиты: labelsToEntries / labelsFromEntries (canonical имена), labelsFromMap /
// labelsToMap (алиасы для совместимости со старыми импортами).

import { EditableKVTable } from "@shared/components/molecules/EditableKVTable";

export interface LabelEntry {
  key: string;
  value: string;
}

interface Props {
  value: LabelEntry[];
  onChange: (next: LabelEntry[]) => void;
  disabled?: boolean;
}

export function LabelsEditor({ value, onChange, disabled }: Props) {
  return (
    <EditableKVTable
      rows={value.map((l) => ({ a: l.key, b: l.value }))}
      onChange={(rows) => onChange(rows.map((r) => ({ key: r.a, value: r.b })))}
      colA={{ header: "Ключ", placeholder: "ключ" }}
      colB={{ header: "Значение", placeholder: "значение" }}
      addLabel="Добавить метку"
      disabled={disabled}
    />
  );
}

export function labelsToEntries(m: Record<string, string> | undefined): LabelEntry[] {
  if (!m) return [];
  return Object.entries(m).map(([key, value]) => ({ key, value }));
}

export function labelsFromEntries(entries: LabelEntry[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const l of entries) {
    if (l.key.trim()) out[l.key.trim()] = l.value;
  }
  return out;
}

// Алиасы для совместимости с InlineNetworkInterface*/InlineAddressPool*,
// которые импортировали под этими именами.
export const labelsFromMap = labelsToEntries;
export const labelsToMap = labelsFromEntries;
