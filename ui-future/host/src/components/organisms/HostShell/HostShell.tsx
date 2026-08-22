import { useCallback, useState } from "react";
import type { Dispatch, FC, ReactNode, SetStateAction } from "react";
import { Layout, theme } from "antd";
import { useLocation, useNavigate } from "react-router";
import { HeaderActions, HostBreadcrumb } from "../../molecules";
import { loadHostContext, type HostContext } from "../../../utils";
import { HostRail } from "../HostRail";
import { ModuleNav } from "../ModuleNav";

// Футера у консоли нет: эталон отдаёт рабочей области всю высоту, а строка с
// годом занимала полосу под каждым экраном, не сообщая ничего.
const { Header, Sider, Content } = Layout;

// Ширина рейла — не вкус, а следствие геометрии пункта: пункт 44 плюс поля по
// 9 с каждой стороны (см. RailButton и .rail-nav). То же число объявлено в
// геометрии темы (`SHAPE.railWidth`, shared/src/lib/theme.ts), но наружу оттуда
// не выходит: `SHAPE` не экспортирован, а Sider принимает ширину числом пропса,
// а не токеном. Экспортируют — брать оттуда.
//
// Высота шапки здесь НЕ объявляется намеренно: её задаёт токен темы
// `Layout.headerHeight` (54), и второе объявление рядом разошлось бы с ним молча
// — ровно так шапка и жила на 48 при теме, обещавшей 54.
const SIDEBAR_WIDTH = 62;

export const HostShell: FC<{
  dark: boolean;
  setDark: Dispatch<SetStateAction<boolean>>;
  showReachability: boolean;
  children: ReactNode | ((context: HostContext) => ReactNode);
}> = ({ dark, setDark, showReachability, children }) => {
  const { token } = theme.useToken();
  const navigate = useNavigate();
  useLocation();
  const [, setContextRevision] = useState(0);
  const hostContext = loadHostContext();
  const refreshHostContext: Dispatch<SetStateAction<HostContext>> = useCallback(() => {
    setContextRevision((revision) => revision + 1);
  }, []);

  return (
    <Layout className="app-shell" hasSider>
      <Sider
        width={SIDEBAR_WIDTH}
        className="app-rail"
        style={{
          borderRight: `1px solid ${token.colorBorder}`,
          // Рейл — своя плоскость, на полтона темнее страницы. Роли рейла у AntD
          // нет, поэтому берётся общий токен, а не цвет страницы.
          background: "var(--kc-rail)",
        }}
      >
        <HostRail
          context={hostContext}
          currentPath={location.pathname}
          showReachability={showReachability}
          navigate={navigate}
        />
      </Sider>

      <Layout className="app-main" style={{ background: token.colorBgLayout }}>
        <Header
          className="app-header"
          style={{
            borderBottom: `1px solid ${token.colorBorder}`,
            background: token.colorBgLayout,
          }}
        >
          <HostBreadcrumb context={hostContext} onChange={refreshHostContext} navigate={navigate} />
          <HeaderActions dark={dark} setDark={setDark} />
        </Header>

        {/* Во всю высоту идёт ТОЛЬКО рейл модулей; второй уровень начинается под
            шапкой — как в эталоне, где полосу хлебных крошек перекрывает лишь
            рейл, а колонка разделов стоит уже под ней. Иначе шапка обрывается о
            вторую тёмную полосу и перестаёт читаться единой строкой. */}
        <div style={{ display: "flex", flex: 1, minHeight: 0, minWidth: 0 }}>
          <ModuleNav context={hostContext} currentPath={location.pathname} navigate={navigate} />
          <Content className="app-content">{typeof children === "function" ? children(hostContext) : children}</Content>
        </div>
      </Layout>
    </Layout>
  );
};
