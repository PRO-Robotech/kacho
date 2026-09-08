// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

// Перепись исходящих проводов composition root — механическая, не глазами.
//
// Строгая production-стража рёбер (insecureEdgesInProductionStrict) объявляет,
// что перечисляет КАЖДОЕ реально дозваниваемое транспортное ребро. Пока этот
// перечень поддерживается руками, он расходится с кодом бесшумно: ребро
// добавляют в dialPeers, про стражу забывают, и провод остаётся вне гейта — при
// том что заголовок стражи по-прежнему обещает полноту.
//
// Поэтому полнота проверяется не чтением, а сверкой: перечень стражи сверяется с
// местами, где composition root РЕАЛЬНО резолвит TLS-credentials. Источник
// правды — синтаксис main.go, а не комментарий рядом с ним.
//
// Ребро попадает в перепись, если main.go:
//   - зовёт grpcclient.TLSClientTransportCreds(cfg.<Поле>) напрямую, либо
//   - зовёт аксессор Config, который внутри резолвит TLS*Creds(c.<Поле>)
//     (cfg.PublicServerCreds / cfg.IAMRegisterClientCreds и т.п.).
//
// Обе формы живут в дереве одновременно, поэтому гейт обязан понимать обе:
// иначе следующее ребро уедет от него по второй форме.

// tlsResolverFuncs — имена функций corelib, резолвящих TLS-креды из value-struct
// ребра. Появление четвёртой — событие, которое обязано быть замечено (см.
// TestEdgeCensus_ResolverVocabularyIsExhaustive ниже).
var tlsResolverFuncs = map[string]struct{}{
	"TLSClientTransportCreds": {},
	"TLSClientCreds":          {},
	"TLSServerCreds":          {},
	// Появился вместе с носителем (первая фаза XC-7): резолвит креды СЛУШАТЕЛЯ и
	// отдаёт сами `credentials.TransportCredentials`, чтобы вызывающий спрашивал
	// у транспорта, а не у своей ручки, — настроенное-но-выродившееся ребро от
	// проверенного по ручке не отличить.
	//
	// Гейт покраснел ровно за этим: перепись, не знающая резолвера, не видит
	// РЁБЕР, провязанных через него, и её «полно» относится к меньшему, чем
	// звучит.
	"TLSServerTransportCreds": {},
}

// moduleRoot поднимается от каталога теста до go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

func parseGoFile(t *testing.T, path string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return f
}

// tlsResolverArgField — если выражение является вызовом резолвера TLS-кредов с
// единственным аргументом-селектором (`pkg.TLSClientCreds(c.GeoMTLS)`),
// возвращает имя поля конфига.
func tlsResolverArgField(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if _, isResolver := tlsResolverFuncs[sel.Sel.Name]; !isResolver {
		return "", false
	}
	if len(call.Args) != 1 {
		return "", false
	}
	arg, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	return arg.Sel.Name, true
}

// configCredsAccessors — аксессоры Config, резолвящие TLS-креды: имя метода →
// имя поля конфига. Читается из исходника config.go, а не перечисляется руками.
func configCredsAccessors(t *testing.T, root string) map[string]string {
	t.Helper()
	f := parseGoFile(t, filepath.Join(root, "services", "compute", "internal", "config", "config.go"))

	out := map[string]string{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			if field, isResolver := tlsResolverArgField(call); isResolver {
				out[fn.Name.Name] = field
			}
			return true
		})
	}
	return out
}

// dialedEdgeFields — перепись: поля конфига, чьи TLS-креды composition root
// реально резолвит (напрямую либо через аксессор Config).
func dialedEdgeFields(t *testing.T, root string) []string {
	t.Helper()
	accessors := configCredsAccessors(t, root)
	if len(accessors) == 0 {
		t.Fatal("no TLS-creds accessor found in config.go — the census would resolve nothing, " +
			"so it would assert nothing; check that the resolver vocabulary still matches corelib")
	}

	// Композиционный корень — КАТАЛОГ, а не один файл. С переездом контура на
	// общий носитель транспорт обоих слушателей уехал из `main.go` в
	// `describe.go`, и перепись, читавшая только первый, объявила бы эти два ребра
	// несуществующими — то есть потребовала бы снять с них требование mTLS. Состав
	// выводится обходом каталога: следующий файл корня попадает под перепись в
	// момент появления, а не тогда, когда кто-нибудь вспомнит дополнить список.
	dir := filepath.Join(root, "services", "compute", "cmd", "compute")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read composition root dir %s: %v", dir, err)
	}
	seen := map[string]struct{}{}
	read := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		read++
		f := parseGoFile(t, filepath.Join(dir, name))
		ast.Inspect(f, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			if field, isResolver := tlsResolverArgField(call); isResolver {
				seen[field] = struct{}{}
				return true
			}
			if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel {
				if field, isAccessor := accessors[sel.Sel.Name]; isAccessor {
					seen[field] = struct{}{}
				}
			}
			return true
		})
	}
	if read == 0 {
		t.Fatalf("no non-test source read under %s — the census read nothing, so it asserted nothing", dir)
	}
	t.Logf("census read %d non-test file(s) of the composition root", read)

	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// envKnob — env-имя ручки `Enable` данного ребра (то, что стража обязана назвать
// оператору). Берётся из тега структуры, а не из литерала в тесте: разъехавшись,
// они дали бы гейт, зелёный при неверном тексте отказа.
func envKnob(t *testing.T, field string) string {
	t.Helper()
	sf, ok := reflect.TypeOf(config.Config{}).FieldByName(field)
	if !ok {
		t.Fatalf("config.Config has no field %q — the census resolved a name that is not a config field", field)
	}
	tag := sf.Tag.Get("envconfig")
	if tag == "" {
		t.Fatalf("config field %s carries no envconfig tag — its env knob is unnameable", field)
	}
	return tag + "_ENABLE"
}

// everyEdgeLive — production-strict-конфиг, в котором АКТИВНЫ все условные рёбра
// (адреса заданы, дренер включён, аварийные выключатели сняты) и все per-edge
// mTLS включены. Единственная конфигурация, которую строгая стража обязана
// пропускать; отключение любого одного ребра обязано её ронять.
func everyEdgeLive(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Config{
		AuthMode:                  "production-strict",
		DBSSLMode:                 "verify-full",
		FGARegisterDrainerEnabled: true,
		AuthZTrustedForwarderSANs: []string{"spiffe://kacho.cloud/ns/kacho-system/sa/kacho-api-gateway"},
		ListFilterEnabled:         true,
		// адреса всех условных рёбер заданы ⇒ каждое реально дозванивается
		IAMGRPCAddr:             "kaname.kacho.svc:9090",
		GeoGRPCAddr:             "kacho-geo.kacho.svc:9090",
		VPCGRPCAddr:             "kacho-vpc.kacho.svc:9090",
		VPCInternalGRPCAddr:     "kacho-vpc-internal.kacho.svc:9091",
		StorageInternalGRPCAddr: "kacho-storage-internal.kacho.svc:9091",
		AuthZIAMGRPCAddr:        "kaname-internal.kacho.svc:9091",
		// Домен величин объявлен АДРЕСОМ: ребро дилится, значит страж обязан
		// требовать от него удостоверение. Объявление «не развёрнут» вывело бы
		// ребро из наблюдения, и отключение его mTLS перестало бы что-либо
		// значить — фикстура молча перестала бы быть представительной.
		QuotaAuthority: "kaname-internal.kacho.svc:9091",
	}
	v := reflect.ValueOf(&cfg).Elem()
	for _, field := range dialedEdgeFields(t, moduleRoot(t)) {
		enable := v.FieldByName(field).FieldByName("Enable")
		if !enable.IsValid() || enable.Kind() != reflect.Bool {
			t.Fatalf("config field %s has no bool Enable — it is not an edge TLS value-struct", field)
		}
		enable.SetBool(true)
	}
	return cfg
}

// TestEdgeCensus_GuardCoversEveryDialedEdge — гейт полноты: КАЖДОЕ ребро из
// переписи composition root обязано ронять строгую стражу, будучи отключённым.
// Проверяется ПОВЕДЕНИЕ стражи (отказ старта + названная оператору ручка), а не
// присутствие имени в её тексте.
func TestEdgeCensus_GuardCoversEveryDialedEdge(t *testing.T) {
	edges := dialedEdgeFields(t, moduleRoot(t))
	if len(edges) == 0 {
		t.Fatal("edge census is empty — the walk found nothing, so it would have asserted nothing")
	}
	t.Logf("composition root resolves TLS creds for %d edges: %v", len(edges), edges)

	if _, err := validateAuthMode(everyEdgeLive(t), discardLogger()); err != nil {
		t.Fatalf("baseline with every edge secured must pass the strict gate; got: %v", err)
	}

	for _, field := range edges {
		t.Run(field, func(t *testing.T) {
			cfg := everyEdgeLive(t)
			reflect.ValueOf(&cfg).Elem().FieldByName(field).FieldByName("Enable").SetBool(false)

			_, err := validateAuthMode(cfg, discardLogger())
			if err == nil {
				t.Fatalf("edge %s is dialed by the composition root but the strict gate accepts it plaintext — "+
					"the guard's stated completeness is false", field)
			}
			if knob := envKnob(t, field); !strings.Contains(err.Error(), knob) {
				t.Errorf("refusal must name %s so the operator can act on it; got: %v", knob, err)
			}
		})
	}
}

// TestEdgeCensus_GuardDemandsNothingUndialed — обратное направление: стража не
// вправе требовать mTLS с провода, которого никто не поднимает. Без этой
// половины гейт ловил бы форму (имя упомянуто), а не существо, и первое же
// снятое ребро оставило бы в страже требование-призрак.
func TestEdgeCensus_GuardDemandsNothingUndialed(t *testing.T) {
	root := moduleRoot(t)
	census := map[string]struct{}{}
	for _, f := range dialedEdgeFields(t, root) {
		census[f] = struct{}{}
	}

	guarded := guardNamedEdgeFields(t, root)
	if len(guarded) == 0 {
		t.Fatal("the strict guard reads no edge Enable flag at all — it cannot be guarding anything")
	}
	for _, field := range guarded {
		if _, ok := census[field]; !ok {
			t.Errorf("the strict guard demands mTLS on %s, but the composition root never resolves creds for it — "+
				"a phantom requirement outlives the wire it guarded", field)
		}
	}
}

// guardNamedEdgeFields — поля конфига, чей `Enable` читает тело строгой стражи.
// Читается ИСПОЛНЯЕМАЯ часть (AST), не текст: имя ребра в комментарии рядом со
// снятой проверкой не должно считаться охраной.
func guardNamedEdgeFields(t *testing.T, root string) []string {
	t.Helper()
	f := parseGoFile(t, filepath.Join(root, "services", "compute", "cmd", "compute", "main.go"))

	var body *ast.BlockStmt
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "insecureEdgesInProductionStrict" {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatal("insecureEdgesInProductionStrict not found in main.go — the guard this gate protects is gone; " +
			"if it was renamed, re-point the gate rather than leaving it vacuous")
	}

	cfgType := reflect.TypeOf(config.Config{})
	seen := map[string]struct{}{}
	ast.Inspect(body, func(n ast.Node) bool {
		outer, ok := n.(*ast.SelectorExpr)
		if !ok || outer.Sel.Name != "Enable" {
			return true
		}
		inner, ok := outer.X.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if _, isField := cfgType.FieldByName(inner.Sel.Name); isField {
			seen[inner.Sel.Name] = struct{}{}
		}
		return true
	})

	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// TestEdgeCensus_ResolverVocabularyIsExhaustive — проверка СВОЕЙ предпосылки.
//
// Перепись опознаёт ребро по имени функции-резолвера. Значит она верна ровно
// пока словарь совпадает с тем, что corelib предлагает для резолва TLS-кред.
// Заведи corelib четвёртый резолвер, оставь словарь прежним — и перепись тихо
// перестанет видеть провода, поднятые через него: «ноль пропущенных рёбер» будет
// означать «ноль прочитанных». Поэтому словарь сверяется не с местами вызова
// (там его незнание как раз и не видно), а с ЭКСПОРТИРУЕМОЙ поверхностью
// транспортных пакетов corelib.
func TestEdgeCensus_ResolverVocabularyIsExhaustive(t *testing.T) {
	root := moduleRoot(t)

	exported := map[string]struct{}{}
	for _, rel := range []string{
		filepath.Join("pkg", "grpcclient", "tls.go"),
		filepath.Join("pkg", "grpcsrv", "tls.go"),
	} {
		f := parseGoFile(t, filepath.Join(root, rel))
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "TLS") && strings.HasSuffix(fn.Name.Name, "Creds") {
				exported[fn.Name.Name] = struct{}{}
			}
		}
	}
	if len(exported) == 0 {
		t.Fatal("no TLS*Creds resolver found in the corelib transport packages — the census keys on a " +
			"vocabulary that no longer exists; re-point this gate rather than leaving it vacuous")
	}

	var unknown []string
	for name := range exported {
		if _, known := tlsResolverFuncs[name]; !known {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		t.Fatalf("corelib exports transport creds resolver(s) the census does not know: %v — add them to "+
			"tlsResolverFuncs, otherwise every edge wired through them is invisible to the completeness gate", unknown)
	}
}
