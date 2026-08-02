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
