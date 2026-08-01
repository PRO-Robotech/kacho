// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ingress_backend_test.go — публичный вход не вправе смотреть на слушателя,
// который отдаёт Internal*.
//
// ЧТО ОХРАНЯЕТСЯ. Под api-gateway держит три слушателя, и Service объявляет их
// тремя портами: `cmux` (внутрикластерный, НЕ помеченный как внешний, поэтому
// REST-диспетчер отдаёт по нему Internal*-пути), `internal-rest` (единственный
// admin-REST, для admin-UI и port-forward) и `tls` (помеченный внешним —
// listenerorigin.ExternalListener, поэтому Internal*-пути на нём 404). Какой из
// трёх портов назван в backend'е входа — и есть решение о том, публикуется ли
// admin-поверхность наружу.
//
// ПОЧЕМУ ГЕЙТ ПОНАДОБИЛСЯ. Требование было записано ровно там, где его никто не
// исполняет: комментарием в service.yaml («the ingress MUST NOT target this
// port») и комментарием в umbrella-шаблоне. Ни одна проба не читала backend
// входа вообще — ни в этом чарте, ни в umbrella. Комментарий, за которым нет
// проверки, держится ровно до первой правки, которая его не прочтёт.
//
// ПОЧЕМУ ДЕКЛАРАТИВНО, А НЕ ЧЕРЕЗ helm template. Соседние render-гейты
// ПРОПУСКАЮТСЯ, когда helm не на PATH (вне CI). Гейт, который может не
// выполниться, для этого свойства не годится: «не выполнилось» неотличимо от
// «прошло». Здесь читается ОБЪЯВЛЕНИЕ — шаблон и файлы значений, — поэтому
// пропуск невозможен по построению и зависимостей нет.
//
// ЗАПРЕТ ЧИТАЕТСЯ ВМЕСТЕ С ПОЛОЖИТЕЛЬНЫМ. Проверять «backend не называет
// internal-rest» в одиночку бессмысленно: то же самое сказал бы вход, ведущий в
// никуда, и вход, назвавший порт с опечаткой. Поэтому рядом утверждается, что
// имя, которое мы запрещаем, ДЕЙСТВИТЕЛЬНО объявлено портом Service (иначе
// запрет беспредметен), и что каждое найденное имя backend'а — тоже настоящий
// порт Service.
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	// internalAdminPortName — порт Service, отдающий admin-REST. Внешний вход не
	// вправе его называть ни при каких значениях.
	internalAdminPortName = "internal-rest"
	// externalMarkedPortName — единственный слушатель, помеченный внешним;
	// только на нём REST-диспетчер 404-ит Internal*-пути.
	externalMarkedPortName = "tls"
	// unmarkedPortName — внутрикластерный слушатель: Internal*-пути на нём
	// обслуживаются, поэтому наружу он выставляться не должен.
	unmarkedPortName = "cmux"

	subchartIngress  = "templates/ingress.yaml"
	subchartService  = "templates/service.yaml"
	umbrellaIngress  = "../../deploy/helm/umbrella/templates/api-gateway-ingress.yaml"
	umbrellaValuesGl = "../../deploy/helm/umbrella/values*.yaml"
)

// templateAction — действие шаблона внутри строки.
var templateAction = regexp.MustCompile(`\{\{[^}]*\}\}`)

// readDeclaredYAML читает ШАБЛОН как объявление: строки, состоящие целиком из
// управляющего действия (условие, присваивание, end, fail), выбрасываются, а
// действия-подстановки заменяются меткой. Остаётся настоящая структура YAML —
// та, которую шаблон объявляет при ЛЮБЫХ значениях.
//
// Выбрасывание условий сделано намеренно и усиливает гейт: backend, который
// появляется только под каким-то профилем, всё равно попадает под проверку.
func readDeclaredYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("чтение %s: %v — гейт не прочитал объявление и не вправе давать вердикт", path, err)
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "{{") && strings.HasSuffix(trimmed, "}}") {
			continue // управляющее действие целиком — структуры не несёт
		}
		kept = append(kept, templateAction.ReplaceAllString(line, "__tmpl__"))
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(kept, "\n")), &doc); err != nil {
		t.Fatalf("разбор объявления %s: %v", path, err)
	}
	if len(doc) == 0 {
		t.Fatalf("объявление %s пусто после снятия действий — читать нечего", path)
	}
	return doc
}

// ingressBackendPorts вытаскивает КАЖДОЕ имя порта, названное backend'ом входа.
func ingressBackendPorts(t *testing.T, path string) []string {
	t.Helper()
	doc := readDeclaredYAML(t, path)
	spec, _ := doc["spec"].(map[string]any)
	rules, _ := spec["rules"].([]any)
	var out []string
	for _, r := range rules {
		rule, _ := r.(map[string]any)
		httpBlock, _ := rule["http"].(map[string]any)
		paths, _ := httpBlock["paths"].([]any)
		for _, p := range paths {
			pth, _ := p.(map[string]any)
			backend, _ := pth["backend"].(map[string]any)
			svc, _ := backend["service"].(map[string]any)
			port, _ := svc["port"].(map[string]any)
			if name, ok := port["name"].(string); ok {
				out = append(out, name)
				continue
			}
			if num, ok := port["number"]; ok {
				t.Fatalf("%s: backend адресует порт номером (%v), а не именем — "+
					"номер не сверить с объявлением Service, и гейт теряет предмет", path, num)
			}
			t.Fatalf("%s: у backend'а нет порта — вход никуда не ведёт", path)
		}
	}
	return out
}

// servicePortNames — имена портов, объявленные Service api-gateway.
func servicePortNames(t *testing.T) []string {
	t.Helper()
	doc := readDeclaredYAML(t, subchartService)
	spec, _ := doc["spec"].(map[string]any)
	ports, _ := spec["ports"].([]any)
	var out []string
	for _, p := range ports {
		port, _ := p.(map[string]any)
		if name, ok := port["name"].(string); ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// TestIngress_BackendNeverTargetsInternalAdminPort — ни один вход не называет
// порт admin-REST, и запрет при этом имеет предмет.
func TestIngress_BackendNeverTargetsInternalAdminPort(t *testing.T) {
	svcPorts := servicePortNames(t)
	t.Logf("осмотрено: Service объявляет порты %v", svcPorts)

	// Парный положительный №1: запрещаемое имя действительно существует.
	// Без этого «backend не называет internal-rest» держалось бы и в дереве,
	// где такого порта нет вовсе, — форма запрета без предмета.
	if !contains(svcPorts, internalAdminPortName) {
		t.Fatalf("Service не объявляет порт %q — запрет ниже стал беспредметным; "+
			"либо порт переименовали (обнови константу), либо admin-REST больше не "+
			"выставляется и гейт надо переписать, но молча он остаться не может",
			internalAdminPortName)
	}
	// Парный положительный №2: внешне-помеченный порт тоже существует, иначе
	// требование «вести на него» ниже было бы невыполнимо.
	if !contains(svcPorts, externalMarkedPortName) {
		t.Fatalf("Service не объявляет порт %q — вести внешний вход некуда", externalMarkedPortName)
	}

	ingresses := []string{subchartIngress, umbrellaIngress}
	total := 0
	for _, ing := range ingresses {
		ports := ingressBackendPorts(t, ing)
		if len(ports) == 0 {
			t.Fatalf("%s: не найдено ни одного backend'а — гейт ничего не осмотрел, "+
				"и его молчание не означает, что свойство держится", ing)
		}
		total += len(ports)
		for _, p := range ports {
			if p == internalAdminPortName {
				t.Errorf("%s: backend входа адресует порт %q — это единственный слушатель, "+
					"отдающий admin-REST (запрет #6); он существует для admin-UI и "+
					"port-forward, не для публичного края", ing, internalAdminPortName)
			}
			// Имя backend'а обязано быть настоящим портом Service: иначе опечатка
			// проходила бы этот гейт как «не internal-rest» и вход вёл бы в никуда.
			if !contains(svcPorts, p) {
				t.Errorf("%s: backend называет порт %q, которого Service не объявляет (%v) — "+
					"вход никуда не ведёт, и запрет выше на нём ничего не значит",
					ing, p, svcPorts)
			}
		}
	}
	t.Logf("осмотрено: %d шаблонов входа, %d backend'ов", len(ingresses), total)
}

// TestIngress_PublicEdgeTargetsTheExternalMarkedListener — вход, который umbrella
// действительно рендерит, ведёт на помеченный внешним слушатель.
//
// Именно пометка решает: REST-диспетчер 404-ит Internal*-пути только для
// запросов, пришедших на помеченного слушателя. Вход, ведущий на `cmux`,
// открывает admin-REST снаружи, оставаясь при этом совершенно исправным входом.
func TestIngress_PublicEdgeTargetsTheExternalMarkedListener(t *testing.T) {
	ports := ingressBackendPorts(t, umbrellaIngress)
	if len(ports) == 0 {
		t.Fatal("umbrella-вход не объявляет ни одного backend'а — осматривать нечего")
	}
	for _, p := range ports {
		if p != externalMarkedPortName {
			t.Errorf("umbrella-вход адресует порт %q, а обязан %q: только на помеченном "+
				"слушателе REST-диспетчер отвечает 404 на Internal*-пути, на %q они "+
				"обслуживаются", p, externalMarkedPortName, unmarkedPortName)
		}
	}
	t.Logf("осмотрено: umbrella-вход, backend'ов %d, порты %v", len(ports), ports)
}

// TestProfiles_SubchartIngressStaysDisabled — вход под-чарта ведёт на `cmux`,
// поэтому в umbrella он обязан оставаться выключенным ВО ВСЕХ профилях.
//
// Проверяется перечислением, а не образцом: достаточно одного профиля, который
// его включит, чтобы отрендерились два входа и один из них открыл admin-REST
// наружу. Базовое значения-объявление чарта — основа любого профиля, поэтому
// оно обязано нести выключение, а ни один оверлей — не отменять его.
func TestProfiles_SubchartIngressStaysDisabled(t *testing.T) {
	matches, err := filepath.Glob(umbrellaValuesGl)
	if err != nil {
		t.Fatalf("поиск профилей: %v", err)
	}
	sort.Strings(matches)
	if len(matches) < 2 {
		t.Fatalf("найдено %d файлов значений umbrella — гейт смотрит не туда, "+
			"его вердикт беспредметен", len(matches))
	}

	baseDeclaresOff := false
	for _, path := range matches {
		raw, rerr := os.ReadFile(path) //nolint:gosec // путь из glob по дереву репозитория
		if rerr != nil {
			t.Fatalf("чтение %s: %v", path, rerr)
		}
		var tree map[string]any
		if uerr := yaml.Unmarshal(raw, &tree); uerr != nil {
			t.Fatalf("разбор %s: %v", path, uerr)
		}
		ag, _ := tree["api-gateway"].(map[string]any)
		ing, ok := ag["ingress"].(map[string]any)
		if !ok {
			continue // профиль про вход под-чарта не высказывается — базовое значение в силе
		}
		enabled, has := ing["enabled"]
		if !has {
			continue
		}
		if enabled == true {
			t.Errorf("%s: включает вход под-чарта (api-gateway.ingress.enabled=true). Он ведёт "+
				"на %q — слушателя, который отдаёт Internal*-пути; вместе с umbrella-входом "+
				"отрендерятся два входа, и один из них публикует admin-поверхность",
				filepath.Base(path), unmarkedPortName)
		}
		if filepath.Base(path) == "values.yaml" && enabled == false {
			baseDeclaresOff = true
		}
	}
	if !baseDeclaresOff {
		t.Error("базовый values.yaml umbrella не объявляет api-gateway.ingress.enabled=false — " +
			"вход под-чарта включён по умолчанию его собственным чартом, поэтому " +
			"молчание базового профиля означает, что он отрендерится")
	}
	t.Logf("осмотрено: %d файлов значений umbrella", len(matches))
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
