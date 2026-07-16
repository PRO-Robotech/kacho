// ServiceSidebar — KAC-246 «Layered Calm» Фаза 2A: расширенный сгруппированный
// навигатор премиум-дарк (Linear/Vercel). Два режима в одном компоненте:
//
//   expanded (~224px)  бренд(full) · группы с caps-заголовками · иконка+подпись
//   collapsed (56px)   бренд(mark) · только иконки + tooltip-подписи
//
// Группы выводятся из service-modules через buildSidebarGroups (Обзор / активный
// модуль или Сервисы / Система). Маршруты `leaf.to(projectId)` и requiresProject-
// гейтинг сохранены 1-в-1 с прежним icon-rail.
//
// Collapse-состояние приходит из Layout (persist в localStorage там), кнопка
// сворачивания — внизу сайдбара.

import { useMemo, type ReactNode } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { Avatar, Dropdown, Spin, Tooltip, theme } from "antd";
import { LoginOutlined, LogoutOutlined, UserOutlined } from "@ant-design/icons";
import { KachoLogo } from "@/components/atoms/brand/KachoLogo";
import { useContext } from "@shared/lib/context-store";
import { useAuth } from "@shared/contexts/AuthContext";
import { COMMON_BOTTOM, type NavLeaf } from "@shared/lib/service-modules";
import { activeLeafKey, buildSidebarGroups } from "@shared/lib/sidebar-groups";

const RAIL_WIDTH = 56;
const EXPANDED_WIDTH = 232;

export function ServiceSidebar() {
  // KAC-246: статичный узкий icon-rail; подписи — тултипом при наведении на
  // пункт (без выплывающей панели).
  const collapsed = true;
  const navigate = useNavigate();
  const location = useLocation();
  const projectId = useContext((s) => s.project)?.id ?? null;
  const accountId = useContext((s) => s.account)?.id ?? null;
  const { user, loading: authLoading } = useAuth();
  const { token } = theme.useToken();

  // IAM/system-entry: «Администрирование» показываем только авторизованному
  // user'у — server-side authz сам решит 200/403 при CRUD (см. прежний коммент).
  const bottomItems = useMemo<NavLeaf[]>(() => {
    return COMMON_BOTTOM.filter((leaf) => {
      if (leaf.key === "system") {
        if (authLoading) return false;
        return !!user;
      }
      return true;
    });
  }, [authLoading, user]);

  const groups = useMemo(
    () => buildSidebarGroups(location.pathname, projectId, accountId, bottomItems),
    [location.pathname, projectId, accountId, bottomItems],
  );
  const activeKey = useMemo(() => activeLeafKey(groups, location.pathname), [groups, location.pathname]);

  // KAC-246: «Система» (шестерёнка-администрирование) — пинуется в нижний блок,
  // не в общий скроллящийся список.
  const navGroups = groups.filter((g) => g.key !== "system");
  const systemGroup = groups.find((g) => g.key === "system");

  const renderLeaf = (leaf: NavLeaf) => {
    const disabled = !!leaf.requiresProject && !projectId;
    const active = activeKey === leaf.key;
    return (
      <SidebarItem
        key={leaf.key}
        icon={leaf.icon}
        label={leaf.label}
        disabledLabel="Выберите проект"
        active={active}
        disabled={disabled}
        collapsed={collapsed}
        onClick={() => !disabled && navigate(leaf.to(projectId))}
        token={token}
      />
    );
  };

  return (
    <nav
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        paddingTop: 6,
        paddingBottom: 6,
      }}
      aria-label="Навигация сервиса"
    >
      {/* Бренд */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          height: 56,
          paddingInline: collapsed ? 0 : 14,
          justifyContent: collapsed ? "center" : "flex-start",
          marginBottom: 4,
        }}
      >
        <button
          type="button"
          onClick={() => navigate("/")}
          aria-label="Kachō Console"
          style={{
            display: "inline-flex",
            alignItems: "center",
            background: "transparent",
            border: "none",
            cursor: "pointer",
            padding: 0,
            height: 48,
          }}
        >
          {collapsed ? (
            <KachoLogo variant="mark" size={44} />
          ) : (
            <KachoLogo variant="full" size={44} wordmarkColor={token.colorText} />
          )}
        </button>
      </div>

      {/* Группы — скроллятся, нижний блок (user/collapse) прижат вниз */}
      <div style={{ flex: 1, overflowY: "auto", overflowX: "hidden", paddingInline: 8 }}>
        {navGroups.map((g, i) => (
          <div key={g.key} style={{ marginTop: i === 0 ? 0 : 10 }}>
            {!collapsed && g.title && (
              <div
                style={{
                  padding: "0 8px 4px",
                  fontSize: 11,
                  fontWeight: 600,
                  letterSpacing: "0.04em",
                  textTransform: "uppercase",
                  color: token.colorTextTertiary,
                  userSelect: "none",
                }}
              >
                {g.title}
              </div>
            )}
            {collapsed && i > 0 && <SidebarDivider token={token} />}
            <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>{g.leaves.map(renderLeaf)}</div>
          </div>
        ))}
      </div>

      {/* Нижний блок: «Система» (шестерёнка) + user-menu — прижаты к низу. */}
      <div
        style={{
          paddingInline: 8,
          paddingTop: 6,
          marginTop: 4,
          borderTop: `1px solid ${token.colorBorderSecondary}`,
          display: "flex",
          flexDirection: "column",
          gap: 4,
        }}
      >
        {systemGroup && systemGroup.leaves.length > 0 && (
          <div style={{ display: "flex", flexDirection: "column", gap: 2, marginBottom: 2 }}>
            {systemGroup.leaves.map(renderLeaf)}
          </div>
        )}
        <SidebarUserButton token={token} collapsed={collapsed} />
      </div>
    </nav>
  );
}

ServiceSidebar.RAIL_WIDTH = RAIL_WIDTH;
ServiceSidebar.EXPANDED_WIDTH = EXPANDED_WIDTH;

// ── SidebarItem ─────────────────────────────────────────────────────────────
// Один пункт нав. В expanded — иконка + label (h=34, r=8); в collapsed — только
// иконка по центру (w=40 h=36) + tooltip-подпись. Active: sidebar-active-bg +
// левый 2px accent-бар + accent-цвет. Hover: kc-hover-fill. transition 150ms.
function SidebarItem({
  icon,
  label,
  disabledLabel,
  active,
  disabled,
  collapsed,
  onClick,
  token,
}: {
  icon: ReactNode;
  label: string;
  disabledLabel: string;
  active: boolean;
  disabled: boolean;
  collapsed: boolean;
  onClick: () => void;
  token: ReturnType<typeof theme.useToken>["token"];
}) {
  const restColor = disabled ? (token.colorTextDisabled ?? token.colorTextTertiary) : token.colorTextSecondary;
  const tooltip = disabled ? disabledLabel : label;

  const btn = (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      aria-current={active ? "page" : undefined}
      disabled={disabled}
      style={{
        position: "relative",
        display: "flex",
        alignItems: "center",
        gap: collapsed ? 0 : 10,
        justifyContent: collapsed ? "center" : "flex-start",
        width: "100%",
        height: collapsed ? 36 : 34,
        flexShrink: 0,
        paddingInline: collapsed ? 0 : 10,
        borderRadius: 8,
        border: "none",
        background: active ? "var(--sidebar-active-bg)" : "transparent",
        color: active ? token.colorPrimary : restColor,
        cursor: disabled ? "not-allowed" : "pointer",
        fontSize: collapsed ? 18 : 13,
        fontWeight: active ? 600 : 500,
        opacity: disabled ? 0.55 : 1,
        transition: "background-color 150ms ease, color 150ms ease",
        overflow: "hidden",
        whiteSpace: "nowrap",
      }}
      onMouseEnter={(e) => {
        if (!disabled && !active) {
          e.currentTarget.style.background = "var(--kc-hover-fill)";
          e.currentTarget.style.color = token.colorText;
        }
      }}
      onMouseLeave={(e) => {
        if (!active) {
          e.currentTarget.style.background = "transparent";
          e.currentTarget.style.color = restColor;
        }
      }}
    >
      {active && (
        <span
          style={{
            position: "absolute",
            left: collapsed ? -8 : 0,
            top: 6,
            bottom: 6,
            width: 2,
            borderRadius: 2,
            background: token.colorPrimary,
          }}
        />
      )}
      <span style={{ display: "inline-flex", fontSize: 18, lineHeight: 0 }}>{icon}</span>
      {!collapsed && (
        <span
          style={{
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {label}
        </span>
      )}
    </button>
  );

  // Tooltip только в collapsed (в expanded подпись и так видна) либо для disabled.
  if (collapsed || disabled) {
    return (
      <Tooltip title={tooltip} placement="right" mouseEnterDelay={0.4}>
        {btn}
      </Tooltip>
    );
  }
  return btn;
}

// ── SidebarUserButton ───────────────────────────────────────────────────────
// KAC-198: user-menu внизу сайдбара. KAC-246: поддержка expanded (avatar + email)
// и collapsed (только avatar + tooltip).
function SidebarUserButton({
  token,
  collapsed,
}: {
  token: ReturnType<typeof theme.useToken>["token"];
  collapsed: boolean;
}) {
  const { user, loading, login, logout } = useAuth();
  const navigate = useNavigate();

  if (loading) {
    return (
      <div
        style={{
          height: 36,
          display: "inline-flex",
          alignItems: "center",
          justifyContent: collapsed ? "center" : "flex-start",
          paddingInline: collapsed ? 0 : 10,
        }}
      >
        <Spin size="small" />
      </div>
    );
  }

  if (!user) {
    const loginBtn = (
      <button
        type="button"
        // KAC-199: login() из useAuth → full-page redirect на Kratos login.
        onClick={() => login(window.location.pathname + window.location.search)}
        aria-label="Войти"
        style={{
          display: "flex",
          alignItems: "center",
          gap: collapsed ? 0 : 10,
          justifyContent: collapsed ? "center" : "flex-start",
          width: "100%",
          height: 36,
          paddingInline: collapsed ? 0 : 10,
          borderRadius: 8,
          border: "none",
          background: "transparent",
          color: token.colorTextSecondary,
          cursor: "pointer",
          fontSize: collapsed ? 18 : 13,
          transition: "background-color 150ms ease, color 150ms ease",
        }}
        onMouseEnter={(e) => {
          e.currentTarget.style.background = "var(--kc-hover-fill)";
          e.currentTarget.style.color = token.colorText;
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.background = "transparent";
          e.currentTarget.style.color = token.colorTextSecondary;
        }}
      >
        <span style={{ display: "inline-flex", fontSize: 18, lineHeight: 0 }}>
          <LoginOutlined />
        </span>
        {!collapsed && <span>Войти</span>}
      </button>
    );
    if (collapsed) {
      return (
        <Tooltip title="Войти" placement="right" mouseEnterDelay={0.4}>
          {loginBtn}
        </Tooltip>
      );
    }
    return loginBtn;
  }

  const display = user.display_name || user.email || user.id;
  const ini = initials(user.display_name || user.email);

  const items = [
    {
      key: "user-info",
      label: (
        <div style={{ padding: "4px 4px", minWidth: 200 }}>
          <div style={{ fontSize: 13, fontWeight: 600 }}>{display}</div>
          {user.email && user.email !== display && (
            <div style={{ fontSize: 11, color: token.colorTextTertiary }}>{user.email}</div>
          )}
        </div>
      ),
      disabled: true,
    },
    { type: "divider" as const },
    {
      key: "profile",
      icon: <UserOutlined />,
      label: "Профиль",
      onClick: () => navigate("/iam/users"),
    },
    {
      key: "logout",
      icon: <LogoutOutlined />,
      label: "Выйти",
      onClick: async () => {
        logout();
        navigate("/");
      },
    },
  ];

  const trigger = (
    <button
      type="button"
      aria-label={display}
      style={{
        display: "flex",
        alignItems: "center",
        gap: collapsed ? 0 : 10,
        justifyContent: collapsed ? "center" : "flex-start",
        width: "100%",
        height: 36,
        paddingInline: collapsed ? 0 : 8,
        borderRadius: 8,
        border: "none",
        background: "transparent",
        cursor: "pointer",
        transition: "background-color 150ms ease",
        overflow: "hidden",
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.background = "var(--kc-hover-fill)";
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.background = "transparent";
      }}
    >
      <Avatar
        size={26}
        style={{
          background: token.colorPrimary,
          color: "#fff",
          fontSize: 11,
          fontWeight: 600,
          flexShrink: 0,
        }}
      >
        {ini || <UserOutlined />}
      </Avatar>
      {!collapsed && (
        <span
          style={{
            fontSize: 13,
            color: token.colorText,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {display}
        </span>
      )}
    </button>
  );

  return (
    <Dropdown menu={{ items }} placement={collapsed ? "topRight" : "topLeft"} trigger={["click"]}>
      {collapsed ? (
        <Tooltip title={display} placement="right" mouseEnterDelay={0.4}>
          {trigger}
        </Tooltip>
      ) : (
        trigger
      )}
    </Dropdown>
  );
}

function initials(name?: string): string {
  if (!name) return "";
  const parts = name.trim().split(/\s+/);
  if (parts.length === 0) return "";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

function SidebarDivider({ token }: { token: ReturnType<typeof theme.useToken>["token"] }) {
  return (
    <div
      style={{
        width: 32,
        height: 1,
        background: token.colorBorderSecondary,
        margin: "6px auto",
        flexShrink: 0,
      }}
    />
  );
}
