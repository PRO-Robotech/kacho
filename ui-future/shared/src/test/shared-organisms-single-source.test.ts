import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { declaredSymbols, isReExportOnly, sourceFiles, sweep, type ForkHit } from "./shared-symbol-sweep";

/**
 * Гейт единого источника: реализация живёт в `shared/`, приложение берёт её
 * оттуда. Копия внутри приложения — форк: правка, севшая в одну копию, молча
 * минует остальные, и пользователь читает разницу как «другое место продукта».
 *
 * ПРАВИЛО, А НЕ ПЕРЕЧЕНЬ. Прежняя редакция наблюдала ПЯТЬ поимённо названных
 * компонентов, тогда как парных файлов у каждого приложения около полутора
 * сотен: гейт был зелёным ровно про то, что в него вписали, и о всей остальной
 * поверхности не утверждал ничего. Сегодня правило перевёрнуто, и признаков у
 * него ДВА — по символу и по адресу:
 *
 *   файл вне `shared/`, который ОБЪЯВЛЯЕТ символ, объявленный `shared/`,
 *   ЛИБО лежит по тому же пути под `src/`, что файл `shared/src/`, и прослойкой
 *   не является, — находка, пока он не назван в ведомости с причиной.
 *
 * Второго признака здесь не было, и слепая зона у первого точная: копия с
 * ПЕРЕИМЕНОВАННЫМИ символами не спорит с общим ни одним именем, поэтому для
 * признака по символу невидима by construction. Так и жила копия помощников
 * подписи ссылки — `headLabelFor`/`extraInfoFor` против `refOptionHead`/
 * `refOptionExtra`, — которую перепись не показывала ни разу.
 *
 * Тонкая прослойка (`export * from "@shared/…"`, барель, реэкспорт с
 * переименованием) ничего не объявляет и остаётся разрешённой формой без всякой
 * записи — именно её и требуется писать вместо копии. Прослойкой файл признаётся
 * по СОДЕРЖИМОМУ, а не по имени: `index.ts` бывает и барелем, и копией.
 *
 * ВЕДОМОСТЬ САМОИСТЕКАЕТ. `shared-fork-ledger.json` перечисляет форки, которые
 * уже есть в дереве, с причиной по группе. Запись, которой больше нечего
 * исключать (файл сведён к ре-экспорту, символ переименован, группа опустела),
 * роняет гейт: послабление, пережившее свой предмет, — тот же класс, что мы
 * ловим в коде. Поэтому ведомость может только уменьшаться сама собой, а расти
 * — только явной правкой человека, которому придётся написать причину.
 *
 * ПЕРЕПИСЬ. Первая проба несёт числа осмотренного (приложения, прочитанные
 * файлы, символы `shared/`) и требует их ненулевыми: «форков не найдено» обязано
 * быть отличимо от «ничего не прочитано» — сдвинутый корень или переименованная
 * раскладка иначе делают все утверждения ниже вакуумно истинными.
 *
 * СВОЯ ПРЕДПОСЫЛКА. Вторая проба гоняет распознаватель объявлений по самому
 * `shared/` и требует ровно одно объявление на каждый компонент перечня
 * `COMPONENTS`. Если распознаватель перестанет узнавать реализацию (переезд на
 * `const X = () => …`, экспорт по умолчанию), это всплывёт здесь, а не превратит
 * обход дерева в тихий no-op.
 *
 * ПЕРЕСБОР ВЕДОМОСТИ — тем же предикатом, что и суд:
 *   KACHO_REGEN_FORK_LEDGER=1 npm test -- shared-organisms-single-source
 * Пересбор намеренно завершает пробу ОТКАЗОМ: причины по новым группам пишет
 * человек, и зелёный после автоматического пересбора означал бы «гейт согласен
 * с тем, что сам же и записал».
 */

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const LEDGER_PATH = path.join(repoRoot, "shared/src/test/shared-fork-ledger.json");

interface LedgerEntry {
  file: string;
  symbols: string[];
}
interface LedgerGroup {
  id: string;
  reason: string;
  entries: LedgerEntry[];
}
interface Ledger {
  note: string;
  groups: LedgerGroup[];
}

// Компонент описывается ТРЕМЯ фактами, а не одним именем: каталог в дереве,
// имя файла и экспортируемый символ. Прежняя редакция считала их совпадающими —
// верно ровно для трёх страниц ресурса и неверно для тела формы (лежит в
// `organisms/form/`) и рендерера поля (файл `FormField`, символ
// `FormFieldRenderer`, потому что `FormField` — это ТИП поля, а не компонент).
interface Organism {
  /** Каталог ОТ `components/`, а не от `components/organisms/`: ведомый перечень
   *  перестал быть только про организмы, когда в него вошла оболочка экранов
   *  состояния (`molecules/StatePanel`). Слой — часть адреса, а не подразумеваемая
   *  константа. */
  dir: string;
  file: string;
  symbol: string;
}

const COMPONENTS: readonly Organism[] = [
  {
    dir: "organisms/ResourceListPage",
    file: "ResourceListPage",
    symbol: "ResourceListPage",
  },
  {
    dir: "organisms/ResourceCreatePage",
    file: "ResourceCreatePage",
    symbol: "ResourceCreatePage",
  },
  {
    dir: "organisms/ResourceEditPage",
    file: "ResourceEditPage",
    symbol: "ResourceEditPage",
  },
  {
    dir: "organisms/form/ResourceFormBody",
    file: "ResourceFormBody",
    symbol: "ResourceFormBody",
  },
  { dir: "organisms/form/FormField", file: "FormField", symbol: "FormFieldRenderer" },
  // Граница отказа модуля (#371). Символ — HOC `withModuleBoundary`, а не класс
  // `ModuleErrorBoundary`: у обёртки один вид на всё дерево, и копия в модуле
  // означала бы, что правка экрана отказа доезжает не всюду. Правило здесь
  // ПОКАТАЛОЖНОЕ, поэтому соседи по каталогу — панель отказа и её рисунок
  // (`ModuleUnavailableArt`) — накрыты этой же записью, и своей им не нужно.
  {
    dir: "organisms/ModuleErrorBoundary",
    file: "ModuleErrorBoundary",
    symbol: "withModuleBoundary",
  },
  // Оболочка экранов состояния: «раздел временно недоступен» и «список пуст» —
  // ОДИН предмет и один вид (решение владельца о единстве вида). Копия в модуле
  // означала бы два вида одного предмета, а её не поймал бы ни признак по символу
  // (переименуют), ни признак по адресу (переименуют файл) — только это правило.
  { dir: "molecules/StatePanel", file: "StatePanel", symbol: "StatePanel" },
  // ── Пополнение перечня (#406) ────────────────────────────────────────────
  //
  // Гейт наблюдал СЕМЬ компонентов при полусотне парных каталогов, и такой
  // перечень читается как «форк закрыт», удостоверяя семь из пятидесяти. Ниже —
  // все компоненты, которые сегодня УЖЕ сведены к прослойке во ВСЯКОМ
  // приложении, где их каталог есть: добавление не краснит ни одного дерева, а
  // запрещает вернуть копию завтра. Отбор механический и повторяемый: символ
  // объявлен в `shared/` РОВНО ОДИН раз формой `export function`, каталог
  // приложения содержит только `index.ts`, и тот ведёт в `@shared`.
  { dir: "atoms/BoolFact", file: "BoolFact", symbol: "BoolFact" },
  { dir: "atoms/ContextBadge", file: "ContextBadge", symbol: "ContextBadge" },
  { dir: "atoms/CopyableId", file: "CopyableId", symbol: "CopyableId" },
  { dir: "atoms/CopyableName", file: "CopyableName", symbol: "CopyableName" },
  { dir: "atoms/LabelsCell", file: "LabelsCell", symbol: "LabelsCell" },
  { dir: "molecules/EditableKVTable", file: "EditableKVTable", symbol: "EditableKVTable" },
  { dir: "molecules/JsonEditor", file: "JsonEditor", symbol: "JsonEditor" },
  { dir: "molecules/PanelHeader", file: "PanelHeader", symbol: "PanelHeader" },
  { dir: "molecules/ProjectRequiredEmpty", file: "ProjectRequiredEmpty", symbol: "ProjectRequiredEmpty" },
  { dir: "organisms/AdminLayout", file: "AdminLayout", symbol: "AdminLayout" },
  { dir: "organisms/GlobalResourceFormModal", file: "GlobalResourceFormModal", symbol: "GlobalResourceFormModal" },
  { dir: "organisms/form/FieldLabel", file: "FieldLabel", symbol: "FieldLabel" },
  { dir: "organisms/form/FormFooter", file: "FormFooter", symbol: "FormFooter" },
  { dir: "organisms/form/FormShell", file: "FormShell", symbol: "FormShell" },
  { dir: "organisms/form/ImmutableField", file: "ImmutableField", symbol: "ImmutableField" },
  { dir: "organisms/LabelsEditor", file: "LabelsEditor", symbol: "LabelsEditor" },
  { dir: "organisms/form/LabelsEditor", file: "LabelsEditor", symbol: "LabelsFieldRenderer" },
  { dir: "atoms/StatusBadge", file: "StatusBadge", symbol: "StatusBadge" },
  { dir: "molecules/OperationsTable", file: "OperationsTable", symbol: "OperationsTable" },
  // ── Пополнение перечня (#1505/#1506) ────────────────────────────────────
  //
  // Предикат каталога перестал считать копией ПРОБУ и ВТОРУЮ ПРОСЛОЙКУ рядом с
  // `index.ts`, и вместе с этим отпало основание, по которому перечень стоял на
  // 22 записях при полусотне парных каталогов. Отбор — тот же механический и
  // повторяемый, что и в #406, с одной уточнённой осью: символ обязан
  // СОВПАДАТЬ С ИМЕНЕМ каталога и файла. Прежде фильтр брал «единственную
  // `export function` файла», и на трёх каталогах это давало ЧУЖОЙ символ
  // (`atoms/ui/Input` → `Label`, `atoms/VisibilityTag` → `visibilityLabel`):
  // компонент там объявлен `export const`, а проба реализации ниже утверждает
  // `export function`. Форма объявления у ведомых одна, и ослаблять её ради
  // трёх записей нельзя — та же причина, по которой вне перечня остаётся
  // `ModuleUnavailableArt`.
  //
  // Предикат отбора целиком: символ объявлен в `shared/` РОВНО ОДИН раз формой
  // `export function`, имя символа = имя файла = имя каталога, и в КАЖДОМ
  // приложении, где каталог есть, лежит `index.ts` в `@shared/components/<dir>`,
  // а всё прочее рядом — проба либо прослойка.
  { dir: "atoms/DirectionFact", file: "DirectionFact", symbol: "DirectionFact" },
  { dir: "atoms/PlacementBadge", file: "PlacementBadge", symbol: "PlacementBadge" },
  { dir: "molecules/CidrListCell", file: "CidrListCell", symbol: "CidrListCell" },
  { dir: "molecules/ConsumersFact", file: "ConsumersFact", symbol: "ConsumersFact" },
  { dir: "molecules/DeleteDialog", file: "DeleteDialog", symbol: "DeleteDialog" },
  { dir: "molecules/DopplerButton", file: "DopplerButton", symbol: "DopplerButton" },
  { dir: "molecules/IpamUtilizationBar", file: "IpamUtilizationBar", symbol: "IpamUtilizationBar" },
  { dir: "molecules/MoveStubDialog", file: "MoveStubDialog", symbol: "MoveStubDialog" },
  { dir: "molecules/PlacementAnchor", file: "PlacementAnchor", symbol: "PlacementAnchor" },
  { dir: "molecules/ResourceRefChips", file: "ResourceRefChips", symbol: "ResourceRefChips" },
  { dir: "molecules/SectionHeader", file: "SectionHeader", symbol: "SectionHeader" },
  { dir: "molecules/SubnetCidrChips", file: "SubnetCidrChips", symbol: "SubnetCidrChips" },
  { dir: "organisms/AddressPoolCidrManager", file: "AddressPoolCidrManager", symbol: "AddressPoolCidrManager" },
  { dir: "organisms/CidrGroupBlocksManager", file: "CidrGroupBlocksManager", symbol: "CidrGroupBlocksManager" },
  { dir: "organisms/CidrTableSection", file: "CidrTableSection", symbol: "CidrTableSection" },
  { dir: "organisms/DependencyTreePanel", file: "DependencyTreePanel", symbol: "DependencyTreePanel" },
  { dir: "organisms/InlineAddressPoolCreateForm", file: "InlineAddressPoolCreateForm", symbol: "InlineAddressPoolCreateForm" },
  { dir: "organisms/InlineAddressPoolEditForm", file: "InlineAddressPoolEditForm", symbol: "InlineAddressPoolEditForm" },
  { dir: "organisms/InlineNetworkInterfaceCreateForm", file: "InlineNetworkInterfaceCreateForm", symbol: "InlineNetworkInterfaceCreateForm" },
  { dir: "organisms/InlineNetworkInterfaceEditForm", file: "InlineNetworkInterfaceEditForm", symbol: "InlineNetworkInterfaceEditForm" },
  { dir: "organisms/InlineResourceCreateForm", file: "InlineResourceCreateForm", symbol: "InlineResourceCreateForm" },
  { dir: "organisms/InlineResourceEditForm", file: "InlineResourceEditForm", symbol: "InlineResourceEditForm" },
  { dir: "organisms/InlineSecurityGroupEditForm", file: "InlineSecurityGroupEditForm", symbol: "InlineSecurityGroupEditForm" },
  { dir: "organisms/InlineSubnetCreateForm", file: "InlineSubnetCreateForm", symbol: "InlineSubnetCreateForm" },
  { dir: "organisms/InlineSubnetEditForm", file: "InlineSubnetEditForm", symbol: "InlineSubnetEditForm" },
  { dir: "organisms/NetworkCidrManager", file: "NetworkCidrManager", symbol: "NetworkCidrManager" },
  { dir: "organisms/ResourceDetailPage", file: "ResourceDetailPage", symbol: "ResourceDetailPage" },
  { dir: "organisms/RoutesEditor", file: "RoutesEditor", symbol: "RoutesEditor" },
  { dir: "organisms/RoutesPanel", file: "RoutesPanel", symbol: "RoutesPanel" },
  { dir: "organisms/SgRulesPanel", file: "SgRulesPanel", symbol: "SgRulesPanel" },
  { dir: "organisms/SubnetCidrPanel", file: "SubnetCidrPanel", symbol: "SubnetCidrPanel" },
  { dir: "organisms/form/AddressVpcCascader", file: "AddressVpcCascader", symbol: "AddressVpcCascader" },
  { dir: "organisms/form/FieldError", file: "FieldError", symbol: "FieldError" },
  { dir: "organisms/form/FormGrid", file: "FormGrid", symbol: "FormGrid" },
  { dir: "organisms/form/NicSpecFields", file: "NicSpecFields", symbol: "NicSpecFields" },
  { dir: "organisms/form/NlbVipSourceField", file: "NlbVipSourceField", symbol: "NlbVipSourceField" },
  { dir: "organisms/form/SgRulesEditor", file: "SgRulesEditor", symbol: "SgRulesEditor" },
  { dir: "organisms/system/GrantAdminModal", file: "GrantAdminModal", symbol: "GrantAdminModal" },
  { dir: "organisms/system/OneTimeSecretModal", file: "OneTimeSecretModal", symbol: "OneTimeSecretModal" },
] as const;

/*
 * ПОЧЕМУ ВНЕ ПЕРЕЧНЯ ОСТАЛИСЬ ТЕ, КТО ОСТАЛСЯ — назвать причину обязательно,
 * иначе следующий читатель прочтёт пропуск как недосмотр и внесёт их, получив
 * красное на исправном дереве. Причина ПЕРЕМЕРЕНА (#1505/#1506) и оказалась НЕ
 * той, что здесь стояла: прежняя редакция объясняла пропуск двадцати одного
 * компонента тем, что «правило требует „в каталоге только index.ts“», и после
 * правки предиката это перестало быть верным — а пропуск остался. Настоящих
 * причин ровно три, и они разные.
 *
 * 1. ПРОСЛОЙКА В ДВА ЗВЕНА. `ResourceShell`, `ResourceTable`, `RefSelect`,
 *    `DetailShell`, `RefNameLink`, `ResourceLink`, `Toaster`, `OperationDialog`,
 *    `OperationToastWatcher`, `OperationBanner`, `ErrorResult`,
 *    `ResourceEmptyState`, `RowActionsMenu`, `ResourceIcon`,
 *    `InlineResourceForm`, `OperationsTab`, `ResourceFormModal`,
 *    `JsonMonacoView`, `DetailOverviewActions`, `IamRefLink`, `StepUpModal`,
 *    `PageHead`, `DetailSurface`, `RefMultiSelect` — у них `index.ts` ведёт не в
 *    `@shared`, а в СОСЕДНИЙ файл того же каталога (`export * from "./X"`), и уже
 *    тот файл — прослойка в общий барель. Форма законная и заведена осознанно:
 *    ре-экспорт ведёт на барель общего модуля, а не на один файл из него, иначе
 *    соседние части того же языка карточки остаются за барьером. Правило ниже
 *    требует `@shared` в тексте САМОГО `index.ts` и цепочку в два звена не
 *    проходит. Расширять его до цепочки — отдельный предмет: часть этих
 *    каталогов несёт НАСТОЯЩИЕ форки (у registry `ResourceShell.tsx` прослойкой
 *    не является и стоит в ведомости), поэтому внесение потребует сперва их
 *    свести. Все двадцать четыре накрыты правилом дерева выше — не наблюдением,
 *    а другим.
 *
 * 2. ФОРМА ОБЪЯВЛЕНИЯ. `ModuleUnavailableArt`, `atoms/ui/Button`,
 *    `atoms/ui/Dialog`, `atoms/ui/Input`, `atoms/ArtifactTypeTag`,
 *    `atoms/VisibilityTag`, `atoms/RepositoryLifecycleTag`,
 *    `molecules/NlbVipCell`, `molecules/PageHeaderSlot`, `molecules/TableToolbar`,
 *    `organisms/ResourceDetailExtensions`, `organisms/form/field-rules`,
 *    `organisms/iam/IamCommon` — компонент объявлен `export const` либо каталог
 *    вообще не имеет символа, совпадающего с именем файла. Проба реализации ниже
 *    утверждает `export function`, и ослаблять её ради этих записей нельзя:
 *    форма объявления у ведомых компонентов ОДНА, и именно на ней держится
 *    «своя предпосылка». `ModuleUnavailableArt` вдобавок сосед по уже ведомому
 *    каталогу и отдельной записи не требует.
 *
 * 3. ДВА ОБЪЯВЛЕНИЯ ОДНОГО ИМЕНИ В `shared/`. `organisms/SubnetCidrManager` —
 *    символ `SubnetCidrManager` объявлен и там, и в `molecules/SubnetCidrChips`.
 *    «Своя предпосылка» требует единственного объявления и покраснела бы на
 *    самом ОБЩЕМ модуле. Это настоящая находка, но её предмет — раздвоение
 *    внутри `shared/`, а не форк приложения. Тот же класс был у `LabelsEditor` и
 *    закрыт разведением имён (#1504).
 */
const SWEEP = sweep(repoRoot);

/** Причина по умолчанию для группы, которой человек ещё не написал свою. */
const REASON_TEMPLATE = "ПРИЧИНА НЕ НАПИСАНА — заполнить перед посадкой";

/** Группа файла: приложение + первый сегмент под `src/`. */
function groupIdOf(hit: ForkHit): string {
  const rel = hit.file.slice(hit.app.length + "/src/".length);
  const area = rel.includes("/") ? rel.slice(0, rel.indexOf("/")) : "(корень src)";
  return `${hit.app}:${area}`;
}

function regenerateLedger(): void {
  const prev: Ledger | null = existsSync(LEDGER_PATH)
    ? (JSON.parse(readFileSync(LEDGER_PATH, "utf8")) as Ledger)
    : null;
  const reasons = new Map<string, string>((prev?.groups ?? []).map((g) => [g.id, g.reason]));
  const byGroup = new Map<string, LedgerEntry[]>();
  for (const hit of SWEEP.hits) {
    const id = groupIdOf(hit);
    if (!byGroup.has(id)) byGroup.set(id, []);
    byGroup.get(id)!.push({ file: hit.file, symbols: hit.symbols });
  }
  const ledger: Ledger = {
    note:
      "Ведомость форков: файлы вне shared/, объявляющие символ, который объявляет shared/. " +
      "Держится гейтом shared-organisms-single-source. Запись, которой нечего исключать, роняет гейт — " +
      "ведомость уменьшается сама, растёт только правкой человека с причиной. Пересбор: " +
      "KACHO_REGEN_FORK_LEDGER=1 npm test -- shared-organisms-single-source",
    groups: [...byGroup.entries()]
      .sort(([a], [b]) => (a < b ? -1 : 1))
      .map(([id, entries]) => ({ id, reason: reasons.get(id) ?? REASON_TEMPLATE, entries })),
  };
  writeFileSync(LEDGER_PATH, `${JSON.stringify(ledger, null, 2)}\n`, "utf8");
}

if (process.env.KACHO_REGEN_FORK_LEDGER === "1") regenerateLedger();

const LEDGER: Ledger = JSON.parse(readFileSync(LEDGER_PATH, "utf8")) as Ledger;

describe("единый источник: реализация в shared/, приложение берёт её оттуда", () => {
  it(`перепись: приложения ${SWEEP.apps.length}, файлов приложений ${SWEEP.filesRead}, файлов shared ${SWEEP.sharedFilesRead}, символов shared ${SWEEP.sharedSymbols}, парных по пути ${SWEEP.pathPairedFiles} (из них прослоек ${SWEEP.shims})`, () => {
    expect(SWEEP.apps.length).toBeGreaterThan(0);
    expect(SWEEP.filesRead).toBeGreaterThan(0);
    expect(SWEEP.sharedFilesRead).toBeGreaterThan(0);
    expect(SWEEP.sharedSymbols).toBeGreaterThan(0);
    // Признак по адресу обязан что-то РАЗЛИЧАТЬ. Ноль парных путей означал бы,
    // что раскладка разъехалась и вторая ось не наблюдает ничего; ноль прослоек
    // — что распознаватель прослойки перестал их узнавать, и тогда все сведённые
    // файлы разом объявились бы форками.
    expect(SWEEP.pathPairedFiles).toBeGreaterThan(0);
    expect(SWEEP.shims).toBeGreaterThan(0);
    // Перечисление по факту наличия `src/` — то, что удержит гейт для
    // приложения, заведённого завтра; известные потребители при этом
    // закреплены, чтобы регрессия обнаружения не сузила обход незаметно.
    expect(SWEEP.apps).toEqual(
      expect.arrayContaining(["compute", "iam", "nlb", "registry", "storage", "vpc"]),
    );
  });

  it("своя предпосылка: распознаватель объявлений узнаёт реализации в shared/", () => {
    const sharedSources = sourceFiles(path.join(repoRoot, "shared/src"));
    expect(sharedSources.length).toBeGreaterThan(0);
    for (const comp of COMPONENTS) {
      const hits = sharedSources.filter((f) => declaredSymbols(readFileSync(f, "utf8")).has(comp.symbol));
      expect(hits.map((f) => path.relative(repoRoot, f))).toEqual([
        `shared/src/components/${comp.dir}/${comp.file}.tsx`,
      ]);
    }
  });

  it("ведомость несёт причину по каждой группе", () => {
    const withoutReason = LEDGER.groups
      .filter((g) => !g.reason || g.reason.trim() === "" || g.reason === REASON_TEMPLATE)
      .map((g) => g.id);
    expect(withoutReason).toEqual([]);
  });

  it("ведомость самоистекает: записи, которой нечего исключать, быть не должно", () => {
    const live = new Map(SWEEP.hits.map((h) => [h.file, h.symbols.join(",")]));
    const stale: string[] = [];
    for (const group of LEDGER.groups) {
      // Пустая группа — послабление, пережившее свой предмет.
      if (group.entries.length === 0) stale.push(`группа ${group.id}: записей нет`);
      for (const entry of group.entries) {
        const now = live.get(entry.file);
        if (now === undefined) {
          stale.push(`${entry.file}: форка больше нет — снять запись`);
          continue;
        }
        const declared = [...entry.symbols].sort().join(",");
        if (declared !== now) {
          stale.push(`${entry.file}: ведомость называет [${declared}], в дереве [${now}]`);
        }
      }
    }
    expect(stale).toEqual([]);
  });

  it("новых форков нет: каждая копия вне shared/ названа в ведомости", () => {
    const excused = new Set(LEDGER.groups.flatMap((g) => g.entries.map((e) => e.file)));
    // Координаты, а не счёт: красный гейт обязан сказать ГДЕ и ПОЧЕМУ он это
    // считает копией — иначе находка по адресу читается как ложное срабатывание
    // на файле, который ни одного общего имени не объявляет.
    const findings = SWEEP.hits
      .filter((h) => !excused.has(h.file))
      .map((h) =>
        h.symbols.length > 0
          ? `${h.file}: объявляет символы shared — ${h.symbols.join(", ")}`
          : `${h.file}: парен по пути с shared/src и прослойкой не является`,
      );
    expect(findings).toEqual([]);
  });

  it("пересбор ведомости не выдаёт себя за вердикт", () => {
    // Автоматический пересбор пишет ведомость по дереву — то есть соглашается с
    // тем состоянием, которое сам же и записал. Причины пишет человек, поэтому
    // прогон с пересбором обязан быть красным.
    expect(process.env.KACHO_REGEN_FORK_LEDGER).not.toBe("1");
  });
});

// Ведомые компоненты форку не подлежат ВОВСЕ: их в ведомости нет и быть не может
// (правило выше поймало бы объявление, эта проба ловит и остатки каталога — файл
// рядом с прослойкой, который прослойкой не является, в том числе с ПЕРЕИМЕНОВАННЫМ
// именем файла и переименованными символами: такую копию правило выше не видит).
describe("ведомые компоненты: в приложении допустима только прослойка @shared", () => {
  for (const comp of COMPONENTS) {
    const sharedFile = path.join(repoRoot, "shared/src/components", comp.dir, `${comp.file}.tsx`);

    it(`${comp.symbol} реализован в shared/`, () => {
      expect(existsSync(sharedFile)).toBe(true);
      expect(readFileSync(sharedFile, "utf8")).toContain(`export function ${comp.symbol}`);
    });

    for (const app of SWEEP.apps) {
      const appDir = path.join(repoRoot, app, "src/components", comp.dir);

      it(`${app}/${comp.dir}: только прослойка @shared`, () => {
        if (!existsSync(appDir)) return; // приложение этот компонент не показывает
        const indexFile = path.join(appDir, "index.ts");

        expect({ app, comp: comp.dir, hasIndex: existsSync(indexFile) }).toEqual({
          app,
          comp: comp.dir,
          hasIndex: true,
        });
        expect(readFileSync(indexFile, "utf8")).toContain("@shared/components/" + comp.dir);
        // Всё, кроме прослоек, — переодетый форк. Слово тут во множественном
        // числе не случайно: прослойка бывает не одна. `index.ts` ведёт в общий
        // барель, а рядом с ним законно лежит вторая прослойка — на подмодуль,
        // который импортируют отдельно (`opFilter.ts` → чистые предикаты
        // фильтрации без графа antd). Прежний предикат «в каталоге только
        // index.ts» такую прослойку от копии не отличал и потому дал бы ложное
        // красное на исправном дереве; ровно этим и был закрыт от наблюдения
        // `OperationsTable` (#1505), а до него — два десятка компонентов, у
        // которых рядом с `index.ts` лежит законная прослойка `<Имя>.tsx`.
        //
        // Прослойкой файл признаётся ПО СОДЕРЖИМОМУ (`isReExportOnly`), тем же
        // предикатом, которым его признаёт правило дерева выше. Второй копии
        // предиката здесь не заводится: разойдясь, они разошлись бы молча — на
        // валидном входе оба «согласны».
        //
        // Но ПРОБА рядом с прослойкой
        // форком не является: она ничего не объявляет, в бандл не попадает и
        // утверждает о том же общем компоненте, только с точки зрения этого
        // модуля (тон полосы у compute, словарь состояний у storage). Прежний
        // предикат «в каталоге только index.ts» её от копии не отличал, и цена
        // слепоты была ровно один невидимый компонент: `StatusBadge` пришлось
        // держать ВНЕ перечня наблюдаемых, потому что внесение дало бы красное
        // на исправном дереве (#1506). Дальше она стоила бы по компоненту за
        // каждого, у кого рядом с прослойкой захотят написать пробу.
        //
        // Признак — суффикс имени файла, и здесь это не «мерить соглашение об
        // именовании»: `.test.`/`.spec.` — то, по чему ФАЙЛ ОТБИРАЕТ САМ jest
        // (`testMatch` в `jest.config.cjs` каждого приложения). Файл с таким
        // именем исполняется как проба by construction, а не по чьей-то
        // договорённости.
        const stray = readdirSync(appDir).filter((f) => {
          if (f === "index.ts") return false;
          if (/\.(test|spec)\.tsx?$/.test(f)) return false;
          const full = path.join(appDir, f);
          if (!statSync(full).isFile()) return true;
          return !isReExportOnly(readFileSync(full, "utf8"));
        });
        expect({ app, comp: comp.dir, stray }).toEqual({
          app,
          comp: comp.dir,
          stray: [],
        });
      });
    }
  }
});
