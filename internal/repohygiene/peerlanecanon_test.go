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
// # У владельца судится ТОКЕН, а не форма эмиссии
//
// Закрытый тип выражает ОБЕ стороны ребра: и «я не нашёл СВОЁ», и «предусловие
// на ЧУЖОЙ ресурс не выполнено». Поэтому сам факт его использования у владельца
// не говорит ничего — сторону называет только токен, и гейт судит по нему.
//
// Прежняя редакция судила по форме, и это было верно ОТНОСИТЕЛЬНО ПОПУЛЯЦИИ, на
// которой писалось: закрытого типа для полосы прямого чтения тогда в дереве не
// было, и «владелец воспользовался закрытым типом» действительно означало
// «владелец назвал чужую полосу». Как только полоса прямого чтения обрела
// машинный признак, гейт покраснел на коде, который конвенции прямо
// предписывают (`api-conventions.md` §By-lane code-split: geo Region/Zone.Get —
// direct → RESOURCE_NOT_FOUND/NOT_FOUND). Узкая популяция предпосылку не
// подтверждает — она её СКРЫВАЕТ.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: строка «unknown zone
// id» встречается в комментариях, объясняющих эту же полосу, и текстовый поиск
// принял бы объяснение за эмиссию — ровно тот класс, который гейт ловит.
package repohygiene

import (
	"fmt"
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

// laneSide — О ЧЬЁМ ресурсе полоса делает утверждение. Это и есть то, чем
// различаются стороны ребра, и различие НЕ выводится из факта использования
// закрытого типа: один и тот же тип выражает обе стороны, а называет сторону
// только токен.
type laneSide uint8

const (
	// sideUnset — сторона не назначена. Нулевое значение намеренно НЕ является
	// законной стороной: полоса, которой забыли назначить сторону, обязана
	// покраснеть, а не пройти наравне с own-полосой.
	sideUnset laneSide = iota

	// sideOwn — утверждение о СВОЁМ: формат own-id и промах прямого чтения по
	// своей БД. Законна у владельца ресурса.
	sideOwn

	// sidePeer — утверждение о ЧУЖОМ ресурсе у его владельца. У владельца
	// собственного ресурса незаконна: о своей строке нельзя сказать
	// «предусловие на чужой ресурс не выполнено».
	sidePeer
)

// laneFacts — что гейт знает о полосе: сама полоса и сторона, о которой она
// утверждает.
type laneFacts struct {
	reason kerrors.Reason
	side   laneSide
}

// lanesByName — соответствие «идентификатор объявления → полоса и её сторона».
// Список рукописный намеренно: он ЗАМЫКАЕТСЯ с двух концов —
// TestLanesByNameCoversTheWholeDictionary требует имени для каждой полосы
// словаря, TestLaneSidesMatchTheCarrier сверяет назначенную сторону С
// НОСИТЕЛЕМ. Поэтому забытая полоса роняет гейт, а не тихо выпадает из его
// объёма. Выводить имя или сторону из токена разбором строки было бы дешевле и
// неверно: деривация молча вернула бы пустое значение на полосе, названной
// иначе, и гейт перестал бы её видеть, оставаясь зелёным.
var lanesByName = map[string]laneFacts{
	"ReasonInvalidResourceID":   {kerrors.ReasonInvalidResourceID, sideOwn},
	"ReasonResourceNotFound":    {kerrors.ReasonResourceNotFound, sideOwn},
	"ReasonPeerResourceMissing": {kerrors.ReasonPeerResourceMissing, sidePeer},
	"ReasonPeerResourceState":   {kerrors.ReasonPeerResourceState, sidePeer},
	"ReasonPeerUnavailable":     {kerrors.ReasonPeerUnavailable, sidePeer},
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
	f, ok := lanesByName[name]
	return f.reason, ok
}

// laneSideByName — сторона, о которой утверждает полоса с этим именем. Второе
// значение ложно и для незнакомого имени, и для полосы без назначенной стороны:
// оба случая означают «судить нечем», и гейт обязан сказать это вслух, а не
// пропустить место молча.
func laneSideByName(name string) (laneSide, bool) {
	f, ok := lanesByName[name]
	if !ok || f.side == sideUnset {
		return sideUnset, false
	}
	return f.side, true
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
	for name, f := range lanesByName {
		if !declared[f.reason.Token()] {
			t.Errorf("имя %s указывает на полосу %q, которой нет в словаре фундамента", name, f.reason.Token())
		}
		mapped[f.reason.Token()] = true
	}
	for tok := range declared {
		if !mapped[tok] {
			t.Errorf("полоса %q объявлена в фундаменте, но у гейта для неё нет имени —\n"+
				"    значит её эмиссии гейт не судит и молчит именно о ней", tok)
		}
	}
	t.Logf("перепись: полос в словаре %d, имён у гейта %d", len(declared), len(lanesByName))
}

// Сторона полосы — сверкой С НОСИТЕЛЕМ, а не объявлением по памяти.
//
// Назначение стороны — решение, и оно записано здесь рукой. Рукописное решение
// разъезжается с деревом молча, поэтому у него есть авторитет: pkg/peer —
// ЕДИНСТВЕННОЕ место, где ответ соседа превращается в полосу, значит набор
// полос, которые он производит, И ЕСТЬ набор полос peer-validate. Сверка идёт в
// обе стороны: полоса, названная гейтом peer-стороной, обязана производиться
// носителем; полоса, названная own-стороной, — не обязана и не вправе.
//
// Без зеркала проверка была бы односторонней и зеленела бы на подмене: сделай
// кто-нибудь исход промаха полосой прямого чтения, владелец Geography стал бы
// отвечать о ЧУЖОМ ресурсе своей полосой — и гейт молчал бы, потому что искал
// только недостающее.
func TestLaneSidesMatchTheCarrier(t *testing.T) {
	produced := map[string]bool{}
	for _, o := range peer.AllOutcomes() {
		r := o.Reason()
		if !r.IsDeclared() {
			continue
		}
		produced[r.Token()] = true
	}
	if len(produced) == 0 {
		t.Fatalf("носитель не производит ни одной полосы — сверять нечего.\n"+
			"    «Ноль расхождений» здесь означало бы «ноль осмотренного»: исходов у носителя %d",
			len(peer.AllOutcomes()))
	}

	var ownNamed, peerNamed int
	namedPeer := map[string]bool{}
	for name, f := range lanesByName {
		switch f.side {
		case sidePeer:
			peerNamed++
			namedPeer[f.reason.Token()] = true
			if !produced[f.reason.Token()] {
				t.Errorf("гейт назвал %s полосой peer-validate, но носитель её НЕ производит.\n"+
					"    Либо сторона назначена неверно, либо у полосы не осталось производителя —\n"+
					"    и тогда запрет на неё у владельца охраняет то, чего не бывает.", name)
			}
		case sideOwn:
			ownNamed++
			if produced[f.reason.Token()] {
				t.Errorf("гейт назвал %s own-полосой, а носитель производит её как ответ о ЧУЖОМ\n"+
					"    ресурсе. Тогда владелец вправе эмитировать её о своей строке — и гейт\n"+
					"    промолчит ровно о том, ради чего заведён.", name)
			}
		default:
			t.Errorf("полосе %s не назначена сторона. Гейт судит владельца ПО СТОРОНЕ, поэтому\n"+
				"    полоса без неё не судится вовсе — и молчание о ней неотличимо от согласия.", name)
		}
	}
	for tok := range produced {
		if !namedPeer[tok] {
			t.Errorf("носитель производит полосу %q, но гейт не называет её peer-стороной —\n"+
				"    значит владелец вправе ответить ею о своей строке, и гейт не возразит", tok)
		}
	}
	t.Logf("перепись: полос у гейта %d (own %d, peer %d), носитель производит %d",
		len(lanesByName), ownNamed, peerNamed, len(produced))
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
//
// У владельца own-полоса законна В ДВУХ формах: закрытым типом (тогда токен
// уезжает клиенту машинным признаком) и обёрткой sentinel'а отсутствия (тогда
// полосу называет граница сервиса). Различает их не форма записи, а токен, —
// и гейт обязан различать так же.
const geoOwnerDir = "services/geo/"

// notFoundSentinel — часть имени sentinel'а собственной полосы отсутствия.
const notFoundSentinel = "ErrNotFound"

// collectGeoOwnerAdapters — адаптеры к соседям внутри каталога владельца.
//
// Вынесено отдельной функцией, чтобы инъекция гоняла ТО ЖЕ САМОЕ, что гоняется
// по дереву, а не свою копию правила: копия разошлась бы с оригиналом молча.
//
// Признак адаптера берётся у СОСЕДНЕГО гейта (peerClientDir), а не выписывается
// здесь второй раз. Раскладка слоёв в этом продукте нормативна
// (`architecture.md`): адаптер к соседу лежит в `internal/clients/`, и другого
// дома у него нет.
func collectGeoOwnerAdapters(t *testing.T, root string) (adapters []string, filesRead int) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(geoOwnerDir))
	base := filepath.ToSlash(dir) + "/"

	err := rootedWalk(dir, func(rel string) bool {
		return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
	}, func(abs string, _ []byte) error {
		filesRead++
		rel := strings.TrimPrefix(filepath.ToSlash(abs), base)
		if strings.Contains(rel, peerClientDir) {
			adapters = append(adapters, geoOwnerDir+rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v", dir, err)
	}
	sort.Strings(adapters)
	return adapters, filesRead
}

// ПРЕДПОСЫЛКА ГЕЙТА — сторону ребра называет ПУТЬ, и это верно не само по себе.
//
// Утверждение «эмиссия внутри каталога владельца говорит о СВОЁМ ресурсе» верно
// ОТНОСИТЕЛЬНО ПОПУЛЯЦИИ: geo — leaf (`polyrepo.md` §runtime-edges), чужих
// ссылок на request-path не держит и адаптера к соседу не имеет, поэтому
// сказать о чужом ресурсе ему попросту нечем. Заведут адаптер — путь перестанет
// различать сторону, и запрет на peer-полосу у владельца станет ложью ровно
// там, где консумер прав.
//
// Предпосылку проверяет ГЕЙТ, а не память, и повод назван прямо: ровно эта
// предпосылка уже рушилась однажды на уровень ниже. «Закрытый тип у владельца»
// означало «чужая полоса» только пока у полосы прямого чтения закрытого типа не
// было; как только он появился, гейт покраснел на коде, который конвенции
// предписывают. Узкая популяция предпосылку не подтверждает — она её СКРЫВАЕТ.
func TestOwnerSideIsDecidedByPathOnlyWhileOwnerIsNoConsumer(t *testing.T) {
	adapters, filesRead := collectGeoOwnerAdapters(t, repoRoot(t))

	if filesRead == 0 {
		t.Fatalf("предпосылка не проверена: в каталоге владельца (%s) не прочитано ни одного\n"+
			"    прод-файла. «Адаптеров ноль» здесь означало бы «осмотрено ноль» — гейт обязан\n"+
			"    отличать одно от другого.", geoOwnerDir)
	}
	for _, a := range adapters {
		t.Errorf("%s — в каталоге владельца появился адаптер к соседу (признак %q).\n"+
			"    ПРЕДПОСЫЛКА ГЕЙТА РУХНУЛА: сторону ребра он выводит из ПУТИ, а путь различал\n"+
			"    стороны ровно пока владелец не был консумером. Теперь эмиссия полосы\n"+
			"    peer-validate внутри этого каталога может быть ЗАКОННОЙ, и запрет на неё — ложь.\n"+
			"    Править надо ДИСКРИМИНАТОР (выводить сторону из места вызова, а не из каталога),\n"+
			"    а не полосу в коде и не текст этого требования.", a, peerClientDir)
	}
	t.Logf("перепись: прод-файлов владельца прочитано %d, адаптеров к соседям %d",
		filesRead, len(adapters))
}

// collectLaneSites обходит прод-дерево и собирает места, где эмитируется текст
// полосы geo либо вызывается конструктор закрытого типа.
func collectLaneSites(t *testing.T, root string, subs []string) (sites []laneSite, filesRead int) {
	t.Helper()

	for _, sub := range subs {
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

// laneVerdict — как гейт судит ОДНО место полосы. Молчание здесь — тоже вердикт
// и называется наравне с прочими: иначе «гейт ничего не сказал» неотличимо от
// «гейт этого места не видел».
type laneVerdict uint8

const (
	// verdictSilent — законная форма.
	verdictSilent laneVerdict = iota

	// verdictOwnerPeerLane — владелец отвечает о СВОЕЙ строке полосой,
	// утверждающей о ЧУЖОМ ресурсе.
	verdictOwnerPeerLane

	// verdictOwnerLaneUnexpressed — у владельца полоса прямого чтения не
	// выражена ничем: ни закрытым типом, ни sentinel'ом отсутствия.
	verdictOwnerLaneUnexpressed

	// verdictOwnerUnknownLane — полоса у владельца не опознана, и судить её
	// сторону нечем.
	verdictOwnerUnknownLane

	// verdictConsumerHandRolled — консумер собрал статус полосы в обход
	// закрытого типа.
	verdictConsumerHandRolled
)

// laneFinding — место, о котором гейт высказывается, вместе с ГОТОВЫМ текстом.
//
// Текст собирается здесь, а не в пробе: инъекция обязана проверять не только
// «покраснел», но и ЧТО НАПЕЧАТАЛ. Находка, называющая симптом вместо причины,
// посылает читателя искать не там — на неё тратят прогон, а потом снимают гейт
// как непонятный.
type laneFinding struct {
	file    string
	line    int
	verdict laneVerdict
	message string
}

// laneTally — перепись по ПОЛОСАМ, а не только по сторонам ребра.
//
// Одно число на сторону скрывает ровно тот случай, ради которого гейт заведён: у
// владельца собственная полоса прямого чтения и чужая полоса peer-validate
// выражаются ОДНИМ закрытым типом и различаются только токеном. Пока сторона
// считалась одним числом, переход владельца на чужую полосу был бы неотличим от
// законного перехода на свою.
type laneTally struct {
	consumerViaType    int
	consumerHandRolled int
	ownerTotal         int
	ownerOwnLane       int
	ownerSentinel      int
	ownerPeerLane      int
	ownerUnknownLane   int
}

// judgeLaneSites — ВЕСЬ вердикт гейта, вынесенный отдельной функцией.
//
// Отдельной — чтобы инъекция гоняла ТО ЖЕ САМОЕ, что гоняется по дереву, а не
// свою копию правила: копия разошлась бы с оригиналом молча и доказывала бы
// способность падать у того, чего в дереве нет.
func judgeLaneSites(sites []laneSite) (findings []laneFinding, tally laneTally) {
	for _, s := range sites {
		if s.atOwner {
			tally.ownerTotal++
			if s.viaType {
				// Закрытый тип выражает ОБЕ стороны ребра, поэтому сам факт его
				// использования у владельца ничего не решает. Решает ТОКЕН: он и
				// есть то, что называет клиенту полосу.
				side, known := laneSideByName(s.lane)
				switch {
				case !known:
					tally.ownerUnknownLane++
					findings = append(findings, laneFinding{
						file: s.file, line: s.line, verdict: verdictOwnerUnknownLane,
						message: fmt.Sprintf("%s:%d — у владельца полоса %s не опознана: её нет в словаре гейта\n"+
							"    либо ей не назначена сторона. Судить сторону нечем, и молчание здесь было бы\n"+
							"    fail-open: неопознанная полоса прошла бы наравне с own-полосой, а она может\n"+
							"    оказаться и peer-validate.",
							s.file, s.line, s.lane),
					})
				case side == sidePeer:
					tally.ownerPeerLane++
					findings = append(findings, laneFinding{
						file: s.file, line: s.line, verdict: verdictOwnerPeerLane,
						message: fmt.Sprintf("%s:%d — ВЛАДЕЛЕЦ ресурса отвечает полосой peer-validate (%s).\n"+
							"    О собственной строке своей БД владелец отвечает own-полосой: формат own-id\n"+
							"    либо промах прямого чтения (канон — NOT_FOUND). Полоса peer-validate\n"+
							"    утверждает «предусловие на ЧУЖОЙ ресурс не выполнено» — о своей строке\n"+
							"    своей БД это неверно.",
							s.file, s.line, s.lane),
					})
				default:
					// own-полоса закрытым типом — законная и предписанная форма:
					// код и машинный признак берутся у полосы и разойтись не могут.
					tally.ownerOwnLane++
				}
				continue
			}
			if !strings.Contains(s.sentinel, notFoundSentinel) {
				findings = append(findings, laneFinding{
					file: s.file, line: s.line, verdict: verdictOwnerLaneUnexpressed,
					message: fmt.Sprintf("%s:%d — у владельца полоса прямого чтения не выражена НИЧЕМ:\n"+
						"    ни own-полосой закрытого типа, ни sentinel'ом отсутствия. Текст %q,\n"+
						"    ожидалось имя, содержащее %q; найдено %q. Ответ владельца о своей строке\n"+
						"    стал зависеть от того, куда эта ошибка попадёт дальше.",
						s.file, s.line, s.text, notFoundSentinel, s.sentinel),
				})
				continue
			}
			tally.ownerSentinel++
			continue
		}
		if s.viaType {
			tally.consumerViaType++
			continue
		}
		tally.consumerHandRolled++
		findings = append(findings, laneFinding{
			file: s.file, line: s.line, verdict: verdictConsumerHandRolled,
			message: fmt.Sprintf("%s:%d — текст полосы peer-validate (%q) собран в обход закрытого типа.\n"+
				"    Полоса обязана эмитироваться конструктором pkg/errors (%s.Errf): тогда код и\n"+
				"    машинный признак берутся у полосы и разойтись не могут. Собранный руками статус\n"+
				"    расходится с признаком МОЛЧА — HTTP-статус у обоих кодов один и тот же (400),\n"+
				"    поэтому ни REST-клиент, ни e2e перехода не заметят.",
				s.file, s.line, s.text, canonPeerMissName),
		})
	}
	return findings, tally
}

// ГЕЙТ 1 — полоса к владельцу Geography собирается ЗАКРЫТЫМ ТИПОМ у КОНСУМЕРА,
// и остаётся OWN-ПОЛОСОЙ У ВЛАДЕЛЬЦА.
//
// Утверждаются ОБЕ стороны ребра, потому что текст у них общий, а полоса —
// разная. Гейт, проверяющий только консумеров, молчал бы о владельце, начавшем
// отвечать чужой полосой о своей же строке; гейт, судящий обе стороны одним
// правилом, краснел бы на законном близнеце и был бы снят первым же обходом.
//
// У владельца судится ТОКЕН, а не форма эмиссии. Закрытый тип выражает обе
// стороны ребра, поэтому «владелец воспользовался закрытым типом» — не признак
// нарушения, а «владелец назвал полосу peer-validate» — признак.
//
// Самодельная сборка статуса у консумера запрещена не из вкуса: код, взятый у
// полосы, не может разойтись с её машинным признаком, а выписанный руками —
// расходится, и расходится молча (HTTP-статус тот же).
func TestGeoPeerLaneIsBuiltByTheClosedType(t *testing.T) {
	sites, filesRead := collectLaneSites(t, repoRoot(t), prodRoots)
	findings, tally := judgeLaneSites(sites)

	for _, f := range findings {
		t.Error(f.message)
	}

	if tally.consumerViaType == 0 {
		t.Fatalf("предмет гейта не найден: ни одной эмиссии через закрытый тип в %v.\n"+
			"    Прочитано файлов: %d. «Ноль находок» здесь означало бы «ноль прочитанного» —\n"+
			"    гейт обязан отличать одно от другого.", prodRoots, filesRead)
	}
	if tally.ownerTotal == 0 {
		t.Fatalf("вторая половина утверждения осталась без предмета: мест на стороне владельца\n"+
			"    (%s) не найдено. Проверка законного близнеца перестала что-либо проверять —\n"+
			"    значит гейт больше не отличает форму текста от существа полосы.", geoOwnerDir)
	}
	t.Logf("перепись: прод-файлов прочитано %d, мест полосы найдено %d "+
		"(консумер через закрытый тип %d, консумер в обход %d, сторона владельца %d: "+
		"own-полосой закрытого типа %d, sentinel'ом отсутствия %d, полосой peer-validate %d, "+
		"полосой вне словаря %d)",
		filesRead, len(sites), tally.consumerViaType, tally.consumerHandRolled, tally.ownerTotal,
		tally.ownerOwnLane, tally.ownerSentinel, tally.ownerPeerLane, tally.ownerUnknownLane)
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
	sites, filesRead := collectLaneSites(t, repoRoot(t), prodRoots)

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
	sites, filesRead := collectLaneSites(t, repoRoot(t), prodRoots)

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
