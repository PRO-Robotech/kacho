import { lazy, Suspense } from "react";
import type { ComponentType, FC } from "react";
import { Spin } from "antd";
import { useNavigate } from "react-router-dom";
import type { HostContext } from "../utils";

// Props every federated remote page accepts from the host shell.
export interface RemotePageProps {
  context?: HostContext;
  navigate?: (path: string) => void | Promise<void>;
}

// makeRemote — single source for the lazy()+Suspense+navigate scaffold shared by
// every module-federation remote (Vpc/Iam/Dashboard). `loader` keeps its
// import("<remote>/<Page>") specifier literal inside the closure, so
// @originjs/vite-plugin-federation still statically resolves each remote.
export function makeRemote(
  loader: () => Promise<Record<string, unknown>>,
  pick: (mod: Record<string, unknown>) => ComponentType<RemotePageProps> | undefined,
  fallbackLabel?: string,
): FC<{ context: HostContext }> {
  const Page = lazy(async () => {
    const mod = await loader();
    const Component = pick(mod);
    if (!Component) throw new Error("remote module did not export a page component");
    return { default: Component };
  });

  return function Remote({ context }) {
    const navigate = useNavigate();
    return (
      <Suspense fallback={<Spin aria-label={fallbackLabel} />}>
        <Page context={context} navigate={navigate} />
      </Suspense>
    );
  };
}
