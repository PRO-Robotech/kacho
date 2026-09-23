// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// provider_road_posture_test.go — дорога к административному API поставщика
// личности судится ПО ПОСАДКЕ той половины стенда, чей страж старта её
// требует (kacho#2816).
//
// # Предмет
//
// Четыре пробы стеков (revocation_endpoint_test.go, admin_hop_transport_test.go)
// требовали адресов административного API поставщика от КАЖДОЙ цепочки: адреса
// интроспекции и снятия сессии у края, административной дороги у службы
// доступа. Требование было верно, пока поставщик стоял на каждом стенде.
// Цепочка `own` его больше не поднимает, и пробы краснели на стенде, который
// поднимается: сами процессы этих адресов под `own` не требуют.
//
//   - край: validateProductionRevocationConfig
//     (gateway/cmd/api-gateway/revocation_validation.go) требует обоих адресов
//     только под `external`; под `own` ось поставщика замещается осью нашего
//     авторитета отзыва;
//   - служба доступа: строки authn.hydra-admin-url и authn.hydra-admin-ca-file
//     её таблицы требований объявлены с полосой `external`
//     (kaname, internal/apps/kaname/config/required_settings.go).
//
// # Как судится
//
// Посадка читается так, как её получит страж старта ЭТОЙ половины: объявление
// слитой цепочки, а у молчащей цепочки — умолчание собственного чарта половины
// (helm кладёт его под каждый `-f`, и слоем цепочки оно не значится). Значение
// разбирает тот же словарь, что и процесс (corelib/identityposture; у края —
// через его config.Config). Второго словаря здесь нет.
//
//   - `external` — как прежде: адрес обязан быть объявлен;
//   - `own` — адреса НЕТ, и это утверждается, а не пропускается. Отсутствие
//     обязано быть настоящим: процесс, получивший адрес, провязывает его, даже
//     когда не требует, — край спрашивал бы интроспекцию на каждом промахе кэша
//     у процесса, которого на этой посадке нет. То же на живом стенде
//     утверждает секция D deploy/scripts/assert-admin-hop-transport.sh (исход
//     consumer-names-provider); здесь — её половина по объявлению;
//   - посадка не разбирается словарём процесса либо не объявлена ни цепочкой,
//     ни чартом — находка: какие адреса требуются, не решено.
//
// Форму и транспорт ОБЪЯВЛЕННОГО адреса пробы судят одинаково на обеих
// посадках — как страж края: посадка снимает требование НАЛИЧИЯ, а не правила
// формы.
//
// # Чего здесь НЕ утверждается
//
// Согласие посадок двух половин одного стенда — предмет
// deploy/helm/umbrella/identity_posture_profiles_test.go; здесь каждая половина
// судится своей посадкой, и второго суждения о согласии не заводится.
package deploy_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// postureHalf — ОДНА половина стенда: где цепочка объявляет её посадку, откуда
// молчащая цепочка берёт умолчание и каким разборщиком посадку читает процесс.
type postureHalf struct {
	// who — половина в родительном падеже, для текста находки.
	who string
	// path — абсолютный путь ручки в слитой цепочке зонта.
	path []string
	// chartPath — путь той же ручки в собственных значениях чарта половины.
	chartPath []string
	// chartDefaults — собственные значения чарта половины.
	chartDefaults func(t *testing.T) map[string]any
	// parse — разборщик ЭТОГО процесса; пустое значение — «не задано» без ошибки.
	parse func(raw string) (identityposture.Provider, error)
}

var (
	// edgePostureHalf — край. Ручку разбирает его же config.Config, то есть
	// ровно тот код, что исполняет композиционный корень при старте.
	edgePostureHalf = postureHalf{
		who:           "края",
		path:          []string{"api-gateway", "authn", "identityProvider"},
		chartPath:     []string{"authn", "identityProvider"},
		chartDefaults: gatewayChartValues,
		parse: func(raw string) (identityposture.Provider, error) {
			return config.Config{IdentityProvider: raw}.ResolvedIdentityProvider()
		},
	}
	// iamPostureHalf — служба доступа. Её разборщик — общий словарь
	// corelib/identityposture; пустое значение служба читает как «не задано»,
	// и так же читает его здесь.
	iamPostureHalf = postureHalf{
		who:       "службы доступа",
		path:      []string{"kaname", "config", "authn", "identityProvider"},
		chartPath: []string{"config", "authn", "identityProvider"},
		chartDefaults: func(t *testing.T) map[string]any {
			t.Helper()
			return umbrellaValues(t, filepath.Join("charts", "kaname", "values.yaml"))
		},
		parse: func(raw string) (identityposture.Provider, error) {
			if strings.TrimSpace(raw) == "" {
				return identityposture.Unset, nil
			}
			return identityposture.Parse("kaname.config.authn.identityProvider", raw)
		},
	}
)

// postureReading — посадка одной половины, какой её получит страж старта.
type postureReading struct {
	Provider identityposture.Provider
	// Raw — прочитанное значение, для текста находки.
	Raw string
	// Inherited — цепочка ручку не называет, действует умолчание чарта.
	Inherited bool
	// Err — значение не разбирается словарём процесса.
	Err error
}

// scalarAt — значение по абсолютному пути и признак того, что КЛЮЧ ЕСТЬ.
//
// Присутствие и значение разведены намеренно: ключ, заданный пустым или null,
// перекрывает умолчание чарта (шаблон с `with` тогда ничего не выставляет), а
// отсутствующий ключ — нет. Схлопнув их, проба читала бы гашёную посадку
// унаследованной.
func scalarAt(tree map[string]any, path ...string) (string, bool) {
	var cur any = tree
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[key]; !ok {
			return "", false
		}
	}
	switch v := cur.(type) {
	case nil:
		return "", true
	case string:
		return v, true
	default:
		return fmt.Sprint(v), true
	}
}

// readPosture — чистая: слитая цепочка и умолчания чарта → посадка половины.
// Вход ей подаёт и дерево, и инъекция.
func readPosture(half postureHalf, merged, chartDefaults map[string]any) postureReading {
	raw, present := scalarAt(merged, half.path...)
	inherited := false
	if !present {
		raw, _ = scalarAt(chartDefaults, half.chartPath...)
		inherited = true
	}
	p, err := half.parse(raw)
	return postureReading{Provider: p, Raw: raw, Inherited: inherited, Err: err}
}

// foldStack сливает профили цепочки так, как их сливает helm.
func foldStack(t *testing.T, stack []string) map[string]any {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	return merged
}

// stackPosture — посадка половины на слитой цепочке дерева.
func stackPosture(t *testing.T, stack []string, half postureHalf) postureReading {
	t.Helper()
	return readPosture(half, foldStack(t, stack), half.chartDefaults(t))
}

// providerRoadRequired — требует ли страж старта половины дорогу к поставщику
// при этой посадке. Ошибка означает, что по посадке судить нельзя, и это
// находка сама по себе.
func providerRoadRequired(who string, r postureReading) (bool, error) {
	switch {
	case r.Err != nil:
		return false, fmt.Errorf("посадка %s не разбирается словарём процесса (%v) — процесс "+
			"не поднимется, и требовать с цепочки адрес не о чем", who, r.Err)
	case r.Provider == identityposture.External:
		return true, nil
	case r.Provider == identityposture.Own:
		return false, nil
	case !r.Provider.IsSet():
		return false, fmt.Errorf("посадка %s не объявлена ни цепочкой, ни умолчанием чарта "+
			"(прочитано %q) — какие адреса поставщика требуются, не решено", who, r.Raw)
	default:
		return false, fmt.Errorf("посадка %s = %s этой пробе не известна — молчание означало бы, "+
			"что стек не осмотрен, а не что он исправен", who, r.Provider)
	}
}

// providerRoadKnob — ОДНА ручка адреса административного API поставщика.
type providerRoadKnob struct {
	// label — как ручку называет профиль.
	label string
	// half — чей страж старта её судит.
	half postureHalf
	// path — абсолютный путь в слитой цепочке.
	path []string
	// missing — чем отсутствие под `external` оборачивается для процесса.
	missing string
	// present — чем присутствие под `own` оборачивается для процесса.
	present string
}

var (
	introspectionRoad = providerRoadKnob{
		label: "api-gateway.hydra.introspectionUrl",
		half:  edgePostureHalf,
		path:  []string{"api-gateway", "hydra", "introspectionUrl"},
		missing: "the revocation check has nowhere to ask, so every token stays good until " +
			"it expires no matter what is revoked",
		present: "край провязывает заданный адрес в проверку отзыва и спрашивает его на каждом " +
			"промахе кэша — у процесса, которого на этой посадке нет",
	}
	adminRoad = providerRoadKnob{
		label:   "api-gateway.hydra.adminUrl",
		half:    edgePostureHalf,
		path:    []string{"api-gateway", "hydra", "adminUrl"},
		missing: "signing out then leaves the session alive at the identity provider",
		present: "выход человека шлёт снятие сессии процессу, которого на этой посадке нет, — " +
			"сессии у поставщика под `own` не существует",
	}
	iamAdminRoad = providerRoadKnob{
		label: "kaname.platform.iam.hydraAdminUrl",
		half:  iamPostureHalf,
		path:  []string{"kaname", "platform", "iam", "hydraAdminUrl"},
		missing: "iam then DERIVES it from the issuer, which names the public ingress host and " +
			"does not resolve inside the cluster; the derivation is never empty, so the facade " +
			"reads as configured while addressing a host nobody chose",
		present: "фасад службы доступа адресует административные вызовы процессу, которого на " +
			"этой посадке нет",
	}
)

// roadPresenceFinding — чистая: судит НАЛИЧИЕ адреса по посадке половины и
// отдаёт находку либо "". Форму и транспорт объявленного адреса судит
// вызывающий — одинаково на обеих посадках.
func roadPresenceFinding(stack string, k providerRoadKnob, r postureReading, declared string) string {
	required, err := providerRoadRequired(k.half.who, r)
	if err != nil {
		return fmt.Sprintf("%s: %s: %v", stack, k.label, err)
	}
	declared = strings.TrimSpace(declared)
	switch {
	case required && declared == "":
		return fmt.Sprintf("%s: %s is not declared — %s", stack, k.label, k.missing)
	case !required && declared != "":
		return fmt.Sprintf("%s: посадка %s — %s, а %s объявлен (%q): страж старта его не "+
			"требует, но %s", stack, k.half.who, r.Provider, k.label, declared, k.present)
	}
	return ""
}

// postureCensus — ОБЪЁМ ОСМОТРЕННОГО по посадкам одной половины. «Находок
// ноль» обязано быть отличимо от «ни одного стека на этой посадке не было».
type postureCensus struct {
	Stacks, External, Inherited, Own, Unjudged int
}

func (c *postureCensus) add(r postureReading) {
	c.Stacks++
	switch {
	case r.Err != nil || !r.Provider.IsSet():
		c.Unjudged++
	case r.Provider == identityposture.External:
		c.External++
		if r.Inherited {
			c.Inherited++
		}
	case r.Provider == identityposture.Own:
		c.Own++
	default:
		c.Unjudged++
	}
}

func (c postureCensus) String() string {
	return fmt.Sprintf("стеков %d · посадка external %d (из них наследуют умолчание чарта %d) · "+
		"own %d · посадку судить нельзя %d", c.Stacks, c.External, c.Inherited, c.Own, c.Unjudged)
}
