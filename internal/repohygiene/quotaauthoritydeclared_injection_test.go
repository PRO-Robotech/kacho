// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// quotaauthoritydeclared_injection_test.go — доказательство способности гейта
// упасть и смолчать. Инъекция настоящей формой из дерева, с законным близнецом.

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func sitesOfSource(t *testing.T, src string) []quotaStartSite {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, parser.SkipObjectResolution)
	require.NoError(t, err)
	return quotaStartSitesIn(fset, file, "main.go")
}

// TestQuotaStartSite_ConditionalIsAFinding — форма, ради которой гейт написан:
// подъём под наличием соединения соседа.
func TestQuotaStartSite_ConditionalIsAFinding(t *testing.T) {
	sites := sitesOfSource(t, `package main
func run() error {
	if authzConn != nil {
		stop, err := corequota.StartLimitSync(ctx, pool, authority, src, "kacho_vpc", corequota.Config{}, logger)
		_, _ = stop, err
	}
	return nil
}`)
	require.Len(t, sites, 1)
	require.True(t, sites[0].Conditional, "подъём под условием обязан быть находкой")
	require.Equal(t, "if", sites[0].Under, "находка обязана называть предмет, а не симптом")
}

// TestQuotaStartSite_UnconditionalIsSilent — законный близнец той же формы.
//
// Без него «находок нет» было бы неотличимо от «гейт ничего не различает».
func TestQuotaStartSite_UnconditionalIsSilent(t *testing.T) {
	sites := sitesOfSource(t, `package main
func run() error {
	stop, err := corequota.StartLimitSync(ctx, pool, authority, src, "kacho_vpc", corequota.Config{}, logger)
	_, _ = stop, err
	return nil
}`)
	require.Len(t, sites, 1)
	require.False(t, sites[0].Conditional)
}

// TestQuotaStartSite_MentionInACommentIsNotACall — гейт судит УЗЕЛ вызова.
//
// Имя глагола стоит в комментариях, объясняющих сам запрет; гейт, судящий
// подстроку, краснел бы на собственном объяснении.
func TestQuotaStartSite_MentionInACommentIsNotACall(t *testing.T) {
	sites := sitesOfSource(t, `package main
// StartLimitSync заводит тянущего под объявлением. Строка ниже — литерал, а не вызов.
const doc = "StartLimitSync"
func run() { _ = doc }`)
	require.Empty(t, sites, "упоминание в комментарии и в литерале вызовом не является")
}

// TestQuotaStartSite_ForAndSwitchAreFindingsToo — ветвление бывает не только `if`.
func TestQuotaStartSite_ForAndSwitchAreFindingsToo(t *testing.T) {
	for name, src := range map[string]string{
		"switch": `package main
func run() {
	switch mode {
	case "on":
		_, _ = corequota.StartLimitSync(ctx, pool, a, s, "sch", corequota.Config{}, log)
	}
}`,
		"for": `package main
func run() {
	for _, s := range schemas {
		_, _ = corequota.StartLimitSync(ctx, pool, a, s, "sch", corequota.Config{}, log)
	}
}`,
	} {
		t.Run(name, func(t *testing.T) {
			sites := sitesOfSource(t, src)
			require.Len(t, sites, 1)
			require.True(t, sites[0].Conditional)
		})
	}
}
