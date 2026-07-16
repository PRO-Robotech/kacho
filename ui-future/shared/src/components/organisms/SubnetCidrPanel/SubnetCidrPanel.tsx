// SubnetCidrPanel — две секции CIDR подсети в блоке «Обзор»: IPv4 и IPv6,
// каждая — самодостаточный CidrSection (свой SectionHeader с бейджем IPv4/IPv6,
// табличный read/edit-вид + batch-save, как «Статические маршруты»).
import { CidrSection } from "@shared/components/organisms/SubnetCidrManager";

interface Props {
  subnetId: string;
  v4Blocks: string[];
  v6Blocks: string[];
  projectId: string | null;
}

export function SubnetCidrPanel({ subnetId, v4Blocks, v6Blocks, projectId }: Props) {
  return (
    <>
      <CidrSection subnetId={subnetId} kind="v4" blocks={v4Blocks} projectId={projectId} />
      <CidrSection subnetId={subnetId} kind="v6" blocks={v6Blocks} projectId={projectId} />
    </>
  );
}
