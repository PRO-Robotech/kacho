import { artifactTypeLabel, artifactTypeLabels } from "./ArtifactTypeTag";

// Маппинг типа артефакта в человекочитаемую метку (колонка «Тип» + facet).
describe("artifactTypeLabel", () => {
  it("docker-образ → «Docker-образ»", () => {
    expect(artifactTypeLabel("ARTIFACT_TYPE_CONTAINER_IMAGE")).toBe("Docker-образ");
  });
  it("helm-чарт → «Helm-чарт»", () => {
    expect(artifactTypeLabel("ARTIFACT_TYPE_HELM_CHART")).toBe("Helm-чарт");
  });
  it("иной артефакт → «Иной»", () => {
    expect(artifactTypeLabel("ARTIFACT_TYPE_OTHER")).toBe("Иной");
  });
  it("UNSPECIFIED / отсутствие / мусор → «—»", () => {
    expect(artifactTypeLabel("ARTIFACT_TYPE_UNSPECIFIED")).toBe("—");
    expect(artifactTypeLabel(undefined)).toBe("—");
    expect(artifactTypeLabel("")).toBe("—");
    expect(artifactTypeLabel(42)).toBe("—");
  });
});

// Набор типов (смешанный репозиторий): scalar/array → метки распознанных типов.
describe("artifactTypeLabels", () => {
  it("одиночное значение (scalar) → одна метка", () => {
    expect(artifactTypeLabels("ARTIFACT_TYPE_CONTAINER_IMAGE")).toEqual(["Docker-образ"]);
  });
  it("смешанный массив (docker + helm) → обе метки по порядку", () => {
    expect(artifactTypeLabels(["ARTIFACT_TYPE_CONTAINER_IMAGE", "ARTIFACT_TYPE_HELM_CHART"])).toEqual([
      "Docker-образ",
      "Helm-чарт",
    ]);
  });
  it("дубли и нераспознанные значения отбрасываются", () => {
    expect(
      artifactTypeLabels([
        "ARTIFACT_TYPE_HELM_CHART",
        "ARTIFACT_TYPE_HELM_CHART",
        "ARTIFACT_TYPE_UNSPECIFIED",
        "garbage",
      ]),
    ).toEqual(["Helm-чарт"]);
  });
  it("пусто / undefined / только UNSPECIFIED → []", () => {
    expect(artifactTypeLabels([])).toEqual([]);
    expect(artifactTypeLabels(undefined)).toEqual([]);
    expect(artifactTypeLabels("ARTIFACT_TYPE_UNSPECIFIED")).toEqual([]);
  });
});
