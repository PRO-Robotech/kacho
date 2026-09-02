// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// recipient_conditioned_test.go — оси, которые чтение канона обязано знать, но
// СВОЙ разборщик не знал (#1891, #1936).
//
// # Предмет: судья типа получателя читал канон ВТОРЫМ разборщиком
//
// Вопрос «принимает ли это отношение членство группы» решает, исполним ли путь
// §3.5 приёмки-основания («получатель — группа, служебная запись вступает в
// неё») для двух живых выдач, стоящих ВНЕ ВЕРДИКТА сверки посева. На этом
// ответе держится и граница #1891, и объём #1936.
//
// Отвечал на него собственный построчный разбор канона, и его шапка объясняла
// это тем, что «разборщика `.fga` в дереве нет ни одного». Утверждение было
// НЕВЕРНО в день, когда оно писалось: `internal/authzplan` разбирает канон
// целиком, и на него уже опираются четыре пакета iam, один из них прод
// (`services/iam/internal/authzmodel`). То есть второе место об одном предмете
// было заведено ровно тем доводом, которым его следовало не заводить.
//
// # Расхождение НЕ гипотетическое — оно на форме, которую канон уже пишет
//
// Прямая запись userset допускает условие: `[user with mfa_fresh]`. В каноне
// такие записи есть. Свой разбор сравнивал запись со СТРОКОЙ `group#member`,
// поэтому `group#member with <условие>` он читал как «членство группы НЕ
// принимается» — то есть отвечал «предпосылка жива» там, где она умерла.
//
// Направление ошибки — худшее из двух: судья остаётся ЗЕЛЁНЫМ. Проба
// предпосылки (`recipient_test.go`) утверждает `require.False(AdmitsGroupMember)`
// и обещает в собственной шапке, что «станет модель принимать членство группы —
// проба скажет об этом сама, а не переживёт свой предмет». С условием на записи
// она бы его пережила, ничем себя не выдав.
//
// # Почему это оси, а не одна проба
//
// Каждая ось подаётся вместе с законным близнецом: запись с условием, которая
// членство группы НЕ несёт, обязана по-прежнему отвечать «не принимает».
// Односторонняя проба зеленела бы на чтении, отвечающем «принимает» на всё.
package moduleseedparity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/iam/internal/moduleseedparity"
)

// conditionedModel — канон, ЗАКОННЫЙ по форме и по связности: каждый названный
// тип субъекта объявлен. Прежний синтетический канон соседней пробы этого не
// соблюдал (`service_account` и `group` в нём не объявлены), и свой разбор
// отвечал по нему уверенно — то есть судил вход, каноном не являющийся.
const conditionedModel = `model
  schema 1.1

type user

type service_account

type group
    relations
    define member: [user, service_account]

type cluster
    relations
    define with_conditioned_group: [service_account, group#member with mfa_fresh]
    define with_conditioned_account: [user, service_account with mfa_fresh]
    define with_group_wildcard: [user, group:*]

condition mfa_fresh(now: timestamp, mfa_at: timestamp) {
  now - mfa_at < duration("15m")
}
`

// TestRelationLookupSeesGroupMemberCarryingACondition — судимая ось.
//
// `group#member with mfa_fresh` есть членство группы: условие сужает, КОГДА
// членство действует, и не отменяет того, ЧТО это членство. Прочитав его как
// «группа не принимается», судья объявляет путь §3.5 неисполнимым там, где он
// исполним, — и делает это молча.
func TestRelationLookupSeesGroupMemberCarryingACondition(t *testing.T) {
	d, ok := moduleseedparity.LookupRelation(conditionedModel, "cluster", "with_conditioned_group")
	require.True(t, ok, "объявление есть в каноне — «не нашёл» здесь было бы вымыслом")
	require.True(t, d.HasDirectSubjects, "прямой userset объявлен")
	require.True(t, d.AdmitsGroupMember(),
		"запись `%s with mfa_fresh` ЕСТЬ членство группы: условие сужает, когда членство "+
			"действует, а не отменяет его. Чтение, сравнивающее запись со строкой, отвечает "+
			"здесь «не принимает» — и проба предпосылки остаётся зелёной, пережив свой предмет",
		moduleseedparity.SubjectGroupMember)
}

// TestRelationLookupConditionOnAnotherSubjectDoesNotAdmitGroup — законный
// близнец судимой оси: условие стоит, членства группы нет.
//
// Без него «принимает» могло бы означать «отвечает да на всякую запись с
// условием», и первая же ось зеленела бы на чтении, ничего не различающем.
func TestRelationLookupConditionOnAnotherSubjectDoesNotAdmitGroup(t *testing.T) {
	d, ok := moduleseedparity.LookupRelation(conditionedModel, "cluster", "with_conditioned_account")
	require.True(t, ok)
	require.True(t, d.HasDirectSubjects)
	require.False(t, d.AdmitsGroupMember(),
		"условие на записи служебного аккаунта членства группы не заводит")
}

// TestRelationLookupGroupWildcardIsNotGroupMember — вторая изоляция: тип
// субъекта `group` назван, а членство не названо.
//
// `group:*` есть «любой объект типа group» как СУБЪЕКТ, а не «любой член
// группы». Чтение, судящее по вхождению слова `group`, ответило бы «принимает» —
// и объявило бы путь §3.5 исполнимым на объявлении, которое членства не несёт.
func TestRelationLookupGroupWildcardIsNotGroupMember(t *testing.T) {
	d, ok := moduleseedparity.LookupRelation(conditionedModel, "cluster", "with_group_wildcard")
	require.True(t, ok)
	require.True(t, d.HasDirectSubjects)
	require.False(t, d.AdmitsGroupMember(),
		"`group:*` называет тип субъекта, а не членство: подстановка не есть `%s`",
		moduleseedparity.SubjectGroupMember)
}

// TestRelationLookupRefusesACanonItCannotParse — канон, который разбору не
// поддаётся, обязан давать «не нашёл», а не уверенный ответ по недочитанному.
//
// Здесь `service_account` не объявлен типом, то есть вход каноном не является.
// Разбор, отвечающий по такому входу, произвёл бы вердикт о модели, которой
// нет, — и на живом дереве это выглядело бы исправной работой.
func TestRelationLookupRefusesACanonItCannotParse(t *testing.T) {
	const notACanon = `model
  schema 1.1

type user

type cluster
    relations
    define system_viewer: [user, service_account]
`
	_, ok := moduleseedparity.LookupRelation(notACanon, "cluster", "system_viewer")
	require.False(t, ok,
		"тип субъекта `service_account` в этом входе не объявлен — вход каноном не является. "+
			"«Не нашёл» обязано отличаться от «нашёл и группу не принимает»")
}
