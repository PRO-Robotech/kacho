// TS-типы реестра образов против контракта ствола.
//
// Ground truth: proto/kacho/cloud/registry/v1/registry.proto и
// registry_service.proto.
//
// Два слоя, как и в nlb: типовой (компиляция ts-jest'ом ловит объектный литерал
// с неизвестным полем и отсутствие обязательного) и текстовый — файл не должен
// ОБЪЯВЛЯТЬ имён, снятых в стволе. Второй слой нужен потому, что лишнее
// необязательное поле компиляции не мешает: оно просто обещает то, чего сервер
// не пришлёт, и первый же читатель принимает обещание за факт.
//
// Здесь этот слой ловит не поле реестра, а ЦЕЛЫЕ описания чужих ресурсов:
// файл нёс формы Disk/Image/Snapshot/Instance/Subnet времён main — блочное
// хранение с тех пор уехало из compute в storage и переписано (у Image
// сменились даже номера полей, то есть это другое сообщение, а не эволюция
// прежнего), а подсеть переименовала диапазоны. Ни одно из этих описаний
// приложение не импортирует — но пока они лежат рядом с живыми, они читаются
// как действующий контракт.
//
// `v4_cidr_blocks`/`v6_cidr_blocks` в список НЕ входят: они остаются живыми
// именами у vpc SecurityGroup (message CidrBlocks), и запрет по имени вообще
// отвергал бы законное объявление.

import { readFileSync } from "node:fs";
import { join } from "node:path";

import type { Registry, Repository, Tag } from "./types";

const TYPES_FILE = "src/api/types.ts";

function declarationsOnly(): string {
  const raw = readFileSync(join(process.cwd(), TYPES_FILE), "utf8");
  return raw.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/[^\n]*/g, "$1");
}

const RETIRED_DECLARATIONS = [
  "platform_id",
  "storage_size",
  "min_disk_size",
  "product_ids",
  "source_disk_id",
  "disk_size",
  "pooled",
  "dhcp_options",
  "default_visibility?",
];

describe("типы реестра образов против контракта ствола", () => {
  it("Registry несёт регион, тип размещения и видимость по умолчанию", () => {
    const r: Registry = {
      id: "reg-000000000000000",
      project_id: "prj-1",
      // region_id обязателен на Create (registry_service.proto), immutable,
      // peer-validate у geo; placement_type всегда REGIONAL и на вход не подаётся.
      region_id: "ru-central1",
      placement_type: "REGIONAL",
      default_repository_visibility: "PRIVATE",
      endpoint: "registry.kacho.local/reg-000000000000000",
      repository_count: 0,
      status: "REGISTRY_STATUS_ACTIVE",
    };
    expect(r.region_id).toBe("ru-central1");
    expect(r.default_repository_visibility).toBe("PRIVATE");
  });

  it("Repository несёт класс исчезаемости и собственную видимость", () => {
    const repo: Repository = { name: "team/app", registry_id: "reg-000000000000000", lifecycle: "EPHEMERAL", visibility: "PRIVATE" };
    expect(repo.lifecycle).toBe("EPHEMERAL");
  });

  it("Tag адресуется реестром по id, а не по имени реестра", () => {
    const t: Tag = { tag: "v1", registry_id: "reg-000000000000000", repository: "team/app", digest: "sha256:00" };
    expect(t.registry_id).toMatch(/^reg-/);
  });

  it("файл типов не объявляет имён, снятых в стволе", () => {
    const text = declarationsOnly();
    for (const name of RETIRED_DECLARATIONS) {
      expect(text).not.toContain(name);
    }
  });

  it("файл типов объявляет то, что контракт действительно несёт", () => {
    // Положительный контроль отрицания выше.
    const text = declarationsOnly();
    for (const name of ["region_id", "placement_type", "default_repository_visibility", "lifecycle"]) {
      expect(text).toContain(name);
    }
  });
});
