// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// named_surfaces_test.go — ни одна проба этого пакета не вправе рассуждать о
// методе, которого нет.
//
// ЗАЧЕМ. Утверждение вида «этот путь НЕ разрешён», сделанное про путь, за
// которым не стоит ни одного RPC, не может упасть никогда: чтобы оно упало,
// кто-то должен вписать в список несуществующий метод. Такая проба занимает
// слот, отчитывается зелёным и создаёт уверенность, которой нет — она хуже
// отсутствующей. В этом пакете таких литералов было 24 из 188: восемь имён
// сервисов в форме «<Xxx>InternalService», которой не пользуется НИ ОДИН
// сервис дерева, семь и один из пакета, удалённого целиком, три из пакета,
// который переехал, и пять снятых с контракта глаголов.
//
// ПОЧЕМУ ИХ МОЖНО СНЯТЬ, НЕ ОСЛАБИВ НИЧЕГО. Отрицание годится только в паре с
// положительным, поэтому снимаются они не в пустоту: их предмет целиком
// накрыт вычисляемым гейтом рядом (parity_test.go). «Список \ все-RPC = пусто»
// краснеет на ЛЮБОМ пути, за которым нет RPC, — включая те двадцать четыре и
// те, которых никто не предвидел; «список ∩ Internal* = пусто» проверяет
// запрет #6 на всех 79 реальных Internal-методах вместо выборки из одиннадцати
// имён, восемь из которых были выдуманы.
//
// ЧТО ДЕЛАЕТ ЭТОТ ФАЙЛ. Держит класс закрытым: разбирает тестовые файлы пакета
// как СИНТАКСИС (go/ast, только строковые литералы — не текст, поэтому имя в
// комментарии его не обманет) и требует, чтобы каждый названный путь либо
// существовал в дескрипторах, либо был записан ниже с причиной.
//
// САМОИСТЕЧЕНИЕ. Запись ниже живёт, пока у неё есть предмет: если названный
// путь СТАНЕТ настоящим RPC, запись — находка, потому что окружающее её
// утверждение молча меняет смысл (из «предикат на синтетическом входе» в
// «проба про живую поверхность»). Предикат снятия внешний — дескрипторы, — и
// поэтому изменением самого списка его не сделать тождественно истинным.
package allowlist_test

import (
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

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
)

// namedButNonexistent — пути, которые пробы пакета называют НАМЕРЕННО, зная,
// что RPC за ними нет. Значение — причина; она обязана объяснять, почему
// синтетический вход здесь уместен, а не «так исторически».
var namedButNonexistent = map[string]string{
	"/kacho.cloud.vpc.v1.NetworkInternalService/Exists": "синтетический вход для второй конвенции " +
		"HasInternalSuffix (суффикс <Xxx>InternalService): предикат её поддерживает и обязан " +
		"поддерживать дальше, но ни один сервис дерева так не назван, поэтому живого примера нет",
}

// methodPathLiteral — форма полного gRPC-пути. Совпадает только с целым
// литералом: префикс "/kacho.cloud." сам по себе (константа соседнего файла)
// под неё не подходит.
var methodPathLiteral = regexp.MustCompile(`^/kacho\.cloud\.[A-Za-z0-9_.]+/[A-Za-z0-9_]+$`)

// Полы переписи: «ноль находок» обязано быть отличимо от «ноль прочитанного».
const (
	minTestFilesParsed = 4
	minLiteralsFound   = 100
)

type namedPath struct {
	file string
	line int
	path string
	// isRegistryDecl — литерал стоит ВНУТРИ объявления namedButNonexistent,
	// то есть это сама запись реестра, а не её употребление пробой.
	//
	// Различать обязательно: ключ реестра — тоже строковый литерал в тестовом
	// файле пакета, поэтому без этого признака каждая запись «употребляет» сама
	// себя и проверка на неиспользуемую запись не может упасть НИКОГДА. Ровно
	// тот класс, который этот файл и заведён ловить; поймано инъекцией.
	isRegistryDecl bool
}

// registryDeclName — имя объявления, литералы внутри которого считаются
// записями реестра, а не употреблениями.
const registryDeclName = "namedButNonexistent"

// collectNamedPaths разбирает тестовые файлы пакета и возвращает КАЖДОЕ
// вхождение пути с координатой. Читается синтаксис, а не текст: строковый
// литерал в комментарии литералом не является и сюда не попадёт.
func collectNamedPaths(t *testing.T) ([]namedPath, int) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("чтение директории пакета: %v — гейт не знает, что он осмотрел", err)
	}
	fset := token.NewFileSet()
	var out []namedPath
	files := 0
	registryFound := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("разбор %s: %v", name, perr)
		}
		files++

		// Границы объявления реестра в этом файле (если оно здесь).
		declFrom, declTo := token.NoPos, token.NoPos
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, id := range vs.Names {
				if id.Name == registryDeclName {
					declFrom, declTo = vs.Pos(), vs.End()
					registryFound = true
				}
			}
			return true
		})

		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, uerr := strconv.Unquote(lit.Value)
			if uerr != nil || !methodPathLiteral.MatchString(val) {
				return true
			}
			out = append(out, namedPath{
				file:           filepath.Base(name),
				line:           fset.Position(lit.Pos()).Line,
				path:           val,
				isRegistryDecl: declFrom.IsValid() && lit.Pos() >= declFrom && lit.Pos() < declTo,
			})
			return true
		})
	}
	if !registryFound {
		t.Fatalf("объявление %s не найдено ни в одном разобранном файле — гейт не отличает "+
			"запись реестра от её употребления, и проверка неиспользуемых записей беспредметна",
			registryDeclName)
	}
	return out, files
}

// descriptorMethods — все методы маршрутизируемой поверхности, публичные и
// Internal вместе. Источник тот же, что у parity_test.go.
func descriptorMethods(t *testing.T) map[string]struct{} {
	t.Helper()
	s := readDescriptorSurface(t)
	all := make(map[string]struct{}, len(s.public)+len(s.internal))
	for k := range s.public {
		all[k] = struct{}{}
	}
	for k := range s.internal {
		all[k] = struct{}{}
	}
	return all
}

// TestNamedSurfaces_EveryNamedPathHasASubject — ни один литерал не называет
// путь, за которым нет RPC, кроме зарегистрированных синтетических входов.
func TestNamedSurfaces_EveryNamedPathHasASubject(t *testing.T) {
	named, files := collectNamedPaths(t)
	real := descriptorMethods(t)

	t.Logf("перепись: разобрано %d тестовых файлов, найдено %d литералов-путей, "+
		"в дескрипторах %d методов, зарегистрированных синтетических входов %d",
		files, len(named), len(real), len(namedButNonexistent))

	if files < minTestFilesParsed {
		t.Fatalf("разобрано %d тестовых файлов (< %d) — гейт смотрит не в ту директорию, "+
			"и его зелёный вердикт беспредметен", files, minTestFilesParsed)
	}
	if len(named) < minLiteralsFound {
		t.Fatalf("найдено %d литералов-путей (< %d) — разбор ничего не прочитал",
			len(named), minLiteralsFound)
	}
	if len(real) == 0 {
		t.Fatal("в дескрипторах ноль методов — сверять не с чем")
	}

	var orphans []string
	for _, n := range named {
		if _, ok := real[n.path]; ok {
			continue
		}
		if _, ok := namedButNonexistent[n.path]; ok {
			continue
		}
		orphans = append(orphans, n.file+":"+strconv.Itoa(n.line)+" "+n.path)
	}
	sort.Strings(orphans)
	if len(orphans) > 0 {
		t.Errorf("%d литералов называют путь, за которым нет ни одного RPC. Утверждение вокруг "+
			"такого литерала не может упасть — оно занимает слот и отчитывается зелёным. "+
			"Либо путь настоящий и его надо исправить, либо утверждение уже накрыто вычисляемым "+
			"гейтом (parity_test.go) и его надо снять, либо вход синтетический и его надо "+
			"записать в namedButNonexistent с причиной:\n  %s",
			len(orphans), strings.Join(orphans, "\n  "))
	}
}

// TestNamedSurfaces_RegistryEntriesStillHaveNoSubject — самоистечение реестра.
//
// Запись, чей путь стал настоящим RPC, — находка: утверждение вокруг неё
// молча сменило смысл, а исключение продолжает молчать про уже живую
// поверхность.
func TestNamedSurfaces_RegistryEntriesStillHaveNoSubject(t *testing.T) {
	real := descriptorMethods(t)
	if len(real) == 0 {
		t.Fatal("в дескрипторах ноль методов — истечение проверять нечем")
	}
	var revived []string
	for p := range namedButNonexistent {
		if _, ok := real[p]; ok {
			revived = append(revived, p)
		}
	}
	sort.Strings(revived)
	if len(revived) > 0 {
		t.Errorf("%d записей namedButNonexistent называют СУЩЕСТВУЮЩИЙ теперь RPC — "+
			"исключение пережило свой предмет; сними запись и пересмотри утверждение, "+
			"которое её использовало:\n  %s", len(revived), strings.Join(revived, "\n  "))
	}
}

// TestNamedSurfaces_RegistryIsUsed — обратная сторона: запись реестра, которую
// НИКТО не называет, тоже беспредметна. Реестр не свалка разрешений.
//
// Употреблением считается литерал ВНЕ объявления реестра: сам ключ — тоже
// строковый литерал в тестовом файле, поэтому без этого различения запись
// «употребляет» сама себя и проверка не может упасть никогда.
func TestNamedSurfaces_RegistryIsUsed(t *testing.T) {
	named, _ := collectNamedPaths(t)
	used := map[string]struct{}{}
	for _, n := range named {
		if n.isRegistryDecl {
			continue
		}
		used[n.path] = struct{}{}
	}
	var unused []string
	for p := range namedButNonexistent {
		if _, ok := used[p]; !ok {
			unused = append(unused, p)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Errorf("%d записей namedButNonexistent не названы ни одной пробой — разрешение, "+
			"которому больше нечего разрешать:\n  %s", len(unused), strings.Join(unused, "\n  "))
	}
}

// TestHasInternalSuffix_BothNamingConventions — предикат, на котором держится
// запрет #6, опознаёт ОБЕ принятые в дереве конвенции именования, и не считает
// Internal публичный сервис.
//
// Отрицание здесь стоит в паре с положительным намеренно: проба, состоящая из
// одних «не Internal», зеленеет сильнее всего, когда предикат сломан и не
// опознаёт вообще ничего.
func TestHasInternalSuffix_BothNamingConventions(t *testing.T) {
	// Префиксная конвенция — живой пример из дерева.
	if !allowlist.HasInternalSuffix("/kaname.cloud.iam.v1.InternalIAMService/Check") {
		t.Error("префиксная конвенция Internal<Xxx>Service перестала опознаваться — " +
			"запрет #6 не держится ни на одном сервисе дерева")
	}
	// Суффиксная конвенция — синтетический вход (см. namedButNonexistent).
	if !allowlist.HasInternalSuffix("/kacho.cloud.vpc.v1.NetworkInternalService/Exists") {
		t.Error("суффиксная конвенция <Xxx>InternalService перестала опознаваться — " +
			"сервис, названный так, попал бы на внешний листенер")
	}
	// Парный положительный: публичный сервис Internal НЕ считается, иначе
	// предикат, отвечающий «да» всем, тоже прошёл бы обе проверки выше.
	if allowlist.HasInternalSuffix("/kacho.cloud.vpc.v1.NetworkService/Get") {
		t.Error("публичный NetworkService/Get опознан как Internal — предикат отвечает " +
			"утвердительно всем, и обе проверки выше ничего не значат")
	}
}
