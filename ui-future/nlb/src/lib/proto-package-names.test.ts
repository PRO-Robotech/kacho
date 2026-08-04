// Имена proto-пакетов, которые называет исходник этого приложения, — против
// дерева `proto/` ствола.
//
// Комментарий, называющий несуществующий пакет, — не опечатка: он отправляет
// читателя искать контракт по адресу, которого нет, и по дороге переучивает его
// неверному имени домена. Частый источник — совпадение REST-сегмента с именем
// пакета там, где они РАЗНЫЕ: у балансировщика путь `/nlb/v1/...`, а пакет
// `kacho.cloud.loadbalancer.v1`, и оба верны.
//
// Единица счёта — вхождение вида `kacho.cloud.<домен>[.vN]` в любом `.ts`/`.tsx`
// под `src`, включая комментарии (в них-то они и живут). Допустимое множество
// ВЫВОДИТСЯ из `package`-объявлений дерева `proto/`, а не выписывается здесь:
// заведут домен — гейт узнает об этом сам, снесут — тоже.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

const REPO_ROOT = join(process.cwd(), "..", "..");
const PROTO_ROOT = join(REPO_ROOT, "proto");
const SRC_ROOT = join(process.cwd(), "src");

function walk(dir: string, accept: (f: string) => boolean): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      out.push(...walk(full, accept));
    } else if (accept(full)) {
      out.push(full);
    }
  }
  return out;
}

/** Пакеты, объявленные в дереве proto ствола. */
function declaredPackages(): Set<string> {
  const pkgs = new Set<string>();
  for (const file of walk(PROTO_ROOT, (f) => f.endsWith(".proto"))) {
    const m = readFileSync(file, "utf8").match(/^package\s+([A-Za-z0-9_.]+)\s*;/m);
    if (m) pkgs.add(m[1]);
  }
  return pkgs;
}

/** Вхождения `kacho.cloud.<домен>[.vN]` в тексте. */
export function mentionedPackages(text: string): string[] {
  return text.match(/kacho\.cloud\.[a-z_]+(?:\.v\d+)?/g) ?? [];
}

// Из обхода исключён РОВНО один файл — этот: ниже стоят синтетические входы
// инъекции, и без исключения гейт находил бы собственную фикстуру и краснел на
// себе. Прочие тесты домена из обхода НЕ исключены: лживый комментарий в тесте —
// такая же ложь, как в прод-коде.
const SELF = join(SRC_ROOT, "lib", "proto-package-names.test.ts");
const sourceFiles = walk(SRC_ROOT, (f) => /\.tsx?$/.test(f) && f !== SELF);

describe("имена proto-пакетов в исходнике против дерева ствола", () => {
  it("предпосылка: и дерево proto, и исходник приложения непусты", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: обход,
    // не нашедший ни одного файла, — находка, а не чистота.
    expect(declaredPackages().size).toBeGreaterThan(0);
    expect(sourceFiles.length).toBeGreaterThan(0);
  });

  it("каждое названное имя пакета существует в стволе", () => {
    const known = declaredPackages();
    const alien: string[] = [];
    for (const file of sourceFiles) {
      for (const pkg of mentionedPackages(readFileSync(file, "utf8"))) {
        if (!known.has(pkg)) alien.push(`${file.slice(process.cwd().length + 1)}: ${pkg}`);
      }
    }
    expect(alien).toEqual([]);
  });

  it("инъекция: имя, которого в стволе нет, предикатом извлекается", () => {
    // Настоящий вход — та самая строка, что стояла в дереве.
    expect(mentionedPackages("// proto: kacho.cloud.nlb.v1.NetworkLoadBalancerService.")).toEqual([
      "kacho.cloud.nlb.v1",
    ]);
    expect(declaredPackages().has("kacho.cloud.nlb.v1")).toBe(false);
  });

  it("законный близнец: настоящее имя того же домена гейт пропускает", () => {
    expect(mentionedPackages("// proto: kacho.cloud.loadbalancer.v1 (REST — /nlb/v1/...)")).toEqual([
      "kacho.cloud.loadbalancer.v1",
    ]);
    expect(declaredPackages().has("kacho.cloud.loadbalancer.v1")).toBe(true);
  });
});
