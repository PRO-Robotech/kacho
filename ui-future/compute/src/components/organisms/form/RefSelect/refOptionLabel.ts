// Подписи опций RefSelect — чистая часть, отделённая от компонента, чтобы её
// можно было проверять без графа antd/react-query.
//
// Общие ресурсы (подсеть, адрес, сеть, зона, регион, шлюз, пул, RT/SG, NLB)
// подписываются ОБЩЕЙ реализацией из @shared. Здесь лежала её копия, и копия
// разошлась молча: подсеть в ней продолжала читать v4_cidr_blocks/v6_cidr_blocks,
// снятые в стволе (Subnet — `reserved 10, 11`, вместо них ipv4_cidr_primary +
// ipv4_cidr_blocks), а у зоны не было признака доступности размещения, который
// пришёл на место снятого Zone.status. Обе поломки беззвучны — подпись просто
// пустеет. Поэтому расхождение снято по построению: своим здесь остаётся ровно
// то, чего у общей реализации нет.
//
// @shared виден этому remote'у и в сборке (resolve.alias в vite.config.ts), и в
// тестах (moduleNameMapper в jest.config.cjs) — как и другим модулям compute,
// уже импортирующим оттуда.

import { refOptionExtra, refOptionHead } from "@shared/components/organisms/form/RefSelect/refOptionLabel";

/** Основная подпись option. У большинства ресурсов — `name`, у User его нет. */
export function headLabelFor(refResource: string, row: Record<string, unknown>): string {
  return refOptionHead(refResource, row);
}

/**
 * Короткая «адресная» приписка к option.
 *
 * MachineType — ресурс этого домена, и у общей реализации его нет: показываем
 * размер (vCPU · память) + семейство, чтобы различать безымянно-похожие типы в
 * дропдауне machineTypeId. Sizing после редизайна идёт единственным каналом
 * machine_type_id → effective_resources, поэтому подпись строится из него.
 */
export function extraInfoFor(refResource: string, row: Record<string, unknown>): string {
  if (refResource === "machine-types") {
    const er = (row.effective_resources as Record<string, unknown> | undefined) ?? {};
    const vcpu = er.v_cpu != null ? `${er.v_cpu} vCPU` : "";
    const memMib = typeof er.memory_mib === "string" ? Number.parseInt(er.memory_mib, 10) : Number(er.memory_mib);
    const mem = Number.isFinite(memMib) && memMib > 0 ? `${Math.round(memMib / 1024)} ГиБ` : "";
    const fam = (row.family as string | undefined) ?? "";
    return [vcpu, mem, fam].filter(Boolean).join(" · ");
  }
  return refOptionExtra(refResource, row);
}
