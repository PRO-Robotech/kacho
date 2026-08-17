import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { stripComments } from "@shared/test/strip-comments";

/**
 * Гейт: поле подбора называет свою область — или спрашивает сервер.
 *
 * # Класс
 *
 * Выпадающее поле читает список ОДНОЙ страницей и сужает его в браузере. Ввод
 * не покидает вкладку, а поле отвечает «нет совпадений» — то есть утверждает об
 * отсутствии ресурса то, чего не спрашивало.
 *
 * У списка (#373) это чинилось подписью: рядом стоят «Показать ещё» и счётчик,
 * то есть у пользователя есть продолжение, и «отобрано среди загруженных» —
 * законный ответ. У поля подбора продолжения нет BY CONSTRUCTION: человек видит
 * список, не находит имя и заключает, что ресурса не существует. Поэтому #528
 * и заведён отдельно — тем же классом с ДРУГИМ исходом.
 *
 * # Что требуется
 *
 * Всякий исходник консоли, который сужает варианты в браузере (`showSearch`,
 * `optionFilterProp`, `filterOption`), обязан читать область поиска из общего
 * места — `@shared/lib/picker-search`. Область решает ровно одно: уходит ли ввод
 * запросом, и что стоит на месте пустого ответа.
 *
 * # Чего гейт НЕ держит, и это сказано, а не умолчано
 *
 * Он не может доказать, что область применена ПРАВИЛЬНО: связь «ввод → параметр
 * запроса» проверяется модульными пробами поля (`RefSelect.test.tsx`,
 * `GrantAdminModal.test.tsx`) и сквозной пробой браузером. Гейт держит другое,
 * и это половина, которую нельзя удержать вниманием: НОВОЕ поле подбора не
 * появится молча со своим сужением в браузере. Ровно так же устроен гейт
 * единого источника форков — он тоже утверждает происхождение, а не поведение.
 *
 * # Исключение — поимённое, и оно САМОИСТЕКАЕТ
 *
 * Поле над СТАТИЧЕСКИМ набором (перечисление формы) сужать нечем: сервера за
 * ним нет, и «нет совпадений» там — утверждение о самом наборе, то есть правда.
 * Такие файлы перечислены поимённо; запись, у которой больше нет поля подбора,
 * — находка, а не наследуемая слепая зона.
 *
 * # Объём осмотренного
 *
 * Печатается перепись: исходников прочитано, поверхностей подбора найдено,
 * из них читают область, исключений, находок. «Ноль находок» обязано быть
 * отличимо от «ноль прочитанного».
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Признаки сужения вариантов в браузере — как это пишется в этом дереве. */
const CLIENT_NARROWING = ["showSearch", "optionFilterProp", "filterOption"];

/** Чтение области поиска из общего места. */
const READS_SCOPE = ["pickerScope", "pickerScopeOfSpec"];

/**
 * Поля над статическим набором: сужать нечем, сервера за набором нет.
 *
 * Ключ — путь от корня консоли; значение — почему. Запись без поля подбора
 * становится находкой (см. проверку самоистечения ниже).
 */
const STATIC_ONLY: Record<string, string> = {
  "shared/src/components/organisms/form/FormField/FormField.tsx":
    "поле типа «перечисление»: варианты объявлены самой формой, у набора нет владельца, которого можно спросить",
};

// `src/test` — оснастка проб (заменители antd, обходчики дерева). Она не поле
// подбора и владельца не имеет: заменитель ОБЯЗАН нести признаки настоящего,
// иначе проба о них утверждать не может. Каталог исключается по роли, а не
// поимённо — иначе перечень исключений рос бы вместе с оснасткой.
const SKIP_DIRS = new Set(["node_modules", "dist", "build", ".git", "coverage", "e2e", "test"]);

function walk(root: string, acc: string[] = []): string[] {
  let entries: string[];
  try {
    entries = readdirSync(root);
  } catch {
    return acc;
  }
  for (const entry of entries) {
    const full = path.join(root, entry);
    let s;
    try {
      s = statSync(full);
    } catch {
      continue;
    }
    if (s.isDirectory()) {
      if (!SKIP_DIRS.has(entry)) walk(full, acc);
      continue;
    }
    if (/\.(ts|tsx)$/.test(entry) && !/\.(test|spec)\.(ts|tsx)$/.test(entry)) acc.push(full);
  }
  return acc;
}

/** Сужает ли исходник варианты в браузере, и читает ли он область поиска. */
export function pickerFacts(source: string): { narrows: boolean; readsScope: boolean } {
  // Комментарии снимаются: гейт обязан читать исполняемую часть. Объяснение,
  // почему поле НЕ спрашивает сервер, — это разбор, а не сужение.
  const code = stripComments(source);
  return {
    narrows: CLIENT_NARROWING.some((m) => code.includes(m)),
    readsScope: READS_SCOPE.some((m) => code.includes(m)),
  };
}

const FILES = walk(consoleRoot);

interface Surface {
  rel: string;
  readsScope: boolean;
}

const SURFACES: Surface[] = [];
for (const file of FILES) {
  const rel = path.relative(consoleRoot, file).split(path.sep).join("/");
  const facts = pickerFacts(readFileSync(file, "utf8"));
  if (facts.narrows) SURFACES.push({ rel, readsScope: facts.readsScope });
}

const SURFACE_PATHS = new Set(SURFACES.map((s) => s.rel));
const FINDINGS = SURFACES.filter((s) => !s.readsScope && !(s.rel in STATIC_ONLY)).map((s) => s.rel);
const STALE_EXCEPTIONS = Object.keys(STATIC_ONLY).filter((rel) => !SURFACE_PATHS.has(rel));

describe("поле подбора называет свою область или спрашивает сервер", () => {
  it("перепись осмотренного названа, и предпосылка выполняется", () => {
    expect(FILES.length).toBeGreaterThan(100);
    // Ноль поверхностей означал бы, что признак сужения перестал встречаться —
    // то есть обход или предикат сломались, а не что дерево чисто.
    expect(SURFACES.length).toBeGreaterThan(5);
    // eslint-disable-next-line no-console
    console.log(
      `перепись: исходников прочитано ${FILES.length}, поверхностей подбора ${SURFACES.length}, ` +
        `из них читают область ${SURFACES.filter((s) => s.readsScope).length}, ` +
        `исключений ${Object.keys(STATIC_ONLY).length}, находок ${FINDINGS.length}`,
    );
  });

  // Падение печатает пути. Что с ними делать: поле обязано читать область из
  // `@shared/lib/picker-search` и либо спрашивать сервер (если владелец умеет
  // сужать — см. `serverSearchField` / `search.serverTerm`), либо назвать свою
  // область в `notFoundContent`. Третьего исхода нет: молчаливое «нет
  // совпадений» утверждает об отсутствии ресурса то, чего поле не спрашивало.
  it("каждая поверхность подбора читает область поиска", () => {
    expect(FINDINGS).toEqual([]);
  });

  // Исключению нечего исключать — значит оно наследуется следующим как слепая
  // зона. Такая запись снимается вместе с тем, что её оправдывало.
  it("у каждого исключения есть предмет", () => {
    expect(STALE_EXCEPTIONS).toEqual([]);
  });

  it("детектор различает сужение и объяснение сужения", () => {
    const defective = 'const s = <Select showSearch optionFilterProp="label" options={opts} />;';
    expect(pickerFacts(defective)).toEqual({ narrows: true, readsScope: false });

    const lawful =
      'import { pickerScope } from "@shared/lib/picker-search";\n' +
      "const scope = pickerScope({ serverSearchField: \"name\" });\n" +
      "const s = <Select showSearch onSearch={setTerm} notFoundContent={scope.emptyText} />;";
    expect(pickerFacts(lawful)).toEqual({ narrows: true, readsScope: true });

    // Разбор в комментарии сужением не является: запретить объяснять предмет
    // запрета значило бы завести отдельный класс вместо того, чтобы закрыть этот.
    const prose = "// showSearch здесь не нужен: набор статический и сужать нечем\nconst s = <Select options={opts} />;";
    expect(pickerFacts(prose)).toEqual({ narrows: false, readsScope: false });
  });
});
