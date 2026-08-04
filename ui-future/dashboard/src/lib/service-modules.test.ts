// Витрина сервисов на главной — сверка с поверхностью ствола.
//
// Ground truth: HTTP-аннотации proto ствола. Плитка сервиса делает два
// утверждения о продукте, и оба видит пользователь до всякого запроса:
//
//  1) `listPath` — адрес, по которому берётся счётчик. Адрес без производителя
//     на крае даёт вечный «—» в плитке, неотличимый от «ресурсов нет».
//  2) `description` — чем этот домен владеет. Блочное хранение (тома, образы,
//     снимки) уехало из compute в отдельный домен storage: маршрутов
//     /compute/v1/{disks,images,snapshots,diskTypes} в стволе нет ни одного.
//     Плитка, обещающая их под именем Compute, ведёт человека искать раздел,
//     которого в этом домене больше нет.
//
// Перечень маршрутов ниже — не весь ствол, а ровно те пути, которые витрина
// адресует, выписанные из proto поимённо. Проба на предпосылке: если модулей
// или путей вдруг ноль, это находка, а не «расхождений нет».

import { SERVICE_MODULES } from "./service-modules";

/** Пути ствола, которые адресует витрина (proto ствола, http-аннотации). */
const STEM_LIST_PATHS = [
  "/vpc/v1/networks",
  "/vpc/v1/subnets",
  "/vpc/v1/securityGroups",
  "/compute/v1/instances",
  "/storage/v1/volumes",
  "/storage/v1/snapshots",
  "/storage/v1/images",
  "/registry/v1/registries",
  "/nlb/v1/networkLoadBalancers",
  "/nlb/v1/listeners",
  "/nlb/v1/targetGroups",
  "/iam/v1/accounts",
  "/iam/v1/projects",
  "/iam/v1/roles",
];

/** Ресурсы, которых у compute больше нет — они принадлежат домену storage. */
const STORAGE_OWNED = [/диск/i, /образ/i, /снимк/i, /том/i];

describe("витрина сервисов против поверхности ствола", () => {
  it("витрина непуста и каждая плитка называет хотя бы один путь", () => {
    expect(SERVICE_MODULES.length).toBeGreaterThan(0);
    for (const m of SERVICE_MODULES) {
      expect(m.stats.length).toBeGreaterThan(0);
    }
  });

  it("каждый listPath принадлежит поверхности ствола", () => {
    const paths = SERVICE_MODULES.flatMap((m) => m.stats.map((s) => s.listPath));
    const alien = paths.filter((p) => !STEM_LIST_PATHS.includes(p));
    expect(alien).toEqual([]);
  });

  it("плитка compute не обещает блочного хранения — им владеет storage", () => {
    const compute = SERVICE_MODULES.find((m) => m.key === "compute");
    expect(compute).toBeDefined();
    for (const owned of STORAGE_OWNED) {
      expect(compute!.description).not.toMatch(owned);
    }
  });

  it("плитка compute всё же описывает то, чем домен владеет", () => {
    // Положительный контроль отрицания выше: описание не должно оказаться пустым
    // ради прохождения запрета.
    const compute = SERVICE_MODULES.find((m) => m.key === "compute")!;
    expect(compute.description.length).toBeGreaterThan(20);
    expect(compute.description).toMatch(/машин/i);
  });
});
