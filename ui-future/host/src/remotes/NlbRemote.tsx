import { lazy, Suspense } from "react";
import type { FC } from "react";
import { Spin } from "antd";
import { useNavigate } from "react-router-dom";
import type { HostContext } from "../utils";

const NlbPage = lazy(async () => {
  const mod = await import("nlb/NlbPage");
  return { default: mod.default ?? mod.NlbPage };
});

export const NlbRemote: FC<{ context: HostContext }> = ({ context }) => {
  const navigate = useNavigate();
  return (
    <Suspense fallback={<Spin />}>
      <NlbPage context={context} navigate={navigate} />
    </Suspense>
  );
};
