// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// auth_basic_stepup_internal_test.go — отображение уровня третьей полосы на ось
// каталога и умолчания, которые обязаны быть закрытыми (#1215).
//
// Внутренняя проба, а не через край: предмет — ДВА умолчания, до которых с
// поверхности не дотянуться, потому что сегодня их никто не нарушает. Умолчание,
// у которого нет пробы, меняют не заметив.

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

func TestAcrFromCredentialLevel_OffAxisIsRefused_OnAxisPasses(t *testing.T) {
	onAxis := []string{"0", "1", "2", "3"}
	offAxis := []string{"", " ", "aal1", "AAL2", "high", "4", "-1", "1.0", "один"}

	recognized := 0
	for _, v := range onAxis {
		acr, ok := acrFromCredentialLevel(v)
		require.True(t, ok, "положительный контроль: %q стоит на оси каталога", v)
		require.Equal(t, v, acr)
		require.Equal(t, grpcsrv.ACRRank(v), grpcsrv.ACRRank(acr),
			"отображение обязано сохранять ранг общей лестницы, а не заводить свою")
		recognized++
	}
	refused := 0
	for _, v := range offAxis {
		acr, ok := acrFromCredentialLevel(v)
		assert.False(t, ok, "величина %q оси не принадлежит и распознанной быть не может", v)
		assert.Equal(t, "", acr, "нераспознанное обязано приезжать пустым — общее правило "+
			"ранжирует пустое нулём, то есть отказ, а не проход")
		assert.Equal(t, 0, grpcsrv.ACRRank(acr))
		refused++
	}
	t.Logf("перепись: величин на оси %d (все распознаны) · вне оси %d (все отвергнуты)",
		recognized, refused)

	// Косметика не решает: обрамляющие пробелы у величины НА оси снимаются.
	acr, ok := acrFromCredentialLevel(" 2 ")
	assert.True(t, ok, "отвергать по написанию значило бы отвергать по косметике")
	assert.Equal(t, "2", acr)
}

// Величина полосы, съехавшая с оси, обязана давать ПУСТОЙ уровень (fail-closed)
// и ГРОМКИЙ доклад. Тихое состояние здесь жило бы вечно: обычная работа
// арендатора отвалилась бы вся разом, а причина осталась бы незаписанной.
func TestBasicCredentialAssurance_OffAxisLevelFailsClosedAndIsLoud(t *testing.T) {
	var buf bytes.Buffer
	a := &AuthInterceptor{
		logger:                slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		basicAssuranceUnknown: newIntrospectionFailureReporter(0, nil),
	}

	// Положительный контроль: величина на оси проезжает как есть и молча.
	good := a.basicCredentialAssurance(BasicVerifiedCredential{
		PrincipalType: "user", AuthenticationLevel: BasicCredentialLevel}, "/vpc/v1/networks")
	require.Equal(t, BasicCredentialLevel, good.ACR,
		"положительный контроль: объявленный уровень полосы обязан стоять на оси каталога")
	require.Equal(t, "user", good.PrincipalType,
		"тип принципала передаётся ТОТ, что назвал авторитет: освобождение машины — "+
			"арм общего правила, а не решение полосы")
	require.NotContains(t, buf.String(), "does not know",
		"положительный контроль: на величине с оси доклада быть не должно")

	// Отрицание: величина вне оси.
	bad := a.basicCredentialAssurance(BasicVerifiedCredential{
		PrincipalType: "user", AuthenticationLevel: "aal1"}, "/iam/v1/users/usr-abc/tokens")
	assert.Equal(t, "", bad.ACR, "съехавшая с оси величина обязана приезжать пустой")
	assert.Equal(t, 0, grpcsrv.ACRRank(bad.ACR),
		"пустое ранжируется нулём: положительного пола не удовлетворяет")
	assert.Contains(t, buf.String(), "basic_assurance_unknown_total",
		"состояние обязано быть громким и со счётчиком — иначе оно не исчезает и не видно")
	assert.Contains(t, buf.String(), `"lane":"basic"`,
		"доклад обязан называть полосу: журнал, не отвечающий «какая полоса», и есть то, "+
			"чем #1201 был найден")
}

// Умолчание выдачи вызова RFC 9470 — ЗАКРЫТОЕ. Полоса, не названная здесь,
// вызова не получает: он подтверждал бы годность предъявленного и советовал бы
// невозможное. Проба пинит именно УМОЛЧАНИЕ — до него с поверхности края не
// дотянуться, потому что сегодня безымянных полос нет.
func TestStepUpCeremonyReachable_DefaultsClosed(t *testing.T) {
	reachable, unreachable := 0, 0
	for _, lane := range []string{stepUpLaneSession, stepUpLaneBearer} {
		assert.True(t, stepUpCeremonyReachable(lane),
			"положительный контроль: у полосы %q церемония существует, и вызов ей положен", lane)
		reachable++
	}
	for _, lane := range []string{stepUpLaneBasic, "", "полоса-которой-ещё-нет"} {
		assert.False(t, stepUpCeremonyReachable(lane),
			"полоса %q вызова получать не вправе", lane)
		unreachable++
	}
	t.Logf("перепись: полос с достижимой церемонией %d · без неё %d (включая безымянную — "+
		"умолчание закрытое)", reachable, unreachable)
}
