// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// chart_env_names_carry_their_own_product_prefix_injection_test.go —
// ДОКАЗАТЕЛЬСТВО СПОСОБНОСТИ УПАСТЬ для
// TestChartEnvNamesCarryTheirOwnProductPrefix.
//
// Инъекция кормит ТУ ЖЕ чистую функцию auditChartEnvPrefixes, что и настоящее
// дерево. По каждой форме отказа — ПАРА, отличающаяся РОВНО ОДНИМ фактом:
// внесённый дефект обязан дать находку с координатой, законный близнец —
// молчать.

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
	"github.com/stretchr/testify/require"
)

// injPeers — приставки соседних частей, как их выводит вызывающий из дерева.
func injPeers(parts ...string) map[string]string {
	out := map[string]string{}
	for _, p := range parts {
		out[productnaming.EnvPrefix(p)+"_"] = p
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось 1 — СВОЯ переменная приставкой платформы.

func TestInjectionEnvPrefix_OwnVariableWithPlatformPrefixIsAFinding(t *testing.T) {
	findings, census := auditChartEnvPrefixes([]chartEnvDecl{{
		part: "iam", file: "чарт/шаблон.tpl", line: 42, name: "KACHO_IDENTITY_SUBSTITUTED_VARS",
	}}, injPeers("registry", "vpc", "geo"))

	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "чарт/шаблон.tpl:42", "находка обязана назвать КООРДИНАТУ")
	require.Contains(t, findings[0], `"KACHO_IDENTITY_SUBSTITUTED_VARS"`)
	require.Contains(t, findings[0], `"KANAME_"`, "находка обязана назвать канон источника имён")
	require.Contains(t, findings[0], `"kaname"`, "находка обязана назвать имя продукта части")
	require.Equal(t, 1, census.platform)
	require.Equal(t, 0, census.peerKnobs)
}

func TestInjectionEnvPrefix_OwnVariableWithOwnPrefixIsSilence(t *testing.T) {
	// Отличие РОВНО одно — приставка имени.
	findings, census := auditChartEnvPrefixes([]chartEnvDecl{{
		part: "iam", file: "чарт/шаблон.tpl", line: 42, name: "KANAME_IDENTITY_SUBSTITUTED_VARS",
	}}, injPeers("registry", "vpc", "geo"))

	require.Empty(t, findings)
	require.Equal(t, 1, census.ownCorrect,
		"молчание обязано приходить с ПРОЧИТАННЫМ — иначе оно неотличимо от «не смотрели»")
	require.Equal(t, 0, census.platform)
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось 2 — РУЧКА СОСЕДА. Законное упоминание зависимости: чарт объясняет, с чем
// разговаривает. Отличие от оси 1 — принадлежность имени, а НЕ место строки.

func TestInjectionEnvPrefix_PeerKnobIsSilence(t *testing.T) {
	findings, census := auditChartEnvPrefixes([]chartEnvDecl{
		{part: "iam", file: "чарт/значения.yaml", line: 402, name: "KACHO_REGISTRY_TOKEN_REALM"},
		{part: "iam", file: "чарт/помощники.tpl", line: 54, name: "KACHO_REGISTRY_SERVICE_AUD"},
	}, injPeers("registry", "vpc", "geo"))

	require.Empty(t, findings, "ручка соседа — законное упоминание зависимости")
	require.Equal(t, 2, census.platform, "она всё же СОСЧИТАНА как имя с приставкой платформы")
	require.Equal(t, 2, census.peerKnobs)
}

func TestInjectionEnvPrefix_NameOfANonExistentPeerIsAFinding(t *testing.T) {
	// Отличие от предыдущего РОВНО одно: части `identity` в дереве нет, значит
	// имя принадлежит не соседу, а самому чарту.
	findings, _ := auditChartEnvPrefixes([]chartEnvDecl{{
		part: "iam", file: "чарт/шаблон.tpl", line: 7, name: "KACHO_IDENTITY_SMTP_CREDENTIAL",
	}}, injPeers("registry", "vpc", "geo"))

	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "KACHO_IDENTITY_SMTP_CREDENTIAL")
}

// Самая длинная подходящая приставка, а не первая попавшаяся: короткая забрала
// бы чужие имена себе, и находка назвала бы не ту часть.
func TestInjectionEnvPrefix_LongestPeerPrefixWins(t *testing.T) {
	peers := injPeers("api", "api-gateway")
	owner, ok := longestPeerPrefixOwner("KACHO_API_GATEWAY_LISTEN_ADDR", peers)
	require.True(t, ok)
	require.Equal(t, "api-gateway", owner)

	owner, ok = longestPeerPrefixOwner("KACHO_API_SOMETHING", peers)
	require.True(t, ok)
	require.Equal(t, "api", owner)

	_, ok = longestPeerPrefixOwner("KACHO_NOSUCHPART_X", peers)
	require.False(t, ok, "имя вне пространств соседей соседу не принадлежит")
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось 3 — ИМЯ НЕ НАШЕ ВОВСЕ. У переменной стороннего процесса приставки
// продукта нет и быть не должно; требовать её значило бы краснеть на верном
// дереве (`COURIER_SMTP_CONNECTION_URI`, `SRC_HOST`, `HOME`).

func TestInjectionEnvPrefix_ForeignProcessVariableIsSilence(t *testing.T) {
	findings, census := auditChartEnvPrefixes([]chartEnvDecl{
		{part: "iam", file: "f", line: 1, name: "COURIER_SMTP_CONNECTION_URI"},
		{part: "iam", file: "f", line: 2, name: "SRC_HOST"},
	}, injPeers("registry"))

	require.Empty(t, findings)
	require.Equal(t, 0, census.platform, "чужое имя приставкой платформы не считается")
	require.Equal(t, 2, census.names, "но ПРОЧИТАНО оно обязано быть")
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось 4 — ОДНО ИМЯ, ОДНА НАХОДКА. Имя подстановки встречается в шаблоне
// восемнадцать раз; перечень из восемнадцати одинаковых строк не читают.

func TestInjectionEnvPrefix_RepeatedNameYieldsOneFinding(t *testing.T) {
	var decls []chartEnvDecl
	for i := 1; i <= 18; i++ {
		decls = append(decls, chartEnvDecl{
			part: "iam", file: "чарт/шаблон.tpl", line: i, name: "KACHO_SUBST_TOKEN",
		})
	}
	findings, census := auditChartEnvPrefixes(decls, injPeers("registry"))

	require.Len(t, findings, 1, "повторы одного имени обязаны схлопываться в одну находку")
	require.Contains(t, findings[0], "чарт/шаблон.tpl:1", "названа ПЕРВАЯ встреча")
	require.Equal(t, 18, census.platform, "перепись при этом считает ВСЕ вхождения")
}

func TestInjectionEnvPrefix_SameNameInDifferentPartsYieldsTwoFindings(t *testing.T) {
	// Отличие РОВНО одно: части разные, значит и предметов два.
	findings, _ := auditChartEnvPrefixes([]chartEnvDecl{
		{part: "iam", file: "a", line: 1, name: "KACHO_SUBST_TOKEN"},
		{part: "other", file: "b", line: 1, name: "KACHO_SUBST_TOKEN"},
	}, injPeers("registry"))
	require.Len(t, findings, 2)
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось 5 — ПЕРЕПИСЬ И ВАКУУМНЫЙ ЗЕЛЁНЫЙ.

func TestInjectionEnvPrefix_EmptyInputCountsNothing(t *testing.T) {
	findings, census := auditChartEnvPrefixes(nil, injPeers("registry"))
	require.Empty(t, findings)
	require.Zero(t, census.names)
	require.Zero(t, census.ownCorrect,
		"пустой обход обязан быть ВИДЕН числами — по ним тест дерева и отказывает")
}

// ─────────────────────────────────────────────────────────────────────────────
// Предпосылки самой проверки. Обе выведены, а не выписаны, и обе способны
// перестать быть верными молча.

func TestInjectionEnvPrefix_PlatformPrefixIsDerivedNotWritten(t *testing.T) {
	require.Equal(t, "KACHO_", platformEnvPrefix,
		"приставка платформы выводится из источника имён; расхождение означает, "+
			"что источник сменил форму, а проверка об этом не знает")
}

func TestInjectionEnvPrefix_TreeCarriesAPartWithItsOwnProductName(t *testing.T) {
	renamed := productnaming.RenamedServices()
	require.NotEmpty(t, renamed,
		"ведомость собственных имён пуста — у проверки нет предмета, и её зелёный вакуумен")
	for part, name := range renamed {
		require.NotEqual(t, "kacho-"+part, name,
			"запись ведомости не даёт части СВОЕГО имени — предмета у неё нет")
		require.True(t, strings.HasPrefix(productnaming.EnvPrefix(part), "KANAME") ||
			!strings.HasPrefix(productnaming.EnvPrefix(part), "KACHO_"),
			"часть со своим именем продукта обязана иметь и свою приставку окружения")
	}
}

func TestInjectionEnvPrefix_TreeRosterOfPartsIsNotEmpty(t *testing.T) {
	parts := productPartsInTree(t)
	require.NotEmpty(t, parts)
	for _, want := range []string{"iam", "registry", "vpc", "geo"} {
		require.Contains(t, parts, want,
			"часть %q обязана быть найдена обходом дерева — иначе её ручки читались бы "+
				"как чужие переменные этого чарта", want)
	}
}
