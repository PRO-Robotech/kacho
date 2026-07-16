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
              <section className="vpc-remote-frame iam-remote-frame">
                <div className="vpc-host-header-slots">
                  <div className="vpc-host-header-actions">
                    <HeaderRightSlot />
                  </div>
                </div>
                <OperationBanner />
                <div className="vpc-remote-content">{children}</div>
                <GlobalResourceFormModal />
              </section>
            </PageHeaderSlotProvider>
          </AuthProvider>
        </QueryClientProvider>
      </AntdApp>
    </ThemeProvider>
  );
}

export default RemoteShell;
