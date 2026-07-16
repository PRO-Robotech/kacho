import { lazy, Suspense } from "react";
import type { FC } from "react";
import { Spin } from "antd";
import { useNavigate } from "react-router-dom";
import type { HostContext } from "../utils";

const RegistryPage = lazy(async () => {
  const mod = await import("registry/RegistryPage");
  return { default: mod.default ?? mod.RegistryPage };
});

export const RegistryRemote: FC<{ context: HostContext }> = ({ context }) => {
  const navigate = useNavigate();
  return (
    <Suspense fallback={<Spin />}>
      <RegistryPage context={context} navigate={navigate} />
    </Suspense>
  );
};
