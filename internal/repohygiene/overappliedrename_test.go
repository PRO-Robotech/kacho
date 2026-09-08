// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// overappliedrename_test.go — переименование не применено там, где каталог
// так НЕ назван: координата, у которой резолвится якорь, а переименованный
// сегмент — нет.
//
// # Предмет
//
// Служба доступа получила собственное имя продукта и переименовала свои
// каталоги (`internal/apps/kacho` → `internal/apps/kaname` и далее). Массовая
// правка шире своего предмета задевает соседей, у которых своего имени продукта
// нет и чьи каталоги законно зовутся именем платформы. Получается координата,
// которая не была верна НИКОГДА: ни до правки, ни после.
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
// # Два АВТОРИТЕТА, а не два предиката
//
// Токен бывает двух родов, и резолвит их разное:
//
//	координата ДЕРЕВА   `services/iam/internal/repo/kaname` — резолвится
//	                    деревом: точным путём либо СУФФИКСОМ отслеживаемого
//	                    пути, потому что проза пишет координату от корня службы
//	                    (`nlb/internal/repo/...`), а не репозитория
//	путь МОДУЛЯ Go      `github.com/PRO-Robotech/kacho/services/iam/...` —
//	                    резолвится ОБЪЯВЛЕНИЯМИ модулей: путь абсолютен by
//	                    construction, суффиксу здесь взяться неоткуда
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
// Служба доступа вынесена в собственный модуль (`services/iam/go.mod`). Значит
// её пакет РОДИТЕЛЬСКОМУ модулю не принадлежит, и путь вида
// `github.com/PRO-Robotech/<родитель>/services/iam/internal/...` не резолвится
// НИКОГДА — при том что каталог в дереве существует и предикат «якорь+сегмент»
// на нём молчит. Отсюда отдельный род находки: координата недостижима модулем.
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
//
// # Перепись
//
// Печатается: файлов прочитано, двоичных пропущено, объявленных модулей,
// токенов с сегментом имени продукта, каждая полоса своим числом, судимых (и
// сколько из них пришло полосой модуля), из них резолвится, находок.
//
// Ноль токенов — ОТКАЗ: гейт, чей предмет отсутствие, молчит одинаково и когда
// предмета нет, и когда сломан обход. Ноль объявленных модулей и ноль судимых
// полосой модуля — тоже ОТКАЗ: правило полосы не исполнялось НИ РАЗУ, и её
// молчание тогда означает «не искали», а не «не нашли». Условия самоистекающие.
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

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
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
	text   string
}

func (f overAppliedFinding) String() string {
	if f.nested != "" {
		return fmt.Sprintf("%s:%d: %s — ведёт в %s, а это пакет ВЛОЖЕННОГО модуля %s: "+
			"родительскому модулю он не принадлежит, и по этому пути его не найти никогда | %s",
			f.file, f.line, f.token, f.anchor, f.nested, strings.TrimSpace(f.text))
	}
	return fmt.Sprintf("%s:%d: %s — якорь %s резолвится, %s/%s нет | %s",
		f.file, f.line, f.token, f.anchor, f.anchor, standaloneProductSegment,
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
}

func (c overAppliedCensus) String() string {
	return fmt.Sprintf("файлов прочитано %d · двоичных пропущено %d · объявленных модулей %d · "+
		"токенов с сегментом %q %d (авторитет чужой %d · сегмент — имя объявленного модуля %d · "+
		"якорь короче %d сегментов %d · якорь не резолвится %d) · "+
		"судимых %d (полосой модуля %d, резолвится %d)",
		c.filesRead, c.filesBinary, c.modulesDeclared,
		standaloneProductSegment, c.tokensWithName,
		c.moduleForeign, c.moduleOwnSegment,
		minAnchorSegments, c.shortAnchor, c.anchorUnresolved,
		c.judged, c.moduleJudged, c.resolved)
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
				if treeCoordinateResolves(tree, strings.Join(segs[:k+1], "/")) {
					census.resolved++
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
