// SubnetCidrPanel — две секции CIDR подсети в блоке «Обзор»: IPv4 и IPv6,
// каждая — самодостаточный CidrSection (свой SectionHeader с бейджем IPv4/IPv6,
// табличный read/edit-вид + batch-save, как «Статические маршруты»).
import { CidrSection } from "@shared/components/organisms/SubnetCidrManager";

interface Props {
  subnetId: string;
  /** VPC-1: immutable primary anchors (rendered locked) + additional ranges. */
  v4Primary?: string;
  v6Primary?: string;
  v4Blocks: string[];
  v6Blocks: string[];
  projectId: string | null;
}

export function SubnetCidrPanel({ subnetId, v4Primary, v6Primary, v4Blocks, v6Blocks, projectId }: Props) {
  return (
    <>
      <CidrSection subnetId={subnetId} kind="v4" primary={v4Primary} blocks={v4Blocks} projectId={projectId} />
      <CidrSection subnetId={subnetId} kind="v6" primary={v6Primary} blocks={v6Blocks} projectId={projectId} />
    </>
  );
}
