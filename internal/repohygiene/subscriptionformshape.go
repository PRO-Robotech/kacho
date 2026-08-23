// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionformshape.go — анализатор ФОРМЫ единой подписки.
//
// # Один анализатор, несколько утверждений — и почему именно так
//
// Предмет один: форма общей подписки. Читается она одним разбором контракта, а
// утверждений о ней приёмка требует девять. Разложить их по девяти гейтам
// значило бы девять раз прочитать один файл, девять раз объявить премису и
// девять раз разойтись с ней — при том что расходиться им нельзя: они об одном
// и том же тексте.
//
// # Что он утверждает
//
//	ПЕРЕЧЕНЬ ПОЛЕЙ ЗАКРЫТ    поле запроса вне перечня — находка с именем поля;
//	                         поле, добавленное вместе с записью, — молчание;
//	                         запись, которой в форме нечего исключать, — находка.
//	ПРИЗНАК КАТЕГОРИИ        у каждой ОСИ проставлен признак — «якорная» либо
//	                         «удобства». Гейт требует, чтобы признак СТОЯЛ, и не
//	                         судит, верно ли он выбран: это смысл, а не синтаксис.
//	ОТСУТСТВУЮЩАЯ ОСЬ        ось, объявленная отсутствующей, полем не является, а
//	                         причина её отсутствия названа в контракте.
//	ИСХОД НЕЗАДАННОГО        начало — ветвление; исход НЕЗАДАННОЙ ветви назван и
//	                         называет значение, объявленное этим же контрактом.
//	НОСИТЕЛЬ — ВЫБОР         нагрузка события — ветвление ровно из двух ветвей,
//	                         поэтому пустая нагрузка непредставима by construction.
//	НОСИТЕЛЬ НАЗВАН ТИПОМ    ветвь состояния — именованный тип, а не свободная
//	                         структура: у свободной ключи на проводе производятся
//	                         от имён идентификаторов Go.
//	ЯКОРЬ — ПОЛЕ ОБОЛОЧКИ    авторизуемый якорь стоит полем верхнего уровня
//	                         события, не внутри нагрузки и не внутри ветвления.
//	ГОРИЗОНТ ВЫРАЗИМ         служебное сообщение несёт поля, которыми исход
//	                         «позиция утрачена» вообще может быть сказан.
//	ПЕРИМЕТР                 ни `service`, ни `rpc`; ни одной зависимости от
//	                         другого контракта дерева.
//	ПРИЧИНА ОСТАНОВКИ        событие не обязывает нести исход остановки потока
//	                         полезной нагрузкой.
//	СЛОВАРЬ ВИДОВ            ось видов — строки владельца, а не перечисление,
//	                         объявленное контрактом.
//	ОСЬ НЕ ОБЯЗАТЕЛЬНА       ни одна ось не помечена опцией обязательности:
//	                         незаданная ось не сужает — это её единственное
//	                         значение.
//
// # Чего он НЕ утверждает, и это сказано, а не умолчано
//
// Число ветвей начала он НЕ проверяет. Состояний на проводе четыре — незаданное
// есть и у ветвления, и у перечисления с нулевым значением, — поэтому «ветвей
// три» зелено на форме с молчаливым умолчанием, то есть не измеряет того, ради
// чего написано. Проверяется ИСХОД незаданного, а не счёт ветвей.
//
// А вот «ни одна ось не обязательна» он проверяет — и это правка собственного
// вывода. Первая редакция объявляла проверку вакуумной: в proto3 ключевого слова
// `required` нет by construction. Основание оказалось фольклором: обязательность
// в этом дереве выражается ОПЦИЕЙ поля, и такие опции в контрактах живут. Значит
// пометить ось обязательной можно, а проверка способна упасть.
//
// # Единица счёта: ВИДОВ находок 20, МЕСТ внесения 22 — это разные величины
//
// Два вида вносятся не одной ветвью каждый:
//
//	carrier-not-a-choice    ветвления носителя нет ВОВСЕ | ветвей не две;
//	anchor-not-in-envelope  якоря в оболочке нет ВОВСЕ | якорь ветвью ветвления.
//
// Смешивать «двадцать» и «двадцать два» нельзя, и цена этого измерена: проба,
// утверждающая только ВИД, остаётся зелёной, когда снятую ветвь подхватывает её
// соседка. Сделав первую ветвь носителя недостижимой, прогон инъекции остался
// ПОЛНОСТЬЮ зелёным — у несуществующего ветвления ноль ветвей, ноль не равен
// двум, и находку того же вида выдавала вторая ветвь.
//
// Поэтому обе пары утверждаются в инъекции ТЕКСТОМ (`requireKindSaying`), и
// каждая из четырёх ветвей измерена снятием: краснеет ровно своя подпроба, три
// соседние молчат. У остальных 18 мест вид и место СОВПАДАЮТ — вид вносится
// ровно одним местом, — поэтому там достаточно утверждения вида, и наблюдаемость
// следует из того, что подпроба есть у каждого вида, а не из отдельного замера.
//
// Предикаты пересчёта — по местам внесения, по видам и по покрытию ОТДЕЛЬНО,
// иначе число снова станет двусмысленным. Каждый анкерён НА НАЧАЛО СТРОКИ, и это
// не педантизм: предикат без якоря считает СВОЁ ЖЕ объяснение — три строки ниже
// содержат `add(` текстом, и наивный счёт даёт 25 вместо 22. Проверено на этом
// самом абзаце в момент его написания.
//
//	cd internal/repohygiene
//	grep -cE '^[[:space:]]+add\("' subscriptionformshape.go                    # мест: 22
//	grep -oE '^[[:space:]]+add\("[a-z-]*"' subscriptionformshape.go |
//		grep -o '"[a-z-]*"' | sort -u | wc -l                             # видов: 20
//	# вид без подпробы — находка. `missing-message` требуется своей пробой по
//	# полю Kind, поэтому правая сторона берёт литералы, а не вызов помощника:
//	comm -23 <(grep -oE '^[[:space:]]+add\("[a-z-]*"' subscriptionformshape.go |
//		grep -o '"[a-z-]*"' | tr -d '"' | sort -u) \
//		<(grep -o '"[a-z-]*"' subscriptionformshape_injection_test.go |
//			tr -d '"' | sort -u)
//
// # Требование к тексту — не то же, что поиск слова в комментарии
//
// Часть утверждений («причина отсутствия названа», «исход незаданного назван»,
// «признак категории проставлен») есть требования К САМОМУ ТЕКСТУ контракта, а
// не попытка вычитать из комментария поведение кода. Разница существенна:
// запрещено принимать комментарий ЗА защиту, а требовать, чтобы решение было
// записано рядом с полем, — законно и есть единственный механизм, отличающий
// решение от пропуска.
//
// # Пустой обход — отказ
package repohygiene

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SubscriptionAxisRole — роль поля запроса подписки.
const (
	// SubscriptionRoleAxis — ось фильтра: по ней сужается поток.
	SubscriptionRoleAxis = "ось"
	// SubscriptionRoleStart — ветвь выбора начала: с какого места отдавать.
	SubscriptionRoleStart = "начало"
)

// SubscriptionFieldRecord — одна запись ЗАКРЫТОГО перечня полей запроса.
type SubscriptionFieldRecord struct {
	// Field — имя поля в запросе подписки.
	Field string
	// Role — SubscriptionRoleAxis либо SubscriptionRoleStart.
	Role string
	// Why — зачем поле в форме. Читается на обзоре; гейт требует, чтобы оно
	// было непусто, и не судит содержания.
	Why string
}

// SubscriptionAbsentAxis — ось, которой в форме нет ПО РЕШЕНИЮ.
type SubscriptionAbsentAxis struct {
	// Axis — имя поля, которого в запросе быть не должно.
	Axis string
	// Marker — слово, которым контракт обязан назвать её отсутствие.
	Marker string
	// Why — почему её нет.
	Why string
}

// SubscriptionShapeExpectation — что форма обязана нести структурно.
//
// Перечислено ЗДЕСЬ, а не зашито в анализатор, по двум причинам: требование
// становится читаемым без чтения кода, и инъекция может подать анализатору
// другое ожидание, не переписывая его.
type SubscriptionShapeExpectation struct {
	RequestMessage string
	OpenedMessage  string
	EventMessage   string

	// StartOneof — ветвление начала в запросе.
	StartOneof string
	// StartAnchorBranch — ветвь, называющая место словом.
	StartAnchorBranch string
	// CarrierOneof — ветвление носителя нагрузки в событии.
	CarrierOneof string
	// CarrierStateBranch — ветвь состояния.
	CarrierStateBranch string
	// UntypedCarriers — типы, которые НЕ являются названным носителем: у них
	// ключи на проводе производятся от имён идентификаторов Go.
	UntypedCarriers []string
	// AuthorizationAnchor — поле-якорь, по которому принимается решение о показе.
	AuthorizationAnchor string
	// HorizonFields — поля служебного сообщения, которыми исход «позиция
	// утрачена» вообще может быть сказан.
	HorizonFields []string
	// StopReasonNames — закрытый список имён, означающих исход остановки потока.
	// Ни одного из них в событии быть не должно.
	StopReasonNames []string
	// OwnerVocabularyAxis — ось, чей словарь принадлежит ВЛАДЕЛЬЦУ.
	OwnerVocabularyAxis string
	// OwnerVocabularyType — тип, которым владельческий словарь выражается.
	OwnerVocabularyType string
	// MandatoryOption — опция поля, делающая его обязательным. Ни одна ось её
	// нести не вправе.
	MandatoryOption string
}

// SubscriptionShapeOptions — вход анализатора.
type SubscriptionShapeOptions struct {
	Root      string
	ProtoRoot string
	// FormFile — путь контракта общей формы относительно ProtoRoot.
	FormFile string

	RequestFields []SubscriptionFieldRecord
	AbsentAxes    []SubscriptionAbsentAxis
	Expect        SubscriptionShapeExpectation
}

// SubscriptionShapeCensus — то, что анализатор прочитал и проверил.
type SubscriptionShapeCensus struct {
	Lines      int
	Types      int
	TopTypes   int
	Fields     int
	Axes       int
	EnumValues int
	// Assertions — утверждений проверено.
	Assertions int
}

// SubscriptionShapeFinding — одна находка.
type SubscriptionShapeFinding struct {
	Kind   string
	Where  string
	Line   int
	Reason string
}

func (f SubscriptionShapeFinding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s %s:%d — %s", f.Kind, f.Where, f.Line, f.Reason)
	}
	return fmt.Sprintf("%s %s — %s", f.Kind, f.Where, f.Reason)
}

// subCategoryRe — признак категории оси. Словарь ЗАКРЫТ: третьего значения не
// заводится, потому что «прочее» перестаёт различать через месяц.
//
// Класс букв здесь выписан диапазоном, а не `\w`: в этом движке `\w` — ASCII, и
// первая же редакция этого выражения не находила слова, которое в контракте
// стоит, — то есть краснела на исправной форме. Ошибка нашлась сразу только
// потому, что гейт прогонялся по НАСТОЯЩЕМУ дереву, а не по одной синтетике.
var subCategoryRe = regexp.MustCompile(`(?i)Категори[^\n:]*:\s*(ЯКОРНАЯ|УДОБСТВА)`)

// subMandatoryRe — опция, делающая поле обязательным.
//
// Проверка НЕ вакуумна, и это измерено, а не предположено: ключевого слова
// `required` в proto3 нет, но опция поля с тем же смыслом в дереве ЖИВА —
// предикат `git grep -cE '\(.*required.*\)\s*=\s*true' -- proto` даёт непустой
// ответ. Первая редакция этого файла объявляла проверку невозможной по
// построению; основание оказалось фольклором, снятым одной командой.
var subMandatoryRe = regexp.MustCompile(`\(\s*required\s*\)\s*=\s*true`)

// subUnsetRe — утверждение об исходе НЕЗАДАННОГО. Форма терпима к переизложению
// (пробелы, падеж), но требует, чтобы утверждение СТОЯЛО.
var subUnsetRe = regexp.MustCompile(`(?i)не\s*задан`)

// AuditSubscriptionFormShape читает контракт общей формы и возвращает
// расхождения с объявленным перечнем и ожиданием.
func AuditSubscriptionFormShape(
	opts SubscriptionShapeOptions, out io.Writer,
) ([]SubscriptionShapeFinding, SubscriptionShapeCensus, error) {
	var c SubscriptionShapeCensus
	rel := filepath.ToSlash(filepath.Join(opts.ProtoRoot, opts.FormFile))
	body, err := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, c, fmt.Errorf(
			"контракт общей формы %s не прочитан: %w — судить не о чем, и молчание "+
				"гейта было бы неотличимо от чистоты", rel, err)
	}
	f := ParseProtoFile(rel, string(body))
	c.Lines = f.Lines
	c.Types = len(f.Types)
	for _, t := range f.Types {
		if t.TopLevel() {
			c.TopTypes++
		}
		c.Fields += len(t.Fields)
		c.EnumValues += len(t.Values)
	}
	if c.Types == 0 || c.Fields == 0 {
		return nil, c, fmt.Errorf(
			"в %s разобрано типов %d, полей %d — разбор пуст, и всякое утверждение "+
				"о форме было бы утверждением ни о чём", rel, c.Types, c.Fields)
	}
	if f.Package != SubscriptionCommonPackage {
		return nil, c, fmt.Errorf(
			"%s объявляет пакет %q, а форма подписки живёт в %q — анализатор судит "+
				"не тот файл", rel, f.Package, SubscriptionCommonPackage)
	}

	var findings []SubscriptionShapeFinding
	add := func(kind string, line int, reason string) {
		findings = append(findings, SubscriptionShapeFinding{
			Kind: kind, Where: rel, Line: line, Reason: reason})
	}
	e := opts.Expect

	req, okReq := f.Type(e.RequestMessage)
	opened, okOpened := f.Type(e.OpenedMessage)
	event, okEvent := f.Type(e.EventMessage)
	for _, m := range []struct {
		name string
		ok   bool
	}{{e.RequestMessage, okReq}, {e.OpenedMessage, okOpened}, {e.EventMessage, okEvent}} {
		c.Assertions++
		if !m.ok {
			add("missing-message", 0, "форма обязана объявлять сообщение "+m.name+
				", а его в контракте нет: часть утверждений ниже проверить не на чем")
		}
	}

	// ── 1. Перечень полей запроса ЗАКРЫТ, в обе стороны ──────────────────────
	if okReq {
		ledger := map[string]SubscriptionFieldRecord{}
		for _, r := range opts.RequestFields {
			c.Assertions++
			if r.Why == "" {
				add("field-record-without-reason", req.Line,
					"запись перечня "+r.Field+" не называет, зачем поле в форме. "+
						"Запись без причины неотличима от недосмотра и не переживёт обзора")
			}
			ledger[r.Field] = r
		}
		seen := map[string]bool{}
		for _, fl := range req.Fields {
			c.Assertions++
			seen[fl.Name] = true
			rec, ok := ledger[fl.Name]
			if !ok {
				add("field-outside-the-ledger", fl.Line,
					"поле `"+fl.Name+"` запроса подписки не значится в закрытом перечне. "+
						"Ось заводится не злым умыслом — «для удобства», — и следующий заведёт "+
						"вторую. Перечень закрыт: добавь поле ВМЕСТЕ с записью, называющей "+
						"роль и причину, и решение станет видимым на обзоре")
				continue
			}
			if rec.Role != SubscriptionRoleAxis {
				continue
			}
			c.Axes++
			c.Assertions++
			if e.MandatoryOption != "" && subMandatoryRe.MatchString(fl.Options) {
				add("axis-made-mandatory", fl.Line,
					"ось `"+fl.Name+"` помечена опцией `"+e.MandatoryOption+"`. Ни одна ось "+
						"обязательной быть не вправе: незаданная ось не сужает — это её "+
						"единственное значение, и на этом держится вывод о том, что ось, "+
						"которую владелец отобрать не умеет, остаётся незаданной, не порождая "+
						"ломающего изменения. Обязательная ось вдобавок не вмещает владельца, "+
						"чьи события не про ресурс")
			}
			c.Assertions++
			if !subCategoryRe.MatchString(fl.Comment) {
				add("axis-without-category", fl.Line,
					"у оси `"+fl.Name+"` не проставлен признак категории. Категорий две: "+
						"ЯКОРНАЯ — по ней принимается решение о показе, и исхода «названа, но "+
						"не применена» у неё не существует; УДОБСТВА — доотбор на клиенте "+
						"законен. Гейт требует, чтобы признак СТОЯЛ; верен ли выбор — смысл, "+
						"а не синтаксис, и решается обзором")
			}
		}
		for _, r := range opts.RequestFields {
			c.Assertions++
			if !seen[r.Field] {
				add("stale-field-record", req.Line,
					"записи перечня `"+r.Field+"` больше нечего исключать: поля с таким "+
						"именем в форме нет. Запись обязана быть снята — оставленная, она "+
						"усыновит следующее поле того же имени и сделает его невидимым")
			}
		}

		// ── 2. Оси, которых нет ПО РЕШЕНИЮ ───────────────────────────────────
		for _, a := range opts.AbsentAxes {
			c.Assertions++
			if _, present := req.Field(a.Axis); present {
				add("absent-axis-present", req.Line,
					"перечень объявляет ось `"+a.Axis+"` отсутствующей, а поле с таким именем "+
						"в форме есть. Одно из двух утверждений ложно, и читатель не знает, какое")
				continue
			}
			c.Assertions++
			if !strings.Contains(req.Comment, a.Marker) {
				add("absent-axis-unexplained", req.Line,
					"ось `"+a.Axis+"` в форме отсутствует, но контракт об этом молчит: "+
						"слова «"+a.Marker+"» в описании запроса нет. Поле без записанной "+
						"причины отсутствия заводит следующий «для удобства», и он будет "+
						"прав по-своему — возразить ему будет нечем")
			}
		}

		// ── 3. Исход НЕЗАДАННОГО начала назван ───────────────────────────────
		c.Assertions++
		start, okStart := req.Oneof(e.StartOneof)
		switch {
		case !okStart:
			add("start-not-a-choice", req.Line,
				"начало отдачи не выражено ветвлением `"+e.StartOneof+"`. Без ветвления "+
					"«с начала журнала», «с этой позиции» и «с текущего конца» перестают "+
					"быть различимы, и клиенту приходится угадывать намерение по значению")
		default:
			c.Assertions++
			named := subUnsetRe.MatchString(start.Comment)
			mentions := ""
			if anchor, ok := req.Field(e.StartAnchorBranch); ok {
				for _, t := range f.Types {
					if t.Kind != "enum" || t.Name != anchor.Type {
						continue
					}
					for _, v := range t.Values {
						if strings.Contains(start.Comment, v.Name) {
							mentions = v.Name
						}
					}
				}
			}
			if !named || mentions == "" {
				add("unset-outcome-unnamed", start.Line,
					"исход НЕЗАДАННОГО начала не назван: описание ветвления `"+e.StartOneof+
						"` обязано сказать, что означает незаданная ветвь, и назвать при этом "+
						"значение, объявленное этим же контрактом. Число ветвей здесь не "+
						"проверяется намеренно — состояний на проводе больше, чем ветвей, и "+
						"«ветвей три» зелено на форме с молчаливым умолчанием")
			}
		}

		// ── 4. Словарь видов принадлежит ВЛАДЕЛЬЦУ ───────────────────────────
		c.Assertions++
		if ax, ok := req.Field(e.OwnerVocabularyAxis); ok && ax.Type != e.OwnerVocabularyType {
			add("owner-vocabulary-fixed-by-contract", ax.Line,
				"ось `"+e.OwnerVocabularyAxis+"` объявлена типом `"+ax.Type+"`, а не `"+
					e.OwnerVocabularyType+"`. Словарь видов принадлежит ВЛАДЕЛЬЦУ и им закрыт; "+
					"перечисление, объявленное здесь, закрыло бы его контрактом — и владелец, "+
					"чьи события не про ресурс, в эту форму не поместился бы вовсе")
		}
	}

	// ── 5. Горизонт выразим служебным сообщением ─────────────────────────────
	if okOpened {
		for _, name := range e.HorizonFields {
			c.Assertions++
			if _, ok := opened.Field(name); !ok {
				add("horizon-field-missing", opened.Line,
					"служебное сообщение не несёт поля `"+name+"`. Без него владелец, "+
						"переставший удерживать часть журнала, не может сказать «твоя позиция "+
						"больше не возобновима» и назвать место, с которого подписка возможна, — "+
						"а молчаливое начало с ближайшего удержанного клиент запишет как "+
						"«изменений не было». Дописать этот исход потом — ломающее изменение")
			}
		}
	}

	// ── 6. Оболочка события ──────────────────────────────────────────────────
	if okEvent {
		c.Assertions++
		if _, ok := event.Oneof(e.CarrierOneof); !ok {
			add("carrier-not-a-choice", event.Line,
				"носитель нагрузки не выражен ветвлением `"+e.CarrierOneof+"`. Запретить "+
					"прозой значение, которое допускает собственный тип, невозможно: без "+
					"ветвления пустая нагрузка остаётся представимой, и подписчик прочтёт её "+
					"как «у предмета не осталось полей»")
		} else {
			branches := event.OneofBranches(e.CarrierOneof)
			c.Assertions++
			if len(branches) != 2 {
				add("carrier-not-a-choice", event.Line,
					fmt.Sprintf("ветвление носителя несёт %d ветв(ей), а обязано ровно две: "+
						"состояние ЛИБО признак, что состояния нет. Одна ветвь возвращает "+
						"представимость пустой нагрузки, третья — заводит исход, о котором "+
						"подписчику не сказано, что с ним делать", len(branches)))
			}
			c.Assertions++
			state, okState := event.Field(e.CarrierStateBranch)
			switch {
			case !okState:
				add("carrier-state-branch-missing", event.Line,
					"в ветвлении носителя нет ветви `"+e.CarrierStateBranch+"` — состояние "+
						"предмета передать нечем")
			default:
				c.Assertions++
				for _, bad := range e.UntypedCarriers {
					if state.Type != bad && !strings.HasPrefix(state.Type, "map<") {
						continue
					}
					add("carrier-state-untyped", state.Line,
						"ветвь состояния объявлена как `"+state.Type+"` — свободной структурой. "+
							"У неё ключи на проводе производятся от имён идентификаторов Go, "+
							"сверка совместимости контракта этого не видит by construction (в "+
							"контракте написано «объект»), и обычный внутренний рефактор молча "+
							"ломает публичную нагрузку. Носитель обязан быть НАЗВАН типом")
					break
				}
			}
		}

		c.Assertions++
		anchor, okAnchor := event.Field(e.AuthorizationAnchor)
		switch {
		case !okAnchor:
			add("anchor-not-in-envelope", event.Line,
				"оболочка события не несёт авторизуемого якоря `"+e.AuthorizationAnchor+"`. "+
					"Решение «кому это показать» пришлось бы принимать, обратившись к предмету, "+
					"а у события удаления предмета больше нет: выбор был бы из двух негодных — "+
					"спрашивать модель прав про несуществующий объект либо не показывать "+
					"удаления вовсе. Второе наступает ТИХО")
		case anchor.Oneof != "":
			add("anchor-not-in-envelope", anchor.Line,
				"якорь `"+e.AuthorizationAnchor+"` стоит ветвью ветвления `"+anchor.Oneof+
					"`: он есть не всегда, а решение о показе принимается всегда")
		}

		for _, t := range f.Types {
			if len(t.Path) < 2 || t.Path[0] != e.EventMessage {
				continue
			}
			c.Assertions++
			if _, inner := t.Field(e.AuthorizationAnchor); inner {
				add("anchor-inside-the-payload", t.Line,
					"якорь `"+e.AuthorizationAnchor+"` объявлен ВНУТРИ вложенного сообщения "+
						strings.Join(t.Path, ".")+". Якорь — поле ОБОЛОЧКИ: внутри нагрузки он "+
						"недоступен решению о показе ровно тогда, когда нагрузки нет")
			}
		}

		for _, name := range e.StopReasonNames {
			c.Assertions++
			if fl, ok := event.Field(name); ok {
				add("stop-reason-mandated", fl.Line,
					"событие несёт поле `"+name+"`. Исход остановки потока — статус вызова, "+
						"который следующая фаза вправе выбрать отличимым; поле внутри события "+
						"загоняет его в контракт ДАННЫХ и обязывает сообщать полезной нагрузкой "+
						"то, что нагрузкой не является")
			}
		}
	}

	// ── 7. Периметр: ни глагола, ни зависимости от домена ────────────────────
	c.Assertions++
	if len(f.Services) > 0 || len(f.RPCs) > 0 {
		add("verb-declared", 0,
			fmt.Sprintf("в общей форме объявлено служб %v и глаголов %v. Форма обязана быть "+
				"утверждена ДО того, как под неё пишут сервер: объявленный глагол без сервера "+
				"потребовал бы послабления гейту монтирования, записи в каталоге прав и увёл "+
				"бы предикат потоковых методов с нуля, не отличая «поток есть» от «поток "+
				"объявлен и мёртв»", f.Services, f.RPCs))
	}
	for _, imp := range f.Imports {
		c.Assertions++
		if !strings.HasPrefix(imp, "kacho/") {
			continue
		}
		add("foreign-contract-dependency", 0,
			"общая форма импортирует контракт дерева `"+imp+"`. Форма, зависящая от домена, "+
				"общей не является: домен, у которого этого контракта нет, взять её не сможет, "+
				"и рядом появится вторая")
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Line < findings[j].Line
	})

	if out != nil {
		_, _ = fmt.Fprintf(out,
			"перепись: %s — строк %d, типов %d (верхнего уровня %d), полей %d "+
				"(осей %d), значений перечислений %d; утверждений проверено %d; находок %d\n",
			rel, c.Lines, c.Types, c.TopTypes, c.Fields, c.Axes, c.EnumValues,
			c.Assertions, len(findings))
	}
	return findings, c, nil
}
