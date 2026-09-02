// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// recipient_injection_test.go — доказательство того, что чтение канона модели
// СПОСОБНО ответить и «да», и «нет», и «не нашёл».
//
// Проба предпосылки над живой базой (recipient_test.go) судит по этому чтению;
// чтение, которое молча отвечает «группа не принимается» на любом входе,
// сделало бы её вечнозелёной. Здесь ответ спрашивается на синтетическом каноне
// по каждой оси, и законный близнец подаётся первым: соседнее отношение того же
// типа, которое членство группы ПРИНИМАЕТ, не имеет права перекрасить судимое.
package moduleseedparity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/iam/internal/moduleseedparity"
)

// syntheticModel — канон той же формы, что и настоящий: тип столбцом, объявления
// отношений с отступом. Два типа и четыре отношения нужны, чтобы обе изоляции —
// между отношениями одного типа и между одноимёнными отношениями разных типов —
// проверялись на входе, где перепутать ЕСТЬ что.
const syntheticModel = `model
  schema 1.1

type user

type cluster
    relations
    define system_viewer: [user, service_account]
    define quota_reader: [service_account, group#member]
    define derived_admin: system_viewer or quota_reader

type account
    relations
    define system_viewer: [service_account, group#member]
`

func TestRelationLookupLegitimateTwinAdmitsGroupAndIsNotConfused(t *testing.T) {
	// КОНТРОЛЬ: соседнее отношение того же типа членство группы принимает.
	// Если бы чтение отвечало по типу целиком, а не по объявлению, оно
	// перекрасило бы судимое отношение — и проба предпосылки покраснела бы на
	// ровном месте.
	d, ok := moduleseedparity.LookupRelation(syntheticModel, "cluster", "quota_reader")
	require.True(t, ok, "объявление quota_reader у cluster не найдено")
	require.True(t, d.HasDirectSubjects)
	require.True(t, d.AdmitsGroupMember(), "quota_reader объявлен с group#member")
}

func TestRelationLookupJudgedRelationDoesNotAdmitGroup(t *testing.T) {
	d, ok := moduleseedparity.LookupRelation(syntheticModel, "cluster", "system_viewer")
	require.True(t, ok, "объявление system_viewer у cluster не найдено")
	require.True(t, d.HasDirectSubjects)
	require.False(t, d.AdmitsGroupMember(),
		"system_viewer у cluster объявлен без group#member — чтение обязано это увидеть")
	require.Equal(t, []string{"user", "service_account"}, d.DirectSubjects)
}

func TestRelationLookupSameNameOnAnotherTypeDoesNotSubstitute(t *testing.T) {
	// Одноимённое отношение ДРУГОГО типа членство группы принимает. Чтение,
	// берущее первое совпадение по имени, ответило бы «принимает» и на
	// кластерном — то есть объявило бы предмет #1936 закрытым, не будучи им.
	d, ok := moduleseedparity.LookupRelation(syntheticModel, "account", "system_viewer")
	require.True(t, ok)
	require.True(t, d.AdmitsGroupMember(), "у account это отношение принимает группу")

	c, ok := moduleseedparity.LookupRelation(syntheticModel, "cluster", "system_viewer")
	require.True(t, ok)
	require.False(t, c.AdmitsGroupMember(), "тип судится по СВОЕМУ объявлению")
}

func TestRelationLookupComputedRelationIsNotJudgedSilently(t *testing.T) {
	// Вычисляемое отношение прямого userset не несёт вовсе. Ответ «группу не
	// принимает» был бы здесь вымыслом: членство может прийти транзитивно.
	// Поэтому чтение говорит об ОТСУТСТВИИ прямых субъектов, а судья краснеет.
	d, ok := moduleseedparity.LookupRelation(syntheticModel, "cluster", "derived_admin")
	require.True(t, ok, "объявление найдено — оно есть в каноне")
	require.False(t, d.HasDirectSubjects,
		"у вычисляемого отношения прямых субъектов нет, и это отличается от «нет группы»")
	require.Empty(t, d.DirectSubjects)
}

func TestRelationLookupUnknownTypeIsNotFound(t *testing.T) {
	_, ok := moduleseedparity.LookupRelation(syntheticModel, "project", "system_viewer")
	require.False(t, ok, "типа project в этом каноне нет — «не нашёл» обязано отличаться от «нет группы»")
}

func TestRelationLookupUnknownRelationIsNotFound(t *testing.T) {
	_, ok := moduleseedparity.LookupRelation(syntheticModel, "cluster", "fga_writer")
	require.False(t, ok, "такого отношения у cluster нет")
}

func TestRelationLookupEmptyCanonIsNotFound(t *testing.T) {
	// Пустой вход — не «группа не принимается». Иначе судья, которому подсунули
	// пустой файл, объявил бы предпосылку живой, ничего не прочитав.
	_, ok := moduleseedparity.LookupRelation("", "cluster", "system_viewer")
	require.False(t, ok)
}
