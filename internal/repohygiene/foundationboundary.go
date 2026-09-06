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
// Рёбра запрещённого направления в дереве сегодня ЕСТЬ: раскол не выполнен, и
// приёмка называет это открытым остатком (З2, З3, З8) с предикатом снятия у
// каждого пункта. Ведомость записывает их поимённо и с точным счётом файлов,
// поэтому:
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
	"modulemanifest":  classKacho,
	"nameformdb":      classToolchain,
	"observability":   classCorelib,
	"operations":      classCorelib,
	"option":          classCorelib,
	"outbox":          classCorelib,
	"ownerregister":   classKaname,
	"pagetoken":       classCorelib,
	"peer":            classCorelib,
	"pgtest":          classToolchain,
	"platformmodules": classCorelib,
	"principalwire":   classCorelib,
	"quota":           classCorelib,
	"retention":       classCorelib,
	"retry":           classCorelib,
	"safeconv":        classCorelib,
	"schemaguard":     classCorelib,
	"servicecontract": classCorelib,
	"servicehost":     classCorelib,
	"shutdown":        classCorelib,
	"singlepass":      classCorelib,
	"subjectchange":   classKaname,
	"subscription":    classCorelib,
	"tokenpolicy":     classKaname,
	"treecorpus":      classToolchain,
	"validate":        classCorelib,
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
	{"pkg/api/kacho/iam/authz", classCorelib},
	{"pkg/quota/quotaiam", classKaname},
	{"pkg/quota/quotapb", classKaname},
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
	return classToolchain, true
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
	// З3 — пробы с межмодульными привязками (приёмка §7.3).
	{"pkg/authz/catalogderive", "pkg/api/kacho/cloud/api", 0, 1, "З3"},
	{"pkg/authz/catalogderive", "pkg/api/kacho/cloud/registry/v1", 0, 1, "З3"},
	{"pkg/authz/catalogderive", "pkg/api/kacho/cloud/storage/v1", 0, 1, "З3"},
	{"pkg/authz/catalogderive", "pkg/api/kacho/cloud/vpc/v1", 0, 1, "З3"},
	{"pkg/servicehost", "pkg/api/kacho/cloud/compute/v1", 0, 1, "З3"},
	{"pkg/servicehost", "pkg/api/kacho/cloud/vpc/v1", 0, 1, "З3"},
	{"pkg/subscription", "pkg/api/kaname/cloud/iam/v1", 0, 1, "З3"},
	{"services/iam/cmd/kaname", "pkg/api/kacho/cloud/api", 0, 1, "З3"},

	// З2 — порт сужения и вынос адаптера носителя (приёмка §7.2).
	{"pkg/listnarrow", "pkg/api/kaname/cloud/iam/v1", 3, 3, "З2"},
	{"pkg/listnarrow/narrowtest", "pkg/api/kaname/cloud/iam/v1", 1, 0, "З2"},
	{"pkg/servicehost", "pkg/api/kaname/cloud/iam/v1", 1, 1, "З2"},

	// З8 — оснастка, разбирающая дерево, в двоичном службы (приёмка §11).
	{"services/iam/internal/manifest", "pkg/modulemanifest", 1, 1, "З8"},
	{"services/iam/internal/manifest", "pkg/modulemanifest/producer", 0, 1, "З8"},
	{"services/iam/internal/modelrender", "pkg/modulemanifest", 1, 0, "З8"},

	// K3-НОВОЕ — разрез дерева контрактов; приёмка выносит его в полосу
	// контракта (§10) и этих двух рёбер не называет. Найдены обходом по обеим
	// осям на ревизии полосы; предмет заведён отчётом полосы.
	{"pkg/api/kacho/cloud/quota/v1", "pkg/api/kaname/cloud/iam/v1", 1, 0, "K3-НОВОЕ"},
	{"pkg/api/kaname/cloud/iam/v1", "pkg/api/kacho/cloud/api", 18, 0, "K3-НОВОЕ"},
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
