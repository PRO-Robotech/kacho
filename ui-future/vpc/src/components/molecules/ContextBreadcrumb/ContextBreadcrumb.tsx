// ContextBreadcrumb — KAC-246 Фаза 2A: breadcrumb-контекст в top-bar в виде
// пилюль-сегментов `Account › Project[ › Resource]`.
//
// Account-сегмент — пилюля-кнопка, открывает account-выбор. Project-сегмент —
// пилюля-кнопка с ChevronRight-индикатором, открывает project-switcher (ленивая
// загрузка projects per-account через iamApi). Resource-сегмент (из page-
// breadcrumb slot) — просто текст справа от Project. Заменил прежний
// ContextCascader (KAC-246 Фаза 2A: визуальная переупаковка выбора контекста).
//
// Логика выбора контекста переиспользует contextApi (context-store) + iamApi —
// никакого нового API не вводится.

import { forwardRef, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Dropdown, theme, type MenuProps } from "antd";
import { ChevronRight } from "lucide-react";
import { iamApi } from "@shared/api/iam";
import { contextApi, useContext } from "@shared/lib/context-store";
import { HeaderBreadcrumbSlot } from "@shared/components/molecules/PageHeaderSlot";

interface AccountOpt {
  id: string;
  name: string;
}
interface ProjectOpt {
  id: string;
  name: string;
}

export function ContextBreadcrumb() {
  const account = useContext((s) => s.account);
  const project = useContext((s) => s.project);
  const navigate = useNavigate();
  const { token } = theme.useToken();

  const [accounts, setAccounts] = useState<AccountOpt[]>([]);
  const [projects, setProjects] = useState<ProjectOpt[]>([]);
  const [projectsLoadedFor, setProjectsLoadedFor] = useState<string | null>(null);

  // Список accounts — один раз.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const r = await iamApi.listAccounts({ pageSize: "1000" });
        if (cancelled) return;
        setAccounts((r.accounts ?? []).map((a) => ({ id: a.id, name: a.name || a.id })));
      } catch {
        /* ignore */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Projects текущего account — лениво, при открытии project-dropdown.
  const loadProjects = (accountId: string) => {
    if (projectsLoadedFor === accountId) return;
    void (async () => {
      try {
        const r = await iamApi.listProjects({ account_id: accountId, pageSize: "1000" });
        setProjects((r.projects ?? []).map((p) => ({ id: p.id, name: p.name || p.id })));
        setProjectsLoadedFor(accountId);
      } catch {
        setProjects([]);
        setProjectsLoadedFor(accountId);
      }
    })();
  };

  const accountMenu: MenuProps = useMemo(
    () => ({
      items: accounts.length
        ? accounts.map((a) => ({ key: a.id, label: a.name }))
        : [{ key: "__empty", label: "Аккаунтов нет", disabled: true }],
      selectedKeys: account ? [account.id] : [],
      onClick: ({ key }) => {
        const a = accounts.find((x) => x.id === key);
        if (a) contextApi.setAccount({ id: a.id, name: a.name });
      },
    }),
    [accounts, account],
  );

  const projectMenu: MenuProps = useMemo(
    () => ({
      items: projects.length
        ? projects.map((p) => ({ key: p.id, label: p.name }))
        : [{ key: "__empty", label: account ? "Проектов нет" : "Выберите аккаунт", disabled: true }],
      selectedKeys: project ? [project.id] : [],
      onClick: ({ key }) => {
        const p = projects.find((x) => x.id === key);
        if (p && account) {
          contextApi.setProject({ id: p.id, name: p.name, accountId: account.id });
          navigate(`/projects/${p.id}/dashboard`);
        }
      },
    }),
    [projects, project, account, navigate],
  );

  const sep = (
    <ChevronRight size={14} strokeWidth={2} style={{ color: token.colorTextTertiary, flexShrink: 0 }} aria-hidden />
  );

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 4,
        minWidth: 0,
        fontSize: 13,
      }}
    >
      <Dropdown menu={accountMenu} trigger={["click"]} placement="bottomLeft">
        <PillButton token={token} active={!!account} placeholder="Выберите аккаунт" chevron>
          {account?.name || account?.id}
        </PillButton>
      </Dropdown>

      {sep}

      <Dropdown
        menu={projectMenu}
        trigger={["click"]}
        placement="bottomLeft"
        onOpenChange={(open) => {
          if (open && account) loadProjects(account.id);
        }}
        disabled={!account}
      >
        <PillButton token={token} active={!!project} placeholder="Проект" chevron>
          {project?.name || project?.id}
        </PillButton>
      </Dropdown>

      {/* Resource-сегмент из page-level breadcrumb slot (если задан страницей). */}
      <ResourceSegment token={token} sep={sep} />
    </div>
  );
}

/** Resource-сегмент: показывает page-breadcrumb (slot) после `›`, если он есть. */
function ResourceSegment({ token, sep }: { token: ReturnType<typeof theme.useToken>["token"]; sep: React.ReactNode }) {
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 4,
        minWidth: 0,
        color: token.colorTextSecondary,
        overflow: "hidden",
        whiteSpace: "nowrap",
        textOverflow: "ellipsis",
      }}
    >
      <BreadcrumbSlotWithSeparator sep={sep} />
    </span>
  );
}

// Рендерит `›` + slot только если slot непустой. Slot всегда монтируется (через
// HeaderBreadcrumbSlot), а сепаратор показываем через :empty CSS-трюк? Проще —
// всегда рисуем сепаратор, но он маленький; если страница не задала breadcrumb,
// HeaderBreadcrumbSlot отрендерит null → пустой span. Чтобы не было «висящего»
// сепаратора, оборачиваем slot и сепаратор в group с `:has`-like поведением через
// контейнер, который скрывается, если пуст. Здесь — простой подход: сепаратор
// внутри slot-обёртки, скрытой когда контента нет (через CSS class).
function BreadcrumbSlotWithSeparator({ sep }: { sep: React.ReactNode }) {
  return (
    <span
      className="kc-breadcrumb-resource"
      style={{ display: "inline-flex", alignItems: "center", gap: 4, minWidth: 0 }}
    >
      <span className="kc-breadcrumb-sep" style={{ display: "inline-flex" }}>
        {sep}
      </span>
      <HeaderBreadcrumbSlot />
    </span>
  );
}

// PillButton — forwardRef + проброс props ОБЯЗАТЕЛЬНЫ: AntD Dropdown инжектит
// в свой триггер onClick/ref/aria-* через cloneElement; без forwardRef и {...rest}
// onClick проглатывается и дропдаун не открывается (KAC-246 фикс).
type PillButtonProps = React.ButtonHTMLAttributes<HTMLButtonElement> & {
  token: ReturnType<typeof theme.useToken>["token"];
  active: boolean;
  placeholder: string;
  chevron?: boolean;
};

const PillButton = forwardRef<HTMLButtonElement, PillButtonProps>(function PillButton(
  { children, token, active, placeholder, chevron, onMouseEnter, onMouseLeave, ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type="button"
      {...rest}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 4,
        maxWidth: 200,
        height: 28,
        paddingInline: 10,
        borderRadius: 8,
        border: "none",
        background: "transparent",
        color: active ? token.colorText : token.colorTextTertiary,
        fontSize: 13,
        fontWeight: active ? 500 : 400,
        cursor: "pointer",
        transition: "background-color 150ms ease, color 150ms ease",
        overflow: "hidden",
        whiteSpace: "nowrap",
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.background = "var(--kc-hover-fill)";
        e.currentTarget.style.color = token.colorText;
        onMouseEnter?.(e);
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.background = "transparent";
        e.currentTarget.style.color = active ? token.colorText : token.colorTextTertiary;
        onMouseLeave?.(e);
      }}
    >
      <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {active ? children : placeholder}
      </span>
      {chevron && (
        <ChevronRight
          size={13}
          strokeWidth={2}
          style={{ color: token.colorTextTertiary, transform: "rotate(90deg)", flexShrink: 0 }}
          aria-hidden
        />
      )}
    </button>
  );
});
