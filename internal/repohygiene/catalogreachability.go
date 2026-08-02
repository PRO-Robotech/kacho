// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogreachability.go — анализатор «каталог прав объявляет полосу авторизации
// для метода, которого не обслуживает ни один листенер».
//
// # Что он ищет
//
// Запись каталога — это решение о доступе: по ней край выбирает отношение,
// объект и то, спрашивать ли модель вообще. Запись, чей метод не резолвится в
// обслуживаемый, выглядит гейтом и не гейтит ничего. Это соседний класс к
// «принято-и-проигнорировано» (`api-conventions.md`), только этажом выше — на
// уровне контракта прав, а не поля запроса.
//
// Симптома у такой записи нет по построению. Она не консультируется, потому что
// вызова не происходит: grpc-go отвечает `Unimplemented` раньше, чем край дойдёт
// до каталога. Значит ни прогон, ни обзор диффа её не покажут — «полоса объявлена
// для несуществующего метода» и «полоса объявлена верно» дают одинаково зелёный
// стенд.
//
// Отложенное следствие — то, ради чего гейт и нужен. Запись переживает своё
// отсутствие: когда метод однажды реализуют и смонтируют, он приедет с уже
// готовой полосой, выбранной когда-то под другой замысел и с тех пор никем не
// перечитанной. Полоса `<exempt>` в такой записи означает, что метод выйдет в
// эксплуатацию вообще без вопроса к модели прав.
//
// # Почему предмет ВЫЧИСЛЯЕТСЯ, а не перечисляется
//
// Обе стороны читаются оттуда, где живут:
//
//   - множество методов КОНТРАКТА — из сгенерированных стабов, разбором
//     `grpc.ServiceDesc` (`ServiceName` + `MethodName`/`StreamName`). Это ровно та
//     таблица, по которой grpc-go диспатчит вызов, поэтому «метод есть» здесь
//     значит то же, что значит в рантайме;
//   - множество СМОНТИРОВАННОГО — разбором AST композиционных корней, через ту же
//     `mountedServices`, которой пользуется анализатор монтирования. Одна
//     реализация на оба гейта: две разошлись бы, и тогда один из них начал бы
//     утверждать о композиционном корне неправду.
//
// Рукописного перечня «что обслуживается» здесь нет ни одного — перечень был бы
// утверждением о дереве, а не измерением его.
//
// # Исключение — ОДНО решение на два гейта
//
// Сервис, намеренно не поднимаемый по gRPC, уже объявлен в `mountAllow`
// (`grpcmountparity_test.go`). Записи каталога, называющие такой сервис,
// неизбежно инертны — это следствие того же решения, а не отдельное. Поэтому
// список сюда ПЕРЕДАЁТСЯ, а не копируется: вторая копия была бы поверхностью
// правки, за которой никто не смотрит.
//
// Исключение живёт, пока у него есть предмет. Запись, которой больше нечего
// исключать — потому что сервис смонтировали и его строки стали резолвиться, либо
// потому что строк с этим именем в каталоге не осталось, — сама является
// находкой.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CatalogReachabilityOptions — вход анализатора.
type CatalogReachabilityOptions struct {
	// Root — корень репозитория.
	Root string
	// CatalogPath — путь (относительно Root) к той копии каталога, которую
	// ИСПОЛНЯЕТ край. Побайтовое совпадение копий между собой — предмет другого
	// гейта (catalogparity_test.go), здесь читается исполняемая.
	CatalogPath string
	// APIRoot — путь (относительно Root) к сгенерированным стабам.
	APIRoot string
	// ModulePath — import-префикс модуля.
	ModulePath string
	// Roots — каталоги, внутри которых ищутся композиционные корни.
	Roots []string
	// Allow — FQN СЕРВИСОВ, намеренно не поднимаемых по gRPC. Передаётся из
	// mountAllow; собственного списка у этого гейта нет.
	Allow []string
}

// CatalogReachabilityCensus — то, что анализатор прочитал. Ноль находок обязано
// быть отличимо от нуля прочитанного.
type CatalogReachabilityCensus struct {
	CatalogRows     int
	StubFiles       int
	DeclaredSvcs    int
	DeclaredMethods int
	CmdPackages     int
	CmdFiles        int
	MountedSvcs     int
	OwningBinaries  int
	RowsResolved    int
	RowsExcused     int
}

// CatalogReachabilityFinding — одна находка.
type CatalogReachabilityFinding struct {
	Kind   string // "unknown-service" | "unknown-method" | "unmounted" | "stale-allow"
	FQN    string
	Reason string
}

func (f CatalogReachabilityFinding) String() string { return f.Kind + " " + f.FQN + ": " + f.Reason }

// catalogReachRow — одна строка каталога. Читается только то, что нужно этому гейту.
type catalogReachRow struct {
	FQN        string `json:"fqn"`
	Permission string `json:"permission"`
}

// AuditCatalogReachability сводит записи каталога с методами контракта и с
// композиционными корнями.
func AuditCatalogReachability(opts CatalogReachabilityOptions, out io.Writer) ([]CatalogReachabilityFinding, CatalogReachabilityCensus, error) {
	var c CatalogReachabilityCensus

	rows, err := readCatalogRows(filepath.Join(opts.Root, opts.CatalogPath))
	if err != nil {
		return nil, c, err
	}
	c.CatalogRows = len(rows)
	if c.CatalogRows == 0 {
		return nil, c, fmt.Errorf("каталог %q пуст — предмета нет, и любой вердикт ниже беспредметен",
			opts.CatalogPath)
	}

	declared, err := declaredMethods(filepath.Join(opts.Root, opts.APIRoot), &c)
	if err != nil {
		return nil, c, err
	}
	if c.StubFiles == 0 {
		return nil, c, fmt.Errorf("не прочитано ни одного файла стабов в %q — «все методы контракта неизвестны» "+
			"получено даром", filepath.Join(opts.Root, opts.APIRoot))
	}

	// Смонтированное берётся ТОЙ ЖЕ реализацией, что и в анализаторе монтирования:
	// две разошлись бы, и один из гейтов начал бы утверждать о композиционном корне
	// неправду.
	var mc MountCensus
	mountOpts := MountOptions{Root: opts.Root, APIRoot: opts.APIRoot, ModulePath: opts.ModulePath, Roots: opts.Roots}
	_, dirToProto, err := declaredServices(filepath.Join(opts.Root, opts.APIRoot), &mc)
	if err != nil {
		return nil, c, err
	}
	mounted, err := mountedServices(mountOpts, dirToProto, &mc)
	if err != nil {
		return nil, c, err
	}
	c.CmdPackages, c.CmdFiles = mc.CmdPackages, mc.CmdFiles
	if c.CmdFiles == 0 {
		return nil, c, fmt.Errorf("не прочитано ни одного файла композиционных корней в %v — "+
			"«ничего не смонтировано» получено даром", opts.Roots)
	}
	mountedSet := map[string]struct{}{}
	for _, set := range mounted {
		if len(set) > 0 {
			c.OwningBinaries++
		}
		for fqn := range set {
			mountedSet[fqn] = struct{}{}
		}
	}
	c.MountedSvcs = len(mountedSet)

	allow := map[string]struct{}{}
	for _, a := range opts.Allow {
		allow[a] = struct{}{}
	}
	excusedSvc := map[string]struct{}{}

	var findings []CatalogReachabilityFinding
	for _, row := range rows {
		svc, method, ok := splitFQN(row.FQN)
		if !ok {
			findings = append(findings, CatalogReachabilityFinding{
				Kind: "unknown-method", FQN: row.FQN,
				Reason: "строка каталога не имеет формы `<сервис>/<метод>` и не может быть сопоставлена ни с чем",
			})
			continue
		}
		methods, svcKnown := declared[svc]
		if !svcKnown {
			findings = append(findings, CatalogReachabilityFinding{
				Kind: "unknown-service", FQN: row.FQN,
				Reason: "такого сервиса нет в контракте — запись пережила его снятие и объявляет полосу " +
					"для имени, которое ничему не принадлежит",
			})
			continue
		}
		if _, methodKnown := methods[method]; !methodKnown {
			findings = append(findings, CatalogReachabilityFinding{
				Kind: "unknown-method", FQN: row.FQN,
				Reason: "сервис в контракте есть, а такого метода у него нет — запись пережила снятие метода",
			})
			continue
		}
		if _, isMounted := mountedSet[svc]; isMounted {
			c.RowsResolved++
			continue
		}
		if _, excused := allow[svc]; excused {
			excusedSvc[svc] = struct{}{}
			c.RowsExcused++
			continue
		}
		findings = append(findings, CatalogReachabilityFinding{
			Kind: "unmounted", FQN: row.FQN,
			Reason: "метод объявлен контрактом, но его сервис не смонтирован ни в одном композиционном корне — " +
				"полоса авторизации объявлена для вызова, который не дойдёт до края, а когда метод однажды " +
				"смонтируют, он приедет с этой полосой уже готовой (permission " + strconv.Quote(row.Permission) + ")",
		})
	}

	// Исключение живёт, пока у него есть предмет — предмет ИМЕННО этого гейта:
	// инертные строки каталога. Их не осталось ⇒ исключать здесь нечего.
	for _, a := range opts.Allow {
		if _, used := excusedSvc[a]; used {
			continue
		}
		// Список — общий с анализатором монтирования, поэтому формулировка обязана
		// различать две причины: «сервис подняли» — решение изменилось, и правка
		// идёт в общий список; «строк не осталось» — изменился каталог, и это
		// вопрос к переписи инертных строк, а не повод трогать чужое решение.
		reason := "исключать здесь больше нечего: в каталоге нет ни одной инертной строки этого сервиса. " +
			"Это вопрос к переписи инертных строк, а НЕ повод снимать запись из общего списка — " +
			"её вторая сторона (монтирование) могла остаться в силе"
		if _, isMounted := mountedSet[a]; isMounted {
			reason = "исключать больше нечего: сервис СМОНТИРОВАН, его строки каталога резолвятся — " +
				"решение «не монтируем» отменено, и запись общего списка пережила его"
		}
		findings = append(findings, CatalogReachabilityFinding{Kind: "stale-allow", FQN: a, Reason: reason})
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].FQN < findings[j].FQN
	})

	if out != nil {
		_, _ = fmt.Fprintf(out, "перепись: строк каталога %d (резолвится %d, исключено %d); "+
			"стабов %d файлов, сервисов %d, методов контракта %d; "+
			"композиционных корней %d (файлов %d), из них монтирующих %d, смонтировано сервисов %d; находок %d\n",
			c.CatalogRows, c.RowsResolved, c.RowsExcused,
			c.StubFiles, c.DeclaredSvcs, c.DeclaredMethods,
			c.CmdPackages, c.CmdFiles, c.OwningBinaries, c.MountedSvcs, len(findings))
	}
	return findings, c, nil
}

// splitFQN разбирает `pkg.Service/Method`.
func splitFQN(fqn string) (svc, method string, ok bool) {
	i := strings.IndexByte(fqn, '/')
	if i <= 0 || i == len(fqn)-1 {
		return "", "", false
	}
	return fqn[:i], fqn[i+1:], true
}

func readCatalogRows(path string) ([]catalogReachRow, error) {
	b, err := os.ReadFile(path) // #nosec G304 — путь из конфигурации гейта внутри репозитория
	if err != nil {
		return nil, fmt.Errorf("каталог не прочитан: %w", err)
	}
	var rows []catalogReachRow
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("каталог %q не разобран: %w", path, err)
	}
	return rows, nil
}

// declaredMethods читает сгенерированные стабы и возвращает `сервис → множество
// методов` ровно так, как их видит grpc-go: из `grpc.ServiceDesc`, где
// `ServiceName` — имя для диспатча, а `Methods`/`Streams` — то, что по нему
// вызывается.
//
// Разбор идёт по AST, а не по тексту: `MethodName` встречается в файле и вне
// дескриптора, и текстовый поиск приписал бы методы одного сервиса другому в
// файле, где дескрипторов несколько.
func declaredMethods(apiRoot string, c *CatalogReachabilityCensus) (map[string]map[string]struct{}, error) {
	out := map[string]map[string]struct{}{}
	fset := token.NewFileSet()
	err := filepath.Walk(apiRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, "_grpc.pb.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("стаб %q не разобран: %w", path, perr)
		}
		c.StubFiles++
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok || !isGRPCServiceDesc(cl.Type) {
				return true
			}
			name, methods := serviceDescContent(cl)
			if name == "" {
				return true
			}
			if _, seen := out[name]; !seen {
				out[name] = map[string]struct{}{}
			}
			for _, m := range methods {
				out[name][m] = struct{}{}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	c.DeclaredSvcs = len(out)
	for _, ms := range out {
		c.DeclaredMethods += len(ms)
	}
	return out, nil
}

// isGRPCServiceDesc распознаёт тип литерала `grpc.ServiceDesc`.
func isGRPCServiceDesc(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "ServiceDesc" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "grpc"
}

// serviceDescContent достаёт из литерала дескриптора имя сервиса и имена всех его
// методов (унарных и потоковых).
func serviceDescContent(cl *ast.CompositeLit) (string, []string) {
	var name string
	var methods []string
	for _, el := range cl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "ServiceName":
			name = stringLit(kv.Value)
		case "Methods", "Streams":
			inner, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, e := range inner.Elts {
				entry, ok := e.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, f := range entry.Elts {
					fkv, ok := f.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					fk, ok := fkv.Key.(*ast.Ident)
					if !ok || (fk.Name != "MethodName" && fk.Name != "StreamName") {
						continue
					}
					if m := stringLit(fkv.Value); m != "" {
						methods = append(methods, m)
					}
				}
			}
		}
	}
	return name, methods
}

func stringLit(e ast.Expr) string {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return ""
	}
	return s
}
