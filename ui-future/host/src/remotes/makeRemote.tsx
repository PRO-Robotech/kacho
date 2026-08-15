import { lazy, Suspense, useMemo, useState } from "react";
import type { ComponentType, FC } from "react";
import { Spin } from "antd";
import { useNavigate } from "react-router";
import { ModuleErrorBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import type { HostContext } from "../utils";

// Props every federated remote page accepts from the host shell.
export interface RemotePageProps {
  context?: HostContext;
  navigate?: (path: string) => void | Promise<void>;
}

// makeRemote — single source for the lazy()+Suspense+boundary+navigate scaffold
// shared by every module-federation remote. `loader` keeps its
// import("<remote>/<Page>") specifier literal inside the closure, so
// @originjs/vite-plugin-federation still statically resolves each remote.
//
// #371: каждый remote несёт СВОЮ границу отказа. `Suspense` ловит ОЖИДАНИЕ, а не
// отказ: без границы неудачная загрузка remoteEntry.js (сеть, кэш, выкатка) или
// ошибка рендера внутри модуля доходят до корня и снимают с экрана ВСЮ консоль.
// `moduleLabel` обязателен — экран отказа называет раздел по имени.
export function makeRemote(
  loader: () => Promise<Record<string, unknown>>,
  pick: (mod: Record<string, unknown>) => ComponentType<RemotePageProps> | undefined,
  moduleLabel: string,
  fallbackLabel?: string,
): FC<{ context: HostContext }> {
  return function Remote({ context }) {
    const navigate = useNavigate();
    // React кэширует ОТКЛОНЁННЫЙ промис lazy() навсегда: сброса границы мало,
    // повтор попадёт в тот же отказ немедленно. Поэтому попытка нумеруется, и на
    // каждую создаётся свежий lazy().
    const [attempt, setAttempt] = useState(0);
    const Page = useMemo(
      () =>
        lazy(async () => {
          const mod = await loader();
          const Component = pick(mod);
          if (!Component) throw new Error("remote module did not export a page component");
          return { default: Component };
        }),
      // eslint-disable-next-line react-hooks/exhaustive-deps -- ключ пересоздания: номер попытки
      [attempt],
    );

    return (
      <ModuleErrorBoundary moduleLabel={moduleLabel} onRetry={() => setAttempt((n) => n + 1)}>
        <Suspense fallback={<Spin aria-label={fallbackLabel} />}>
          <Page context={context} navigate={navigate} />
        </Suspense>
      </ModuleErrorBoundary>
    );
  };
}
