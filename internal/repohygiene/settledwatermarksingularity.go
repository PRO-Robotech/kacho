// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// settledwatermarksingularity.go — анализатор «наблюдатель границы устоявшегося
// в дереве ОДИН, и он в фундаменте».
//
// # Предмет — не файл, а ПРИЁМ
//
// Номер строки журнала выдаётся счётчиком на вставке, а строка становится видимой
// на фиксации. Порядок номеров и порядок фиксаций поэтому НЕЗАВИСИМЫ: отдать
// подписчику номер, за которым ещё может появиться меньший, значит потерять
// меньший навсегда — перечитывание идёт строго «больше курсора», и возобновление
// с той же позиции воспроизводит ту же дыру.
//
// Написанный ответ на этот класс в дереве ОДИН: наблюдение границы по БЛОКИРОВКАМ
// таблицы журнала (см. `pkg/subscription/watermark.go`). Он не выводится заново
// при надобности — он был выведен один раз, разобран и покрыт пробой на трёх
// подслучаях; его повторное изобретение стоило бы того же разбора, а его
// молчаливая утрата не стоила бы ничего и потому случилась бы незаметно.
//
// # Зачем гейт, если файл на месте
//
// Затем, что предикат «файл на месте» выполняется УДАЛЕНИЕМ — достаточно снести
// того, кто его называет, и условие станет истинным. Предикат «приём живёт в
// фундаменте» удалением не выполняется: его выполняет только перенос.
//
// Гейт заведён при снятии мёртвого читателя nlb (kacho#1043). Тот читатель нёс
// эту технику ЕДИНСТВЕННЫМ экземпляром до фазы, перенёсшей её в `pkg/`
// (kacho#1018). Снятие мёртвого кода — работа правильная; снятие мёртвого кода,
// который один во всём дереве отвечает на живой класс, — утрата, неотличимая от
// уборки. Различает их этот гейт, а не память следующего.
//
// # Что он считает — ДВЕ половины, обе точны
//
//  1. НАБЛЮДАТЕЛЬ СУЩЕСТВУЕТ. Ноль — техника исчезла из дерева. Это находка
//     ГРОМЧЕ второй половины: форк чинится сведением, утрата — повторным выводом.
//  2. НАБЛЮДАТЕЛЬ ОДИН, И ОН В ФУНДАМЕНТЕ. Второй — находка, ГДЕ БЫ ОН НИ ЛЕЖАЛ,
//     включая сам `pkg/`: два наблюдателя в фундаменте — то же расхождение,
//     только ближе. Домен, заведший свой, получит свой курсор, свой размен и
//     свой разбор отката — то есть общий механизм останется общим по имени.
//
// # Чем опознаётся наблюдатель — и почему именно этим
//
// Тремя НЕСУЩИМИ признаками наблюдения, которые обязаны стоять В ОДНОМ
// СТРОКОВОМ ЛИТЕРАЛЕ — то есть в одном тексте запроса:
//
//   - `RowExclusiveLock` — режим блокировки, который писатель держит с момента
//     ПЛАНИРОВАНИЯ вставки, то есть ещё до того, как счётчик выдал ему номер;
//   - `virtualtransaction` — идентификатор ТРАНЗАКЦИИ, а не соединения: иначе
//     непрерывно пишущий фоновый работник удерживал бы горизонт вечно;
//   - `pg_backend_pid` — исключение СЕБЯ из множества писателей: без него
//     наблюдатель видит собственную блокировку и не двигает границу никогда.
//
// Ни один из трёх не украшение: сними любой — и наблюдение перестаёт быть
// верным, оставаясь похожим на верное. Поэтому предикат требует все три, а не
// любой из них: файл, где стоит один, техникой не является.
//
// # Почему разбор, а не поиск подстроки
//
// Признаки живут в ТЕКСТЕ ЗАПРОСА, то есть в строковом литерале, и те же слова
// стоят рядом в объяснениях — в этом файле их по два раза каждое. Поиск по
// подстроке краснел бы на собственном комментарии гейта.
//
// Поэтому: разбор, строковые литералы прод-кода. Пробы исключены by construction
// — и это не экономия, а необходимость: в дереве есть гейты, чьи фикстуры несут
// синтетический Go-код строковым литералом (`treewalkindex_test.go` держит в нём
// целое второе объявление функции, из-за чего поиск по тексту находит в пакете
// два определения одного имени там, где компилятор видит одно).
//
// # Ведомость послаблений и её самоистечение
//
// Наблюдатель, законно живущий вне фундамента, стоит в ведомости с причиной и
// предикатом истечения. Запись, которой в дереве больше нечего исключать, — САМА
// НАХОДКА: иначе она переживёт своё снятие и разрешит следующему завести свой
// наблюдатель под тем же оправданием.
//
// Падает анализатор на ПУСТОМ ОБХОДЕ: ноль прочитанных файлов прод-кода — тогда
// «ноль находок» неотличимо от «ноль прочитанного», а находка «техника исчезла»
// неотличима от «дерева не видели».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// SettledWatermarkHome — каталог, в котором обязан лежать единственный наблюдатель.
const SettledWatermarkHome = "pkg/"

// settledWatermarkMarkers — несущие признаки наблюдения. Обязаны стоять ВСЕ ТРИ
// в строковых литералах одного файла; разбор каждого — в шапке пакета.
var settledWatermarkMarkers = []string{
	"RowExclusiveLock",
	"virtualtransaction",
	"pg_backend_pid",
}

// SettledWatermarkAllowance — одно послабление: наблюдатель, законно живущий вне
// фундамента.
type SettledWatermarkAllowance struct {
	// File — файл наблюдателя относительно корня.
	File string
	// Because — причина и предикат истечения. Пустая причина объявлением не
	// является: закрыть глаз стало бы дешевле, чем перенести технику.
	Because string
}

// SettledWatermarkOptions — вход анализатора.
type SettledWatermarkOptions struct {
	Root string
	// GoRoots — каталоги прод-кода, в которых ищется наблюдатель.
	GoRoots []string
	Allow   []SettledWatermarkAllowance
}

// SettledWatermarkCensus — объём осмотренного. Печатается ВСЕГДА.
type SettledWatermarkCensus struct {
	GoFiles    int
	Literals   int
	Observers  int
	InHome     int
	Allowances int
}

// SettledWatermarkFinding — одна находка.
type SettledWatermarkFinding struct {
	Kind  string
	Where string
	What  string
}

func (f SettledWatermarkFinding) String() string {
	return fmt.Sprintf("[%s] %s — %s", f.Kind, f.Where, f.What)
}

// AuditSettledWatermarkSingularity судит дерево.
func AuditSettledWatermarkSingularity(
	o SettledWatermarkOptions, log io.Writer,
) ([]SettledWatermarkFinding, SettledWatermarkCensus, error) {
	var census SettledWatermarkCensus
	census.Allowances = len(o.Allow)

	var goFiles []string
	for _, root := range o.GoRoots {
		files, err := collectFiles(filepath.Join(o.Root, root), ".go")
		if err != nil {
			return nil, census, err
		}
		goFiles = append(goFiles, files...)
	}

	var observers []string
	for _, path := range goFiles {
		rel, _ := filepath.Rel(o.Root, path)
		rel = filepath.ToSlash(rel)
		// Пробы и сгенерённые стабы вне предмета: первые несут синтетические
		// фикстуры строковым литералом, вторые техники не содержат вовсе.
		if strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "pkg/api/") {
			continue
		}
		census.GoFiles++
		lits, err := goStringLiterals(path)
		if err != nil {
			return nil, census, err
		}
		census.Literals += len(lits)
		if allMarkersPresent(lits) {
			observers = append(observers, rel)
		}
	}
	sort.Strings(observers)
	census.Observers = len(observers)
	for _, rel := range observers {
		if strings.HasPrefix(rel, SettledWatermarkHome) {
			census.InHome++
		}
	}

	_, _ = fmt.Fprintf(log,
		"осмотрено: файлов прод-кода Go %d · строковых литералов %d · наблюдателей границы %d (из них в %s — %d) · послаблений %d\n",
		census.GoFiles, census.Literals, census.Observers,
		SettledWatermarkHome, census.InHome, census.Allowances)

	if census.GoFiles == 0 {
		return nil, census, fmt.Errorf(
			"обход пуст: файлов прод-кода Go %d — «ноль находок» неотличимо от «ноль прочитанного»",
			census.GoFiles)
	}

	allowed := make(map[string]SettledWatermarkAllowance, len(o.Allow))
	var findings []SettledWatermarkFinding
	for _, a := range o.Allow {
		if a.Because == "" {
			findings = append(findings, SettledWatermarkFinding{
				Kind:  "ПОСЛАБЛЕНИЕ-БЕЗ-ПРИЧИНЫ",
				Where: a.File,
				What:  "послабление без причины и предиката истечения: закрыть глаз стало бы дешевле, чем перенести технику",
			})
			continue
		}
		allowed[filepath.ToSlash(a.File)] = a
	}

	// Половина 1: наблюдатель существует. Громче второй половины — форк чинится
	// сведением, утрата требует повторного вывода.
	if census.Observers == 0 {
		findings = append(findings, SettledWatermarkFinding{
			Kind:  "ТЕХНИКА-ИСЧЕЗЛА",
			Where: SettledWatermarkHome,
			What: fmt.Sprintf(
				"ни один файл прод-кода не несёт всех трёх признаков наблюдения (%s): единственный написанный ответ на потерю строк при чтении по номеру утрачен — восстанавливать придётся выводом, а не правкой",
				strings.Join(settledWatermarkMarkers, ", ")),
		})
	}

	// Половина 2: наблюдатель один, и он в фундаменте.
	used := map[string]bool{}
	for _, rel := range observers {
		if a, ok := allowed[rel]; ok {
			used[rel] = true
			_ = a
			continue
		}
		if !strings.HasPrefix(rel, SettledWatermarkHome) {
			findings = append(findings, SettledWatermarkFinding{
				Kind:  "НАБЛЮДАТЕЛЬ-ВНЕ-ФУНДАМЕНТА",
				Where: rel,
				What: fmt.Sprintf(
					"наблюдатель границы устоявшегося вне %s: у домена заводится свой курсор, свой размен и свой разбор отката, и общий механизм остаётся общим по имени",
					SettledWatermarkHome),
			})
		}
	}
	if census.Observers > 1 {
		findings = append(findings, SettledWatermarkFinding{
			Kind:  "ВТОРОЙ-НАБЛЮДАТЕЛЬ",
			Where: strings.Join(observers, ", "),
			What: fmt.Sprintf(
				"наблюдателей границы %d, ожидался 1: два наблюдения одного класса расходятся молча — каждое защитимо по отдельности, а сходство видно только сплошной переписью",
				census.Observers),
		})
	}

	// Самоистечение ведомости.
	for rel, a := range allowed {
		if !used[rel] {
			findings = append(findings, SettledWatermarkFinding{
				Kind:  "ПОСЛАБЛЕНИЕ-БЕЗ-ПРЕДМЕТА",
				Where: rel,
				What: fmt.Sprintf(
					"наблюдателя по этому пути в дереве нет, а послабление осталось (%s): запись переживёт своё снятие и разрешит следующему завести свой наблюдатель под тем же оправданием",
					a.Because),
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Where < findings[j].Where
	})
	return findings, census, nil
}

// allMarkersPresent — несёт ли файл ТЕКСТ НАБЛЮДЕНИЯ: литерал, в котором стоят
// ВСЕ три несущих признака СРАЗУ.
//
// Признаки требуются в ОДНОМ литерале, а не врозь по файлу, и это не строгость
// ради строгости — это и есть предмет. Наблюдение суть один запрос: режим
// блокировки, идентификатор транзакции и исключение себя обязаны стоять в одном
// его тексте, иначе они не про один вопрос. Файл, где те же три слова лежат
// тремя отдельными литералами, запроса не содержит — он о признаках ГОВОРИТ.
//
// Различие проверяется на этом самом анализаторе: он объявляет все три признака
// данными, по одному литералу на признак, и наблюдателем НЕ является. Пока
// предикат считал признаки по файлу, гейт находил сам себя — тот же класс, что
// поиск по подстроке, находящий слово в собственном объяснении, только в форме
// объявления, а не комментария. Ведомостью это не чинится: послабление на самого
// себя скрыло бы и настоящего наблюдателя, поставленного рядом.
func allMarkersPresent(lits []string) bool {
	for _, l := range lits {
		hasAll := true
		for _, m := range settledWatermarkMarkers {
			if !strings.Contains(l, m) {
				hasAll = false
				break
			}
		}
		if hasAll {
			return true
		}
	}
	return false
}

// goStringLiterals — значения строковых литералов файла, взятые РАЗБОРОМ.
// Комментарии сюда не попадают by construction: узел литерала не есть узел
// комментария, и подгонять это отдельным вычищением не требуется.
func goStringLiterals(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			return true
		}
		out = append(out, v)
		return true
	})
	return out, nil
}
