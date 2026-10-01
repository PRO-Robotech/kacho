// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package umbrella_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// login_console_test.go — a production-class stand must resolve the sign-in
// console, because the console is what conducts the ceremony a HUMAN completes
// to end up holding a bearer the edge accepts (IAM-INT-1, S2).
//
// THE UNIT IS THE FOLDED STACK, NOT A SINGLE PROFILE — and that is the whole
// point of this file rather than a detail of it. Two honest idioms live in this
// package and they answer DIFFERENT questions:
//
//   - resolveStackAt / mergeInto / deployableStacks — "what does the RELEASE get",
//     i.e. helm's own left-to-right overlay of every `-f` in the chain;
//   - reading ONE profile file — "does this profile declare its own", which is
//     right only where `dev` and `prod` are alternatives rather than layers (the
//     retired token_shape_test.go asked exactly that about the removed identity
//     provider's tier, #1276).
//
// Picking the wrong one silently answers the other question. An earlier draft of
// the IAM-INT-1 acceptance did exactly that: it grepped ONE overlay, found the
// console unmentioned, and reported a second independent gap in the platform's
// ability to sign a human in. There was none — the late overlay is merely SILENT
// about the key, and helm's merge is additive, so silence changes nothing. The
// finding was an artefact of the measuring unit.
//
// So this gate folds. And because "the merge is additive" is the premise the
// whole answer rests on, the premise is asserted here too rather than assumed:
// see TestLoginConsole_PremiseLateSilenceDoesNotClearAnEarlierKey.
//
// WHERE THIS LIVES (#2734). Every key the gate reads is the umbrella's: the
// provider's sign-in console subchart, the service's lane port and the edge's
// relay address under the umbrella's `api-gateway` key. The gate lived in
// gateway/deploy and moved to the umbrella chart unchanged, next to its
// neighbour identity_posture_profiles_test.go that judges the posture agreement.

// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ИМЕННО ЧЕЛОВЕК ВХОДИТ — РЕШАЕТ ПОСАДКА, А НЕ АДРЕС, ВЫПИСАННЫЙ ЗДЕСЬ
//
// Вопрос у гейта один и он не менялся: «может ли человек на этом стенде пройти
// церемонию и оказаться с предъявителем, который край примет?» Адрес ответа —
// менялся.
//
// Полоса формы входа — предмет ПОСАДКИ, и посадка у службы одна — `own`
// (kaname#363): четыре глагола формы край ретранслирует на слушатель полосы
// службы (gateway/cmd/api-gateway/main.go, ветка
// `identityLane == identityposture.Own`; держит
// TestOwnLane_F3_45_TheLoginLaneRelayIsWiredUnderTheOwnPostureOnly). Чьё печенье
// край при этом ЧИТАЕТ, решает та же посадка: под `own` — только наше
// (TestOwnLane_F3_12_NoProviderCarrierReaderIsWiredUnderOwn).
//
// Поэтому на КАЖДОМ боевом стеке спрашиваются ОБЕ половины полосы — слушатель
// службы и адрес ретрансляции края, — потому что половина полосы даёт стенд, где
// форма отвечает 503 на каждом запросе, неотличимо от «служба лежит».
//
// ЗДЕСЬ БЫЛА ВТОРАЯ ВЕТВЬ — посадка `external`, где церемонию проводил экран
// входа прежнего поставщика, и гейт спрашивал его выключатель в значениях
// зонта. Подчарт экрана снят с зонта вместе с поставщиком (#1276), и путь,
// который ветвь читала, на каждом стеке отвечал «ключа нет»: ветвь исполнялась
// беспредметно. Снята тем же изменением, что её предмет получил держателя
// возврата — TestLoginConsole_EveryKeyItReadsBelongsToAChartOfTheUmbrella: путь
// чтения в подчарт, которого зонт не несёт, — находка. Посадка вне `own` теперь
// — «значение, которого гейт не знает»: человека на ней проверить нечем.

// gitignoredStackMember — ПОСЛЕДНИЙ `-f` живой цепочки fe3455, намеренно ВНЕ
// дерева: он несёт учётные данные площадки. Его отсутствие находкой НЕ является,
// и гейт говорит об этом вслух, вместо того чтобы тихо выдать частичное слитие
// за полноту.
//
// Число ВЫВОДИТСЯ и никогда не пересказывается: прежняя редакция писала прозой
// «3 из 4», цепочка затем получила отслеживаемый слой посадки, и проза
// продолжала утверждать прежнюю арифметику, пока слитие под ней менялось.
const gitignoredStackMember = "values.fe3455-ory.yaml"

// lanePortPath / laneURLPath — две половины СОБСТВЕННОЙ полосы формы: порт, на
// котором служба поднимает слушатель, и адрес, на который край ретранслирует
// глаголы. Обе — абсолютные пути в слитом дереве зонта.
var (
	lanePortPath = []string{"kaname", "ports", "loginLane"}
	laneURLPath  = []string{"api-gateway", "authn", "iamLoginLaneUrl"}
)

// ПОСАДКА СТЕКА — посадка службы, и она одна (kanameLanding, kaname#363): ключа
// посадки у подчарта службы нет (kacho#2818), читать с цепочки нечего. СОГЛАСИЕ
// края с ней — предмет deploy/helm/umbrella/identity_posture_profiles_test.go, и
// второго суждения о нём здесь не заводится.
const postureOwn = "own"

// loginConsoleFacts — что ОДНА цепочка объявила о том, чем на ней входит человек.
type loginConsoleFacts struct {
	Stack string
	// Posture — посадка личности стека. Пустая — посадка не установлена, и это
	// находка, а не наследование: умолчания посадки у чарта службы больше нет.
	Posture string
	// LanePort / LaneURL — половины собственной полосы формы.
	LanePort string
	LaneURL  string
}

// loginConsoleCensus — ОБЪЁМ ОСМОТРЕННОГО. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного», и от «боевых стеков не нашлось» тоже.
type loginConsoleCensus struct {
	Stacks     int
	Production int
	OnOwn      int
}

func (c loginConsoleCensus) String() string {
	return fmt.Sprintf("стеков в таблице %d · боевых %d · из них на собственной посадке %d",
		c.Stacks, c.Production, c.OnOwn)
}

// judgeLoginConsole — НАХОДКИ по перечню боевых цепочек.
//
// Функция чистая: вход ей подаёт и дерево, и инъекция. Второго разбора значений
// внутри неё нет — она судит уже прочитанное.
func judgeLoginConsole(facts []loginConsoleFacts) ([]string, loginConsoleCensus) {
	var findings []string
	census := loginConsoleCensus{}
	for _, f := range facts {
		census.Production++
		posture := strings.TrimSpace(f.Posture)
		switch posture {
		case postureOwn:
			census.OnOwn++
			// ОБЕ половины, и каждая называется отдельно: общий отказ «полоса не
			// настроена» скрыл бы, какая именно половина отсутствует, а половины
			// правят разные файлы.
			if strings.TrimSpace(f.LanePort) == "" {
				findings = append(findings, fmt.Sprintf(
					"%s: посадка %q, а слитый стек НЕ объявляет %s — слушатель формы не "+
						"поднимается, и службе нечем провести церемонию, которую на этой посадке "+
						"проводит она, а не чужой поставщик",
					f.Stack, posture, strings.Join(lanePortPath, ".")))
			}
			if strings.TrimSpace(f.LaneURL) == "" {
				findings = append(findings, fmt.Sprintf(
					"%s: посадка %q, а слитый стек НЕ объявляет %s — краю некуда ретранслировать "+
						"четыре глагола формы, и человек получает 503 на каждом запросе, "+
						"неотличимо от «служба лежит»",
					f.Stack, posture, strings.Join(laneURLPath, ".")))
			}
		default:
			findings = append(findings, fmt.Sprintf(
				"%s: посадка объявлена значением %q, которого этот гейт не знает. Молчание "+
					"здесь означало бы, что стенд не осмотрен, — а не что он исправен",
				f.Stack, posture))
		}
	}
	return findings, census
}

// resolveStackScalarAt читает СКАЛЯР по абсолютному пути в слитом стеке и
// отдаёт его текстом.
//
// Сосед `resolveStackAt` требует строки и отдаёт («», false) на числе; порт
// полосы — число, и строковый читатель молча объявил бы его необъявленным.
// Отдельный читатель, а не правка соседа: у него свой предмет, и смена его типа
// поменяла бы вердикт у чужих проверок.
func resolveStackScalarAt(t *testing.T, stack []string, path ...string) string {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	var cur any = merged
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		if cur, ok = m[key]; !ok {
			return ""
		}
	}
	switch v := cur.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case map[string]any, []any:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// TestStacks_ProductionClassResolvesTheLoginConsole — сценарий IAM-INT-1-18.
func TestStacks_ProductionClassResolvesTheLoginConsole(t *testing.T) {
	// ОБЪЁМ ОСМОТРЕННОГО — УТВЕРЖДЕНИЕ САМО ПО СЕБЕ. «Находок нет» обязано
	// отличаться от «читать было нечего»: опустевшая таблица и переставший
	// совпадать боевой предикат печатали бы ровно тот же зелёный.
	var facts []loginConsoleFacts
	stacks := deployableStacks(t)
	for _, name := range sortedStackNames(stacks) {
		stack := stacks[name]
		if !stackIsProductionClass(t, stack) {
			t.Logf("%s: dev-класс по собственному объявлению — пропущен (%d профиль(ей))", name, len(stack))
			continue
		}
		t.Logf("%s: боевой класс, слитие %d профиль(ей): %s",
			name, len(stack), strings.Join(stack, " -> "))

		facts = append(facts, loginConsoleFacts{
			Stack:    name,
			Posture:  kanameLanding,
			LanePort: resolveStackScalarAt(t, stack, lanePortPath...),
			LaneURL:  resolveStackScalarAt(t, stack, laneURLPath...),
		})
	}
	findings, census := judgeLoginConsole(facts)
	census.Stacks = len(stacks)
	if census.Production == 0 {
		t.Fatalf("гейт осмотрел НОЛЬ боевых стеков из %d объявленных. Либо опустела таблица "+
			"стеков, либо stackIsProductionClass перестал совпадать — что бы из двух ни "+
			"случилось, зелёное выше не значит ничего", len(stacks))
	}
	for _, f := range findings {
		t.Error(f)
	}
	t.Logf("перепись: %s", census)
}

// TestLoginConsoleGate_Injection_TheOwnLaneHalvesAreEachNamed — гейт УМЕЕТ
// УПАСТЬ на посадке `own`, и называет ту половину, которой нет.
//
// Без этого посадочная ветка была бы объявлением, а не проверкой: стек на `own`
// сегодня в дереве один и он полон, поэтому ветка исполняется, ничего не находя,
// и «зелено» о ней означало бы «условие не создано».
func TestLoginConsoleGate_Injection_TheOwnLaneHalvesAreEachNamed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts loginConsoleFacts
		want  string
	}{
		{"нет слушателя службы", loginConsoleFacts{
			Stack: "synthetic", Posture: postureOwn, LaneURL: "https://kaname-internal.kacho.svc:9100",
		}, "kaname.ports.loginLane"},
		{"нет адреса ретрансляции края", loginConsoleFacts{
			Stack: "synthetic", Posture: postureOwn, LanePort: "9100",
		}, "api-gateway.authn.iamLoginLaneUrl"},
		{"посадка неизвестного значения", loginConsoleFacts{
			Stack: "synthetic", Posture: "provider-x", LanePort: "9100",
		}, "которого этот гейт не знает"},
		// Посадка прежнего поставщика — тоже «не знает»: экрана, которым на ней
		// проходили церемонию, в зонте нет, и человека на такой посадке проверить
		// нечем. Прежде это была своя ветвь с двумя находками о его выключателе.
		{"посадка внешнего поставщика", loginConsoleFacts{
			Stack: "synthetic", Posture: "external",
			LanePort: "9100", LaneURL: "https://kaname-internal.kacho.svc:9100",
		}, "которого этот гейт не знает"},
		// Пустая посадка — не наследование, а находка: умолчания посадки у чарта
		// службы больше нет (kacho#2818).
		{"посадка не установлена", loginConsoleFacts{
			Stack: "synthetic", Posture: "",
			LanePort: "9100", LaneURL: "https://kaname-internal.kacho.svc:9100",
		}, "которого этот гейт не знает"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings, census := judgeLoginConsole([]loginConsoleFacts{tc.facts})
			if len(findings) != 1 {
				t.Fatalf("находок %d, ожидалась ровно одна: %v (перепись: %s)", len(findings), findings, census)
			}
			if !strings.Contains(findings[0], tc.want) {
				t.Fatalf("находка не называет %q: %s", tc.want, findings[0])
			}
		})
	}
}

// TestLoginConsoleGate_Twin_ACompleteOwnLaneIsSilent — ЗАКОННЫЙ БЛИЗНЕЦ к
// инъекции выше: тот же вход с обеими половинами полосы обязан молчать. Без
// него инъекция удовлетворялась бы гейтом, который краснеет на всяком `own`.
func TestLoginConsoleGate_Twin_ACompleteOwnLaneIsSilent(t *testing.T) {
	findings, census := judgeLoginConsole([]loginConsoleFacts{{
		Stack: "synthetic", Posture: postureOwn,
		LanePort: "9100", LaneURL: "https://kaname-internal.kacho.svc:9100",
	}})
	if len(findings) != 0 {
		t.Fatalf("полная собственная полоса объявлена находкой: %v", findings)
	}
	if census.OnOwn != 1 {
		t.Fatalf("перепись не отнесла стек к собственной посадке: %s", census)
	}
}

// TestLoginConsole_PremiseLateSilenceDoesNotClearAnEarlierKey — the gate above
// is only meaningful while helm's merge is ADDITIVE, i.e. while a later profile
// that says nothing about a key leaves the earlier value standing. That is the
// exact property an earlier reading of this stand got wrong, so it is asserted
// rather than trusted: if mergeInto is ever changed to a replacing merge, this
// fails and names the reason, instead of the gate above silently starting to
// measure the last profile only.
func TestLoginConsole_PremiseLateSilenceDoesNotClearAnEarlierKey(t *testing.T) {
	base := map[string]any{
		"kaname": map[string]any{
			"ports": map[string]any{"loginLane": 9100, "grpc": 9090},
		},
	}
	// A late overlay that touches a NEIGHBOURING key and says nothing about
	// `loginLane` — the shape a stand overlay has when it moves one port only.
	late := map[string]any{
		"kaname": map[string]any{
			"ports": map[string]any{"grpc": 9091},
		},
	}
	merged := mergeInto(mergeInto(map[string]any{}, base), late)
	sub, _ := merged["kaname"].(map[string]any)
	ports, _ := sub["ports"].(map[string]any)
	if lane, _ := ports["loginLane"].(int); lane != 9100 {
		t.Fatalf("PREMISE BROKEN: a later profile that never mentions `loginLane` cleared it. "+
			"Every stack answer in this file is derived from an additive merge; if the merge "+
			"replaces instead, those answers are about the last profile and not about the "+
			"release. got merged sub-tree: %#v", ports)
	}
	// The paired positive: an overlay that DOES speak still wins. Without this the
	// assertion above is satisfied just as well by a merge that ignores overlays
	// altogether, which would be a different and equally wrong gate.
	if got, _ := ports["grpc"].(int); got != 9091 {
		t.Fatalf("PREMISE BROKEN in the other direction: a later profile that DOES declare a key "+
			"did not win (grpc=%d, want %d). A merge that ignores overlays would satisfy the "+
			"silence check above while measuring nothing.", got, 9091)
	}
}

// TestLoginConsole_GitignoredStackMemberExclusionStillHasASubject — the exclusion
// this file relies on must expire by itself.
//
// The live fe3455 chain is invoked with one MORE `-f` than the table names; the
// extra one is out of the tree on purpose (per-cluster site credentials). That is
// a real limit on what the gate above can see, and an unstated limit is how a
// partial fold gets read as completeness. So the basis of the exclusion — the
// ignore rule — is checked here: if it goes away, the exclusion has lost its
// subject and this fails, which is the same discipline every other known-gap list
// in this repository is held to.
func TestLoginConsole_GitignoredStackMemberExclusionStillHasASubject(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(raw), gitignoredStackMember) {
		t.Fatalf("%s is no longer named in .gitignore. This file excludes it from the fold and "+
			"says so; with the ignore rule gone the exclusion has no subject — either fold the "+
			"profile in and delete this test, or restore the rule.", gitignoredStackMember)
	}
	stacks := deployableStacks(t)
	for _, name := range sortedStackNames(stacks) {
		for _, profile := range stacks[name] {
			if profile == gitignoredStackMember {
				t.Fatalf("%s now appears in stack %q. It is excluded precisely because it is not "+
					"in the tree; if it has become foldable, remove the exclusion instead of "+
					"carrying both.", gitignoredStackMember, name)
			}
		}
	}
	folded := len(deployableStacks(t)["fe3455"])
	t.Logf("declared limit: the live fe3455 chain carries %s as one more -f on top of the %d "+
		"the table names; it is outside the tree by design, so the fold above sees %d of its %d "+
		"members and does not call that completeness",
		gitignoredStackMember, folded, folded, folded+1)
}

// loginConsoleReadPaths — абсолютные пути слитого дерева, которые читает гейт.
var loginConsoleReadPaths = [][]string{lanePortPath, laneURLPath}

// umbrellaChartKeys — ключи значений подчартов зонта: имя (или алиас) каждой
// зависимости Chart.yaml и имя каждого чарта в charts/, который helm грузит и
// без объявления.
func umbrellaChartKeys(t *testing.T) map[string]bool {
	t.Helper()
	type chartFile struct {
		Name         string `yaml:"name"`
		Dependencies []struct {
			Name  string `yaml:"name"`
			Alias string `yaml:"alias"`
		} `yaml:"dependencies"`
	}
	read := func(path string) chartFile {
		t.Helper()
		var c chartFile
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s не читается: %v — ключей подчартов не знаю, судить нечем", path, err)
		}
		if err := yaml.Unmarshal(raw, &c); err != nil {
			t.Fatalf("%s не разобран: %v", path, err)
		}
		return c
	}
	keys := map[string]bool{}
	for _, dep := range read("Chart.yaml").Dependencies {
		key := dep.Name
		if dep.Alias != "" {
			key = dep.Alias
		}
		keys[key] = true
	}
	local, err := filepath.Glob(filepath.Join("charts", "*", "Chart.yaml"))
	if err != nil {
		t.Fatalf("обход charts/: %v", err)
	}
	for _, path := range local {
		keys[read(path).Name] = true
	}
	return keys
}

// pathsWithoutAChart — пути, чей первый ключ не называет подчарта зонта.
func pathsWithoutAChart(paths [][]string, keys map[string]bool) []string {
	var out []string
	for _, p := range paths {
		if len(p) == 0 || !keys[p[0]] {
			out = append(out, strings.Join(p, "."))
		}
	}
	sort.Strings(out)
	return out
}

// TestLoginConsole_EveryKeyItReadsBelongsToAChartOfTheUmbrella — ПРЕДПОСЫЛКА
// гейта: всякий путь, который он читает, начинается ключом подчарта, который
// зонт действительно несёт.
//
// Путь в снятый подчарт на КАЖДОМ стеке отвечает «ключа нет», и ветвь, которая
// его судит, исполняется беспредметно: отказ «не объявлено» становится
// свойством дерева, а не стека. Так ветвь посадки `external` пережила экран
// входа поставщика — подчарт снят с зонта (#1276), а гейт его ещё спрашивал.
func TestLoginConsole_EveryKeyItReadsBelongsToAChartOfTheUmbrella(t *testing.T) {
	keys := umbrellaChartKeys(t)
	if len(keys) == 0 {
		t.Fatal("ключей подчартов зонта ноль — обход Chart.yaml и charts/ прочитал ничто")
	}
	for _, p := range pathsWithoutAChart(loginConsoleReadPaths, keys) {
		t.Errorf("гейт читает %s, а подчарта %q в зонте нет — ветвь, судящая этот путь, "+
			"исполняется беспредметно на каждом стеке; снимите её вместе с подчартом",
			p, strings.SplitN(p, ".", 2)[0])
	}
	t.Logf("путей чтения %d · ключей подчартов зонта %d", len(loginConsoleReadPaths), len(keys))
}

// TestLoginConsole_PathWithoutAChartIsFound — предикат предпосылки падает на
// пути в отсутствующий подчарт и молчит на законном близнеце.
func TestLoginConsole_PathWithoutAChartIsFound(t *testing.T) {
	keys := map[string]bool{"kaname": true, "api-gateway": true}
	if got := pathsWithoutAChart([][]string{{"kaname", "ports", "loginLane"}, {"api-gateway", "authn"}}, keys); len(got) != 0 {
		t.Fatalf("законные пути названы находкой: %v", got)
	}
	got := pathsWithoutAChart([][]string{{"kaname", "ports"}, {"retired-screen", "enabled"}, {}}, keys)
	if strings.Join(got, " · ") != " · retired-screen.enabled" {
		t.Fatalf("путь в отсутствующий подчарт и пустой путь не названы оба: %q", got)
	}
}
