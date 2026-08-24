// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation_endpoint_test.go — every deployed stand must tell the gateway where
// to ask whether a token has been revoked.
//
// The gateway cannot work this address out. Introspection is served by the
// identity provider's ADMIN API, on a Service and port distinct from the public
// issuer, and reachable only inside the cluster — so a profile that leaves it
// out does not fall back to something workable, it leaves the check with nowhere
// to ask. The same holds for the admin base the logout handler uses to end the
// provider-side session.
//
// Both used to be DERIVED from the public issuer when unset, and no profile set
// either, so every stand ran with both aimed at a server that does not serve
// them. That is why the addresses are asserted here rather than assumed.
//
// This guard reads the DECLARATIONS, like its neighbour token_shape_test.go: the
// contract is what the profiles declare, it needs no chart dependencies, and it
// therefore can never skip. It merges each stack the way helm does, because the
// profiles are layered — the base carries the address and an overlay may correct
// it, so asking each FILE in isolation would demand redundant restatements and
// still miss an overlay that blanks the value.
package deploy_test

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// readRepoFile reads a file addressed from the repository root. Like
// umbrellaValues it reads the declaration, not a render, so it needs nothing
// installed and cannot skip.
func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, parts...)...)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// stacksTable — the ONE place in the tree where the `-f` chains are declared.
// Read from here, from deploy/tests/helm/stacks.sh and from the deploy package;
// nowhere else, and TestNoSecondCopyOfAStackChain (deploy/stack_table_test.go)
// keeps it that way.
const stacksTable = "../../deploy/stacks.txt"

var stackTableLine = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*):(values[^,\s]*(?:,values[^,\s]*)*)$`)

// deployableStacks — the `-f` chains helm is actually invoked with, in order,
// read from the table above rather than restated here.
//
// This file used to carry its own copy. It disagreed with the shell-side table
// about which layers one of the stacks is made of, and BOTH stayed green —
// each honestly checked the stand it had declared. Worse, the disagreement
// decided whether that stand counted as production-class at all, so the two
// answers were mutually exclusive and nothing could tell you which was true.
//
// "dev" is an INTERMEDIATE chain, not a stand anybody leaves running: `dev-up`
// rolls it during the two-phase bootstrap and then upgrades onto "dev-prod",
// which is where the local stand ends up. It is in the table because helm really
// is invoked with it — a chain that renders during bootstrap can still crash-loop
// a pod — and because every question asked here is one the intermediate state
// must also answer.
//
// values.fe3455-ory.yaml is deliberately absent from the table — it is gitignored
// (site credentials) and carries no gateway configuration; the cutover script
// appends it itself.
func deployableStacks(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(stacksTable)
	if err != nil {
		t.Fatalf("stack table %s is unreadable (%v) — the premise of every check in this "+
			"package is gone, which is not the same as a clean tree", stacksTable, err)
	}
	out := map[string][]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := stackTableLine.FindStringSubmatch(line)
		if m == nil {
			// An unparsed line is NOT "fewer stacks", it is "the predicate stopped
			// recognising them". Staying silent here narrows every check downstream.
			t.Fatalf("stack table line not parsed: %q (%s)", line, stacksTable)
		}
		out[m[1]] = strings.Split(m[2], ",")
	}
	if len(out) == 0 {
		t.Fatalf("%s declares no stacks — this package is not entitled to conclude that "+
			"none are left", stacksTable)
	}
	return out
}

// introspectionAdminPath — the path the provider's admin API serves token
// introspection on, mirrored from the gateway's own boot guard. An address
// ending anywhere else is the public API, which serves no introspection at all.
const introspectionAdminPath = "/admin/oauth2/introspect"

// mergeInto overlays src onto dst the way helm merges values files: maps merge
// key by key, anything else replaces wholesale.
func mergeInto(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if cur, ok := dst[k].(map[string]any); ok {
				dst[k] = mergeInto(cur, sub)
				continue
			}
		}
		dst[k] = v
	}
	return dst
}

// resolveStack merges a stack's profiles in order and returns the gateway value
// at the given path, or ("", false) when the stack never declares it.
func resolveStack(t *testing.T, stack []string, path ...string) (string, bool) {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	var cur any = merged
	for _, key := range append([]string{"api-gateway"}, path...) {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[key]; !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok && strings.TrimSpace(s) != ""
}

// Every stack that deploys the gateway must name the introspection endpoint, and
// it must be the admin path — pointing it at the public API is exactly the state
// this contract exists to prevent.
func TestStacks_DeclareIntrospectionEndpoint(t *testing.T) {
	for name, stack := range deployableStacks(t) {
		t.Run(name, func(t *testing.T) {
			got, ok := resolveStack(t, stack, "hydra", "introspectionUrl")
			if !ok {
				t.Fatalf("%s (%s): api-gateway.hydra.introspectionUrl is not declared — the "+
					"revocation check has nowhere to ask, so every token stays good until it "+
					"expires no matter what is revoked",
					name, strings.Join(stack, " + "))
			}
			if err := checkAdminEndpoint(got, introspectionAdminPath); err != nil {
				t.Errorf("%s: api-gateway.hydra.introspectionUrl %v", name, err)
			}
		})
	}
}

// And the admin base the logout handler needs to end the provider-side session.
// Unset, the session kill is skipped and signing out leaves the session alive.
func TestStacks_DeclareAdminEndpoint(t *testing.T) {
	for name, stack := range deployableStacks(t) {
		t.Run(name, func(t *testing.T) {
			got, ok := resolveStack(t, stack, "hydra", "adminUrl")
			if !ok {
				t.Fatalf("%s (%s): api-gateway.hydra.adminUrl is not declared — signing out "+
					"then leaves the session alive at the identity provider",
					name, strings.Join(stack, " + "))
			}
			if err := checkAdminEndpoint(got, ""); err != nil {
				t.Errorf("%s: api-gateway.hydra.adminUrl %v", name, err)
			}
		})
	}
}

// The chart must still emit the environment variables these values drive. A
// value nothing renders is a decision that never reaches the process — the same
// way the sender-constrained token knob was documented for its whole life while
// no template emitted it.
func TestChart_EmitsRevocationEnv(t *testing.T) {
	deployment := readRepoFile(t, "gateway", "deploy", "templates", "deployment.yaml")
	// Пара клиентской личности стоит здесь по той же причине, что и адреса:
	// профиль её ОБЪЯВЛЯЕТ (это утверждает соседний declared-тест), но пока
	// шаблон её не эмитит, объявление ничего не меняет — ручка инертна, а
	// контроль выглядит настроенным. Ровно этим и отличалось состояние, при
	// котором каждый предъявитель нашей чеканки получал отказ.
	// Ручки НАШЕЙ полосы (KACHO_API_GATEWAY_..._TOKEN_...) отсюда выведены и
	// сверяются ВЫВОДИМО — TestChart_EmitsEveryDeclaredTokenAcceptanceKnob
	// читает их перечень из объявления config. Выписанный список рядом с
	// выводимым дал бы два места об одном предмете, и разошлись бы они молча:
	// новая ручка попадала бы в одно и не попадала в другое.
	for _, name := range []string{
		"KACHO_HYDRA_INTROSPECTION_URL", "KACHO_HYDRA_ADMIN_URL",
	} {
		// Имя сверяется ДО КОНЦА СТРОКИ, а не вхождением: подстрока
		// удовлетворяется и удлинённым именем, поэтому переименование
		// `…_CERT_FILE` → `…_CERT_FILE_X` оставляло гейт зелёным. Найдено
		// инъекцией при заведении второй пары — до неё гейт три имени из трёх
		// «проверял» так же.
		if !strings.Contains(deployment, "name: "+name+"\n") {
			t.Errorf("the api-gateway template no longer emits %s — the values knob would be "+
				"silently inert and the profiles above would assert nothing", name)
		}
	}
}

// checkAdminEndpoint mirrors the gateway's boot guard: an absolute in-cluster
// http(s) URL and, for introspection, the admin path.
func checkAdminEndpoint(raw, wantPath string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("is not a valid URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must be an absolute http(s) URL, got %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("has no host: %q", raw)
	}
	if wantPath != "" && strings.TrimRight(u.Path, "/") != wantPath {
		return fmt.Errorf("must address %q, got %q — the public OAuth2 API serves no "+
			"introspection, and the gateway refuses to start on an address shaped like it",
			wantPath, u.Path)
	}
	return nil
}

// ─── НАША ПОЛОСА ОТЗЫВА: СТЕНД, А НЕ ФАЙЛ ───────────────────────────────────

// resolveStackGateway сливает профили стенда так, как их сливает helm, и отдаёт
// поддерево края.
func resolveStackGateway(t *testing.T, stack []string) (map[string]any, bool) {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	gw, ok := merged["api-gateway"].(map[string]any)
	return gw, ok
}

// TestStacks_AcceptingOurIssuerNameTheRevocationAuthority — стенд, принимающий
// НАШЕГО издателя, обязан назвать НАШ авторитет отзыва и то, чем край
// представляется ему.
//
// # Почему СТЕНД, а не файл — соседняя проба спрашивает не то же самое
//
// f1b_token_acceptance_declared_test.go спрашивает каждый ФАЙЛ: «объявление
// этого файла даёт поднимающийся процесс». Это верное свойство и оно остаётся.
// Но стенд поднимается ЦЕПОЧКОЙ (deploy/stacks.txt), и накладка вправе
// переопределить любое значение базового слоя одной строкой. Накладка, ГАСЯЩАЯ
// адрес авторитета, пофайловому чтению невидима by construction: сама она
// нашего издателя не объявляет (значит пропускается), а базовый слой объявляет
// оба значения (значит проходит). Померено инъекцией при заведении этой пробы:
// накладка `revocationUrl: ""` у одного стенда оставляла ВЕСЬ пакет зелёным.
//
// # Почему зовётся НАСТОЯЩИЙ читатель, а не свой предикат
//
// Вердикт выносит config.Config.TokenAcceptance — тот же предикат, который
// исполняет процесс при старте. Он же держит запрет п.9 (адрес объявляется, а
// не выводится из чужого базового): относительный адрес отвергается, потому что
// выведенный адрес всегда непуст и потому контроль выглядел бы включённым.
//
// # Чего здесь НЕ утверждается
//
// Не утверждается, что адрес разрешается и отвечает: это свойство поднятого
// кластера. И ноль стендов, принимающих нашего издателя, — состояние ЗАКОННОЕ,
// а не поломка: откат полосы состоит ровно в снятии нашего издателя. Поэтому
// перепись печатает ОБЕ величины, а падает проба только на нуле прочитанных
// стендов — там «ноль находок» означало бы «ноль прочитанного».
func TestStacks_AcceptingOurIssuerNameTheRevocationAuthority(t *testing.T) {
	stacks := deployableStacks(t)
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)

	read, declaringOurIssuer, namingAuthority := 0, 0, 0
	for _, name := range names {
		stack := stacks[name]
		gw, ok := resolveStackGateway(t, stack)
		if !ok {
			continue
		}
		read++
		cfg, _ := f1bGatewayConfig(gw)
		chain := strings.Join(stack, " + ")

		// Намерение и адрес считаются ОТДЕЛЬНО от вердикта: иначе обе колонки
		// переписи совпадали бы тождественно (расхождение уже отвергнуто
		// читателем выше), и перепись перестала бы что-либо измерять.
		wantsOurIssuer := strings.TrimSpace(cfg.PlatformTokenIssuer) != ""
		hasAuthority := strings.TrimSpace(cfg.PlatformTokenRevocationURL) != ""
		if wantsOurIssuer {
			declaringOurIssuer++
		}
		if hasAuthority {
			namingAuthority++
		}

		bindings, err := cfg.TokenAcceptance()
		if err != nil {
			t.Errorf("стенд %s (%s): объявление приёма, с которым процесс НЕ ПОДНИМЕТСЯ: %v\n\n"+
				"Отказ верен и не смягчается: контроль, действующий только там, где "+
				"удостоверение ВЫДАЮТ, отзывом не является.", name, chain, err)
			continue
		}
		reads := false
		for _, b := range bindings {
			reads = reads || b.ReadRevocation
		}
		if !reads {
			if wantsOurIssuer {
				t.Errorf("стенд %s (%s): назван НАШ издатель %q, а полоса чтения отзыва не "+
					"включилась — объявление не доезжает до читателя",
					name, chain, cfg.PlatformTokenIssuer)
			}
			continue
		}

		cert := strings.TrimSpace(cfg.PlatformTokenRevocationCertFile)
		key := strings.TrimSpace(cfg.PlatformTokenRevocationKeyFile)
		switch {
		case cert == "" && key == "":
			t.Errorf("стенд %s (%s): назван авторитет отзыва %q, но не названа клиентская "+
				"пара хопа (tokenAcceptance.revocationClientCert.certFile/keyFile). "+
				"Авторитет спрашивает проверенную цепочку и без неё отвечает отказом — "+
				"контроль будет выглядеть настроенным, отказывая КАЖДОМУ предъявителю",
				name, chain, cfg.PlatformTokenRevocationURL)
		case cert == "" || key == "":
			t.Errorf("стенд %s (%s): клиентская пара хопа названа НАПОЛОВИНУ "+
				"(certFile=%q keyFile=%q) — процесс откажется стартовать; половина пары "+
				"хуже отсутствия обеих, потому что выглядит настроенной",
				name, chain, cert, key)
		}
	}

	t.Logf("перепись: стендов прочитано %d · объявляют НАШЕГО издателя %d · называют авторитет %d",
		read, declaringOurIssuer, namingAuthority)
	if read == 0 {
		t.Fatalf("прочитано НОЛЬ стендов, называющих край (%s) — «ноль находок» на таком "+
			"объёме означает «ноль прочитанного», и молчание этой пробы сказано ни о чём",
			stacksTable)
	}
}

// tokenLaneKnob — объявление ручки полосы приёма токена в config.
//
// Перечень ВЫВОДИТСЯ из объявления, а не выписывается: выписанный разошёлся бы
// с деревом молча, и новая ручка осталась бы непровязанной в чарте — ровно тот
// случай, когда ручка задокументирована всю свою жизнь, а шаблон её не эмитит.
var tokenLaneKnob = regexp.MustCompile(
	`envconfig:"(KACHO_API_GATEWAY_(?:TOKEN_ISSUER[A-Z_]*|PLATFORM_TOKEN[A-Z_]*))"`)

// tokenLaneEnv — то же имя, как его эмитит шаблон.
var tokenLaneEnv = regexp.MustCompile(
	`name: (KACHO_API_GATEWAY_(?:TOKEN_ISSUER[A-Z_]*|PLATFORM_TOKEN[A-Z_]*))\n`)

// TestChart_EmitsEveryDeclaredTokenAcceptanceKnob — каждая объявленная ручка
// полосы приёма токена обязана доезжать до процесса, и наоборот.
//
// # Почему это ПАРА утверждений, а не одно
//
// Ручка, объявленная config и НЕ эмитируемая шаблоном, инертна: профиль её
// задаёт, процесс её не видит, и решение не доезжает. Для адреса авторитета
// исход был бы шумным (страж старта отказывает на пустом), а для САМОГО НАШЕГО
// ИЗДАТЕЛЯ — тихим: без него полоса не включается вовсе, отзыв не читается ни
// разу, и НИЧТО не отказывает — состояние выглядит исправным.
//
// Обратное — имя, которое шаблон эмитит, а config не читает, — переживает свой
// предмет так же тихо: значение принято и никогда не прочитано.
//
// # Почему до конца строки, а не вхождением
//
// Подстрока удовлетворяется и УДЛИНЁННЫМ именем, поэтому переименование
// `…_ISSUER` → `…_ISSUER_X` оставляло гейт зелёным. Найдено инъекцией.
func TestChart_EmitsEveryDeclaredTokenAcceptanceKnob(t *testing.T) {
	declaration := readRepoFile(t, "gateway", "internal", "config", "config.go")
	deployment := readRepoFile(t, "gateway", "deploy", "templates", "deployment.yaml")

	declared := map[string]bool{}
	for _, m := range tokenLaneKnob.FindAllStringSubmatch(declaration, -1) {
		declared[m[1]] = true
	}
	emitted := map[string]bool{}
	for _, m := range tokenLaneEnv.FindAllStringSubmatch(deployment, -1) {
		emitted[m[1]] = true
	}

	if len(declared) == 0 {
		t.Fatal("в объявлении config не распознано НИ ОДНОЙ ручки полосы приёма токена — " +
			"это не «полосы нет», а «предикат перестал её узнавать»; вердикта у этой пробы нет")
	}

	wired := 0
	for _, name := range sortedKeys(declared) {
		if !emitted[name] {
			t.Errorf("config объявляет %s, а шаблон края её НЕ ЭМИТИТ — профиль задаёт "+
				"значение, процесс его не видит, и решение не доезжает вовсе", name)
			continue
		}
		wired++
	}
	for _, name := range sortedKeys(emitted) {
		if !declared[name] {
			t.Errorf("шаблон края эмитит %s, а config такой ручки НЕ ЧИТАЕТ — значение "+
				"принято и не прочитано ни разу; переменная пережила свой предмет", name)
		}
	}

	t.Logf("перепись: ручек полосы объявлено %d · шаблон эмитит %d · сходятся %d",
		len(declared), len(emitted), wired)
}

// sortedKeys — детерминированный порядок обхода набора имён.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// tokenLaneEnvValue — пара «имя переменной полосы → выражение её значения» в
// шаблоне.
var tokenLaneEnvValue = regexp.MustCompile(
	`name: (KACHO_API_GATEWAY_(?:TOKEN_ISSUER[A-Z_]*|PLATFORM_TOKEN[A-Z_]*))\n\s*value: \{\{([^}]*)\}\}`)

// TestChart_TokenLaneEnvIsWiredToItsOwnKnob — переменная полосы приёма токена
// обязана брать значение из СВОЕГО объявления профиля, а не из соседнего.
//
// # Что это закрывает и почему одного «имя эмитится» мало
//
// Читателей объявления двое: проба выше спрашивает КЛЮЧ ПРОФИЛЯ, процесс читает
// ПЕРЕМЕННУЮ ОКРУЖЕНИЯ, и связывает их ровно одно место — эта строка шаблона.
// Перевесив её на адрес соседа (`.Values.hydra.introspectionUrl`), получаем
// состояние, в котором обе проверки зелены, профиль объявляет одно, а процесс
// получает другое — и адрес контроля безопасности оказывается ВЫВЕДЕННЫМ из
// чужого. Выведенный адрес всегда непуст, поэтому страж старта молчит, контроль
// выглядит включённым и ведёт в никуда; ни один профиль не обязан ничего
// задавать, чтобы это заметить.
//
// # Границы
//
// Утверждается происхождение выражения, а НЕ то, что адрес разрешается и
// отвечает: последнее — свойство поднятого кластера.
func TestChart_TokenLaneEnvIsWiredToItsOwnKnob(t *testing.T) {
	deployment := readRepoFile(t, "gateway", "deploy", "templates", "deployment.yaml")
	pairs := tokenLaneEnvValue.FindAllStringSubmatch(deployment, -1)
	if len(pairs) == 0 {
		t.Fatal("в шаблоне края не распознано НИ ОДНОЙ пары «переменная полосы → значение» — " +
			"это не «полосы нет», а «предикат перестал её узнавать»")
	}
	own := 0
	for _, m := range pairs {
		name, expr := m[1], strings.TrimSpace(m[2])
		if !strings.Contains(expr, ".Values.tokenAcceptance") {
			t.Errorf("%s берёт значение из %q — это не её объявление. Адрес контроля "+
				"безопасности, выведенный из чужого, всегда непуст: страж старта молчит, "+
				"контроль выглядит включённым и ведёт в никуда", name, expr)
			continue
		}
		own++
	}
	t.Logf("перепись: пар «переменная полосы → значение» %d · берут из своего объявления %d",
		len(pairs), own)
}
