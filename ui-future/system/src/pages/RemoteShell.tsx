// RemoteShell — провайдеры + фрейм, общие для self-contained федеративных
// exposes system-remote (SystemPage / TokensPage). Провайдер-обвязка
// (ThemeProvider / AntdApp / QueryClient / AuthProvider / StepUpModal /
// PageHeaderSlotProvider) + рамка (HeaderRightSlot / OperationBanner /
// GlobalResourceFormModal). Требует Router-предка (host предоставляет
// BrowserRouter; в standalone — App.tsx).

import { useEffect, useMemo, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp } from "antd";
import { ThemeProvider } from "@shared/lib/theme-context";
import { AuthProvider } from "@shared/contexts/AuthContext";
import { StepUpModal } from "@/components/molecules/auth/StepUpModal";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { OperationBanner } from "@shared/components/molecules/OperationBanner";
import { Toaster } from "@shared/components/molecules/Toaster";
import { GlobalResourceFormModal } from "@shared/components/organisms/GlobalResourceFormModal";
import "@shared/typography.css";
import "@shared/index.css";

export function RemoteShell({ children }: { children: ReactNode }) {
  const isTest = process.env.NODE_ENV === "test";
  const queryClient = useMemo(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            retry: isTest ? false : 1,
            gcTime: isTest ? Infinity : 5 * 60 * 1000,
            staleTime: 5_000,
            refetchOnWindowFocus: false,
          },
        },
      }),
    [isTest],
  );

  useEffect(() => {
    return () => {
      queryClient.clear();
    };
  }, [queryClient]);

  return (
    <ThemeProvider>
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <StepUpModal />
            <PageHeaderSlotProvider>
              {/* Здесь стояло ВТОРОЕ имя класса, под которое правил не объявляет ни
                  один лист стилей дерева: ноль вхождений и в исходниках, и в
                  собранном листе этого модуля. Мёртвое имя обещает оформление,
                  которого нет, и следующий читатель ищет его лист. Оно снято, а
                  не оставлено «на всякий случай», и здесь намеренно не
                  воспроизводится: имя в обратных кавычках читается как живая
                  координата. Рамку задаёт `vpc-remote-frame` — он объявлен в
                  общем листе, и его берут все федеративные модули. */}
              <section className="vpc-remote-frame">
                <div className="vpc-host-header-slots">
                  <div className="vpc-host-header-actions">
                    <HeaderRightSlot />
                  </div>
                </div>
                <OperationBanner />
                <div className="vpc-remote-content">{children}</div>
                <GlobalResourceFormModal />
                {/* Показ уведомлений. Без него КАЖДЫЙ сигнал об исходе мутации
                    уходит в очередь, которую никто не читает: этот модуль
                    маршрутизирует общие страницы создания/правки/удаления, они
                    исправно сообщали об отказе, и отказ не было видно нигде.
                    Так выглядела находка владельца на форме создания региона —
                    «ничего не происходит, но выдаёт 403». */}
                <Toaster />
              </section>
            </PageHeaderSlotProvider>
          </AuthProvider>
        </QueryClientProvider>
      </AntdApp>
    </ThemeProvider>
  );
}

export default RemoteShell;
