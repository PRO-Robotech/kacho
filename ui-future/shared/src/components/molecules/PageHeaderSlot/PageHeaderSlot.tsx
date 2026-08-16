import { createContext, useCallback, useContext, useEffect, useId, useMemo, useState, type ReactNode } from "react";

/**
 * Заявка на правый слот шапки: кто написал и что именно.
 *
 * Слот один на страницу, а писателей у карточки ресурса двое — оболочка карточки
 * и содержимое открытой вкладки. Общий `useState` на всех означал «побеждает тот,
 * чей эффект выполнился последним», а порядок эффектов задаёт React (родитель
 * после потомка) — то есть родитель с ПУСТЫМИ руками затирал написанное вкладкой.
 *
 * Заявки разводят этот спор явно: пустая заявка снимает только собственную и
 * никогда не стирает чужую, а при двух непустых показывается заявленная ПОЗЖЕ —
 * то есть внешним писателем, как и было до правки. Изменена ровно семантика
 * пустого значения (#425).
 */
interface Claim {
  id: string;
  node: ReactNode | null;
}

interface SlotContextValue {
  claimHeaderRight: (id: string, node: ReactNode | null) => void;
  releaseHeaderRight: (id: string) => void;
  setBreadcrumb: (node: ReactNode | null) => void;
  setPageTitle: (title: string | null) => void;
  headerRight: ReactNode | null;
  breadcrumb: ReactNode | null;
  pageTitle: string | null;
}

const SlotContext = createContext<SlotContextValue | null>(null);

export function PageHeaderSlotProvider({ children }: { children: ReactNode }) {
  const [claims, setClaims] = useState<Claim[]>([]);
  const [breadcrumb, setBreadcrumb] = useState<ReactNode | null>(null);
  const [pageTitle, setPageTitle] = useState<string | null>(null);

  // Тождественная правка возвращает ПРЕЖНИЙ массив: иначе каждая перерисовка
  // вызывающего меняла бы состояние провайдера и запускала следующую — цикл,
  // который виден не падением утверждения, а исчерпанием памяти прогона
  // (см. соседнюю пробу про стабильность узла).
  const claimHeaderRight = useCallback((id: string, node: ReactNode | null) => {
    setClaims((prev) => {
      const i = prev.findIndex((c) => c.id === id);
      if (i < 0) return [...prev, { id, node }];
      if (prev[i].node === node) return prev;
      const next = prev.slice();
      next[i] = { id, node };
      return next;
    });
  }, []);

  const releaseHeaderRight = useCallback((id: string) => {
    setClaims((prev) => (prev.some((c) => c.id === id) ? prev.filter((c) => c.id !== id) : prev));
  }, []);

  const headerRight = useMemo(() => {
    for (let i = claims.length - 1; i >= 0; i -= 1) {
      if (claims[i].node) return claims[i].node;
    }
    return null;
  }, [claims]);

  const value = useMemo(
    () => ({
      headerRight,
      claimHeaderRight,
      releaseHeaderRight,
      breadcrumb,
      setBreadcrumb,
      pageTitle,
      setPageTitle,
    }),
    [headerRight, claimHeaderRight, releaseHeaderRight, breadcrumb, pageTitle],
  );

  return <SlotContext.Provider value={value}>{children}</SlotContext.Provider>;
}

function useSlot(): SlotContextValue {
  const ctx = useContext(SlotContext);
  if (!ctx) throw new Error("PageHeaderSlot hook called outside provider");
  return ctx;
}

export function useHeaderRight(node: ReactNode | null) {
  const { claimHeaderRight, releaseHeaderRight } = useSlot();
  const id = useId();

  useEffect(() => {
    claimHeaderRight(id, node);
  }, [id, node, claimHeaderRight]);

  // Снятие заявки — ОТДЕЛЬНЫМ эффектом, зависящим только от личности писателя.
  // Слитое с записью, оно снималось бы и возвращалось на каждой смене узла, и
  // между этими двумя тактами заявка исчезала бы из перечня, отдавая слот
  // соседу — мигание кнопок на ровном месте.
  useEffect(() => () => releaseHeaderRight(id), [id, releaseHeaderRight]);
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
