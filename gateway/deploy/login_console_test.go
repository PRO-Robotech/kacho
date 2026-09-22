// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
//   - token_shape_test.go — "does this profile declare its own", which is right
//     for ITS question, because `dev` and `prod` there are alternatives rather
//     than layers, and it says so itself.
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

// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ИМЕННО ЧЕЛОВЕК ВХОДИТ — РЕШАЕТ ПОСАДКА, А НЕ АДРЕС, ВЫПИСАННЫЙ ЗДЕСЬ
//
// Вопрос у гейта один и он не менялся: «может ли человек на этом стенде пройти
// церемонию и оказаться с предъявителем, который край примет?» Адрес ответа —
// менялся.
//
// Прежняя редакция отождествляла церемонию с интерфейсом входа ЧУЖОГО
// поставщика (`kratos-selfservice-ui`). На посадке `external` это верно. На
// посадке `own` — неверно by construction: полоса формы входа — предмет
// ПОСАДКИ, и под `own` четыре глагола формы край ретранслирует на слушатель
// полосы службы (gateway/cmd/api-gateway/main.go, ветка
// `identityLane == identityposture.Own`; держит
// TestOwnLane_F3_45_TheLoginLaneRelayIsWiredUnderTheOwnPostureOnly).
//
// Чьё печенье край при этом ЧИТАЕТ, посадка не решает: читателей носителя
// заводит МНОЖЕСТВО (config.ResolvedSessionCarriers — объявленное ручкой
// `KACHO_API_GATEWAY_SESSION_CARRIERS` либо, пока ручка не задана, выведенное
// из посадки), а пару с посадкой сверяет страж старта
// (validateSessionCarrierConfig, session_carrier_validation.go). Под `own`
// законны два состояния множества — `own` и `own,external`, — и во втором
// чужое печенье ЧИТАЕТСЯ: это переходное окно. Вопрос гейта от этого не
// зависит: под `own` он спрашивает обе половины собственной полосы, читается ли
// ещё чужое печенье или нет.
//
// Значит стек на `own`, выключивший чужой интерфейс, прежняя редакция объявляла
// находкой, называя при этом ВЕРНУЮ причину о НЕВЕРНОМ предмете: «интерфейс входа
// ВЫКЛЮЧИЛИ» — тогда как консоль этой посадки стоит в
// другом месте и включена. Требование пережило бы свой предмет и было бы снято
// не по предикату, а как непонятное.
//
// ПОСЛАБЛЕНИЕМ ЭТО НЕ ЯВЛЯЕТСЯ: отказ остаётся на КАЖДОМ боевом стеке, на обеих
// посадках. Меняется адрес, по которому гейт спрашивает, а не наличие спроса.
// На `own` спрашиваются ОБЕ половины полосы — слушатель службы и адрес
// ретрансляции края, — потому что половина полосы даёт стенд, где форма
// отвечает 503 на каждом запросе, неотличимо от «служба лежит».
//
// ЧЕГО ГЕЙТ НЕ СУДИТ. Включённый чужой интерфейс на посадке `own` находкой не
// объявляется, и причина не в том, что чужое печенье там не читается: читается
// ли оно, решает множество носителей, а не посадка, и при `own,external` оно
// читается. Причина в том, что предмет этого гейта ОДИН, «есть ли чем войти», и
// на `own` на него отвечает собственная полоса. Снятие чужого интерфейса с
// посадки `own` — предмет переписи чужих служб, а не этого файла.

// gitignoredStackMember — ПОСЛЕДНИЙ `-f` живой цепочки fe3455, намеренно ВНЕ
// дерева: он несёт учётные данные площадки. Его отсутствие находкой НЕ является,
// и гейт говорит об этом вслух, вместо того чтобы тихо выдать частичное слитие
// за полноту.
//
// Число ВЫВОДИТСЯ и никогда не пересказывается: прежняя редакция писала прозой
// «3 из 4», цепочка затем получила отслеживаемый слой посадки, и проза
// продолжала утверждать прежнюю арифметику, пока слитие под ней менялось.
const gitignoredStackMember = "values.fe3455-ory.yaml"

// consolePath — где интерфейс входа ЧУЖОГО поставщика объявляет себя в
// значениях зонта. Подчарт — `kratos-selfservice-ui`, его собственные значения
// лежат под вложенным ключом `kratosSelfServiceUI`.
var consolePath = []string{"kratos-selfservice-ui", "kratosSelfServiceUI", "enabled"}

// lanePortPath / laneURLPath — две половины СОБСТВЕННОЙ полосы формы: порт, на
// котором служба поднимает слушатель, и адрес, на который край ретранслирует
// глаголы. Обе — абсолютные пути в слитом дереве зонта.
var (
	lanePortPath = []string{"kaname", "ports", "loginLane"}
	laneURLPath  = []string{"api-gateway", "authn", "iamLoginLaneUrl"}
)

// posturePathService / posturePathEdge — где посадка объявляется каждой
// половиной. Читается сначала служба, затем край: СОГЛАСИЕ половин — предмет
// deploy/helm/umbrella/identity_posture_profiles_test.go, и второго суждения о
// нём здесь не заводится.
var (
	posturePathService = []string{"kaname", "config", "authn", "identityProvider"}
	posturePathEdge    = []string{"api-gateway", "authn", "identityProvider"}
)

// postureExternal — значение, которое получает цепочка, посадку не объявившая.
// Умолчание живёт в базовом профиле чарта службы
// (`deploy/helm/umbrella/charts/kaname/values.yaml`), поэтому молчание цепочки
// есть `external`, а не «не решено»: гейт обязан спрашивать с неё чужой
// интерфейс ровно так же, как с объявившей.
const (
	postureExternal = "external"
	postureOwn      = "own"
)

// loginConsoleFacts — что ОДНА цепочка объявила о том, чем на ней входит человек.
type loginConsoleFacts struct {
	Stack string
	// Posture — посадка личности; "" означает «цепочка молчит», и это НЕ третье
	// состояние: молчание разрешается в `external` умолчанием чарта. Поле
	// хранится сырым, чтобы перепись могла отличить объявивших от наследующих.
	Posture string
	// ForeignUI / ForeignUIDeclared — `enabled: false` и «ключа нет вовсе» суть
	// ПРОТИВОПОЛОЖНЫЕ находки («интерфейс выключили» против «его никто не
	// провязывал»), и гейт, их схлопнувший, называет неверную причину.
	ForeignUI         bool
	ForeignUIDeclared bool
	// LanePort / LaneURL — половины собственной полосы формы.
	LanePort string
	LaneURL  string
}

// loginConsoleCensus — ОБЪЁМ ОСМОТРЕННОГО. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного», и от «боевых стеков не нашлось» тоже.
type loginConsoleCensus struct {
	Stacks     int
	Production int
	OnExternal int
	OnOwn      int
	Inherited  int
}

func (c loginConsoleCensus) String() string {
	return fmt.Sprintf("стеков в таблице %d · боевых %d · из них на чужой посадке %d "+
		"(из них посадку НЕ объявляют, а наследуют умолчание чарта %d) · на собственной %d",
		c.Stacks, c.Production, c.OnExternal, c.Inherited, c.OnOwn)
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
		if posture == "" {
			census.Inherited++
			posture = postureExternal
		}
		switch posture {
		case postureExternal:
			census.OnExternal++
			switch {
			case !f.ForeignUIDeclared:
				findings = append(findings, fmt.Sprintf(
					"%s: посадка %q, а слитый стек НЕ объявляет %s. Боевой стенд без интерфейса "+
						"входа не проводит церемонию, которую человек завершает предъявителем, и "+
						"всякая проверка про человека на нём утверждает про никого",
					f.Stack, posture, strings.Join(consolePath, ".")))
			case !f.ForeignUI:
				findings = append(findings, fmt.Sprintf(
					"%s: посадка %q, а слитый стек разрешает %s в false. Интерфейс входа ВЫКЛЮЧИЛИ — "+
						"это объявленное значение, а не пропуск, поэтому оно отвергается здесь, а не "+
						"подставляется умолчанием",
					f.Stack, posture, strings.Join(consolePath, ".")))
			}
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
// Соседи `resolveStackAt` и `resolveStackBoolAt` требуют конкретного типа и
// отдают («», false) на числе; порт полосы — число, и строковый читатель молча
// объявил бы его необъявленным. Отдельный читатель, а не правка соседей: у них
// свой предмет, и смена их типа поменяла бы вердикт у чужих проверок.
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
	for name, stack := range stacks {
		if !stackIsProductionClass(t, stack) {
			t.Logf("%s: dev-класс по собственному объявлению — пропущен (%d профиль(ей))", name, len(stack))
			continue
		}
		t.Logf("%s: боевой класс, слитие %d профиль(ей): %s",
			name, len(stack), strings.Join(stack, " -> "))

		posture := resolveStackScalarAt(t, stack, posturePathService...)
		if posture == "" {
			posture = resolveStackScalarAt(t, stack, posturePathEdge...)
		}
		enabled, declared := resolveStackBoolAt(t, stack, consolePath...)
		facts = append(facts, loginConsoleFacts{
			Stack:             name,
			Posture:           posture,
			ForeignUI:         enabled,
			ForeignUIDeclared: declared,
			LanePort:          resolveStackScalarAt(t, stack, lanePortPath...),
			LaneURL:           resolveStackScalarAt(t, stack, laneURLPath...),
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
		// Чужой интерфейс ВЫКЛЮЧЕН — и молчание намеренное: под `own` гейт
		// спрашивает обе половины собственной полосы, а читается ли ещё чужое
		// печенье (это решает множество носителей, а не посадка), на его
		// вопрос не влияет.
		ForeignUI: false, ForeignUIDeclared: true,
	}})
	if len(findings) != 0 {
		t.Fatalf("полная собственная полоса объявлена находкой: %v", findings)
	}
	if census.OnOwn != 1 {
		t.Fatalf("перепись не отнесла стек к собственной посадке: %s", census)
	}
}

// TestLoginConsoleGate_Injection_TheForeignLaneStillRefuses — ПОЛОЖИТЕЛЬНЫЙ
// КОНТРОЛЬ прежнего требования: на посадке `external` выключенный и
// необъявленный интерфейс остаются ДВУМЯ РАЗНЫМИ находками.
//
// Посадка здесь подаётся и объявленной, и ПУСТОЙ: пустая означает наследование
// умолчания чарта, и стек, посадку не объявивший, обязан спрашиваться так же.
func TestLoginConsoleGate_Injection_TheForeignLaneStillRefuses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts loginConsoleFacts
		want  string
	}{
		{"выключен явно", loginConsoleFacts{
			Stack: "synthetic", Posture: postureExternal, ForeignUI: false, ForeignUIDeclared: true,
		}, "ВЫКЛЮЧИЛИ"},
		{"не объявлен вовсе", loginConsoleFacts{
			Stack: "synthetic", Posture: postureExternal,
		}, "НЕ объявляет"},
		{"посадка унаследована, интерфейс выключен", loginConsoleFacts{
			Stack: "synthetic", Posture: "", ForeignUI: false, ForeignUIDeclared: true,
		}, "ВЫКЛЮЧИЛИ"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings, _ := judgeLoginConsole([]loginConsoleFacts{tc.facts})
			if len(findings) != 1 {
				t.Fatalf("находок %d, ожидалась ровно одна: %v", len(findings), findings)
			}
			if !strings.Contains(findings[0], tc.want) {
				t.Fatalf("находка не называет %q: %s", tc.want, findings[0])
			}
		})
	}
}

// TestLoginConsoleGate_Twin_AnEnabledForeignConsoleIsSilent — законный близнец
// к предыдущей инъекции.
func TestLoginConsoleGate_Twin_AnEnabledForeignConsoleIsSilent(t *testing.T) {
	findings, census := judgeLoginConsole([]loginConsoleFacts{{
		Stack: "synthetic", Posture: "", ForeignUI: true, ForeignUIDeclared: true,
	}})
	if len(findings) != 0 {
		t.Fatalf("включённый чужой интерфейс объявлен находкой: %v", findings)
	}
	if census.Inherited != 1 || census.OnExternal != 1 {
		t.Fatalf("перепись не отнесла молчащий стек к наследующим чужую посадку: %s", census)
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
		"kratos-selfservice-ui": map[string]any{
			"kratosSelfServiceUI": map[string]any{"enabled": true, "image": "base"},
		},
	}
	// A late overlay that touches a NEIGHBOURING key and says nothing about
	// `enabled` — the shape values.fe3455-prod.yaml actually has.
	late := map[string]any{
		"kratos-selfservice-ui": map[string]any{
			"kratosSelfServiceUI": map[string]any{"image": "late"},
		},
	}
	merged := mergeInto(mergeInto(map[string]any{}, base), late)
	sub, _ := merged["kratos-selfservice-ui"].(map[string]any)
	ui, _ := sub["kratosSelfServiceUI"].(map[string]any)
	if enabled, _ := ui["enabled"].(bool); !enabled {
		t.Fatalf("PREMISE BROKEN: a later profile that never mentions `enabled` cleared it. "+
			"Every stack answer in this file is derived from an additive merge; if the merge "+
			"replaces instead, those answers are about the last profile and not about the "+
			"release. got merged sub-tree: %#v", ui)
	}
	// The paired positive: an overlay that DOES speak still wins. Without this the
	// assertion above is satisfied just as well by a merge that ignores overlays
	// altogether, which would be a different and equally wrong gate.
	if got, _ := ui["image"].(string); got != "late" {
		t.Fatalf("PREMISE BROKEN in the other direction: a later profile that DOES declare a key "+
			"did not win (image=%q, want %q). A merge that ignores overlays would satisfy the "+
			"silence check above while measuring nothing.", got, "late")
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
	raw, err := os.ReadFile(filepath.Join("..", "..", ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(raw), gitignoredStackMember) {
		t.Fatalf("%s is no longer named in .gitignore. This file excludes it from the fold and "+
			"says so; with the ignore rule gone the exclusion has no subject — either fold the "+
			"profile in and delete this test, or restore the rule.", gitignoredStackMember)
	}
	for name, stack := range deployableStacks(t) {
		for _, profile := range stack {
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
