// IamListShell — единая «поверхность списка» для кастомных IAM-страниц (Roles,
// Groups, AccessBindings, Access, Users), которые не проходят через generic
// ResourceListPage (у них свои табы/фильтры/редакторы). Даёт тот же вид, что и
// generic-списки: белая kc-surface-подложка на всю высоту до футера, шапка
// PanelHeader (иконка ресурса + «Список» + заголовок + счётчик), а тело —
// flex-column, который заполняет остаток поверхности и скроллится ВНУТРИ неё
// (не тянет страницу). Устраняет расхождение: кастомные iam-страницы висели на
// голом тёмном фоне без подложки, а таблицы обрезались у футера.

import { type ReactNode, useEffect, useRef, useState } from "react";
import { Tag } from "antd";
import { PanelHeader } from "@shared/components/molecules/PanelHeader";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";

interface Props {
  /** specId для ResourceIcon (тот же глиф, что в сайдбаре): roles/groups/… */
  specId: string;
  title: ReactNode;
  /** Счётчик рядом с заголовком (как у generic-списка). */
  count?: number;
  /** Правая часть шапки — фильтры/поиск (CTA «Создать» живёт в шапке страницы). */
  right?: ReactNode;
  children: ReactNode;
}

// IamListShell — kc-surface + PanelHeader (фикс. сверху) + fill-контейнер тела.
export function IamListShell({ specId, title, count, right, children }: Props) {
  return (
    <div
      className="kc-surface"
      style={{ padding: 20, height: "100%", overflow: "hidden", display: "flex", flexDirection: "column" }}
    >
      <div style={{ flexShrink: 0, marginBottom: 12 }}>
        <PanelHeader
          icon={<ResourceIcon specId={specId} />}
          eyebrow="Список"
          title={
            <span style={{ display: "inline-flex", alignItems: "center", gap: 8, height: 20, lineHeight: "20px" }}>
              {title}
              {count != null && (
                <Tag
                  style={{
                    margin: 0,
                    fontSize: 11.5,
                    fontWeight: 600,
                    lineHeight: "16px",
                    height: 18,
                    paddingInline: 6,
                    borderRadius: 5,
                  }}
                >
                  {count}
                </Tag>
              )}
            </span>
          }
          right={right}
        />
      </div>
      <div style={{ flex: 1, minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column" }}>{children}</div>
    </div>
  );
}

// useTableScrollY — фиксирует thead и скроллит тело antd-Table внутри своей
// области (как ResourceTable). Оберни <Table> в
//   <div ref={wrapRef} className="kc-table-fill" style={{ flex:1, minHeight:0, minWidth:0 }}>
// и передай Table scroll={{ x: "max-content", y: scrollY }}. scroll.x=max-content
// снимает посимвольный перенос колонок; scroll.y — вертикальный скролл тела.
export function useTableScrollY() {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [scrollY, setScrollY] = useState<number | undefined>(undefined);
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const recompute = () => {
      const thead = el.querySelector(".ant-table-thead") as HTMLElement | null;
      const theadH = thead?.offsetHeight ?? 40;
      const avail = el.clientHeight - theadH;
      setScrollY(avail > 48 ? avail : undefined);
    };
    const ro = new ResizeObserver(recompute);
    ro.observe(el);
    recompute();
    return () => ro.disconnect();
  }, []);
  return { wrapRef, scrollY };
}
