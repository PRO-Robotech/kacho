// Per-resource API helpers registry-домена. Обёртки над generic api.* (client.ts),
// который выполняет case-конверсию (snake→camel на отправку, camel→snake на
// приём) и заворачивает мутации в Operation-envelope. URL'ы — verbatim из proto
// google.api.http annotations (kacho.cloud.registry.v1).
//
// Generic ResourceListPage/Shell/Create ходят напрямую через api.* по
// spec.apiPath для project-scoped Registry; эти helpers дают доменные вызовы для
// PATH-scoped дочерних ресурсов (Repository/Tag адресуются ПОД реестром —
// ListRepositories(registryId) / ListTags(registryId, repository)), которых нет
// в generic-конвейере (он умеет только project_id-query фильтр).

import { api } from "./client";
import type { Operation, RegistryList, RepositoryList, TagList } from "./types";

const REGISTRIES = "/registry/v1/registries";

// repositoriesPath / tagsPath — path-scoped вложенные ресурсы: репозитории
// адресуются под реестром, теги — под репозиторием реестра.
export function repositoriesPath(registryId: string): string {
  return `${REGISTRIES}/${encodeURIComponent(registryId)}/repositories`;
}

export function tagsPath(registryId: string, repository: string): string {
  return `${repositoriesPath(registryId)}/${encodeURIComponent(repository)}/tags`;
}

export const registriesApi = {
  list: (q?: Record<string, string>) => api.list<RegistryList>(REGISTRIES, q),
  get: (id: string) => api.get<Record<string, unknown>>(`${REGISTRIES}/${id}`),
  create: (body: unknown): Promise<{ operation: Operation }> => api.create(REGISTRIES, body),
  update: (id: string, body: unknown): Promise<{ operation: Operation }> => api.update(`${REGISTRIES}/${id}`, body),
  delete: (id: string): Promise<{ operation: Operation }> => api.delete(`${REGISTRIES}/${id}`),

  // Read-only дочерние ресурсы — path-scoped под реестром/репозиторием.
  listRepositories: (registryId: string, q?: Record<string, string>) =>
    api.list<RepositoryList>(repositoriesPath(registryId), q),
  listTags: (registryId: string, repository: string, q?: Record<string, string>) =>
    api.list<TagList>(tagsPath(registryId, repository), q),

  // DeleteTag — единственная мутация тега (async Operation).
  deleteTag: (registryId: string, repository: string, tag: string): Promise<{ operation: Operation }> =>
    api.delete(`${tagsPath(registryId, repository)}/${encodeURIComponent(tag)}`),
};
