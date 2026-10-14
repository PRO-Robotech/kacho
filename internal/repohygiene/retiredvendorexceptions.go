// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// retiredvendorexceptions.go — ЗАКРЫТЫЙ ПОИМЁННЫЙ ПЕРЕЧЕНЬ мест, где имя снятого
// поставщика личности в Go и в развёртывании законно (задача #1276, волна 5
// #2940 эпика #2564).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Предикат снятия #1276 — «упоминаний поставщика в Go → 0, в развёртывании → 0».
// Буквального нуля у него нет и быть не может: страж возврата называет имя,
// возврат которого он ловит, и без этого имени не судит ничего. Поэтому предикат
// читается в форме эпика: ВНЕ ЗАКРЫТОГО ПОИМЁННОГО ПЕРЕЧНЯ ИСКЛЮЧЕНИЙ — 0, и
// перечень — часть предиката. Здесь он записан и судится против дерева.
//
// Исключением бывает только:
//
//	страж          код, который отвергает возврат или находит его (гейт,
//	               отказ рендера, живой гейт стенда, проба отсутствия);
//	проба стража   проба либо инъекция стража, названного полем Guard;
//	словарь        единственный дом имён, которые читают стражи.
//
// Проза-история, синтетика, мёртвые ветви и настройки исключением НЕ являются:
// прозу и синтетику переписывают без имени, мёртвую ветвь снимают вместе с её
// стражем. Корзины «прочее» нет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОБЛАСТЬ И ЕДИНИЦА — ТЕ ЖЕ, ЧТО У КОМАНД ТЕЛА ЗАДАЧИ
//
// Область — отслеживаемые файлы `*.go` в любом каталоге и всё под `deploy/`;
// пересечение (`deploy/*.go`) считается один раз. Единица — строка, несущая
// отметку имени без учёта регистра; отметки берутся у единственного дома
// (`internal/identityvendor`), а не выписываются здесь копией. Подстрока ищется
// без границы слова — ровно так, как её ищет `git grep -i`: число гейта и число
// команды тела обязаны совпасть, и проба на дереве сверяет их двумя
// выражениями (обход индекса здесь и `git grep` там). Двоичный файл, несущий
// отметку в байтах, — одна единица, как строка «Binary file … matches».
//
// Отметки шире двух слов команды тела на одну — пространство образов
// поставщика. Строк, несущих её без двух слов, сегодня 4, все у стража; число
// гейта поэтому не меньше числа команды тела, и перепись печатает оба.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ
//
//  1. Строка с отметкой в файле без записи — находка с координатой и текстом.
//  2. Число строк записи ТОЧНОЕ: рост и убыль — находка обе. Потолок прощал бы
//     возврат до себя и переставал бы быть наблюдением.
//  3. Запись, которой нечего исключать (файла в индексе нет либо отметки в нём
//     нет), — находка: исключение истекает вместе с предметом.
//  4. Вид — из закрытого словаря; якорь стоит в файле (иначе запись описывает
//     уже другое содержимое); у пробы стража страж назван путём, и он в
//     индексе; у стража и словаря поля Guard нет.
//  5. Путь вне области, повтор пути, пустой довод — находка.
//
// Сам перечень и его гейт отметок не несут: файлы гейта лежат в области, и
// строка с именем в них была бы находкой первого вида без всякой записи.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ГЕЙТ НЕ УТВЕРЖДАЕТ
//
//   - что строка внутри записанного файла — строка стража. Единица записи —
//     файл с точным числом: замена строки стража строкой прозы того же файла
//     число не меняет. Держит это ревью правки стража, а не гейт;
//   - ничего о файлах вне области (проза `docs/`, `ui-future/`, чарты служб):
//     их предикат — другие держатели (запись Б ведомости судьбы проб, страж
//     переписи адресов консоли);
//   - ничего о бренде поставщика отдельным словом и о закодированных значениях
//     профилей: это другой распознаватель (потолок привязок и рендерные пробы).

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// Виды исключения. Словарь ЗАКРЫТ.
const (
	retiredVendorExceptionGuard      = "страж"
	retiredVendorExceptionGuardProbe = "проба стража"
	retiredVendorExceptionDictionary = "словарь"
)

// retiredVendorExceptionKinds — закрытый словарь видов в порядке печати.
var retiredVendorExceptionKinds = []string{
	retiredVendorExceptionGuard, retiredVendorExceptionGuardProbe, retiredVendorExceptionDictionary,
}

// retiredVendorExceptionScopeText — область предиката одним текстом для переписи.
const retiredVendorExceptionScopeText = "отслеживаемые *.go в любом каталоге и всё под deploy/"

// retiredVendorExceptionInScope — путь лежит в области предиката.
func retiredVendorExceptionInScope(rel string) bool {
	return strings.HasSuffix(rel, ".go") || strings.HasPrefix(rel, "deploy/")
}

// retiredVendorException — запись перечня.
type retiredVendorException struct {
	// Path — путь от корня репозитория.
	Path string
	// Lines — ТОЧНОЕ число строк файла, несущих отметку.
	Lines int
	// Kind — вид из retiredVendorExceptionKinds.
	Kind string
	// Anchor — текст, который стоит в файле: символ стража, пробы или словаря.
	Anchor string
	// Guard — у пробы стража путь стража, которого она судит; у прочих пусто.
	Guard string
	// Why — довод: что страж отвергает и почему без имени не судит.
	Why string
}

// retiredVendorExceptions — перечень. Порядок — по пути.
var retiredVendorExceptions = []retiredVendorException{
	{
		Path: "deploy/helm/umbrella/charts/kaname/templates/_helpers.tpl", Lines: 3,
		Kind: retiredVendorExceptionGuard, Anchor: `{{- define "kaname.refuseRetiredKnobs" -}}`,
		Why: "отказ рендера подчарта службы на трёх снятых ручках дороги обмена (#2936): " +
			"имя ручки — то, что может написать оператор, и отказ называет её",
	},
	{
		Path: "deploy/kaname_subchart_retired_exchange_road_test.go", Lines: 3,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestKanameSubchartRefusesTheRetiredExchangeRoadKnobs(",
		Guard: "deploy/helm/umbrella/charts/kaname/templates/_helpers.tpl",
		Why:   "проба отказа на каждой из трёх снятых ручек с близнецом без ручки",
	},
	{
		Path: "deploy/kaname_subchart_retired_identity_wiring_injection_test.go", Lines: 6,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestKanameRetiredWiringInjection_VendorWiringIsFoundOnEveryAxis(",
		Guard: "deploy/kaname_subchart_retired_identity_wiring_test.go",
		Why:   "инъекция провязки поставщика в подчарт службы по каждой оси",
	},
	{
		Path: "deploy/kaname_subchart_retired_identity_wiring_test.go", Lines: 3,
		Kind: retiredVendorExceptionGuard, Anchor: "func TestKanameSubchartRendersNoVendorWiring(",
		Why: "возврат провязки поставщика, снятых ключей пина и полосы хуков в рендер подчарта службы (#2818)",
	},
	{
		Path: "deploy/own_posture_foreign_identity_injection_test.go", Lines: 13,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestOwnPostureForeignIdentityGate_FindsTheInheritedFlag(",
		Guard: "deploy/own_posture_foreign_identity_test.go",
		Why:   "инъекция возвращённого компонента поставщика в зонт с законными близнецами",
	},
	{
		Path: "deploy/own_posture_foreign_identity_test.go", Lines: 2,
		Kind: retiredVendorExceptionGuard, Anchor: "func TestOwnPostureRaisesNoForeignIdentityService(",
		Why: "возврат компонента поставщика в зонт — сторона дерева: распознаватель имени его подчарта",
	},
	{
		Path: "deploy/scripts/assert-identity-provider-absent.sh", Lines: 3,
		Kind: retiredVendorExceptionGuard, Anchor: "ЖИВОЙ ГЕЙТ ПОСАДКИ ЛИЧНОСТИ",
		Why: "живой гейт стенда: подов поставщика нет и адреса его административного API " +
			"нет ни у одного потребителя — метки подов и имена переменных и есть предмет",
	},
	{
		Path: "deploy/stack_render_carries_no_vendor_residue_test.go", Lines: 10,
		Kind: retiredVendorExceptionGuard, Anchor: "func TestNoStackRenderCarriesAVendorResidue(",
		Why: "след поставщика в рендере каждой цепочки; распознаватель и инъекция возвращённого издателя",
	},
	{
		Path: "deploy/tests/helm/edge-keyset-hop-test.sh", Lines: 9,
		Kind: retiredVendorExceptionGuard, Anchor: "PROVIDER_SPELLING=",
		Why: "чарт края не рендерит снятых ключей скалярного пина и адреса издателя поставщика " +
			"ни в одном написании, в том числе когда профиль их задал",
	},
	{
		Path: "internal/identityvendor/identityvendor.go", Lines: 1,
		Kind: retiredVendorExceptionDictionary, Anchor: "func Marks(",
		Why: "единственное объявление отметок имени, которые читают потолок привязок, рендерные пробы и этот гейт",
	},
	{
		Path: "internal/repohygiene/foreignidpname.go", Lines: 21,
		Kind: retiredVendorExceptionGuard, Anchor: "var foreignIDPWords = ",
		Why: "имя поставщика в НАШИХ идентификаторах; словарь слов и законных форм гидратации",
	},
	{
		Path: "internal/repohygiene/foreignidpname_injection_test.go", Lines: 36,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestForeignIDPNameInjection_NameOutsideTheLedgerIsFound(",
		Guard: "internal/repohygiene/foreignidpname.go",
		Why:   "инъекция чужого имени в каждой законной форме записи и близнецы",
	},
	{
		Path: "internal/repohygiene/providersurface.go", Lines: 3,
		Kind: retiredVendorExceptionGuard, Anchor: "func FindProviderSurface(",
		Why: "поверхность API поставщика в коде и развёртывании — пути его API, а не слово",
	},
	{
		Path: "internal/repohygiene/providersurface_injection_test.go", Lines: 5,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestProviderSurfaceInjection_UnledgeredReachIsFound(",
		Guard: "internal/repohygiene/providersurface.go",
		Why:   "инъекция обращения к поверхности поставщика и прозаический близнец",
	},
	{
		Path: "internal/repohygiene/providersurfacedeployment_injection_test.go", Lines: 2,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestDeploymentExecutablePart_InjectionBothWays(",
		Guard: "internal/repohygiene/providersurfacedeployment_test.go",
		Why:   "инъекция административного адреса поставщика в исполняемую часть шаблона и комментарий-близнец",
	},
	{
		Path: "internal/repohygiene/retiredidentityvendorceiling.go", Lines: 27,
		Kind: retiredVendorExceptionGuard, Anchor: "func vendorMarkBoundedIn(",
		Why: "убывающий потолок привязок к поставщику по трём деревьям: привязка, добавленная против базы, — находка",
	},
	{
		Path: "internal/repohygiene/retiredidentityvendorceiling_injection_test.go", Lines: 69,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestRetiredVendorCeiling_NameAxisInjection(",
		Guard: "internal/repohygiene/retiredidentityvendorceiling.go",
		Why:   "инъекции по шести осям потолка (путь, строка, склейка, поверхность, двоичное, архив)",
	},
	{
		Path: "internal/repohygiene/retiredidentityvendorceiling_test.go", Lines: 9,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestRetiredIdentityVendorBindingsStayUnderTheirCeiling(",
		Guard: "internal/repohygiene/retiredidentityvendorceiling.go",
		Why:   "гейт потолка на дереве и предпосылки его распознавателя",
	},
	{
		Path: "internal/repohygiene/retiredidentityvendorceilingbase_test.go", Lines: 1,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestRetiredVendorCeiling_GrowthBranchIsRedWithTheAddedLine(",
		Guard: "internal/repohygiene/retiredidentityvendorceilingbase.go",
		Why:   "строка роста, которой проба на настоящих ветках git краснит потолок против базы",
	},
	{
		Path: "internal/repohygiene/retiredissuerclaim.go", Lines: 11,
		Kind: retiredVendorExceptionGuard, Anchor: "func judgeRetiredIssuerClaims(",
		Why: "утверждение, что снятый издатель всё ещё подписывает или выдаёт: формы утверждения называют его",
	},
	{
		Path: "internal/repohygiene/retiredissuerclaim_injection_test.go", Lines: 13,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestRetiredIssuerClaim_InjectionBothWays(",
		Guard: "internal/repohygiene/retiredissuerclaim.go",
		Why:   "инъекция каждой формы утверждения и описательные близнецы",
	},
	{
		Path: "internal/repohygiene/retiredvendorpathname_injection_test.go", Lines: 1,
		Kind: retiredVendorExceptionGuardProbe, Anchor: "func TestRetiredVendorPathName_ForeignReferentStaysSilent(",
		Guard: "internal/repohygiene/retiredvendorpathname.go",
		Why:   "законный близнец стража путей: путь с другим референтом (гидратация) молчит",
	},
	{
		Path: "internal/retiredknobs/retiredknobs.go", Lines: 6,
		Kind: retiredVendorExceptionDictionary, Anchor: "func Edge(",
		Why: "ведомость ручек, снятых с края: имя снятой переменной — предмет пробы отказа читателя и рендера",
	},
	{
		Path: "services/registry/internal/apps/kacho/config/config_test.go", Lines: 3,
		Kind: retiredVendorExceptionGuard, Anchor: "func TestConfig_RetiredTokenEnvsAreNotConsulted(",
		Why: "снятые переменные издателя реестра не консультируются: имя переменной — то, что может " +
			"оставить оператор",
	},
}

// retiredVendorMarkedLine — строка файла, несущая отметку.
type retiredVendorMarkedLine struct {
	Line int
	Text string
}

// retiredVendorMarkedLines — строки текста, несущие отметку (без учёта
// регистра, подстрокой). Двоичный файл (NUL в первых 8000 байтах — тот же
// признак, что у git) с отметкой в байтах — одна единица со строкой 0.
func retiredVendorMarkedLines(raw []byte, marks []string) (lines []retiredVendorMarkedLine, binary bool) {
	head := raw
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		lower := vendorASCIILower(string(raw))
		for _, m := range marks {
			if strings.Contains(lower, m) {
				return []retiredVendorMarkedLine{{Line: 0, Text: "двоичный файл несёт отметку в байтах"}}, true
			}
		}
		return nil, true
	}
	for i, l := range strings.Split(string(raw), "\n") {
		lower := vendorASCIILower(l)
		for _, m := range marks {
			if strings.Contains(lower, m) {
				lines = append(lines, retiredVendorMarkedLine{Line: i + 1, Text: strings.TrimSpace(l)})
				break
			}
		}
	}
	return lines, false
}

// retiredVendorExceptionCensus — перепись прогона.
type retiredVendorExceptionCensus struct {
	ScopeFiles, MarkedFiles, MarkedLines, BinaryMarked int
	Entries, Covered, Outside                          int
	ByKind                                             map[string]int
}

func (c retiredVendorExceptionCensus) String() string {
	kinds := make([]string, 0, len(retiredVendorExceptionKinds))
	for _, k := range retiredVendorExceptionKinds {
		kinds = append(kinds, fmt.Sprintf("%s %d", k, c.ByKind[k]))
	}
	return fmt.Sprintf("перечень исключений имени снятого поставщика: область — %s; файлов в области %d · "+
		"с отметкой %d файлов, %d строк (двоичных %d) · записей %d (%s) · строк под записями %d · вне перечня %d",
		retiredVendorExceptionScopeText, c.ScopeFiles, c.MarkedFiles, c.MarkedLines, c.BinaryMarked,
		c.Entries, strings.Join(kinds, ", "), c.Covered, c.Outside)
}

// retiredVendorExceptionTree — то, что гейт знает о дереве: состав индекса и
// содержимое каждого файла области и каждого названного стража.
type retiredVendorExceptionTree struct {
	Tracked map[string]bool
	Text    map[string][]byte
}

// judgeRetiredVendorExceptions — суд перечня против дерева. Находки
// отсортированы; ошибка — только у вызова без отметок (судить нечем).
func judgeRetiredVendorExceptions(ledger []retiredVendorException, marks []string,
	tree retiredVendorExceptionTree) ([]string, retiredVendorExceptionCensus, error) {
	c := retiredVendorExceptionCensus{ByKind: map[string]int{}}
	if len(marks) == 0 {
		return nil, c, fmt.Errorf("отметок имени ноль — распознавателю нечего искать, и «вне перечня 0» значило бы «ничего не читали»")
	}
	var found []string

	marked := map[string][]retiredVendorMarkedLine{}
	scope := make([]string, 0, len(tree.Tracked))
	for rel := range tree.Tracked {
		if retiredVendorExceptionInScope(rel) {
			scope = append(scope, rel)
		}
	}
	sort.Strings(scope)
	c.ScopeFiles = len(scope)
	for _, rel := range scope {
		lines, binary := retiredVendorMarkedLines(tree.Text[rel], marks)
		if len(lines) == 0 {
			continue
		}
		marked[rel] = lines
		c.MarkedFiles++
		c.MarkedLines += len(lines)
		if binary {
			c.BinaryMarked++
		}
	}

	byPath := map[string]retiredVendorException{}
	kinds := map[string]bool{}
	for _, k := range retiredVendorExceptionKinds {
		kinds[k] = true
	}
	for _, e := range ledger {
		c.Entries++
		if _, dup := byPath[e.Path]; dup {
			found = append(found, fmt.Sprintf("%s: запись перечня повторена — у файла одна запись и одно число", e.Path))
			continue
		}
		byPath[e.Path] = e
		c.ByKind[e.Kind]++
		if !retiredVendorExceptionInScope(e.Path) {
			found = append(found, fmt.Sprintf("%s: путь вне области предиката (%s) — исключать там нечего",
				e.Path, retiredVendorExceptionScopeText))
			continue
		}
		if !kinds[e.Kind] {
			found = append(found, fmt.Sprintf("%s: вид %q вне закрытого словаря {%s} — исключением бывает только "+
				"страж, его проба и словарь имён", e.Path, e.Kind, strings.Join(retiredVendorExceptionKinds, ", ")))
		}
		if strings.TrimSpace(e.Why) == "" {
			found = append(found, fmt.Sprintf("%s: довода нет — запись без довода не отличима от прощения", e.Path))
		}
		switch e.Kind {
		case retiredVendorExceptionGuardProbe:
			if e.Guard == "" {
				found = append(found, fmt.Sprintf("%s: проба стража не называет стража (поле Guard пусто)", e.Path))
			} else if !tree.Tracked[e.Guard] {
				found = append(found, fmt.Sprintf("%s: проба стража называет стража %s, которого в индексе нет — "+
					"страж снят, и проба описывает предмет, которого больше нет", e.Path, e.Guard))
			}
		case retiredVendorExceptionGuard, retiredVendorExceptionDictionary:
			if e.Guard != "" {
				found = append(found, fmt.Sprintf("%s: у вида %q поля Guard нет (стоит %s) — запись "+
					"смешивает стража и его пробу", e.Path, e.Kind, e.Guard))
			}
		}
		if !tree.Tracked[e.Path] {
			found = append(found, fmt.Sprintf("%s: исключению нечего исключать — файла в индексе нет; "+
				"запись снимается тем же изменением, что её файл", e.Path))
			continue
		}
		if e.Anchor == "" || !bytes.Contains(tree.Text[e.Path], []byte(e.Anchor)) {
			found = append(found, fmt.Sprintf("%s: якорь %q в файле не стоит — запись описывает уже другое "+
				"содержимое", e.Path, e.Anchor))
		}
		got := marked[e.Path]
		switch {
		case len(got) == 0:
			found = append(found, fmt.Sprintf("%s: исключению нечего исключать — отметки имени в файле нет; "+
				"запись снимается тем же изменением, что сняло имя", e.Path))
		case len(got) != e.Lines:
			found = append(found, fmt.Sprintf("%s: в перечне %d строк, в файле %d — число записывается ТОЧНО, "+
				"а не потолком: рост — возврат имени, убыль — запись, пережившая снятое. Строки:\n%s",
				e.Path, e.Lines, len(got), retiredVendorLinesText(e.Path, got)))
		}
		c.Covered += len(got)
	}

	outsidePaths := make([]string, 0)
	for rel := range marked {
		if _, ok := byPath[rel]; !ok {
			outsidePaths = append(outsidePaths, rel)
		}
	}
	sort.Strings(outsidePaths)
	for _, rel := range outsidePaths {
		c.Outside += len(marked[rel])
		found = append(found, fmt.Sprintf("%s: имя снятого поставщика вне перечня исключений, строк %d — "+
			"исключением бывает только страж, его проба и словарь имён (retiredVendorExceptions); прозу и "+
			"синтетику перепишите без имени, мёртвую ветвь снимите вместе с её стражем:\n%s",
			rel, len(marked[rel]), retiredVendorLinesText(rel, marked[rel])))
	}
	sort.Strings(found)
	return found, c, nil
}

// retiredVendorLinesText — строки находки, по одной на строку, с координатой.
func retiredVendorLinesText(rel string, lines []retiredVendorMarkedLine) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		text := l.Text
		if r := []rune(text); len(r) > 160 {
			text = string(r[:160]) + "…"
		}
		out = append(out, fmt.Sprintf("    %s:%d: %s", rel, l.Line, text))
	}
	return strings.Join(out, "\n")
}
