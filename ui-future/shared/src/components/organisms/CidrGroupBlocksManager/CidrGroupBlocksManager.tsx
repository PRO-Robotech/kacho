// CidrGroupBlocksManager — состав набора префиксов (IPv4/IPv6).
//
// Вид и механика — общие с CIDR подсети и супернетом сети (`CidrTableSection`):
// это тот же набор блоков, и показывать его иначе значило бы назвать один
// предмет двумя именами.
//
// Состав НЕ меняется правкой: `UpdateCidrGroupRequest` полей состава не несёт
// вовсе — только глаголы, отвечающие Operation:
//   POST /vpc/v1/cidrGroups/{id}:add-cidr-blocks     { v4_cidr_blocks:[string] }
//   POST /vpc/v1/cidrGroups/{id}:remove-cidr-blocks  { v6_cidr_blocks:[string] }
//
// Имена полей семейства у этого ресурса КОРОЧЕ, чем у подсети и сети
// (`v4_…` против `ipv4_…`), поэтому пара объявлена здесь, рядом со своим путём
// глагола, а не взята умолчанием секции: край отбрасывает неизвестный ключ
// молча, и действие вернуло бы успех, ничего не изменив.
import { REGISTRY } from "@shared/lib/resource-registry";
import { CidrTableSection, type CidrBlockFields } from "@shared/components/organisms/CidrTableSection";

const CIDR_GROUPS_API = "/vpc/v1/cidrGroups";

const BLOCK_FIELDS: CidrBlockFields = { v4: "v4_cidr_blocks", v6: "v6_cidr_blocks" };

interface Props {
  cidrGroupId: string;
  v4Blocks: string[];
  v6Blocks: string[];
}

export function CidrGroupBlocksManager({ cidrGroupId, v4Blocks, v6Blocks }: Props) {
  return (
    <>
      <CidrTableSection
        actionPath={(verb) => `${CIDR_GROUPS_API}/${cidrGroupId}:${verb}-cidr-blocks`}
        blockFields={BLOCK_FIELDS}
        invalidateKey="cidr-groups"
        expectOperation={REGISTRY["cidr-groups"].mutationsReturnOperation !== false}
        kind="v4"
        blocks={v4Blocks}
        title="Префиксы"
        prefixExample="/24"
        placeholder="203.0.113.0/24"
        opNoun="префикса"
        errNoun="префикс"
        emptyText="Префиксов нет"
      />
      <CidrTableSection
        actionPath={(verb) => `${CIDR_GROUPS_API}/${cidrGroupId}:${verb}-cidr-blocks`}
        blockFields={BLOCK_FIELDS}
        invalidateKey="cidr-groups"
        expectOperation={REGISTRY["cidr-groups"].mutationsReturnOperation !== false}
        kind="v6"
        blocks={v6Blocks}
        title="Префиксы"
        prefixExample="/48"
        placeholder="2001:db8::/48"
        opNoun="префикса"
        errNoun="префикс"
        emptyText="Префиксов нет"
      />
    </>
  );
}
