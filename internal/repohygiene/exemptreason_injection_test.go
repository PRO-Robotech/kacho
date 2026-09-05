// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// exemptreason_injection_test.go — СУДЬЯ ПОЛОСЫ «ПРОВЕРКИ НЕТ» СПОСОБЕН УПАСТЬ И
// СПОСОБЕН СМОЛЧАТЬ.
//
// Гейт над деревом молчит и когда дерево чисто, и когда предикат сломан. Здесь
// вход судьи задаётся синтетически, поэтому каждое утверждение проверяется В ОБЕ
// СТОРОНЫ: рядом с воспроизведённым дефектом стоит ЗАКОННЫЙ БЛИЗНЕЦ той же
// формы, на котором судья обязан промолчать.
//
// Инъекция НЕ читает каталог дерева намеренно: читая его, она проверяла бы
// дерево, а не судью, — и осталась бы зелёной на судье, который не умеет
// краснеть вовсе.

import (
	"strings"
	"testing"
)

// allSitesExist — предикат существования координаты, отвечающий «да» всегда.
// Отдельным именем, чтобы случай «координата не резолвится» задавался явно, а не
// зависел от состояния диска.
func allSitesExist(string) bool { return true }

func hasFinding(j ExemptJudgement, needle string) bool {
	for _, f := range j.Findings {
		if strings.Contains(f, needle) {
			return true
		}
	}
	return false
}

func TestR893_ExemptJudgeCanFailAndCanStaySilent(t *testing.T) {
	sites := map[string]string{
		"kacho.cloud.iam.v1.UserService/List": "services/iam/internal/apps/kaname/api/user/list.go",
	}

	cases := []struct {
		name string
		rows []ExemptCatalogRow
		// want — подстрока находки, которую судья ОБЯЗАН назвать. Пусто =
		// законный близнец: судья обязан промолчать.
		want string
		// sites/exists — вход судьи, когда случай про перечень координат.
		sites  map[string]string
		exists func(string) bool
	}{
		{
			name: "освобождение без причины — находка",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.UserService/List", Permission: ExemptSentinelValue},
			},
			sites: sites,
			want:  "полоса `<exempt>` без причины",
		},
		{
			name: "законный близнец: та же запись с причиной и координатой — молчание",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.UserService/List", Permission: ExemptSentinelValue,
					ExemptReason: ReasonServiceSideAuthz},
			},
			sites: sites,
		},
		{
			name: "причина вне закрытого словаря — находка",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.UserService/List", Permission: ExemptSentinelValue,
					ExemptReason: "SERVICE_SIDE_AUTH"},
			},
			sites: sites,
			want:  "вне закрытого словаря",
		},
		{
			name: "прежняя причина «глобальный справочник» больше не существует — находка",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.UserService/List", Permission: ExemptSentinelValue,
					ExemptReason: "PUBLIC_CATALOG"},
			},
			sites: sites,
			want:  "вне закрытого словаря",
		},
		{
			name: "причина «внутренний слушатель» у публичной службы — находка",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.UserService/List", Permission: ExemptSentinelValue,
					ExemptReason: ReasonInternalListener},
			},
			sites: map[string]string{},
			want:  "у НЕвнутренней службы",
		},
		{
			name: "законный близнец: та же причина у внутренней службы — молчание",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.InternalUserService/Get", Permission: ExemptSentinelValue,
					ExemptReason: ReasonInternalListener},
			},
			sites: map[string]string{},
		},
		{
			name: "причина требует координаты, а её не назвали — находка",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.GroupService/List", Permission: ExemptSentinelValue,
					ExemptReason: ReasonServiceSideAuthz},
			},
			sites: map[string]string{},
			want:  "координата энфорса не названа",
		},
		{
			name: "координата названа, но не резолвится — находка",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.UserService/List", Permission: ExemptSentinelValue,
					ExemptReason: ReasonServiceSideAuthz},
			},
			sites:  sites,
			exists: func(string) bool { return false },
			want:   "координата энфорса не резолвится",
		},
		{
			name: "перечень координат пережил свою запись — находка (самоистечение)",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.InternalUserService/Get", Permission: ExemptSentinelValue,
					ExemptReason: ReasonInternalListener},
			},
			sites: sites,
			want:  "перечень координат называет метод",
		},
		{
			name: "причина освобождения у НЕосвобождённой записи — находка",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.NetworkService/Get", Permission: "vpc.networks.get",
					ExemptReason: ReasonSelfService},
			},
			sites: map[string]string{},
			want:  "не-освобождённая запись несёт причину",
		},
		{
			name: "законный близнец: обычная запись без причины — молчание",
			rows: []ExemptCatalogRow{
				{FQN: "kacho.cloud.iam.v1.NetworkService/Get", Permission: "vpc.networks.get"},
			},
			sites: map[string]string{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			exists := c.exists
			if exists == nil {
				exists = allSitesExist
			}
			j := JudgeExemptLane(c.rows, c.sites, exists)
			if c.want == "" {
				if len(j.Findings) != 0 {
					t.Fatalf("законный близнец обязан молчать, а судья назвал: %v\n%s",
						j.Findings, j.Census())
				}
				return
			}
			if !hasFinding(j, c.want) {
				t.Fatalf("судья обязан назвать находку %q, а вернул: %v\n%s",
					c.want, j.Findings, j.Census())
			}
		})
	}

	// Предпосылка самой инъекции: словарь не пуст и перечень координат не пуст.
	// Пустые сделали бы половину случаев выше тождественно истинными.
	if len(exemptReasonDictionary) == 0 {
		t.Fatalf("словарь причин пуст — инъекция беспредметна")
	}
	if len(EnforcementSite) == 0 {
		t.Fatalf("перечень координат пуст — инъекция беспредметна")
	}
	t.Logf("осмотрено: случаев инъекции %d, причин в словаре %d, координат в перечне %d",
		len(cases), len(exemptReasonDictionary), len(EnforcementSite))
}
