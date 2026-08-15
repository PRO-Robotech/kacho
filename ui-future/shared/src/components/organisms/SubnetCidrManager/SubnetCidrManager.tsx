// CidrSection — CIDR-блоки подсети одного семейства (v4/v6) в блоке «Обзор».
//
// Вид и механика живут в общем `CidrTableSection`: тот же набор блоков есть и у
// сети (объявленный супернет), и рисовать их по-разному значило бы называть
// один предмет двумя именами. Здесь остаётся только то, чем подсеть от сети
// отличается: неизменяемый ОСНОВНОЙ якорь семейства, который показывается
// запертым и через глаголы никогда не проходит.
//
// Край (services/vpc) запрещает менять CIDR через PATCH (immutable after
// Subnet.Create) — только `:add-cidr-blocks` / `:remove-cidr-blocks`.
import { CidrTableSection, IP_PREFIXED_BLOCK_FIELDS } from "@shared/components/organisms/CidrTableSection";

const SUBNETS_API = "/vpc/v1/subnets";

type CidrKind = "v4" | "v6";

interface SectionProps {
  subnetId: string;
  kind: CidrKind;
  /** Дополнительные диапазоны (изменяемы глаголами). */
  blocks: string[];
  /** VPC-1: неизменяемый основной якорь семейства. Пуст у v6-only / v4-only. */
  primary?: string;
  /** Не используется (инвалидация по ключу владельца), оставлен для совместимости. */
  projectId?: string | null;
}

export function CidrSection({ subnetId, kind, blocks, primary }: SectionProps) {
  return (
    <CidrTableSection
      actionPath={(verb) => `${SUBNETS_API}/${subnetId}:${verb}-cidr-blocks`}
      blockFields={IP_PREFIXED_BLOCK_FIELDS}
      invalidateKey="subnets"
      kind={kind}
      blocks={blocks}
      primary={primary}
      primaryHint="Основной CIDR неизменяем после создания подсети"
      title="CIDR"
      prefixExample={kind === "v4" ? "/24" : "/64"}
      placeholder={kind === "v4" ? "10.0.1.0/24" : "fd00:1234::/64"}
      opNoun="CIDR"
      errNoun="CIDR"
      emptyText="CIDR-блоков нет"
    />
  );
}
