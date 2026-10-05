// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// kaname_mail_relay_production_start_test.go — КАЖДЫЙ БОЕВОЙ СТЕК ОТДАЁТ
// СЛУЖБЕ ДОСТУПА ПОЧТОВЫЙ УЗЕЛ И АДРЕС ОТПРАВИТЕЛЯ, А РЕНДЕР БЕЗ НИХ ОТКАЗЫВАЕТ
// (kacho#3020).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// С пина kaname, несущего kaname#475, почтовый узел — условие боевого старта
// службы доступа: в режиме production и production-strict процесс без
// `invite-mail.relay` и `invite-mail.from` в старте отказывает (строка таблицы
// требований полосы «почтовый узел объявлен»). Код подтверждения адреса и код
// восстановления доступа уходят только письмом, и без узла дальше входа не
// прошёл бы ни один человек.
//
// Отсюда два свойства, и оба судятся исходом рендера:
//
//   - каждый стек таблицы stacks.txt, чья служба доступа поднимается в боевом
//     режиме, отдаёт ей непустые `invite-mail.relay` и `invite-mail.from` —
//     стек, их не отдавший, поднял бы службу, которая откажет в старте;
//   - страж рендера (templates/identity-mail-lane-guard.yaml) отказывает,
//     когда в боевом стеке снят ОДИН из двух ключей, и называет снятый.
//     Близнец: тот же снятый ключ вне боевого режима рендеру не мешает —
//     там пустой узел законен, и процесс его так и читает.
//
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ. Что узел боевого профиля доставляет письма:
// боевой профиль объявляет его нерезолвимой заглушкой (RFC 2606), как и чарт
// самой службы, и заменяет её тот, кто ставит. Что процесс принял величины —
// вопрос поднятого стенда (rollout ready), а не рендера.
package deploy_test

import (
	"sort"
	"strings"
	"testing"
)

// kanameProductionModes — режимы, в которых служба требует почтовый узел
// (kaname internal/apps/kaname/config/mode.go, Mode.IsProduction).
var kanameProductionModes = map[string]bool{"production": true, "production-strict": true}

func kanameAuthnMode(cfg map[string]any) string {
	v, _ := lookup(cfg, "authn", "mode")
	return yamlScalar(v)
}

func TestEveryProductionStackHandsTheAccessServiceItsMailRelay(t *testing.T) {
	names := make([]string, 0)
	for n := range deployStacks(t) {
		names = append(names, n)
	}
	sort.Strings(names)
	var production, other int
	for _, n := range names {
		r := renderNamedStack(t, n)
		cfg, _ := r.serviceConfig(t)
		mode := kanameAuthnMode(cfg)
		if !kanameProductionModes[mode] {
			other++
			t.Logf("стек %s: режим службы %q — узел не обязателен", n, mode)
			continue
		}
		production++
		lane := inviteMailOf(cfg)
		from, _ := lane.block["from"].(string)
		t.Logf("стек %s (%s): режим %q, invite-mail.relay непуст: %v, invite-mail.from непуст: %v",
			n, strings.Join(r.chain, " + "), mode, strings.TrimSpace(lane.relay) != "", strings.TrimSpace(from) != "")
		if strings.TrimSpace(lane.relay) == "" {
			t.Errorf("стек %s: служба доступа поднимается в режиме %q без invite-mail.relay — "+
				"процесс откажет в старте (kaname#475)", n, mode)
		}
		if strings.TrimSpace(from) == "" {
			t.Errorf("стек %s: служба доступа поднимается в режиме %q без invite-mail.from — "+
				"процесс откажет в старте (kaname#475)", n, mode)
		}
	}
	t.Logf("стеков осмотрено %d: боевых %d, прочих %d", len(names), production, other)
	if production == 0 {
		t.Fatalf("ни одного боевого стека в таблице — предпосылка пробы исчезла, а не дерево стало чистым")
	}
}

func TestMailLaneGuardRefusesAProductionStackWithoutTheRelay(t *testing.T) {
	stacks := deployStacks(t)
	const refusal = "служба доступа поднимается в боевом режиме"
	clearLane := []string{
		"global.kacho.identity.smtp.connectionURI=",
		"global.kacho.identity.smtp.fromAddress=",
		"global.kacho.identity.smtp.fromName=",
	}
	cases := []struct {
		name, stack string
		sets        []string
		want        []string // пусто — рендер обязан пройти
	}{
		{"законный близнец: боевой профиль как есть", addrGateProdStack, nil, nil},
		{"законный близнец: стенд наборов как есть", addrGateStandStack, nil, nil},
		{"боевой профиль без узла и отправителя", addrGateProdStack, clearLane,
			[]string{refusal, "connectionURI"}},
		{"боевой профиль без отправителя", addrGateProdStack,
			[]string{"global.kacho.identity.smtp.fromAddress="},
			[]string{"fromAddress"}},
		{"законный близнец: та же полоса снята вне боевого режима", "dev",
			append([]string{"kaname.authMode=dev", "mailpit.enabled=false",
				"global.kacho.identity.smtp.trustAnchorSecret.name=",
				"global.kacho.identity.smtp.trustAnchorSecret.key="}, clearLane...), nil},
		{"та же полоса снята в боевом режиме", "dev",
			append([]string{"kaname.authMode=production-strict", "mailpit.enabled=false",
				"global.kacho.identity.smtp.trustAnchorSecret.name=",
				"global.kacho.identity.smtp.trustAnchorSecret.key="}, clearLane...),
			[]string{refusal, "production-strict"}},
	}
	for _, c := range cases {
		chain, ok := stacks[c.stack]
		if !ok {
			t.Fatalf("%s: стека %q в таблице нет", c.name, c.stack)
		}
		out, err := renderStack(t, chain, c.sets...)
		switch {
		case len(c.want) == 0 && err != nil:
			t.Errorf("%s: законный близнец отвергнут рендером: %v\n%s", c.name, err, lastLines(out, 5))
		case len(c.want) != 0 && err == nil:
			t.Errorf("%s: рендер прошёл, а страж обязан был отказать (ждали %q)", c.name, c.want)
		default:
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("%s: рендер отказал, но текст не называет %q:\n%s", c.name, w, lastLines(out, 5))
				}
			}
			if !t.Failed() {
				t.Logf("%s: исход как ждали", c.name)
			}
		}
	}
}
