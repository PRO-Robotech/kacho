// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// peerlanecanon_test.go — два гейта на форму отказа полосы peer-validate.
//
// # Предмет
//
// Ответ на «чужой идентификатор не резолвится у владельца» состоит из трёх
// половин: КОД выбирает полосу резолва, МАШИННЫЙ ПРИЗНАК в details называет ту
// же полосу для клиента, ТЕКСТ не добавляет к сказанному ничего нового. Разъехаться
// они могут независимо, и разъезжались: на общем ребре к владельцу Geography
// четыре сервиса отвечали одним кодом, один другим, а два — обоими на разных
// ресурсах при дословно одном тексте.
//
// # Почему гейт, а не разовая правка
//
// Смена кода на этой полосе НЕ меняет HTTP-статус: по таблице
// `api-conventions.md` §«gRPC-код → HTTP» и INVALID_ARGUMENT, и
// FAILED_PRECONDITION дают 400. Значит ни REST-клиент, ни e2e-утверждение о
// статусе перехода не заметят — расхождение невидимо ровно там, где его стали бы
// искать. Единственное, что способно его удержать, — проверка на уровне Go.
//
// # Канон задаётся ПАРАМЕТРОМ
//
// Решение «промах peer-валидации — FAILED_PRECONDITION» принято владельцем
// (§9 п.1 приёмки XC-6) и записано здесь ОДНОЙ величиной. Передумать — правка
// одной строки в гейте и одной в объявлении полосы, а не тринадцати мест в
// сервисах. Вшитая константа в каждом сервисе стоила бы тринадцати правок и
// разъехалась бы на первой же.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: строка «unknown zone
// id» встречается в комментариях, объясняющих эту же полосу, и текстовый поиск
// принял бы объяснение за эмиссию — ровно тот класс, который гейт ловит.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	kerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"github.com/PRO-Robotech/kacho/pkg/peer"
)

// canonPeerMissLane — КАНОН полосы «чужой ресурс не резолвится у владельца».
// Это и есть параметр гейта: решение владельца §9 п.1, записанное величиной.
// Смена решения — правка ЭТИХ ДВУХ строк, после чего гейт сам назовёт все места,
// которые перестали ему соответствовать.
var (
	canonPeerMissLane = kerrors.ReasonPeerResourceMissing
	canonPeerMissName = "ReasonPeerResourceMissing"
)

// geoLaneTexts — контракт-тексты полосы к владельцу Geography. Это ОБЪЁМ гейта,
// а не список исключений: текст, которого здесь нет, гейтом не покрыт, поэтому у
// каждой записи проверяется предмет (TestGeoLaneTextsHaveSubject) — запись,
// которой больше нечего распознавать, создаёт впечатление покрытия, которого нет.
var geoLaneTexts = map[string]string{
	"unknown zone id":     "форма vpc/storage: зона названа корректно, у владельца не резолвится",
	"unknown region id":   "форма vpc/storage: регион названа корректно, у владельца не резолвится",
	"Zone %s not found":   "форма compute: контракт-тон отсутствия применён к ЧУЖОЙ зоне",
	"Region %s not found": "форма nlb: контракт-тон отсутствия применён к ЧУЖОМУ региону",
	"region %s not found": "форма registry: то же, со строчной буквы",
}

// absenceTone — контракт-тон «ресурса нет». Принадлежит own-полосе прямого
// чтения; на полосе, которая ЕГО ЖЕ и переиспользует для чужого ресурса, он
// допустим (так пишут registry и nlb), но НИКОГДА не вместе с кодом
// синтаксической полосы: тогда код утверждает «ввод неверен», а текст —
// «ресурса нет», и клиент не может знать, чему верить.
const absenceTone = "not found"

// codesInvalidArgument — код синтаксической полосы. Назван здесь, чтобы гейт не
// тянул grpc/codes ради одного сравнения.
const codesInvalidArgument = 3

// lanesByName — соответствие «идентификатор объявления → полоса». Список
// рукописный намеренно: он ЗАМЫКАЕТСЯ проверкой
// TestLanesByNameCoversTheWholeDictionary, поэтому забытая полоса роняет гейт, а
// не тихо выпадает из его объёма. Выводить имя из токена разбором строки было бы
// дешевле и неверно: деривация молча вернула бы пустое имя на полосе, названной
// иначе, и гейт перестал бы её видеть, оставаясь зелёным.
var lanesByName = map[string]kerrors.Reason{
	"ReasonInvalidResourceID":   kerrors.ReasonInvalidResourceID,
	"ReasonResourceNotFound":    kerrors.ReasonResourceNotFound,
	"ReasonPeerResourceMissing": kerrors.ReasonPeerResourceMissing,
	"ReasonPeerResourceState":   kerrors.ReasonPeerResourceState,
	"ReasonPeerUnavailable":     kerrors.ReasonPeerUnavailable,
}

// proseFieldLanes — соответствие «поле peer.Prose → имя полосы». Оно НЕ
// изобретается здесь: то же соответствие реализует носитель, и
// TestProseFieldsMatchTheCarrier сверяет их между собой. Два экземпляра одного
// соответствия разошлись бы молча — и разошлись бы ровно на полосе, добавленной
// позже в одно из двух мест.
var proseFieldLanes = map[string]string{
	"Missing":     "ReasonPeerResourceMissing",
	"State":       "ReasonPeerResourceState",
	"Unavailable": "ReasonPeerUnavailable",
}

// Соответствие полей носителю — сверкой С НИМ, а не прочтением его кода.
func TestProseFieldsMatchTheCarrier(t *testing.T) {
	byOutcome := map[string]peer.Outcome{
		"Missing":     peer.OutcomeMissing,
		"State":       peer.OutcomeStateRefused,
		"Unavailable": peer.OutcomeUnavailable,
	}
	for field, wantName := range proseFieldLanes {
		o, ok := byOutcome[field]
		if !ok {
			t.Errorf("поле %s объявлено гейтом, но исхода для него нет — соответствие\n"+
				"    описывает форму, которой носитель не производит", field)
			continue
		}
		want, ok := laneByName(wantName)
		if !ok {
			t.Errorf("полоса %s не найдена в словаре", wantName)
			continue
		}
		if got := o.Reason(); got.Token() != want.Token() {
			t.Errorf("поле %s: гейт ждёт полосу %q, носитель отдаёт %q — два места об одном\n"+
				"    предмете разошлись", field, want.Token(), got.Token())
		}
	}
	t.Logf("перепись: полей прозы сверено %d", len(proseFieldLanes))
}

func laneByName(name string) (kerrors.Reason, bool) {
	r, ok := lanesByName[name]
	return r, ok
}

// Соответствие имён обязано покрывать словарь ЦЕЛИКОМ. Иначе шестая полоса,
// заведённая в фундаменте, для гейта осталась бы безымянной — и он молчал бы
// именно о ней.
func TestLanesByNameCoversTheWholeDictionary(t *testing.T) {
	declared := map[string]bool{}
	for _, r := range kerrors.AllReasons() {
		declared[r.Token()] = true
	}
	mapped := map[string]bool{}
	for name, r := range lanesByName {
		if !declared[r.Token()] {
			t.Errorf("имя %s указывает на полосу %q, которой нет в словаре фундамента", name, r.Token())
		}
		mapped[r.Token()] = true
	}
	for tok := range declared {
		if !mapped[tok] {
			t.Errorf("полоса %q объявлена в фундаменте, но у гейта для неё нет имени —\n"+
				"    значит её эмиссии гейт не судит и молчит именно о ней", tok)
		}
	}
	t.Logf("перепись: полос в словаре %d, имён у гейта %d", len(declared), len(lanesByName))
}

type laneSite struct {
	file     string
	line     int
	lane     string // имя полосы, если эмиссия идёт через закрытый тип
	text     string // строковый литерал сообщения
	viaType  bool
	atOwner  bool   // место лежит в сервисе-владельце названного ресурса
	sentinel string // идентификатор sentinel'а, обёрнутого в этом же вызове
}

// geoOwnerDir — каталог сервиса, ВЛАДЕЮЩЕГО Region/Zone.
//
// Он здесь не ради исключения, а ради второй половины утверждения. Один и тот же
// текст «Region <id> not found» означает РАЗНОЕ по разные стороны ребра: у
// владельца это своя полоса прямого чтения (своя БД, своя строка, канон —
// NOT_FOUND), у консумера — полоса peer-validate (чужой ресурс, канон —
// FAILED_PRECONDITION). Гейт, судивший бы обе стороны одним правилом, ловил бы
// ФОРМУ текста, а не существо полосы, и первый же законный близнец его отключил
// бы как ложно-срабатывающий.
//
// Поэтому обе стороны утверждаются, и ни одна не пропускается молча.
const geoOwnerDir = "services/geo/"

// notFoundSentinel — часть имени sentinel'а собственной полосы отсутствия.
const notFoundSentinel = "ErrNotFound"

// collectLaneSites обходит прод-дерево и собирает места, где эмитируется текст
// полосы geo либо вызывается конструктор закрытого типа.
func collectLaneSites(t *testing.T, roots []string) (sites []laneSite, filesRead int) {
	t.Helper()
	root := repoRoot(t)

	for _, sub := range roots {
		dir := filepath.Join(root, sub)
		err := rootedWalk(dir, func(rel string) bool {
			return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
		}, func(abs string, body []byte) error {
			filesRead++
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, abs, body, 0)
			if perr != nil {
				t.Fatalf("%s: разбор не удался: %v — гейт не вправе засчитать "+
					"непрочитанный файл в перепись", abs, perr)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				// Эмиссия через НОСИТЕЛЬ (pkg/peer): текст полосы стоит полем
				// `peer.Prose`, а сама полоса выбирается классификацией. Место
				// собирается по ИМЕНИ ПОЛЯ — оно и связывает текст с полосой.
				//
				// Без этой ветки гейт перестал бы видеть эмиссии, переехавшие на
				// носитель, и «ноль находок» означало бы «ноль осмотренного»
				// ровно там, где полоса теперь и живёт.
				if lit, ok := n.(*ast.CompositeLit); ok {
					if selT, ok := lit.Type.(*ast.SelectorExpr); ok && selT.Sel.Name == "Prose" {
						pos := fset.Position(lit.Pos())
						rel := relTo(root, pos.Filename)
						for _, el := range lit.Elts {
							kv, ok := el.(*ast.KeyValueExpr)
							if !ok {
								continue
							}
							key, ok := kv.Key.(*ast.Ident)
							if !ok {
								continue
							}
							lane, known := proseFieldLanes[key.Name]
							if !known {
								continue
							}
							bl, ok := kv.Value.(*ast.BasicLit)
							if !ok || bl.Kind != token.STRING {
								continue
							}
							txt, uerr := strconv.Unquote(bl.Value)
							if uerr != nil {
								continue
							}
							sites = append(sites, laneSite{
								file: rel, line: fset.Position(kv.Pos()).Line,
								lane: lane, text: txt, viaType: true,
								atOwner: strings.HasPrefix(filepath.ToSlash(rel), geoOwnerDir),
							})
						}
					}
					return true
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				lane := laneOfConstructor(sel)
				lit := firstStringLit(call)
				if lane == "" && !mentionsGeoLaneText(lit) {
					return true
				}
				if lane == "" && !isStatusBuilder(sel) {
					return true
				}
				pos := fset.Position(call.Pos())
				rel := relTo(root, pos.Filename)
				sites = append(sites, laneSite{
					file: rel, line: pos.Line,
					lane: lane, text: lit, viaType: lane != "",
					atOwner:  strings.HasPrefix(filepath.ToSlash(rel), geoOwnerDir),
					sentinel: wrappedSentinel(call),
				})
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", dir, err)
		}
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].line < sites[j].line
	})
	return sites, filesRead
}

// laneOfConstructor — имя полосы, если вызов есть конструктор закрытого типа
// (`<что-то>.Reason<Полоса>.Errf(...)`). Пусто — если это не он.
func laneOfConstructor(sel *ast.SelectorExpr) string {
	if sel.Sel.Name != "Errf" {
		return ""
	}
	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok || !strings.HasPrefix(inner.Sel.Name, "Reason") {
		return ""
	}
	return inner.Sel.Name
}

// isStatusBuilder — самодельная сборка статуса (`status.Error`/`status.Errorf`)
// либо обёртка sentinel'а (`fmt.Errorf`). Именно они обходят закрытый тип.
func isStatusBuilder(sel *ast.SelectorExpr) bool {
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch pkg.Name + "." + sel.Sel.Name {
	case "status.Error", "status.Errorf", "fmt.Errorf":
		return true
	}
	return false
}

// wrappedSentinel — имя идентификатора-ошибки, обёрнутого в этом же вызове
// (`fmt.Errorf("%w: …", pkg.ErrNotFound, …)`). Пусто, если такого нет.
func wrappedSentinel(call *ast.CallExpr) string {
	for _, a := range call.Args {
		switch v := a.(type) {
		case *ast.SelectorExpr:
			return v.Sel.Name
		case *ast.Ident:
			if strings.HasPrefix(v.Name, "Err") {
				return v.Name
			}
		}
	}
	return ""
}

func firstStringLit(call *ast.CallExpr) string {
	for _, a := range call.Args {
		if bl, ok := a.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			if v, err := strconv.Unquote(bl.Value); err == nil {
				return v
			}
		}
	}
	return ""
}

func mentionsGeoLaneText(lit string) bool {
	for txt := range geoLaneTexts {
		if strings.Contains(lit, txt) {
			return true
		}
	}
	return false
}

var prodRoots = []string{"pkg", "services", "gateway"}

// ГЕЙТ 1 — полоса к владельцу Geography собирается ЗАКРЫТЫМ ТИПОМ у КОНСУМЕРА,
// и остаётся полосой прямого чтения У ВЛАДЕЛЬЦА.
//
// Утверждаются ОБЕ стороны ребра, потому что текст у них общий, а полоса —
// разная. Гейт, проверяющий только консумеров, молчал бы о владельце, начавшем
// отвечать чужой полосой о своей же строке; гейт, судящий обе стороны одним
// правилом, краснел бы на законном близнеце и был бы снят первым же обходом.
//
// Самодельная сборка статуса у консумера запрещена не из вкуса: код, взятый у
// полосы, не может разойтись с её машинным признаком, а выписанный руками —
// расходится, и расходится молча (HTTP-статус тот же).
func TestGeoPeerLaneIsBuiltByTheClosedType(t *testing.T) {
	sites, filesRead := collectLaneSites(t, prodRoots)

	var handRolled, viaType, ownerSide int
	for _, s := range sites {
		if s.atOwner {
			ownerSide++
			// Владелец отвечает о СВОЕЙ строке — полоса прямого чтения. Она
			// обязана быть выражена собственным sentinel'ом отсутствия, а не
			// полосой peer-validate: иначе владелец сообщал бы о своей строке
			// «предусловие на чужой ресурс не выполнено».
			if s.viaType {
				t.Errorf("%s:%d — ВЛАДЕЛЕЦ ресурса отвечает полосой peer-validate (%s).\n"+
					"    О собственной строке своей БД владелец отвечает полосой прямого чтения\n"+
					"    (канон — NOT_FOUND). Полоса peer-validate говорит о ЧУЖОМ ресурсе.",
					s.file, s.line, s.lane)
				continue
			}
			if !strings.Contains(s.sentinel, notFoundSentinel) {
				t.Errorf("%s:%d — у владельца текст %q не обёрнут sentinel'ом отсутствия\n"+
					"    (ожидалось имя, содержащее %q; найдено %q). Полоса прямого чтения\n"+
					"    перестала быть выраженной, и ответ владельца о своей строке стал\n"+
					"    зависеть от того, куда эта ошибка попадёт дальше.",
					s.file, s.line, s.text, notFoundSentinel, s.sentinel)
			}
			continue
		}
		if s.viaType {
			viaType++
			continue
		}
		handRolled++
		t.Errorf("%s:%d — текст полосы peer-validate (%q) собран в обход закрытого типа.\n"+
			"    Полоса обязана эмитироваться конструктором pkg/errors (%s.Errf): тогда код и\n"+
			"    машинный признак берутся у полосы и разойтись не могут. Собранный руками статус\n"+
			"    расходится с признаком МОЛЧА — HTTP-статус у обоих кодов один и тот же (400),\n"+
			"    поэтому ни REST-клиент, ни e2e перехода не заметят.",
			s.file, s.line, s.text, canonPeerMissName)
	}

	if viaType == 0 {
		t.Fatalf("предмет гейта не найден: ни одной эмиссии через закрытый тип в %v.\n"+
			"    Прочитано файлов: %d. «Ноль находок» здесь означало бы «ноль прочитанного» —\n"+
			"    гейт обязан отличать одно от другого.", prodRoots, filesRead)
	}
	if ownerSide == 0 {
		t.Fatalf("вторая половина утверждения осталась без предмета: мест на стороне владельца\n"+
			"    (%s) не найдено. Проверка законного близнеца перестала что-либо проверять —\n"+
			"    значит гейт больше не отличает форму текста от существа полосы.", geoOwnerDir)
	}
	t.Logf("перепись: прод-файлов прочитано %d, мест полосы найдено %d "+
		"(консумер через закрытый тип %d, консумер в обход %d, сторона владельца %d)",
		filesRead, len(sites), viaType, handRolled, ownerSide)
}

// ГЕЙТ 2 — код полосы не противоречит её тексту.
//
// Контракт-тон отсутствия ресурса («… not found») допустим и на полосе
// peer-validate: так пишут registry и nlb, и это осознанно — клиенту сообщают,
// что названного ресурса у владельца нет. Недопустимо другое: тот же тон вместе
// с кодом СИНТАКСИЧЕСКОЙ полосы, где код утверждает «ввод неверен». Тогда две
// половины одного ответа говорят разное, и клиент не может знать, чему верить.
//
// Гейт смотрит на эмиссию через закрытый тип, то есть на код, КОТОРОГО ЕЩЁ НЕТ:
// сегодняшние места он проходит, а следующего, кто соберёт такую пару, назовёт.
func TestLaneCodeDoesNotContradictItsText(t *testing.T) {
	sites, filesRead := collectLaneSites(t, prodRoots)

	var checked int
	for _, s := range sites {
		if !s.viaType {
			continue
		}
		checked++
		lane, ok := laneByName(s.lane)
		if !ok {
			t.Errorf("%s:%d — полоса %s не найдена в словаре pkg/errors. Гейт судит по\n"+
				"    объявленному словарю; полоса вне его либо опечатка, либо контракт,\n"+
				"    который забыли объявить.", s.file, s.line, s.lane)
			continue
		}
		if lane.Code() == codesInvalidArgument && strings.Contains(s.text, absenceTone) {
			t.Errorf("%s:%d — код полосы %s утверждает «ввод неверен», а текст (%q) —\n"+
				"    контракт-тоном, что РЕСУРСА НЕТ. Две половины одного ответа говорят разное.\n"+
				"    Либо полоса выбрана неверно (промах у владельца — это %s), либо текст\n"+
				"    принадлежит другой полосе.", s.file, s.line, s.lane, s.text, canonPeerMissName)
		}
	}

	if checked == 0 {
		t.Fatalf("предмет гейта не найден: ни одной эмиссии полосы в %v (прочитано файлов %d)",
			prodRoots, filesRead)
	}
	t.Logf("перепись: эмиссий полосы осмотрено %d, прод-файлов прочитано %d", checked, filesRead)
}

// canonPeerMissLane обязан совпадать с решением владельца. Утверждение стоит
// ОТДЕЛЬНО, потому что это не свойство реализации, а зафиксированное решение:
// правка объявления полосы без правки этой строки обязана покраснеть здесь и
// назвать себя, а не разъехаться молча.
func TestCanonMatchesOwnerDecision(t *testing.T) {
	if got := canonPeerMissLane.Code().String(); got != "FailedPrecondition" {
		t.Fatalf("канон полосы промаха разошёлся с решением владельца §9 п.1 приёмки XC-6:\n"+
			"    объявление pkg/errors говорит %s, решение — FailedPrecondition.\n"+
			"    Если канон меняли осознанно — правьте ЭТУ строку тем же коммитом.", got)
	}
	if canonPeerMissLane.Token() != "PEER_RESOURCE_MISSING" {
		t.Fatalf("токен полосы промаха разошёлся с контрактом XC-1 D2: %q", canonPeerMissLane.Token())
	}
}

// Каждая запись словаря текстов обязана иметь предмет в дереве. Запись, которой
// нечего распознавать, создаёт впечатление покрытия, которого нет, — и её
// унаследует следующая слепая зона.
func TestGeoLaneTextsHaveSubject(t *testing.T) {
	sites, filesRead := collectLaneSites(t, prodRoots)

	for txt, why := range geoLaneTexts {
		found := false
		for _, s := range sites {
			if strings.Contains(s.text, txt) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("запись словаря %q (%s) больше нечего распознавать — предмета в дереве нет.\n"+
				"    Такая запись не безобидна: она создаёт впечатление покрытия, которого нет.\n"+
				"    Либо текст переименован (обновите запись), либо полоса снята (уберите её).",
				txt, why)
		}
	}
	t.Logf("перепись: записей словаря %d, мест полосы %d, прод-файлов прочитано %d",
		len(geoLaneTexts), len(sites), filesRead)
}
