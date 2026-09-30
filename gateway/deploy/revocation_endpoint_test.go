// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation_endpoint_test.go — every deployed stand must tell the gateway where
// to ask whether a token has been revoked, and the chart must carry what the
// process reads for it.
//
// WHICH AUTHORITY. Our own: a stand that accepts our issuer names our revocation
// authority (TestStacks_AcceptingOurIssuerNameTheRevocationAuthority below). The
// previous identity provider's admin-API addresses are gone (#2734): the edge no
// longer asks that provider anything, and the probes that demanded its addresses
// were retired together with them (the tombstone below says which and why).
//
// This guard reads the DECLARATIONS, like deploy/helm/umbrella/token_shape_test.go:
// the contract is what the profiles declare, it needs no chart dependencies, and it
// therefore can never skip. It merges each stack the way helm does, because the
// profiles are layered — the base carries the address and an overlay may correct
// it, so asking each FILE in isolation would demand redundant restatements and
// still miss an overlay that blanks the value.
package deploy_test

import (
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

// stacksTableFromRoot — the ONE place in the tree where the `-f` chains are
// declared, addressed from the repository root. TestNoSecondCopyOfAStackChain
// (deploy/stack_table_test.go) keeps the chains themselves from being copied
// anywhere else; it does not count readers. Inside this package the table has
// exactly one reader, readStackTable, and so exactly one grammar: two readers
// with two grammars would each honestly judge the stands its own grammar
// recognised, and a line one of them skips would narrow only half the package.
var stacksTableFromRoot = filepath.Join("deploy", "stacks.txt")

// stacksTable — the same table addressed from this package.
var stacksTable = filepath.Join("..", "..", stacksTableFromRoot)

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
	return readStackTable(t, stacksTable)
}

// readStackTable — the table's only reader in this package. It takes the path
// so that a check which derives the repository root on its own (see
// lanePrereqRoot) reads the same lines through the same grammar.
func readStackTable(t *testing.T, path string) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- the stack table of this tree
	if err != nil {
		t.Fatalf("stack table %s is unreadable (%v) — the premise of every check in this "+
			"package is gone, which is not the same as a clean tree", path, err)
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
			t.Fatalf("stack table line not parsed: %q (%s)", line, path)
		}
		out[m[1]] = strings.Split(m[2], ",")
	}
	if len(out) == 0 {
		t.Fatalf("%s declares no stacks — this package is not entitled to conclude that "+
			"none are left", path)
	}
	return out
}

// sortedStackNames — имена цепочек таблицы в устойчивом порядке. Обход карты
// давал бы подпробы и находки в порядке, разном от прогона к прогону, и два
// прогона одного дерева нельзя было бы сравнить построчно.
func sortedStackNames(stacks map[string][]string) []string {
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// mergeInto overlays src onto dst the way helm merges values files: maps merge
// key by key, anything else replaces wholesale. It is the package's only
// overlay.
//
// A map taken from src is COPIED into dst, never shared. A shared one would let
// the next overlay edit src in place: `mergeInto(mergeInto({}, base), late)`
// used to leave `late`'s keys inside `base`, so one profile tree held by a
// caller ended up carrying another profile's declarations.
func mergeInto(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			cur, _ := dst[k].(map[string]any)
			dst[k] = mergeInto(cur, sub)
			continue
		}
		dst[k] = v
	}
	return dst
}

// TestMergeInto_LeavesItsSourceIntact — the overlay does not write THROUGH
// itself into a source tree. The result carries both the layer and the overlay
// (the lawful twin of the property), while the layer the caller still holds
// stays exactly what was read from its file.
func TestMergeInto_LeavesItsSourceIntact(t *testing.T) {
	base := map[string]any{"kaname": map[string]any{"ports": map[string]any{"loginLane": 9100}}}
	late := map[string]any{"kaname": map[string]any{"ports": map[string]any{"public": 9090}}}

	merged := mergeInto(mergeInto(map[string]any{}, base), late)

	if got := laneString(lookupLane(merged, "kaname", "ports", "public")); got != "9090" {
		t.Fatalf("the overlay did not reach the result (kaname.ports.public = %q): %v", got, merged)
	}
	if got := laneString(lookupLane(merged, "kaname", "ports", "loginLane")); got != "9100" {
		t.Fatalf("the overlay erased the layer under it (kaname.ports.loginLane = %q): %v", got, merged)
	}
	if v, leaked := lookupLane(base, "kaname", "ports", "public"); leaked {
		t.Fatalf("the overlay wrote its key INTO THE SOURCE: base now carries kaname.ports.public = %v, "+
			"a declaration its file never made: %v", v, base)
	}
}

// ЗДЕСЬ СТОЯЛИ ТРИ ПРОБЫ О ДОРОГЕ КРАЯ К ПРЕЖНЕМУ ПОСТАВЩИКУ — и сняты вместе с ней
// (#2734): TestStacks_DeclareIntrospectionEndpoint и TestStacks_DeclareAdminEndpoint
// требовали адресов его административного API под посадкой `external`,
// TestChart_EmitsRevocationEnv требовал, чтобы шаблон края эти адреса эмитировал.
//
// Последняя ИСТЕКЛА, а не снята молча (#2778): её перечень был сведён с ведомостью
// снятых ручек (`internal/retiredknobs`), и имя, снятое с процесса, проба больше не
// требовала, а называла находкой своего перечня. Когда с процесса сняты оба имени
// перечня, требовать проба больше не может ничего, и снимается вместе с последним
// — тем же изменением, что снимает читателя. Что снятые имена не вернутся, судят
// двухколоночный гейт края (knob_producer_parity_test.go) и рендерная проба
// каждой цепочки (deploy/edge_retired_knobs_render_test.go).

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
// Перевесив её на адрес соседа (скажем, `.Values.advertisedEndpoint`), получаем
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
