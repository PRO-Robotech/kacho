// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// manifestnamedpredicate.go — ПРЕДИКАТ, НАЗВАННЫЙ МАНИФЕСТОМ ДОМЕНА, ОБЯЗАН
// БЫТЬ ИСПОЛНИМ В ЭТОМ ДЕРЕВЕ (задача #1110, действующий предикат п. 4:
// «у внешних читателей модели прав есть предмет после разъезда»).
//
// Живёт в НЕ-тестовом файле намеренно: инъекция обязана звать ТЕ ЖЕ функции,
// что и держатель, иначе она доказывает свойство своей копии.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Манифест домена — документ ПЛАТФОРМЫ и вторая сторона сверки с каноном модели
// прав (`proto/kaname/cloud/iam/v1/fga_model.fga`). В своей шапке он называет
// КОМАНДУ, которой судится каждый его раздел: это единственная запись о том, кто
// его держит. Читатель манифеста идёт за этой командой — и другого способа
// узнать держателя у него нет.
//
// Команда, называющая каталог, которого в дереве нет, даёт не красное и не
// зелёное, а ТРЕТИЙ исход:
//
//	$ make -C services/iam model-canon-check
//	make: *** services/iam: No such file or directory.  Stop.
//
// Прочитавший это как «проверка прошла» ошибётся молча, а прочитавший как
// «проверка упала» ошибётся тоже: вердикта о манифесте не произведено вовсе.
// Манифест при этом продолжает ОБЕЩАТЬ вердикт — то есть документ утверждает о
// себе то, чего никто не проверяет.
//
// ЦЕНА ИЗМЕРЕНА, А НЕ ПРЕДПОЛОЖЕНА. Разрез службы доступа (#2598) унёс её
// каталог целиком: `git ls-files services/iam | wc -l` → 0. Пять манифестов
// платформы продолжали называть 23 команды `make -C services/iam …` и
// `go test -C services/iam …` и две координаты `services/iam/internal/manifest/
// roles.go`. Двадцать пять названных держателей, из которых исполним НИ ОДИН, и
// ни одна проверка дерева этого не заметила: обход, ищущий отсутствующий
// каталог, не краснеет — он просто ничего не находит.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ — КОМАНДНАЯ СТРОКА, РАЗОБРАННАЯ ПО ПОЗИЦИЯМ АРГУМЕНТОВ
//
// Проверка подстрокой здесь зеленела бы на собственном объяснении: имена целей и
// путь `services/iam` встречаются и в прозе манифеста, и в этой шапке. Поэтому
// разбор идёт по ПОЗИЦИЯМ:
//
//	комментарий → тело после `#` → первый токен есть ГЛАГОЛ (`make` | `go test`)
//	            → ключ `-C` даёт каталог → следующий не-флаг даёт цель/пакет
//	            → ключ `-run` даёт имя пробы
//
// Строка прозы, лишь УПОМИНАЮЩАЯ команду («форму судит `make -C … цель`»), под
// разбор не подпадает: её первый токен — не глагол. Это решение, а не пробел, и
// оно названо в §«Чего гейт НЕ покрывает».
//
// ─────────────────────────────────────────────────────────────────────────────
// ПЯТЬ ОСЕЙ, И У КАЖДОЙ СВОЙ ВОПРОС
//
//	N1  каталог, названный ключом `-C`, существует в индексе git;
//	N2  `make -C <кат> <цель>` — у каталога есть `Makefile`, и тот ОБЪЯВЛЯЕТ
//	    цель. Объявление берётся строкой-целью, не вхождением имени: цель
//	    объясняют комментарием рядом с собой, и поиск по имени зеленел бы на
//	    объяснении;
//	N3  `go test [-C <кат>] <пакет>` — каталог пакета существует и несёт хотя бы
//	    один файл `.go`. Пакет без исходников не исполним, а `go test` на нём
//	    даёт `no Go files` — снова третий исход;
//	N4  `-run <Имя>` — названная проба ОБЪЯВЛЕНА в этом пакете. Без этой оси
//	    манифест мог бы назвать живой пакет и мёртвое имя: `go test -run` на
//	    несуществующем имени печатает `no tests to run` и выходит кодом 0, то
//	    есть отсутствие держателя читается как зелёное;
//	N5  путь дерева, названный в обратных кавычках, существует в индексе.
//	    Координата в прозе — не команда, но читатель идёт и по ней;
//	N6  командная строка ИНОГО глагола из закрытого набора (`grep`, `sed`, `git`,
//	    …) — каждый её аргумент-путь резолвится, а образец с `*` совпадает хотя
//	    бы с одним путём индекса. Без этой оси класс возвращался бы через другой
//	    глагол: `grep -l … services/iam/internal/migrations/*.sql` даёт пустой
//	    вывод и код 1 на снятом каталоге, то есть «ничего не нашлось» вместо
//	    «искать было негде». Набор глаголов ЗАКРЫТ намеренно: открытый признал бы
//	    командой всякую строку прозы, начатую латиницей.
//
// ИСКЛЮЧЕНИЕ У N5 РОВНО ОДНО, И ОНО ВИДИМОЕ. Строка, назвавшая ЧУЖОЙ
// репозиторий (`PRO-Robotech/<не kacho>`), объявляет свои координаты его
// координатами — требовать их от этого дерева значило бы завести находку на
// правдивой записи. Такие пути не проверяются, но и не умалчиваются: перепись
// печатает их ЧИСЛО отдельной величиной, иначе исключение стало бы слепой зоной
// ровно того размера, которого никто не видит.
//
// Исключение НЕ распространяется на `make` и `go test`: эти две формы называют
// ДЕРЖАТЕЛЯ документа, и строка обещает исполнимость там, где документ лежит;
// упоминание чужого репозитория рядом с ней обещания не снимает — оно делает его
// противоречивым. Держатель из чужого дерева называется прозой.
//
// Иные глаголы (ось N6) под исключение подпадают: они не называют держателя, они
// ПОКАЗЫВАЮТ ЗАМЕР, и замер над чужим деревом законен, если это дерево названо.
//
// ─────────────────────────────────────────────────────────────────────────────
// КОМАНДНАЯ СТРОКА ОЗНАЧАЕТ «ЗАПУСКАЕТСЯ ЗДЕСЬ» — И ЭТО ПРАВИЛО, А НЕ ПРИДИРКА
//
// Часть держателей манифеста уехала в репозиторий службы и живёт там
// (`internal/moduleroleparity`, `internal/moduleseedparity`, цели
// `model-canon-check` и `module-manifest-check` в его `Makefile`). Назвать их
// КОМАНДНОЙ строкой в манифесте платформы нельзя: строка обещает исполнимость
// там, где документ лежит. Чужой держатель называется ПРОЗОЙ, вместе с
// репозиторием, — и тогда читатель знает, что идти ему в другое дерево.
//
// Различие не косметическое. Команда, исполнимая «где-то», неотличима от
// команды, исполнимой нигде, ровно тем способом, который этот гейт и закрывает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ГЕЙТ НЕ ПОКРЫВАЕТ, И ЭТО СКАЗАНО ПРЯМО
//
//   - команду, упомянутую внутри фразы, а не отдельной строкой: её первый токен
//     не глагол. Ловить её значило бы краснеть на этой шапке;
//   - ИСТИННОСТЬ названного держателя: что цель `Makefile` судит именно то, о
//     чём манифест говорит, машинного предиката не имеет. Судится существование
//     и объявленность, не смысл;
//   - разделы манифеста, не назвавшие держателя ВОВСЕ. Это соседний класс
//     («утверждение без держателя»), и у него другой предмет: здесь судится
//     названное, там — неназванное.

// manifestPredicateKindMake — команда `make`, названная манифестом.
const manifestPredicateKindMake = "make"

// manifestPredicateKindGoTest — команда `go test`, названная манифестом.
const manifestPredicateKindGoTest = "go test"

// manifestPredicateKindPath — координата дерева в обратных кавычках.
const manifestPredicateKindPath = "путь"

// manifestPredicateKindShell — командная строка иного глагола (ось N6).
const manifestPredicateKindShell = "команда"

// manifestShellVerbs — ЗАКРЫТЫЙ набор глаголов, чьи аргументы-пути судятся.
//
// Закрыт потому, что открытый признал бы командой всякую строку прозы, начатую
// латиницей, и гейт ловил бы форму вместо существа.
var manifestShellVerbs = map[string]bool{
	"grep": true, "sed": true, "awk": true, "git": true, "cat": true,
	"wc": true, "ls": true, "find": true, "python3": true,
	"helm": true, "kubectl": true, "buf": true,
}

// manifestNamedPredicate — один держатель, названный манифестом.
type manifestNamedPredicate struct {
	// Rel — манифест, который назвал.
	Rel string
	// Line — строка манифеста, чтобы находка называла координату.
	Line int
	// Raw — тело комментария как есть, для текста находки.
	Raw string
	// Kind — одна из трёх констант выше.
	Kind string
	// Dir — каталог, названный ключом `-C`. Пусто = корень дерева.
	Dir string
	// Target — цель `make` либо пакет `go test`. Для координаты — сам путь.
	Target string
	// RunName — имя пробы из `-run`. Пусто, если ключа не было.
	RunName string
	// Foreign — строка объявила свои координаты координатами ЧУЖОГО
	// репозитория. Проверке существования в этом дереве такая координата не
	// подлежит, но из переписи не исчезает.
	Foreign bool
}

// manifestTreeView — то, что гейт спрашивает у дерева.
//
// Отдельным типом, а не обходом внутри предиката: инъекция подаёт синтетику, и
// её вердикт обязан выноситься ТЕМ ЖЕ кодом. Обход, спрятанный в предикате,
// доказывался бы правкой живого дерева — то есть не доказывался бы.
type manifestTreeView struct {
	// Files — пути индекса git.
	Files map[string]bool
	// Dirs — их каталоги вместе со всеми предками.
	Dirs map[string]bool
	// MakeTargets — цели, ОБЪЯВЛЕННЫЕ каждым прочитанным `Makefile`.
	MakeTargets map[string]map[string]bool
	// GoDirs — каталоги, несущие хотя бы один файл `.go`.
	GoDirs map[string]bool
	// GoFuncs — имена функций, объявленных в каталоге.
	GoFuncs map[string]map[string]bool
}

// manifestCommentBody — тело строки-комментария YAML. Группа 1 — текст после `#`.
var manifestCommentBody = regexp.MustCompile(`^\s*#+\s?(.*)$`)

// manifestBacktickToken — токен в обратных кавычках.
var manifestBacktickToken = regexp.MustCompile("`([^`]+)`")

// manifestOwnRepo — репозиторий, которому принадлежит это дерево.
const manifestOwnRepo = "PRO-Robotech/kacho"

// manifestForeignRepo — упоминание репозитория организации в строке.
var manifestForeignRepo = regexp.MustCompile(`PRO-Robotech/([A-Za-z0-9_.-]+)`)

// manifestLineNamesForeignRepo — строка объявила свои координаты координатами
// ДРУГОГО репозитория.
func manifestLineNamesForeignRepo(body string) bool {
	for _, m := range manifestForeignRepo.FindAllString(body, -1) {
		if m != manifestOwnRepo {
			return true
		}
	}
	return false
}

// manifestMakeTargetLine — строка-ОБЪЯВЛЕНИЕ цели `Makefile`.
//
// Присваивание (`VAR := …`) целью не является, поэтому двоеточие не должно
// сопровождаться знаком равенства.
var manifestMakeTargetLine = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.\-/]*)\s*::?([^=]|$)`)

// manifestPathExtensions — расширения, по которым токен в кавычках признаётся
// координатой дерева.
//
// Перечень закрыт намеренно: токен `vpc/v1` или `<domain>.<resource>.<verb>`
// путём не является, и требовать его существования значило бы завести находку на
// каждом упоминании формы имени.
var manifestPathExtensions = []string{
	".go", ".yaml", ".yml", ".fga", ".sql", ".md", ".mdx",
	".json", ".proto", ".py", ".sh", ".ts", ".tsx",
}

// extractManifestNamedPredicates — разбор шапки одного манифеста.
//
// Возвращает названных держателей и ЧИСЛО прочитанных строк-комментариев: без
// второй величины «ноль держателей» было бы неотличимо от «ноль прочитанного».
func extractManifestNamedPredicates(rel, content string) (preds []manifestNamedPredicate, commentLines int) {
	for i, line := range strings.Split(content, "\n") {
		m := manifestCommentBody.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		commentLines++
		body := strings.TrimSpace(m[1])
		if body == "" {
			continue
		}
		lineNo := i + 1

		if cmd, ok := parseManifestCommandLine(body); ok {
			cmd.Rel = rel
			cmd.Line = lineNo
			cmd.Raw = body
			cmd.Foreign = manifestLineNamesForeignRepo(body)
			preds = append(preds, cmd)
		}

		for _, tok := range manifestBacktickToken.FindAllStringSubmatch(body, -1) {
			p := strings.TrimSpace(tok[1])
			if !manifestLooksLikeTreePath(p) {
				continue
			}
			preds = append(preds, manifestNamedPredicate{
				Rel:     rel,
				Line:    lineNo,
				Raw:     body,
				Kind:    manifestPredicateKindPath,
				Target:  p,
				Foreign: manifestLineNamesForeignRepo(body),
			})
		}
	}
	return preds, commentLines
}

// manifestLooksLikeTreePath — токен есть координата дерева, а не форма имени.
func manifestLooksLikeTreePath(tok string) bool {
	if !strings.Contains(tok, "/") || strings.ContainsAny(tok, " \t`") {
		return false
	}
	if strings.HasPrefix(tok, "/") || strings.Contains(tok, "://") {
		return false
	}
	for _, ext := range manifestPathExtensions {
		if strings.HasSuffix(tok, ext) {
			return true
		}
	}
	return false
}

// parseManifestCommandLine — командная строка, разобранная по позициям.
//
// Глагол обязан быть ПЕРВЫМ токеном: строка прозы, упоминающая команду внутри
// фразы, командой не объявлена и разбору не подлежит.
func parseManifestCommandLine(body string) (manifestNamedPredicate, bool) {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return manifestNamedPredicate{}, false
	}

	var (
		out  manifestNamedPredicate
		args []string
	)
	switch {
	case fields[0] == "make":
		out.Kind = manifestPredicateKindMake
		args = fields[1:]
	case fields[0] == "go" && len(fields) > 1 && fields[1] == "test":
		out.Kind = manifestPredicateKindGoTest
		args = fields[2:]
	case manifestShellVerbs[fields[0]]:
		out.Kind = manifestPredicateKindShell
		out.Target = strings.Join(fields, " ")
		return out, true
	default:
		return manifestNamedPredicate{}, false
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C" && i+1 < len(args):
			out.Dir = strings.Trim(args[i+1], "`\"'")
			i++
		case a == "-run" && i+1 < len(args):
			out.RunName = strings.Trim(args[i+1], "`\"'")
			i++
		case strings.HasPrefix(a, "-"):
			// Прочие ключи (`-count=1`, `-v`) координаты не несут.
		case out.Target == "":
			out.Target = strings.Trim(a, "`\"'")
		}
	}
	if out.Target == "" {
		return manifestNamedPredicate{}, false
	}
	return out, true
}

// manifestPredicatePackageDir — каталог пакета, названного `go test`.
func manifestPredicatePackageDir(p manifestNamedPredicate) string {
	// Порядок снятия важен: `./...` после снятия приставки даёт `...`, и суффикс
	// `/...` в нём уже не находится. Ошибку нашла инъекция, не чтение.
	pkg := strings.TrimPrefix(p.Target, "./")
	pkg = strings.TrimSuffix(pkg, "...")
	pkg = strings.TrimSuffix(pkg, "/")
	if p.Dir == "" || p.Dir == "." {
		if pkg == "" {
			return "."
		}
		return pkg
	}
	if pkg == "" {
		return p.Dir
	}
	return p.Dir + "/" + pkg
}

// auditManifestNamedPredicates — предикат пяти осей. Пусто = норма.
//
// Чистая функция от разобранных держателей и вида дерева: её же зовёт инъекция.
func auditManifestNamedPredicates(preds []manifestNamedPredicate, tree manifestTreeView) []string {
	var found []string
	at := func(p manifestNamedPredicate) string {
		return fmt.Sprintf("%s:%d", p.Rel, p.Line)
	}

	for _, p := range preds {
		// N1 — каталог ключа `-C`.
		if p.Dir != "" && p.Dir != "." && !tree.Dirs[p.Dir] {
			found = append(found, fmt.Sprintf(
				"%s: названо `%s`, а каталога %q в дереве нет — команда даёт ТРЕТИЙ исход "+
					"(«не выполнилось»), а манифест продолжает обещать вердикт. Держателя из "+
					"другого репозитория называют ПРОЗОЙ вместе с его репозиторием, не командной строкой",
				at(p), p.Raw, p.Dir))
			continue
		}

		switch p.Kind {
		case manifestPredicateKindMake:
			mf := "Makefile"
			if p.Dir != "" && p.Dir != "." {
				mf = p.Dir + "/Makefile"
			}
			if !tree.Files[mf] {
				found = append(found, fmt.Sprintf(
					"%s: названо `%s`, а файла %s в дереве нет — цель объявить некому",
					at(p), p.Raw, mf))
				continue
			}
			if !tree.MakeTargets[mf][p.Target] {
				found = append(found, fmt.Sprintf(
					"%s: названо `%s`, а %s цели %q НЕ ОБЪЯВЛЯЕТ — прочитано объявлений %d",
					at(p), p.Raw, mf, p.Target, len(tree.MakeTargets[mf])))
			}
		case manifestPredicateKindGoTest:
			dir := manifestPredicatePackageDir(p)
			if !tree.Dirs[dir] {
				found = append(found, fmt.Sprintf(
					"%s: названо `%s`, а каталога пакета %q в дереве нет",
					at(p), p.Raw, dir))
				continue
			}
			if !tree.GoDirs[dir] {
				found = append(found, fmt.Sprintf(
					"%s: названо `%s`, а в %q нет ни одного файла .go — `go test` на таком "+
						"пакете даёт `no Go files`, то есть снова третий исход",
					at(p), p.Raw, dir))
				continue
			}
			if p.RunName != "" && !tree.GoFuncs[dir][p.RunName] {
				found = append(found, fmt.Sprintf(
					"%s: названо `%s`, а функции %q в пакете %q НЕТ — `go test -run` на "+
						"несуществующем имени печатает «no tests to run» и выходит кодом 0, "+
						"то есть отсутствие держателя читалось бы как зелёное",
					at(p), p.Raw, p.RunName, dir))
			}
		case manifestPredicateKindShell:
			if p.Foreign {
				// Команда объявлена принадлежащей чужому дереву. Число таких
				// печатает перепись.
				continue
			}
			for _, arg := range manifestShellPathArgs(p.Target) {
				if manifestTreeHasPath(tree, arg) {
					continue
				}
				found = append(found, fmt.Sprintf(
					"%s: названо `%s`, а координаты %q в дереве нет — команда даёт пустой "+
						"вывод, то есть «ничего не нашлось» вместо «искать было негде»",
					at(p), p.Raw, arg))
			}
		case manifestPredicateKindPath:
			if p.Foreign {
				// Координата чужого репозитория. Число таких печатает перепись.
				continue
			}
			if !tree.Files[p.Target] && !tree.Dirs[p.Target] {
				found = append(found, fmt.Sprintf(
					"%s: названа координата %q, которой в дереве нет — читатель пойдёт за ней "+
						"и не найдёт", at(p), p.Target))
			}
		}
	}

	sort.Strings(found)
	return found
}

// manifestNamedPredicateCensus — перепись объёма осмотренного.
//
// Величины печатаются ПОРОЗНЬ: одно число скрыло бы ровно тот случай, ради
// которого гейт заведён, — манифест, чья шапка не прочитана.
func manifestNamedPredicateCensus(manifests, commentLines int, preds []manifestNamedPredicate, resolved int) string {
	byKind := map[string]int{}
	foreign := 0
	for _, p := range preds {
		byKind[p.Kind]++
		if p.Foreign {
			foreign++
		}
	}
	return fmt.Sprintf(
		"перепись: манифестов %d, строк-комментариев прочитано %d, названных держателей %d "+
			"(make %d · go test %d · команда %d · путь %d), координат чужого репозитория %d "+
			"(не проверяются), резолвится %d",
		manifests, commentLines, len(preds),
		byKind[manifestPredicateKindMake],
		byKind[manifestPredicateKindGoTest],
		byKind[manifestPredicateKindShell],
		byKind[manifestPredicateKindPath],
		foreign, resolved)
}

// manifestShellPathArgs — аргументы командной строки, похожие на путь дерева.
//
// Ключи и их значения отброшены: `-l`, `'образец'`, `--jq` путей не несут.
func manifestShellPathArgs(cmd string) []string {
	var out []string
	for _, a := range strings.Fields(cmd) {
		a = strings.Trim(a, "`\"'")
		if a == "" || strings.HasPrefix(a, "-") {
			continue
		}
		if !strings.Contains(a, "/") || strings.Contains(a, "://") {
			continue
		}
		if strings.HasPrefix(a, "/") || strings.HasPrefix(a, "./") || strings.HasPrefix(a, "$") {
			continue
		}
		// Первый сегмент обязан быть каталогом ЭТОГО дерева — иначе токен есть
		// имя пакета Go, форма имени права или адрес репозитория, а не путь.
		if strings.Contains(strings.SplitN(a, "/", 2)[0], ".") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// manifestTreeHasPath — путь либо образец резолвится в индексе.
func manifestTreeHasPath(tree manifestTreeView, p string) bool {
	if tree.Files[p] || tree.Dirs[p] {
		return true
	}
	if !strings.ContainsAny(p, "*?[") {
		return false
	}
	for rel := range tree.Files {
		if ok, err := path.Match(p, rel); err == nil && ok {
			return true
		}
	}
	return false
}

// countManifestForeignPredicates — сколько координат отнесено к чужому
// репозиторию и потому НЕ проверялось.
//
// Отдельной функцией, а не подсчётом на месте: величину зовут и держатель (чтобы
// «резолвится» считалось от ПРОВЕРЕННЫХ, а не от названных), и перепись.
func countManifestForeignPredicates(preds []manifestNamedPredicate) int {
	n := 0
	for _, p := range preds {
		if p.Foreign {
			n++
		}
	}
	return n
}

// parseManifestMakeTargets — цели, ОБЪЯВЛЕННЫЕ текстом `Makefile`.
func parseManifestMakeTargets(content string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "\t") {
			continue
		}
		m := manifestMakeTargetLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		if name == ".PHONY" || strings.HasPrefix(name, ".") {
			continue
		}
		out[name] = true
	}
	return out
}
