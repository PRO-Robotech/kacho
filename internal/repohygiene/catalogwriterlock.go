// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogwriterlock.go — разбор прод-дерева на предмет писателя строк
// `kacho_iam.catalog_*`, который НЕ берёт глобальный транзакционный замок
// каталога (приёмка
// `services/iam/docs/engineering/acceptance/plan-confirms-what-apply-withdraws.md`
// §7, держатель Г1; объём О11; предпосылки П43 и П1).
//
// # Предмет
//
// Подтверждение применения (отпечаток состояния модуля) есть CAS **только
// потому**, что между чтением отпечатка и записью строк не может встать второй
// писатель. Обеспечивает это не сравнение, а `pg_advisory_xact_lock`, взятый
// первым оператором транзакции: сравнение под конкуренцией сравнивало бы
// состояние, которое сосед меняет прямо сейчас, и «совпало» означало бы «совпало
// на момент чтения» — то есть ничего.
//
// Свойство сегодня истинно и НЕ УДЕРЖАНО НИЧЕМ. Замер дня заведения гейта:
// текстовый предикат по операторам записи даёт **2** прод-файла, исполняющий
// среди них **1** (`repo/kacho/pg/catalog_writer.go`), и он замок берёт;
// `grep -rln 'CatalogLockKey' internal/repohygiene/` → **0**.
//
// Истечёт свойство МОЛЧА. Заведут второго писателя — отпечаток продолжит
// сравниваться, проба подтверждения останется зелёной (она гоняет один
// применитель против одной базы), и «CAS» превратится в форму без содержания:
// код на месте, сравнение исполняется, сериализовать его больше нечему.
//
// # Гейт судит УЗЕЛ РАЗБОРА, а не подстроку — и это НЕСУЩЕЕ
//
// `services/iam/internal/check/catalog_seed_parity.go` несёт РОВНО ТЕ ЖЕ
// операторы: он разбирает текст миграции и сверяет посев с манифестом, поэтому
// строки `INSERT INTO kacho_iam.catalog_module (module) VALUES` лежат у него
// литералами и константами. Он обязан МОЛЧАТЬ: разбирать оператор и исполнять
// его — разные вещи, а гейт, судящий подстроку, краснел бы на файле, который
// ничего не нарушает, и был бы снят первым читателем.
//
// Различает их не слово, а то, КУДА литерал уезжает:
//
//	исполнение  литерал — аргумент вызова `Exec`/`Query`/`QueryRow`/…
//	            ЛИБО вызова функции пакета, которая сама передаёт свой параметр
//	            такому вызову (`catalogWriter.changed`)
//	разбор      литерал — аргумент `parseSeedBlock`, чьё тело базы не касается
//
// # Единица суждения — ТИП, а не файл и не пакет
//
// Замок берёт `catalogWriter.LockCatalog`, и обязателен он тому, кто пишет.
// Единица «пакет» пропустила бы второй пишущий тип в том же пакете: замок в
// пакете есть, берёт его кто-то другой. Единица «файл» разошлась бы с деревом на
// первом же типе, чьи методы лежат в двух файлах.
//
// Поэтому единица — ПОЛУЧАТЕЛЬ метода (`catalogWriter`), а для функции без
// получателя — она сама (`func:<имя>`), и вердикт по ней fail-closed: по разбору
// нельзя установить, что свободную функцию зовут только из-под замка, а догадка
// в разрешающую сторону открыла бы гейт ровно тем приёмом, которым его обходят
// молча. Чинится это тем, что и без гейта верно: писатель — метод типа, который
// умеет запирать каталог.
//
// # Замок обязан быть ТРАНЗАКЦИОННЫМ, и сессионный не считается
//
// `pg_advisory_xact_lock` снимается коммитом И откатом, поэтому оборванный
// применитель не оставляет каталог запертым. Сессионная форма
// (`pg_advisory_lock`) держится до явного снятия либо до возврата соединения в
// пул — то есть оборванный применитель запирает каталог на неопределённый срок,
// а следующий писатель ждёт того, кого уже нет. Образец требует `_xact_`
// намеренно; подмена формы — находка, и это доказано инъекцией.
//
// # Ключ замка обязан быть ТЕМ ЖЕ
//
// Замок на чужом ключе не сериализует ничего: два писателя на разных ключах
// проходят одновременно, и «замок взят» становится утверждением о вызове, а не о
// свойстве. Принимается имя `CatalogLockKey` (в том числе через селектор пакета)
// либо его значение литералом.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. запрос, собранный из кусков в рантайме (`"INSERT INTO " + tbl`). Форма,
//     которой в дереве нет; заведись она — её держал бы обзор, а не разбор;
//  2. литерал, уехавший через ЧУЖОЙ пакет (writer передаёт SQL соседу, тот
//     исполняет). Разбор идёт по пакету, и цепочка через границу пакета ему не
//     видна; такой формы в дереве нет;
//  3. ПОРЯДОК: гейт требует, чтобы замок брался, но не доказывает, что он берётся
//     ПЕРВЫМ оператором. Порядок держит интеграционная проба `-18` (два
//     конкурирующих применителя за тот же ключ) — она и есть производитель этой
//     половины, а разбором «первым оператором» не устанавливается: путь до
//     первого `Exec` проходит через ветки.
//
// # Имя функции — над-приближение, и оно fail-closed
//
// Исполнителей гейт опознаёт ПО ИМЕНИ в пределах пакета: `w.changed(...)` и
// `changed(...)` суть один узел вызова с разной формой обращения. Два метода с
// одним именем на разных типах слились бы в одного исполнителя — то есть
// исполнителей стало бы БОЛЬШЕ, а писателей строже. Ошибка в разрешающую сторону
// этим приёмом невыразима.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// CatalogSource — один прод-файл на вход разбора. Состав приходит ПАРАМЕТРОМ:
// в живом дереве его даёт индекс git, инъекция подаёт синтетический —
// доказательство, требующее испортить рабочую копию, в конвейере не исполняется
// никогда.
type CatalogSource struct {
	Path string
	Src  []byte
}

// CatalogWriteFinding — координата находки: писатель без замка.
type CatalogWriteFinding struct {
	File string
	Line int
	// Unit — единица суждения: имя получателя либо `func:<имя>`.
	Unit string
	// What — начало оператора, чтобы находка называла предмет, а не только место.
	What string
	// Why — чем именно единица негодна: замка нет вовсе, замок сессионный,
	// замок на чужом ключе. Чинятся они по-разному, и находка, их не
	// различающая, посылает читателя не туда.
	Why string
}

// CatalogWriteCensus — объём осмотренного. Печатается ВСЕГДА, на всяком исходе:
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
//
// Две оси названы отдельно и вместе составляют предпосылку П43:
// `TextMatches`/`TextFiles` — что нашёл бы ТЕКСТОВЫЙ предикат;
// `Executed`/`ExecutingFiles` — что из этого действительно ИСПОЛНЯЕТСЯ.
// Расхождение этих пар и есть разница между «судит подстроку» и «судит узел
// разбора»; равенство означало бы, что различение ничего не различает.
type CatalogWriteCensus struct {
	Files          int
	Parsed         int
	Funcs          int
	Executors      int
	StringLiterals int
	Comments       int
	TextMatches    int
	TextFiles      int
	Executed       int
	ExecutingFiles int
	WriteUnits     int
	LockedUnits    int
	LockSites      int
}

// catalogWriteStmtRe — оператор ЗАПИСИ над строкой каталога.
//
// Привязка к началу строки — по тому же доводу, что у соседних гейтов: запрос
// НАЧИНАЕТСЯ глаголом (возможно после отступа, перевода строки или общего
// табличного выражения), а проза о самом запрете несёт те же слова посреди
// предложения. Три таблицы перечислены поимённо: `catalog_*` без перечня накрыл
// бы любую будущую таблицу с этим префиксом, о запирании которой никто не решал.
var catalogWriteStmtRe = regexp.MustCompile(
	`(?im)^\s*(with\b[^\n]*\n\s*)?(insert\s+into|update)\s+(kacho_iam\.)?catalog_(module|resource|verb)\b`)

// catalogXactLockRe — ТРАНЗАКЦИОННЫЙ консультативный замок. Сессионная форма под
// образец не подпадает намеренно (шапка файла).
var catalogXactLockRe = regexp.MustCompile(`(?i)\bpg_advisory_xact_lock\s*\(`)

// catalogSessionLockRe — сессионная форма. Нужна не ради запрета, а ради ТЕКСТА
// находки: «замка нет» и «замок не тот» чинятся по-разному.
var catalogSessionLockRe = regexp.MustCompile(`(?i)\bpg_advisory_lock\s*\(`)

// catalogLockKeyIdent — имя константы ключа.
const catalogLockKeyIdent = "CatalogLockKey"

// catalogLockKeyValue — её значение. Принимается и литералом: проба, состязающаяся
// за замок, вправе назвать ключ прямо, и объявлять это находкой не за что.
const catalogLockKeyValue = "kacho_iam.module_catalog"

// catalogExecSelectors — методы, ИСПОЛНЯЮЩИЕ оператор.
//
// Перечень объявлен здесь один раз, а не выведен из сегодняшней раскладки
// писателя: гейт, знающий только `QueryRow`, молчал бы на форме столь же
// законной. `Queue` — постановка в пакет операторов (`pgx.Batch`), `CopyFrom` —
// потоковая вставка: обе пишут, обе обязаны быть под замком.
var catalogExecSelectors = map[string]bool{
	"Exec": true, "Query": true, "QueryRow": true,
	"SendBatch": true, "Queue": true, "CopyFrom": true,
}

// ScanCatalogWriteLocking разбирает состав прод-файлов и отвечает, какие
// единицы пишут строки каталога, не запирая его.
//
// Файлы группируются ПО ПАКЕТУ (каталогу): исполнитель и его вызывающий вправе
// лежать в разных файлах одного пакета, и разбор по одному файлу пропустил бы
// ровно живую раскладку писателя.
func ScanCatalogWriteLocking(files []CatalogSource) ([]CatalogWriteFinding, CatalogWriteCensus, error) {
	var census CatalogWriteCensus

	byPkg := map[string][]CatalogSource{}
	var pkgs []string
	for _, f := range files {
		dir := filepath.ToSlash(filepath.Dir(f.Path))
		if _, seen := byPkg[dir]; !seen {
			pkgs = append(pkgs, dir)
		}
		byPkg[dir] = append(byPkg[dir], f)
		census.Files++
	}
	sort.Strings(pkgs)

	var findings []CatalogWriteFinding
	for _, dir := range pkgs {
		f, c, err := scanCatalogPackage(dir, byPkg[dir])
		if err != nil {
			return nil, census, err
		}
		findings = append(findings, f...)
		census.Parsed += c.Parsed
		census.Funcs += c.Funcs
		census.Executors += c.Executors
		census.StringLiterals += c.StringLiterals
		census.Comments += c.Comments
		census.TextMatches += c.TextMatches
		census.TextFiles += c.TextFiles
		census.Executed += c.Executed
		census.ExecutingFiles += c.ExecutingFiles
		census.WriteUnits += c.WriteUnits
		census.LockedUnits += c.LockedUnits
		census.LockSites += c.LockSites
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, census, nil
}

// catalogUnitState — что известно об одной единице суждения.
type catalogUnitState struct {
	unit        string
	writes      []CatalogWriteFinding
	xactLocked  bool
	sessionOnly bool
	wrongKey    bool
}

func scanCatalogPackage(dir string, files []CatalogSource) ([]CatalogWriteFinding, CatalogWriteCensus, error) {
	var census CatalogWriteCensus
	fset := token.NewFileSet()

	type parsed struct {
		rel  string
		file *ast.File
	}
	var ps []parsed
	for _, src := range files {
		file, err := parser.ParseFile(fset, src.Path, src.Src, parser.ParseComments)
		if err != nil {
			return nil, census, err
		}
		census.Parsed++
		ps = append(ps, parsed{rel: filepath.ToSlash(src.Path), file: file})
	}

	// ── Идентификаторы, СВЯЗАННЫЕ с оператором записи ────────────────────────
	//
	// Оператор, вынесенный в константу и переданный по имени, — форма столь же
	// исполняемая; разбор, знающий только литерал в аргументе, молчал бы на ней.
	sqlNames := map[string]bool{}
	for _, p := range ps {
		ast.Inspect(p.file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.ValueSpec:
				for i, name := range v.Names {
					if i < len(v.Values) && catalogLiteralWrites(v.Values[i]) {
						sqlNames[name.Name] = true
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range v.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || i >= len(v.Rhs) {
						continue
					}
					if catalogLiteralWrites(v.Rhs[i]) {
						sqlNames[id.Name] = true
					}
				}
			}
			return true
		})
	}

	// ── Перепись текста: что нашёл бы ТЕКСТОВЫЙ предикат ─────────────────────
	for _, p := range ps {
		hit := false
		for _, g := range p.file.Comments {
			census.Comments += len(g.List)
		}
		ast.Inspect(p.file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			census.StringLiterals++
			if catalogWriteStmtRe.MatchString(catalogLitText(lit.Value)) {
				census.TextMatches++
				hit = true
			}
			return true
		})
		if hit {
			census.TextFiles++
		}
	}

	// ── Исполнители пакета: функции, чей ПАРАМЕТР уезжает в оператор ─────────
	executors := map[string]bool{}
	for {
		grew := false
		for _, p := range ps {
			for _, decl := range p.file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if executors[fn.Name.Name] {
					continue
				}
				if catalogFuncForwardsAParamToAnExecutor(fn, executors) {
					executors[fn.Name.Name] = true
					grew = true
				}
			}
		}
		if !grew {
			break
		}
	}
	census.Executors = len(executors)

	// ── Места записи и места замка ───────────────────────────────────────────
	units := map[string]*catalogUnitState{}
	unitOf := func(name string) *catalogUnitState {
		if u, ok := units[name]; ok {
			return u
		}
		u := &catalogUnitState{unit: name}
		units[name] = u
		return u
	}

	for _, p := range ps {
		fileExecuted := 0
		for _, decl := range p.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			census.Funcs++
			name := dir + "::" + catalogUnitName(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !catalogCallExecutes(call, executors) {
					return true
				}
				for _, arg := range call.Args {
					if !catalogArgWrites(arg, sqlNames) {
						continue
					}
					u := unitOf(name)
					u.writes = append(u.writes, CatalogWriteFinding{
						File: p.rel,
						Line: fset.Position(arg.Pos()).Line,
						Unit: catalogUnitName(fn),
						What: catalogFirstLine(arg, sqlNames),
					})
					census.Executed++
					fileExecuted++
				}
				if kind, key := catalogCallLocks(call); kind != "" {
					u := unitOf(name)
					census.LockSites++
					switch {
					case kind == "xact" && key:
						u.xactLocked = true
					case kind == "xact":
						u.wrongKey = true
					default:
						u.sessionOnly = true
					}
				}
				return true
			})
		}
		if fileExecuted > 0 {
			census.ExecutingFiles++
		}
	}

	var findings []CatalogWriteFinding
	for _, u := range units {
		if len(u.writes) == 0 {
			continue
		}
		census.WriteUnits++
		if u.xactLocked {
			census.LockedUnits++
			continue
		}
		why := "замка каталога не берёт вовсе"
		switch {
		case u.sessionOnly:
			why = "берёт СЕССИОННЫЙ замок (`pg_advisory_lock`): он не снимается " +
				"откатом, и оборванный применитель запирает каталог до возврата " +
				"соединения в пул"
		case u.wrongKey:
			why = "берёт транзакционный замок на ЧУЖОМ ключе: два писателя на разных " +
				"ключах проходят одновременно, и «замок взят» становится утверждением " +
				"о вызове, а не о свойстве"
		}
		w := u.writes[0]
		w.Why = why
		findings = append(findings, w)
	}
	return findings, census, nil
}

// catalogFuncForwardsAParamToAnExecutor — предикат исполнителя: тело зовёт
// исполняющий метод (либо уже известного исполнителя пакета) и передаёт ему
// СВОЙ параметр.
//
// Требование к параметру — несущее: без него исполнителем стала бы всякая
// функция, где рядом стоит любой `Exec`, и предмет разбора расплылся бы на весь
// пакет.
func catalogFuncForwardsAParamToAnExecutor(fn *ast.FuncDecl, known map[string]bool) bool {
	params := map[string]bool{}
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, n := range field.Names {
				params[n.Name] = true
			}
		}
	}
	if len(params) == 0 {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !catalogCallExecutes(call, known) {
			return true
		}
		for _, arg := range call.Args {
			id, ok := arg.(*ast.Ident)
			if ok && params[id.Name] {
				found = true
			}
		}
		return true
	})
	return found
}

// catalogCallExecutes — вызов ИСПОЛНЯЕТ оператор: либо это метод драйвера, либо
// уже опознанный исполнитель пакета. Имя берётся из узла вызова, поэтому
// `w.changed(…)` и `changed(…)` — одна и та же форма обращения.
func catalogCallExecutes(call *ast.CallExpr, known map[string]bool) bool {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		return catalogExecSelectors[fn.Sel.Name] || known[fn.Sel.Name]
	case *ast.Ident:
		return known[fn.Name]
	}
	return false
}

// catalogArgWrites — аргумент несёт оператор записи: литералом либо по имени
// связанной с ним константы.
func catalogArgWrites(arg ast.Expr, sqlNames map[string]bool) bool {
	if catalogLiteralWrites(arg) {
		return true
	}
	id, ok := arg.(*ast.Ident)
	return ok && sqlNames[id.Name]
}

// catalogLiteralWrites — выражение есть строковый литерал с оператором записи.
func catalogLiteralWrites(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	return catalogWriteStmtRe.MatchString(catalogLitText(lit.Value))
}

// catalogCallLocks — вызов берёт консультативный замок. Первое значение — форма
// (`xact` либо `session`, пусто = не замок), второе — назван ли ТОТ ключ.
func catalogCallLocks(call *ast.CallExpr) (kind string, rightKey bool) {
	for _, arg := range call.Args {
		lit, ok := arg.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		text := catalogLitText(lit.Value)
		switch {
		case catalogXactLockRe.MatchString(text):
			kind = "xact"
		case catalogSessionLockRe.MatchString(text):
			if kind == "" {
				kind = "session"
			}
		}
	}
	if kind == "" {
		return "", false
	}
	for _, arg := range call.Args {
		switch v := arg.(type) {
		case *ast.Ident:
			if v.Name == catalogLockKeyIdent {
				rightKey = true
			}
		case *ast.SelectorExpr:
			if v.Sel.Name == catalogLockKeyIdent {
				rightKey = true
			}
		case *ast.BasicLit:
			if v.Kind == token.STRING && strings.Contains(catalogLitText(v.Value), catalogLockKeyValue) {
				rightKey = true
			}
		}
	}
	return kind, rightKey
}

// catalogUnitName — единица суждения: получатель метода либо сама функция.
func catalogUnitName(fn *ast.FuncDecl) string {
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		if n := catalogRecvTypeName(fn.Recv.List[0].Type); n != "" {
			return n
		}
	}
	return "func:" + fn.Name.Name
}

func catalogRecvTypeName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.StarExpr:
		return catalogRecvTypeName(v.X)
	case *ast.Ident:
		return v.Name
	case *ast.IndexExpr:
		return catalogRecvTypeName(v.X)
	case *ast.IndexListExpr:
		return catalogRecvTypeName(v.X)
	}
	return ""
}

// catalogLitText — содержимое литерала БЕЗ кавычек. Обязательно: образец привязан
// к началу строки, а `Value` несёт открывающую кавычку — без снятия привязка не
// срабатывала бы никогда, и гейт молчал бы всегда.
func catalogLitText(v string) string {
	if u, err := strconv.Unquote(v); err == nil {
		return u
	}
	return strings.Trim(v, "`\"")
}

// catalogFirstLine — начало оператора для текста находки: целый запрос в
// сообщении нечитаем, а координата и первая строка называют место однозначно.
func catalogFirstLine(arg ast.Expr, sqlNames map[string]bool) string {
	lit, ok := arg.(*ast.BasicLit)
	if !ok {
		if id, ok := arg.(*ast.Ident); ok && sqlNames[id.Name] {
			return "оператор по имени `" + id.Name + "`"
		}
		return "оператор"
	}
	s := strings.TrimSpace(catalogLitText(lit.Value))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return strings.TrimSpace(s)
}
