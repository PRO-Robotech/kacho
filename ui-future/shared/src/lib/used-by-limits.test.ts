// Потолок «кем используется» объявлен в ДВУХ местах — и второе обязано сверяться
// с первым, а не переписываться по памяти.
//
// Сервер отдаёт не больше предела плюс одну строку; лишняя строка — единственный
// признак «есть ещё», отдельного поля под него в контракте нет. Значит консоль
// обязана знать ровно то же число: занизит — припишет усечение полному списку,
// завысит — промолчит об усечении. Ни то ни другое не даёт ошибки сборки и не
// видно на ревью.
//
// Проба несёт проверку СВОЕЙ предпосылки: если исходник сервера не прочитался
// или объявления в нём нет, она падает, а не объявляет согласие.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { SECURITY_GROUP_USED_BY_LIMIT } from "./used-by-limits";

// cwd прогона — каталог приложения (ui-future/<app>), как и у соседних проб,
// читающих дерево.
const REPO_ROOT = resolve(process.cwd(), "../..");
const GO_SOURCE = resolve(REPO_ROOT, "services/vpc/internal/repo/kacho/entity_security_group.go");

describe("потолок used_by совпадает с серверным", () => {
  it("число взято из объявления сервера, а не из памяти", () => {
    const src = readFileSync(GO_SOURCE, "utf8");
    expect(src.length).toBeGreaterThan(0);

    const m = src.match(/const\s+SecurityGroupUsedByLimit\s*=\s*(\d+)/);
    expect(m).not.toBeNull();

    expect(SECURITY_GROUP_USED_BY_LIMIT).toBe(Number(m![1]));
  });

  it("сервер читает на одну строку больше предела — иначе признака «есть ещё» нет", () => {
    const src = readFileSync(GO_SOURCE, "utf8");
    expect(src).toMatch(/const\s+SecurityGroupUsedByFetch\s*=\s*SecurityGroupUsedByLimit\s*\+\s*1/);
  });
});
