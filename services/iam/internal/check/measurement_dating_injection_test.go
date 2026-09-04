// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

package check_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Доказательство способности гейта датировки упасть — инъекцией в обе стороны.
// Каждая проба меняет РОВНО ОДИН факт против своего близнеца.

// TestDatingGateRedsOnASelfReference — прямая сторона: датировка самоссылкой.
// Это ровно та форма, что жила в §19 и в двух приёмках.
func TestDatingGateRedsOnASelfReference(t *testing.T) {
	findings, c := auditDating(map[string]string{
		"a.md": "**Замер на ревизии записи** (единица счёта названа):\n\n51 вызов\n",
	})
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "a.md", "находка обязана НАЗЫВАТЬ документ")
	require.Contains(t, findings[0], "ХЕШЕМ", "и называть, что делать дальше")
	require.Equal(t, 1, c.headings)
	require.Zero(t, c.byHash)
}

// TestDatingGateIsSilentOnAHash — законный близнец: тот же текст, один изменённый
// факт — ревизия названа хешем.
func TestDatingGateIsSilentOnAHash(t *testing.T) {
	findings, c := auditDating(map[string]string{
		"a.md": "**Замер на ревизии `a30d906b8e`** (единица счёта названа):\n\n53 вызова\n",
	})
	require.Empty(t, findings)
	require.Equal(t, 1, c.headings)
	require.Equal(t, 1, c.byHash)
}

// TestDatingGateSeesFormsOtherThanTheBoldHeading — форма, которую первая редакция
// гейта пропускала МОЛЧА. Обе живые самоссылки дерева были записаны именно так.
func TestDatingGateSeesFormsOtherThanTheBoldHeading(t *testing.T) {
	findings, c := auditDating(map[string]string{
		"b.md": "* Замер на ревизии: у registry таких функций 2, обе провязаны*\n",
		"c.md": "Замер на ревизии измерения, предикаты названы:\n",
	})
	require.Len(t, findings, 2, "распознаватель обязан знать все законные формы записи предмета")
	require.Equal(t, 2, c.headings)
}

// TestDatingGateIsSilentOnTheProseQuotingTheBadForm — законный близнец: разбор
// собственной ошибки цитирует негодную форму строчной буквой внутри ёлочек.
// Без этого различения гейт краснел бы на объяснении самого себя.
func TestDatingGateIsSilentOnTheProseQuotingTheBadForm(t *testing.T) {
	findings, c := auditDating(map[string]string{
		"d.md": "> Здесь стояло «замер на ревизии записи» — самоссылка, восстановимая\n" +
			"> только раскопками по истории.\n",
	})
	require.Empty(t, findings, "цитата негодной формы прозой находкой не является")
	require.Zero(t, c.headings, "и в перепись утверждений о замере она не попадает")
}

// TestDatingGateRejectsATokenTooShortToBeAHash — граница предиката названа: семь
// шестнадцатеричных знаков минимум, иначе «ревизия» неотличима от слова.
func TestDatingGateRejectsATokenTooShortToBeAHash(t *testing.T) {
	findings, _ := auditDating(map[string]string{
		"e.md": "**Замер на ревизии `abc`** — три знака хешем не являются\n",
	})
	require.Len(t, findings, 1)
}

// TestDatingGateReportsAnEmptyWalkInsteadOfPassing — «ноль находок» обязано быть
// отличимо от «ноль прочитанного»: утверждений о замере ноль ⇒ предмет исчез, и
// несущая проба обязана упасть именно на этом.
func TestDatingGateReportsAnEmptyWalkInsteadOfPassing(t *testing.T) {
	findings, c := auditDating(map[string]string{"f.md": "Документ без единого замера.\n"})
	require.Empty(t, findings)
	require.Zero(t, c.headings)
	require.Equal(t, 1, c.filesRead)
}
