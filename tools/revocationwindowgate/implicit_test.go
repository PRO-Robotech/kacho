// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package revocationwindowgate_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/tools/revocationwindowgate"
)

// Инъекция настоящим входом. Оба источника ниже — форма, которой composition
// root в этом дереве действительно пользуется (см. services/*/internal/check/
// factory.go), а не выдумка под предикат.

// srcImplicit — дефект: интерсептор строится, кеш не назван. Окно отзыва при
// этом есть (конструктор раньше заводил кеш сам), но не объявлено ничем.
const srcImplicit = `package check

import "github.com/PRO-Robotech/kacho/pkg/authz"

func NewInterceptor(opts Options) *authz.Interceptor {
	return authz.NewInterceptor(authz.InterceptorOptions{
		ServiceName: opts.ServiceName,
		Map:         PermissionMap(),
		Client:      opts.Client,
	})
}
`

// srcExplicit — законный близнец: та же форма, кеш назван. Гейт обязан молчать,
// иначе он ловит форму вызова, а не существо, и первый же ложный срабат его
// отключит.
const srcExplicit = `package check

import "github.com/PRO-Robotech/kacho/pkg/authz"

func NewInterceptor(opts Options) *authz.Interceptor {
	return authz.NewInterceptor(authz.InterceptorOptions{
		ServiceName: opts.ServiceName,
		Map:         PermissionMap(),
		Client:      opts.Client,
		Cache:       authz.NewCache(0),
	})
}
`

// srcIndirect — вторая законная форма, живущая в дереве (nlb): опции
// собираются в переменную и уже её передают в конструктор. Кеш назван, значит
// гейт молчит; но литерал обязан быть ОСМОТРЕН, иначе предикат «каждый литерал
// называет кеш» обходится переносом литерала в переменную.
const srcIndirect = `package check

import "github.com/PRO-Robotech/kacho/pkg/authz"

func NewInterceptor(opts Options) *authz.Interceptor {
	cache := authz.NewCache(opts.CacheTTL)
	base := authz.InterceptorOptions{
		Map:   PermissionMap(),
		Cache: cache,
	}
	return authz.NewInterceptor(base)
}
`

// srcIndirectImplicit — та же непрямая форма, но БЕЗ кеша. Ровно тот обход,
// ради которого предикат берёт литерал, а не аргумент вызова.
const srcIndirectImplicit = `package check

import "github.com/PRO-Robotech/kacho/pkg/authz"

func NewInterceptor(opts Options) *authz.Interceptor {
	base := authz.InterceptorOptions{
		Map:    PermissionMap(),
		Client: opts.Client,
	}
	return authz.NewInterceptor(base)
}
`

// srcCommentOnly — контроль на разбор вместо текста. Godoc конструктора в
// pkg/authz содержит РОВНО ту форму, которую ищет гейт; текстовый поиск
// объявил бы её дефектом. Это не гипотетическая опасность: перепись
// регулярным выражением по дереву именно на этом примере и ошиблась.
const srcCommentOnly = `package authz

// Использование (composition root):
//
//	authzIntr := authz.NewInterceptor(authz.InterceptorOptions{
//	    ServiceName: "kacho-vpc",
//	    Map:         PermissionMap(),
//	})
type Interceptor struct{}
`

func TestScanImplicitSites_FindsTheUnnamedCache(t *testing.T) {
	got, err := revocationwindowgate.ScanImplicitSites("seventh", "services/seventh/internal/check/factory.go", srcImplicit)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(got.Sites) != 1 {
		t.Fatalf("дефект внесён настоящим входом, а гейт нашёл %d площадок; ожидалась 1", len(got.Sites))
	}
	if got.Sites[0].Service != "seventh" {
		t.Errorf("сервис в находке = %q, ожидался %q", got.Sites[0].Service, "seventh")
	}
	if got.Sites[0].Line != 6 {
		t.Errorf("координата находки = строка %d, ожидалась 6 (иначе находка не показывает, ГДЕ чинить)", got.Sites[0].Line)
	}
	if got.LiteralsSeen != 1 {
		t.Errorf("осмотрено литералов = %d, ожидался 1", got.LiteralsSeen)
	}
}

func TestScanImplicitSites_SilentOnTheLegitimateTwin(t *testing.T) {
	for name, src := range map[string]string{
		"прямой литерал":   srcExplicit,
		"через переменную": srcIndirect,
	} {
		got, err := revocationwindowgate.ScanImplicitSites("nlb", "services/nlb/internal/check/factory.go", src)
		if err != nil {
			t.Fatalf("%s: разбор: %v", name, err)
		}
		if len(got.Sites) != 0 {
			t.Errorf("%s: кеш назван, а гейт нашёл %d площадок — он ловит форму, а не существо: %+v",
				name, len(got.Sites), got.Sites)
		}
		if got.LiteralsSeen != 1 {
			t.Errorf("%s: литерал не осмотрен (LiteralsSeen=%d) — молчание значит «не читал», а не «чисто»",
				name, got.LiteralsSeen)
		}
	}
}

func TestScanImplicitSites_SeesThroughTheVariable(t *testing.T) {
	got, err := revocationwindowgate.ScanImplicitSites("seventh", "services/seventh/internal/check/factory.go", srcIndirectImplicit)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(got.Sites) != 1 {
		t.Fatalf("литерал вынесен в переменную — гейт нашёл %d площадок; предикат обязан брать литерал, "+
			"а не аргумент вызова, иначе обходится переносом в переменную", len(got.Sites))
	}
}

func TestScanImplicitSites_IgnoresTheCommentedExample(t *testing.T) {
	got, err := revocationwindowgate.ScanImplicitSites("authz", "pkg/authz/interceptor.go", srcCommentOnly)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(got.Sites) != 0 {
		t.Errorf("гейт принял пример из godoc за площадку: %+v.\n"+
			"Он обязан читать исполняемую часть, а не текст.", got.Sites)
	}
	if got.LiteralsSeen != 0 {
		t.Errorf("в комментарии литералов нет, а осмотрено %d", got.LiteralsSeen)
	}
}

// TestScanImplicitSites_ReportsAParseFailure — испорченный вход обязан быть
// ошибкой, а не пустым результатом: «ноль находок» и «ноль прочитанного» не
// должны печататься одинаково.
func TestScanImplicitSites_ReportsAParseFailure(t *testing.T) {
	_, err := revocationwindowgate.ScanImplicitSites("x", "x.go", "package check\nfunc (")
	if err == nil {
		t.Fatalf("испорченный исходник разобрался без ошибки — тогда нечитаемый файл неотличим от чистого")
	}
	if !strings.Contains(err.Error(), "x.go") {
		t.Errorf("ошибка разбора не называет файл: %v", err)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Площадка БЕЗ литерала вовсе
// ────────────────────────────────────────────────────────────────────────────

// srcNoLiteralAtAll — третья форма сборки опций: нулевое значение и присвоение
// полей по одному. Литерала нет, поэтому предикат «каждый литерал называет
// кеш» смотреть здесь не на что — он читает файл, засчитывает его в
// «осмотрено» и объявляет чистым.
//
// Инъекцией на настоящем дереве проверено: седьмой сервис в этой форме прошёл
// ВСЕ четыре проверки гейта зелёным, и число прочитанных файлов при этом
// выросло на единицу.
const srcNoLiteralAtAll = `package check

import "github.com/PRO-Robotech/kacho/pkg/authz"

func NewInterceptor(opts Options) *authz.Interceptor {
	var o authz.InterceptorOptions
	o.Map = PermissionMap()
	o.Client = opts.Client
	return authz.NewInterceptor(o)
}
`

// srcNoLiteralCacheAssigned — законный близнец той же формы: кеш назван
// присвоением. Гейт обязан молчать, иначе он запрещает форму, а не сужает
// неявный путь.
const srcNoLiteralCacheAssigned = `package check

import "github.com/PRO-Robotech/kacho/pkg/authz"

func NewInterceptor(opts Options) *authz.Interceptor {
	var o authz.InterceptorOptions
	o.Map = PermissionMap()
	o.Cache = authz.NewCache(opts.CacheTTL)
	return authz.NewInterceptor(o)
}
`

// srcOpaqueArgument — опции приходят из чужой функции. Доказать, что кеш
// назван, здесь нечем, поэтому исход — находка: гейт про доступ обязан быть
// fail-closed, а «не смог посмотреть» это не «чисто».
const srcOpaqueArgument = `package check

import "github.com/PRO-Robotech/kacho/pkg/authz"

func NewInterceptor(opts Options) *authz.Interceptor {
	return authz.NewInterceptor(buildOptions(opts))
}
`

// TestScanInterceptorCalls_FindsTheCallWithNoLiteralBehindIt — отрицательная
// сторона на форме, которой в дереве не было, но которая возможна.
func TestScanInterceptorCalls_FindsTheCallWithNoLiteralBehindIt(t *testing.T) {
	for name, src := range map[string]string{
		"переменная без кеша":   srcNoLiteralAtAll,
		"непрозрачный аргумент": srcOpaqueArgument,
	} {
		got, err := revocationwindowgate.ScanInterceptorCalls("seventh", "services/seventh/internal/check/factory.go", src)
		if err != nil {
			t.Fatalf("%s: разбор: %v", name, err)
		}
		if got.CallsSeen != 1 {
			t.Errorf("%s: вызовов конструктора осмотрено %d, ожидался 1 — молчание значит «не читал», а не «чисто»",
				name, got.CallsSeen)
		}
		if len(got.Sites) != 1 {
			t.Fatalf("%s: кеш не назван ничем, а гейт нашёл %d площадок; ожидалась 1", name, len(got.Sites))
		}
		if got.Sites[0].Service != "seventh" {
			t.Errorf("%s: сервис в находке = %q", name, got.Sites[0].Service)
		}
		if got.Sites[0].Line == 0 {
			t.Errorf("%s: находка без координаты не показывает, ГДЕ чинить", name)
		}
	}
}

// TestScanInterceptorCalls_SilentOnTheLegitimateTwins — положительная сторона.
// Все три формы, которыми дерево пользуется законно, обязаны молчать: иначе
// проверка запрещает форму вместо того, чтобы сужать неявный путь.
func TestScanInterceptorCalls_SilentOnTheLegitimateTwins(t *testing.T) {
	for name, src := range map[string]string{
		"прямой литерал с кешем":       srcExplicit,
		"литерал в переменной с кешем": srcIndirect,
		"присвоение поля кеша":         srcNoLiteralCacheAssigned,
	} {
		got, err := revocationwindowgate.ScanInterceptorCalls("nlb", "services/nlb/internal/check/factory.go", src)
		if err != nil {
			t.Fatalf("%s: разбор: %v", name, err)
		}
		if got.CallsSeen != 1 {
			t.Errorf("%s: вызовов осмотрено %d, ожидался 1", name, got.CallsSeen)
		}
		if len(got.Sites) != 0 {
			t.Errorf("%s: кеш назван, а гейт нашёл %d площадок — он ловит форму, а не существо: %+v",
				name, len(got.Sites), got.Sites)
		}
	}
}

// TestScanInterceptorCalls_LeavesTheLiteralPredicateItsSubject — литерал прямо
// в вызове БЕЗ кеша остаётся предметом переписи литералов и здесь НЕ
// удваивается: одна находка на одну площадку, иначе цена дефекта в отчёте
// растёт вдвое и вердикт перестаёт быть числом.
func TestScanInterceptorCalls_LeavesTheLiteralPredicateItsSubject(t *testing.T) {
	for name, src := range map[string]string{
		"литерал в вызове":     srcImplicit,
		"литерал в переменной": srcIndirectImplicit,
	} {
		got, err := revocationwindowgate.ScanInterceptorCalls("seventh", "services/seventh/internal/check/factory.go", src)
		if err != nil {
			t.Fatalf("%s: разбор: %v", name, err)
		}
		if got.CallsSeen != 1 {
			t.Errorf("%s: вызовов осмотрено %d, ожидался 1", name, got.CallsSeen)
		}
		if len(got.Sites) != 0 {
			t.Errorf("%s: площадка с литералом принадлежит переписи литералов; здесь она даёт %d находок — двойной счёт",
				name, len(got.Sites))
		}
	}
}

// TestScanInterceptorCalls_IgnoresTheCommentedExample — тот же контроль на
// разбор вместо текста: godoc конструктора содержит ровно эту форму вызова.
func TestScanInterceptorCalls_IgnoresTheCommentedExample(t *testing.T) {
	got, err := revocationwindowgate.ScanInterceptorCalls("authz", "pkg/authz/interceptor.go", srcCommentOnly)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if got.CallsSeen != 0 {
		t.Errorf("в комментарии вызовов нет, а осмотрено %d", got.CallsSeen)
	}
	if len(got.Sites) != 0 {
		t.Errorf("гейт принял пример из godoc за площадку: %+v", got.Sites)
	}
}

// TestScanInterceptorCalls_ReportsAParseFailure — нечитаемый файл обязан быть
// ошибкой, а не пустым результатом.
func TestScanInterceptorCalls_ReportsAParseFailure(t *testing.T) {
	_, err := revocationwindowgate.ScanInterceptorCalls("x", "x.go", "package check\nfunc (")
	if err == nil {
		t.Fatalf("испорченный исходник разобрался без ошибки — нечитаемый файл неотличим от чистого")
	}
	if !strings.Contains(err.Error(), "x.go") {
		t.Errorf("ошибка разбора не называет файл: %v", err)
	}
}
