// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Инъекция сверки посева: доказательство, что она СПОСОБНА упасть и способна
// смолчать.
//
// Вход синтетический и подаётся Diff напрямую: вердикт сверки есть свойство
// пары «объявлено / живёт», и он не должен зависеть ни от состояния базы, ни от
// сегодняшнего содержимого манифестов. Каждая ось — В ОБЕ СТОРОНЫ; законный
// близнец стоит ПЕРВЫМ, иначе отрицание зеленело бы на сверке, находящей
// расхождение в любом входе.
package moduleseedparity_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/iam/internal/moduleseedparity"
)

func injSA(desc string) moduleseedparity.ServiceAccount {
	return moduleseedparity.ServiceAccount{Account: "kacho-system", Name: "kacho-demo", Description: desc}
}

func injJoin(groupAccount, group string) moduleseedparity.Join {
	return moduleseedparity.Join{
		AccountName: "kacho-system", SAName: "kacho-demo",
		GroupAccount: groupAccount, GroupName: group,
	}
}

func injState(declaredSA, liveSA []moduleseedparity.ServiceAccount,
	declaredJoin, liveJoin []moduleseedparity.Join,
) moduleseedparity.ModuleState {
	return moduleseedparity.ModuleState{
		Module: "demo", ManifestFile: "services/demo/manifest.yaml",
		DeclaredSA: declaredSA, LiveSA: liveSA,
		DeclaredJoin: declaredJoin, LiveJoin: liveJoin,
	}
}

// Законный близнец: объявленное сходится с живым по обеим осям.
func TestInjectionSeedParityAgreeingStateIsSilent(t *testing.T) {
	sa := []moduleseedparity.ServiceAccount{injSA("Module SA: kacho-demo")}
	joins := []moduleseedparity.Join{injJoin("kacho-system", "module-quota-readers")}
	findings := moduleseedparity.Diff([]moduleseedparity.ModuleState{injState(sa, sa, joins, joins)})
	require.Emptyf(t, findings,
		"согласованное состояние даёт находки — отрицание зеленело бы на всём сломанном: %v", findings)
}

// Пустое с ОБЕИХ сторон — тоже согласие: модуль без посева законен.
func TestInjectionSeedParityEmptyBothSidesIsSilent(t *testing.T) {
	require.Empty(t, moduleseedparity.Diff([]moduleseedparity.ModuleState{injState(nil, nil, nil, nil)}))
}

func TestInjectionSeedParityLiveRowNotDeclaredIsAFinding(t *testing.T) {
	live := []moduleseedparity.ServiceAccount{injSA("Module SA: kacho-demo")}
	findings := moduleseedparity.Diff([]moduleseedparity.ModuleState{injState(nil, live, nil, nil)})
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "служебная запись ЖИВЁТ и не объявлена")
	require.Contains(t, findings[0], "kacho-demo")
	require.Contains(t, findings[0], "services/demo/manifest.yaml")
}

// Обратная сторона: обещанное и не заведённое — тоже находка, и не мягче.
func TestInjectionSeedParityDeclaredRowNotLiveIsAFinding(t *testing.T) {
	declared := []moduleseedparity.ServiceAccount{injSA("Module SA: kacho-demo")}
	findings := moduleseedparity.Diff([]moduleseedparity.ModuleState{injState(declared, nil, nil, nil)})
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "служебная запись ОБЪЯВЛЕНА и не живёт")
}

// Сверка идёт по НАЗНАЧЕНИЮ тоже, а не по одному имени: применитель кладёт
// объявленное дословно поверх живой строки, поэтому расхождение прозы есть
// переписывание того, что уже действует.
func TestInjectionSeedParityDescriptionIsPartOfTheComparison(t *testing.T) {
	findings := moduleseedparity.Diff([]moduleseedparity.ModuleState{injState(
		[]moduleseedparity.ServiceAccount{injSA("почти то же самое")},
		[]moduleseedparity.ServiceAccount{injSA("Module SA: kacho-demo")},
		nil, nil)})
	require.Len(t, findings, 2, "расхождение прозы обязано быть названо с обеих сторон: %v", findings)
	joined := strings.Join(findings, "\n")
	require.Contains(t, joined, "ЖИВЁТ и не объявлена")
	require.Contains(t, joined, "ОБЪЯВЛЕНА и не живёт")
}

func TestInjectionSeedParityLiveJoinNotDeclaredIsAFinding(t *testing.T) {
	live := []moduleseedparity.Join{injJoin("kacho-system", "module-relation-writers")}
	findings := moduleseedparity.Diff([]moduleseedparity.ModuleState{injState(nil, nil, nil, live)})
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "вступление ЖИВЁТ и не объявлено")
	require.Contains(t, findings[0], "module-relation-writers")
}

// Сторона вступления адресуется ПАРОЙ (аккаунт, имя): один аккаунт не
// подразумевается, и подмена аккаунта обязана быть находкой.
func TestInjectionSeedParityJoinComparesThePairNotJustTheName(t *testing.T) {
	findings := moduleseedparity.Diff([]moduleseedparity.ModuleState{injState(nil, nil,
		[]moduleseedparity.Join{injJoin("другой-аккаунт", "module-quota-readers")},
		[]moduleseedparity.Join{injJoin("kacho-system", "module-quota-readers")})})
	require.Len(t, findings, 2, "подмена аккаунта группы не замечена: %v", findings)
}

// Находка называет МОДУЛЬ: перечень из десяти строк без имени владельца
// нечитаем, и читать его перестанут.
func TestInjectionSeedParityFindingNamesTheModule(t *testing.T) {
	findings := moduleseedparity.Diff([]moduleseedparity.ModuleState{
		injState(nil, []moduleseedparity.ServiceAccount{injSA("Module SA: kacho-demo")}, nil, nil),
		{Module: "second", ManifestFile: "services/second/manifest.yaml",
			LiveJoin: []moduleseedparity.Join{injJoin("kacho-system", "g")}},
	})
	require.Len(t, findings, 2)
	joined := strings.Join(findings, "\n")
	require.Contains(t, joined, "модуль demo")
	require.Contains(t, joined, "модуль second")
}
