// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

// Посадка группы правил по умолчанию: модель закрыта в обе стороны, послабление
// объявлено РЕСУРСОМ.
//
// ─────────────────────────────────────────────────────────────────────────────
// РЕШЕНИЕ ВЛАДЕЛЬЦА, КОТОРОЕ ЗДЕСЬ ЗАКРЕПЛЕНО
//
// Три части, вводятся ОДНИМ изменением:
//
//  1. модель fail-closed в ОБЕ стороны: нет правила — значит не разрешено;
//  2. засеянная группа по умолчанию несёт ЯВНОЕ разрешение исходящего и НИ ОДНОГО
//     входящего правила — то есть послабление видно в ресурсе, а не спрятано в
//     умолчании;
//  3. интерфейс НАСЛЕДУЕТ группу по умолчанию своей сети, и это записано в
//     контракте.
//
// Почему не «закрыто всё»: безопасность требует закрытой модели, работоспособность
// — исходящего доступа. Разница между «модель открыта» и «модель закрыта, а
// послабление объявлено ресурсом» в том, что второе ВИДНО и правится, а первое
// обнаруживается по последствиям.
//
// Почему исходящее — В ОБОИХ СЕМЕЙСТВАХ: обещание симметрии семейств не сужается
// (решение §8.5). Асимметрия здесь была бы скрытой дырой наоборот: правило
// защищает одно семейство и не защищает другое, а арендатор об этом не знает.
//
// ЧТО БЫЛО ДО: два правила — разрешить весь ВХОД и весь ВЫХОД с `0.0.0.0/0`, то
// есть входящее было открыто у каждой сети по умолчанию, и притом только в одном
// семействе.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSecurityGroupRulesAllowEgressOnly(t *testing.T) {
	rules := NewDefaultSecurityGroupRules()

	var ingress, egress []SecurityGroupRule
	for _, r := range rules {
		switch r.Direction {
		case SecurityGroupRuleDirectionIngress:
			ingress = append(ingress, r)
		case SecurityGroupRuleDirectionEgress:
			egress = append(egress, r)
		default:
			t.Fatalf("правило с направлением вне закрытого набора: %v", r.Direction)
		}
	}

	assert.Empty(t, ingress,
		"входящее правило в группе по умолчанию означает, что вход открыт у КАЖДОЙ сети, "+
			"и арендатор об этом не спрашивал. Закрытая модель обязана быть закрытой на входе")

	require.Len(t, egress, 2,
		"исходящее разрешение обязано быть в ОБОИХ семействах: обещание симметрии не "+
			"сужается, а асимметрия — скрытая дыра наоборот (правило защищает одно "+
			"семейство и не защищает другое, а арендатор об этом не знает)")

	var v4, v6 bool
	for _, r := range egress {
		assert.Equal(t, "ANY", r.ProtocolName, "послабление объявлено целиком, а не по протоколам")
		switch {
		case len(r.V4CidrBlocks) == 1 && r.V4CidrBlocks[0] == "0.0.0.0/0":
			v4 = true
			assert.Empty(t, r.V6CidrBlocks, "правило одного семейства не несёт диапазонов другого")
		case len(r.V6CidrBlocks) == 1 && r.V6CidrBlocks[0] == "::/0":
			v6 = true
			assert.Empty(t, r.V4CidrBlocks, "правило одного семейства не несёт диапазонов другого")
		default:
			t.Errorf("исходящее правило не покрывает семейство целиком: v4=%v v6=%v", r.V4CidrBlocks, r.V6CidrBlocks)
		}
	}
	assert.True(t, v4, "нет исходящего разрешения для IPv4")
	assert.True(t, v6, "нет исходящего разрешения для IPv6")
}

// TestDefaultSecurityGroupRulesAreFreshEachCall — builder, а не глобальная
// переменная: вызывающий вправе мутировать результат без побочных эффектов у
// следующего. Свойство было у прежней редакции и не должно потеряться.
func TestDefaultSecurityGroupRulesAreFreshEachCall(t *testing.T) {
	a := NewDefaultSecurityGroupRules()
	require.NotEmpty(t, a)
	a[0].ProtocolName = "МУТИРОВАНО"
	b := NewDefaultSecurityGroupRules()
	require.NotEmpty(t, b)
	assert.NotEqual(t, "МУТИРОВАНО", b[0].ProtocolName,
		"второй вызов вернул тот же слайс — мутация одного вызывающего видна другому")
}
