// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// nongrpcsurfaceprofile_test.go — гейт против второй поверхности подъёма:
// композиционный корень сервиса не поднимает не-gRPC слушатель сам.
//
// # Предмет
//
// Пока слушатель собирался в корне, его жизненный цикл держался тем, что автор
// написал то же, что сосед. Не написал: замер на `563f92852` дал ВОСЕМЬ файлов и
// ОДИННАДЦАТЬ мест, и разошлись они по всем осям сразу —
//
//   - гашение: двое гасили контекстом БЕЗ СРОКА (остановка ждала последнего
//     запроса неограниченно), двое — сроком дренажа исполнителей операций,
//     четверо — пятью секундами, выписанными по месту;
//   - привязка порта: десять привязывали синхронно, один — `ListenAndServe` в
//     горутине, где занятый адрес попадал в журнал строкой Error, а процесс
//     продолжал работать, объявляя себя исправным при мёртвой поверхности;
//   - сроки: `IdleTimeout` не задавал никто из шести диагностических, а на
//     потоковой поверхности его отсутствие означало, что брошенное соединение
//     висит до конца жизни процесса;
//   - решение об аутентификации: не объявлял НИКТО. Снятая осознанно (скрейп,
//     зеркало публичных ключей) была неотличима от забытой ни в коде, ни в
//     журнале.
//
// Профиль (`pkg/servicecontract.Surface` + `pkg/servicehost.ServeSurface`)
// убирает предмет разногласия, но сам по себе не мешает завести двенадцатое
// место рядом. Это и есть работа гейта.
//
// # На что гейт целится
//
// Единица — СОБСТВЕННЫЙ подъём HTTP-поверхности: литерал `http.Server`, вызовы
// `http.ListenAndServe`/`ListenAndServeTLS`/`http.Serve` и ручная обёртка
// слушателя транспортом (`tls.NewListener`). Последняя входит потому, что она —
// вторая половина того же предмета: решение «этот слушатель под TLS» обязано
// быть объявлением, а не строкой кода в корне.
//
// Чего гейт НЕ ловит и почему это названо: `net.Listen` сам по себе. У владельца
// модели прав им поднимаются два gRPC-слушателя (он остаётся вне контура по
// решению владельца), поэтому запрет на него потребовал бы исключения — то есть
// записи, которая укрывала бы и HTTP-подъём тоже. Достаточно того, что всякий
// потребитель голого слушателя обязан отдать его либо серверу-литералу, либо
// `http.Serve`, а оба они здесь единицы счёта.
//
// # Почему разбор AST, а не текст
//
// Имена `http.Server`, `tls.NewListener` и `ListenAndServe` стоят в комментариях
// этого дерева (в том числе в шапке, которую вы читаете) и в строковых литералах
// проб. Текстовый гейт покраснел бы на собственной документации и позеленел бы
// на вынесенном в строку подъёме. Локальное имя пакета берётся из ОБЪЯВЛЕНИЯ
// ИМПОРТА каждого файла: файл вправе импортировать `net/http` под своим именем.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

const (
	httpPkgPath = "net/http"
	tlsPkgPath  = "crypto/tls"
)

// httpServeFuncs — функции, поднимающие HTTP-поверхность своими руками.
var httpServeFuncs = map[string]string{
	"ListenAndServe": "подъём HTTP-поверхности своими руками: привязка порта происходит ТАМ ЖЕ, " +
		"где обслуживание, поэтому занятый адрес становится строкой журнала, а процесс " +
		"продолжает работать, объявляя себя исправным при мёртвой поверхности",
	"ListenAndServeTLS": "подъём HTTP-поверхности своими руками (с транспортом): тот же дефект " +
		"привязки, плюс решение о транспорте, не объявленное данными",
	"Serve": "обслуживание HTTP на своём слушателе: гашение остаётся обязанностью вызывающего, " +
		"и на этом дереве эта обязанность уже расходилась — от срока дренажа до контекста без срока",
}

// surfaceFinding — одна находка с координатой.
type surfaceFinding struct {
	service string
	where   string // файл:строка
	what    string
}

// surfaceResult — исход обхода вместе с ПЕРЕПИСЬЮ осмотренного.
type surfaceResult struct {
	findings []surfaceFinding
	roots    int
	files    int
	summary  string
}

// surfaceProfileException — корень, поднимающий не-gRPC слушатель сам.
//
// Перечень ПУСТ, и это состояние дерева, а не забывчивость: все одиннадцать мест
// переведены на профиль в одном изменении. Тип и обход самоистечения оставлены
// намеренно — запись, у которой больше нет находок, обязана падать; без этого
// первая же заведённая запись пережила бы свой предмет и укрыла бы следующий
// ручной подъём.
type surfaceProfileException struct {
	// why — почему запись существует и что должно случиться, чтобы она ушла.
	why string
}

var surfaceProfileExceptions = map[string]surfaceProfileException{}

// TestCompositionRootsRaiseNoNonGRPCListenerOfTheirOwn — сам гейт.
//
// Что делать, если сработал, — два исхода, третьего нет:
//
//  1. поверхность поднимается своими руками → перевести её на профиль
//     (`servicecontract.NewSurface` + `servicehost.ServeSurface`), а не заводить
//     исключение;
//  2. это не подъём поверхности, а совпадение формы → уточнить распознавание.
//
// Проверено инъекцией в обе стороны (`nongrpcsurfaceprofile_injection_test.go`).
func TestCompositionRootsRaiseNoNonGRPCListenerOfTheirOwn(t *testing.T) {
	res := auditSurfaceProfile(t, repoRoot(t))
	t.Log(res.summary)

	if res.roots == 0 {
		t.Fatal("не осмотрено ни одного композиционного корня — «ноль находок» здесь означало бы " +
			"«ноль прочитанного», а не исправное дерево")
	}

	byService := map[string][]surfaceFinding{}
	for _, f := range res.findings {
		byService[f.service] = append(byService[f.service], f)
	}

	var unexpected []string
	for svc, fs := range byService {
		if _, excused := surfaceProfileExceptions[svc]; excused {
			continue
		}
		for _, f := range fs {
			unexpected = append(unexpected, fmt.Sprintf("%s: %s — %s", f.service, f.where, f.what))
		}
	}
	// САМОИСТЕЧЕНИЕ: запись, которой больше нечего исключать, — находка.
	var expired []string
	for svc, ex := range surfaceProfileExceptions {
		if len(byService[svc]) > 0 {
			continue
		}
		expired = append(expired, fmt.Sprintf("%s: исключение потеряло предмет — своего подъёма "+
			"не-gRPC поверхности в корне больше нет. Причина, записанная при заведении: %s. "+
			"Снимите запись: оставленная, она укроет следующий ручной подъём", svc, ex.why))
	}

	sort.Strings(unexpected)
	sort.Strings(expired)
	if len(unexpected) > 0 {
		t.Errorf("композиционные корни поднимают не-gRPC слушатели сами (%d):\n  %s",
			len(unexpected), strings.Join(unexpected, "\n  "))
	}
	if len(expired) > 0 {
		t.Errorf("исключения без предмета (%d):\n  %s", len(expired), strings.Join(expired, "\n  "))
	}
}

// auditSurfaceProfile обходит композиционные корни, ВЫВЕДЕННЫЕ из дерева.
//
// Область не выписана списком: восьмой сервис попадает под гейт в момент
// появления. Состав берётся у git (`treecorpus`), а не с диска: вердикт обязан
// быть свойством коммита, а не рабочего каталога с чужими распаковками.
func auditSurfaceProfile(t *testing.T, root string) surfaceResult {
	t.Helper()
	var res surfaceResult

	servicesDir := filepath.Join(root, "services")
	tracked, err := treecorpus.Under(servicesDir)
	if err != nil {
		t.Fatalf("состав дерева под %s не читается: %v", servicesDir, err)
	}

	roots := map[string]struct{}{}
	seen := map[string]struct{}{}
	fset := token.NewFileSet()
	for _, abs := range tracked {
		rel, rerr := filepath.Rel(servicesDir, abs)
		if rerr != nil {
			t.Fatalf("относительный путь для %s: %v", abs, rerr)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 3 || parts[1] != "cmd" {
			continue
		}
		if !strings.HasSuffix(abs, ".go") || strings.HasSuffix(abs, "_test.go") {
			continue
		}
		svc := parts[0]
		roots[svc+"/"+parts[2]] = struct{}{}
		res.files++

		file, perr := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("разбор %s: %v", abs, perr)
		}
		for _, f := range scanFileForOwnSurface(fset, file, svc, relTo(root, abs)) {
			key := f.where + "\x00" + f.what
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			res.findings = append(res.findings, f)
		}
	}
	res.roots = len(roots)
	res.summary = fmt.Sprintf(
		"перепись: композиционных корней %d, не-тестовых файлов в них %d, находок %d, записей %d",
		res.roots, res.files, len(res.findings), len(surfaceProfileExceptions))
	return res
}

// scanFileForOwnSurface читает ИСПОЛНЯЕМУЮ ЧАСТЬ одного файла.
func scanFileForOwnSurface(fset *token.FileSet, file *ast.File, svc, where string) []surfaceFinding {
	local := map[string]string{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		local[name] = path
	}
	pkgOf := func(x ast.Expr) string {
		id, ok := x.(*ast.Ident)
		if !ok {
			return ""
		}
		return local[id.Name]
	}

	var out []surfaceFinding
	add := func(pos token.Pos, what string) {
		out = append(out, surfaceFinding{
			service: svc,
			where:   fmt.Sprintf("%s:%d", where, fset.Position(pos).Line),
			what:    what,
		})
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			// Литерал `http.Server{…}` — сервер, собранный в корне. Тип узнаётся по
			// разрешённому импорту, поэтому одноимённый чужой тип сюда не попадает.
			sel, ok := node.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Server" || pkgOf(sel.X) != httpPkgPath {
				return true
			}
			add(sel.Pos(), "литерал HTTP-сервера в композиционном корне: слушатель собирается "+
				"здесь, значит его сроки, привязка порта, порядок гашения и решение об "+
				"аутентификации остаются собственным делом сервиса. Их место — объявление "+
				"(servicecontract.Surface), а подъём — профиль (servicehost.ServeSurface)")
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch pkgOf(sel.X) {
			case httpPkgPath:
				if why, hit := httpServeFuncs[sel.Sel.Name]; hit {
					add(sel.Pos(), why)
				}
			case tlsPkgPath:
				if sel.Sel.Name == "NewListener" {
					add(sel.Pos(), "ручная обёртка слушателя транспортом: решение «этот слушатель "+
						"под TLS» обязано быть ОБЪЯВЛЕНИЕМ (поле TLS профиля), а не строкой кода "+
						"в корне — иначе оно не попадает ни в самоотчёт при старте, ни в обзор")
				}
			}
		}
		return true
	})
	return out
}
