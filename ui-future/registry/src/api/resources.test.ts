import { repositoriesPath, tagsPath } from "./resources";

// Repository/Tag — path-scoped вложенные ресурсы (ListRepositories(registryId) /
// ListTags(registryId, repository)); строим REST-путь по proto-форме, а не
// project_id-query-фильтром. Сегменты URL-энкодятся (репозиторий может нести «/»).
describe("registry nested path builders", () => {
  it("repositoriesPath — реестр-scoped путь репозиториев", () => {
    expect(repositoriesPath("reg-1")).toBe("/registry/v1/registries/reg-1/repositories");
  });

  it("tagsPath — путь тегов под репозиторием реестра", () => {
    expect(tagsPath("reg-1", "app")).toBe("/registry/v1/registries/reg-1/repositories/app/tags");
  });

  it("сегменты url-энкодятся (репозиторий с «/» в имени)", () => {
    expect(tagsPath("reg-1", "team/app")).toBe("/registry/v1/registries/reg-1/repositories/team%2Fapp/tags");
  });
});
