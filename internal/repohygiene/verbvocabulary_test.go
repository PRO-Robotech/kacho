// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verbvocabulary_test.go — гейт: КАЖДЫЙ литеральный словарь глаголов в дереве
// обоснован моделью прав, и ни один не появляется незамеченным.
//
// Предмет. Ось ТИПОВ давно привязана к модели: гейт дрейфа требует точного
// равенства в обе стороны. Ось ГЛАГОЛОВ не сверялась ни с чем: её сторожили
// литералы, каждый из которых объявлял ожидаемое и ни один — на каком основании
// именно это. Литерал чинится дописыванием в себя, поэтому СОГЛАСОВАННОЕ
// расширение всех литералов проходило молча. Гейт спрашивает МОДЕЛЬ.
//
// Почему предикат — ВХОЖДЕНИЕ, а не равенство. Пока набор глаголов был
// платформенным, каждый литерал претендовал на «все глаголы», и равенство было
// верным требованием. С набором У ТИПА претензия исчезла: набор типа, порядок
// показа, общий для всех ресурсов словарь — все они ПОДМНОЖЕСТВА словаря модели и
// равенству не обязаны. Равенство здесь краснело бы на законном расширении набора
// ОДНОГО типа, то есть сторожило бы снятое допущение. Вхождение ловит ровно
// исходный дефект — литерал, выросший за пределы модели, — и молчит на законном.
//
// Полнота реестра держится ОБНАРУЖЕНИЕМ: гейт сам находит объявления-кандидаты по
// форме и требует, чтобы каждое было в реестре с указанием, ЧТО оно утверждает и
// КТО его проверяет. Иначе реестр описывал бы вчерашнее дерево, а новый литерал
// въезжал бы молча — тем самым способом, которым въехали нынешние.
//
// Гейт НИЧЕГО не импортирует и читает обе стороны разбором исходного текста: он
// достаёт неэкспортируемые переменные, до которых символом не добраться, и не
// делает ни одну из сторон источником истины для другой.
//
// Разбор идёт через go/ast, а не регуляркой: предмет — объявление переменной, и
// текстовый поиск нашёл бы то же имя в комментарии, который её объясняет.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// canonicalVerbModelRelPath — каноническая модель прав относительно корня репо.
// Тот же файл, который сторожит гейт дрейфа iam; здесь он читается независимо,
// потому что гейт не импортирует ни один пакет сервиса.
const canonicalVerbModelRelPath = "proto/kacho/cloud/iam/v1/fga_model.fga"

// verbRelationPrefix — приставка, по которой имя отношения модели опознаётся как
// глагольное. Она же — форма, в которой глагол попадает в кортеж.
const verbRelationPrefix = "v_"

// verbLiteralScanRoots — ОБЛАСТЬ обнаружения. Расширение области — отдельная
// работа, а не строчка сюда.
var verbLiteralScanRoots = []string{"services", "internal", "gateway"}

// verbLiteral — запись реестра.
//
// Запись обязана называть, ЧТО литерал утверждает и КТО его проверяет по существу.
// «Проверяется где-то» не годится: реестр тогда сам становится описанием вчерашнего
// дерева. Запись, которой больше нечего сверять, — находка, а не «просто устарела».
type verbLiteral struct {
	path       string // путь от корня репо
	varName    string // имя объявления верхнего уровня
	claims     string // ЧТО литерал утверждает о себе
	checkedBy  string // КТО проверяет это утверждение по существу
	retireWhen string // условие снятия, привязанное к ВНЕШНЕМУ факту
}

// verbLiteralRoster — все объявления-словари глаголов верхнего уровня. Полнота
// держится обнаружением (TestVerbVocabularyRosterCoversEveryLiteral), а не
// добросовестностью того, кто сюда дописывает.
var verbLiteralRoster = []verbLiteral{
	// ЗДЕСЬ СТОЯЛИ ЧЕТЫРЕ ЗАПИСИ — наборы `v_*` пакета authzmap. Их предмет снят
	// вместе с литералами: обе таблицы типов ПОРОЖДАЮТСЯ из манифестов модулей
	// (#1092, `services/iam/internal/authzmapgen`), и наборы перечислены в
	// порождённом `tables_gen.go` поштучно у каждого типа — то есть ровно то
	// условие снятия, которое эти записи и называли.
	//
	// Записи сняты, а не поправлены: реестр стережёт РУКОПИСНЫЙ литерал, который
	// кто-то может разойтись с моделью. У порождённого литерала автора нет —
	// свежесть его сверяет побайтово гейт производителя
	// (authzmapgen: TestGeneratedTablesAreFresh), а согласие наборов с моделью
	// по-прежнему требует гейт дрейфа (TestDrift_TypeVerbSetsMatchModelExactly).
	{
		path:    "services/iam/internal/apps/kaname/api/permission_catalog/resource_verbs_test.go",
		varName: "previouslyOfferedToEveryResource",
		claims: "что выпадающий список редактора ролей предлагал КАЖДОМУ ресурсу до появления " +
			"словаря по ресурсу (#1128) — база сравнения «никто не потерял», а не словарь платформы. " +
			"Литерал здесь намеренный: сверка поля с его собственным источником зеленела бы при " +
			"любом сужении",
		checkedBy: "тот же файл: TestCatalogResourceVerbs_DescribeTheTypesOwnSets — каждый " +
			"глагольный ресурс предлагает не меньше прежнего, кроме поимённо суженных с причиной; " +
			"способность предиката упасть доказана рядом (resource_verbs_injection_test.go)",
		retireWhen: "объявление удалено (сравнивать с прежним предложением стало не с чем)",
	},
	{
		path: "services/iam/internal/domain/role_effective_verbs.go", varName: "verbDisplayPrecedence",
		claims:     "старшинство ПОКАЗА в превью роли; полноты НЕ утверждает — глагол вне списка идёт в хвост",
		checkedBy:  "domain: TestEffectiveVerbs_UnchangedForEveryRuleShape — порядок вывода по каждой форме правила",
		retireWhen: "объявление удалено (порядок берётся откуда-то ещё)",
	},
	{
		path: "services/iam/internal/modelrender/render.go", varName: "canonicalVerbOrder",
		claims: "ПОРЯДОК, в котором канон ставит глагольные отношения внутри блока типа, — " +
			"и только он. Ни полноты словаря глаголов платформы, ни правила класса из имени " +
			"объявление НЕ утверждает: членство объявляет загрузчик манифеста " +
			"(canonicalVerbClasses), а здесь стоит порядок, потому что предмет у него другой " +
			"и живёт он в каноне. Замер, из которого выведен: у 24 блоков из 27 порядок " +
			"`get list update delete`, у одного `create` третьим, у одного два глагола " +
			"управления составом ПОСЛЕ операций над объектом. Прочие глаголы идут после " +
			"канонических в порядке документа, а не отсортированно",
		checkedBy: "modelrender: TestCanonicalVerbOrderAgreesWithTheClassRule — равенство " +
			"множеств с набором загрузчика В ОБЕ СТОРОНЫ (глагол, известный одному и " +
			"неизвестный другому, уехал бы в хвост «прочих» и развёл бы блок с каноном); " +
			"плюс TestB01RenderOfARealResourceIsByteEqualToTheCanonBlock и " +
			"TestB07TheUnreachableRemainderIsNamedByNumber — порядок проверяется ПОБАЙТОВОЙ " +
			"сверкой с каноном из дерева, а не сверкой с литералом рядом",
		retireWhen: "объявление удалено (порядок отношений внутри блока перестал быть " +
			"свойством канона либо рендер берёт его у производителя раздела — #1092)",
	},
	{
		path: "services/iam/internal/manifest/resources.go", varName: "canonicalVerbClasses",
		claims: "закрытый перечень КЛАССОВ действия манифеста домена и одновременно " +
			"ЕДИНСТВЕННОЕ в дереве объявление правила «класс из имени» (#1778). Полноты " +
			"словаря глаголов платформы НЕ утверждает: каталог несёт 324 неосвобождённые " +
			"строки, из них канонический глагол — у 197, а у 95 класс не выводится ни одним " +
			"правилом и назван в манифесте явно. Совпадение перечня классов с перечнем " +
			"канонических имён — построение, а не случай: класс, невыводимый ни из одного " +
			"канонического имени, никем не производился бы",
		checkedBy: "internal/repohygiene: TestVerbClassRuleIsDeclaredOnce — правило объявлено " +
			"в дереве ровно один раз, и распознаватель отличает его от порядка показа глаголов " +
			"и от классификатора яруса (доказано инъекцией по четырём осям, " +
			"manifestverbclassrule_injection_test.go); плюс manifest: " +
			"TestMODMR03VerbsParseInBothFormsAndClassComesFromOneRule — обе формы записи " +
			"глагола выводят класс ТОЙ ЖЕ функцией",
		retireWhen: "объявление удалено (класс действия перестал выводиться из имени либо " +
			"перечень классов переехал к производителю раздела — #1092)",
	},
	{
		path: "services/iam/internal/domain/rule_verbs_test.go", varName: "scopeTypeVerbs",
		claims:     "набор глаголов ТИПА якоря привязки на этой стадии — фикстура домена, не словарь платформы",
		checkedBy:  "тот же файл: разворот подстановки на якоре",
		retireWhen: "объявление удалено",
	},
	{
		path: "services/iam/internal/domain/scope_self_admin_cascade_source_test.go", varName: "anchorTypeVerbs",
		claims:     "то же во внешнем тестовом пакете домена",
		checkedBy:  "тот же файл: вывод яруса на якоре",
		retireWhen: "объявление удалено",
	},
	{
		path: "services/iam/internal/domain/role_effective_verbs_typeset_test.go", varName: "commonVocabulary",
		claims: "ЗАПАСНОЙ словарь, который вызывающий подаёт в `WithCommonFallback`, — " +
			"вход фикстуры домена, а не суждение о платформе. Ни полноты словаря глаголов, " +
			"ни набора какого-либо типа объявление НЕ утверждает: в проде запасной словарь " +
			"НЕ литерал вовсе — его вычисляет `catalog.Facts.RolePreviewLookup` объединением " +
			"наборов живых типов, и объединением, а не пересечением, чтобы правка чужого " +
			"типа не сужала обещание роли `*.*`. Пятёрка здесь взята как обозримый вход: " +
			"кейсы файла различают ФОРМУ правила (подстановка против названного ресурса), " +
			"а не состав словаря",
		checkedBy: "тот же файл, ПАРОЙ кейсов, и по существу проверяется ПРОНОС значения " +
			"обёрткой, а не сама пятёрка: TestAuthoredVerbs_WildcardRuleFallsBackToCommon — " +
			"правило-подстановка получает поданный словарь (положительный контроль); " +
			"TestAuthoredVerbs_NamedUnresolvedTypeContributesNothing — названный " +
			"нерезолвящийся ресурс не получает НИЧЕГО (#1814). Без первого второе зеленело " +
			"бы и на обёртке, не работающей вовсе; без второго снятие ресурса РАСШИРЯЛО бы " +
			"превью роли до глаголов всей платформы. Сверху словарь ограничен моделью: " +
			"TestVerbVocabularyLiteralsMatchModel в этом же файле гейта",
		retireWhen: "объявление удалено (фикстура берёт запасной словарь у производителя — " +
			"`catalog.Facts.RolePreviewLookup` — вместо того чтобы объявлять его литералом)",
	},
	{
		path:    "services/iam/internal/repo/kaname/pg/applied_type_reaches_the_verdict_integration_test.go",
		varName: "verdictProbeVerbs",
		claims: "набор действий СИНТЕТИЧЕСКОГО ресурса, объявляемого манифестом в пробе " +
			"последней мили DoD-1 (#1968); ни полноты словаря платформы, ни набора живого типа " +
			"НЕ утверждает. Величина здесь — вход манифеста, и совпадает она с набором " +
			"поставляемого соседа НАМЕРЕННО: две пробы файла обязаны отличаться ровно тем, что " +
			"проверяется (знает ли тип сборка), а не ещё и составом действий",
		checkedBy: "тот же файл: " +
			"TestDoD1_TypeUnknownToTheBuildReachesTheVerdictThroughTheComposedModel — " +
			"проекция роли по заведённому типу обязана нести РОВНО столько пар, сколько " +
			"объявлено здесь (require.Lenf), то есть литерал сверяется с тем, что произвёл " +
			"настоящий применитель, а не сам с собой",
		retireWhen: "объявление удалено (проба снята либо берёт набор действий у манифеста, " +
			"а не объявляет его)",
	},
	{
		path: "services/iam/internal/repo/kaname/pg/relverdict/xc12f5_labelcost_test.go", varName: "f5Verbs",
		claims: "множитель M замера стоимости XC-12 Ф5 — сколько глаголов раздаёт ОДНА роль " +
			"в измеряемом сценарии; ни полноты платформы, ни набора типа НЕ утверждает. Кривая " +
			"снимается по N при неизменных M и S, поэтому величина здесь — параметр замера, " +
			"а не суждение о модели",
		checkedBy: "прогон замера: роль с этими глаголами кладётся в role_verb, и до первой " +
			"измеренной величины проходят два контроля — посторонний на объекте набора получает " +
			"отказ, субъект правила получает разрешение. Глагол, которого модель у типа не " +
			"объявляет, не дал бы разрешения, и контроль уронил бы прогон вместо того, чтобы " +
			"напечатать время неверного ответа",
		retireWhen: "объявление удалено (замер снят либо берёт множитель M откуда-то ещё)",
	},
}

// TestVerbVocabularyLiteralsMatchModel — несущий гейт оси глаголов.
func TestVerbVocabularyLiteralsMatchModel(t *testing.T) {
	root := repoRoot(t)

	// --- предпосылка гейта: модель действительно разобрана ---
	model, defines := modelVerbVocabulary(t, root)
	if len(model) == 0 {
		t.Fatalf("из канонической модели %s не выведено ни одного глагола — предпосылка "+
			"гейта сломана, и его молчание ничего не доказывает (корень=%s)",
			canonicalVerbModelRelPath, root)
	}
	t.Logf("перепись: из модели выведено глаголов: %d (%s); объявлений `define %s*` прочитано: %d",
		len(model), strings.Join(model, ", "), verbRelationPrefix, defines)

	// --- перепись осмотренного: ноль находок ≠ ноль прочитанного ---
	if len(verbLiteralRoster) == 0 {
		t.Fatalf("реестр литеральных словарей пуст — гейту нечего сверять; "+
			"пустой реестр молчит по той же причине, по какой молчит исправное дерево (корень=%s)", root)
	}
	t.Logf("перепись: литеральных словарей в реестре: %d", len(verbLiteralRoster))

	allowed := map[string]bool{}
	for _, v := range model {
		allowed[v] = true
		allowed[verbRelationPrefix+v] = true
	}

	// Каждый литерал — самостоятельный под-тест: расхождение сразу в НЕСКОЛЬКИХ
	// словарях обязано назвать ВСЕ координаты, а не первую. Ровно этот случай —
	// «правка плюс обновление зеркал» — и есть предмет гейта.
	for _, lit := range verbLiteralRoster {
		t.Run(lit.path+":"+lit.varName, func(t *testing.T) {
			got, ok := parseStringSliceVar(t, filepath.Join(root, lit.path), lit.varName)
			if !ok {
				t.Fatalf("%s: объявления %q в дереве нет. Если оно удалено законно — снимите "+
					"запись реестра (условие снятия: %s), а не игнорируйте пропажу: следующий "+
					"литерал того же имени унаследует слепую зону молча",
					lit.path, lit.varName, lit.retireWhen)
			}
			var stray []string
			for _, v := range got {
				if !allowed[normalizeVerbToken(v)] {
					stray = append(stray, v)
				}
			}
			if len(stray) != 0 {
				sort.Strings(stray)
				t.Fatalf("%s: %s содержит имена, которых каноническая модель %s не знает: %v\n"+
					"литерал: %v\nсловарь модели: %v\n"+
					"Словарь глаголов ОБОСНОВЫВАЕТСЯ моделью. Дописать имя в литерал и в его "+
					"зеркала — не исход: эмиттер начнёт писать отношение, которого в модели нет, "+
					"а владелец модели такую запись отвергает окончательно.\n"+
					"Что этот литерал утверждает: %s\nКто проверяет это по существу: %s",
					lit.path, lit.varName, canonicalVerbModelRelPath, stray, got, model,
					lit.claims, lit.checkedBy)
			}
		})
	}
}

// TestVerbVocabularyRosterCoversEveryLiteral — ПОЛНОТА реестра держится
// обнаружением, а не добросовестностью.
//
// Гейт сам находит объявления-кандидаты по ФОРМЕ (список строк верхнего уровня,
// все элементы которого — глаголы модели либо их отношения) и требует, чтобы каждое
// было в реестре. Без этого реестр описывал бы вчерашнее дерево: новый литерал
// въезжал бы молча — ровно тем способом, которым въехали нынешние.
func TestVerbVocabularyRosterCoversEveryLiteral(t *testing.T) {
	root := repoRoot(t)
	model, _ := modelVerbVocabulary(t, root)
	if len(model) == 0 {
		t.Fatalf("модель не разобрана — обнаружению не с чем сверять форму")
	}

	found, files := discoverTopLevelVerbLiterals(t, root, model)
	if files == 0 {
		t.Fatalf("не прочитано ни одного файла в %v — предпосылка обнаружения сломана, "+
			"молчание ничего не доказывает (корень=%s)", verbLiteralScanRoots, root)
	}
	t.Logf("перепись: файлов осмотрено: %d; объявлений-кандидатов найдено: %d; записей реестра: %d",
		files, len(found), len(verbLiteralRoster))
	if len(found) == 0 {
		t.Fatalf("обнаружение не нашло ни одного объявления во всём дереве — предикат формы " +
			"перестал что-либо находить; молчание при нуле кандидатов ничего не утверждает")
	}

	rostered := map[string]bool{}
	for _, lit := range verbLiteralRoster {
		rostered[lit.path+":"+lit.varName] = true
	}
	for _, key := range found {
		if rostered[key] {
			continue
		}
		t.Errorf("%s — объявление-словарь глаголов, которого НЕТ в реестре.\n"+
			"Каждый такой литерал обязан объявить, ЧТО он утверждает и КТО проверяет это по "+
			"существу: словарь, объявляющий ожидаемое сам по себе, чинится дописыванием в себя "+
			"и потому останавливает случайное расширение, но пропускает намеренное.", key)
	}
}

// TestVerbVocabularyRosterEntriesStillHaveSubject — самоистечение реестра.
//
// Запись, чьё объявление исчезло из дерева, — НАХОДКА, а не «просто устарела»:
// следующий литерал того же имени унаследует слепую зону молча. Условие снятия у
// каждой записи привязано к ВНЕШНЕМУ факту (объявлению в дереве), а не к
// наблюдаемому рядом, — иначе предикат снятия отменяется тем же изменением,
// которое его вызвало.
func TestVerbVocabularyRosterEntriesStillHaveSubject(t *testing.T) {
	root := repoRoot(t)
	if len(verbLiteralRoster) == 0 {
		t.Fatalf("реестр пуст — самоистечению нечего проверять")
	}
	alive := 0
	for _, lit := range verbLiteralRoster {
		if _, ok := parseStringSliceVar(t, filepath.Join(root, lit.path), lit.varName); !ok {
			t.Errorf("запись реестра %s:%s больше нечего исключать — объявления в дереве нет. "+
				"Это находка: снимите запись (условие снятия: %s). Оставленная запись описывает "+
				"вчерашнее дерево и молча покроет собой следующий литерал того же имени",
				lit.path, lit.varName, lit.retireWhen)
			continue
		}
		alive++
	}
	t.Logf("перепись: записей реестра с живым предметом: %d из %d", alive, len(verbLiteralRoster))
}

// TestVerbVocabularyGateCannotImportEitherSide — предпосылка НЕЗАВИСИМОСТИ,
// проверяемая самим гейтом, а не командой в документе.
//
// Формулировка обязана быть точной, иначе она ложна. Сохраняемое свойство — НЕ
// «тестовый пакет не зависит от эмиттера» (у пакета гейта дрейфа такой
// независимости нет и не было: соседние файлы того же тестового пакета импортируют
// домен). Свойство таково: ОЖИДАЕМОЕ ЗНАЧЕНИЕ этого гейта не выводится ни из одной
// сверяемой стороны — оно выводится ТОЛЬКО из модели.
//
// Первая редакция этой проверки утверждала «файл гейта не импортирует ни один пакет
// сервиса» — и была ПУСТОЙ: такой импорт отвергает сам компилятор (правило
// `internal`), то есть проверка отвечала «да» на условие, которого не бывает. Гейт
// без предмета — находка, а не защита.
//
// Проверяется поэтому ФАКТ, на котором структурная гарантия держится и который
// может тихо исчезнуть: каждая сверяемая сторона лежит там, откуда её импорт
// компилятором ЗАПРЕЩЁН. Переезд словаря в импортируемый пакет снимает гарантию
// бесшумно — с этого момента следующий контрибьютор вправе «упростить» гейт,
// подставив символ вместо разбора.
func TestVerbVocabularyGateCannotImportEitherSide(t *testing.T) {
	const gateDir = "internal/repohygiene"
	if len(verbLiteralRoster) == 0 {
		t.Fatalf("реестр пуст — проверять недостижимость нечего")
	}
	for _, lit := range verbLiteralRoster {
		if importForbiddenFrom(lit.path, gateDir) {
			continue
		}
		t.Errorf("%s: %s лежит там, откуда пакет %s ВПРАВЕ его импортировать. "+
			"Ожидаемое значение гейта обязано выводиться только из модели; пока стороны "+
			"недостижимы по правилу `internal`, это гарантировано устройством сборки. "+
			"Переезд снимает гарантию бесшумно — верните словарь под internal/ своего "+
			"сервиса либо предъявите другую гарантию", lit.path, lit.varName, gateDir)
	}
	t.Logf("перепись: сторон проверено на недостижимость из %s: %d", gateDir, len(verbLiteralRoster))
}

// importForbiddenFrom сообщает, запрещает ли правило `internal` импорт пакета,
// лежащего по пути filePath, из каталога fromDir (оба — от корня модуля).
//
// Правило: пакет под `<x>/internal/…` импортируем только изнутри `<x>`. Берётся
// ПЕРВЫЙ сегмент `internal` — он даёт самую широкую границу.
func importForbiddenFrom(filePath, fromDir string) bool {
	segs := strings.Split(filePath, "/")
	for i, s := range segs {
		if s != "internal" {
			continue
		}
		enclosing := strings.Join(segs[:i], "/") // "" → корень модуля
		if enclosing == "" {
			return false // internal/ в корне: импортируем из любого места модуля
		}
		return !(fromDir == enclosing || strings.HasPrefix(fromDir, enclosing+"/"))
	}
	return false // не под internal/ вовсе → импортируем откуда угодно
}

// TestImportForbiddenFrom_HasBothControls — контроль предиката в обе стороны.
// Без случая, который предикат обязан ПРОПУСТИТЬ, он ловил бы форму («в пути есть
// слово internal»), а не существо («компилятор запретит импорт»).
func TestImportForbiddenFrom_HasBothControls(t *testing.T) {
	const gate = "internal/repohygiene"
	cases := []struct {
		path string
		want bool
		why  string
	}{
		{"services/iam/internal/domain/rule_verbs.go", true, "под internal чужого сервиса — запрещён"},
		{"services/iam/internal/authzmap/fga_model_drift_test.go", true, "то же"},
		{"internal/repohygiene/other_test.go", false, "internal в корне модуля — разрешён"},
		{"internal/other/x.go", false, "internal в корне модуля — разрешён"},
		{"pkg/verbs/dictionary.go", false, "не под internal вовсе — разрешён"},
		{"services/iam/domain/rule_verbs.go", false, "переезд из-под internal — гарантия исчезла"},
	}
	for _, c := range cases {
		if got := importForbiddenFrom(c.path, gate); got != c.want {
			t.Errorf("importForbiddenFrom(%q, %q) = %v, ожидалось %v (%s)", c.path, gate, got, c.want, c.why)
		}
	}
	t.Logf("перепись: контрольных случаев предиката: %d (запрещающих: 2, пропускающих: 4)", len(cases))
}

// ---------------------------------------------------------------------------
// обнаружение
// ---------------------------------------------------------------------------

// discoverTopLevelVerbLiterals находит объявления `var`/`const` ВЕРХНЕГО УРОВНЯ
// вида `[]string{…}` (или массив), все элементы которых — глаголы модели либо их
// `v_*`-отношения, и возвращает ключи `путь:имя` плюс число прочитанных файлов.
//
// Верхний уровень — намеренно: локальная переменная внутри проверки есть данные
// одного случая, а не объявленный словарь. Элементов меньше двух — тоже не словарь
// (одиночное имя глагола встречается как обычный аргумент).
func discoverTopLevelVerbLiterals(t *testing.T, root string, model []string) (keys []string, files int) {
	t.Helper()
	allowed := map[string]bool{}
	for _, v := range model {
		allowed[v] = true
		allowed[verbRelationPrefix+v] = true
	}

	for _, sub := range verbLiteralScanRoots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if n := d.Name(); n == "vendor" || n == "node_modules" || n == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".pb.go") {
				return nil
			}
			files++
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				t.Fatalf("%s: разбор не удался (%v) — обнаружение не вправе трактовать "+
					"неразобранный файл как «объявлений нет»", path, perr)
			}
			rel, _ := filepath.Rel(root, path)
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || (gd.Tok != token.VAR && gd.Tok != token.CONST) {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, ident := range vs.Names {
						if i >= len(vs.Values) {
							continue
						}
						elems, ok := stringLiteralElements(vs.Values[i])
						if !ok || len(elems) < 2 {
							continue
						}
						all := true
						for _, e := range elems {
							if !allowed[normalizeVerbToken(e)] {
								all = false
								break
							}
						}
						if all {
							keys = append(keys, filepath.ToSlash(rel)+":"+ident.Name)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", base, err)
		}
	}
	sort.Strings(keys)
	return keys, files
}

// stringLiteralElements возвращает строковые элементы составного литерала
// `[]string{…}` / `[N]string{…}`; ok=false для любой другой формы.
func stringLiteralElements(expr ast.Expr) ([]string, bool) {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	switch at := lit.Type.(type) {
	case *ast.ArrayType:
		id, ok := at.Elt.(*ast.Ident)
		if !ok || id.Name != "string" {
			return nil, false
		}
	default:
		if at != nil {
			return nil, false
		}
	}
	out := make([]string, 0, len(lit.Elts))
	for _, e := range lit.Elts {
		bl, ok := e.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return nil, false
		}
		s, err := strconv.Unquote(bl.Value)
		if err != nil {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// normalizeVerbToken — приведение имени к канонической форме, ТА ЖЕ, что на путях
// эмиссии (нижний регистр + обрезка пробелов).
func normalizeVerbToken(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ---------------------------------------------------------------------------
// разбор модели
// ---------------------------------------------------------------------------

// modelVerbVocabulary возвращает отсортированный список глаголов, выведенный из
// имён отношений `v_*` канонической модели, и число прочитанных объявлений
// (перепись: «ноль глаголов» обязано быть отличимо от «ноль прочитанного»).
func modelVerbVocabulary(t *testing.T, root string) (verbs []string, defines int) {
	t.Helper()
	p := filepath.Join(root, canonicalVerbModelRelPath)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("каноническая модель %s не прочитана (%v) — у гейта нет источника истины; "+
			"это ОТКАЗ, а не пропуск: отсутствие источника и есть тот дефект, который гейт ловит",
			canonicalVerbModelRelPath, err)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		name, ok := defineRelationName(line)
		if !ok || !strings.HasPrefix(name, verbRelationPrefix) {
			continue
		}
		defines++
		set[strings.TrimPrefix(name, verbRelationPrefix)] = true
	}
	for v := range set {
		verbs = append(verbs, v)
	}
	sort.Strings(verbs)
	return verbs, defines
}

// defineRelationName достаёт имя отношения из строки вида `    define <name>: …`.
// Строка объявления типа (колонка 0) отношением не является.
func defineRelationName(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) == len(line) { // не с отступом → не тело типа
		return "", false
	}
	const kw = "define "
	if !strings.HasPrefix(trimmed, kw) {
		return "", false
	}
	rest := trimmed[len(kw):]
	i := strings.IndexByte(rest, ':')
	if i <= 0 {
		return "", false
	}
	name := strings.TrimSpace(rest[:i])
	if name == "" || strings.ContainsAny(name, " \t") {
		return "", false
	}
	return name, true
}

// ---------------------------------------------------------------------------
// разбор Go-литералов (AST, не текст)
// ---------------------------------------------------------------------------

// parseStringSliceVar возвращает значения объявления `var <name> = []string{…}`
// верхнего уровня. Разбор синтаксического дерева, а не текста: то же имя
// встречается в комментарии, который переменную объясняет, и текстовый поиск
// зеленел бы на удалённой переменной с живым комментарием.
func parseStringSliceVar(t *testing.T, path, name string) ([]string, bool) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("%s: разбор не удался (%v) — гейт не вправе трактовать неразобранный "+
			"файл как «переменной нет»", path, err)
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.VAR && gd.Tok != token.CONST) {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range vs.Names {
				if ident.Name != name || i >= len(vs.Values) {
					continue
				}
				elems, ok := stringLiteralElements(vs.Values[i])
				if !ok {
					t.Fatalf("%s: %s объявлена не списком строковых литералов — гейт читает "+
						"только `var %s = []string{…}`; смена формы объявления обязана быть "+
						"осознанной, а вычисляемый словарь этот гейт не сверяет и молчать о "+
						"нём не вправе", path, name, name)
				}
				return elems, true
			}
		}
	}
	return nil, false
}
