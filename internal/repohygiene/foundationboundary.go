// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"sort"
	"strings"
)

// foundationboundary.go — вердикт о ГРАНИЦЕ ФУНДАМЕНТА: где живёт каждый каталог
// `pkg/*`, куда ему позволено смотреть и что не имеет права оказаться в
// поставляемом двоичном (приёмка K3-1, сценарии K3-01…K3-19).
//
// # Зачем гейт таблице, которая уже написана
//
// Целевая раскладка — три модуля с однонаправленными зависимостями:
//
//	corelib  <-  kaname  <-  kacho
//
// Приёмка K3-1 классифицировала каждый каталог `pkg/*` и назвала запрещённые
// направления. До этого файла таблица не держалась НИЧЕМ, кроме документа в
// соседнем репозитории: заведи 52-й каталог — и его класс не спросит никто, а
// заведи ребро запрещённого направления — оно соберётся чисто, потому что
// сегодня все каталоги лежат в одном модуле и Go отвергнуть их не может.
// Отказ пришёл бы только в день раскола, разом и на всё дерево.
//
// # Три оси, и ни одна не выводится из двух других
//
//	класс       у каждого каталога `pkg/*` он объявлен, и объявление сверено с деревом
//	направление ребро между модулями сверено с перечнем разрешённых
//	поставка    каталог класса «оснастка сборки» не лежит в замыкании двоичного
//
// Третью ось не заменяет вторая: оснастка физически живёт в `corelib`, поэтому
// направление у ребра к ней законное. Запрещено не направление, а ИСПОЛНЕНИЕ —
// двоичное, читающее дерево исходников и запускающее git.
//
// # Ведомость известных рёбер хранит ТОЧНОЕ число, а не потолок
//
// Ведомость ЭТОЙ оси сегодня ПУСТА: рёбер запрещённого направления в дереве не
// осталось ни одного — записи З2, З3 и З8 сняты вместе со своими предметами
// (разбор у самой ведомости). Пустая ведомость есть ЦЕЛЬ, а не поломка, и судья
// на ней проходит: он отказывает на пустом ОБХОДЕ, а не на пустом послаблении.
//
// Читать это как «предмет З8 закрыт» НЕЛЬЗЯ: у оси ТРЕТЬЕЙ своя ведомость, и в
// ней З8 жив двумя записями (`knownShippedToolchain` ниже). Ведомости разные,
// предмет у них общий только по имени.
//
// Пока записи были, они хранились поимённо и с точным счётом файлов, и правило
// остаётся в силе для следующей:
//
//   - новое ребро — находка, даже между уже названными пакетами (счёт вырос);
//   - ребро, которого больше нет, — ТОЖЕ находка: запись, которой нечего
//     исключать, обязана истечь сама, иначе послабление переживёт свой предмет.
//
// Потолок («не больше N») не годится: он не краснеет на сокращении долга и
// потому не истекает никогда.

// foundationClass — класс размещения. Набор ЗАКРЫТ: пятого значения не бывает,
// и это проверяется, а не подразумевается (K3-02).
type foundationClass string

const (
	classCorelib   foundationClass = "corelib"
	classKaname    foundationClass = "kaname"
	classKacho     foundationClass = "kacho"
	classToolchain foundationClass = "оснастка сборки"
)

// foundationClasses — класс каждого каталога `pkg/*`.
//
// Ключ — имя каталога, а не путь: подкаталоги разрешает foundationSubtrees.
// Каталог `pkg/*`, которого здесь нет, — находка, а не умолчание: 52-й каталог
// обязан быть классифицирован ПРАВИЛОМ приёмки (§3), а не молчанием карты.
var foundationClasses = map[string]foundationClass{
	"api":             classKacho,
	"audit":           classCorelib,
	"auth":            classCorelib,
	"authz":           classCorelib,
	"backoff":         classCorelib,
	"baggage":         classCorelib,
	"config":          classCorelib,
	"contractroot":    classCorelib,
	"credsecret":      classKaname,
	"db":              classCorelib,
	"dbready":         classCorelib,
	"dropguard":       classCorelib,
	"errors":          classCorelib,
	"filter":          classCorelib,
	"gitenv":          classToolchain,
	"grpcclient":      classCorelib,
	"grpcsrv":         classCorelib,
	"httpbody":        classCorelib,
	"identityposture": classKaname,
	"ids":             classCorelib,
	"internal":        classCorelib,
	"listcursorplan":  classToolchain,
	"listfiltergate":  classToolchain,
	"listnarrow":      classCorelib,
	"migrations":      classCorelib,
	"migratorcli":     classCorelib,
	"migratorrun":     classCorelib,
	// modulemanifest — общий словарь ОБОИХ продуктов, как platformmodules и
	// moduleselfgating рядом: имя, под которым модуль платформы кладёт своё
	// объявление в дереве, и ключ, которым тот же документ приезжает в доставке.
	// Платформа под этим именем ПИШЕТ, служба доступа по нему ЧИТАЕТ, и вывести
	// его не может ни одна сторона: у платформы нет формата, адресованного iam,
	// у службы нет раскладки чужого дерева. Это не код, а договорённость о
	// формате, — и владельца у неё двое.
	//
	// Здесь стоял класс платформы, и он делал раскол НЕИСПОЛНИМЫМ, а не просто
	// отложенным: пакет лежит в замыкании РАНТАЙМА трёх поставляемых двоичных
	// службы (перепись оси шестой), то есть в день раскола его обязан разрешить
	// `go.mod` модуля службы — а разрешить направление «служба → платформа»
	// нечем, кроме обратного require, то есть цикла, отменяющего порядок выпуска
	// целиком.
	"modulemanifest": classCorelib,
	// moduleselfgating — общий словарь ОБОИХ продуктов, как и platformmodules
	// рядом: платформа его сверяет со своим прод-кодом, служба доступа читает
	// как источник третьей полосы читателей отношения. Ни одна сторона не может
	// вывести его сама — у одной нет модели, у другой нет чужого кода, — поэтому
	// каталог общий, а не kaname и не kacho.
	"moduleselfgating": classCorelib,
	"nameformdb":       classToolchain,
	"observability":    classCorelib,
	"operations":       classCorelib,
	"option":           classCorelib,
	"outbox":           classCorelib,
	"ownerregister":    classKaname,
	"pagetoken":        classCorelib,
	"peer":             classCorelib,
	"pgtest":           classToolchain,
	"platformmodules":  classCorelib,
	"principalwire":    classCorelib,
	"quota":            classCorelib,
	"retention":        classCorelib,
	"retry":            classCorelib,
	"safeconv":         classCorelib,
	"schemaguard":      classCorelib,
	"servicecontract":  classCorelib,
	"servicehost":      classCorelib,
	"shutdown":         classCorelib,
	"singlepass":       classCorelib,
	"subjectchange":    classKaname,
	"subscription":     classCorelib,
	"tokenpolicy":      classKaname,
	"treecorpus":       classToolchain,
	"validate":         classCorelib,
}

// foundationSubtrees — каталоги, уезжающие НЕ ЦЕЛИКОМ (приёмка §5, знак †).
//
// Порядок значения не имеет: побеждает самая длинная совпавшая приставка,
// поэтому запись нельзя обезвредить, поставив её выше соседней.
//
// `pkg/api` расщепляется по правилу «контракт едет туда, где живёт пакет,
// который его РЕАЛИЗУЕТ» (§5.1): контракт доступа — в `kaname`, общие
// примитивы (операция, поток изменений, учёт потолков, разметка доступа) —
// в `corelib`, доменные контракты платформы — в `kacho`.
//
// После KAN-PKG-1 контракт доступа лежит под СВОИМ корнем (`pkg/api/kaname/…`),
// и путь с классом теперь СОВПАДАЮТ. Прежде запись переопределяла класс вопреки
// пути — стабы службы лежали под корнем платформы, — и это переопределение было
// единственным, что удерживало границу. Совпадение не делает запись лишней:
// класс объявляется здесь, а не выводится из пути, иначе следующий переезд
// сменил бы владение молча.
var foundationSubtrees = []struct {
	Prefix string
	Class  foundationClass
}{
	{"pkg/api/kaname/cloud/iam", classKaname},
	{"pkg/api/kacho/cloud/operation", classCorelib},
	{"pkg/api/kacho/cloud/subscription", classCorelib},
	{"pkg/api/kacho/cloud/quota", classCorelib},
	// Нейтральный корень объявляется ЦЕЛИКОМ, а не по одному словарю. Прежде здесь
	// стоял `pkg/api/corelib/authz`, и записи хватало ровно на один переезд: второй
	// словарь (#2395, разметка операции) лёг рядом в `pkg/api/corelib/api/v1` и
	// получил бы класс `kacho` от каталога `pkg/api` — то есть фундамент был бы
	// объявлен платформой молча. Корень `corelib/` есть фундамент ПО ПОСТРОЕНИЮ:
	// туда кладут то, что не принадлежит ни одному продукту.
	{"pkg/api/corelib", classCorelib},
	{"pkg/quota/quotaiam", classKaname},
	{"pkg/quota/quotapb", classKaname},

	// Адаптер порта сужения к контракту владельца модели (приёмка §7.2, задача
	// #2131). Правило то же, что у расщепления `pkg/api`: контракт остаётся у
	// того, кто его РЕАЛИЗУЕТ. Порт при этом остаётся в `corelib` — шов проходит
	// сквозь тип, а не по каталогу.
	{"pkg/listnarrow/narrowiam", classKaname},
	{"pkg/authz/authziam", classKaname},

	// Производитель ConfigMap — ОСНАСТКА СБОРКИ, а не фундамент: его вход — дерево
	// исходников и профили стенда, а не запрос, и ни в одном поставляемом двоичном
	// его нет. Объявляется ЯВНО именно потому, что родительский каталог стал
	// `corelib`: без записи производитель унаследовал бы класс фундамента МОЛЧА и
	// уехал бы в нейтральный модуль вместе с зашитой раскладкой чужого дерева
	// (`services/`, профили умбреллы) — то есть карта стала бы менее правдива от
	// правки, которая её правдивость и восстанавливает.
	{"pkg/modulemanifest/producer", classToolchain},
}

// foundationRoots — класс дерева ВНЕ `pkg/`. Нужен затем, что направление
// проверяется по дереву модуля-источника ЦЕЛИКОМ: предикат, сужённый до `pkg/`,
// отвечает одинаково на «ребро снято» и «ребро переехало в службу» (приёмка §7.4).
//
// Всё, что не названо здесь, — оснастка: гейты, инструменты, пробы стенда и
// профили развёртывания в поставку не входят и ни одному из трёх модулей не
// принадлежат.
var foundationRoots = []struct {
	Prefix string
	Class  foundationClass
}{
	{"services/iam", classKaname},
	{"services", classKacho},
	{"gateway", classKacho},
	{"terraform", classKacho},

	// Оснастка объявляется ПОИМЁННО, а не умолчанием. Прежде эти четыре корня
	// получали класс молча — и вместе с ними его получал ЛЮБОЙ новый верхний
	// корень, потому что умолчание не спрашивает имени. Разбор оси пятой ниже.
	{"internal", classToolchain},
	{"deploy", classToolchain},
	{"tools", classToolchain},
	{"ui-future", classToolchain},
}

// classOfPackage — класс пакета по его пути от корня дерева.
//
// Второе значение — false, когда каталог `pkg/*` не объявлен: вызывающий обязан
// прочитать это как НАХОДКУ, а не подставить умолчание. Умолчание здесь и было
// бы той самой дырой, ради которой карта заведена.
func classOfPackage(rel string) (foundationClass, bool) {
	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")

	if strings.HasPrefix(rel, "pkg/") || rel == "pkg" {
		best := ""
		var cls foundationClass
		for _, s := range foundationSubtrees {
			if (rel == s.Prefix || strings.HasPrefix(rel, s.Prefix+"/")) && len(s.Prefix) > len(best) {
				best, cls = s.Prefix, s.Class
			}
		}
		if best != "" {
			return cls, true
		}
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			return "", false
		}
		c, ok := foundationClasses[parts[1]]
		return c, ok
	}

	best := ""
	var cls foundationClass
	for _, r := range foundationRoots {
		if (rel == r.Prefix || strings.HasPrefix(rel, r.Prefix+"/")) && len(r.Prefix) > len(best) {
			best, cls = r.Prefix, r.Class
		}
	}
	if best != "" {
		return cls, true
	}
	// Здесь стояло `return classToolchain, true` — УМОЛЧАНИЕ, и оно делало гейт
	// fail-open ровно для того корня, который заведёт разъезд: каталог вне
	// известных карт получал класс «оснастка сборки», а оснастка не участвует
	// НИ В ОДНОЙ запрещённой паре (см. forbiddenDirections). Значит рёбра из
	// нового верхнего корня не судились ни одной парой: файл прочитывался —
	// перепись росла — и молча разрешался.
	//
	// Теперь незнакомый путь возвращает false, и его обязана поймать ось пятая
	// (judgeTreeRoots): корень без объявленного класса — находка, а не
	// умолчание. Это то же требование, что ось первая предъявляет каталогу
	// `pkg/*`, распространённое на корни дерева.
	return "", false
}

// forbiddenDirections — направления, запрещённые целевой раскладкой.
//
// Оснастка в этой таблице не участвует НАМЕРЕННО: она не модуль, а класс, и её
// предмет — не направление, а неисполнение в поставляемом процессе (ось три).
var forbiddenDirections = map[[2]foundationClass]bool{
	{classCorelib, classKaname}: true,
	{classCorelib, classKacho}:  true,
	{classKaname, classKacho}:   true,
}

// boundaryEdge — наблюдённое ребро между пакетами, с раздельным счётом прод и
// проб: пробы входят в граф зависимостей своего модуля (`go.mod` обязан их
// разрешить), поэтому они считаются — но отдельно, у них свой предмет (З3).
type boundaryEdge struct {
	From      string
	To        string
	FromClass foundationClass
	ToClass   foundationClass
	Prod      int
	Test      int
}

func (e boundaryEdge) key() string { return e.From + " -> " + e.To }

// knownBoundaryEdge — запись ведомости: ребро, которое в дереве СЕГОДНЯ есть и
// снимается названным предметом. Счёт точный — потолок не истекает никогда.
type knownBoundaryEdge struct {
	From    string
	To      string
	Prod    int
	Test    int
	Subject string
}

// knownBoundaryEdges — ведомость рёбер запрещённого направления, живых на
// сегодняшнем дереве. Каждая запись названа предметом приёмки K3-1 §11.
//
// Записи НЕ группируются «по причине»: ведомость обязана называть пару пакетов,
// иначе новое ребро между уже названными модулями уедет под чужую запись.
var knownBoundaryEdges = []knownBoundaryEdge{
	// Здесь стояли ПЯТЬ записей З3 — пробы фундамента с межмодульными привязками.
	// Их больше нет НИ ОДНОЙ, и это значит, что рёбер запрещённого направления в
	// `pkg/` не осталось вовсе: ни в прод-коде, ни в пробах.
	//
	// Сняты задачей #2532 вместе со своим предметом. Обе стороны переведены на
	// НЕЙТРАЛЬНЫЕ дескрипторы, собираемые самим прогоном:
	// `pkg/authz/catalogderive/probefixture_test.go` (пакет `corelib.authz.probe.v1`,
	// шесть полос вывода) и `TestMain` в `pkg/servicehost/wiring_test.go` (пакет
	// `corelib.servicehost.probe.v1`, два мутирующих метода).
	//
	// Ради чего это стоило делать перед публикацией: пока привязки были живы,
	// `go mod tidy` на собранном фундаменте дописывал
	// `require github.com/PRO-Robotech/kacho` псевдоверсией — то есть опубликованный
	// фундамент объявил бы зависимость от платформы и перевернул целевую раскладку.
	// Версию из базы контрольных сумм не отозвать, поэтому ошибка была бы
	// необратимой.
	//
	// Шестая привязка того же семейства снята вместе с МЕСТОМ, а не с предметом:
	// проба `TestNoDomainServesServerStreams` судила свойство дерева ПЛАТФОРМЫ,
	// живя в фундаменте, и это свойство уже держится строже в самой платформе —
	// `TestSubscriptionFormIsDeclaredOnce` читает дерево контрактов и опознаёт
	// подписку тремя независимыми признаками.

	// Здесь стояли ТРИ записи З8 — рёбра службы к `pkg/modulemanifest` и к его
	// производителю. Их больше нет НИ ОДНОЙ, и вместе с ними ведомость опустела
	// целиком.
	//
	// Сняты вместе со своим предметом, и предметом оказалась не координата, а
	// КЛАСС: пакет объявляет имя, которым платформа ПИШЕТ, а служба ЧИТАЕТ, —
	// общий словарь обоих продуктов, как `platformmodules` и `moduleselfgating`
	// рядом, — но числился платформой. Отсюда и все три ребра сразу.
	//
	// Долгом это быть не могло. Пакет лежит в замыкании РАНТАЙМА трёх
	// поставляемых двоичных службы, поэтому «донесём до дня раскола» означало бы
	// «до дня, когда работа уже невыполнима»: разрешить направление в `go.mod`
	// нечем, кроме обратного require. Класс исправлен на `corelib`, и рёбра стали
	// законными СМЕНОЙ ВЛАДЕНИЯ, а не переименованием пути — переименование
	// двигает подстроку, оставляя граф модулей нетронутым (разбор — в шапке
	// `scripts/release/assert-no-module-reciprocity.sh`).

	// Здесь стояли записи разреза дерева контрактов. Их больше нет НИ ОДНОЙ, и
	// это значит, что прод-рёбер запрещённого направления в `pkg/` не осталось:
	// фундамент извлекаем как есть.
	//
	// Последней снята пара `pkg/api/kaname/cloud/iam/v1` → `pkg/api/kacho/cloud/api`
	// (задача #2395, 18 прод-файлов): словарь разметки операции переехал под
	// нейтральный корень `pkg/api/corelib/api/v1`, потому что им размечают свои
	// глаголы ОБА продукта, а пометка носителя секрета — под корень службы
	// `pkg/api/kaname/cloud/iam/v1`, потому что её импортируют 4 контракта и все
	// они службы. Прежде тем же изменением снята пара
	// `pkg/api/kacho/cloud/quota/v1` → `pkg/api/kaname/cloud/iam/v1` (#2117, S2).
	//
	// Ведомость несёт точный счёт и краснеет сама, когда прощать становится нечего:
	// снятие каждой записи — её вердикт, а не решение автора.

	// #2089 — словарь аннотаций доступа переехал под нейтральный корень
	// (`pkg/api/kacho/iam/authz/v1` → `pkg/api/corelib/authz/v1`). Здесь стояли ДВЕ
	// записи: пакеты службы называли прежний путь, потому что модуль службы резолвит
	// платформу опубликованной версией, а нового пути в ней ещё не было.
	//
	// Записи СНЯТЫ вместе со своим предметом (задача #2131): пин сдвинут на ревизию,
	// несущую новый путь, оба импорта переведены, и прежнего пути в замыкании службы
	// больше нет НИ ОДНИМ ребром — ни прямым, ни транзитивным. Ведомость краснела на
	// этих строках сама («исключению нечего исключать»), и снятие — её вердикт, а не
	// решение автора.
}

// boundaryCensus — объём осмотренного. Печатается ВСЕГДА: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type boundaryCensus struct {
	FilesRead   int
	Imports     int
	Catalogs    int
	Declared    int
	Edges       int
	LedgerRows  int
	Binaries    int
	ReachedPkgs int
}

// Перепись печатается ПО ОСЯМ, а не одной строкой на все восемь величин: ось
// меряет своё, и общая строка называла бы нулём то, чего эта ось не спрашивала.
// Ноль, полученный «не спрашивали», и ноль, полученный замером, — разные
// утверждения, и печатать их одинаково значит лгать о прочитанном.

func (c boundaryCensus) CatalogSummary() string {
	return fmt.Sprintf("каталогов pkg в дереве %d · объявлено классов %d",
		c.Catalogs, c.Declared)
}

func (c boundaryCensus) EdgeSummary() string {
	return fmt.Sprintf(
		"файлов Go прочитано %d · внутридревесных импортов (файл×путь) %d · "+
			"рёбер запрещённого направления %d · строк ведомости %d",
		c.FilesRead, c.Imports, c.Edges, c.LedgerRows)
}

func (c boundaryCensus) ClosureSummary() string {
	return fmt.Sprintf(
		"поставляемых двоичных %d · каталогов pkg в их замыкании %d · строк ведомости %d",
		c.Binaries, c.ReachedPkgs, c.LedgerRows)
}

// judgeFoundationCatalogs — ось ПЕРВАЯ: у каждого каталога `pkg/*` объявлен
// класс, и объявление сверено с деревом в ОБЕ стороны (K3-01, K3-02, K3-03).
//
// Односторонняя сверка пропускает ровно то, ради чего карта заведена: каталог
// без записи читался бы умолчанием, а запись без каталога — описанием дерева,
// которого нет.
func judgeFoundationCatalogs(inTree []string, declared map[string]foundationClass) ([]string, boundaryCensus) {
	census := boundaryCensus{Catalogs: len(inTree), Declared: len(declared)}
	var faults []string

	// Пустой обход — ОТКАЗ, а не «находок ноль»: вердикт о непрочитанном
	// неотличим от вердикта о чистом дереве (K3-15).
	if len(inTree) == 0 {
		return []string{"обход пуст: под pkg/ не найдено ни одного каталога — " +
			"вердикт относился бы к непрочитанному, а не к дереву"}, census
	}

	known := map[string]bool{}
	for _, d := range inTree {
		known[d] = true
		cls, ok := declared[d]
		if !ok {
			faults = append(faults, "каталог pkg/"+d+": класса не объявлено — "+
				"классифицируйте его правилом приёмки K3-1 §3 (Ограничение → В1 → В2 → "+
				"В3 → В4, первый сработавший даёт класс) и внесите строку в "+
				"foundationClasses")
			continue
		}
		switch cls {
		case classCorelib, classKaname, classKacho, classToolchain:
		default:
			faults = append(faults, "каталог pkg/"+d+": класс "+string(cls)+
				" вне закрытого набора четырёх")
		}
	}

	for d := range declared {
		if !known[d] {
			faults = append(faults, "объявлен класс каталога pkg/"+d+
				", которого в дереве нет: запись описывает несуществующее — снимите её "+
				"вместе с каталогом")
		}
	}

	sort.Strings(faults)
	return faults, census
}

// judgeBoundaryEdges — ось ВТОРАЯ: направление каждого ребра между модулями
// (K3-05, K3-08, K3-17, K3-19).
//
// Вход — рёбра, УЖЕ отобранные по запрещённому направлению, и ведомость.
// Сверка двусторонняя, и вторая сторона важнее первой: запись, которой нечего
// исключать, — находка, иначе послабление переживёт свой предмет и следующий
// читатель примет его за действующее ограничение.
//
// Счёт файлов сверяется ТОЧНО: ребро между уже названными пакетами, набравшее
// лишний файл, — новое нарушение, и потолок его не поймал бы.
func judgeBoundaryEdges(observed []boundaryEdge, ledger []knownBoundaryEdge, filesRead, imports int) ([]string, boundaryCensus) {
	census := boundaryCensus{
		FilesRead:  filesRead,
		Imports:    imports,
		Edges:      len(observed),
		LedgerRows: len(ledger),
	}
	var faults []string

	if filesRead == 0 {
		return []string{"обход пуст: не прочитано ни одного файла Go — вердикт о " +
			"направлении рёбер относился бы к непрочитанному"}, census
	}

	byKey := map[string]boundaryEdge{}
	for _, e := range observed {
		byKey[e.key()] = e
	}
	inLedger := map[string]knownBoundaryEdge{}
	for _, k := range ledger {
		inLedger[k.From+" -> "+k.To] = k
	}

	for _, e := range observed {
		k, ok := inLedger[e.key()]
		if !ok {
			faults = append(faults, fmt.Sprintf(
				"ребро %s -> %s (%s -> %s): направление запрещено целевой раскладкой "+
					"corelib <- kaname <- kacho; прод-файлов %d, пробных %d. Сегодня оно "+
					"собирается чисто, потому что модуль один — отказ пришёл бы только в "+
					"день раскола",
				e.From, e.To, e.FromClass, e.ToClass, e.Prod, e.Test))
			continue
		}
		if k.Prod != e.Prod || k.Test != e.Test {
			faults = append(faults, fmt.Sprintf(
				"ребро %s -> %s: ведомость (предмет %s) записывает прод %d и проб %d, "+
					"в дереве прод %d и проб %d — счёт точный, а не потолок: расхождение "+
					"означает либо новое нарушение, либо закрытую часть долга, "+
					"которую надо списать из ведомости",
				e.From, e.To, k.Subject, k.Prod, k.Test, e.Prod, e.Test))
		}
	}

	for _, k := range ledger {
		if _, ok := byKey[k.From+" -> "+k.To]; !ok {
			faults = append(faults, fmt.Sprintf(
				"ведомость (предмет %s) прощает ребро %s -> %s, которого в дереве НЕТ: "+
					"исключению нечего исключать — снимите запись, иначе послабление "+
					"переживёт свой предмет",
				k.Subject, k.From, k.To))
		}
	}

	sort.Strings(faults)
	return faults, census
}

// knownShippedToolchain — ведомость оси ТРЕТЬЕЙ: каталоги класса «оснастка
// сборки», лежащие в замыкании поставляемого двоичного СЕГОДНЯ.
//
// Оба приезжают одним корнем — пакетом службы, который разбирает дерево
// исходников на пути старта, — и снимаются предметом З8.
var knownShippedToolchain = map[string]string{
	"gitenv":     "З8",
	"treecorpus": "З8",
}

// judgeShippedToolchain — ось ТРЕТЬЯ: оснастка сборки не исполняется в
// поставляемом процессе (K3-13, K3-14, F5).
//
// reached — каталоги `pkg/*`, достижимые импортами из главных пакетов
// поставляемых двоичных. Направление тут ни при чём: оснастка физически живёт
// в `corelib`, поэтому ребро к ней законно по второй оси. Запрещено ИСПОЛНЕНИЕ.
//
// Положительный близнец подан ЧИСЛОМ, а не отдельной пробой: замыкание обязано
// содержать рантайм-пакеты, иначе «оснастки не нашлось» означало бы пустое
// замыкание. Отличие близнеца от находки — один факт: чем пакет питается,
// запросом или деревом исходников.
func judgeShippedToolchain(reached map[string]foundationClass, ledger map[string]string, binaries int) ([]string, boundaryCensus) {
	census := boundaryCensus{Binaries: binaries, ReachedPkgs: len(reached), LedgerRows: len(ledger)}
	var faults []string

	if binaries == 0 {
		return []string{"обход пуст: главных пакетов поставляемых двоичных не найдено " +
			"ни одного — вердикт о замыкании относился бы к непрочитанному"}, census
	}
	if len(reached) == 0 {
		return []string{"замыкание пусто: из главных пакетов не достижимо ни одного " +
			"каталога pkg/ — «оснастки не нашлось» означало бы непрочитанное"}, census
	}

	runtimeSeen := 0
	for _, cls := range reached {
		if cls == classCorelib {
			runtimeSeen++
		}
	}
	if runtimeSeen == 0 {
		faults = append(faults, "в замыкании поставляемых двоичных нет НИ ОДНОГО "+
			"каталога класса corelib — положительный близнец не сработал, и вердикт "+
			"об оснастке зеленел бы на пустом замыкании")
	}

	for dir, cls := range reached {
		if cls != classToolchain {
			continue
		}
		subject, forgiven := ledger[dir]
		if !forgiven {
			faults = append(faults, "каталог pkg/"+dir+" класса «"+string(classToolchain)+
				"» лежит в замыкании поставляемого двоичного: его вход — дерево "+
				"исходников и индекс git, а не запрос; поставляемый процесс не имеет "+
				"права его исполнять")
			continue
		}
		// Послабление без предмета не истечёт никогда: предмет и есть то, чем
		// оно однажды снимается.
		if subject == "" {
			faults = append(faults, "ведомость прощает оснастку pkg/"+dir+" в замыкании "+
				"поставляемого двоичного, не называя предмета: послабление без предмета "+
				"не истечёт никогда")
		}
	}

	for dir, subject := range ledger {
		if _, ok := reached[dir]; !ok {
			faults = append(faults, "ведомость (предмет "+subject+") прощает оснастку pkg/"+
				dir+" в замыкании поставляемого двоичного, а её там НЕТ: исключению "+
				"нечего исключать — снимите запись")
		}
	}

	sort.Strings(faults)
	return faults, census
}

// judgeFoundationPrefixes — ось ЧЕТВЁРТАЯ: у каждой записи двух карт путей
// (foundationSubtrees, foundationRoots) есть предмет в дереве.
//
// # Зачем она, если неверный КЛАСС уже ловится тремя осями
//
// Неверный класс ловится: он немедленно меняет вердикт о рёбрах. А вот мёртвое
// ИМЯ не ловится ничем — запись, чьей приставке в дереве не соответствует ни
// один путь, просто никогда не совпадает и проходит молча:
//
//	{"pkg/api/kacho/cloud/nosuchdomain", classKaname}  // путей 0 → тишина
//	{"services/nosuchservice",           classKaname}  // путей 0 → тишина
//
// Это ровно то послабление, которое три ведомости этого файла обязаны истекать
// сами: запись, которой нечего исключать, — находка. Две карты путей жили без
// такой сверки, и симметрию собственного принципа надо было закрыть.
//
// Сверка ОДНОСТОРОННЯЯ намеренно: путь дерева без записи — не находка, а
// умолчание (класс «оснастка сборки»), и оно объявлено осознанно. Находка —
// только запись без пути.
func judgeFoundationPrefixes(declared []string, pathsUnder map[string]int) ([]string, boundaryCensus) {
	census := boundaryCensus{Declared: len(declared)}
	var faults []string

	if len(declared) == 0 {
		return []string{"карты путей пусты: судить нечего, и всякий вердикт о " +
			"разрешении пути относился бы к непрочитанному"}, census
	}
	if len(pathsUnder) == 0 {
		return []string{"обход пуст: под объявленными приставками не сосчитано ни " +
			"одного пути — «мёртвых записей нет» означало бы непрочитанное"}, census
	}

	for _, prefix := range declared {
		if pathsUnder[prefix] > 0 {
			census.Catalogs++
			continue
		}
		faults = append(faults, "карта путей объявляет приставку "+prefix+
			", которой в дереве не соответствует НИ ОДИН путь: запись описывает "+
			"несуществующее, никогда не совпадёт и потому не истечёт сама — "+
			"снимите её вместе с предметом")
	}

	sort.Strings(faults)
	return faults, census
}

// declaredPrefixes — приставки обеих карт путей, одним перечнем и в порядке
// объявления. Перечень ВЫВОДИТСЯ из карт, а не выписывается: выписанный
// разошёлся бы с ними молча — тем же способом, каким расходится всё, о чём
// сказано в двух местах.
func declaredPrefixes() []string {
	out := make([]string, 0, len(foundationSubtrees)+len(foundationRoots))
	for _, s := range foundationSubtrees {
		out = append(out, s.Prefix)
	}
	for _, r := range foundationRoots {
		out = append(out, r.Prefix)
	}
	return out
}

// PrefixSummary — перепись оси четвёртой.
func (c boundaryCensus) PrefixSummary() string {
	return fmt.Sprintf("объявлено приставок %d · с живым предметом в дереве %d",
		c.Declared, c.Catalogs)
}

// judgeTreeRoots — ось ПЯТАЯ: у каждого ВЕРХНЕГО КОРНЯ дерева объявлен класс.
//
// # Зачем она, если класс каталога `pkg/*` уже судит ось первая
//
// Ось первая обходит `pkg/`, ось вторая — рёбра, ось третья — замыкание, ось
// четвёртая — мёртвые записи карт путей. НИ ОДНА из четырёх не спрашивала, что
// за корень появился в дереве, и это была не педантичность, а дыра: незнакомый
// путь получал класс «оснастка сборки» умолчанием, а оснастка НАМЕРЕННО не
// участвует ни в одной запрещённой паре — её предмет ось третья, не вторая.
//
// Сложение двух верных решений давало fail-open: рёбра всего, что попало в
// умолчание, не судились в ОБЕ стороны. Внутри `pkg/` дыры не было (там ось
// двусторонняя), снаружи — была, и открывалась она ровно тогда, когда разъезд
// заводит корень `corelib/`.
//
// Сверка ДВУСТОРОННЯЯ, как у оси первой: корень в дереве без записи и запись
// без корня — обе находки. Корень `pkg` этой оси не принадлежит: его каталоги
// классифицируются ПОКАТАЛОЖНО осью первой, и один класс на весь корень был бы
// неправдой — там живут все четыре.
func judgeTreeRoots(inTree []string, declared map[string]foundationClass) ([]string, boundaryCensus) {
	census := boundaryCensus{Catalogs: len(inTree), Declared: len(declared)}
	var faults []string

	if len(inTree) == 0 {
		return []string{"обход пуст: верхних корней дерева не прочитано ни одного — " +
			"вердикт о классе корня относился бы к непрочитанному"}, census
	}

	known := map[string]bool{}
	for _, root := range inTree {
		known[root] = true
		cls, ok := declared[root]
		if !ok {
			faults = append(faults, "верхний корень "+root+"/: класса не объявлено. "+
				"Умолчания у корня НЕТ намеренно: прежде незнакомый корень получал "+
				"«оснастку сборки», а она не участвует ни в одной запрещённой паре — "+
				"то есть первое же нарушение границы в новом модуле прошло бы молча. "+
				"Внесите строку в foundationRoots, назвав класс правилом приёмки K3-1 §3")
			continue
		}
		switch cls {
		case classCorelib, classKaname, classKacho, classToolchain:
		default:
			faults = append(faults, "верхний корень "+root+"/: класс "+string(cls)+
				" вне закрытого набора четырёх")
		}
	}

	for root := range declared {
		if !known[root] {
			faults = append(faults, "объявлен класс верхнего корня "+root+
				"/, которого в дереве нет: запись описывает несуществующее — снимите её "+
				"вместе с корнем")
		}
	}

	sort.Strings(faults)
	return faults, census
}

// RootSummary — перепись оси пятой.
func (c boundaryCensus) RootSummary() string {
	return fmt.Sprintf("верхних корней дерева прочитано %d · объявлено классов %d",
		c.Catalogs, c.Declared)
}

// declaredTreeRoots — объявленные ВЕРХНИЕ корни, одним перечнем.
//
// ВЫВОДИТСЯ из foundationRoots (односегментные приставки), а не выписывается:
// выписанный разошёлся бы с картой молча — тем же способом, каким расходится
// всё, о чём сказано в двух местах. Многосегментные приставки (`services/iam`)
// корнями не являются и в перечень не идут: их предмет — расщепление корня, а
// не его класс.
func declaredTreeRoots() map[string]foundationClass {
	out := map[string]foundationClass{}
	for _, r := range foundationRoots {
		if strings.Contains(r.Prefix, "/") {
			continue
		}
		out[r.Prefix] = r.Class
	}
	return out
}

// executedReach — пакет, достижимый ИМПОРТАМИ ПРОД-КОДА из главного пакета
// поставляемого двоичного, вместе с классами обеих сторон.
type executedReach struct {
	Binary   string
	BinClass foundationClass
	Pkg      string
	PkgClass foundationClass
}

// ExecutedSummary — перепись оси ШЕСТОЙ.
func (c boundaryCensus) ExecutedSummary() string {
	return fmt.Sprintf(
		"поставляемых двоичных %d · пар «двоичное × достижимый пакет» %d · "+
			"из них запрещённого направления %d",
		c.Binaries, c.ReachedPkgs, c.Edges)
}

// judgeExecutedModules — ось ШЕСТАЯ: поставляемое двоичное не ИСПОЛНЯЕТ пакет
// модуля, от которого его собственный модуль зависеть не вправе.
//
// # Чем это отличается от оси второй, у которой ведомость есть
//
// Ось вторая судит ребро между пакетами и ведёт ведомость: ребро запрещённого
// направления сегодня в дереве бывает, раскол не выполнен, и каждая запись
// снимается названным предметом. Это ДОЛГ — его несут до дня раскола.
//
// Здесь долга не бывает. Пакет, лежащий в замыкании поставляемого двоичного,
// исполняется в рантайме, поэтому в день раскола его обязан разрешить `go.mod`
// того модуля, чьё это двоичное. Если направление запрещено, разрешить его
// нельзя ничем, кроме обратного `require`, — а обратное требование версий и
// есть тот цикл, который отменяет порядок выпуска целиком
// (`scripts/release/assert-no-module-reciprocity.sh`).
//
// То есть ось вторая говорит «это ещё не сделано», а ось шестая — «это не
// станет сделанным никогда, пока класс объявлен так». Ведомости у неё нет
// НАМЕРЕННО: прощать здесь означало бы отложить работу до дня, когда она уже
// невыполнима.
//
// # Положительный близнец подан ЧИСЛОМ
//
// Замыкание обязано быть непустым и обязано содержать пары законного
// направления, иначе «запрещённых пар не нашлось» означало бы пустой обход.
func judgeExecutedModules(reach []executedReach, binaries int) ([]string, boundaryCensus) {
	census := boundaryCensus{Binaries: binaries, ReachedPkgs: len(reach)}
	var faults []string

	if binaries == 0 {
		return []string{"обход пуст: главных пакетов поставляемых двоичных не найдено " +
			"ни одного — вердикт о замыкании относился бы к непрочитанному"}, census
	}
	if len(reach) == 0 {
		return []string{"замыкание пусто: из главных пакетов не достижимо ни одного " +
			"пакета дерева — «запрещённых пар не нашлось» означало бы непрочитанное"}, census
	}

	for _, r := range reach {
		if !forbiddenDirections[[2]foundationClass{r.BinClass, r.PkgClass}] {
			continue
		}
		census.Edges++
		faults = append(faults, fmt.Sprintf(
			"двоичное %s (класс «%s») исполняет %s (класс «%s»): направление "+
				"«%s → %s» запрещено целевой раскладкой, а пакет лежит в замыкании "+
				"РАНТАЙМА — в день раскола его обязан разрешить go.mod модуля %s, и "+
				"разрешить его нечем, кроме обратного require, то есть цикла. "+
				"Долгом это быть не может: ведомости у этой оси нет намеренно",
			r.Binary, r.BinClass, r.Pkg, r.PkgClass, r.BinClass, r.PkgClass, r.BinClass))
	}

	sort.Strings(faults)
	return faults, census
}
