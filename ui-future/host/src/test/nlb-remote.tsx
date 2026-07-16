import type { FC } from "react";
import type { HostContext } from "../utils";

export interface NlbPageProps {
  context?: HostContext;
}

export const NlbPage: FC<NlbPageProps> = ({ context }) => {
  const label = context?.project?.name || context?.project?.id || "NLB remote";
  return (
    <section data-testid="nlb-remote">
      <h3>Network Load Balancing</h3>
      <span>{label}</span>
    </section>
  );
};

export default NlbPage;
