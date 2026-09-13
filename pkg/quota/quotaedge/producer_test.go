// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package quotaedge_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	corequota "github.com/PRO-Robotech/corelib/quota"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaedge"
)

const producerKnob = "quota.authority (KACHO_VPC_QUOTA__AUTHORITY)"

// TestAddressIsRefusedBecauseTheAuthorityHasNoProducer — адрес домена величин
// отвергается СТАРТОМ, а не принимается и не читается.
//
// Производителя у контракта авторитета величин нет ни в одном дереве: служба
// доступа сняла его целиком, платформа своего не завела. Ручка, продолжающая
// принимать адрес, была бы полем, на которое никто не смотрит — ровно тот класс,
// который `api-conventions.md` §«Принято-и-проигнорировано» запрещает; законных
// исходов там три, и выбран второй — отвергать явно.
func TestAddressIsRefusedBecauseTheAuthorityHasNoProducer(t *testing.T) {
	t.Parallel()

	err := quotaedge.ValidateAuthorityHasAProducer(producerKnob, "kaname-internal.kacho.svc:9091")
	require.Error(t, err, "адрес принят молча — ручка объявляет возможность, которой нет")

	msg := err.Error()
	require.Contains(t, msg, producerKnob,
		"отказ обязан НАЗВАТЬ ручку: без её имени стенд не поднять (`security.md` "+
			"§«Публичные артефакты», три выведенных из-под запрета места)")
	require.Contains(t, msg, corequota.NotDeployed,
		"отказ обязан назвать СЛЕДУЮЩИЙ ШАГ оператора — единственное оставшееся "+
			"значение ручки (ban #18: отказ, не восстанавливающий следующий шаг, — находка)")
	require.Contains(t, msg, "#2190",
		"отказ обязан назвать ПРЕДМЕТ, которым состояние снимается: развилка о судьбе "+
			"авторитета величин не решена, и отказ без её номера бессрочен")
}

// TestDeclaredAbsenceIsSilent — законный близнец. Без него отрицание выше
// зеленело бы на предикате, отвергающем всё подряд.
func TestDeclaredAbsenceIsSilent(t *testing.T) {
	t.Parallel()

	require.NoError(t, quotaedge.ValidateAuthorityHasAProducer(producerKnob, corequota.NotDeployed))
	require.NoError(t, quotaedge.ValidateAuthorityHasAProducer(producerKnob, "  "+corequota.NotDeployed+"  "),
		"обрамляющие пробелы читает разборщик объявления — предикат обязан судить то же значение")
}

// TestUnsetIsLeftToTheDeclarationParser — незаданное значение судит РАЗБОРЩИК
// объявления, а не этот предикат.
//
// Два места об одном предмете дали бы два разных текста на один вход, и
// оператор читал бы тот, который сработал первым.
func TestUnsetIsLeftToTheDeclarationParser(t *testing.T) {
	t.Parallel()

	require.NoError(t, quotaedge.ValidateAuthorityHasAProducer(producerKnob, ""))
	require.NoError(t, quotaedge.ValidateAuthorityHasAProducer(producerKnob, "   "))
}

// TestKnobNameIsNamedEvenWhenTheCallerForgotIt — отказ без имени ручки
// неисполним: оператор не знает, что править.
func TestKnobNameIsNamedEvenWhenTheCallerForgotIt(t *testing.T) {
	t.Parallel()

	err := quotaedge.ValidateAuthorityHasAProducer("", "kaname-internal.kacho.svc:9091")
	require.Error(t, err)
	require.NotEmpty(t, strings.TrimSpace(err.Error()))
	require.Contains(t, err.Error(), "quota.authority",
		"без имени ручки у вызывающего предикат обязан назвать каноническое")
}
