// NlbVipCell — VIP-адрес(а) балансировщика в колонке списка и в строке обзора.
//
// Поля `v4_address_id` / `v6_address_id` ссылаются на Address домена vpc, то
// есть на ЧУЖОЙ ресурс со своей карточкой. Значит это обычная ссылка на ресурс,
// и рисует её единственный вид ссылки консоли — `ResourceLink` (канон §9:
// «двух реализаций одного вида не бывает»).
//
// Здесь стояла своя: собственный `<Link>` с собственным набором классов,
// собственной сборкой адреса `/projects/<id>/vpc/addresses/<id>` и без иконки
// типа, значка копирования, подсказки с полным значением и признака «я в строке
// свойств». В одной таблице из-за этого жили два поведения: у «Имени» отзывался
// общий значок справа, у адреса — ничего.
//
// Что осталось своим и почему: РЕЗОЛВ САМОГО IP. `RefNameLink` показывает имя
// ресурса, а здесь читателю нужен адрес, который лежит внутри одной из четырёх
// ветвей ответа. Поэтому список адресов проекта запрашивается здесь, а рисуется
// уже общим компонентом.

import type { FC } from "react";
import { useParams } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import { ResourceLink } from "@/components/molecules/ResourceLink";
import { getByPath } from "@/lib/resource-registry";
import { MONO_FONT } from "@shared/components/organisms/form/editor-surface";

export interface NlbVipCellProps {
  v4AddressId?: string;
  v6AddressId?: string;
}

// addressIp — вытаскивает сам IP из Address-ресурса (external/internal, v4/v6).
function addressIp(data: Record<string, unknown> | undefined): string {
  if (!data) return "";
  return (
    getByPath<string>(data, "external_ipv4_address.address") ??
    getByPath<string>(data, "external_ipv6_address.address") ??
    getByPath<string>(data, "internal_ipv4_address.address") ??
    getByPath<string>(data, "internal_ipv6_address.address") ??
    ""
  );
}

// VipAddressLink — резолвит Address по id и показывает сам IP ссылкой на его
// карточку. Резолв идёт ОДНОЙ выборкой адресов проекта (ключ на проект,
// TanStack дедуплицирует), а не запросом на каждую ячейку: список
// балансировщиков делит один запрос. Пока не загрузилось — идентификатор,
// усечённый тем же правилом, что у всякой ссылки.
const VipAddressLink: FC<{ id: string }> = ({ id }) => {
  const { projectId } = useParams();
  const { data } = useQuery({
    queryKey: ["addresses-by-project", projectId],
    queryFn: () =>
      api.list<{ addresses: Array<Record<string, unknown>> }>("/vpc/v1/addresses", {
        project_id: projectId ?? "",
        pageSize: "1000",
      }),
    enabled: !!projectId,
    staleTime: 30_000,
  });
  const addr = (data?.addresses ?? []).find((a) => (a.id as string) === id);
  // Моноширинным набором — САМ АДРЕС: это машинное значение, и цифры в нём
  // читают по столбикам. Шрифт берётся общей константой редакторов, а не своим
  // сокращением: два перечня гарнитур расходятся молча.
  return (
    <span style={{ fontFamily: MONO_FONT }}>
      <ResourceLink specId="addresses" id={id} name={addressIp(addr)} projectId={projectId ?? null} icon plain />
    </span>
  );
};

export const NlbVipCell: FC<NlbVipCellProps> = ({ v4AddressId, v6AddressId }) => {
  const ids = [v4AddressId, v6AddressId].filter((x): x is string => !!x);
  if (ids.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  // Оба семейства — КАЖДОЕ своей строкой: балансировщик двойного стека несёт два
  // адреса, и свёртка второго в «ещё 1» назвала бы число вместо адреса.
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
      {ids.map((id) => (
        <VipAddressLink key={id} id={id} />
      ))}
    </span>
  );
};
