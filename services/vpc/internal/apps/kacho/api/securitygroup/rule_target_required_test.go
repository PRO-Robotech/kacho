// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

// Правило обязано нести РОВНО ОДНУ цель: ни нуля, ни двух.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ: аннотация контракта без энфорсера
//
// `oneof target` в контракте несёт `option (exactly_one) = true` — то есть контракт
// ОБЕЩАЕТ ровно одну цель. Проверки не существовало: `validateSGRule` разбирал
// направление, порты, протокол, описание, метки и формат каждого блока адресов — и ни
// одного утверждения о самой цели.
//
// Что из этого следовало, и почему это не косметика:
//
//   - правило БЕЗ цели принималось, сохранялось и возвращалось при чтении. Оно
//     описывает «разрешить трафик... куда?» — и по закрытой модели не разрешает
//     ничего, то есть вызывающий получил успех на правиле, которое не делает того,
//     что он написал. Модуль terraform документировал этот исход дословно:
//     «правило без цели край принимает МОЛЧА, сохраняет и отдаёт обратно без цели»;
//   - правило с ДВУМЯ целями принималось тоже, а на проводе `oneof` держит одну:
//     вторая молча терялась при обратном преобразовании. То есть сохранённое правило
//     отличалось от написанного, и узнать об этом можно было только чтением.
//
// ПОЧЕМУ ПРОВЕРКА ЗДЕСЬ, А НЕ В `oneof` НА ПРОВОДЕ. Край действительно не даст
// прислать две ветви одного `oneof` в JSON. Но use-case принимает `domain`-структуру,
// а не тело запроса: у неё поля цели ПЛОСКИЕ, и два непустых поля в ней —
// представимое состояние. Значит проверка обязана стоять там, где состояние
// представимо, а не там, где его запрещает форма передачи.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

func ingressBase() domain.SecurityGroupRule {
	return domain.SecurityGroupRule{
		Direction:      domain.SecurityGroupRuleDirectionIngress,
		FromPort:       -1,
		ToPort:         -1,
		ProtocolName:   "ANY",
		ProtocolNumber: -1,
	}
}

func TestRuleTargetIsExactlyOne(t *testing.T) {
	for _, tc := range []struct {
		имя     string
		правило func() domain.SecurityGroupRule
		отказ   bool
		поле    string
		почему  string
	}{
		{
			имя: "без цели — отказ",
			правило: func() domain.SecurityGroupRule {
				return ingressBase()
			},
			отказ: true,
			поле:  ".target",
			почему: "правило без цели описывает «разрешить трафик куда?» и по закрытой модели не " +
				"разрешает ничего — вызывающий получал успех на правиле, которое не делает того, " +
				"что он написал",
		},
		{
			имя: "две цели — отказ",
			правило: func() domain.SecurityGroupRule {
				r := ingressBase()
				r.V4CidrBlocks = []string{"10.0.0.0/8"}
				r.SecurityGroupID = "sgrpeer00000000000000"
				return r
			},
			отказ: true,
			поле:  ".target",
			почему: "на проводе `oneof` держит ОДНУ ветвь: вторая молча теряется при обратном " +
				"преобразовании, и сохранённое правило отличается от написанного",
		},
		{
			имя: "цель — блоки v4",
			правило: func() domain.SecurityGroupRule {
				r := ingressBase()
				r.V4CidrBlocks = []string{"10.0.0.0/8"}
				return r
			},
			почему: "положительный контроль: без него отказ зеленел бы на реализации, отвергающей ЛЮБОЕ правило",
		},
		{
			имя: "цель — блоки v6",
			правило: func() domain.SecurityGroupRule {
				r := ingressBase()
				r.V6CidrBlocks = []string{"fd00::/8"}
				return r
			},
			почему: "второе семейство — тоже законная цель, и оно проверяется отдельно: " +
				"проверка, считающая целью только v4, отвергала бы законное v6-правило",
		},
		{
			имя: "цель — блоки обоих семейств суть ОДНА цель",
			правило: func() domain.SecurityGroupRule {
				r := ingressBase()
				r.V4CidrBlocks = []string{"10.0.0.0/8"}
				r.V6CidrBlocks = []string{"fd00::/8"}
				return r
			},
			почему: "ветвь `cidr_blocks` несёт ОБА набора, поэтому «v4 и v6» — одна цель, а не две. " +
				"Проверка, считающая наборы по отдельности, отвергала бы законное dualstack-правило",
		},
		{
			имя: "цель — группа",
			правило: func() domain.SecurityGroupRule {
				r := ingressBase()
				r.SecurityGroupID = "sgrpeer00000000000000"
				return r
			},
			почему: "третий положительный контроль: цель-группа законна",
		},
	} {
		t.Run(tc.имя, func(t *testing.T) {
			err := validateSGRule("rule_specs[0]", tc.правило())
			if !tc.отказ {
				require.NoError(t, err, tc.почему)
				return
			}
			require.Error(t, err, tc.почему)
			assert.Equal(t, codes.InvalidArgument, status.Code(err),
				"это неверный ввод, а не состояние ресурса")
			assert.Equal(t, "rule_specs[0]"+tc.поле, fieldViolation(t, err),
				"отказ обязан называть поле: иначе вызывающий правит наугад")
		})
	}
}
