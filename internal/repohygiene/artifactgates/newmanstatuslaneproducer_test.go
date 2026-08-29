// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanstatuslaneproducer_test.go — гейт против допуска, перечисляющего исход,
// у которого НА ЭТОЙ ПОЛОСЕ нет производителя.
//
// # Чем это отличается от соседа
//
// `httpstatusproducer_test.go` спрашивает: «производит ли край такой код ВООБЩЕ».
// Он ловит 412 и 422 — статусы, которых `runtime.HTTPStatusFromCode` не отдаёт ни
// для одного кода gRPC. Против 409 он бессилен by construction: 409 край
// производит — из `ALREADY_EXISTS` и `ABORTED`.
//
// Здесь спрашивается другое: «производит ли такой код ЭТА ПОЛОСА». Допуск
// `oneOf([400, 409])` на пути, где ни `ALREADY_EXISTS`, ни `ABORTED` не возникают,
// перечисляет исход без производителя — и не краснеет НИКОГДА, потому что ветка,
// которую он разрешает, недостижима. Кейс утверждает меньше, чем выглядит
// (`e2e-flow.md` §3, класс «412»).
//
// Полоса в общем случае не вычисляется механически: производители у каждого RPC
// свои. Поэтому гейт судит ДВА подкласса, у каждого из которых полоса задана
// самим текстом пробы или самим устройством дерева, — и молчит обо всём
// остальном. Граница названа здесь прямо, чтобы «ноль находок» не читалось шире,
// чем есть.
//
// # Подкласс A — допуск шире СОБСТВЕННОГО безусловного пина кода
//
// Шаг, БЕЗУСЛОВНО утверждающий `pm.response.json().code === N`, тем самым назвал
// свою полосу сам: при этом коде HTTP-статус ровно один — `HTTPStatusFromCode(N)`,
// и другого не бывает. Всякий иной статус в допуске того же шага недостижим
// СОВМЕСТНО с пином, то есть внутренне противоречив.
//
// Что это нашло, когда писалось (2026-08-27, задача #1372): registry
// `REG-RD-F4-NEG-REGION-NOTFOUND` принимал `oneOf([400, 409])` и тут же пинил
// `code == 9`. Подставленный 409 проходил через допуск и ронял СОСЕДА («code 9 не
// совпал») — то есть падение называло невиновного, и разбор шёл к коду вместо
// статуса (`testing.md` §«Диагноз ставится по ТЕКСТУ отказа»).
//
// УСЛОВНЫЙ ПИН НЕ СУДИТСЯ, и это несущее различие. Принятая в дереве форма
// многополосного негатива — допуск по составу полос плюс пин В ВЕТКЕ:
//
//	pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([400, 403]));
//	if (pm.response.code === 400) { pm.test('grpc 3', () => …to.eql(3)); }
//
// Здесь полос ДВЕ, каждая со своим кодом, и допуск законен. Первая редакция
// предиката считала условными только блоки, открытые условием ПО КОДУ ОТВЕТА, — и
// дала четыре находки из четырёх ложных: в `load-balancer` допуск и пин лежат в
// разных ветках `if (!фикстура) … else …`. Поэтому условным считается содержимое
// ЛЮБОГО ветвления.
//
// # Подкласс B — полоса операций, у которой производители СВЕДЕНЫ
//
// Арендаторская полоса операций (`/operations/{id}`, `/operations/{id}:cancel`)
// обслуживается двумя местами на всё дерево: общим слоем `pkg/operations` и краем
// `gateway/internal/opsproxy`. Значит множество её кодов ВЫЧИСЛЯЕТСЯ переписью
// этих двух каталогов — оно не выписано литералом и стареет вместе с деревом.
//
// Что это нашло: nlb `AZD-OP-CANCEL-NONCREATOR` принимал `oneOf([400, 403, 404,
// 409])` на отмене чужой операции. Отмена эмитит `InvalidArgument`/`NotFound`/
// `FailedPrecondition`/`Internal`, край добавляет `PermissionDenied`; 409 не даёт
// ни один, а `ALREADY_EXISTS`/`ABORTED` на полосе — ноль вхождений. Шаг зеленел на
// подставленном 409, не имея ни одного другого утверждения.
//
// # Форма, которая ВЫГЛЯДИТ допуском и им не является
//
// `[403, 404].includes(pm.response.code)` встречается в закоммиченных коллекциях
// 3819 раз — больше, чем все судимые формы вместе, — и гейт её намеренно НЕ
// читает. Перепись по вхождениям: они либо стоят внутри `if (…)`, либо
// присваиваются в `const _p200retryCode`, откуда уходят в условие повтора. То
// есть форма вычисляет ПРИЗНАК ПОВТОРА, а не утверждает исход: она ничего не
// обещает и упасть не может by construction.
//
// Сказано здесь потому, что молчание гейта на трёх с лишним тысячах вхождений
// иначе неотличимо от слепой зоны, а слепая зона и объявленная граница выглядят
// одинаково ровно до тех пор, пока кто-нибудь не пересчитает.
//
// # Предпосылка проверяется, а не объявляется
//
// Оба подкласса опираются на отображение кода в статус — оно ВЫЧИСЛЯЕТСЯ вызовом
// библиотеки, а не выписано. Подкласс B дополнительно опирается на утверждение о
// дереве («производители полосы живут в этих двух каталогах»), и оно проверяется:
// каталоги обязаны существовать и давать непустой набор кодов, иначе гейт
// отказывается судить. Заведись в них `ALREADY_EXISTS` — 409 станет законным сам
// собой, без правки списка.
package artifactgates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
)

// ─── распознаватели ──────────────────────────────────────────────────────────
//
// Форм ДОПУСКА в этом дереве три, и все настоящие: `oneOf([…])`, строгое
// равенство и обратный порядок `[…].to.include(pm.response.code)`. Предикат,
// знающий одну, молча освобождал бы остальные — и «ноль находок» покрывал бы то,
// чего он не читал.
var (
	slpOneOf = regexp.MustCompile(`pm\.response\.code[^;]{0,200}?\.to\.be\.oneOf\(\s*\[([0-9,\s]+)\]`)
	slpEql   = regexp.MustCompile(`pm\.response\.code[^;]{0,200}?\.to\.(?:eql|equal)\(\s*(\d{3})\s*\)`)
	slpInc   = regexp.MustCompile(`\[([0-9,\s]+)\][^;]{0,80}?\.to\.include\(\s*pm\.response\.code`)
	// Пин кода из тела. Читается и вложенная форма (`json().error.code`) — она
	// говорит о коде операции, а не ответа, поэтому исключается явно ниже.
	slpPin    = regexp.MustCompile(`pm\.response\.json\(\)\.code[^;]{0,120}?\.to\.(?:eql|equal)\(\s*(\d+)\s*\)`)
	slpBranch = regexp.MustCompile(`\b(?:if|else)\b`)
	slpOpsURL = regexp.MustCompile(`/operations/`)
)

// slpEdgeOwnStatuses — статусы, которые край отдаёт САМ, до вызова бэкенда, и
// потому законны на ЛЮБОЙ полосе. Перечень намеренно совпадает по существу с
// `muxOwnStatuses` соседнего гейта; повторён здесь, а не связан импортом, потому
// что пакеты разные, а предмет — три числа с причиной у каждого.
var slpEdgeOwnStatuses = map[int]string{
	405: "метод не тот — решает мультиплексор до бэкенда",
	415: "тип тела не тот — тоже до бэкенда",
	413: "тело больше потолка — middleware края и ingress",
}

// slpOpsLaneInfraStatuses — исходы полосы операций, которые производит НЕ её
// собственный код, а окружение вызова. Названы поимённо с причиной: список без
// причин через полгода неотличим от списка удобства.
var slpOpsLaneInfraStatuses = map[int]string{
	401: "неаутентифицированный вызывающий — интерсептор края отвечает до маршрутизации",
	503: "бэкенд полосы недоступен — fail-closed UNAVAILABLE",
	504: "бэкенд не ответил в срок вызова — DEADLINE_EXCEEDED",
	500: "внутренний отказ — INTERNAL общего слоя",
}

// slpOpsLaneProducerDirs — где живут производители арендаторской полосы операций.
// Утверждение о дереве, и оно проверяется: каталог обязан существовать и давать
// непустой набор кодов.
var slpOpsLaneProducerDirs = []string{
	filepath.Join("pkg", "operations"),
	filepath.Join("gateway", "internal", "opsproxy"),
}

var slpCodeLiteral = regexp.MustCompile(`\bcodes\.([A-Z][A-Za-z]*)\b`)

// slpGrpcCodeByName — имена кодов gRPC, встречающиеся в исходниках, в их числовые
// значения. Строится из библиотеки, а не выписывается: `codes.Code.String()` —
// единственный источник, который не может разойтись с ней.
func slpGrpcCodeByName() map[string]codes.Code {
	out := map[string]codes.Code{}
	for c := codes.Code(0); c <= codes.Code(16); c++ {
		out[strings.ReplaceAll(c.String(), " ", "")] = c
	}
	// `codes.Code.String()` даёт «OK», «InvalidArgument», … — те же имена, что и
	// идентификаторы пакета, кроме одного: «Unauthenticated» совпадает, «OK» в коде
	// пишется как `codes.OK`. Совпадение проверяется утверждением ниже.
	return out
}

// slpStripJSComment убирает хвостовой `//`-комментарий: гейт обязан отличать код
// от комментария, иначе краснеет на разборе, который сам же просил написать.
func slpStripJSComment(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		return s[:i]
	}
	return s
}

// slpStripGoComment — то же для Go: имя кода стоит и в комментариях, объясняющих
// маппинг, и засчитывать их за производителя значило бы доказывать предпосылку
// её собственным объяснением.
func slpStripGoComment(line string) string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "//") {
		return ""
	}
	return slpStripJSComment(line)
}

type slpFinding struct {
	collection string
	casePath   string
	step       string
	allowed    []int
	why        string
}

func (f slpFinding) String() string {
	return fmt.Sprintf("%s :: %s / %s — допуск %v: %s", f.collection, f.casePath, f.step, f.allowed, f.why)
}

type slpCensus struct {
	collections, steps      int
	withAllowance           int
	withUnconditionalPin    int
	opsLaneSteps            int
	opsLaneStepsWithAllowed int
}

// slpStepAssertions — разбор ОДНОГО шага: что он допускает на верхнем уровне и
// что пинит безусловно.
//
// «Верхний уровень» — вне любого ветвления. Условное содержимое не судится:
// многополосный негатив законно называет несколько исходов, у каждого свой код.
func slpStepAssertions(lines []string) (allowed map[int]bool, uncondPins map[int]bool) {
	allowed, uncondPins = map[int]bool{}, map[int]bool{}
	var branchStack []bool
	inBranch := func() bool {
		for _, b := range branchStack {
			if b {
				return true
			}
		}
		return false
	}
	for _, raw := range lines {
		line := slpStripJSComment(raw)
		onelineBranch := slpBranch.MatchString(line)
		if !inBranch() && !onelineBranch {
			for _, m := range slpOneOf.FindAllStringSubmatch(line, -1) {
				for _, part := range strings.Split(m[1], ",") {
					if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
						allowed[n] = true
					}
				}
			}
			for _, m := range slpInc.FindAllStringSubmatch(line, -1) {
				for _, part := range strings.Split(m[1], ",") {
					if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
						allowed[n] = true
					}
				}
			}
			for _, m := range slpEql.FindAllStringSubmatch(line, -1) {
				if n, err := strconv.Atoi(m[1]); err == nil {
					allowed[n] = true
				}
			}
			for _, m := range slpPin.FindAllStringSubmatch(line, -1) {
				if n, err := strconv.Atoi(m[1]); err == nil {
					uncondPins[n] = true
				}
			}
		}
		opens, closes := strings.Count(line, "{"), strings.Count(line, "}")
		for i := 0; i < opens; i++ {
			branchStack = append(branchStack, onelineBranch)
		}
		for i := 0; i < closes; i++ {
			if len(branchStack) > 0 {
				branchStack = branchStack[:len(branchStack)-1]
			}
		}
	}
	return allowed, uncondPins
}

func slpSorted(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// auditStatusLaneProducers — весь разбор одним входом, чтобы инъекция гоняла ТУ
// ЖЕ функцию, а не свою копию логики.
func auditStatusLaneProducers(root string, cols []string, opsLane map[int]bool) ([]slpFinding, slpCensus, error) {
	var findings []slpFinding
	var cen slpCensus

	for _, rel := range cols {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, cen, fmt.Errorf("чтение %s: %w", rel, err)
		}
		var col nmCollection
		if err := json.Unmarshal(b, &col); err != nil {
			return nil, cen, fmt.Errorf("разбор %s: %w", rel, err)
		}
		cen.collections++

		var walk func(items []nmItem, path []string)
		walk = func(items []nmItem, path []string) {
			for _, it := range items {
				if it.isFolder() {
					walk(it.Item, append(path, it.Name))
					continue
				}
				cen.steps++
				var lines []string
				for _, ev := range it.Event {
					if ev.Listen == "test" {
						lines = append(lines, ev.Script.Exec...)
					}
				}
				if len(lines) == 0 {
					continue
				}
				allowed, pins := slpStepAssertions(lines)
				if len(allowed) > 0 {
					cen.withAllowance++
				}
				if len(pins) > 0 {
					cen.withUnconditionalPin++
				}
				isOps := slpOpsURL.MatchString(it.rawURL())
				if isOps {
					cen.opsLaneSteps++
					if len(allowed) > 0 {
						cen.opsLaneStepsWithAllowed++
					}
				}

				// Подкласс A: допуск шире собственного безусловного пина.
				if len(allowed) > 0 && len(pins) == 1 {
					g := slpSorted(pins)[0]
					want := runtime.HTTPStatusFromCode(codes.Code(g)) // #nosec G115 -- значение из литерала пробы
					var extra []int
					for _, s := range slpSorted(allowed) {
						if s == want {
							continue
						}
						if _, ok := slpEdgeOwnStatuses[s]; ok {
							continue // край отдаёт сам, до бэкенда — пин к нему не относится
						}
						extra = append(extra, s)
					}
					if len(extra) > 0 {
						findings = append(findings, slpFinding{
							collection: rel, casePath: strings.Join(path, " / "), step: it.Name,
							allowed: slpSorted(allowed),
							why: fmt.Sprintf(
								"шаг БЕЗУСЛОВНО пинит `code == %d`, а при этом коде край отдаёт ровно HTTP %d "+
									"(`runtime.HTTPStatusFromCode`). Статусы %v недостижимы СОВМЕСТНО с этим пином: "+
									"допуск внутренне противоречив и на них не краснеет никогда. "+
									"Либо сузь допуск до %d, либо переведи пин в ветку — если полос и правда несколько",
								g, want, extra, want),
						})
					}
				}

				// Подкласс B: полоса операций.
				if isOps && len(allowed) > 0 {
					var extra []int
					for _, s := range slpSorted(allowed) {
						if opsLane[s] {
							continue
						}
						if _, ok := slpEdgeOwnStatuses[s]; ok {
							continue
						}
						if _, ok := slpOpsLaneInfraStatuses[s]; ok {
							continue
						}
						extra = append(extra, s)
					}
					if len(extra) > 0 {
						findings = append(findings, slpFinding{
							collection: rel, casePath: strings.Join(path, " / "), step: it.Name,
							allowed: slpSorted(allowed),
							why: fmt.Sprintf(
								"шаг идёт по полосе операций, а статусы %v она не производит: её код живёт в "+
									"`pkg/operations` и `gateway/internal/opsproxy`, и ни один тамошний код gRPC "+
									"не отображается в них. Допуск на исход без производителя не краснеет никогда",
								extra),
						})
					}
				}
			}
		}
		walk(col.Item, nil)
	}
	return findings, cen, nil
}

// slpOpsLaneStatuses — HTTP-статусы, производимые арендаторской полосой операций.
// ВЫЧИСЛЯЕТСЯ переписью производителей, а не выписывается: список литералом
// пережил бы первое же изменение полосы и начал бы освобождать пробу, ждущую
// исхода, которого больше нет (или обвинять в исходе, который появился).
//
// СОСТАВ ФАЙЛОВ ПРИХОДИТ ИЗ ИНДЕКСА git, а не с диска. Первая редакция обходила
// каталоги `filepath.WalkDir` — и была права ровно до первой машины, где под
// корнем лежит рабочая копия агента, распаковка отчёта прогона или сборочный
// каталог: тогда перепись производителей включила бы чужие файлы, и множество
// кодов полосы стало бы свойством диска, а не коммита. Поймано гейтом дерева
// `TestTreeWalkersAskTheIndex` — и поймано только ПОСЛЕ `git add`: пока файл
// оставался неотслеживаемым, гейт его не видел, и «локально зелено» означало
// «мой файл ещё не в индексе».
func slpOpsLaneStatuses(root string, goFiles []string) (map[int]bool, map[string]int, error) {
	byName := slpGrpcCodeByName()
	seen := map[string]int{}
	out := map[int]bool{http200: true}
	covered := map[string]bool{}
	for _, rel := range goFiles {
		dir := ""
		for _, d := range slpOpsLaneProducerDirs {
			if strings.HasPrefix(rel, d+string(filepath.Separator)) {
				dir = d
				break
			}
		}
		if dir == "" {
			continue
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		covered[dir] = true
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, nil, fmt.Errorf("чтение %s: %w", rel, err)
		}
		for _, raw := range strings.Split(string(b), "\n") {
			line := slpStripGoComment(raw)
			for _, m := range slpCodeLiteral.FindAllStringSubmatch(line, -1) {
				c, ok := byName[m[1]]
				if !ok {
					continue // `codes.Code` как тип и прочее — не производитель
				}
				seen[m[1]]++
				out[runtime.HTTPStatusFromCode(c)] = true
			}
		}
	}
	// Каталог, который перепись назвала и не нашла в индексе, — это утверждение о
	// дереве, пережившее свой предмет: полоса переехала, а гейт продолжает судить
	// по половине производителей.
	for _, d := range slpOpsLaneProducerDirs {
		if !covered[d] {
			return nil, nil, fmt.Errorf("в индексе git нет ни одного не-тестового .go под %s — "+
				"утверждение о том, где живёт полоса операций, пережило свой предмет", d)
		}
	}
	return out, seen, nil
}

const http200 = 200

// slpGoFilesOf — отслеживаемые .go дерева, отсортированные. Вынесено отдельной
// функцией, чтобы её же звала проба предпосылки: перепись, собранная другим
// способом, доказывала бы свойство своей копии.
func slpGoFilesOf(tt *trackedTree) []string {
	out := make([]string, 0, len(tt.files))
	for rel := range tt.files {
		if strings.HasSuffix(rel, ".go") {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

func TestNoCaseAllowsAStatusItsOwnLaneCannotProduce(t *testing.T) {
	root := repoRoot(t)

	// Предпосылка первая: отображение кода в статус вычисляется библиотекой.
	if runtime.HTTPStatusFromCode(codes.FailedPrecondition) != 400 ||
		runtime.HTTPStatusFromCode(codes.AlreadyExists) != 409 {
		t.Fatalf("отображение кода в статус не то, из которого выведен гейт: "+
			"FAILED_PRECONDITION → %d, ALREADY_EXISTS → %d. Канон — api-conventions.md "+
			"§«gRPC-код → HTTP-статус»; если край завёл свой обработчик ошибок, чинить надо ОБА места",
			runtime.HTTPStatusFromCode(codes.FailedPrecondition),
			runtime.HTTPStatusFromCode(codes.AlreadyExists))
	}

	// Состав дерева — из ИНДЕКСА git: под корнем лежат рабочие копии агентов и
	// распаковки отчётов прогонов, и вердикт по ним был бы свойством чужого
	// рабочего каталога, а не коммита.
	tt := newTrackedTree(t, root)

	opsLane, producers, err := slpOpsLaneStatuses(root, slpGoFilesOf(tt))
	if err != nil {
		t.Fatal(err)
	}
	// Предпосылка вторая: перепись производителей полосы непуста. Пустая означает,
	// что распознаватель ослеп либо полоса переехала, — и тогда «409 не производится»
	// перестаёт быть замером и становится памятью автора.
	if len(producers) == 0 {
		t.Fatal("в производителях полосы операций не найдено НИ ОДНОГО кода gRPC — " +
			"перепись ослепла; чинить надо гейт, а не выходить успехом")
	}
	if opsLane[409] {
		t.Logf("ВНИМАНИЕ: полоса операций теперь производит 409 — предмет подкласса B исчез. " +
			"Если это осознанное изменение, подкласс B пора снимать вместе с ним")
	}

	var cols []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			cols = append(cols, rel)
		}
	}
	sort.Strings(cols)

	findings, cen, err := auditStatusLaneProducers(root, cols, opsLane)
	if err != nil {
		t.Fatal(err)
	}

	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать")
	}
	if cen.steps == 0 {
		t.Fatalf("прочитано коллекций %d, шагов 0 — обход не узнал ни одного шага", cen.collections)
	}
	if cen.withAllowance == 0 {
		t.Fatalf("в %d шагах не найдено НИ ОДНОГО утверждения о коде ответа — "+
			"распознаватель допуска ослеп", cen.steps)
	}
	if cen.opsLaneSteps == 0 {
		t.Fatalf("в %d шагах не найдено НИ ОДНОГО шага полосы операций — "+
			"подкласс B ничего не осматривает", cen.steps)
	}

	names := make([]string, 0, len(producers))
	for n := range producers {
		names = append(names, n)
	}
	sort.Strings(names)
	t.Logf("осмотрено: коллекций %d, шагов %d; из них с допуском по коду ответа %d, "+
		"с БЕЗУСЛОВНЫМ пином кода %d (подкласс A судит только их); "+
		"шагов полосы операций %d, из них с допуском %d (подкласс B). "+
		"Производимые полосой статусы %v — вычислены переписью кодов %v в %v",
		cen.collections, cen.steps, cen.withAllowance, cen.withUnconditionalPin,
		cen.opsLaneSteps, cen.opsLaneStepsWithAllowed,
		slpSorted(opsLane), names, slpOpsLaneProducerDirs)

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "допусков, перечисляющих исход без производителя на своей полосе: %d\n\n", len(findings))
		b.WriteString("Такой допуск НЕ КРАСНЕЕТ НИКОГДА: ветка, которую он разрешает, недостижима,\n")
		b.WriteString("поэтому кейс утверждает меньше, чем выглядит (e2e-flow.md §3, класс «412»).\n")
		b.WriteString("Чинится в cases/*.py набора сужением допуска до состава исходов, которые\n")
		b.WriteString("полоса действительно производит, после чего коллекции перегенерируются\n")
		b.WriteString("scripts/gen.py.\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}
