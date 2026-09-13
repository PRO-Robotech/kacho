// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// overappliedrename_test.go — переименование не применено там, где каталог
// так НЕ назван: координата, у которой резолвится якорь, а переименованный
// сегмент — нет.
//
// # Предмет
//
// Служба доступа получила собственное имя продукта и переименовала свои
// каталоги — слой use-case, слой хранилища и композиционный корень сменили в
// пути сегмент имени платформы на сегмент имени продукта. Координаты здесь
// намеренно не воспроизводятся: служба вынесена отдельным продуктом, её каталоги
// в этом дереве не резолвятся, и цитата такого пути стала бы ровно тем, что гейт
// и ловит. Массовая правка шире своего предмета задевает соседей, у которых
// своего имени продукта нет и чьи каталоги законно зовутся именем платформы.
// Получается координата, которая не была верна НИКОГДА: ни до правки, ни после.
//
// Это ЗЕРКАЛО класса «утверждение пережило свой предмет». Там координата была
// верна и перестала; здесь — не была верна ни в один момент времени, и потому
// её нельзя найти ни поиском по истории, ни сверкой с прежней ревизией.
// Молчит она тем же способом: комментарий не собирается, не импортируется и
// конфликта при слиянии не даёт.
//
// # Предикат: ЯКОРЬ резолвится, переименованный сегмент — нет
//
// Судится путеподобный токен, среди сегментов которого есть имя продукта
// службы. Якорь — часть токена ДО этого сегмента. Находкой считается токен, у
// которого якорь резолвится, а якорь ВМЕСТЕ с этим сегментом — нет.
//
// Тогда утверждение звучит буквально «вот существующий каталог, а в нём
// подкаталог с именем продукта», и второй половины в дереве не существует.
//
// # ТРИ АВТОРИТЕТА, а не три предиката
//
// Токен бывает двух родов, и резолвит их разное; ДЕРЕВО, которым проверяется
// остаток, зависит от того, чьё имя модуля токен назвал:
//
//	координата ДЕРЕВА   путь БЕЗ имени модуля, один из сегментов которого — имя
//	                    продукта (каталог слоя хранилища службы, каталог её
//	                    контрактов). Резолвится деревом: точным путём либо
//	                    СУФФИКСОМ отслеживаемого пути, потому что проза пишет
//	                    координату от корня службы, а не репозитория. Не нашлось
//	                    здесь — спрашивается дерево ВНЕШНЕГО модуля: имени модуля
//	                    такой токен не несёт, и координата заглушек службы в
//	                    прозе этого дерева — законное указание на чужой
//	                    репозиторий
//	путь МОДУЛЯ Go      токен, начинающийся с имени модуля, — резолвится
//	                    ОБЪЯВЛЕНИЯМИ модулей: путь абсолютен by construction,
//	                    суффиксу здесь взяться неоткуда
//	дерево ВНЕШНЕГО     решением владельца (kacho#2616, исход C, 2026-09-13)
//	модуля              контракты и заглушки службы доступа уехали в её
//	                    репозиторий и приезжают модулем ПО ТЕМ ЖЕ относительным
//	                    путям. Остаток проверяется ТОЧНЫМ путём от корня этого
//	                    модуля, без суффикса: суффикс по чужому дереву гасил бы
//	                    находки, а не находил их
//
// ДЕРЕВО ВНЕШНЕГО МОДУЛЯ ОТВЕЧАЕТ ЗА ОСТАТОК ТОЛЬКО СВОИХ ПУТЕЙ, и это решение,
// а не упущение. Путь модуля называет пакет ИМЕНЕМ модуля, поэтому остаток
// судится тем деревом, которое это имя обозначает:
//
//	имя модуля СЛУЖБЫ + остаток до заглушек — остаток проверяется деревом её
//	  модуля: там он и лежит, и это ЗАКОННАЯ форма импорта
//	имя модуля ПЛАТФОРМЫ + ТОТ ЖЕ остаток — остаток проверяется ЭТИМ деревом и
//	  не резолвится: пакет уехал под другое имя модуля, и Go по такому пути не
//	  найдёт его никогда. НАХОДКА
//
// Разреши мы второй форме резолвиться чужим деревом, гейт замолчал бы ровно о
// том, что обязан ловить: об импорте, пережившем переезд. Он её и поймал — в трёх
// файлах инъекций сразу после выноса.
//
// СОБСТВЕННАЯ ОШИБКА, НАЗВАННАЯ ЗДЕСЬ, ПОТОМУ ЧТО ГЕЙТ ЕЁ НАШЁЛ: первая редакция
// этого абзаца выписала вторую форму ЛИТЕРАЛОМ — и прогон дал находку на строке
// 62 этого самого файла. Правило §«Предмет» («координаты здесь намеренно не
// воспроизводятся») распространяется и на объяснение правила; поэтому формы
// названы словами, а не цитатами.
//
// Прежняя редакция объявляла путь модуля СЛЕПОЙ ЗОНОЙ («судить его этим
// предикатом значило бы мерить не тот авторитет») — верно про авторитет и
// неверно про вывод: авторитет для него лежит в том же дереве. Модуль пути —
// самый длинный объявленный префикс; остаток отсчитывается от КОРНЯ этого
// модуля. Сборки предикат не требует: он читает `go.mod`.
//
// Замер, из которого выведена правка (голова линии `release/kaname`, 3a3c7711;
// единица счёта — вхождение путеподобного токена с сегментом имени продукта):
// токенов 5729, судимых 990 (17 %), полоса «путь модуля» — 3836, то есть 67 %
// предмета и вчетверо больше судимой полосы.
//
// Однофакторная сверка тем же деревом двумя распознавателями: судимых
// 990 → 2744 (+1754 полосой модуля), полоса ПРЕЖНЕЙ формы неподвижна (судимых
// деревом 990 → 990, короткий якорь 824 → 824), находок 0 → 0. Прибавка была
// СЛЕПОЙ ЗОНОЙ, а не регрессией дерева — числа перемеряй, они стареют.
//
// # Второй РОД находки: пакет ВЛОЖЕННОГО модуля через путь родительского
//
// Пакет вложенного модуля РОДИТЕЛЬСКОМУ не принадлежит: путь вида
// `github.com/PRO-Robotech/<родитель>/<корень вложенного>/internal/...` не
// резолвится НИКОГДА — при том что каталог в дереве существует и предикат
// «якорь+сегмент» на нём молчит. Отсюда отдельный род находки: координата
// недостижима модулем.
//
// ЗДЕСЬ СТОЯЛ ПРИМЕР `services/iam/go.mod` как живой. Он больше не живой и не
// станет: служба доступа вынесена в свой РЕПОЗИТОРИЙ (kacho#2616, исход C,
// 2026-09-13), и вложенного модуля в этом дереве не осталось ни одного —
// `git ls-files '*go.mod'` даёт одну запись, корневую. Род находки от этого не
// снят и снимать его нельзя: он держится инъекцией (подпроба «ОСЬ (модуль): пакет
// ВЛОЖЕННОГО модуля через путь родительского — находка» строит своё дерево с
// вложенным модулем), и заведётся вложенный модуль снова — предикат готов. Но
// утверждать, что предмет у него есть В ЭТОМ дереве, значило бы писать о
// несуществующем.
//
// # Почему якорь обязан быть не короче двух сегментов
//
// Имя продукта — ещё и обычное слово прозы, и проза перечисляет службы через
// косую черту: «единый пол безопасности, идентичный kaname/geo/nlb/registry».
// Такой токен путём не является, но односегментный якорь у него резолвится
// (каталоги с этим именем в дереве есть), и предикат с порогом в один сегмент
// объявил бы находкой шесть законных перечислений — измерено на дереве, а не
// предположено.
//
// Порог обрезает и полосу настоящих координат из одного сегмента; она названа
// числом (`shortAnchor`), а не умолчана. К путям модуля порог НЕ применяется:
// там якорь отсчитывается от корня модуля и односегментным двусмысленным не
// бывает.
//
// # Полосы названы числами, а не оговорками
//
//	moduleForeign    путь начинается с адреса, но ни с одного ОБЪЯВЛЕННОГО в
//	                 дереве модуля (голый `owner/repo`, чужая зависимость):
//	                 авторитет не наш
//	moduleOwnSegment имя продукта встречается ТОЛЬКО в самом пути модуля
//	                 (`github.com/PRO-Robotech/kaname[/…]`) и ни разу после него.
//	                 Якорь вместе с сегментом резолвится тогда ОБЪЯВЛЕНИЕМ, а не
//	                 деревом, и судить его нечем. Сюда же by construction уходит
//	                 веб-адрес GitHub (`…/kaname/issues/2224`) — и потому
//	                 отдельного перечня «глаголов GitHub» заводить не нужно:
//	                 перечень стареет, а объявление модуля — нет
//	shortAnchor      якорь короче двух сегментов — см. выше
//	anchorUnresolved якорь не резолвится вовсе: токен путём этого дерева не
//	                 является (адрес в контейнере, ресурс кластера, адрес URL,
//	                 синтетическая фикстура чужой пробы). Синтетика чужих
//	                 инъекций попадает СЮДА, а не в перечень исключений:
//	                 дерева под ней нет by construction
//	externalResolved координата резолвится не этим деревом, а деревом модуля,
//	                 публикующего внешний корень контрактов. Считается отдельным
//	                 числом, а не растворяется в `resolved`: пропади это дерево —
//	                 полоса обнулится, и обнуление обязано быть видно
//
// # Перепись
//
// Печатается: файлов прочитано, двоичных пропущено, объявленных модулей, деревьев
// внешних корней, токенов с сегментом имени продукта, каждая полоса своим числом,
// судимых (и сколько из них пришло полосой модуля), из них резолвится (и сколько
// резолвилось деревом внешнего модуля), находок.
//
// Ноль токенов — ОТКАЗ: гейт, чей предмет отсутствие, молчит одинаково и когда
// предмета нет, и когда сломан обход. Ноль объявленных модулей и ноль судимых
// полосой модуля — тоже ОТКАЗ: правило полосы не исполнялось НИ РАЗУ, и её
// молчание тогда означает «не искали», а не «не нашли». Условия самоистекающие.
//
// Ноль ДЕРЕВЬЕВ ВНЕШНИХ КОРНЕЙ отказом НЕ является, и это не послабление, а
// направление ошибки: потеря этого авторитета делает гейт ГРОМЧЕ — координаты,
// лежащие в чужом дереве, снова становятся находками, с именем и строкой. Молчать
// он от этого не начинает, поэтому число печатается, а вердиктом не становится;
// у синтетических деревьев инъекции внешних корней нет by construction.
package repohygiene

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"

	"github.com/PRO-Robotech/kacho/internal/contractsource"
)

// standaloneProductSegment — имя продукта, которым служба доступа назвала свои
// каталоги. Объявлено ОДИН раз: и обход, и перепись, и инъекция читают отсюда.
const standaloneProductSegment = "kaname"

// minAnchorSegments — нижняя граница длины якоря. Обоснование — в шапке; число
// стоит здесь, а не в регулярке, чтобы его изменение было решением.
const minAnchorSegments = 2

// coordinateTokenRe — путеподобный токен: два и более сегмента через косую
// черту. Знаки подстановки и фигурные скобки включены намеренно — координата в
// прозе пишется с ними (`pg/*.go`, `api/{create`), и токен обязан ловиться
// целиком, иначе якорь считался бы от обрубка.
var coordinateTokenRe = regexp.MustCompile(`[A-Za-z0-9_.*{}$@-]+(?:/[A-Za-z0-9_.*{}$@-]+)+`)

// goModDeclarationRe — объявление модуля в `go.mod`. Читается ПЕРВАЯ строка
// объявления, а не имя каталога: имя модуля и путь до него совпадать не
// обязаны, и здесь они как раз не совпадают.
var goModDeclarationRe = regexp.MustCompile(`(?m)^module[ \t]+(\S+)`)

// declaredModule — объявление модуля, найденное в дереве: путь импорта и
// каталог, от которого отсчитывается остаток.
type declaredModule struct {
	importPath string // github.com/PRO-Robotech/kaname
	root       string // services/iam ("." — корень дерева)
	segments   int    // сегментов в importPath
}

// declaredModules — авторитет для путей модуля, взятый ИЗ ТОГО ЖЕ дерева.
// Список отсортирован по убыванию длины пути: модуль токена — самый длинный
// объявленный префикс, иначе вложенный модуль резолвился бы родительским.
func declaredModules(tree *treecorpus.Tree) ([]declaredModule, error) {
	var mods []declaredModule
	for _, rel := range tree.SortedFiles() {
		slash := filepath.ToSlash(rel)
		if path.Base(slash) != "go.mod" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("объявление модуля %s не прочитано: %w", slash, err)
		}
		m := goModDeclarationRe.FindSubmatch(raw)
		if m == nil {
			continue
		}
		p := string(m[1])
		mods = append(mods, declaredModule{
			importPath: p,
			root:       path.Dir(slash),
			segments:   len(strings.Split(p, "/")),
		})
	}
	sort.Slice(mods, func(i, j int) bool {
		if len(mods[i].importPath) != len(mods[j].importPath) {
			return len(mods[i].importPath) > len(mods[j].importPath)
		}
		return mods[i].importPath < mods[j].importPath
	})
	return mods, nil
}

// moduleOf — самый длинный объявленный префикс токена, либо nil.
func moduleOf(mods []declaredModule, tok string) *declaredModule {
	for i := range mods {
		if tok == mods[i].importPath || strings.HasPrefix(tok, mods[i].importPath+"/") {
			return &mods[i]
		}
	}
	return nil
}

// rootContains — путь `child` лежит СТРОГО внутри каталога `parent`.
func rootContains(parent, child string) bool {
	if parent == "." || parent == "" {
		return child != "." && child != ""
	}
	return strings.HasPrefix(child, parent+"/")
}

// unreachableThroughNestedModule — целевой путь лежит внутри ДРУГОГО модуля,
// вложенного в модуль токена. Пакет вложенного модуля родительскому не
// принадлежит: Go по такому пути не найдёт его никогда, сколько бы каталог ни
// существовал в дереве. Возвращает путь того модуля, либо пустую строку.
func unreachableThroughNestedModule(mods []declaredModule, own declaredModule, target string) string {
	for i := range mods {
		m := mods[i]
		if m.root == own.root {
			continue
		}
		if !rootContains(own.root, m.root) {
			continue
		}
		if target == m.root || strings.HasPrefix(target, m.root+"/") {
			return m.importPath
		}
	}
	return ""
}

// joinUnderRoot — остаток пути модуля, отсчитанный от корня этого модуля.
func joinUnderRoot(root string, segs []string) string {
	tail := strings.Join(segs, "/")
	if root == "." || root == "" {
		return tail
	}
	if tail == "" {
		return root
	}
	return root + "/" + tail
}

type overAppliedFinding struct {
	file   string
	line   int
	token  string
	anchor string // якорь (координата дерева) либо целевой путь (координата модуля)
	nested string // непусто — путь вложенного модуля, из-за которого координата недостижима
	// external — непусто: якорь резолвился деревом ВНЕШНЕГО модуля, а не этим
	// деревом. Авторитет обязан быть назван в находке: без него читатель пошёл бы
	// искать якорь у себя и не нашёл бы, решив, что находка ложная.
	external string
	text     string
}

func (f overAppliedFinding) String() string {
	if f.nested != "" {
		return fmt.Sprintf("%s:%d: %s — ведёт в %s, а это пакет ВЛОЖЕННОГО модуля %s: "+
			"родительскому модулю он не принадлежит, и по этому пути его не найти никогда | %s",
			f.file, f.line, f.token, f.anchor, f.nested, strings.TrimSpace(f.text))
	}
	where := "в этом дереве"
	if f.external != "" {
		where = "в дереве модуля " + f.external
	}
	return fmt.Sprintf("%s:%d: %s — якорь %s резолвится %s, %s/%s нет | %s",
		f.file, f.line, f.token, f.anchor, where, f.anchor, standaloneProductSegment,
		strings.TrimSpace(f.text))
}

type overAppliedCensus struct {
	filesRead        int
	filesBinary      int
	modulesDeclared  int
	tokensWithName   int
	moduleForeign    int
	moduleOwnSegment int
	shortAnchor      int
	anchorUnresolved int
	judged           int
	moduleJudged     int
	resolved         int
	externalTrees    int
	externalResolved int
}

func (c overAppliedCensus) String() string {
	return fmt.Sprintf("файлов прочитано %d · двоичных пропущено %d · объявленных модулей %d · "+
		"деревьев внешних корней %d · "+
		"токенов с сегментом %q %d (авторитет чужой %d · сегмент — имя объявленного модуля %d · "+
		"якорь короче %d сегментов %d · якорь не резолвится %d) · "+
		"судимых %d (полосой модуля %d, резолвится %d, из них деревом внешнего модуля %d)",
		c.filesRead, c.filesBinary, c.modulesDeclared, c.externalTrees,
		standaloneProductSegment, c.tokensWithName,
		c.moduleForeign, c.moduleOwnSegment,
		minAnchorSegments, c.shortAnchor, c.anchorUnresolved,
		c.judged, c.moduleJudged, c.resolved, c.externalResolved)
}

// externalRootTree — дерево МОДУЛЯ, публикующего внешний корень дерева
// контрактов: путь модуля (для текста находки) и его каталог на диске.
type externalRootTree struct {
	modulePath string
	dir        string
}

// externalRootTrees — деревья модулей внешних корней, закреплённых go.mod
// судимого дерева, в устойчивом порядке.
//
// Перечень корней и их модулей спрашивается у contractsource: он объявлен ОДИН
// раз, и второе объявление разошлось бы с ним при появлении третьего корня.
// Резолв — общим предикатом `pinnedModuleRootDir`, тем же, которым второй дом
// заглушек резолвит анализатор монтирования: два разных резолва одной версии
// молча разошлись бы при бампе.
//
// ПУСТОЙ ПЕРЕЧЕНЬ — ЗАКОННЫЙ ИСХОД, и он не тише, а громче: у синтетических
// деревьев проб (t.TempDir() без своего go.mod) внешних корней нет by
// construction, а на настоящем дереве потеря этого авторитета добавляет находки,
// а не отнимает. Число печатается переписью, чтобы потеря была видна.
func externalRootTrees(treeRoot string) []externalRootTree {
	roots := make([]string, 0, len(contractsource.ExternalRootModules))
	for r := range contractsource.ExternalRootModules {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	seen := map[string]bool{}
	var out []externalRootTree
	for _, r := range roots {
		modulePath := contractsource.ExternalRootModules[r]
		if seen[modulePath] {
			continue
		}
		dir, err := pinnedModuleRootDir(treeRoot, modulePath)
		if err != nil {
			continue
		}
		if st, serr := os.Stat(dir); serr != nil || !st.IsDir() {
			continue
		}
		seen[modulePath] = true
		out = append(out, externalRootTree{modulePath: modulePath, dir: dir})
	}
	return out
}

// externalTreeResolves — координата существует в дереве какого-нибудь внешнего
// модуля, отсчитанная от ЕГО корня. Возвращает путь модуля — им и назван
// авторитет в переписи.
//
// Только точный путь, и только ВНУТРИ каталога модуля: `..` в середине токена
// вывел бы проверку за его пределы, а там она стала бы утверждением о случайном
// соседе по кэшу.
func externalTreeResolves(exts []externalRootTree, coord string) (string, bool) {
	coord = strings.Trim(coord, "./,;:")
	if coord == "" {
		return "", false
	}
	for _, e := range exts {
		p := filepath.Join(e.dir, filepath.FromSlash(coord))
		if p != e.dir && !strings.HasPrefix(p, e.dir+string(os.PathSeparator)) {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return e.modulePath, true
		}
	}
	return "", false
}

// externalModuleOf — токен начинается с пути ВНЕШНЕГО модуля, чьё дерево
// резолвится. Возвращает это дерево и число сегментов пути модуля — остаток
// отсчитывается от его корня.
//
// Спрашивается ПЕРЕД перечнем объявленных в дереве модулей: внешний модуль в
// `go.mod` объявлен зависимостью, а не своим `module`-заголовком, поэтому
// `declaredModules` его не знает и `moduleOf` вернул бы nil — токен уехал бы в
// полосу «авторитет не наш», при том что авторитет есть и лежит в кэше модулей.
func externalModuleOf(exts []externalRootTree, tok string) (*externalRootTree, int) {
	for i := range exts {
		m := exts[i].modulePath
		if tok == m || strings.HasPrefix(tok, m+"/") {
			return &exts[i], len(strings.Split(m, "/"))
		}
	}
	return nil, 0
}

// treeCoordinateResolves — путь есть в дереве сам по себе либо суффиксом
// какого-то отслеживаемого пути. Авторитет КООРДИНАТЫ ДЕРЕВА, и только её:
// путь модуля абсолютен, и суффиксу там взяться неоткуда.
func treeCoordinateResolves(tree *treecorpus.Tree, coord string) bool {
	coord = strings.Trim(coord, "./,;:")
	if coord == "" {
		return false
	}
	if tree.HasFile(coord) || tree.HasDir(coord) {
		return true
	}
	tail := "/" + coord
	for rel := range tree.Files() {
		if strings.HasSuffix(rel, tail) || strings.Contains(rel, tail+"/") {
			return true
		}
	}
	return false
}

// modulePathResolves — путь, отсчитанный от корня модуля, существует ТОЧНО.
func modulePathResolves(tree *treecorpus.Tree, p string) bool {
	if p == "" || p == "." {
		return true
	}
	return tree.HasDir(p) || tree.HasFile(p)
}

// scanOverAppliedRename разбирает ПРОИЗВОЛЬНОЕ дерево: настоящий репозиторий и
// синтетический корень инъекции проходят одну и ту же функцию, поэтому
// доказанное на втором верно для первого.
func scanOverAppliedRename(tree *treecorpus.Tree) (overAppliedCensus, []overAppliedFinding, error) {
	var census overAppliedCensus
	var findings []overAppliedFinding

	mods, err := declaredModules(tree)
	if err != nil {
		return census, nil, err
	}
	census.modulesDeclared = len(mods)

	root := tree.Root()
	exts := externalRootTrees(root)
	census.externalTrees = len(exts)
	for _, rel := range tree.SortedFiles() {
		slash := filepath.ToSlash(rel)
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			if os.IsNotExist(err) {
				// Индекс называет файл, которого нет на диске (частичная
				// выгрузка). Это не находка о прозе — молчим и не считаем.
				continue
			}
			return census, nil, fmt.Errorf("файл %s не прочитан: %w", slash, err)
		}
		head := raw
		if len(head) > 8192 {
			head = head[:8192]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			census.filesBinary++
			continue
		}
		census.filesRead++
		if !bytes.Contains(raw, []byte(standaloneProductSegment)) {
			continue
		}

		for i, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, standaloneProductSegment) {
				continue
			}
			for _, tok := range coordinateTokenRe.FindAllString(line, -1) {
				tok = strings.TrimRight(tok, "/.,;:)")
				segs := strings.Split(tok, "/")
				k := -1
				for j, s := range segs {
					if s == standaloneProductSegment {
						k = j
						break
					}
				}
				if k < 0 {
					continue
				}
				census.tokensWithName++

				if strings.HasPrefix(tok, "github.com/") || strings.HasPrefix(tok, "PRO-Robotech/") {
					// Путь ВНЕШНЕГО модуля судится тем же правилом, но своим
					// авторитетом: остаток отсчитывается от корня того модуля, а
					// не этого дерева. Иначе всякий импорт заглушек службы
					// доступа уезжал бы в полосу «авторитет не наш» — то есть из
					// наблюдения вышла бы ровно та поверхность, которую переезд и
					// тронул.
					if ext, extSegs := externalModuleOf(exts, tok); ext != nil {
						one := []externalRootTree{*ext}
						mk := -1
						for j := extSegs; j < len(segs); j++ {
							if segs[j] == standaloneProductSegment {
								mk = j
								break
							}
						}
						if mk < 0 {
							// Сегмент встречается только в САМОМ пути модуля
							// (`…/kaname`, `…/kaname/issues/2224`): резолвится
							// объявлением зависимости, а не деревом.
							census.moduleOwnSegment++
							continue
						}
						extAnchor := strings.Join(segs[extSegs:mk], "/")
						anchorOK := extAnchor == ""
						if !anchorOK {
							_, anchorOK = externalTreeResolves(one, extAnchor)
						}
						if !anchorOK {
							census.anchorUnresolved++
							continue
						}
						census.judged++
						census.moduleJudged++
						extTarget := strings.Join(segs[extSegs:mk+1], "/")
						if _, ok := externalTreeResolves(one, extTarget); ok {
							census.resolved++
							census.externalResolved++
							continue
						}
						findings = append(findings, overAppliedFinding{
							file: slash, line: i + 1, token: tok, anchor: extAnchor,
							external: ext.modulePath, text: line,
						})
						continue
					}
					mod := moduleOf(mods, tok)
					if mod == nil {
						census.moduleForeign++
						continue
					}
					// Сегмент имени продукта, попавший в САМ путь модуля,
					// резолвится ОБЪЯВЛЕНИЕМ, а не деревом, и судить его нечем.
					// Но он же — ПЕРВЫЙ в токене у всякого пути модуля службы
					// доступа, а вся её раскладка лежит ПОСЛЕ него. Взять первое
					// вхождение значило бы вывести из-под наблюдения её модуль
					// целиком, и это измерено, а не предположено: на голове линии
					// наивное правило оставляет «имя модуля» 3397 и доводит до
					// якоря 438, правило «первое ПОСЛЕ пути модуля» — 2078 и
					// 1757. Разница 1319 токенов, вчетверо больше судимого.
					mk := -1
					for j := mod.segments; j < len(segs); j++ {
						if segs[j] == standaloneProductSegment {
							mk = j
							break
						}
					}
					if mk < 0 {
						census.moduleOwnSegment++
						continue
					}
					anchor := joinUnderRoot(mod.root, segs[mod.segments:mk])
					if !modulePathResolves(tree, anchor) {
						census.anchorUnresolved++
						continue
					}
					census.judged++
					census.moduleJudged++
					target := joinUnderRoot(mod.root, segs[mod.segments:mk+1])
					if nested := unreachableThroughNestedModule(mods, *mod, target); nested != "" {
						findings = append(findings, overAppliedFinding{
							file: slash, line: i + 1, token: tok,
							anchor: target, nested: nested, text: line,
						})
						continue
					}
					if modulePathResolves(tree, target) {
						census.resolved++
						continue
					}
					findings = append(findings, overAppliedFinding{
						file: slash, line: i + 1, token: tok, anchor: anchor, text: line,
					})
					continue
				}

				if k < minAnchorSegments {
					census.shortAnchor++
					continue
				}
				anchor := strings.Join(segs[:k], "/")
				if !treeCoordinateResolves(tree, anchor) {
					census.anchorUnresolved++
					continue
				}
				census.judged++
				target := strings.Join(segs[:k+1], "/")
				if treeCoordinateResolves(tree, target) {
					census.resolved++
					continue
				}
				// Второй авторитет ПЕРВОГО рода: координата может лежать в дереве
				// модуля, публикующего внешний корень контрактов. Спрашивается
				// ПОСЛЕ своего дерева, а не вместо: своё дерево — источник истины,
				// чужое лишь добирает то, что из него уехало.
				if _, ok := externalTreeResolves(exts, target); ok {
					census.resolved++
					census.externalResolved++
					continue
				}
				findings = append(findings, overAppliedFinding{
					file: slash, line: i + 1, token: tok, anchor: anchor, text: line,
				})
			}
		}
	}

	if census.filesRead == 0 {
		return census, nil, fmt.Errorf("обход пуст — вердикт беспредметен (корень %q)", root)
	}
	return census, findings, nil
}

// TestOverAppliedRenameLeavesNoUnresolvableCoordinate — гейт класса.
func TestOverAppliedRenameLeavesNoUnresolvableCoordinate(t *testing.T) {
	t.Parallel()
	tree, err := treecorpus.NewTree(repoRoot(t))
	if err != nil {
		t.Fatalf("состав дерева не собран — вердикт беспредметен: %v", err)
	}

	census, findings, err := scanOverAppliedRename(tree)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	t.Logf("перепись: %s", census)

	if census.tokensWithName == 0 {
		t.Fatalf("предпосылка не выполняется: в %d прочитанных файлах не найдено ни одного "+
			"путеподобного токена с сегментом %q. Либо имя продукта ушло из координат вовсе "+
			"(тогда гейт снимается вместе с предметом), либо сломан обход — и тогда дерево "+
			"молча не читается, ровно тот дефект, ради которого гейт заведён.\nПерепись: %s",
			census.filesRead, standaloneProductSegment, census)
	}

	if census.modulesDeclared == 0 {
		t.Fatalf("предпосылка не выполняется: в дереве не найдено ни одного объявления "+
			"модуля, значит авторитет для полосы «путь модуля» отсутствует и вся она молчит "+
			"как чужая. Молчание тогда означает «не искали», а не «не нашли».\nПерепись: %s",
			census)
	}

	if census.moduleJudged == 0 {
		t.Fatalf("предпосылка не выполняется: полосой модуля не судим НИ ОДИН токен при %d "+
			"объявленных модулях. Правило полосы не исполнялось ни разу — либо пути модуля "+
			"ушли из координат, либо разбор перестал их узнавать, и тогда две трети предмета "+
			"снова невидимы.\nПерепись: %s", census.modulesDeclared, census)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("%d координат не резолвятся своим авторитетом:%s"+
			"\n\nПереименование каталогов принадлежит службе доступа и только ей: остальные "+
			"службы своего имени продукта не имеют, их каталоги зовутся именем платформы. "+
			"Такая координата не была верна НИ В ОДИН момент времени — правьте на ту, что "+
			"резолвится, а не удаляйте: числа рядом сняты на конкретном пакете и обязаны "+
			"остаться привязанными к нему. Координата, ведущая в пакет вложенного модуля "+
			"через путь родительского, правится на путь ЕГО СОБСТВЕННОГО модуля.\nПерепись: %s",
			len(findings), b.String(), census)
	}
}
