import type { FC } from "react";
import type { HostContext } from "../utils";

export interface VpcPageProps {
  context?: HostContext;
}

export const VpcPage: FC<VpcPageProps> = ({ context }) => {
  const label = context?.project?.name || context?.project?.id || "VPC remote";
  return (
    <section data-testid="vpc-remote">
      <h3>Virtual Private Cloud</h3>
      <span>{label}</span>
    </section>
  );
};

export default VpcPage;
