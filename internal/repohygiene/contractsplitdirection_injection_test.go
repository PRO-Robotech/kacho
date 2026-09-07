// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

// Доказательство того, что гейт направления разделения контрактов СПОСОБЕН
// упасть — и что падает он на существе, а не на форме.
//
// ФИКСТУРА ПРИВЯЗАНА К ДЕРЕВУ, А НЕ К ПАМЯТИ АВТОРА: обе стороны берутся из
// закоммиченных описаний контрактов, дефект в них ВНОСИТСЯ, и каждая проба
// сперва утверждает, что предмет её правки в дереве есть. Синтетика доказывала
// бы свойство вчерашнего дерева.
//
// ПРОГОНОВ ЧЕТЫРЕ, и третий обязателен (`testing.md` §«Гейт на класс», п. 2в):
// без него молчание гейта на контроле неотличимо от молчания мёртвого.
// Четвёртый — про ФОРМУ оператора: `import public "…"` законен, и распознаватель,
// её не знающий, выпускал бы объявленное ею ребро из-под наблюдения молча.

// splitFixture — обе стороны, прочитанные из дерева.
func splitFixture(t *testing.T) (platform, service []repohygiene.ContractFile) {
	t.Helper()
	root := repoRootFor(t)
	protoRoot := filepath.Join(root, "proto")
	return readProtoTree(t, protoRoot, "kacho"), readProtoTree(t, protoRoot, "kaname")
}

// withInjectedImport возвращает копию среза, в котором названному описанию
// дописан оператор импорта.
func withInjectedImport(t *testing.T, files []repohygiene.ContractFile, path, stmt string) []repohygiene.ContractFile {
	t.Helper()
	out := make([]repohygiene.ContractFile, len(files))
	copy(out, files)
	for i := range out {
		if out[i].Path != path {
			continue
		}
		require.Containsf(t, out[i].Src, "syntax = \"proto3\";",
			"фикстура беспредметна: %s не выглядит описанием контракта", path)
		out[i].Src = strings.Replace(out[i].Src, "syntax = \"proto3\";",
			"syntax = \"proto3\";\n"+stmt, 1)
		return out
	}
	t.Fatalf("фикстура беспредметна: описания %s в дереве нет — координата умерла, "+
		"и проба доказывала бы свойство вчерашнего дерева", path)
	return nil
}

// Прогон 1 — КОНТРОЛЬ: дерево как есть, гейт молчит, и обе стороны прочитаны.
func TestCSD_Run1_ControlIsSilentAndBothTreesWereRead(t *testing.T) {
	t.Parallel()
	platform, service := splitFixture(t)

	f, cen := repohygiene.AuditContractSplitDirection(platform, service, "kacho", "kaname")
	require.Empty(t, f, "гейт покраснел на дереве как есть — контроль опровергнут:\n  %s",
		strings.Join(f, "\n  "))
	require.NotZero(t, cen.PlatformImports,
		"импортов платформы прочитано ноль — обход пуст, и молчание беспредметно")
	require.NotZero(t, cen.ServiceToPlatform,
		"импортов службы в платформу прочитано ноль: их десятки, и ноль здесь означает "+
			"слепой распознаватель, а не разорванную зависимость")
}

// Прогон 2 — ВНЕСЁННОЕ РЕБРО: контракт платформы импортирует контракт службы.
// Это и есть предмет, которым стадия S2 заводилась.
func TestCSD_Run2_PlatformImportingTheServiceIsAFinding(t *testing.T) {
	t.Parallel()
	platform, service := splitFixture(t)

	const victim = "kacho/cloud/quota/v1/quota.proto"
	injected := withInjectedImport(t, platform, victim,
		`import "kaname/cloud/iam/v1/limit.proto";`)

	f, cen := repohygiene.AuditContractSplitDirection(injected, service, "kacho", "kaname")
	require.Lenf(t, f, 1, "внесённое ребро обязано дать РОВНО одну находку, получено %d:\n  %s",
		len(f), strings.Join(f, "\n  "))
	require.Contains(t, f[0], victim, "находка не называет контракт-виновник")
	require.Contains(t, f[0], "kaname/cloud/iam/v1/limit.proto",
		"находка не называет импортируемый контракт — читателю негде посмотреть")
	require.Equal(t, len(platform), cen.PlatformFiles,
		"перепись сбилась: осмотрено %d описаний платформы вместо %d",
		cen.PlatformFiles, len(platform))
}

// Прогон 3 — ЗАКОННЫЙ БЛИЗНЕЦ: контракт СЛУЖБЫ импортирует контракт платформы.
// Форма та же, направление обратное, и гейт обязан молчать — иначе он ловит
// форму, а не существо, и первый же законный импорт его отключит.
func TestCSD_Run3_ServiceImportingThePlatformIsSilent(t *testing.T) {
	t.Parallel()
	platform, service := splitFixture(t)

	const twin = "kaname/cloud/iam/v1/limit.proto"
	injected := withInjectedImport(t, service, twin,
		`import "kacho/cloud/quota/v1/quota.proto";`)

	f, cen := repohygiene.AuditContractSplitDirection(platform, injected, "kacho", "kaname")
	require.Emptyf(t, f, "гейт покраснел на ЗАКОННОМ импорте службы в платформу:\n  %s",
		strings.Join(f, "\n  "))
	require.NotZero(t, cen.ServiceToPlatform,
		"законное ребро не прочитано вовсе — молчание означает слепоту, а не согласие")
}

// Прогон 4 — ФОРМА ОПЕРАТОРА: `import public "…"` объявляет ту же зависимость.
// Распознаватель, знающий одну форму записи предмета, выпускает объявленное
// другой из-под наблюдения — не как находку и не как зелёное, а МОЛЧА
// (`testing.md` §«Гейт на класс», п. 7).
func TestCSD_Run4_PublicImportFormIsSeenToo(t *testing.T) {
	t.Parallel()
	platform, service := splitFixture(t)

	const victim = "kacho/cloud/quota/v1/quota.proto"
	injected := withInjectedImport(t, platform, victim,
		`import public "kaname/cloud/iam/v1/limit.proto";`)

	f, _ := repohygiene.AuditContractSplitDirection(injected, service, "kacho", "kaname")
	require.Lenf(t, f, 1,
		"форма `import public` не прочитана: ребро, объявленное ею, ушло бы из-под "+
			"наблюдения молча. Получено находок %d", len(f))
	require.Contains(t, f[0], victim)
}
