// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// proxytupleintent_test.go — гейт сверки намерения с приёмной стороной НА СБОРКЕ.
//
// # Предмет
//
// Модуль эмитирует кортеж в очередь намерений, а принимает его kaname — по
// закрытому правилу. Эмитировать и ИМЕТЬ ПРИМЕНЁННЫМ — разные вещи, и цена
// расхождения нелинейна: кортеж, который владелец отвергает, отвергается на
// КАЖДОЙ доставке, дренаж классифицирует отказ в правах как временный, строка
// никогда не помечается отправленной и заклинивает голову своей партиции на всё
// окно повторов. Наблюдаемо это не как красный тест, а как «ресурс создан, а
// доступа к нему нет» — у соседних объектов того же ресурса тоже. Реальный
// инцидент этого класса: очередь, в которой НИ ОДНА строка никогда не была
// доставлена, при внешне исправном поведении (синхронная половина доставки
// короткого замыкания не делает).
//
// # Почему гейт, а не проба у каждого сервиса
//
// Правило принадлежит владельцу и одно на всех; знание о нём у потребителя —
// импорт, а не пересказ (pkg/authz/proxytuple). Гейт спрашивает ТУ ЖЕ функцию,
// которую исполняет владелец в рантайме, поэтому он не может разойтись с ней ни
// в какую сторону — в отличие от копии набора, которая до этой волны жила в
// единственной пробе у одного сервиса и разошлась бы молча.
//
// # Что именно сверяется — ТРОЙКА, а не отношение
//
// Приём — вердикт о «субъект, отношение, тип объекта» вместе. Гейт по одному
// отношению неверен в ОБЕ стороны: он пропустил бы кортеж, отвергаемый по типу
// объекта, и покраснел бы на законной паре публичного чтения (`user:* #v_get`),
// которой ни в одном наборе иерархических отношений нет и отдельного флага
// публичности не существует — сам кортеж и есть видимость.
//
// # Две половины, и вторая обязательна
//
//   - TestProxyRegistrationTriplesAreAcceptedByOwner — всё, что дерево эмитирует,
//     владелец принимает (положительный контроль: он же доказывает, что вход у
//     гейта есть);
//   - TestEveryAcceptedRelationHasAnEmitter — у всего, что владелец принимает,
//     есть эмитент. Запись набора приёма без производителя — находка: она не
//     наблюдается ни работающей, ни сломанной, и следующий читатель принимает её
//     за действующую возможность.
//
// # Разбор идёт по синтаксическому дереву
//
// Имя отношения встречается в комментариях, объясняющих ЭТУ ЖЕ защиту, и в
// картах прав, к очереди намерений отношения не имеющих. Текстовый поиск считал
// бы их эмиссией.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
	"github.com/PRO-Robotech/kacho/pkg/platformmodules"
)

// proxyTupleFieldSet — структурная подпись значения, которое едет в
// RegisterResource/UnregisterResource: составной литерал, задающий РОВНО эти три
// поля-имени. Подпись выбрана по полям, а не по имени типа, потому что тип
// называется у каждого сервиса по-своему (`Tuple`, `FGATuple`), а имена полей
// совпадают с полями запроса и потому стабильны — applier'ы кладут их 1:1.
var proxyTupleFieldSet = []string{"SubjectID", "Relation", "Object"}

// proxyConsumerDomains — каталоги сервисов, чьи ресурсы регистрируются через
// proxy, и одновременно домен объекта каждого (он же short-name из mTLS SAN,
// который владелец сверяет с префиксом типа объекта).
//
// Перечень ВЫВЕДЕН: сервис попадает сюда, если в его дереве есть хоть один
// составной литерал подписи выше. Список ниже — только ожидание переписи, и
// TestProxyIntentCensusMatchesExpectedConsumers требует совпадения в обе
// стороны: новый эмитент, которого здесь нет, и запись, которой больше нечего
// покрывать, — обе находки.
var proxyConsumerDomains = []string{"compute", "nlb", "registry", "storage", "vpc"}

// proxyIntentSite — одна точка эмиссии, найденная в дереве.
type proxyIntentSite struct {
	Service  string
	File     string // относительно корня репозитория
	Line     int
	Subject  string // разрешённое значение либо литеральный префикс ("project:")
	Relation string
	// raw* — выражения точки, как они стоят в исходнике. Разрешаются ПОСЛЕ обхода
	// всего сервиса: константа может быть объявлена в другом файле того же
	// пакета, и разрешение по ходу чтения зависело бы от порядка обхода.
	rawSubject  ast.Expr
	rawRelation ast.Expr
	rawObject   ast.Expr
	// ObjectTypes — типы объектов, с которыми этот кортеж может уехать. Один
	// элемент, когда тип задан в самой точке; иначе — все типы объектов домена
	// сервиса, потому что конструктор принимает тип параметром.
	ObjectTypes []string
	ObjectExact bool
}

// TestProxyRegistrationTriplesAreAcceptedByOwner — всё, что дерево эмитирует в
// очередь намерений, приёмная сторона принимает.
//
// Инъекция в обе стороны проверена: точка с привилегированным отношением
// краснеет и НАЗЫВАЕТ координату; законный близнец той же формы (пара публичного
// чтения registry, которая живёт в дереве и которую гейт «по отношению» отверг
// бы) — молчит.
func TestProxyRegistrationTriplesAreAcceptedByOwner(t *testing.T) {
	root := repoRoot(t)
	sites, files := collectProxyIntentSites(t, root)

	if len(sites) == 0 {
		t.Fatal("в дереве не найдено НИ ОДНОЙ точки эмиссии кортежа — у гейта нет входа. " +
			"Либо подпись составного литерала (SubjectID/Relation/Object) перестала " +
			"совпадать с тем, как пишутся намерения, либо эмиссия ушла из дерева. " +
			"Молчание в этом состоянии означало бы «ничего не проверено», а не «всё в порядке»")
	}

	var findings []string
	triples := 0
	for _, s := range sites {
		for _, ot := range s.ObjectTypes {
			triples++
			// Объект собирается с непустым идентификатором: правило требует его
			// наличия, а гейт спрашивает про ТИП, а не про конкретную строку.
			object := ot + ":x"
			if err := proxytuple.ValidateTuple(s.Service, s.Subject, s.Relation, object); err != nil {
				findings = append(findings, fmt.Sprintf(
					"%s:%d (%s): тройка {субъект %q, отношение %q, тип объекта %q} "+
						"приёмной стороной ОТВЕРГАЕТСЯ",
					s.File, s.Line, s.Service, s.Subject, s.Relation, ot))
			}
		}
	}

	t.Logf("осмотрено прод-файлов сервисов: %d; точек эмиссии: %d; проверено троек: %d; "+
		"доменов-потребителей: %v", files, len(sites), triples, servicesOf(sites))

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("намерение, которое приёмная сторона не примет (%d):\n  %s\n\n"+
			"Отказ владельца приходит на КАЖДОЙ доставке, дренаж считает его временным, "+
			"и строка заклинивает голову своей партиции на всё окно повторов — вместе с "+
			"последующими строками того же объекта. Либо кортеж строится не тем "+
			"отношением/типом, либо приёмная сторона обязана его принимать — и тогда "+
			"правится ОНА, в порядке «расширить приём → сменить эмиссию».",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// TestEveryAcceptedRelationHasAnEmitter — у каждого отношения из набора приёма
// есть производитель в дереве.
//
// Отрицание здесь стоит в паре с положительным контролем выше: без него «ничего
// не эмитируется вовсе» читалось бы как «всё принимаемое покрыто».
//
// Публичное чтение в этот счёт входит наравне с иерархическими: это тоже запись
// набора приёма, и без эмитента она означала бы возможность, которой нет.
func TestEveryAcceptedRelationHasAnEmitter(t *testing.T) {
	root := repoRoot(t)
	sites, _ := collectProxyIntentSites(t, root)

	emitted := map[string][]string{}
	for _, s := range sites {
		emitted[s.Relation] = append(emitted[s.Relation], fmt.Sprintf("%s:%d", s.File, s.Line))
	}

	accepted := make([]string, 0, len(proxytuple.HierarchicalRelations())+1)
	for _, r := range proxytuple.HierarchicalRelations() {
		accepted = append(accepted, string(r))
	}
	accepted = append(accepted, string(proxytuple.PublicReadRelation))

	var orphans []string
	for _, rel := range accepted {
		if len(emitted[rel]) == 0 {
			orphans = append(orphans, rel)
		}
	}

	t.Logf("отношений в наборе приёма: %d; с производителем: %d; эмиссия по отношениям: %s",
		len(accepted), len(accepted)-len(orphans), emissionSummary(emitted))

	if len(orphans) > 0 {
		t.Fatalf("отношение принимается, но не эмитируется НИ ОДНИМ потребителем: %v\n\n"+
			"Такая запись не наблюдается ни работающей, ни сломанной: она не может ни "+
			"пропустить законное, ни отвергнуть незаконное, потому что до неё ничего не "+
			"доезжает. При этом следующий читатель принимает её за действующую "+
			"возможность продукта и строит на ней ресурс. Исход один из двух: снять "+
			"запись, либо завести ресурс, который её эмитирует — и тогда эта проба "+
			"позеленеет сама.", orphans)
	}
}

// TestProxyIntentCensusMatchesExpectedConsumers — перепись эмитентов совпадает с
// ожидаемой в обе стороны.
//
// Смысл — объём гейта: сервис, начавший эмитировать намерения и не попавший в
// перепись, означал бы, что первые две пробы его просто не читали, а их «ноль
// находок» относилось бы к меньшему, чем кажется.
func TestProxyIntentCensusMatchesExpectedConsumers(t *testing.T) {
	root := repoRoot(t)
	sites, _ := collectProxyIntentSites(t, root)

	got := servicesOf(sites)
	want := append([]string(nil), proxyConsumerDomains...)
	sort.Strings(want)

	t.Logf("эмитенты по дереву: %v; ожидалось: %v", got, want)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("перепись эмитентов разошлась с ожидаемой: дерево=%v, ожидалось=%v.\n"+
			"Лишний в дереве — сервис, чью эмиссию гейт до сих пор не читал; лишний в "+
			"ожидании — запись, которой больше нечего покрывать.", got, want)
	}
}

// TestProxyIntentCensusPrintsIdentityCarriersToo — перепись печатает ОБЕ величины:
// писателей и носителей личности, — и падает, когда вторая меньше первой
// (задача продукта #1983, пункт 4 предиката готовности #1885).
//
// # Почему одного числа не хватает
//
// «Писателей пять» не говорит, сколько из них вообще СУДИМЫ по владению типом.
// Владение решается личностью вызывающего — коротким именем службы из
// проверенного SAN, — и решается ДВУМЯ ветвями правила приёма: словарной (тип
// известен закрытой таблице → сверяется модуль каталога) и приставочной (тип
// словарю неизвестен → сверяется домен типов объекта). Обе спрашивают ОДНО И ТО
// ЖЕ объявление (`pkg/platformmodules`), и обе отвечают «не знаю» на службу,
// которой там нет либо у которой домен типов пуст.
//
// Исходов у такого писателя два, и оба тихие. В посадке, где личность до правила
// доезжает, ему отвергается КАЖДЫЙ кортеж — и наблюдается это как «ресурс создан,
// доступа к нему нет», а не как отказ гейта. В посадке, где не доезжает,
// вызывающий проходит ветвь пустого домена, и владение не проверяется ВОВСЕ —
// то есть неотличимо от писателя, у которого владение проверено и совпало.
//
// # Почему величина ВЫВЕДЕНА, а не выписана
//
// Множество писателей даёт обход дерева (те же точки эмиссии, что и у двух проб
// выше), а судимость каждого — то самое объявление, которое читает правило в
// рантайме. Выписанное число M разошлось бы с деревом молча: новый эмитент, чьё
// короткое имя в объявлении отсутствует, не изменил бы его никак.
//
// Инъекция в обе стороны — proxytupleintent_injection_test.go.
func TestProxyIntentCensusPrintsIdentityCarriersToo(t *testing.T) {
	root := repoRoot(t)
	sites, files := collectProxyIntentSites(t, root)

	writers := servicesOf(sites)
	if len(writers) == 0 {
		t.Fatal("писателей ноль — вторая величина считалась бы от пустого множества, " +
			"и её «носителей столько же» получено даром")
	}
	carriers, blind := proxyIdentityCarriers(writers)

	t.Logf("осмотрено прод-файлов сервисов: %d; писателей: %d %v; носителей личности: %d %v",
		files, len(writers), writers, len(carriers), carriers)

	if len(blind) > 0 {
		t.Fatalf("писатель есть, а личности, по которой судится владение типом, у него НЕТ (%d из %d): %v\n\n"+
			"Владение решают обе ветви правила приёма по одному объявлению "+
			"(pkg/platformmodules): словарная сверяет модуль каталога, приставочная — "+
			"домен типов объекта; служба, которой там нет либо у которой домен типов "+
			"пуст, не судима ни одной.\n"+
			"Исход тихий в обе стороны: там, где личность доезжает, такому писателю "+
			"отвергается КАЖДЫЙ кортеж (наблюдается как «ресурс создан, доступа нет»); "+
			"там, где не доезжает, владение не проверяется вовсе — и это неотличимо от "+
			"проверенного и совпавшего.\n"+
			"Исход один из двух: объявить службу в pkg/platformmodules всеми тремя "+
			"написаниями — либо снять её эмиссию.",
			len(blind), len(writers), blind)
	}
}

// proxyIdentityCarriers делит писателей на тех, чья личность до правила владения
// ДОЕЗЖАЕТ, и тех, чья — нет.
//
// Носитель — служба, объявленная в словаре написаний И несущая непустой домен
// типов объекта. Оба условия сразу, а не одно: правило приёма судит владение
// двумя ветвями, и вторая (приставочная) на пустом домене отвечает «не знаю» при
// объявленной службе — так у каталога размещения, который типов объекта модели
// не имеет вовсе. Предикат «объявлена» без второй половины объявил бы его
// судимым, а он не судим.
//
// Форму нельзя сузить до «короткое имя совпадает с модулем каталога»: у
// балансировщика различны ДВА написания из трёх (служба `nlb`, модуль
// `loadbalancer`), и такой предикат отнял бы у него три живых типа.
func proxyIdentityCarriers(writers []string) (carriers, blind []string) {
	for _, w := range writers {
		_, declared := platformmodules.CatalogModuleOfService(w)
		domain, known := platformmodules.ObjectDomainOfService(w)
		if declared && known && domain != "" {
			carriers = append(carriers, w)
			continue
		}
		blind = append(blind, w)
	}
	sort.Strings(carriers)
	sort.Strings(blind)
	return carriers, blind
}

// --- разбор дерева ----------------------------------------------------------

// collectProxyIntentSites обходит прод-исходники сервисов и собирает точки
// эмиссии вместе с типами объектов их домена. Возвращает точки и число
// осмотренных файлов (перепись — отдельное утверждение, иначе «ноль находок»
// неотличимо от «ноль прочитанного»).
func collectProxyIntentSites(t *testing.T, root string) ([]proxyIntentSite, int) {
	t.Helper()

	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("читаю %s: %v", servicesDir, err)
	}

	modelTypes := collectModelObjectTypes(t, root)

	var sites []proxyIntentSite
	filesRead := 0

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		service := e.Name()
		// Владелец правила регистрирует свои кортежи напрямую, не через proxy:
		// его собственная эмиссия под это правило не подпадает и его домен не
		// сверяется. Это ограничение объёма, а не исключение из запрета.
		if service == "iam" {
			continue
		}

		consts := map[string]ast.Expr{}
		var candidates []proxyIntentSite
		objectTypes := map[string]struct{}{}

		dir := filepath.Join(servicesDir, service)
		err := rootedWalk(dir, isProdGoFile, func(abs string, body []byte) error {
			filesRead++
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, abs, body, 0)
			if perr != nil {
				return fmt.Errorf("%s: %w", abs, perr)
			}
			collectConstExprs(f, consts)
			candidates = append(candidates, collectTupleLiterals(fset, f, root, service)...)
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", dir, err)
		}

		collectFGAObjectTypes(consts, service, modelTypes, objectTypes)

		types := make([]string, 0, len(objectTypes))
		for ot := range objectTypes {
			types = append(types, ot)
		}
		sort.Strings(types)

		for _, c := range candidates {
			c.Relation, _ = evalString(c.rawRelation, consts, 0)
			c.Subject, _ = evalString(c.rawSubject, consts, 0)
			if ot, ok := objectTypeOf(c.rawObject, consts); ok {
				c.ObjectTypes = []string{ot}
				c.ObjectExact = true
			}
			if !c.ObjectExact {
				if len(types) == 0 {
					t.Fatalf("%s:%d: тип объекта задан параметром, а объявленных "+
						"типов объектов домена %q в дереве не найдено — тройку не из чего "+
						"собрать, и молчание гейта здесь означало бы непрочитанное",
						c.File, c.Line, service)
				}
				c.ObjectTypes = types
			}
			sites = append(sites, c)
		}
	}

	sort.Slice(sites, func(i, j int) bool {
		if sites[i].File != sites[j].File {
			return sites[i].File < sites[j].File
		}
		return sites[i].Line < sites[j].Line
	})
	return sites, filesRead
}

func isProdGoFile(rel string) bool {
	return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
}

// collectConstExprs собирает объявленные в файле константы как ВЫРАЖЕНИЯ, а не
// как значения: имя типа объекта в этом дереве бывает собрано из другой константы
// (`FGASubjectTypeUser + ":*"`), и снимать значение по ходу чтения файла значило бы
// зависеть от того, в каком файле пакета что объявлено.
func collectConstExprs(f *ast.File, out map[string]ast.Expr) {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					out[name.Name] = vs.Values[i]
				}
			}
		}
	}
}

// objectTypeRe — форма имени типа объекта модели. Нужна как фильтр: без неё
// формат `"%s:%s"` первого аргумента `fmt.Sprintf` прошёл бы за тип «%s», и точка
// с параметрическим типом молча проверилась бы по одной выдуманной тройке вместо
// всех типов домена.
var objectTypeRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// collectFGAObjectTypes отбирает типы объектов домена сервиса среди его констант:
// значения вида `<домен>_<...>`, ОБЪЯВЛЕННЫЕ В МОДЕЛИ.
//
// Пересечение с моделью существенно, а не косметично. Под форму `<домен>_<...>`
// в этих сервисах попадают ещё и имена таблиц-очередей (`<домен>_outbox` и
// подобные): они не являются типами объектов ни в каком смысле, и без пересечения
// точка с параметрическим типом проверялась бы по выдуманным тройкам. Отвергнуть
// законное это не могло бы (домен у них тот же), но перепись врала бы про объём —
// а число проверенных троек здесь и есть утверждение об объёме.
func collectFGAObjectTypes(consts map[string]ast.Expr, service string, modelTypes map[string]struct{}, out map[string]struct{}) {
	prefix := service + "_"
	for _, e := range consts {
		v, complete := evalString(e, consts, 0)
		if !complete || !strings.HasPrefix(v, prefix) || !objectTypeRe.MatchString(v) {
			continue
		}
		if _, declared := modelTypes[v]; !declared {
			continue
		}
		out[v] = struct{}{}
	}
}

// collectModelObjectTypes — имена типов, объявленных в модели прав. Кортеж,
// объект которого не такого типа, модель отвергает независимо от правила приёма,
// поэтому именно это множество ограничивает разбор.
func collectModelObjectTypes(t *testing.T, root string) map[string]struct{} {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, fgaModelPath))
	if err != nil {
		t.Fatalf("читаю модель: %v", err)
	}
	out := map[string]struct{}{}
	for _, line := range strings.Split(string(body), "\n") {
		if m := fgaTypeRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out[m[1]] = struct{}{}
		}
	}
	if len(out) == 0 {
		t.Fatal("в модели не найдено ни одного типа — разбор модели разошёлся с её " +
			"синтаксисом, и всякое «ноль находок» ниже относилось бы к непрочитанному")
	}
	return out
}

// collectTupleLiterals находит составные литералы подписи proxyTupleFieldSet и
// запоминает их выражения (разрешаются позже, см. rawSubject/rawRelation).
func collectTupleLiterals(fset *token.FileSet, f *ast.File, root, service string) []proxyIntentSite {
	var out []proxyIntentSite
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		fields := map[string]ast.Expr{}
		for _, el := range cl.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				return true
			}
			fields[key.Name] = kv.Value
		}
		if len(fields) != len(proxyTupleFieldSet) {
			return true
		}
		for _, want := range proxyTupleFieldSet {
			if _, ok := fields[want]; !ok {
				return true
			}
		}

		// ПЕРЕСБОРКА УЖЕ РЕШЁННОГО КОРТЕЖА — НЕ точка эмиссии.
		//
		// Литерал, все три поля которого — обращения к полям ДРУГОГО значения
		// (`t.SubjectID`, `it.Tuple.Relation`), ничего не решает: он переводит
		// кортеж, собранный где-то ещё, в форму соседнего пакета. Решение о
		// тройке принято на той, другой площадке, и она этим гейтом уже
		// покрыта — там субъект и отношение задаются конструктором домена.
		//
		// Различать это обязательно, а не удобно. Гейт не может разрешить
		// `t.SubjectID` статически, поэтому у пересборки субъект и отношение
		// выглядят ПУСТЫМИ, а пустую тройку приёмная сторона отвергает — то
		// есть каждый такой перевод давал бы находку, которой нет. Обратная
		// ошибка тоже закрыта: если хотя бы одно поле задано ЗДЕСЬ (литералом,
		// константой, вызовом), решение принимается здесь, и площадка остаётся
		// под гейтом.
		//
		// Форма завелась 2026-08-10 вместе с общей формой доставки
		// (pkg/ownerregister): пять сервисов переводят свой кортеж в неё.
		if allFieldsAreSelectors(fields) {
			return true
		}

		pos := fset.Position(cl.Pos())
		rel, _ := filepath.Rel(root, pos.Filename)
		out = append(out, proxyIntentSite{
			Service: service, File: rel, Line: pos.Line,
			rawSubject: fields["SubjectID"], rawRelation: fields["Relation"], rawObject: fields["Object"],
		})
		return true
	})
	return out
}

// allFieldsAreSelectors — все три поля литерала суть обращения к полям другого
// значения, то есть литерал ПЕРЕСОБИРАЕТ готовый кортеж, а не решает его.
//
// Признак намеренно строгий: достаточно ОДНОГО поля, заданного на месте, чтобы
// площадка снова считалась решающей. Ослабить его до «хотя бы одно поле —
// селектор» значило бы выпустить из-под гейта настоящую эмиссию, у которой
// просто один аргумент пришёл из переменной.
func allFieldsAreSelectors(fields map[string]ast.Expr) bool {
	for _, name := range proxyTupleFieldSet {
		if _, ok := fields[name].(*ast.SelectorExpr); !ok {
			return false
		}
	}
	return true
}

// objectTypeOf снимает тип объекта, когда он известен на сборке: из значения
// (`"<тип>:" + id`) либо из первого аргумента конструктора ссылки
// (`FGAObjectRef(<тип>, id)`). Когда тип приходит параметром — ok=false, и точка
// проверяется по ВСЕМ типам домена: пропустить её значило бы не проверить ничего
// именно там, где конструктор общий для нескольких ресурсов.
func objectTypeOf(e ast.Expr, consts map[string]ast.Expr) (string, bool) {
	if v, _ := evalString(e, consts, 0); v != "" {
		if i := strings.IndexByte(v, ':'); i > 0 && objectTypeRe.MatchString(v[:i]) {
			return v[:i], true
		}
	}
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) >= 2 {
		if v, complete := evalString(call.Args[0], consts, 0); complete && objectTypeRe.MatchString(v) {
			return v, true
		}
	}
	return "", false
}

// evalString вычисляет значение выражения настолько, насколько оно известно на
// сборке, и сообщает, ЦЕЛИКОМ ли оно известно. Незавершённое значение — это
// статически известный ПРЕФИКС (`"project:" + projectID` → `project:`), которого
// достаточно, чтобы снять тип объекта, и недостаточно, чтобы выдать динамическую
// строку за пару публичного чтения.
func evalString(e ast.Expr, consts map[string]ast.Expr, depth int) (string, bool) {
	if e == nil || depth > 8 {
		return "", false
	}
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.Ident:
		if inner, ok := consts[v.Name]; ok {
			return evalString(inner, consts, depth+1)
		}
		return "", false
	case *ast.SelectorExpr:
		return resolveProxyConst(v)
	case *ast.ParenExpr:
		return evalString(v.X, consts, depth+1)
	case *ast.CallExpr:
		// `string(x)` — конверсия; всё остальное вычислению не поддаётся.
		if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "string" && len(v.Args) == 1 {
			return evalString(v.Args[0], consts, depth+1)
		}
		return "", false
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, lc := evalString(v.X, consts, depth+1)
		if !lc {
			return l, false
		}
		r, rc := evalString(v.Y, consts, depth+1)
		return l + r, rc
	}
	return "", false
}

// resolveProxyConst подставляет значение константы объявления приёмной стороны.
// Импортировать пакет и спросить значение — именно то, чего нельзя было сделать,
// пока правило жило под `internal/` владельца; здесь оно берётся у САМОГО пакета,
// а не переписывается литералом.
func resolveProxyConst(sel *ast.SelectorExpr) (string, bool) {
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "proxytuple" {
		return "", false
	}
	for _, r := range proxytuple.HierarchicalRelations() {
		name := "Relation" + strings.ToUpper(string(r)[:1]) + string(r)[1:]
		if sel.Sel.Name == name {
			return string(r), true
		}
	}
	switch sel.Sel.Name {
	case "PublicReadRelation":
		return string(proxytuple.PublicReadRelation), true
	case "PublicReadSubject":
		return proxytuple.PublicReadSubject, true
	}
	return "", false
}

func servicesOf(sites []proxyIntentSite) []string {
	seen := map[string]struct{}{}
	for _, s := range sites {
		seen[s.Service] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func emissionSummary(emitted map[string][]string) string {
	keys := make([]string, 0, len(emitted))
	for k := range emitted {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, len(emitted[k])))
	}
	return strings.Join(parts, " ")
}
