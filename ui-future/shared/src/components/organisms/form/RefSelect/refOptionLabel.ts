// Подписи опций RefSelect — чистая часть, отделённая от компонента, чтобы её
// можно было проверять без графа antd/react-query.
//
// Про зоны и регионы отдельно. Именно здесь тенант встречается с geo: zoneId
// любого размещаемого ресурса берётся из этого списка. Зона, закрытая для
// размещения, выглядит в точности как открытая, если подпись об этом не
// говорит, — и запрос падает позже, на Create, без намёка, что дело в выборе.
// Закрытую опцию оставляем ВЫБИРАЕМОЙ намеренно: что разрешено, решает сервер,
// а гасить её здесь значило бы сузить права, которыми интерфейс не распоряжается.
// Она помечена, а не убрана. Отсутствующий флаг — не «закрыта»: молчание сервера
// не превращаем в отказ.

import { openForPlacementLabel } from "@shared/api/geo";
import {
  acceptsNewVolumes,
  lifecycleLabel,
  tierLabel,
} from "@shared/lib/storage-disk-type";

/** Основная подпись option. У большинства ресурсов — `name`, у User его нет. */
export function refOptionHead(
  refResource: string,
  row: Record<string, unknown>,
): string {
  if (refResource === "users") {
    return (
      (row.display_name as string) ||
      (row.email as string) ||
      (row.id as string) ||
      ""
    );
  }
  return (row.name as string) ?? "";
}

function placementSuffix(
  row: Record<string, unknown>,
  closedText: string,
): string {
  const label = openForPlacementLabel(
    row.open_for_placement as boolean | undefined,
  );
  return label.tone === "closed" ? closedText : "";
}

function join(parts: (string | undefined)[]): string {
  return parts.filter(Boolean).join(" · ");
}

/**
 * Короткая «адресная» приписка к option — чтобы различать безымянные ресурсы:
 * CIDR / IP / пул / регион / доступность для размещения.
 */
export function refOptionExtra(
  refResource: string,
  row: Record<string, unknown>,
): string {
  switch (refResource) {
    case "subnets": {
      // VPC-1: prefer the immutable primary anchor; fall back to additional
      // ranges. v4_cidr_blocks[] retired in favour of ipv4_cidr_primary.
      const primary4 = (row.ipv4_cidr_primary as string | undefined) ?? "";
      const primary6 = (row.ipv6_cidr_primary as string | undefined) ?? "";
      const extra4 = (row.ipv4_cidr_blocks as string[] | undefined) ?? [];
      const extra6 = (row.ipv6_cidr_blocks as string[] | undefined) ?? [];
      const cidrs = [primary4, primary6, ...extra4, ...extra6].filter(Boolean);
      return cidrs.length > 0 ? cidrs.join(", ") : "";
    }
    case "addresses": {
      const ext4 = (
        row.external_ipv4_address as { address?: string } | undefined
      )?.address;
      const int4 = (
        row.internal_ipv4_address as { address?: string } | undefined
      )?.address;
      const ext6 = (
        row.external_ipv6_address as { address?: string } | undefined
      )?.address;
      const int6 = (
        row.internal_ipv6_address as { address?: string } | undefined
      )?.address;
      return ext4 || int4 || ext6 || int6 || "";
    }
    case "gateways": {
      // Вид шлюза — ветвь oneof (`nat_gateway` | `egress_only_gateway`), и она же
      // единственное, что стоит показать вместо пустого имени: она говорит, ЧТО
      // шлюз делает. Ни одной ветви — пусто, а не «по умолчанию NAT»: выдать виду
      // имя наугад значило бы соврать о поведении.
      //
      // Прежняя ветвь `shared_egress_gateway` СНЯТА С КОНТРАКТА этой же волной
      // (`reserved 7` и `reserved "shared_egress_gateway"` в gateway.proto), и
      // чтение её из ответа было бы мёртвым: условие не истинно никогда, а
      // следующий читатель принял бы его за действующее.
      if (row.nat_gateway) return "nat";
      if (row.egress_only_gateway) return "egress-only";
      return "";
    }
    case "networks": {
      const ipv4 = (row.ipv4_cidr_blocks as string[] | undefined) ?? [];
      return ipv4.length > 0 ? ipv4.join(", ") : "";
    }
    case "address-pools": {
      // AddressPool несёт v4_cidr_blocks (13) + v6_cidr_blocks (14); слитное
      // `cidr_blocks` ЗАРЕЗЕРВИРОВАНО (tag 7) и с сервера не приходит — чтение
      // старого имени возвращало undefined молча, и подпись пула оставалась без
      // диапазонов. Список пулов в этом же пакете читает уже разделённую пару.
      const v4 = (row.v4_cidr_blocks as string[] | undefined) ?? [];
      const v6 = (row.v6_cidr_blocks as string[] | undefined) ?? [];
      const cidrs = [...v4, ...v6];
      const isDefault = row.is_default === true ? " · default" : "";
      return (cidrs.length > 0 ? cidrs.join(", ") : "") + isDefault;
    }
    // Зона: и запись `zones` (админ-каталог), и запись `compute-zones` — одна и
    // та же публичная выдача geo. Подборщик Instance.zone_id ссылается на вторую,
    // поэтому подпись, заведённая только для первой, до оператора не доезжала —
    // то есть ровно тот случай, ради которого написана шапка этого файла.
    case "zones":
    case "compute-zones": {
      // Регион зоны — авторитетное поле ответа geo, не разбор её идентификатора.
      const region = (row.region_id as string | undefined) ?? "";
      return join([region, placementSuffix(row, "закрыта для размещения")]);
    }
    case "route-tables":
    case "security-groups": {
      const net = (row.network_id as string | undefined) ?? "";
      return net ? `net:${net.slice(0, 8)}` : "";
    }
    // NLB-типы: показываем регион (+ схему) — как «адресную» инфу vpc-ресурсов.
    case "load-balancers":
    case "network-load-balancers": {
      const region = (row.region_id as string | undefined) ?? "";
      const scheme = (row.type as string | undefined) ?? "";
      return [region, scheme].filter(Boolean).join(" · ");
    }
    case "target-groups": {
      return (row.region_id as string | undefined) ?? "";
    }
    // Тип диска: ярус + пометка, если класс НЕ принимает новые тома.
    //
    // Ровно тот же случай, что у зоны, закрытой для размещения (см. шапку
    // файла): выведенный из обращения класс выглядит в точности как рабочий,
    // если подпись об этом не говорит, — и запрос падает позже, на Create
    // (FAILED_PRECONDITION «не принимает новые тома»), без намёка, что дело в
    // выборе. Опция остаётся ВЫБИРАЕМОЙ по той же причине: что разрешено,
    // решает сервер, а гасить её здесь значило бы сузить права, которыми
    // интерфейс не распоряжается. Она помечена, а не убрана.
    //
    // Отсутствие поля — не «выведен»: молчание сервера не превращаем в отказ,
    // поэтому пометка ставится только по НАЗВАННОМУ состоянию.
    case "disk-types": {
      const lifecycle = row.lifecycle as string | undefined;
      const tier = tierLabel(row.tier);
      const retired =
        lifecycle && !acceptsNewVolumes(lifecycle)
          ? (lifecycleLabel(lifecycle) ?? "")
          : "";
      return join([tier ?? "", retired]);
    }
    // Geo Region: name — head-label, id (region-1) — полезный extra.
    case "regions":
    case "compute-regions": {
      const id = (row.id as string | undefined) ?? "";
      return join([id, placementSuffix(row, "закрыт для размещения")]);
    }
    // Тип машины выбирают РАДИ РАЗМЕРА, поэтому размер и есть его приписка: без
    // неё дропдаун `machineTypeId` показывает список неразличимых имён. Sizing
    // после редизайна идёт единственным каналом `machine_type_id` →
    // `effective_resources`, отсюда и читаем.
    //
    // Память приходит СТРОКОЙ (int64 на wire сериализуется строкой), поэтому её
    // обязательно приводить к числу: конкатенация дала бы «4096/1024», а на
    // нечисловом входе — NaN, и подпись опустела бы молча.
    //
    // Неназванного размера не выдумываем: «0 vCPU» — утверждение о типе, которого
    // сервер не делал.
    //
    // Число ядер приходит из нетипизированной строки ответа, поэтому «не пусто»
    // проверки НЕ ЗАМЕНЯЕТ: непустым бывает и объект, а он подставился бы в
    // подпись как `[object Object]` — то есть у типа машины появился бы размер,
    // которого сервер не называл. Принимаются ровно две формы, в которых число
    // приходит с провода: число и строка (int64 сериализуется строкой).
    case "machine-types": {
      const er = (row.effective_resources as Record<string, unknown> | undefined) ?? {};
      const vcpuRaw = er.v_cpu;
      const vcpu = typeof vcpuRaw === "number" || typeof vcpuRaw === "string" ? `${vcpuRaw} vCPU` : "";
      const memMib = typeof er.memory_mib === "string" ? Number.parseInt(er.memory_mib, 10) : Number(er.memory_mib);
      const mem = Number.isFinite(memMib) && memMib > 0 ? `${Math.round(memMib / 1024)} ГиБ` : "";
      const fam = (row.family as string | undefined) ?? "";
      return [vcpu, mem, fam].filter(Boolean).join(" · ");
    }
    default:
      return "";
  }
}

/**
 * Подпись варианта в СПИСКЕ выбора: имя ресурса плюс адресная приписка.
 *
 * Здесь имя нужно — по нему человек и выбирает; приписка лишь различает
 * одноимённые и безымянные строки. Функция одна на оба поля подбора
 * (одиночное и множественное): две копии разошлись бы молча, и один и тот же
 * ресурс читался бы в двух формах по-разному.
 */
export function refOptionLabel(
  refResource: string,
  row: Record<string, unknown>,
): string {
  const id = (row.id as string) ?? "";
  const head = refOptionHead(refResource, row) || id;
  const extra = refOptionExtra(refResource, row);
  return extra ? `${head} · ${extra}` : head;
}

/**
 * Ресурсы, у которых ВЫБРАННОЕ значение называется адресом, а не именем.
 *
 * Сужение намеренное и перечислением, а не признаком «есть приписка»: у зоны
 * приписка — регион, у группы безопасности — сеть, у типа машины — размер, и ни
 * одна из них не заменяет имени. Адрес заменяет: интерфейс привязывают К
 * АДРЕСУ, и имя ресурса-адреса («adr-…», «reserved-2») о выборе не говорит
 * ничего. Решение владельца 2026-08-21: в фишке показываем только адрес.
 */
const TAGGED_BY_ADDRESS = new Set(["addresses"]);

/**
 * Подпись ВЫБРАННОГО значения — то, что стоит фишкой внутри поля.
 *
 * Отличается от подписи варианта в списке намеренно: список помогает выбрать
 * (там имя несёт смысл), а фишка называет уже сделанный выбор в узком поле,
 * где длинная строка либо обрезается, либо выдавливает соседние фишки.
 *
 * Пустой строки не возвращает никогда: край волен прислать ресурс без имени и
 * без адреса, и фишка без подписи выглядела бы как сбой поля, а не как ресурс,
 * о котором сервер ничего не сказал, — тогда остаётся идентификатор.
 */
export function refTagLabel(
  refResource: string,
  row: Record<string, unknown>,
): string {
  const id = (row.id as string) ?? "";
  if (TAGGED_BY_ADDRESS.has(refResource)) {
    return refOptionExtra(refResource, row) || refOptionHead(refResource, row) || id;
  }
  return refOptionHead(refResource, row) || id;
}
