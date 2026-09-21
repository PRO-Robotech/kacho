// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// mounted_paths_not_hidden_test.go — НИ ОДИН путь, который вешает корень, не
// укрыт на внешнем слушателе.
//
// # Предмет, и почему прежней пробы не хватало
//
// Укрытие отвечает «маршрута нет» на всё, чего внешний слушатель не
// обслуживает, и признак «не обслуживает» — отсутствие записи в таблице
// маршрутов. У собственных ручек края записи заводятся в `rest_route_edge.go`,
// и соседняя проба (`internal/middleware/edge_own_handles_not_hidden_test.go`)
// обходит ИМЕННО ЭТО ОБЪЯВЛЕНИЕ — одну запись.
//
// А корень вешает ШЕСТЬ мест регистрации, и они разворачиваются в восемнадцать
// конкретных путей. Семнадцать из них переживают укрытие не благодаря той
// переписи, а по другому признаку — они в списке публичных путей HTTP, который
// короткозамыкает полосу прав раньше фазы укрытия.
//
// Следствие, которое прежние пробы не поймали бы: НОВАЯ ручка, повешенная на
// `httpMux` без записи в двух других местах, отвечает «не найдено» снаружи с
// первого же запроса — и обе существующие пробы остаются зелёными. Одна
// перечисляет не то множество, вторая перечисляет биндинги контракта, где ручки
// нет вовсе.
//
// # Как взят знаменатель
//
// Из РЕГИСТРАЦИЙ КОРНЯ, а не из литерала: места регистрации считаются разбором
// `main.go`.
//
// # ГРАНИЦА ГЕЙТА — измерена, не предполагается
//
// Разбор ищет вызовы `Handle`/`HandleFunc` У ПРИЁМНИКА С ИМЕНЕМ `httpMux` и
// ТОЛЬКО в файле `main.go`. Мимо проходят:
//
//   - регистрация внутри вспомогательной функции в соседнем файле пакета —
//     `main.go` её не содержит, и знаменатель не вырастет;
//   - регистрация на мультиплексор, названный иначе (передан параметром,
//     получен из поля) — имя приёмника не совпадёт;
//   - регистрация через обёртку, скрывающую имя метода.
//
// Сегодня ни одной такой в дереве нет: все шесть мест стоят в `main.go` на
// `httpMux`. Но граница не в том, чего нет: вынеся монтаж в помощника — самое
// естественное, что сделает автор, когда мест станет много, — его выведут
// из-под гейта МОЛЧА, и новая ручка снова начнёт отвечать «не найдено» снаружи
// при зелёной пробе.
//
// Чем это закрывать, когда понадобится: брать знаменатель не из одного файла, а
// из всех файлов пакета, и опознавать приёмник по ТИПУ (`*http.ServeMux`), а не
// по имени переменной. Заводить это сейчас — точность, которой нечего ловить. Разворот каждого места в конкретные пути объявлен ниже и сверяется
// с разбором В ОБЕ СТОРОНЫ — новое место регистрации краснит гейт и называет
// себя, а объявление, которому в корне нечего соответствовать, краснит его
// тоже. Само объявление разворота берёт пути у тех же источников, что и корень
// (`middleware.LoginLaneRoutes()`, `subscriptionstream.Path`), а не переписывает
// их.
package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// alwaysDenyChecker — модель прав, отвечающая отказом. Предмет проб ниже — не
// «пустили ли», а «сказали ли, что маршрута нет»; отказ модели даёт 403, и он
// укрытием не является.
type alwaysDenyChecker struct{}

func (alwaysDenyChecker) Check(context.Context, middleware.AuthzCheckInput) (middleware.AuthzCheckResult, error) {
	return middleware.AuthzCheckResult{Allowed: false}, nil
}

func nowForWiringProbe() time.Time { return time.Unix(1700000000, 0).UTC() }

func quietLoggerForWiringProbe() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mountSite — одно место регистрации в корне: текст аргумента-пути, как он там
// написан, и конкретные пути, в которые он разворачивается.
type mountSite struct {
	// expr — аргумент, как он написан в корне. Ключ сверки с разбором.
	expr string
	// paths — во что он разворачивается. Пусто у диспетчера: его предмет — всё
	// остальное, и укрытию он как раз и подлежит.
	paths []string
	// dispatcher — это сквозная регистрация REST-диспетчера, не ручка края.
	dispatcher bool
}

// declaredMountSites — разворот мест регистрации. Пути берутся у ТЕХ ЖЕ
// источников, которые читает корень.
func declaredMountSites() []mountSite {
	var lane []string
	for _, rt := range middleware.LoginLaneRoutes() {
		lane = append(lane, rt.Path)
	}
	return []mountSite{
		{expr: `"/healthz"`, paths: []string{"/healthz"}},
		{expr: `"/readyz"`, paths: []string{"/readyz"}},
		{expr: `rt.Path`, paths: lane},
		{expr: `"/oauth/logout"`, paths: []string{"/oauth/logout"}},
		{expr: `subscriptionstream.Path`, paths: []string{subscriptionstream.Path}},
		{expr: `"/"`, dispatcher: true},
	}
}

// rootMountExprs — места регистрации, СЧИТАННЫЕ ИЗ КОРНЯ разбором.
func rootMountExprs(t *testing.T) []string {
	t.Helper()
	root := gatewayTreeRootForWiring(t)
	rel := "cmd/api-gateway/main.go"
	src, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("чтение %s: %v", rel, err)
	}
	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, rel, src, 0)
	if parseErr != nil {
		t.Fatalf("разбор %s: %v", rel, parseErr)
	}

	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc" {
			return true
		}
		x, isIdent := sel.X.(*ast.Ident)
		if !isIdent || x.Name != "httpMux" {
			return true
		}
		arg := call.Args[0]
		out = append(out, string(src[fset.Position(arg.Pos()).Offset:fset.Position(arg.End()).Offset]))
		return true
	})
	sort.Strings(out)
	return out
}

// TestMountedPathSetMatchesTheRoot — знаменатель сверен с корнем в обе стороны.
func TestMountedPathSetMatchesTheRoot(t *testing.T) {
	fromRoot := rootMountExprs(t)
	if len(fromRoot) == 0 {
		t.Fatal("в корне не найдено ни одной регистрации на httpMux — разбор сломан, " +
			"и молчание гейта пусто")
	}

	declared := map[string]bool{}
	for _, s := range declaredMountSites() {
		declared[s.expr] = true
	}
	seen := map[string]bool{}
	for _, e := range fromRoot {
		seen[e] = true
	}

	var undeclared, stale []string
	for _, e := range fromRoot {
		if !declared[e] {
			undeclared = append(undeclared, "  "+e)
		}
	}
	for e := range declared {
		if !seen[e] {
			stale = append(stale, "  "+e)
		}
	}
	sort.Strings(stale)

	t.Logf("перепись: мест регистрации в корне %d · объявлено разворотов %d",
		len(fromRoot), len(declared))

	if len(undeclared) > 0 {
		t.Errorf("в корне %d мест(а) регистрации, которых нет в развороте ниже:\n%s\n"+
			"Новая ручка края не переживает укрытие сама собой: если её пути нет ни в списке "+
			"публичных путей HTTP, ни в объявлении маршрутов края, снаружи она отвечает "+
			"«не найдено» с первого запроса. Дописать разворот И решить, чем путь держится.",
			len(undeclared), strings.Join(undeclared, "\n"))
	}
	if len(stale) > 0 {
		t.Errorf("%d объявленных разворота(ов) не соответствуют ни одной регистрации в корне:\n%s\n"+
			"Запись пережила свой предмет.", len(stale), strings.Join(stale, "\n"))
	}
}

// TestEveryMountedPathSurvivesTheHiding — и это главная половина: каждый
// конкретный путь, который корень вешает, снаружи НЕ отвечает «не найдено».
func TestEveryMountedPathSurvivesTheHiding(t *testing.T) {
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	if err != nil {
		t.Fatalf("каталог прав: %v", err)
	}
	rr := middleware.NewRestRouter()
	mw, err := middleware.NewAuthzMiddleware(middleware.AuthzMiddlewareConfig{
		Enabled:         true,
		Catalog:         catalog,
		Subjects:        middleware.NewSubjectExtractor(true),
		Context:         middleware.NewContextExtractor(nowForWiringProbe, true),
		Resources:       middleware.NewResourceExtractor(rr.PathTemplates()),
		Checker:         alwaysDenyChecker{},
		RestRouter:      rr,
		Logger:          quietLoggerForWiringProbe(),
		PublicAllowlist: middleware.DefaultPublicAllowlist(),
	})
	if err != nil {
		t.Fatalf("полоса прав: %v", err)
	}
	// Следующее звено отвечает узнаваемо: если запрос до него дошёл, укрытия не
	// было.
	chain := mw.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var mounted []string
	for _, s := range declaredMountSites() {
		if s.dispatcher {
			continue
		}
		mounted = append(mounted, s.paths...)
	}
	if len(mounted) == 0 {
		t.Fatal("конкретных путей ноль — пустой обход вердиктом не является")
	}

	var hidden []string
	for _, p := range mounted {
		rec := httptest.NewRecorder()
		// Внешнее происхождение — умолчание fail-closed: маркера нет.
		chain.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code == http.StatusNotFound {
			hidden = append(hidden, "  "+p+" -> 404 "+strings.TrimSpace(rec.Body.String()))
		}
	}
	t.Logf("перепись: конкретных путей, которые вешает корень %d · укрыто %d", len(mounted), len(hidden))

	if len(hidden) > 0 {
		t.Errorf("%d из %d путей, которые вешает корень, снаружи отвечают «не найдено»:\n%s\n"+
			"Укрытие накрыло ручку края — снаружи она исчезла целиком.",
			len(hidden), len(mounted), strings.Join(hidden, "\n"))
	}
}
