import React, { type ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { jest } from "@jest/globals";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { contextApi, type AccountRef, type ProjectRef } from "@shared/lib/context-store";
import type { ResourceSpec } from "@shared/lib/resource-registry";
import type { IamPage as IamPageExport } from "./IamPage";

type IamPageComponent = typeof IamPageExport;

let IamPage: IamPageComponent;
let hostContext:
  | {
      account: AccountRef | null;
      project: ProjectRef | null;
    }
  | undefined;

const authValue = {
  user: null,
  session: null,
  loading: false,
  accessToken: null,
  mfaFreshUntil: 0,
  whoami: null,
  login: jest.fn(),
  logout: jest.fn(),
  refresh: jest.fn(),
  refreshWhoAmI: jest.fn(),
  setAccessToken: jest.fn(),
  markMfaFresh: jest.fn(),
  hasPermission: jest.fn(() => false),
  setStepUpHandler: jest.fn(),
};

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  AuthProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  useAuth: () => authValue,
}));

jest.unstable_mockModule("@/components/molecules/auth/StepUpModal", () => ({
  StepUpModal: () => null,
}));

jest.unstable_mockModule("@shared/components/molecules/PageHeaderSlot", () => ({
  HeaderRightSlot: () => null,
  PageHeaderSlotProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  useBreadcrumb: jest.fn(),
  useHeaderRight: jest.fn(),
}));

jest.unstable_mockModule("@shared/components/molecules/OperationBanner", () => ({
  OperationBanner: () => null,
}));

jest.unstable_mockModule("@shared/components/organisms/GlobalResourceFormModal", () => ({
  GlobalResourceFormModal: () => null,
}));

jest.unstable_mockModule("@shared/lib/theme-context", () => ({
  ThemeProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  useThemeMode: () => ({
    mode: "light",
    setMode: jest.fn(),
    toggle: jest.fn(),
  }),
}));

jest.unstable_mockModule("@shared/lib/resource-registry", () => {
  const spec = (id: string, plural: string) => ({
    id,
    plural,
    route: id,
    apiPath: `/iam/v1/${id}`,
    payloadKey: id,
    singular: plural.slice(0, -1),
    serviceTitle: "IAM",
    scope: id === "accounts" ? "global" : "account",
    ops: { create: true, update: true, delete: true },
    columns: [],
    template: () => ({}),
  });

  const REGISTRY: Record<string, ReturnType<typeof spec>> = {
    accounts: spec("accounts", "Accounts"),
    projects: spec("projects", "Projects"),
    "service-accounts": spec("service-accounts", "Service Accounts"),
  };

  // The full named-export surface of `@shared/lib/resource-registry` must be
  // present on the mock: shared components in IamPage's (unmocked) transitive
  // graph — RefSelect, ResourceRefChips, resourceInstanceFetchers — statically
  // `import { getResource, getByPath, ... }` from this module, and the ESM VM
  // linker fails the whole graph if any named binding is missing on the mock.
  return {
    REGISTRY,
    getResource: (id: string) => REGISTRY[id],
    getByPath: (obj: unknown, path: string): unknown =>
      path.split(".").reduce<unknown>((acc, key) => (acc == null ? undefined : (acc as Record<string, unknown>)[key]), obj),
    resourceServicePrefix: () => "iam" as const,
    resourceProjectPath: () => null,
    applyFieldDefaults: (obj: Record<string, unknown>) => obj,
    sanitizeSgRule: (r: Record<string, unknown>) => r,
    sanitizeInstanceCreate: (obj: Record<string, unknown>) => obj,
    fmtBytesGiB: () => "",
    gibToBytes: () => undefined,
  };
});

jest.unstable_mockModule("@/components/organisms/ResourceCreatePage", () => ({
  ResourceCreatePage: ({ spec }: { spec: ResourceSpec }) => <div>{spec.plural} create</div>,
}));

jest.unstable_mockModule("@/components/organisms/ResourceListPage", () => ({
  ResourceListPage: ({ spec }: { spec: ResourceSpec }) => <div>{spec.plural}</div>,
}));

jest.unstable_mockModule("@shared/components/organisms/ResourceShell", () => ({
  ResourceShell: ({ spec }: { spec: ResourceSpec }) => (
    <div>
      <div>{spec.id === "accounts" ? "Account One" : spec.plural}</div>
      <div>JSON</div>
    </div>
  ),
}));

jest.unstable_mockModule("@/components/organisms/iam/IamScopedListShell", () => ({
  IamScopedListShell: ({ spec }: { spec: ResourceSpec }) => {
    if (!hostContext?.account) {
      return <div>{`Выберите Account вверху секции, чтобы увидеть ${spec.plural}.`}</div>;
    }

    return <div>{spec.plural}</div>;
  },
}));

jest.unstable_mockModule("@/pages/iam/AccessBindingsPage", () => ({
  AccessBindingCreatePage: () => <div>Access binding create</div>,
  AccessBindingsPage: () => {
    React.useEffect(() => {
      if (hostContext?.account) {
        void fetch(`/iam/v1/accounts/${hostContext.account.id}/accessBindings`, {});
      }
    }, []);

    return <div data-testid="access-bindings-account-select">Access bindings</div>;
  },
}));

jest.unstable_mockModule("@/pages/iam/AccessPage", () => ({
  AccessGrantPage: () => <div>Access grant</div>,
  AccessPage: () => <div>Access</div>,
}));

jest.unstable_mockModule("@/pages/iam/GroupsPage", () => ({
  GroupCreatePage: () => <div>Group create</div>,
  GroupEditPage: () => <div>Group edit</div>,
  GroupsPage: () => <div>Groups</div>,
}));

jest.unstable_mockModule("@/pages/iam/RolesPage", () => ({
  RoleCreatePage: () => <div>Role create</div>,
  RoleEditPage: () => <div>Role edit</div>,
  RolesPage: () => <div>Roles</div>,
}));

jest.unstable_mockModule("@/pages/iam/UsersPage", () => ({
  InviteUserPage: () => <div>Invite user</div>,
  UsersPage: () => <div>Users</div>,
}));

// IamPage's route table also mounts these dedicated list shells / pages and the
// toaster; the `@/registerExtensions` module runs `registerDetailExtension` /
// `registerInlineForm` at import time. They are unrelated to IamPage's routing
// behaviour under test and drag the full shared graph (plus eval-time
// side-effects) into the ESM VM, so they are stubbed like the other children.
jest.unstable_mockModule("@/components/molecules/Toaster", () => ({
  Toaster: () => null,
}));

jest.unstable_mockModule("@/components/organisms/iam/RolesListShell", () => ({
  RolesListShell: () => <div>Roles</div>,
}));

jest.unstable_mockModule("@/components/organisms/iam/IamUsersListShell", () => ({
  IamUsersListShell: () => <div>Users</div>,
}));

jest.unstable_mockModule("@/pages/iam/IamOperationsPage", () => ({
  IamOperationsPage: () => <div>Operations</div>,
}));

jest.unstable_mockModule("@/registerExtensions", () => ({}));

function renderIam(path: string, context?: Parameters<IamPageComponent>[0]["context"]) {
  hostContext = context;
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/iam/*" element={<IamPage context={context} />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("IamPage", () => {
  beforeAll(async () => {
    ({ IamPage } = await import("./IamPage"));
  });

  beforeEach(() => {
    hostContext = undefined;
    window.localStorage.clear();
    contextApi.setAccount(null);
    contextApi.setProject(null);
    const fetchMock = jest.fn<typeof fetch>().mockResolvedValue({
      ok: true,
      text: () => Promise.resolve("{}"),
      statusText: "OK",
    } as Response);
    globalThis.fetch = fetchMock;
  });

  afterEach(() => {
    jest.clearAllMocks();
  });

  it("redirects /iam to the accounts list", async () => {
    renderIam("/iam");

    expect(await screen.findByText("Accounts")).toBeInTheDocument();
    expect(screen.queryByTestId("iam-account-selector")).not.toBeInTheDocument();
  });

  it("shows account-scoped empty state when no account is selected", async () => {
    renderIam("/iam/projects");

    expect(await screen.findByText("Выберите Account вверху секции, чтобы увидеть Projects.")).toBeInTheDocument();
  });

  it("hydrates account-scoped pages from host context", async () => {
    renderIam("/iam/projects", {
      account: { id: "acc-1", name: "Account One" },
      project: null,
    });

    expect(await screen.findByText("Projects")).toBeInTheDocument();
    expect(screen.queryByText("Выберите Account вверху секции, чтобы увидеть Projects.")).not.toBeInTheDocument();
  });

  it("uses the account-scoped access bindings endpoint for the host account", async () => {
    renderIam("/iam/access-bindings", {
      account: { id: "acc-1", name: "Account One" },
      project: null,
    });

    expect(await screen.findByTestId("access-bindings-account-select")).toBeInTheDocument();
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining("/iam/v1/accounts/acc-1/accessBindings"),
      expect.anything(),
    );
    expect(global.fetch).not.toHaveBeenCalledWith(
      expect.stringContaining("/iam/v1/accessBindings:listByResource"),
      expect.anything(),
    );
  });

  it("keeps account detail tab urls inside the detail shell", async () => {
    renderIam("/iam/accounts/acc-1/json");

    expect(await screen.findByText("Account One")).toBeInTheDocument();
    expect(await screen.findByText("JSON")).toBeInTheDocument();
    expect(screen.queryByText("Accounts")).not.toBeInTheDocument();
  });
});
