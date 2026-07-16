export const DASHBOARD_NAVIGATION = [
  {
    key: "iam",
    segment: "iam",
    icon: "key",
    label: "Identity and Access Management",
    landingPath: "/iam/accounts",
    items: [
      { key: "iam-accounts", icon: "layers", label: "Аккаунты", path: "/iam/accounts" },
      { key: "iam-projects", icon: "folder", label: "Проекты", path: "/iam/projects" },
      { key: "iam-users", icon: "users", label: "Пользователи", path: "/iam/users" },
      { key: "iam-access", icon: "shield", label: "Связки прав", path: "/iam/access-bindings" },
      { key: "iam-access-page", icon: "users", label: "Права доступа", path: "/iam/access" },
    ],
  },
];

export default DASHBOARD_NAVIGATION;
