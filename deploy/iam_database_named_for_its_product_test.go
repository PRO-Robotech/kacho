// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_database_named_for_its_product_test.go — база службы прав зовётся именем
// СВОЕГО продукта, и тот, кто её создаёт, зовёт её так же.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба получила собственное имя продукта — Kaname. Схема Postgres была
// переименована отдельной полосой, а имя БАЗЫ осталось прежним, и половинчатость
// эта была НАЗВАНА, а не получена молча: гейт схемы
// (services/iam/internal/supplyhygiene/schema_name_test.go) пропускает имя базы
// отдельной полосой и считает пропущенное отдельным числом.
//
// Расхождение при этом уже стало наблюдаемым: девять мест документации трёх
// служб пишут «база `kaname`» (iam 00-overview, 19-authorize ×3,
// 29-relational-verdict, 32-observability, failure-domains; compute
// 07-known-divergences; vpc authz.mdx ×2), тогда как чарт подключался к
// `/kacho_iam`. Два места об одном предмете, из которых верно одно, — и неверным
// было то, у которого есть ЧИТАТЕЛЬ на стенде.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРОВЕРЯЕТСЯ ПАРА, А НЕ ОДНО ИМЯ
//
// У имени базы ДВА объявителя, и они разные по роли:
//
//	pg-iam.auth.database   — ПОСТАВЩИК: подчарт postgresql СОЗДАЁТ базу с этим
//	                         именем при первом подъёме;
//	kaname.db.name         — ПОТРЕБИТЕЛЬ: из него шаблон собирает строку
//	                         подключения (charts/kaname/templates/configmap.yaml,
//	                         `printf "postgres://%s@%s:%v/%s"`).
//
// Переименование ОДНОЙ половины даёт стенд, который поднимается и не работает:
// база создана под одним именем, служба стучится в другое, и Postgres отвечает
// «database does not exist» — отказ подключения, а не отказ старта. Поэтому
// проверка требует не «имя каноническое», а ТРИ вещи сразу: каноничность
// потребителя, каноничность поставщика и ИХ СОГЛАСИЕ между собой. Третье не
// выводится из первых двух: они сойдутся и на паре, где обе половины
// переименованы неверно одинаково, — но разойдутся ровно тогда, когда правку
// внесли в одно место из двух, а это и есть наблюдавшийся способ ошибиться.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ РАЗБОР YAML, А НЕ ПОИСК ПО ОБРАЗЦУ
//
// Имя базы записано в дереве НЕ ОДНОЙ формой, и форма, о которой распознаватель
// не знает, даёт не красное и не зелёное, а молчание:
//
//	  database: kacho_iam                             блочное отображение
//	  auth: { …, username: iam, database: kacho_iam } ПОТОЧНОЕ отображение
//	  name: kacho_iam                                 ключ потребителя
//	  "postgres://iam@host:5432/kacho_iam"            последний сегмент адреса
//
// Вторая форма реальна: `values.fe3455-prod.yaml` объявляет учётные данные
// поточным отображением в одну строку, и предикат вида `^\s*database:` её не
// видит вовсе — то есть боевой профиль остался бы вне наблюдения, а перепись
// показала бы это как «нарушений нет». Разбор YAML знает все четыре формы **by
// construction**: он судит УЗЕЛ дерева значений, а не расположение символов в
// строке.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЕДИНИЦА СЧЁТА — СТЕК, А НЕ ФАЙЛ
//
// Судится то, что стенд получает НА САМОМ ДЕЛЕ: цепочка профилей из
// deploy/stacks.txt, наложенная слева направо поверх `values.yaml`, и поверх
// умолчаний подчарта `charts/kaname/values.yaml` под ключом `kaname` — ровно
// так их сливает helm. Профиль, не названный ни одной цепочкой, здесь не
// судится намеренно: у него нет читателя, и его существование — предмет
// соседнего stack_table_test.go.
//
// Следствие, которое стоит назвать: одно и то же объявление судится столько
// раз, в скольких цепочках оно стоит. Это не двойной счёт — это и есть вопрос
// «что получит ЭТОТ стенд».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ДЕЛАЕТ — сказано прямо
//
//   - она НЕ судит имя УЗЛА базы (`db.host`, `kacho-umbrella-pg-iam`) и имя
//     секрета: и то и другое производится из имени релиза умбреллы и псевдонима
//     подчарта, то есть принадлежит витрине ПЛАТФОРМЫ, а не идентичности службы.
//     Смешать их сюда значило бы сделать вердикт этой проверки непрослеживаемым;
//   - она НЕ судит имя ПОЛЬЗОВАТЕЛЯ базы: он зовётся `iam` — именем домена, а не
//     платформы, и переименования не требует. Названо, чтобы «зелено» не читалось
//     как «вся тройка база/пользователь/секрет проверена»;
//   - она НЕ судит имена баз СОСЕДНИХ служб (`kacho_vpc`, `kacho_nlb`, …): те
//     службы остаются Kachō, и их имена верны;
//   - она читает ОБЪЯВЛЕНИЯ, а не рендер. Умолчание подчарта — свойство чарта, оно
//     меняется под профилем без единой правки профиля; и наоборот, проверка,
//     которой нужен `helm` и скачанные зависимости, умеет ПРОПУСТИТЬСЯ, а
//     пропустившаяся проверка неотличима от прошедшей.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ДОКАЗАНА СПОСОБНОСТЬ УПАСТЬ
//
// Разбор вынесен в чистую функцию auditIamDatabase, которая принимает
// ОБЪЯВЛЕНИЯ, а не файлы: настоящее дерево и синтетический вход инъекции
// проходят одну и ту же функцию, поэтому доказанное на втором верно для первого.
// Инъекция — iam_database_named_for_its_product_injection_test.go, по одной оси
// на каждую форму отказа плюс законный близнец на каждую.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// canonicalIamDatabase — имя базы службы прав. ОДНО объявление на дерево: и
// проверка, и её инъекция читают отсюда, поэтому «что проверяется» и «что
// объявлено» разойтись не могут.
const canonicalIamDatabase = "kaname"

// retiredIamDatabase — имя, которое база носила, пока служба звалась доменом
// платформы. Оставлено не памяти ради: это ВХОД распознавателя, и без него
// находка не смогла бы назвать, ЧТО именно она нашла.
const retiredIamDatabase = "kacho_iam"

// iamConsumerKey / iamProviderKey — пути объявлений в дереве значений умбреллы.
// Объявлены здесь один раз; и обход, и текст находки читают отсюда.
var (
	iamConsumerKey = []string{"kaname", "db", "name"}
	iamProviderKey = []string{"pg-iam", "auth", "database"}
)

// iamDatabaseDecl — что ОДИН стек объявляет об имени базы службы прав.
//
// Пустая строка у половины означает «стек её не объявил» и отличается от
// «объявил неверно»: у первого исхода причина в цепочке профилей, у второго —
// в значении. Тексты находок эти исходы различают.
type iamDatabaseDecl struct {
	stack    string // имя стека из deploy/stacks.txt
	chain    string // цепочка профилей, как её получает helm
	consumer string // kaname.db.name — как служба зовёт базу
	provider string // pg-iam.auth.database — как поставщик её создаёт
	dsns     []iamDatabaseDSN
}

// iamDatabaseDSN — адрес подключения, объявленный ЗНАЧЕНИЕМ где-то в дереве
// стека. Сегодня шаблон собирает адрес сам из `db.*`, поэтому таких объявлений
// ноль; полоса заведена не про запас, а потому что объявленный значением адрес
// есть ВТОРОЕ место об имени базы, и разойтись с первым оно сможет молча.
type iamDatabaseDSN struct {
	path     string // путь узла в дереве значений
	database string // последний сегмент адреса
}

// iamDatabaseCensus — объём осмотренного. Печатается всегда, включая зелёный
// прогон: «ноль находок» обязано быть отличимо от «ноль прочитанного».
type iamDatabaseCensus struct {
	stacks         int // стеков рассмотрено
	profiles       int // наложений профилей прочитано
	consumerJudged int // объявлений потребителя рассмотрено
	providerJudged int // объявлений поставщика рассмотрено
	dsnJudged      int // адресов подключения рассмотрено
	canonicalHits  int // объявлений, назвавших каноническое имя
	parityJudged   int // пар чартов сверено на согласие
}

// auditIamDatabase судит ОБЪЯВЛЕНИЯ и возвращает находки с переписью.
//
// Функция чистая и принимает разобранные объявления, а не пути: тем же входом
// её кормит инъекция, поэтому её вердикт о синтетике есть вердикт о механизме.
func auditIamDatabase(decls []iamDatabaseDecl) ([]string, iamDatabaseCensus) {
	var (
		findings []string
		census   iamDatabaseCensus
	)

	for _, d := range decls {
		census.stacks++
		census.profiles += len(strings.Split(d.chain, ","))

		// Потребитель — из него шаблон собирает адрес подключения.
		census.consumerJudged++
		switch {
		case d.consumer == "":
			findings = append(findings, fmt.Sprintf(
				"стек %q (%s): имя базы у ПОТРЕБИТЕЛЯ (%s) не объявлено ни цепочкой, "+
					"ни умолчанием подчарта — служба соберёт адрес подключения с пустым "+
					"последним сегментом", d.stack, d.chain, strings.Join(iamConsumerKey, ".")))
		case d.consumer == canonicalIamDatabase:
			census.canonicalHits++
		default:
			findings = append(findings, fmt.Sprintf(
				"стек %q (%s): ПОТРЕБИТЕЛЬ зовёт базу %q, канон — %q (%s). "+
					"Имя базы есть то, чем продукт себя называет",
				d.stack, d.chain, d.consumer, canonicalIamDatabase,
				strings.Join(iamConsumerKey, ".")))
		}

		// Поставщик — он СОЗДАЁТ базу при первом подъёме стенда.
		census.providerJudged++
		switch {
		case d.provider == "":
			findings = append(findings, fmt.Sprintf(
				"стек %q (%s): имя базы у ПОСТАВЩИКА (%s) не объявлено — подчарт "+
					"postgresql создаст базу своим умолчанием, которое этот стенд не выбирал",
				d.stack, d.chain, strings.Join(iamProviderKey, ".")))
		case d.provider == canonicalIamDatabase:
			census.canonicalHits++
		default:
			findings = append(findings, fmt.Sprintf(
				"стек %q (%s): ПОСТАВЩИК создаёт базу %q, канон — %q (%s)",
				d.stack, d.chain, d.provider, canonicalIamDatabase,
				strings.Join(iamProviderKey, ".")))
		}

		// Согласие половин. Утверждается ОТДЕЛЬНО от каноничности: пара, обе
		// половины которой переименованы неверно ОДИНАКОВО, каноничность
		// провалит и согласие пройдёт, — а пара, где правку внесли в одно место
		// из двух, провалит именно это утверждение и назовёт обе стороны.
		if d.consumer != "" && d.provider != "" && d.consumer != d.provider {
			findings = append(findings, fmt.Sprintf(
				"стек %q (%s): половины имени базы РАЗОШЛИСЬ — поставщик создаёт %q "+
					"(%s), потребитель стучится в %q (%s). Стенд поднимется и не заработает: "+
					"Postgres ответит «database does not exist» на каждом соединении",
				d.stack, d.chain, d.provider, strings.Join(iamProviderKey, "."),
				d.consumer, strings.Join(iamConsumerKey, ".")))
		}

		// Адрес, объявленный значением, — второе место об имени базы.
		for _, dsn := range d.dsns {
			census.dsnJudged++
			if dsn.database == canonicalIamDatabase {
				census.canonicalHits++
				continue
			}
			findings = append(findings, fmt.Sprintf(
				"стек %q (%s): адрес подключения %s называет базу %q, канон — %q",
				d.stack, d.chain, dsn.path, dsn.database, canonicalIamDatabase))
		}
	}

	sort.Strings(findings)
	return findings, census
}

// iamDatabaseChartPair — объявление имени базы в ОДНОМ из двух чартов службы.
type iamDatabaseChartPair struct {
	chart string // путь файла значений
	name  string // db.name
}

// auditIamDatabaseChartParity сверяет два чарта службы между собой.
//
// Чарт службы живёт в дереве дважды: вендоренной копией внутри умбреллы (её и
// ставят все стеки) и отдельным чартом services/iam/deploy, который НЕ
// устанавливает ни один стек. Ровно эта пара уже расходилась молча по ширине
// пула (#709), и опасна она тем, что неверным оказывается место БЕЗ читателя:
// правку вносят туда, куда попали, а действует другое.
func auditIamDatabaseChartParity(pairs []iamDatabaseChartPair) []string {
	var findings []string
	for _, p := range pairs {
		if p.name == "" {
			findings = append(findings, fmt.Sprintf(
				"чарт %s не объявляет имени базы (%s)", p.chart, strings.Join(iamConsumerKey[1:], ".")))
			continue
		}
		if p.name != canonicalIamDatabase {
			findings = append(findings, fmt.Sprintf(
				"чарт %s зовёт базу %q, канон — %q", p.chart, p.name, canonicalIamDatabase))
		}
	}
	sort.Strings(findings)
	return findings
}

// iamDatabaseDSNOf вытаскивает имя базы из адреса `postgres://user@host:port/db`,
// отбрасывая параметры запроса. Пустой ответ означает «адресом не является» —
// и это ОТЛИЧАЕТСЯ от «адрес называет пустую базу», который ответ даёт как "".
func iamDatabaseDSNOf(v string) (string, bool) {
	for _, scheme := range [...]string{"postgres://", "postgresql://"} {
		rest, ok := strings.CutPrefix(v, scheme)
		if !ok {
			continue
		}
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			return "", true // адрес без пути — базу не называет
		}
		db := rest[slash+1:]
		if q := strings.IndexAny(db, "?"); q >= 0 {
			db = db[:q]
		}
		return db, true
	}
	return "", false
}

// collectIamDatabaseDSNs обходит дерево значений и собирает объявленные
// ЗНАЧЕНИЕМ адреса подключения к базе службы прав.
//
// Отбор идёт по имени базы В САМОМ АДРЕСЕ, а не по пути узла: путь у такого
// объявления заранее не известен, и перечень путей был бы вторым местом об одном
// предмете. Адрес, называющий базу соседней службы, сюда не попадает.
func collectIamDatabaseDSNs(node any, path []string, out *[]iamDatabaseDSN) {
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			collectIamDatabaseDSNs(v[k], append(path, k), out)
		}
	case []any:
		for i, item := range v {
			collectIamDatabaseDSNs(item, append(path, fmt.Sprintf("[%d]", i)), out)
		}
	case string:
		db, isDSN := iamDatabaseDSNOf(v)
		if !isDSN {
			return
		}
		if db != canonicalIamDatabase && db != retiredIamDatabase {
			return // адрес чужой базы — не предмет этой проверки
		}
		*out = append(*out, iamDatabaseDSN{path: strings.Join(path, "."), database: db})
	}
}

// readIamDatabaseDecls собирает объявления по КАЖДОМУ стеку таблицы стендов.
func readIamDatabaseDecls(t *testing.T) []iamDatabaseDecl {
	t.Helper()

	// Умолчания подчарта — helm сливает их под ключом чарта, поэтому стек,
	// не объявивший `kaname.db`, получает именно их. Проверка, читающая только
	// профили, объявила бы такой стек «не объявившим ничего».
	subchart := readYAML(t, filepath.Join(umbrellaDir, "charts", "kaname", "values.yaml"))

	var decls []iamDatabaseDecl
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		chain := stacks[name]
		tree := effectiveValues(t, chain)
		tree["kaname"] = mergeValues(mergeValues(map[string]any{}, subchart), asMapOrEmpty(tree["kaname"]))

		consumer, _ := leafString(tree, iamConsumerKey)
		provider, _ := leafString(tree, iamProviderKey)

		var dsns []iamDatabaseDSN
		collectIamDatabaseDSNs(tree, nil, &dsns)

		decls = append(decls, iamDatabaseDecl{
			stack:    name,
			chain:    strings.Join(chain, ","),
			consumer: consumer,
			provider: provider,
			dsns:     dsns,
		})
	}
	return decls
}

// asMapOrEmpty — узел как отображение либо пустое отображение. Отсутствие ключа
// и ключ с не-отображением здесь означают одно: накладывать нечего.
func asMapOrEmpty(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// TestIamDatabaseIsNamedForItsOwnProduct — гейт класса.
func TestIamDatabaseIsNamedForItsOwnProduct(t *testing.T) {
	decls := readIamDatabaseDecls(t)
	findings, census := auditIamDatabase(decls)

	pairs := []iamDatabaseChartPair{}
	for _, chart := range []string{
		filepath.Join(umbrellaDir, "charts", "kaname", "values.yaml"),
		filepath.Join("..", "services", "iam", "deploy", "values.yaml"),
	} {
		name, _ := leafString(readYAML(t, chart), iamConsumerKey[1:])
		pairs = append(pairs, iamDatabaseChartPair{chart: chart, name: name})
	}
	census.parityJudged = len(pairs)
	parity := auditIamDatabaseChartParity(pairs)

	t.Logf("перепись: стеков %d · наложений профилей %d · объявлений потребителя %d · "+
		"объявлений поставщика %d · адресов подключения %d · чартов сверено %d · "+
		"объявлений с каноническим именем %q %d",
		census.stacks, census.profiles, census.consumerJudged, census.providerJudged,
		census.dsnJudged, census.parityJudged, canonicalIamDatabase, census.canonicalHits)

	if census.stacks == 0 || census.consumerJudged == 0 || census.providerJudged == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: стеков %d, потребителей %d, поставщиков %d",
			census.stacks, census.consumerJudged, census.providerJudged)
	}

	all := append(append([]string{}, findings...), parity...)
	if len(all) > 0 {
		t.Fatalf("имя базы службы прав разошлось с именем её продукта — %d находок:\n  %s\n\n"+
			"канон объявлен константой canonicalIamDatabase в этом файле; отставленное "+
			"имя %q — вход распознавателя, а не предмет памяти",
			len(all), strings.Join(all, "\n  "), retiredIamDatabase)
	}

	require.NotZero(t, census.canonicalHits,
		"положительный контроль пуст: канонического имени базы не назвало ни одно "+
			"объявление — отрицание выше выполнилось бы на дереве, из которого вынесли всё")
}
