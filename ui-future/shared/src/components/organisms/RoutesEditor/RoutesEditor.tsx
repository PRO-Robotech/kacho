// RoutesEditor — controlled-редактор статических маршрутов для ФОРМЫ создания
// RouteTable. Вид — единый EditableKVTable (Префикс назначения | Следующий узел
// | ⌫ + dashed «Добавить маршрут»), общий с метками. Controlled (часть state
// формы, без собственного save). Backend поддерживает только next_hop_address
// (kacho-vpc#55) — gateway_id не вводим.
import { EditableKVTable } from "@shared/components/molecules/EditableKVTable";

export interface RouteEntry {
  destination_prefix: string;
  next_hop_address: string;
}

interface Props {
  value: RouteEntry[];
  onChange: (next: RouteEntry[]) => void;
  disabled?: boolean;
}

export function RoutesEditor({ value, onChange, disabled }: Props) {
  return (
    <EditableKVTable
      rows={value.map((r) => ({ a: r.destination_prefix, b: r.next_hop_address }))}
      onChange={(rows) => onChange(rows.map((r) => ({ destination_prefix: r.a, next_hop_address: r.b })))}
      colA={{ header: "Префикс назначения", placeholder: "10.0.0.0/24" }}
      colB={{ header: "Следующий узел", placeholder: "10.0.0.1" }}
      addLabel="Добавить маршрут"
      disabled={disabled}
    />
  );
}
