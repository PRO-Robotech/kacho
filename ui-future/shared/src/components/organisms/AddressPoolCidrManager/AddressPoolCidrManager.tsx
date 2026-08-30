// AddressPoolCidrManager — диапазоны пула адресов (IPv4/IPv6).
//
// Вид и механика — общие с CIDR подсети, супернетом сети и составом набора
// префиксов (`CidrTableSection`): это тот же предмет — набор блоков, который
// правят по одному, — и рисовать его иначе значило бы назвать один предмет
// пятью именами. Прежде здесь стояла карточка с чипами-тегами и английской
// подписью `IPv4 CIDR blocks`, при том что четыре соседних набора рисовали
// таблицу с бейджем семейства и русским заголовком.
//
// Что у этого ресурса ДЕЙСТВИТЕЛЬНО своё — и потому объявлено здесь, рядом с
// путём его глагола, а не унесено в умолчание секции:
//
//   * глаголы названы одним словом (`:addCidrBlocks`), а не через дефис;
//   * тело несёт id пула ВТОРЫМ вхождением — сверх пути. Край отбрасывает
//     неизвестный ключ молча, поэтому подстановка чужого поля вернула бы успех,
//     ничего не изменив;
//   * поля семейства короткие (`v4_…`), как у набора префиксов, а не `ipv4_…`;
//   * ответ СИНХРОННЫЙ — пул возвращается сам, без Operation. Секция это уже
//     умеет: операции в ответе нет, значит обновляем кэш сразу.
//
// KAC-269: правка пула CIDR больше не меняет (`UpdateAddressPoolRequest` этих
// полей не несёт вовсе) — эти два глагола единственный путь. Снятие блока, в
// котором есть выданные адреса, край отвергает; тогда блок остаётся на месте, а
// причина показывается.
import { REGISTRY } from "@shared/lib/resource-registry";
import { CidrTableSection, type CidrBlockFields } from "@shared/components/organisms/CidrTableSection";

const POOLS_API = "/vpc/v1/addressPools";

/** Имена полей семейства у пула — короткие, как у набора префиксов. */
const BLOCK_FIELDS: CidrBlockFields = { v4: "v4_cidr_blocks", v6: "v6_cidr_blocks" };

interface Props {
  poolId: string;
  v4Blocks: string[];
  v6Blocks: string[];
}

export function AddressPoolCidrManager({ poolId, v4Blocks, v6Blocks }: Props) {
  // Путь собирается ЗДЕСЬ, а не в общей секции: голова литерала обязана
  // оставаться статически видимой пробе поверхности API (`lib/api-path-surface`),
  // которая резолвит её в константу этого файла. Спрятав путь за проп, ресурс
  // ушёл бы из-под её наблюдения вовсе — и перепись показала бы это как
  // улучшение остатка.
  //
  // Глагол пула — одно слово, поэтому имя действия дописывается к самому виду
  // действия: `add` + `CidrBlocks`, `remove` + `CidrBlocks`. Условие с двумя
  // готовыми строками читалось бы как выбор из двух путей, тогда как путь один.
  const actionPath = (verb: "add" | "remove") => `${POOLS_API}/${poolId}:${verb}CidrBlocks`;

  // Заполненность пула считается из тех же блоков и лежит под своим ключом:
  // без него число на виджете пережило бы снятие блока, из которого считалось.
  const alsoInvalidate = [["pool-util", poolId]];

  return (
    <>
      <CidrTableSection
        actionPath={actionPath}
        blockFields={BLOCK_FIELDS}
        extraBody={{ address_pool_id: poolId }}
        invalidateKey="address-pools"
        expectOperation={REGISTRY["address-pools"].mutationsReturnOperation !== false}
        alsoInvalidate={alsoInvalidate}
        kind="v4"
        blocks={v4Blocks}
        title="CIDR"
        prefixExample="/24"
        placeholder="198.51.100.0/24"
        opNoun="CIDR"
        errNoun="CIDR"
        emptyText="CIDR-блоков нет"
      />
      <CidrTableSection
        actionPath={actionPath}
        blockFields={BLOCK_FIELDS}
        extraBody={{ address_pool_id: poolId }}
        invalidateKey="address-pools"
        expectOperation={REGISTRY["address-pools"].mutationsReturnOperation !== false}
        alsoInvalidate={alsoInvalidate}
        kind="v6"
        blocks={v6Blocks}
        title="CIDR"
        prefixExample="/64"
        placeholder="2001:db8::/64"
        opNoun="CIDR"
        errNoun="CIDR"
        emptyText="CIDR-блоков нет"
      />
    </>
  );
}
