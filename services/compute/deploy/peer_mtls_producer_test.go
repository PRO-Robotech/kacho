// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// peer_mtls_producer_test.go — у каждой группы per-edge mTLS, которую читает
// конфиг и требует production-страж, обязан быть ПРОИЗВОДИТЕЛЬ в чарте.
//
// Почему это отдельная проверка, а не следствие ревью диффа. Группа переменных
// `KACHO_COMPUTE_<X>_MTLS_*` живёт в трёх местах сразу: её объявляет конфиг
// (`internal/config/config.go`), её требует страж посадки
// (`cmd/compute/main.go`, `insecureEdgesInProductionStrict`) и её обязан
// выставить чарт (`deploy/templates/deployment.yaml`). Первые два места
// компилируются вместе, поэтому расходятся с шумом; третье — YAML, и его
// отсутствие не роняет ничего. Тогда получается ребро, у которого:
//
//   - в plain-production страж per-edge mTLS не проверяет (это задокументированное
//     послабление), значит сервис стартует и выглядит исправным;
//   - клиентские креды остаются выключенными по умолчанию, значит дозвон идёт
//     открытым текстом на порт, который требует проверенный клиентский
//     сертификат, — то есть ребро не работает ВООБЩЕ, а не «работает слабее»;
//   - в production-strict тот же страж отказывает в старте, называя переменную,
//     которую профилю нечем выставить: у требования нет производителя.
//
// Именно так и было с ребром compute→vpc :9091 (NIC-привязка): группа
// `VPC_NIC_MTLS` читалась конфигом с 2026-07-15 и НИ РАЗУ не выставлялась чартом
// (`git log -S 'VPC_NIC_MTLS' -- services/compute/deploy/` → 0 коммитов).
// Наблюдаемое следствие — ребро отвечало codes.Unavailable, а профиль разработки
// обходил это пустым адресом, то есть заглушкой вместо вызова.
//
// Проверка ДЕКЛАРАТИВНА: читает исходник конфига, исходник стража и ТЕКСТ
// шаблона, а не отрендеренный манифест. Поэтому она не зависит ни от helm, ни от
// значений профиля и не может «пропуститься»: производитель обязан быть в
// шаблоне независимо от того, включает ли конкретный профиль это ребро.
//
// Свободный проброс `range .Values.env` производителем НЕ считается — он
// умеет выставить что угодно и потому не является утверждением о чарте
// (та же граница, что у gateway/deploy/token_shape_test.go: first-class knob,
// а не пассажирская мапа).
package deploy_test

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
)

const (
	computeConfigSrc   = "../internal/config/config.go"
	computeMainSrc     = "../cmd/compute/main.go"
	computeDeployTmpl  = "templates/deployment.yaml"
	computeEnvPrefix   = "KACHO_COMPUTE_"
	computeGuardFnName = "insecureEdgesInProductionStrict"
)

// chartProducedEnv — множество имён переменных окружения, которые шаблон
// выставляет ЯВНО (литералом `- name: KACHO_COMPUTE_…`). Проброс `.Values.env`
// сюда не попадает by construction: он рендерит `- name: {{ $k }}`.
func chartProducedEnv(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(computeDeployTmpl))
	if err != nil {
		t.Fatalf("прочитать шаблон %s: %v", computeDeployTmpl, err)
	}
	re := regexp.MustCompile(`(?m)^\s*-\s*name:\s*` + computeEnvPrefix + `([A-Z0-9_]+)\s*$`)
	out := make(map[string]bool)
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		out[computeEnvPrefix+m[1]] = true
	}
	return out
}

// configTLSGroups — envconfig-префиксы полей конфига, чей тип есть транспортные
// креды (`grpcclient.TLSClient` — клиентское ребро, `grpcsrv.TLSServer` —
// листенер). Разбор идёт по AST, а не по тексту: тег в комментарии или в строке
// рядом не должен считаться объявлением.
func configTLSGroups(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.FromSlash(computeConfigSrc), nil, 0)
	if err != nil {
		t.Fatalf("разобрать %s: %v", computeConfigSrc, err)
	}
	groups := make(map[string]string) // envconfig-префикс → имя поля
	ast.Inspect(file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, f := range st.Fields.List {
			sel, ok := f.Type.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if sel.Sel == nil || (sel.Sel.Name != "TLSClient" && sel.Sel.Name != "TLSServer") {
				continue
			}
			if f.Tag == nil || len(f.Names) == 0 {
				continue
			}
			tag, uerr := strconv.Unquote(f.Tag.Value)
			if uerr != nil {
				continue
			}
			prefix := envconfigTag(tag)
			if prefix == "" {
				t.Fatalf("%s: поле %s типа %s без тега envconfig — группу нечем выставить",
					computeConfigSrc, f.Names[0].Name, sel.Sel.Name)
			}
			groups[prefix] = f.Names[0].Name
		}
		return true
	})
	return groups
}

// envconfigTag достаёт значение ключа envconfig из struct-тега.
func envconfigTag(tag string) string {
	const key = `envconfig:"`
	i := strings.Index(tag, key)
	if i < 0 {
		return ""
	}
	rest := tag[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// guardDemandedEnv — имена переменных, которые страж посадки перечисляет как
// «ребро живое, а mTLS на нём выключен». Это ровно тот текст, который оператор
// увидит в отказе стартовать, поэтому чарт обязан уметь его удовлетворить.
func guardDemandedEnv(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.FromSlash(computeMainSrc), nil, 0)
	if err != nil {
		t.Fatalf("разобрать %s: %v", computeMainSrc, err)
	}
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name != nil && fd.Name.Name == computeGuardFnName {
			fn = fd
			break
		}
	}
	if fn == nil {
		// Проверка предпосылки: стража нет — значит эта проверка утверждает
		// о несуществующем и обязана упасть, а не молча позеленеть.
		t.Fatalf("%s: функция %s не найдена — предпосылка проверки исчезла "+
			"(страж переименован или снят); почини проверку вместе с ним",
			computeMainSrc, computeGuardFnName)
	}
	lit := regexp.MustCompile(`^[A-Z0-9_]+_ENABLE$`)
	seen := make(map[string]bool)
	var out []string
	ast.Inspect(fn, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return true
		}
		v, uerr := strconv.Unquote(bl.Value)
		if uerr != nil || !lit.MatchString(v) || seen[v] {
			return true
		}
		seen[v] = true
		out = append(out, v)
		return true
	})
	sort.Strings(out)
	return out
}

// TestChartProducesEveryPeerMTLSGroupTheConfigReads — перепись в ОБЕ стороны
// между объявлением конфига и текстом шаблона.
//
// Пропуск (конфиг читает, чарт не выставляет) — ребро всегда дозванивается
// открытым текстом. Сирота (чарт выставляет, конфиг не читает) — мёртвая ручка,
// которая выглядит настройкой безопасности и ничего не делает; этот класс здесь
// уже случался, поэтому обе стороны утверждаются, а не одна.
func TestChartProducesEveryPeerMTLSGroupTheConfigReads(t *testing.T) {
	groups := configTLSGroups(t)
	produced := chartProducedEnv(t)

	if len(groups) == 0 {
		t.Fatalf("%s: не найдено ни одного поля типа TLSClient/TLSServer — "+
			"предпосылка проверки исчезла (переехал конфиг?), а не «всё чисто»", computeConfigSrc)
	}

	var missing, orphan []string
	for prefix, field := range groups {
		if !produced[computeEnvPrefix+prefix+"_ENABLE"] {
			missing = append(missing, computeEnvPrefix+prefix+"_ENABLE (поле "+field+")")
		}
	}
	producedGroups := 0
	for name := range produced {
		g := strings.TrimSuffix(strings.TrimPrefix(name, computeEnvPrefix), "_ENABLE")
		if g == name || !strings.HasSuffix(name, "_MTLS_ENABLE") {
			continue
		}
		producedGroups++
		if _, ok := groups[g]; !ok {
			orphan = append(orphan, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(orphan)

	// Объём осмотренного печатается всегда: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного».
	t.Logf("осмотрено: %d групп транспортных кред в %s, %d явных env-имён в %s (из них %d групп mTLS)",
		len(groups), computeConfigSrc, len(produced), computeDeployTmpl, producedGroups)

	for _, m := range missing {
		t.Errorf("нет производителя: конфиг читает %s, но шаблон %s его не выставляет — "+
			"ребро дозванивается открытым текстом на порт, требующий клиентский сертификат, "+
			"и production-strict откажется стартовать с требованием, которое профилю нечем закрыть",
			m, computeDeployTmpl)
	}
	for _, o := range orphan {
		t.Errorf("мёртвая ручка: шаблон %s выставляет %s, но конфиг %s такой группы не читает — "+
			"настройка выглядит включённой и ни на что не влияет",
			computeDeployTmpl, o, computeConfigSrc)
	}
}

// TestChartCanSatisfyEveryEnvTheProductionGuardDemands — у каждой переменной,
// которую страж посадки называет в отказе стартовать, обязан быть производитель
// в чарте. Иначе отказ неустраним: оператору сказано включить то, чего профиль
// выставить не может.
func TestChartCanSatisfyEveryEnvTheProductionGuardDemands(t *testing.T) {
	demanded := guardDemandedEnv(t)
	produced := chartProducedEnv(t)

	if len(demanded) == 0 {
		t.Fatalf("%s: страж %s не назвал ни одной переменной — предпосылка проверки исчезла",
			computeMainSrc, computeGuardFnName)
	}

	t.Logf("осмотрено: %d переменных в отказе стража %s, %d явных env-имён в %s",
		len(demanded), computeGuardFnName, len(produced), computeDeployTmpl)

	for _, d := range demanded {
		if !produced[computeEnvPrefix+d] {
			t.Errorf("требование без производителя: страж %s требует %s%s, "+
				"а шаблон %s его не выставляет",
				computeGuardFnName, computeEnvPrefix, d, computeDeployTmpl)
		}
	}
}
