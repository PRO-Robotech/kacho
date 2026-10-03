// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// relay_target_form_consumer_test.go — решение kacho#2849 о слушателях полосы
// формы держится за СВОЕГО ПОТРЕБИТЕЛЯ и истекает вместе с ним.
//
// Глаголы формы отвечают и на граничном admin-REST слушателе края
// (`RelayTargetForm.ExternalListenersOnly() == false`), потому что у них там
// есть потребитель: операторская консоль посадки Б
// (docs/architecture/admin-api-door-on-external-stand.md). У профиля консоли
// ОДИН восходящий узел края — `host.upstreams.apiGateway`, — и через него же
// раздача шлёт и admin-плоскость, и `/iam/v1/` (вход, признак формы, второй
// фактор, повышение уровня). Операторская консоль смотрит этим узлом на
// внутренний слушатель; сними с него глаголы формы — оператор не войдёт.
//
// Потребитель исчезает, когда у консоли появляется СВОЯ дверь к admin-плоскости
// (непустой `adminPlane.upstream`, предикат kacho#2694 п. 1; умолчание пусто —
// двери нет): операторская раздача шлёт на внутренний слушатель выведенный
// перечень admin-путей, а всё остальное — на внешний, как тенантская. С этой
// минуты исключение формы — поверхность без
// предмета, и гейт краснеет, называя, что снять. Обратное направление держится
// так же: глаголы формы, снятые с внутреннего слушателя без двери, — находка.
//
// Предмет судится разбором профиля консоли (узлы YAML), а не поиском слова:
// ключ двери опознаётся на любой глубине, комментарий его не изображает, а
// дверью он становится только с непустым скалярным `upstream` — сам ключ с
// пустым умолчанием дверью не считается.
package middleware_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// consoleProfileRel — профиль консоли: единственное место, где объявляется,
// куда смотрит её раздача.
var consoleProfileRel = filepath.Join("ui-future", "deploy", "values.yaml")

// consoleDoorKey — ключ двери оператора к admin-плоскости (kacho#2694 п. 1).
const consoleDoorKey = "adminPlane"

// consoleDoorUpstreamKey — поле ключа двери, чьё непустое значение и есть
// дверь: kacho#2694 п. 1 объявляет умолчание пустым, и пустое — «двери нет».
const consoleDoorUpstreamKey = "upstream"

// consoleDoorCensus — что разбор профиля консоли установил.
type consoleDoorCensus struct {
	// keys — сколько ключей обойдено: «ноль находок» отличимо от «ноль
	// прочитанного».
	keys int
	// edgeUpstream — объявлен ли единственный восходящий узел края
	// `host.upstreams.apiGateway`.
	edgeUpstream bool
	// doors — пути дверей (`…adminPlane.upstream` с непустым скаляром), где бы
	// они ни стояли.
	doors []string
	// closedDoors — сколько ключей двери объявлено без двери: upstream пуст,
	// null, не объявлен, либо ключ не отображение.
	closedDoors int
}

// doorUpstreamOpen — несёт ли значение ключа двери непустой скалярный
// `upstream`. Пустая строка, null в любом написании, отсутствие поля, пустое
// отображение и скаляр вместо отображения — двери нет (kacho#2694 п. 1).
func doorUpstreamOpen(n *yaml.Node) bool {
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if key.Value == consoleDoorUpstreamKey {
			return val.Kind == yaml.ScalarNode && val.ShortTag() != "!!null" && val.Value != ""
		}
	}
	return false
}

// censusConsoleProfile обходит узлы профиля: каждый ключ считается, ключ двери
// с непустым upstream запоминается путём, без него — считается закрытым, узел
// края — по точному пути `host.upstreams.apiGateway` с непустым значением.
func censusConsoleProfile(raw []byte) (consoleDoorCensus, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return consoleDoorCensus{}, err
	}
	var c consoleDoorCensus
	var walk func(n *yaml.Node, path []string)
	walk = func(n *yaml.Node, path []string) {
		switch n.Kind {
		case yaml.DocumentNode, yaml.SequenceNode:
			for _, child := range n.Content {
				walk(child, path)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				key, val := n.Content[i], n.Content[i+1]
				at := append(append([]string{}, path...), key.Value)
				c.keys++
				if key.Value == consoleDoorKey {
					if doorUpstreamOpen(val) {
						c.doors = append(c.doors, strings.Join(at, ".")+"."+consoleDoorUpstreamKey)
					} else {
						c.closedDoors++
					}
				}
				if strings.Join(at, ".") == "host.upstreams.apiGateway" && val.Kind == yaml.ScalarNode && val.Value != "" {
					c.edgeUpstream = true
				}
				walk(val, at)
			}
		}
	}
	walk(&doc, nil)
	return c, nil
}

// judgeFormListenerDecision сверяет решение о глаголах формы с потребителем:
// двери нет — глаголы формы обязаны отвечать и на внутреннем слушателе; дверь
// есть — только на внешних.
func judgeFormListenerDecision(c consoleDoorCensus, formExternalOnly bool, where string) []string {
	if c.keys == 0 {
		return []string{where + ": в профиле консоли обойдено ноль ключей — есть ли у глаголов формы потребитель на внутреннем слушателе, разбор не устанавливает"}
	}
	if !c.edgeUpstream {
		return []string{where + ": нет host.upstreams.apiGateway — предпосылка исчезла: это единственный узел края, через который " +
			"операторская консоль посадки Б шлёт и admin-плоскость, и полосу формы; есть ли у исключения формы потребитель, разбор больше не устанавливает (kacho#2849)"}
	}
	door := len(c.doors) > 0
	switch {
	case door && !formExternalOnly:
		return []string{where + ": у консоли есть своя дверь к admin-плоскости (" + strings.Join(c.doors, ", ") + ") — операторская " +
			"раздача шлёт полосу формы на внешний слушатель, и у глаголов формы на internal-rest потребителя больше нет: " +
			"RelayTargetForm.ExternalListenersOnly → true, как у выдачи (kacho#2849)"}
	case !door && formExternalOnly:
		return []string{where + ": глаголы формы сняты с внутреннего слушателя, а двери оператора (непустого " + consoleDoorKey + "." + consoleDoorUpstreamKey + ") у консоли нет — " +
			"операторская консоль посадки Б смотрит host.upstreams.apiGateway на internal-rest и теряет вход (kacho#2849, дверь — kacho#2694)"}
	}
	return nil
}

func TestRelayTargetForm_L13_InternalListenerAnswerFollowsItsConsumer(t *testing.T) {
	path := filepath.Join(repoRoot(t), consoleProfileRel)
	raw, err := os.ReadFile(path) // #nosec G304 -- путь собран из корня этого дерева
	require.NoError(t, err)
	c, err := censusConsoleProfile(raw)
	require.NoError(t, err)
	require.Positive(t, c.keys, "%s: не обойдено ни одного ключа — молчание гейта ничего не значило бы", consoleProfileRel)
	require.True(t, c.edgeUpstream, "%s: нет host.upstreams.apiGateway — предпосылка гейта исчезла", consoleProfileRel)
	formExternalOnly := middleware.RelayTargetForm.ExternalListenersOnly()
	findings := judgeFormListenerDecision(c, formExternalOnly, consoleProfileRel)
	for _, f := range findings {
		t.Error(f)
	}
	t.Logf("перепись: %s — ключей %d · узел края объявлен %v · дверей оператора %d · ключей двери без upstream %d · глаголы формы только на внешних слушателях %v · находок %d",
		consoleProfileRel, c.keys, c.edgeUpstream, len(c.doors), c.closedDoors, formExternalOnly, len(findings))
}

// Инъекция настоящими формами профиля — синтетика, а не живая запись: гейт
// обязан краснеть в обе стороны и молчать на двух законных близнецах.
func TestRelayTargetForm_L13_Injection_DecisionAndConsumerMustAgree(t *testing.T) {
	const noDoor = "host:\n  upstreams:\n    apiGateway: api-gateway.kacho.svc.cluster.local:8080\n    dashboard: \"\"\n"
	const topDoor = noDoor + "adminPlane:\n  upstream: api-gateway.kacho.svc.cluster.local:8081\n"
	const nestedDoor = "host:\n  adminPlane:\n    upstream: api-gateway.kacho.svc.cluster.local:8081\n  upstreams:\n    apiGateway: api-gateway.kacho.svc.cluster.local:8080\n"
	const doorInComment = noDoor + "# adminPlane: upstream появится с kacho#2694\n"
	const noEdge = "host:\n  upstreams:\n    dashboard: \"\"\n"

	for _, tc := range []struct {
		name             string
		profile          string
		formExternalOnly bool
		findings         int
		mentions         []string
	}{
		{"дверь есть, форма на внутреннем — исключение без предмета", topDoor, false, 1, []string{"adminPlane", "ExternalListenersOnly", "kacho#2849"}},
		{"дверь вложена, форма на внутреннем — та же находка", nestedDoor, false, 1, []string{"host.adminPlane", "ExternalListenersOnly"}},
		{"двери нет, форма снята — оператор не войдёт", noDoor, true, 1, []string{"операторская консоль", "host.upstreams.apiGateway", "kacho#2694"}},
		{"узла края нет — предпосылка исчезла", noEdge, false, 1, []string{"host.upstreams.apiGateway", "предпосылка"}},
		{"пустой профиль — прочитано ноль", "", false, 1, []string{"ноль"}},
		{"законный близнец: двери нет, форма на внутреннем", noDoor, false, 0, nil},
		{"законный близнец: дверь в комментарии — не дверь", doorInComment, false, 0, nil},
		{"законный близнец: дверь есть, форма снята", topDoor, true, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := censusConsoleProfile([]byte(tc.profile))
			require.NoError(t, err)
			got := judgeFormListenerDecision(c, tc.formExternalOnly, "values.yaml")
			require.Len(t, got, tc.findings, "находки: %v", got)
			joined := strings.Join(got, "\n")
			for _, m := range tc.mentions {
				require.Contains(t, joined, m, "находка обязана назвать %q", m)
			}
		})
	}
}

// Умолчание kacho#2694 п. 1 — `adminPlane.upstream` пуст, и это «двери нет».
// Ключ `adminPlane` в профиле появится именно с пустым умолчанием; гейт,
// считающий дверью сам ключ, потребует снять глаголы формы с внутреннего
// слушателя на посадке, где оператор входит только через него. Каждая
// законная форма пустого значения прогоняется в обе стороны: форма на
// внутреннем слушателе — молчание, форма снята — находка «оператор не войдёт».
// Против пары topDoor каждая строка меняет ровно один факт: значение upstream.
func TestRelayTargetForm_L13_Injection_EmptyDoorDefaultIsNoDoor(t *testing.T) {
	const edge = "host:\n  upstreams:\n    apiGateway: api-gateway.kacho.svc.cluster.local:8080\n"
	for _, tc := range []struct{ name, profile string }{
		{"upstream пустой строкой", edge + "adminPlane:\n  upstream: \"\"\n"},
		{"upstream без значения", edge + "adminPlane:\n  upstream:\n"},
		{"upstream тильдой", edge + "adminPlane:\n  upstream: ~\n"},
		{"upstream словом null", edge + "adminPlane:\n  upstream: null\n"},
		{"upstream не объявлен, есть только entrance", edge + "adminPlane:\n  entrance:\n    allowSourceRanges: []\n"},
		{"adminPlane пустым отображением", edge + "adminPlane: {}\n"},
		{"adminPlane пустым скаляром", edge + "adminPlane:\n"},
		{"вложенный adminPlane с пустым upstream", "host:\n  adminPlane:\n    upstream: \"\"\n  upstreams:\n    apiGateway: api-gateway.kacho.svc.cluster.local:8080\n"},
	} {
		t.Run("законный близнец: "+tc.name+", форма на внутреннем", func(t *testing.T) {
			c, err := censusConsoleProfile([]byte(tc.profile))
			require.NoError(t, err)
			got := judgeFormListenerDecision(c, false, "values.yaml")
			require.Empty(t, got, "пустое умолчание kacho#2694 п. 1 — не дверь; находки: %v", got)
		})
		t.Run("инъекция: "+tc.name+", форма снята", func(t *testing.T) {
			c, err := censusConsoleProfile([]byte(tc.profile))
			require.NoError(t, err)
			got := judgeFormListenerDecision(c, true, "values.yaml")
			require.Len(t, got, 1, "находки: %v", got)
			for _, m := range []string{"операторская консоль", "host.upstreams.apiGateway", "kacho#2694"} {
				require.Contains(t, got[0], m, "находка обязана назвать %q", m)
			}
		})
	}
}
