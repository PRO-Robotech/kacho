import { useCallback, useState } from "react";
import type { Dispatch, FC, ReactNode, SetStateAction } from "react";
import { Layout, theme } from "antd";
import { useLocation, useNavigate } from "react-router-dom";
import { HeaderActions, HostBreadcrumb } from "../../molecules";
import { loadHostContext, type HostContext } from "../../../utils";
import { HostRail } from "../HostRail";

const { Header, Sider, Content, Footer } = Layout;

const SIDEBAR_WIDTH = 56;
const HEADER_HEIGHT = 48;

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
          borderRight: `1px solid ${token.colorBorderSecondary}`,
          background: token.colorBgLayout,
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
            height: HEADER_HEIGHT,
            lineHeight: `${HEADER_HEIGHT}px`,
            borderBottom: `1px solid ${token.colorBorderSecondary}`,
            background: token.colorBgLayout,
          }}
        >
          <HostBreadcrumb context={hostContext} onChange={refreshHostContext} navigate={navigate} />
          <HeaderActions dark={dark} setDark={setDark} />
        </Header>

        <Content className="app-content">{typeof children === "function" ? children(hostContext) : children}</Content>

        {/* Глобальный футер — вне Content, всегда виден внизу (как kacho-ui). */}
        <Footer className="app-footer">PRO Robotech © {new Date().getFullYear()}</Footer>
      </Layout>
    </Layout>
  );
};
