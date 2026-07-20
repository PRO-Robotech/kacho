import { DASHBOARD_NAVIGATION } from "./navigation";

// DASHBOARD_NAVIGATION — агрегатный fallback-реестр, которым HostRail рендерит rail,
// когда remote-навигация не догрузилась. Обязан зеркалить ресурсную модель редизайна
// (compute.MachineType, storage.Image), иначе fallback-rail отстаёт от remote'ов.
describe("dashboard aggregate navigation", () => {
  const sectionBy = (segment: string) => {
    const section = DASHBOARD_NAVIGATION.find((s) => s.segment === segment);
    if (!section) throw new Error(`no ${segment} section`);
    return section;
  };

  it("exposes the compute MachineType catalog item", () => {
    const paths = sectionBy("compute").items.map((i) => i.path);
    expect(paths).toContain("compute/instances");
    expect(paths).toContain("compute/machine-types");
  });

  it("exposes the storage Image item alongside volumes/snapshots/disk-types", () => {
    const paths = sectionBy("storage").items.map((i) => i.path);
    expect(paths).toEqual(["storage/volumes", "storage/snapshots", "storage/images", "storage/disk-types"]);
  });
});
