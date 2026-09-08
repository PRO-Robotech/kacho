// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

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

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// removedpathcensus.go — перепись путей, СНЯТЫХ между базой и HEAD, по ВСЕМУ
// дереву.
//
// Живёт в НЕ-тестовом файле намеренно: инъекция обязана звать ТУ ЖЕ функцию,
// что и держатель, иначе она доказывает свойство своей копии.
//
// # Предмет: удаление не встречает ни одной проверки, отличающей СНЯТИЕ от ПОТЕРИ
//
// «Удалили, и всё зелено» неотличимо от «удалили, и часть проверок замолчала».
// Разница не наблюдаема ничем: проверка, уехавшая вместе со своим файлом, не
// оставляет ни висячей ссылки, ни осиротевшей пары — она просто перестаёт
// существовать, и прогон становится ЗЕЛЕНЕЕ.
//
// Соседний разбор [gateCarrierRemovalSubject] закрывает этот класс для ОДНОГО
// каталога — корпуса гейтов дерева, объявленного константой [gateCorpusDir].
// Здесь — то же для всего остального дерева, и границы между двумя разборами
// названы ниже, а не умолчаны.
//
// Цена предмета названа числом: каталог службы доступа несёт 2055 файлов Go, из
// них 139 носителей и 477 утверждений в них. Снятие каталога уносит их разом, и
// ни одна проверка дерева этого сегодня не замечает.
//
// # Две ОСИ, и они РАЗНЫЕ: перепись считает всё, находка судит носителей
//
// Ось разделена ЗАМЕРОМ, а не вкусом. Перепись по 120 переходам первого родителя
// линии (`git rev-list --first-parent -n 120 HEAD`, каждый переход сравнён со
// своим родителем формой `-M --diff-filter=D`):
//
//	что мерили                                        переходов из 120
//	снято хоть что-нибудь                                          22
//	снят хоть один файл проб Go                                    14
//	снят хоть один НОСИТЕЛЬ ГЕЙТА (предикат ниже)                   2
//	…из них ВНЕ КОРПУСА, то есть предмет ЭТОЙ находки               0
//
// Путей снято за окно 458. Оба срабатывания третьей строки — файлы корпуса
// (`internal/repohygiene/…_injection_test.go`), то есть предмет СОСЕДНЕГО
// разбора; здесь они считаются и находкой не становятся.
//
// Из этих трёх чисел следует форма, и она единственная:
//
//   - требовать объявления на КАЖДЫЙ снятый путь значило бы краснеть на каждом
//     пятом изменении линии — то есть на обычном рефакторинге. Признак,
//     краснеющий на верной работе, отключают первым, и тогда предмет остаётся
//     без всякого держателя;
//   - требовать объявления на снятого НОСИТЕЛЯ ГЕЙТА не покраснело бы ни разу за
//     то же окно.
//
// Ноль в третьей строке — НЕ слепота предиката, и это проверено двумя
// независимыми замерами, а не объявлено:
//
//   - тем же предикатом по ЖИВОМУ дереву: прочитано 6133 файла Go, носителей
//     1017, из них 263 вне корпуса. Число печатается держателем на КАЖДОМ
//     прогоне, и нулевое роняет прогон: «носителей снято 0» обязано быть
//     отличимо от «признак не срабатывает»;
//   - тот же предикат, наведённый на коммит `d46aaa7280` — тот, из-за которого
//     заведён соседний разбор корпуса, — находит снятых носителей 6 из 15 снятых
//     файлов Go: три держателя гейтов и три их доказательства. Остальные девять
//     носителями не были: три разбора того же семейства дерева не читают (им
//     корень передают), шесть — обычный прод-код и продуктовые пробы. Предикат
//     снятие ВИДИТ.
//
// Отсюда: перепись ИМЕНУЕТ всё снятое и печатает числа; находкой становится
// только снятый носитель гейта, не покрытый объявлением. Ординарное удаление
// прод-кода и продуктовых проб проходит молча — но НЕ незаметно: оно посчитано и
// названо в переписи.
//
// # Чем объявляется осознанное снятие — идиом ВЫВЕДЕН из дерева, а не изобретён
//
// В дереве три надгробия, и все три об одном: `retired.json` у миграций сервиса,
// `dropguard.json` у сносимых таблиц, [gateCarrierLedgerName] у носителей гейтов
// корпуса. Форма записи у всех одна — ЧТО снято · ПОЧЕМУ · ЧТО СТАЛО С ПРЕДМЕТОМ,
// и у всех трёх запись самоистекает в обе стороны.
//
// Четвёртого идиома здесь не заводится. Берётся ТО ЖЕ надгробие
// [gateCarrierLedgerName] — снятие носителя и объявление снятия обязаны
// правиться одним изменением, а два надгробия об одном предмете разошлись бы
// молча. Меняется только ЗЕРНО объявления:
//
//	Carrier — ТОЧНЫЙ путь. Единица корпуса: там носителей десятки, и запись на
//	          каждого стоит строки.
//	Scope   — ПРИСТАВКА пути. Единица дерева: снятие каталога службы уносит 53
//	          носителя разом, и 53 записи об одном решении есть перечень имён, а
//	          не объявление.
//
// Обе формы объясняют снятие. Запись, не несущая ни одной из них либо несущая
// обе, — находка: неизвестно, что она объявляет.
//
// # Самоистечение записи-приставки — в ОБЕ стороны, как у остальных трёх
//
//   - приставка, под которой в дереве ЕЩЁ ЕСТЬ отслеживаемые пути, прикрывала бы
//     живую координату: она объявляет снятым то, что не снято. Находка;
//   - приставка, под которой не снимали НИЧЕГО за всю достижимую от HEAD
//     историю, покрывать не может ничего. Находка. Без этой оси запись жила бы
//     вечно неопровержимой после того, как снятие уедет за базу.
//
// Первая ось — ЕДИНСТВЕННАЯ защита от бланкетной записи, и назвать её надо
// прямо. Приставка вида `services` покрыла бы снятие носителя у любого сервиса
// разом; она отвергается тем же предикатом, что и всякая другая, и не по особому
// случаю, а by construction: под `services` живут пути остальных сервисов, и
// запись прикрывает живые координаты. Значит бланкетная приставка проходит РОВНО
// тогда, когда под ней действительно ничего не осталось, — то есть перестаёт
// быть бланкетной.
//
// # Границы названы, а не умолчаны
//
//  1. НОСИТЕЛИ КОРПУСА судит соседний разбор ([gateCorpusDir]), и здесь они из
//     находок исключены. Не послабление, а разведение предметов: иначе одно
//     снятие давало бы две находки, и починка одной оставляла бы вторую.
//     Перепись их всё равно СЧИТАЕТ и печатает — «ноль находок» не должно
//     означать «не смотрели».
//
//  2. СУДИТСЯ ТОЛЬКО Go. Носитель распознаётся разбором синтаксического дерева
//     файла на БАЗОВОЙ ревизии либо именем файла; для не-Go нет ни того, ни
//     другого, и они в находки не попадают. Что остаётся вне наблюдения, названо числом переписи
//     («не-Go снято N»), а не умолчано. Носители на оболочке держит своя
//     ведомость — `deploy/scripts/run-injection-proofs.sh`, где запись, чей файл
//     исчез, объявляется находкой; второго дома им здесь не заводится.
//
//  3. ВЫХОЛАЩИВАНИЕ БЕЗ УДАЛЕНИЯ здесь не ловится: файл на месте, тело
//     утверждения вырезано. Это соседний класс, и его держат инъекция и
//     требование доказывать способность падать повторно после переустройства.
//
//  4. СНЯТИЕ ОТДЕЛЬНОГО `func Test` ИЗ СОХРАНЁННОГО ФАЙЛА — молчание. Прямая
//     цена выбора файла единицей счёта; тот же предел объявляет и соседний
//     разбор корпуса, и по той же измеренной причине.
const removedPathCensusSubject = "перепись снятых путей по дереву"

// gateCarrierRemovalSubject — имя соседнего предмета, названное здесь ссылкой, а
// не пересказом: разбор снятия носителей КОРПУСА живёт в gatecarrierremoval.go.
const gateCarrierRemovalSubject = "снятие носителя гейта корпуса"

// treeReadingImports — пакеты, которыми проба ЧИТАЕТ СОСТАВ ДЕРЕВА.
//
// Импорт — узел синтаксического дерева, а не слово: он не встречается ни в
// комментарии, ни в строковом литерале, поэтому предикат по нему не путает
// предмет с его описанием.
//
// Перечень закрытый и мал намеренно: оба пакета существуют ровно затем, чтобы
// читать дерево (`treecorpus` — состав по индексу git, `gitenv` — сам git), и
// продуктовой пробе они не нужны ни для чего.
var treeReadingImports = map[string]bool{
	"github.com/PRO-Robotech/kacho/pkg/treecorpus": true,
	"github.com/PRO-Robotech/kacho/pkg/gitenv":     true,
}

// treeRootFinderRe — ФОРМА имени функции, разрешающей корень репозитория.
//
// Второй признак носителя, независимый от первого. Нужен потому, что часть
// гейтов дерева поднимается до корня своим помощником и `treecorpus` не зовёт:
// вне корпуса перепись даёт 99 файлов по импортам и 263 по объединению всех трёх
// признаков — то есть один только импорт оставил бы вне наблюдения 164 носителя
// из 263.
//
// Судится ИДЕНТИФИКАТОР вызываемой функции (узел разбора), а не текст файла:
// слово `repoRoot` встречается в комментариях к таким пробам чаще, чем в их
// коде.
//
// Форма, а не перечень: имён у этого помощника в дереве два десятка
// (`repoRoot`, `repoRootFor`, `repoRootForDoc`, `RepoRoot`, …), и выписанный
// перечень разошёлся бы с деревом молча.
var treeRootFinderRe = regexp.MustCompile(`^(?i)repo(sitory)?Root[A-Za-z0-9_]*$`)

// injectionProofSuffix — ИМЯ файла, которым дерево называет доказательство
// способности гейта падать.
//
// Третий признак носителя, и он не про содержимое, а про ПУТЬ. Судить путь здесь
// законно и не подпадает под запрет «гейт читает исполняемую часть, а не текст»:
// имя файла есть факт дерева, а не строка внутри него, и подделать его
// комментарием нельзя. Тем же способом дерево опознаёт доказательства на
// оболочке — `deploy/scripts/run-injection-proofs.sh` собирает их по суффиксу
// имени.
//
// Признак нужен потому, что доказательство дерева НЕ ЧИТАЕТ: оно строит
// синтетику во временном каталоге и подаёт её разбору. Первые два признака его
// не видят by construction, и цена этого измерена: из 78 доказательств каталога
// службы они опознают 6, то есть 72 уехали бы невидимо — при том что снятие
// доказательства и есть та правка, после которой гейт теряет способность падать
// молча.
const injectionProofSuffix = "_injection_test.go"

// removedPath — один снятый путь и то, что о нём известно.
type removedPath struct {
	// Path — путь от корня дерева, ровно в том виде, в каком его называет git.
	Path string
	// CarriedGate — файл на БАЗОВОЙ ревизии нёс утверждение о дереве.
	CarriedGate bool
	// Assertions — сколько утверждений (`func Test`/`Fuzz`/`Benchmark`) уехало
	// вместе с файлом. Считается только у носителей.
	Assertions int
	// Why — чем именно файл опознан носителем. Перепись обязана называть не
	// только число, но и признак: иначе «носителей 0» неотличимо от «признак
	// не сработал».
	Why string
}

// GateCarrierScopeRetirement — одна запись надгробия, объявляющая снятие
// ПРИСТАВКИ пути.
//
// Соседняя форма ([GateCarrierRetirement]) объявляет снятие точного пути. Обе
// живут в одном надгробии и читаются одним читателем: два надгробия об одном
// предмете разошлись бы молча.
type GateCarrierScopeRetirement struct {
	// Scope — приставка пути от корня дерева. Каталог называется БЕЗ хвостовой
	// косой черты: `services/iam`, а не `services/iam/`.
	Scope string `json:"scope"`
	// Reason — почему снято. Пустая причина превращает надгробие в список имён.
	Reason string `json:"reason"`
	// Successor — кто держит предмет теперь либо чем доказано, что предмета
	// больше нет.
	Successor string `json:"successor"`
}

// removedPathsBetween — пути, СНЯТЫЕ между базой и HEAD, по всему дереву.
//
// `-M` включает распознавание переименований: переименованный файл приходит
// видом R и снятым не считается. Порог сходства считает git, а не этот код.
//
// Форма `base...HEAD` — та же, что у соседних разборов дерева: сравнение идёт от
// ТОЧКИ ОТВЕТВЛЕНИЯ, поэтому вердикт относится к текущему изменению целиком.
//
// Ограничения по каталогу здесь НЕТ намеренно: предмет — всё дерево, и константа
// охвата была бы ровно тем сужением, ради снятия которого разбор написан.
func removedPathsBetween(root, base string) ([]string, error) {
	out, err := gitenv.Command(root, "diff", "-M", "--diff-filter=D", "--name-only",
		base+"...HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("git diff от %s: %w — сравнить не с чем, "+
			"и это отказ, а не пустой успех", base, err)
	}
	var gone []string
	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if rel == "" {
			continue
		}
		gone = append(gone, rel)
	}
	sort.Strings(gone)
	return gone, nil
}

// baseTreeSize — сколько путей несла БАЗОВАЯ ревизия.
//
// Это проверка ПРЕДПОСЫЛКИ, а не украшение: «снято 0» при непрочитанном составе
// базы выглядит ровно так же, как чистая ветка. Ноль — ОТКАЗ.
func baseTreeSize(root, base string) (int, error) {
	out, err := gitenv.Command(root, "ls-tree", "-r", "--name-only", base).Output()
	if err != nil {
		return 0, fmt.Errorf("состав базы %s: %w — читать было нечего, "+
			"и это отказ, а не пустой успех", base, err)
	}
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			n++
		}
	}
	if n == 0 {
		return 0, fmt.Errorf(
			"база %s не дала ни одного пути — смотреть было не на что; "+
				"это отказ, а не пустой успех: проверь глубину клона", base)
	}
	return n, nil
}

// fileAtRev — содержимое пути на названной ревизии.
func fileAtRev(root, rev, path string) ([]byte, error) {
	out, err := gitenv.Command(root, "show", rev+":"+path).Output()
	if err != nil {
		return nil, fmt.Errorf("%s:%s: %w", rev, path, err)
	}
	return out, nil
}

// judgeCarrierSource — нёс ли снятый файл утверждение О ДЕРЕВЕ, и сколько
// утверждений уехало вместе с ним.
//
// Признаков ТРИ, и они независимы: два судят УЗЛЫ разбора (импорт — `ImportSpec`,
// вызов помощника корня — `CallExpr` с идентификатором), третий — ИМЯ файла.
// Утверждения считаются по `FuncDecl` с именем пробы.
//
// По тексту не судится ничего: предикат по тексту считал бы предмет вместе с его
// описанием — шапки таких проб объясняют, что они читают дерево, и делают это
// теми же словами.
//
// Неразбираемый источник — НЕ «не носитель»: это отказ, названный вызывающему.
// Молчание на неразобранном файле было бы тем самым «не знаю», выданным за «нет».
func judgeCarrierSource(path string, src []byte) (carrier bool, assertions int, why string, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return false, 0, "", fmt.Errorf("разобрать %s: %w", path, err)
	}

	var reasons []string
	for _, imp := range f.Imports {
		p, e := strconv.Unquote(imp.Path.Value)
		if e != nil {
			continue
		}
		if treeReadingImports[p] {
			reasons = append(reasons, "импорт "+p)
		}
	}

	rootFinders := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			name = fn.Sel.Name
		}
		if name != "" && treeRootFinderRe.MatchString(name) {
			rootFinders[name] = true
		}
		return true
	})
	if len(rootFinders) > 0 {
		names := make([]string, 0, len(rootFinders))
		for n := range rootFinders {
			names = append(names, n)
		}
		sort.Strings(names)
		reasons = append(reasons, "вызов "+strings.Join(names, ",")+"()")
	}

	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name == nil {
			continue
		}
		n := fd.Name.Name
		if strings.HasPrefix(n, "Test") || strings.HasPrefix(n, "Fuzz") ||
			strings.HasPrefix(n, "Benchmark") {
			assertions++
		}
	}

	if strings.HasSuffix(path, injectionProofSuffix) {
		reasons = append(reasons, "имя доказательства "+injectionProofSuffix)
	}

	if len(reasons) == 0 {
		return false, assertions, "", nil
	}
	sort.Strings(reasons)
	return true, assertions, strings.Join(reasons, " + "), nil
}

// classifyRemovedPaths — перепись снятого: что именно уехало и что из этого
// несло утверждение о дереве.
//
// Не-Go пути в разбор не идут (границы см. в шапке) и остаются в переписи как
// отдельное число.
func classifyRemovedPaths(root, base string, removed []string) ([]removedPath, error) {
	out := make([]removedPath, 0, len(removed))
	for _, p := range removed {
		rp := removedPath{Path: p}
		if strings.HasSuffix(p, ".go") {
			src, err := fileAtRev(root, base, p)
			if err != nil {
				return nil, err
			}
			carrier, asserts, why, err := judgeCarrierSource(p, src)
			if err != nil {
				return nil, err
			}
			rp.CarriedGate, rp.Assertions, rp.Why = carrier, asserts, why
		}
		out = append(out, rp)
	}
	return out, nil
}

// removedPathCensusCounts — числа переписи. Печатаются ВСЕ: одно число скрывает
// ровно тот случай, ради которого перепись заведена.
type removedPathCensusCounts struct {
	// Removed — снято путей всего.
	Removed int
	// Go — из них файлов Go (то есть попавших под разбор).
	Go int
	// NonGo — из них не-Go (вне наблюдения разбора; названо, а не умолчано).
	NonGo int
	// Carriers — из них носителей гейта.
	Carriers int
	// CarriersInCorpus — носителей, которых судит СОСЕДНИЙ разбор.
	CarriersInCorpus int
	// Assertions — утверждений, уехавших вместе с носителями.
	Assertions int
	// DeclaredScopes — записей-приставок в надгробии.
	DeclaredScopes int
	// DeclaredCarriers — записей точного пути в надгробии.
	DeclaredCarriers int
	// Covered — носителей вне корпуса, покрытых объявлением.
	Covered int
	// Unexplained — носителей вне корпуса без объявления.
	Unexplained int
}

func (c removedPathCensusCounts) String() string {
	return fmt.Sprintf(
		"снято путей %d (Go %d · не-Go %d) · носителей гейта %d "+
			"(из них в корпусе %d — их судит %q) · утверждений уехало %d · "+
			"объявлено записями надгробия %d (приставок %d · точных путей %d) · "+
			"необъяснённых носителей %d",
		c.Removed, c.Go, c.NonGo, c.Carriers, c.CarriersInCorpus,
		gateCarrierRemovalSubject, c.Assertions,
		c.Covered, c.DeclaredScopes, c.DeclaredCarriers, c.Unexplained)
}

// pathUnderScope — покрывает ли приставка этот путь.
//
// Имя не `scopeCovers`: так уже зовётся соседний предикат покрытия ПАКЕТА
// областью сборки, и совпадение имён при разных предметах читалось бы как одно.
//
// Совпадение считается ПО СЕГМЕНТАМ, а не по строке: приставка `services/iam`
// не вправе покрывать `services/iamx/...`. Ровное равенство тоже покрытие —
// приставкой может быть назван и один файл.
func pathUnderScope(scope, path string) bool {
	return path == scope || strings.HasPrefix(path, scope+"/")
}

// livePathsUnder — сколько отслеживаемых путей дерево несёт под приставкой.
//
// Первое направление самоистечения записи. Считается по индексу git, а не
// обходом диска: вердикт обязан быть свойством коммита.
func livePathsUnder(root, scope string) (int, error) {
	out, err := gitenv.Command(root, "ls-files", "--", scope).Output()
	if err != nil {
		return 0, fmt.Errorf("состав дерева под %s: %w", scope, err)
	}
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			n++
		}
	}
	return n, nil
}

// scopeProbes — то, что суждению нужно спросить у git о приставке. Передаётся, а
// не зовётся напрямую, чтобы суждение осталось проверяемым без репозитория.
type scopeProbes struct {
	LiveUnder   func(scope string) (int, error)
	EverRemoved func(scope string) (bool, error)
}

// judgeRemovedPathCensus — ЧИСТОЕ суждение: что из снятого объявлено, а что нет,
// и какие записи надгробия потеряли предмет.
//
// Разведено с добычей входа намеренно: инъекция гоняет эту функцию на настоящем
// синтетическом репозитории, а не на её копии.
func judgeRemovedPathCensus(
	removed []removedPath, l *gateCarrierLedger, probes scopeProbes,
) (findings []string, counts removedPathCensusCounts) {
	counts.Removed = len(removed)

	declaredExact := map[string]bool{}
	var scopes []GateCarrierScopeRetirement
	if l != nil {
		counts.DeclaredCarriers = len(l.Retired)
		for _, r := range l.Retired {
			if r.Carrier != "" {
				declaredExact[r.Carrier] = true
			}
		}

		seen := map[string]bool{}
		rows := append([]GateCarrierScopeRetirement(nil), l.RetiredScopes...)
		sort.Slice(rows, func(i, j int) bool { return rows[i].Scope < rows[j].Scope })
		for _, r := range rows {
			s := strings.TrimSuffix(strings.TrimSpace(r.Scope), "/")
			switch {
			case s == "":
				findings = append(findings, fmt.Sprintf(
					"%s: запись-приставка без приставки — объявлять нечего",
					gateCarrierLedgerName))
				continue
			case seen[s]:
				findings = append(findings, fmt.Sprintf(
					"%s: приставка %q названа дважды", gateCarrierLedgerName, s))
				continue
			}
			seen[s] = true

			// Самоистечение, направление первое: под приставкой ЕЩЁ ЕСТЬ живые
			// пути — запись прикрыла бы живую координату.
			if probes.LiveUnder != nil {
				n, err := probes.LiveUnder(s)
				if err != nil {
					findings = append(findings, fmt.Sprintf(
						"%s: состав дерева под %q прочитать не удалось (%v) — "+
							"это отказ, а не пустой успех", gateCarrierLedgerName, s, err))
					continue
				}
				if n > 0 {
					findings = append(findings, fmt.Sprintf(
						"%s: числит снятой приставку %q, а под ней в дереве ЕЩЁ %d путей — "+
							"эта запись прикрыла бы живые координаты; снимите запись "+
							"либо сузьте приставку", gateCarrierLedgerName, s, n))
					continue
				}
			}
			// Самоистечение, направление второе: под приставкой не снимали
			// НИЧЕГО — записи нечего покрывать.
			if probes.EverRemoved != nil {
				was, err := probes.EverRemoved(s)
				if err != nil {
					findings = append(findings, fmt.Sprintf(
						"%s: историю %q прочитать не удалось (%v) — это отказ, а не пустой успех",
						gateCarrierLedgerName, s, err))
					continue
				}
				if !was {
					findings = append(findings, fmt.Sprintf(
						"%s: под приставкой %q не снимали НИЧЕГО — в истории, достижимой от "+
							"HEAD, нет коммита, убравшего хотя бы один путь под ней. Строке "+
							"нечего покрывать: она объявляет снятие, которого не было; "+
							"снимите запись либо исправьте приставку",
						gateCarrierLedgerName, s))
					continue
				}
			}
			if strings.TrimSpace(r.Reason) == "" {
				findings = append(findings, fmt.Sprintf(
					"%s: приставка %q снята без причины — запись ничего не объявляет",
					gateCarrierLedgerName, s))
				continue
			}
			if strings.TrimSpace(r.Successor) == "" {
				findings = append(findings, fmt.Sprintf(
					"%s: приставка %q снята, но не сказано, что стало с предметом снятого — "+
						"назовите держателя либо чем доказано, что предмета нет",
					gateCarrierLedgerName, s))
				continue
			}
			scopes = append(scopes, GateCarrierScopeRetirement{Scope: s, Reason: r.Reason, Successor: r.Successor})
		}
		counts.DeclaredScopes = len(scopes)
	}

	corpus := gateCorpusDir
	for _, rp := range removed {
		if strings.HasSuffix(rp.Path, ".go") {
			counts.Go++
		} else {
			counts.NonGo++
		}
		if !rp.CarriedGate {
			continue
		}
		counts.Carriers++
		counts.Assertions += rp.Assertions

		// Носителей КОРПУСА судит соседний разбор — здесь они только считаются.
		if pathUnderScope(corpus, rp.Path) {
			counts.CarriersInCorpus++
			continue
		}

		covered := declaredExact[rp.Path]
		if !covered {
			for _, s := range scopes {
				if pathUnderScope(s.Scope, rp.Path) {
					covered = true
					break
				}
			}
		}
		if covered {
			counts.Covered++
			continue
		}
		counts.Unexplained++
		findings = append(findings, fmt.Sprintf(
			"носитель гейта %s снят МОЛЧА: в %s о нём записи нет ни точной, ни по приставке. "+
				"Он нёс %d утверждений о дереве (опознан: %s). Снятие носителя — "+
				"единственная правка, которую прогон заметить не может: проверка исчезает "+
				"вместе со своей способностью краснеть. Исходов два — вернуть носитель либо "+
				"объявить снятие записью (что снято, почему, что стало с предметом)",
			rp.Path, gateCarrierLedgerName, rp.Assertions, rp.Why))
	}

	sort.Strings(findings)
	return findings, counts
}

// liveGateCarrierTally — сколько носителей гейта дерево несёт СЕЙЧАС.
type liveGateCarrierTally struct {
	// Read — файлов Go прочитано и разобрано.
	Read int
	// Total — из них носителей.
	Total int
	// OutsideCorpus — из них вне корпуса ([gateCorpusDir]), то есть тех, кого
	// соседний разбор не видит by construction.
	OutsideCorpus int
}

// liveGateCarrierCount — КОНТРОЛЬ ПРЕДИКАТА В ОБРАТНУЮ СТОРОНУ.
//
// Перепись снятого печатает «носителей снято N». Ноль там означает одно из двух
// — «носителей не снимали» либо «признак носителя не срабатывает ни на чём», — и
// различить их нельзя ничем, кроме второго счёта: тем же признаком по ЖИВОМУ
// дереву. Если он и здесь даёт ноль, вердикт беспредметен, и это отказ.
//
// Состав берётся у индекса git ([treecorpus] через обёртку ниже), а не обходом
// диска: вердикт обязан быть свойством коммита, а не рабочего каталога.
//
// Неразбираемый файл живого дерева пропускается молча и в счёт `Read` не идёт:
// предмет этой функции — доказать, что признак СРАБАТЫВАЕТ, а не судить дерево
// на разбираемость (её судят соседи, чей это предмет).
func liveGateCarrierCount(root string) (liveGateCarrierTally, error) {
	var t liveGateCarrierTally

	out, err := gitenv.Command(root, "ls-files", "--", "*.go").Output()
	if err != nil {
		return t, fmt.Errorf("состав дерева: %w — читать было нечего, "+
			"и это отказ, а не пустой успех", err)
	}
	paths := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(paths) == 0 || (len(paths) == 1 && paths[0] == "") {
		return t, fmt.Errorf(
			"индекс не дал ни одного файла Go — смотреть было не на что; " +
				"это отказ, а не пустой успех")
	}

	for _, rel := range paths {
		if rel == "" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) // #nosec G304 — путь из индекса git
		if err != nil {
			continue // файл индекса без рабочей копии — не предмет этой функции
		}
		carrier, _, _, err := judgeCarrierSource(rel, src)
		if err != nil {
			continue
		}
		t.Read++
		if !carrier {
			continue
		}
		t.Total++
		if !pathUnderScope(gateCorpusDir, rel) {
			t.OutsideCorpus++
		}
	}
	return t, nil
}
