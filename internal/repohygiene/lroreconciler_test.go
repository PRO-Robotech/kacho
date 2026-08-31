// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// lroreconciler_test.go — гейт против осиротевших операций: сервис, который
// запускает асинхронные мутации, обязан иметь разрешителя осиротевших операций.
//
// Предмет. Мутация у нас возвращает Operation, и клиент поллит её до терминала.
// Строка операции коммитится ДО запуска фоновой работы, а живой исполнитель
// добирает только то, что диспетчеризовал сам ЭТОТ процесс. Значит смерть
// процесса посреди работы (перекат, OOM, SIGKILL), исчерпание бюджета
// терминальной записи и переполнение очереди исполнителя оставляют строку
// «в процессе» НАВСЕГДА: клиент поллит её до конца своего терпения и не узнаёт
// исхода ни разу — ни успеха, ни отказа. На переполнении очереди мутация вообще
// не выполняется, а идентификатор работы у клиента остаётся.
//
// Разрешитель (pkg/operations.Reconciler) сверяет осиротевшую строку с тем, что
// реально закоммичено, и приводит её в терминал. Он не переигрывает работу — он
// перестаёт врать про её состояние.
//
// Почему гейт, а не «починить найденный сервис». Класс жил в ДВУХ сервисах
// одновременно, причём один из них носит частичный индекс, построенный ровно под
// запрос разрешителя: схема заявляла разрешителя, которого в проводке не было.
// Запрет выражен по свойству «сервис зовёт асинхронный запуск», поэтому НОВЫЙ
// сервис краснеет сам, до того как кто-то вспомнит про этот разбор.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: упоминание
// operations.Run или NewReconciler в комментарии или строковом литерале под
// запрет не попадает. Имя пакета берётся из объявления импорта — в дереве есть
// файлы, импортирующие его под своим именем, и текстовый поиск был бы слеп ровно
// на обёртке операций.
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

// operationsImportPath — путь corelib-пакета операций; по нему и опознаётся
// локальное имя в каждом файле.
const operationsImportPath = "/pkg/operations"

// TestEveryServiceWithAsyncMutationsResolvesOrphanedOperations — сам гейт.
//
// Что делать, если сработал: сервис, зовущий operations.Run, обязан построить
// operations.NewReconciler в своём композиционном корне (cmd/…), прогнать
// RecoverAll ДО приёма трафика и повесить периодический Run на супервизор.
// Образцы — services/compute/cmd/compute/recovery.go,
// services/storage/cmd/storage/recovery.go.
//
// «Уберу operations.Run» тоже исход, но тогда мутация становится синхронной, и
// это отдельное контрактное решение, а не способ обойти гейт.
func TestEveryServiceWithAsyncMutationsResolvesOrphanedOperations(t *testing.T) {
	root := repoRoot(t)
	svcRoot := filepath.Join(root, "services")

	entries, err := os.ReadDir(svcRoot)
	if err != nil {
		t.Fatalf("services/ не читается (%v) — область обхода гейта сломана", err)
	}

	var (
		asyncServices   []string
		missing         []string
		scannedServices int
		scannedFiles    int
	)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		svc := e.Name()
		scannedServices++

		runs, files := callsOperationsFunc(t, filepath.Join(svcRoot, svc, "internal"), "Run")
		scannedFiles += files
		if len(runs) == 0 {
			continue
		}
		asyncServices = append(asyncServices, svc)

		builds, files := callsOperationsFunc(t, filepath.Join(svcRoot, svc, "cmd"), "NewReconciler")
		scannedFiles += files
		if len(builds) == 0 {
			missing = append(missing,
				svc+" (async at "+runs[0]+", no operations.NewReconciler under services/"+svc+"/cmd)")
			continue
		}
		// Построить разрешителя мало: функция-строитель обязана быть ДОСТИЖИМА из
		// композиционного корня. Файл recovery.go, который никто не зовёт, гейт
		// проходил бы, а разрешитель не запускался бы ни разу — ровно та форма без
		// содержания, которую этот гейт и ловит. Поймано на себе: правка проводки
		// была потеряна откатом файла, а гейт продолжал зеленеть.
		if dead := unreachableReconcilerBuilders(t, filepath.Join(svcRoot, svc, "cmd")); len(dead) > 0 {
			sort.Strings(dead)
			missing = append(missing, svc+" (operations.NewReconciler построен, но строитель никем не вызван: "+
				strings.Join(dead, ", ")+" — разрешитель не запускается)")
		}
	}

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	if scannedServices == 0 || scannedFiles == 0 {
		t.Fatalf("гейт осмотрел %d сервисов и %d файлов — обход ничего не прочитал, "+
			"молчание ничего не доказывает", scannedServices, scannedFiles)
	}
	sort.Strings(asyncServices)
	t.Logf("осмотрено сервисов: %d, прод-файлов: %d; асинхронные мутации у: %s",
		scannedServices, scannedFiles, strings.Join(asyncServices, ", "))

	if len(asyncServices) == 0 {
		t.Fatal("ни один сервис не зовёт operations.Run — предпосылка распознавания сломана " +
			"(асинхронные мутации есть по контракту), гейт стал бы вечно зелёным")
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("сервисов с асинхронными мутациями и БЕЗ разрешителя осиротевших операций: %d\n  %s\n\n"+
			"Строка операции коммитится до запуска работы, поэтому смерть процесса, исчерпание "+
			"бюджета терминальной записи или переполнение очереди исполнителя оставляют её "+
			"«в процессе» навсегда, и клиент не узнаёт исхода. Построй operations.NewReconciler в "+
			"композиционном корне, прогони RecoverAll до приёма трафика и повесь периодический Run "+
			"на супервизор (образец — services/compute/cmd/compute/recovery.go).",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestOrphanGraceExceedsOperationTimeout — проверка ПРЕДПОСЫЛКИ разрешителя.
//
// Разрешитель считает осиротевшей строку, которая не двигалась дольше своего
// grace-окна. Если окно окажется НЕ больше предела исполнения одной операции, он
// начнёт добивать ЖИВУЮ долгую работу — то есть превратится из подстраховки в
// источник ложных отказов. Инвариант объявлен в godoc corelib; здесь он
// проверяется по значениям, а не по памяти автора.
//
// Композиционный корень вправе окно НЕ задавать — тогда действует умолчание
// corelib, и проверять надо именно его. Требовать литерал в каждом корне значило
// бы запрещать законную форму.
func TestOrphanGraceExceedsOperationTimeout(t *testing.T) {
	root := repoRoot(t)

	worker, err := os.ReadFile(filepath.Join(root, "pkg/operations/worker.go"))
	if err != nil {
		t.Fatalf("pkg/operations/worker.go не читается: %v", err)
	}
	if !strings.Contains(string(worker), "defaultOpTimeout = 4 * time.Minute") {
		t.Fatalf("предел исполнения одной операции сменил значение или форму записи — " +
			"пересчитай grace-окна разрешителей, гейт опирался на 4m")
	}
	rec, err := os.ReadFile(filepath.Join(root, "pkg/operations/reconciler.go"))
	if err != nil {
		t.Fatalf("pkg/operations/reconciler.go не читается: %v", err)
	}
	if !strings.Contains(string(rec), "c.OrphanGrace = 5 * time.Minute") {
		t.Fatalf("умолчание grace-окна в corelib сменило значение или форму записи — " +
			"корни, окно не задающие, полагаются на него; проверь инвариант заново")
	}

	// Корни, задающие окно САМИ, обязаны задать его строго больше предела.
	roots, files := reconcilerGraceLiterals(t, filepath.Join(root, "services"))
	if files == 0 {
		t.Fatal("гейт не прочитал ни одного композиционного корня — обход сломан")
	}
	t.Logf("композиционных корней, задающих grace-окно самостоятельно: %d "+
		"(остальные полагаются на умолчание corelib 5m)", len(roots))
	for rel, minutes := range roots {
		if minutes <= 4 {
			t.Errorf("%s: grace-окно %d минут ≤ предела исполнения операции (4m) — "+
				"разрешитель будет добивать живую долгую работу", rel, minutes)
		}
	}
}

// callsOperationsFunc возвращает координаты вызовов operations.<name>(…) в
// прод-файлах поддерева и число прочитанных файлов. Отсутствующее поддерево — не
// ошибка (у сервиса может не быть cmd/ или internal/), но и не находка: тогда
// вызовов там нет by construction.
func callsOperationsFunc(t *testing.T, dir, name string) (hits []string, scanned int) {
	t.Helper()
	if _, err := os.Stat(dir); err != nil {
		return nil, 0
	}
	root := repoRoot(t)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, "mock") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		rel, _ := filepath.Rel(root, path)

		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, rel, body, 0)
		if perr != nil {
			t.Fatalf("разбор %s: %v", rel, perr)
		}
		local, imported := operationsLocalName(file)
		if !imported {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != name {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != local {
				return true
			}
			hits = append(hits, rel+":"+strconv.Itoa(fset.Position(call.Pos()).Line))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v", dir, err)
	}
	return hits, scanned
}

// reconcilerGraceLiterals собирает из композиционных корней значение OrphanGrace,
// заданное литералом «N * time.Minute». Возвращает map rel-файл → минуты.
func reconcilerGraceLiterals(t *testing.T, servicesDir string) (out map[string]int, scanned int) {
	t.Helper()
	out = map[string]int{}
	root := repoRoot(t)
	err := filepath.Walk(servicesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if !strings.Contains(path, "/cmd/") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		text := string(body)
		// Дешёвый отсев: подстроки довольно, чтобы файл пропустить, но вердикт
		// ниже выносится не по ней, а по наличию и форме `OrphanGrace:`. Хвост
		// чужого имени здесь стоит лишнего чтения, а не находки (замер: хвостов
		// `NewReconciler(` в дереве ноль).
		if !strings.Contains(text, "NewReconciler(") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !strings.Contains(text, "OrphanGrace:") {
			// Окно не задано — действует умолчание corelib, проверенное выше.
			return nil
		}
		minutes, ok := orphanGraceMinutes(text)
		if !ok {
			t.Errorf("%s задаёт OrphanGrace, но значение не записано как «N * time.Minute» — "+
				"гейт не может проверить инвариант «окно > предела исполнения»; запиши окно "+
				"литералом либо не задавай его вовсе (тогда действует умолчание corelib)", rel)
			return nil
		}
		out[rel] = minutes
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v", servicesDir, err)
	}
	return out, scanned
}

// orphanGraceMinutes выковыривает N из «…Grace… = N * time.Minute».
func orphanGraceMinutes(text string) (int, bool) {
	const suffix = "* time.Minute"
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		if !strings.Contains(strings.ToLower(l), "grace") || !strings.Contains(l, suffix) {
			continue
		}
		eq := strings.Index(l, "=")
		if eq < 0 {
			continue
		}
		num := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l[eq+1:]), suffix))
		n, err := strconv.Atoi(num)
		if err != nil || n == 0 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// operationsLocalName — под каким локальным именем этот файл видит corelib-пакет
// операций. Возвращает (имя, импортирован ли вообще). Псевдоним читается из
// объявления импорта, поэтому переименование пакета гейт не ослепляет.
func operationsLocalName(file *ast.File) (string, bool) {
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		if !strings.HasSuffix(strings.Trim(imp.Path.Value, `"`), operationsImportPath) {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				// Пустой/точечный импорт вызовов через селектор не даёт.
				return "", false
			}
			return imp.Name.Name, true
		}
		return "operations", true
	}
	return "", false
}

// unreachableReconcilerBuilders — функции композиционного корня, которые строят
// разрешителя, но которых никто не зовёт.
//
// Проверка намеренно мелкая (одно звено): она ловит «файл есть, вызова нет» —
// состояние, в которое дерево попадает откатом файла или незавершённой правкой
// проводки. Полный анализ достижимости здесь не нужен и был бы хрупок.
func unreachableReconcilerBuilders(t *testing.T, cmdDir string) []string {
	t.Helper()
	if _, err := os.Stat(cmdDir); err != nil {
		return nil
	}

	type builder struct{ file, name string }
	var builders []builder
	files := map[string]string{} // rel → содержимое

	root := repoRoot(t)
	err := filepath.Walk(cmdDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		files[rel] = string(body)

		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, rel, body, 0)
		if perr != nil {
			t.Fatalf("разбор %s: %v", rel, perr)
		}
		local, imported := operationsLocalName(file)
		if !imported {
			return nil
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			found := false
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "NewReconciler" {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == local {
					found = true
				}
				return true
			})
			if found {
				builders = append(builders, builder{file: rel, name: fn.Name.Name})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v", cmdDir, err)
	}

	// Вызванные имена берутся из УЗЛОВ ВЫЗОВА, а не подстрокой. Различие несущее и
	// направление ошибки здесь опаснее обычного: `strings.Contains(src, имя+"(")`
	// находит имя ХВОСТОМ чужого идентификатора (`newReconciler(` содержит
	// `Reconciler(`), и лишнее совпадение объявляет мёртвого строителя ВЫЗВАННЫМ —
	// он не попадает в перечень, гейт зелен, предмет жив. Ложное молчание себя не
	// выдаёт ничем, тогда как ложная находка выдаёт себя сразу.
	called := calledFuncNames(files)

	var dead []string
	for _, b := range builders {
		if !called[b.name] {
			dead = append(dead, b.file+":"+b.name)
		}
	}
	return dead
}

// calledFuncNames — имена, которые в корпусе где-нибудь ВЫЗЫВАЮТСЯ. Собственное
// объявление вызовом не является by construction: узел объявления — не CallExpr.
// Поэтому отдельная поправка «в своём файле имя встречается дважды», которая
// стояла здесь раньше, больше не нужна — она компенсировала разбор текстом.
func calledFuncNames(files map[string]string) map[string]bool {
	called := map[string]bool{}
	fset := token.NewFileSet()
	for rel, src := range files {
		file, err := parser.ParseFile(fset, rel, src, 0)
		if err != nil {
			continue // корпус уже разобран выше; нечитаемый файл сюда не доходит
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				called[fn.Name] = true
			case *ast.SelectorExpr:
				called[fn.Sel.Name] = true
			}
			return true
		})
	}
	return called
}
