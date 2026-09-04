// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Разбор дерева для гейта «ограничение формы имени, поставленное миграцией
// сервиса, обязано быть ДОКАЗАНО вставкой».
//
// Вынесено в не-тестовый файл пакета, чтобы инъекционная проба звала тот же
// разбор, а не свою копию: копия разошлась бы с оригиналом молча и доказывала бы
// способность упасть у кода, который не исполняется.
//
// # Почему разбор синтаксиса, а не поиск подстроки (перемерено 2026-08-19)
//
// Прежняя редакция искала подстроку `nameformdb.Probe` в сыром тексте файла и
// потому не отличала ВЫЗОВ от УПОМИНАНИЯ. Рецензент выпотрошил пробу compute до
// одной строки `// Здесь когда-то звался nameformdb.Probe` — гейт остался
// ЗЕЛЁНЫМ и продолжал числить compute доказанным. Это ровно `testing.md`
// §«Гейт на класс» п.4: проверка обязана читать исполняемую часть, а не текст.
//
// Теперь доказательством считается только то, что прогон действительно
// исполняет: вызов входного метода двигателя на значении типа `Probe`, стоящий
// в функции, ДОСТИЖИМОЙ от точки входа проб того же пакета. Комментарий и
// строковый литерал в синтаксическое дерево не попадают by construction —
// разбор идёт без `parser.ParseComments`, а строка остаётся `*ast.BasicLit` и
// вызовом не становится.

// nameFormEnginePkgPath — путь пакета-двигателя. Служит ДВУМ разным целям, и обе
// названы, чтобы вторую не приняли за возврат к поиску подстрокой:
//
//   - отбор каталогов-кандидатов: файл, зовущий двигатель, обязан его
//     импортировать, а строка импорта содержит этот путь. Каталог, где путь не
//     встречается ни в одном файле, вызова не содержит by construction, и его
//     не нужно разбирать (иначе разбор шёл бы по всем ~1700 файлам проб дерева);
//   - опознание локального имени пакета в разобранном файле — с учётом
//     псевдонима импорта.
//
// Отбор кандидатов доказательством НЕ является: попавший в кандидаты файл всё
// равно проходит разбор, и упоминание пути в комментарии даст ноль вызовов.
const nameFormEnginePkgPath = "pkg/nameformdb"

// nameFormEngineType — тип двигателя, вызов метода которого и есть
// доказательство.
const nameFormEngineType = "Probe"

// nameFormProbeMention — прежний, ТЕКСТОВЫЙ признак. Оставлен ровно для
// диагностики и никогда — для вердикта: он позволяет находке сказать «имя
// двигателя в файле есть, исполняемого вызова нет», то есть отличить
// выпотрошенную пробу от пробы, которой не было никогда. Без этого отличия
// сообщение гейта посылало бы читателя заводить пробу заново там, где её надо
// восстановить.
const nameFormProbeMention = "nameformdb." + nameFormEngineType

// nameFormDBCoverage — исход обхода: кто ставит форму имени в базе и кто
// доказывает её действие.
//
// Объём осмотренного — часть исхода, а не украшение лога: без него «покрыто
// всё» неотличимо от «прочитано ноль».
type nameFormDBCoverage struct {
	MigrationsRead int
	TestsRead      int
	// TestsParsed — сколько файлов проб дошло до разбора синтаксиса. Всегда
	// меньше TestsRead: разбираются только каталоги, где двигатель вообще
	// импортируется. Число печатается, чтобы отбор кандидатов был виден и
	// проверяем, а не молчаливо сужал предмет.
	TestsParsed int
	// Unparsed — файлы, которые разобрать не удалось. Гейт обязан отказаться
	// судить дерево, часть которого он не прочитал: «не разобрали» не может
	// молча означать «вызова нет».
	Unparsed []string
	// Constrained — сервис → файлы миграций, объявляющие канон формы.
	Constrained map[string][]string
	// Probed — сервис → файлы проб с ИСПОЛНЯЕМЫМ вызовом двигателя.
	Probed map[string][]string
	// Mentioned — сервис → файлы проб, где имя двигателя встречается ТЕКСТОМ.
	// Диагностика, не вердикт (см. nameFormProbeMention).
	Mentioned map[string][]string
}

// Services возвращает сервисы, ставящие форму, в устойчивом порядке.
func (c nameFormDBCoverage) Services() []string {
	out := make([]string, 0, len(c.Constrained))
	for svc := range c.Constrained {
		out = append(out, svc)
	}
	sort.Strings(out)
	return out
}

// ProofFiles — сколько файлов несут исполняемый вызов. Часть переписи: без него
// «доказано» не отличить от «доказано одним файлом, который завтра снимут», а
// расхождение с числом ТЕКСТОВЫХ упоминаний (Mentioned) — прямой признак того,
// что где-то осталась выпотрошенная проба.
func (c nameFormDBCoverage) ProofFiles() int {
	n := 0
	for _, fs := range c.Probed {
		n += len(fs)
	}
	return n
}

// MentionFiles — сколько файлов упоминают двигатель ТЕКСТОМ (прежний, отменённый
// признак). Печатается рядом с ProofFiles именно ради их сравнения.
func (c nameFormDBCoverage) MentionFiles() int {
	n := 0
	for _, fs := range c.Mentioned {
		n += len(fs)
	}
	return n
}

// Unproven — сервисы, которые форму ставят и действие её ничем не доказывают.
func (c nameFormDBCoverage) Unproven() []string {
	out := []string{}
	for _, svc := range c.Services() {
		if len(c.Probed[svc]) == 0 {
			out = append(out, svc)
		}
	}
	return out
}

// analyseNameFormDBCoverage разбирает соответствие «путь → содержимое».
//
// Принимает уже прочитанное, а не читает само, чтобы инъекция подавала
// синтетическое дерево тем же входом, каким гейт получает настоящее.
//
// entryMethods — входные методы двигателя. Приезжают ПАРАМЕТРОМ из самого
// двигателя (гейт выводит их разбором `pkg/nameformdb`), а не выписаны
// здесь: выписанный перечень разошёлся бы с двигателем молча — переименовали бы
// `Run`, и гейт перестал бы видеть все пробы разом, объявив дерево сломанным.
func analyseNameFormDBCoverage(files map[string]string, canonPattern string, entryMethods map[string]bool) nameFormDBCoverage {
	cov := nameFormDBCoverage{
		Constrained: map[string][]string{},
		Probed:      map[string][]string{},
		Mentioned:   map[string][]string{},
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Файлы проб группируются по каталогу: достижимость вызова от точки входа
	// разрешается В ПРЕДЕЛАХ ПАКЕТА, а помощник, возвращающий пробу, вправе
	// лежать в соседнем файле того же каталога.
	testsByDir := map[string][]string{}

	// Миграции собираются отдельно и судятся В ПОРЯДКЕ ПРИМЕНЕНИЯ: форму можно не
	// только поставить, но и снять, а «поставлена» и «стоит сейчас» — разные
	// утверждения (см. nameFormWithdrawnRe).
	migrationsBySvc := map[string][]string{}

	for _, p := range paths {
		rel := filepath.ToSlash(p)
		svc, ok := nameFormServiceOf(rel)
		if !ok {
			continue
		}
		body := files[p]

		switch {
		case strings.HasSuffix(rel, ".sql") &&
			strings.Contains(rel, "/internal/migrations/"):
			cov.MigrationsRead++
			migrationsBySvc[svc] = append(migrationsBySvc[svc], rel)
		case strings.HasSuffix(rel, "_test.go"):
			cov.TestsRead++
			testsByDir[path.Dir(rel)] = append(testsByDir[path.Dir(rel)], rel)
			if strings.Contains(body, nameFormProbeMention) {
				cov.Mentioned[svc] = append(cov.Mentioned[svc], rel)
			}
		}
	}

	// Состояние формы у сервиса — исход применения его миграций ПО ПОРЯДКУ.
	for svc, rels := range migrationsBySvc {
		if declaring := nameFormDeclaringMigrations(files, rels, canonPattern); len(declaring) > 0 {
			cov.Constrained[svc] = declaring
		}
	}

	dirs := make([]string, 0, len(testsByDir))
	for d := range testsByDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		rels := testsByDir[dir]
		if !dirImportsNameFormEngine(files, rels) {
			continue
		}
		cov.TestsParsed += len(rels)
		proofs, unparsed := scanPackageForProbeCalls(files, rels, entryMethods)
		cov.Unparsed = append(cov.Unparsed, unparsed...)
		for _, rel := range proofs {
			svc, ok := nameFormServiceOf(rel)
			if !ok {
				continue
			}
			cov.Probed[svc] = append(cov.Probed[svc], rel)
		}
	}
	for svc := range cov.Probed {
		cov.Probed[svc] = uniqueSorted(cov.Probed[svc])
	}
	sort.Strings(cov.Unparsed)
	return cov
}

// dirImportsNameFormEngine — есть ли в каталоге хоть один файл, где встречается
// путь пакета-двигателя. См. оговорку у nameFormEnginePkgPath: это отбор
// кандидатов на разбор, а не признак доказательства.
func dirImportsNameFormEngine(files map[string]string, rels []string) bool {
	for _, rel := range rels {
		if strings.Contains(files[rel], nameFormEnginePkgPath) {
			return true
		}
	}
	return false
}

// scanPackageForProbeCalls возвращает файлы каталога, несущие ИСПОЛНЯЕМЫЙ вызов
// входного метода двигателя, и файлы, которые разобрать не удалось.
//
// Достижимость считается по графу вызовов внутри пакета: корни — точки входа
// прогона (`Test*`/`Benchmark*`/`Fuzz*`/`Example*`), рёбра — имена вызываемых
// функций и методов. Это ОГРУБЛЕНИЕ в сторону достижимости: метод и функция с
// одинаковым именем для графа неразличимы, поэтому граф скорее признает
// достижимым лишнее, чем пропустит настоящее. Огрубление выбрано осознанно —
// ложное красное на живой пробе отключило бы гейт, а ложное зелёное здесь
// требует, чтобы у мёртвого помощника нашёлся одноимённый живой.
//
// Слепое пятно названо прямо: вызов ВНЕ объявления функции (в инициализаторе
// пакетной переменной) разбор не ищет. Такой формы в дереве нет, а появится она
// ЗАМЕТНО — гейт объявит сервис недоказанным, то есть пятно отказывает в
// сторону красного, а не тихого зелёного.
func scanPackageForProbeCalls(files map[string]string, rels []string, entryMethods map[string]bool) (proofs []string, unparsed []string) {
	type parsedFile struct {
		rel   string
		file  *ast.File
		local string // локальное имя пакета-двигателя в этом файле ("" — не импортирован)
	}

	fset := token.NewFileSet()
	var ps []parsedFile
	for _, rel := range rels {
		// Без parser.ParseComments: комментарии в дерево не попадают вовсе,
		// поэтому «упоминание в комментарии» не может стать вызовом ни при
		// какой ошибке ниже по коду.
		f, err := parser.ParseFile(fset, rel, files[rel], parser.SkipObjectResolution)
		if err != nil {
			unparsed = append(unparsed, rel)
			continue
		}
		ps = append(ps, parsedFile{rel: rel, file: f, local: nameFormEngineLocalName(f)})
	}

	// Пакетное знание: какие функции ВОЗВРАЩАЮТ пробу. Без него помощник вида
	// `func geoNameFormProbe(...) nameformdb.Probe` и вызов
	// `geoNameFormProbe(t, pool).Check(...)` остались бы незамеченными —
	// а это законная и уже применённая в дереве форма.
	probeReturning := map[string]bool{}
	for _, p := range ps {
		if p.local == "" {
			continue
		}
		for _, d := range p.file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Type.Results == nil {
				continue
			}
			for _, res := range fn.Type.Results.List {
				if isNameFormProbeType(res.Type, p.local) {
					probeReturning[fn.Name.Name] = true
				}
			}
		}
	}

	// Граф вызовов пакета и множество функций, несущих доказательство.
	callees := map[string]map[string]bool{}
	proofFuncs := map[string][]string{} // имя функции → файлы, где найден вызов

	for _, p := range ps {
		for _, d := range p.file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name := fn.Name.Name
			if callees[name] == nil {
				callees[name] = map[string]bool{}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch f := call.Fun.(type) {
				case *ast.Ident:
					callees[name][f.Name] = true
				case *ast.SelectorExpr:
					callees[name][f.Sel.Name] = true
				}
				return true
			})

			// Локального имени двигателя у файла может не быть вовсе, и это
			// законно: значение приезжает возвратом помощника из соседнего
			// файла пакета. Пустое имя ни с одним идентификатором не совпадёт,
			// поэтому опознание по типу просто не сработает, а опознание по
			// помощнику — сработает.
			bound := nameFormProbeBindings(fn, p.local, probeReturning)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !entryMethods[sel.Sel.Name] {
					return true
				}
				if !nameFormExprYieldsProbe(sel.X, p.local, bound, probeReturning) {
					return true
				}
				proofFuncs[name] = append(proofFuncs[name], p.rel)
				return true
			})
		}
	}

	if len(proofFuncs) == 0 {
		return nil, unparsed
	}

	// Достижимость от точек входа прогона.
	reachable := map[string]bool{}
	var queue []string
	for name := range callees {
		if isTestEntryFuncName(name) {
			reachable[name] = true
			queue = append(queue, name)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for callee := range callees[cur] {
			if reachable[callee] {
				continue
			}
			if _, declared := callees[callee]; !declared {
				continue
			}
			reachable[callee] = true
			queue = append(queue, callee)
		}
	}

	for fn, where := range proofFuncs {
		if !reachable[fn] {
			continue
		}
		proofs = append(proofs, where...)
	}
	return uniqueSorted(proofs), unparsed
}

// nameFormEngineLocalName — под каким именем файл знает пакет-двигатель.
// Псевдоним импорта учитывается: гейт не вправе требовать одного написания.
func nameFormEngineLocalName(f *ast.File) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if !strings.HasSuffix(p, "/"+nameFormEnginePkgPath) && p != nameFormEnginePkgPath {
			continue
		}
		if imp.Name != nil {
			// `_` и `.` пробу не называют: под первым к пакету не обратиться,
			// второй здесь не встречается и опознан быть не может.
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				return ""
			}
			return imp.Name.Name
		}
		return path.Base(p)
	}
	return ""
}

// isNameFormProbeType — тип `<local>.Probe` или указатель на него.
func isNameFormProbeType(e ast.Expr, local string) bool {
	switch t := e.(type) {
	case *ast.StarExpr:
		return isNameFormProbeType(t.X, local)
	case *ast.SelectorExpr:
		id, ok := t.X.(*ast.Ident)
		return ok && id.Name == local && t.Sel.Name == nameFormEngineType
	}
	return false
}

// nameFormProbeBindings — имена внутри функции, за которыми стоит проба:
// параметры и получатель нужного типа плюс переменные, которым пробу присвоили.
//
// Обход повторяется дважды, чтобы поймать цепочку `a := <проба>; b := a`:
// одного прохода для неё не хватает, а полноценного анализа потока данных
// предмет не стоит.
func nameFormProbeBindings(fn *ast.FuncDecl, local string, probeReturning map[string]bool) map[string]bool {
	bound := map[string]bool{}

	addFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			if !isNameFormProbeType(f.Type, local) {
				continue
			}
			for _, n := range f.Names {
				bound[n.Name] = true
			}
		}
	}
	addFields(fn.Recv)
	addFields(fn.Type.Params)

	for pass := 0; pass < 2; pass++ {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				for i, lhs := range s.Lhs {
					if i >= len(s.Rhs) {
						break
					}
					id, ok := lhs.(*ast.Ident)
					if ok && nameFormExprYieldsProbe(s.Rhs[i], local, bound, probeReturning) {
						bound[id.Name] = true
					}
				}
			case *ast.ValueSpec:
				for i, nm := range s.Names {
					if i < len(s.Values) &&
						nameFormExprYieldsProbe(s.Values[i], local, bound, probeReturning) {
						bound[nm.Name] = true
					}
					if s.Type != nil && isNameFormProbeType(s.Type, local) {
						bound[nm.Name] = true
					}
				}
			}
			return true
		})
	}
	return bound
}

// nameFormExprYieldsProbe — даёт ли выражение значение пробы.
func nameFormExprYieldsProbe(e ast.Expr, local string, bound, probeReturning map[string]bool) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return nameFormExprYieldsProbe(x.X, local, bound, probeReturning)
	case *ast.UnaryExpr:
		return nameFormExprYieldsProbe(x.X, local, bound, probeReturning)
	case *ast.StarExpr:
		return nameFormExprYieldsProbe(x.X, local, bound, probeReturning)
	case *ast.CompositeLit:
		return isNameFormProbeType(x.Type, local)
	case *ast.Ident:
		return bound[x.Name]
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok {
			return probeReturning[id.Name]
		}
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			return probeReturning[sel.Sel.Name]
		}
	}
	return false
}

// isTestEntryFuncName — точка входа прогона. `TestMain` под тот же признак
// подпадает и это верно: из него исполняется всё, что он зовёт.
func isTestEntryFuncName(name string) bool {
	for _, p := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return in
	}
	sort.Strings(in)
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

// nameFormServiceOf выделяет имя сервиса из пути `services/<svc>/…`.
//
// Своя, а не соседняя `serviceOfPath` из переписи политики отзыва: та на пути
// вне `services/` возвращает САМ ПУТЬ, потому что ей нужен ключ переписи на
// любой вход. Здесь такой исход завёл бы фантомный «сервис» с именем файла и
// перепись перестала бы что-либо значить, поэтому нужен ответ «это не сервис».
func nameFormServiceOf(rel string) (string, bool) {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != "services" {
		return "", false
	}
	return parts[1], true
}

// nameFormWithdrawnRe — оператор, которым форма СНИМАЕТСЯ вместе со своей
// колонкой. Читается только из исполняемой части (см. nameFormUpSection): в
// комментарии, объясняющем снятие, тот же текст стоит законно.
//
// Признак выбран по существу, а не по имени ограничения: `ALTER TABLE … DROP
// CONSTRAINT <t>_name_check` встречается и в ОТКАТЕ самой миграции 715001 —
// то есть в том же файле, который форму ставит, — и по нему форма выглядела бы
// снятой сразу же. Снятие колонки такой двусмысленности не имеет: `name` больше
// нет, значит и формы на нём нет.
var nameFormWithdrawnRe = regexp.MustCompile(`(?i)DROP\s+COLUMN\s+(IF\s+EXISTS\s+)?name\b`)

// nameFormMigrationVersionRe — числовой префикс имени файла миграции. Порядок
// применения ЧИСЛОВОЙ (его так берёт мигратор), и лексикографический с ним
// расходится: `1000001` меньше `539001` как строка и больше как число.
var nameFormMigrationVersionRe = regexp.MustCompile(`^(\d+)_`)

// nameFormDeclaringMigrations — файлы, которыми форма имени стоит У СЕРВИСА
// СЕЙЧАС; пусто, если её сняли и не вернули.
//
// Почему состояние, а не «встречается в тексте». Текст применённой миграции не
// меняется никогда: сняв колонку, мы не можем убрать канон из файла, который её
// заводил (ban #5). Гейт, читающий одно лишь присутствие канона, продолжал бы
// требовать доказательства ограничения, которого в схеме уже нет, — то самое
// «утверждение, пережившее свой предмет», от которого гейты и защищают.
//
// Порядок разрешения — порядок применения: побеждает ПОСЛЕДНЕЕ решение. Значит
// возвращение формы новой миграцией снова включает требование само, без правки
// гейта и без перечня исключений.
//
// Внутри ОДНОЙ миграции, объявляющей и то и другое, побеждает объявление формы:
// вопрос «в каком порядке идут операторы» гейт не решает, и осторожная сторона
// здесь — потребовать доказательство.
func nameFormDeclaringMigrations(files map[string]string, rels []string, canonPattern string) []string {
	ordered := append([]string(nil), rels...)
	sort.Slice(ordered, func(i, j int) bool {
		vi, vj := nameFormMigrationVersion(ordered[i]), nameFormMigrationVersion(ordered[j])
		if vi != vj {
			return vi < vj
		}
		return ordered[i] < ordered[j]
	})

	var declaring []string
	for _, rel := range ordered {
		up := nameFormUpSection(files[rel])
		declares := canonPattern != "" && strings.Contains(up, canonPattern)
		switch {
		case declares:
			declaring = append(declaring, rel)
		case nameFormWithdrawnRe.MatchString(up):
			declaring = nil
		}
	}
	return declaring
}

// nameFormMigrationVersion — числовой префикс имени файла; 0, если префикса нет
// (такой файл сортируется первым и на исход не влияет: решает последнее решение).
func nameFormMigrationVersion(rel string) int64 {
	m := nameFormMigrationVersionRe.FindStringSubmatch(path.Base(rel))
	if m == nil {
		return 0
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// nameFormUpSection — исполняемая часть миграции: секция `-- +goose Up` без
// комментариев.
//
// Две причины читать именно её. ОТКАТ — не действующее состояние схемы: секция
// `Down` описывает возвращение того, что сейчас снято, и принимать её за
// объявление значило бы считать снятое поставленным. КОММЕНТАРИЙ — не оператор:
// разбор, ловящий объяснение защиты, — ровно тот класс, который гейты и ловят
// (`testing.md` §«Гейт на класс», п. 4).
//
// Файл без маркера `Up` читается целиком: это не форма goose, и молча объявлять
// такой файл пустым нельзя — «не разобрали» не может означать «ничего нет».
func nameFormUpSection(body string) string {
	const upMarker = "-- +goose Up"
	const downMarker = "-- +goose Down"

	up := body
	if i := strings.Index(up, upMarker); i >= 0 {
		up = up[i+len(upMarker):]
	}
	if i := strings.Index(up, downMarker); i >= 0 {
		up = up[:i]
	}

	var b strings.Builder
	for _, line := range strings.Split(up, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
