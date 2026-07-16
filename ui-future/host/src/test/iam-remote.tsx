import type { FC } from "react";
import type { HostContext } from "../utils";

export interface IamPageProps {
  context?: HostContext;
}

export const IamPage: FC<IamPageProps> = ({ context }) => {
  const label = context?.account?.name || context?.account?.id || "IAM remote";
  return (
    <section data-testid="iam-remote">
      <h3>Identity and Access Management</h3>
      <span>{label}</span>
    </section>
  );
};

export default IamPage;
