// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trust_anchor_claim_injection_test.go — ДОКАЗАТЕЛЬСТВО, что гейт посадки
// доверия способен упасть и смолчать, и что каждая инъекция роняет ТОЛЬКО СВОЮ
// ось (#1753).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ НЕДОСТАТОЧНО «ГЕЙТ ПОКРАСНЕЛ»
//
// У гейта три оси, и они независимы. Инъекция, роняющая всё разом, доказывает
// лишь то, что он вообще умеет краснеть, и оставляет две оси непроверенными: их
// молчание неотличимо от их мёртвости. Поэтому дефект вносится ПООСЕВО, и
// каждый случай утверждает ДВЕ вещи — своя ось сработала И чужие промолчали.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕТЫРЕ ПРОГОНА: КОНТРОЛЬ + ПО ОДНОМУ НА ОСЬ
//
//	контроль        всё цело           — молчат ВСЕ оси
//	инъекция A      якорь без отметки  — краснеет ТОЛЬКО A
//	инъекция B      отметка без опоры  — краснеет ТОЛЬКО B
//	инъекция C      отметка без якоря  — краснеет ТОЛЬКО C
//
// Контроль обязателен: без него молчание оси на инъекции соседа ничего не
// доказывает — она могла молчать всегда.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАКОННЫЙ БЛИЗНЕЦ ПРИСУТСТВУЕТ В КАЖДОМ СЛУЧАЕ
//
// В каждую синтетику положен список переменных БЕЗ якорей и БЕЗ отметки —
// обычный контейнер, каких в дереве большинство. Он обязан молчать всегда:
// гейт, требующий отметку от всякого списка подряд, был бы снят первым же
// человеком, которому он покраснел на ровном месте.
//
// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИЯ ЗОВЁТ НАСТОЯЩЕГО СУДЬЮ
//
// Разбор (`scanTrustEnvLists`) и решение (`adjudicateTrustLists`) — те же
// функции, что исполняет гейт. Подставной судья доказывал бы способность упасть
// у копии, а не у продукта.
package deploy_test

import (
	"sort"
	"strings"
	"testing"
)

// trustFixture собирает синтетический профиль: законный близнец + случай.
func trustFixture(caseBlock string) string {
	return `
service:
  deployment:
    # Законный близнец: обычный контейнер без якорей доверия. Обязан молчать
    # при любом состоянии остальных осей.
    env:
      - name: KACHO_LOG_LEVEL
        value: info
      - name: KACHO_DB_SSLMODE
        value: require
` + caseBlock
}

const (
	// Целое: якорь пинен, посадка объявлена, объявление обеспечено.
	trustCaseHealthy = `
  sidecar:
    extraEnv:
      # ДОВЕРИЕ: ДОПОЛНЯЕТ
      - name: SSL_CERT_FILE
        value: /etc/kacho-iam-ca/ca.crt
`
	// A: якорь пинен, посадка НЕ объявлена — сегодняшний дефект #1753.
	trustCaseUndeclared = `
  sidecar:
    extraEnv:
      - name: SSL_CERT_FILE
        value: /etc/kacho-iam-ca/ca.crt
`
	// B: объявлена исключительность, пинена ОДНА переменная — вторая
	// продолжает читаться, и объявленное недостижимо by construction.
	trustCaseUnbacked = `
  sidecar:
    extraEnv:
      # ДОВЕРИЕ: ТОЛЬКО-ВНУТРЕННЕЕ
      - name: SSL_CERT_FILE
        value: /etc/kacho-iam-ca/ca.crt
`
	// B-близнец: та же отметка, но пинены ОБЕ — обеспечено, обязано молчать.
	trustCaseBackedExclusive = `
  sidecar:
    extraEnv:
      # ДОВЕРИЕ: ТОЛЬКО-ВНУТРЕННЕЕ
      - name: SSL_CERT_FILE
        value: /etc/kacho-iam-ca/ca.crt
      - name: SSL_CERT_DIR
        value: /etc/kacho-iam-ca
`
	// C: отметка осталась, якорей больше нет — пережила свой предмет.
	trustCaseOutlived = `
  sidecar:
    extraEnv:
      # ДОВЕРИЕ: ДОПОЛНЯЕТ
      - name: KACHO_SOMETHING_ELSE
        value: "1"
`
)

// axesFired — какие оси сработали на данной синтетике.
func axesFired(t *testing.T, fixture string) []trustAxisCode {
	t.Helper()
	lists := scanTrustEnvLists("synthetic.yaml", fixture)
	if len(lists) == 0 && strings.Contains(fixture, "SSL_CERT_") {
		t.Fatalf("разбор не увидел ни одного списка с якорем — доказывать нечего, "+
			"проверьте форму синтетики:\n%s", fixture)
	}
	var axes []trustAxisCode
	for _, f := range adjudicateTrustLists(lists) {
		axes = append(axes, f.Axis)
	}
	sort.Slice(axes, func(i, j int) bool { return axes[i] < axes[j] })
	return axes
}

func axesEqual(got []trustAxisCode, want ...trustAxisCode) bool {
	if len(got) != len(want) {
		return false
	}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestTrustAnchorClaimInjection(t *testing.T) {
	t.Run("контроль: всё цело — молчат ВСЕ оси", func(t *testing.T) {
		got := axesFired(t, trustFixture(trustCaseHealthy))
		if len(got) != 0 {
			t.Errorf("гейт краснеет на ИСПРАВНОМ объявлении — он ловит форму, а не существо: %v", got)
		}
	})

	t.Run("контроль: обеспеченная исключительность молчит", func(t *testing.T) {
		got := axesFired(t, trustFixture(trustCaseBackedExclusive))
		if len(got) != 0 {
			t.Errorf("ось B краснеет на ОБЕСПЕЧЕННОМ объявлении — тогда она запрещает "+
				"законную посадку, а не ловит необеспеченную: %v", got)
		}
	})

	t.Run("инъекция A: якорь без отметки — краснеет ТОЛЬКО A", func(t *testing.T) {
		got := axesFired(t, trustFixture(trustCaseUndeclared))
		if !axesEqual(got, trustAxisPostureUndeclared) {
			t.Errorf("ожидалась ровно ось «посадка не объявлена», получено %v.\n"+
				"пусто — ось A мертва (это и есть дефект #1753, гейт его не увидел бы);\n"+
				"лишнее — инъекция уронила не только проверяемое, доказано не то", got)
		}
	})

	t.Run("инъекция B: отметка без опоры — краснеет ТОЛЬКО B", func(t *testing.T) {
		got := axesFired(t, trustFixture(trustCaseUnbacked))
		if !axesEqual(got, trustAxisClaimUnbacked) {
			t.Errorf("ожидалась ровно ось «исключительность не обеспечена», получено %v.\n"+
				"пусто — гейт пропускает объявление, недостижимое by construction;\n"+
				"лишнее — инъекция уронила не только проверяемое", got)
		}
	})

	t.Run("инъекция C: отметка без якоря — краснеет ТОЛЬКО C", func(t *testing.T) {
		got := axesFired(t, trustFixture(trustCaseOutlived))
		if !axesEqual(got, trustAxisMarkerOutlived) {
			t.Errorf("ожидалась ровно ось «отметка пережила предмет», получено %v.\n"+
				"пусто — ведомость посадок не истекает сама и станет слепой зоной", got)
		}
	})

	t.Run("законный близнец молчит во ВСЕХ случаях", func(t *testing.T) {
		// Близнец лежит в каждой синтетике; если бы он давал находку, она
		// приходила бы даже в контроле. Прогон контроля выше это уже показал —
		// здесь утверждается отдельно, чтобы причина была названа явно.
		for name, c := range map[string]string{
			"целое": trustCaseHealthy, "A": trustCaseUndeclared,
			"B": trustCaseUnbacked, "C": trustCaseOutlived,
		} {
			for _, ax := range axesFired(t, trustFixture(c)) {
				if strings.Contains(string(ax), "synthetic") {
					t.Errorf("%s: находка приписана законному близнецу: %v", name, ax)
				}
			}
		}
		// Отдельно: сам близнец, взятый БЕЗ случая, обязан не давать ничего.
		if got := axesFired(t, trustFixture("")); len(got) != 0 {
			t.Errorf("обычный контейнер без якорей даёт находку %v — гейт требует "+
				"отметку от всякого списка подряд и будет снят первым же читателем", got)
		}
	})
}
