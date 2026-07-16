import { apiGet, apiList } from "./api-client";

const IAM = {
  accounts: "/iam/v1/accounts",
  projects: "/iam/v1/projects",
} as const;

export interface AccountApi {
  id: string;
  name?: string;
}

export interface ProjectApi {
  id: string;
  name?: string;
  account_id?: string;
  accountId?: string;
}

export function listAccounts(query?: Record<string, string>) {
  return apiList<{ accounts?: AccountApi[] }>(IAM.accounts, query);
}

export function listProjects(query?: Record<string, string>) {
  return apiList<{ projects?: ProjectApi[] }>(IAM.projects, query);
}

export function getAccount(id: string) {
  return apiGet<AccountApi>(`${IAM.accounts}/${encodeURIComponent(id)}`);
}

export function getProject(id: string) {
  return apiGet<ProjectApi>(`${IAM.projects}/${encodeURIComponent(id)}`);
}
