// NetworkCidrManager — объявленный супернет сети (IPv4/IPv6).
//
// Вид и механика — общие с CIDR подсети (`CidrTableSection`): супернет это тот
// же набор блоков, и показывать его иначе значило бы называть один предмет
// двумя именами. Прежде здесь стояла карточка с чипами, при том что подсеть
// рисовала таблицу с заголовком.
//
// VPC-1 wire-format (op-in-response — Network statusless, Operation{done:true}
// приходит сразу + полное тело в .response):
//   POST /vpc/v1/networks/{id}:add-cidr-blocks     { ipv4_cidr_blocks:[string] }
//   POST /vpc/v1/networks/{id}:remove-cidr-blocks  { ipv6_cidr_blocks:[string] }
//
// Супернет неизменяем через Update — эти глаголы единственный путь его
// изменения. Снятие последнего блока, покрывающего живую подсеть, край
// отвергает (`network CIDR block … still contains subnets`) — тогда блок
// остаётся на месте и показывается отказ.
import { REGISTRY } from "@shared/lib/resource-registry";
import { CidrTableSection, IP_PREFIXED_BLOCK_FIELDS } from "@shared/components/organisms/CidrTableSection";

const NETWORKS_API = "/vpc/v1/networks";

interface Props {
  networkId: string;
  v4Blocks: string[];
  v6Blocks: string[];
}

export function NetworkCidrManager({ networkId, v4Blocks, v6Blocks }: Props) {
  return (
    <>
      <CidrTableSection
        actionPath={(verb) => `${NETWORKS_API}/${networkId}:${verb}-cidr-blocks`}
        blockFields={IP_PREFIXED_BLOCK_FIELDS}
        invalidateKey="networks"
        expectOperation={REGISTRY["networks"].mutationsReturnOperation !== false}
        kind="v4"
        blocks={v4Blocks}
        title="CIDR"
        prefixExample="/16"
        placeholder="10.30.0.0/16"
        opNoun="CIDR"
        errNoun="CIDR"
        emptyText="CIDR-блоков нет"
      />
      <CidrTableSection
        actionPath={(verb) => `${NETWORKS_API}/${networkId}:${verb}-cidr-blocks`}
        blockFields={IP_PREFIXED_BLOCK_FIELDS}
        invalidateKey="networks"
        expectOperation={REGISTRY["networks"].mutationsReturnOperation !== false}
        kind="v6"
        blocks={v6Blocks}
        title="CIDR"
        prefixExample="/48"
        placeholder="fd00:30::/48"
        opNoun="CIDR"
        errNoun="CIDR"
        emptyText="CIDR-блоков нет"
      />
    </>
  );
}
