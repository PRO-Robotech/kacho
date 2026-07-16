import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

interface SlotContextValue {
  setHeaderRight: (node: ReactNode | null) => void;
  setBreadcrumb: (node: ReactNode | null) => void;
  setPageTitle: (title: string | null) => void;
  headerRight: ReactNode | null;
  breadcrumb: ReactNode | null;
  pageTitle: string | null;
}

const SlotContext = createContext<SlotContextValue | null>(null);

export function PageHeaderSlotProvider({ children }: { children: ReactNode }) {
  const [headerRight, setHeaderRight] = useState<ReactNode | null>(null);
  const [breadcrumb, setBreadcrumb] = useState<ReactNode | null>(null);
  const [pageTitle, setPageTitle] = useState<string | null>(null);

  return (
    <SlotContext.Provider value={{ headerRight, setHeaderRight, breadcrumb, setBreadcrumb, pageTitle, setPageTitle }}>
      {children}
    </SlotContext.Provider>
  );
}

function useSlot(): SlotContextValue {
  const ctx = useContext(SlotContext);
  if (!ctx) throw new Error("PageHeaderSlot hook called outside provider");
  return ctx;
}

export function useHeaderRight(node: ReactNode | null) {
  const { setHeaderRight } = useSlot();
  useEffect(() => {
    setHeaderRight(node);
    return () => setHeaderRight(null);
  }, [node, setHeaderRight]);
}

export function useBreadcrumb(node: ReactNode | null) {
  const { setBreadcrumb } = useSlot();
  useEffect(() => {
    setBreadcrumb(node);
    return () => setBreadcrumb(null);
  }, [node, setBreadcrumb]);
}

export function usePageTitle(title: string | null) {
  const { setPageTitle } = useSlot();
  useEffect(() => {
    setPageTitle(title);
    return () => setPageTitle(null);
  }, [title, setPageTitle]);
}

export function HeaderRightSlot() {
  const { headerRight } = useSlot();
  return <>{headerRight}</>;
}

export function HeaderBreadcrumbSlot() {
  const { breadcrumb } = useSlot();
  return <>{breadcrumb}</>;
}

export function PageTitleSlot() {
  const { pageTitle } = useSlot();
  if (!pageTitle) return null;
  return <h1 className="text-2xl font-semibold tracking-tight mb-4">{pageTitle}</h1>;
}
