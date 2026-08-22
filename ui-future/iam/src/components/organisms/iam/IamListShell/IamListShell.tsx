// IamListShell — «поверхность списка» для кастомных IAM-страниц (Роли, Группы,
// Привязки доступа, Права доступа), которые не проходят через generic
// ResourceListPage: у них свои табы, отборы и редакторы.
//
// ТА ЖЕ ШАПКА И ТОТ ЖЕ РЯД РУЧЕК, ЧТО У GENERIC-СПИСКА.
//
// Здесь стояла своя конструкция шапки (`PanelHeader`): иконка-плитка,
// надзаголовок «Список», заголовок и счётчик строк. Ни одного из четырёх у
// generic-списка нет, и переход с /iam/accounts на /iam/roles читался как
// переход в другой продукт:
//
//   • надзаголовок «Список» сообщал СПОСОБ показа вместо предмета и стоял
//     одинаковым на каждой странице — то есть не различал ничего;
//   • счётчик снят решением владельца ВЕЗДЕ, а не только у generic-списка;
//   • иконка-плитка — признак идентичности ЭКЗЕМПЛЯРА, у типа её место в
//     навигации;
//   • поля страницы были `20` против `20px 24px` у generic — четыре точки по
//     горизонтали, не видные на статичной странице и отлично видные в переходе.
//
// Всё это приходит теперь из одного места (`PageHead` + `PAGE_PADDING`), и
// правка геометрии доезжает до кастомных страниц вместе с generic.

import { type ReactNode, useEffect, useRef, useState } from "react";
import { PageHead, PAGE_PADDING } from "@shared/components/organisms/DetailShell/PageHead";

interface Props {
  /** Заголовок — САМ предмет страницы: тип во множественном числе. */
  title: ReactNode;
  /** Ручки сужения: отборы, затем поиск (порядок — решение владельца: отбор
   *  меняет НАБОР строк, среди которых потом ищут). */
  narrowing?: ReactNode;
  /** Ручки действия: «Столбцы», затем «Создать» — последней в ряду. */
  actions?: ReactNode;
  children: ReactNode;
}

export function IamListShell({ title, narrowing, actions, children }: Props) {
  return (
    <div
      className="kc-surface"
      style={{
        padding: PAGE_PADDING,
        flex: 1,
        minHeight: 0,
        height: "100%",
        overflow: "hidden",
        display: "flex",
        flexDirection: "column",
      }}
    >
      <div style={{ flexShrink: 0 }}>
        <PageHead
          title={title}
          right={
            // Класс задаёт ОДНУ высоту и один радиус всем ручкам ряда (32 и 8) —
            // тот же, что у generic-списка. Без него каждая ручка приносила свою:
            // поле поиска от antd, отбор-селект, переключатель и первичная кнопка
            // давали четыре разные высоты в одном ряду.
            narrowing !== undefined || actions !== undefined ? (
              <div
                className="kc-list-tools"
                style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", justifyContent: "flex-end" }}
              >
                <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>{narrowing}</div>
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>{actions}</div>
              </div>
            ) : undefined
          }
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
