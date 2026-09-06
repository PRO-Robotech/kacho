// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// image_name_agrees_with_the_source_of_names_injection_test.go — ДОКАЗАТЕЛЬСТВО
// СПОСОБНОСТИ УПАСТЬ для TestProfileImageNameAgreesWithTheSourceOfNames.
//
// Инъекция кормит ТУ ЖЕ чистую функцию auditProfileImageNames, что и настоящее
// дерево, поэтому доказанное здесь верно и там.
//
// По каждой форме отказа — ПАРА: внесённый дефект обязан дать находку, назвав
// координату, а ЗАКОННЫЙ БЛИЗНЕЦ той же формы обязан молчать. Пара отличается
// РОВНО ОДНИМ фактом: без этого красное могло прийти от соседа, и вердикт
// инъекции был бы про что угодно.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Ось 1 — ОТСТАВНОЕ ИМЯ. Ровно тот дефект, который приёмщик воспроизвёл на
// живом дереве: имя узнаётся источником имён как часть продукта, но не равно
// каноническому имени этой части.

func TestInjection_RetiredNameIsAFinding(t *testing.T) {
	findings, census := auditProfileImageNames([]imageNameDecl{{
		where:   "профиль:kaname.image.repository",
		section: "kaname",
		ref:     "kacho-iam:dev",
	}})

	require.Len(t, findings, 1, "отставное имя обязано дать РОВНО одну находку")
	require.Contains(t, findings[0], "профиль:kaname.image.repository",
		"находка обязана назвать КООРДИНАТУ — иначе её негде чинить")
	require.Contains(t, findings[0], `"kacho-iam"`, "находка обязана назвать найденное имя")
	require.Contains(t, findings[0], `"kaname"`, "находка обязана назвать канон")
	require.Contains(t, findings[0], "ОТСТАВНОЕ", "находка обязана назвать КЛАСС, а не только факт")
	require.Equal(t, 1, census.productRefs, "ссылка обязана быть УЗНАНА частью продукта")
	require.Equal(t, 0, census.canonicalHits)
}

func TestInjection_CanonicalNameIsSilence(t *testing.T) {
	// Законный близнец: тот же узел, та же секция, отличие РОВНО в имени.
	findings, census := auditProfileImageNames([]imageNameDecl{{
		where:   "профиль:kaname.image.repository",
		section: "kaname",
		ref:     "kaname:dev",
	}})

	require.Empty(t, findings, "каноническое имя обязано молчать")
	require.Equal(t, 1, census.canonicalHits,
		"молчание обязано приходить с ПРОЧИТАННЫМ: иначе оно неотличимо от «не смотрели»")
}

// Полный адрес реестра и пин по содержимому — те же имена, другая запись.
// Распознаватель, знающий одну форму, на остальных молчит НЕ ПОТОМУ ЧТО чисто.
func TestInjection_RetiredNameIsFoundInEveryFormOfTheReference(t *testing.T) {
	for _, ref := range []string{
		"kacho-iam",
		"kacho-iam:dev",
		"docker.io/prorobotech/kacho-iam:main-abc1234",
		"registry.example.invalid:5000/pro-robotech/kacho-iam:0.1.0",
		"docker.io/prorobotech/kacho-iam@sha256:0123456789abcdef",
	} {
		findings, _ := auditProfileImageNames([]imageNameDecl{{
			where: "профиль:kaname.image.repository", section: "kaname", ref: ref,
		}})
		require.Len(t, findings, 1, "форма записи %q обязана быть узнана", ref)
	}
	for _, ref := range []string{
		"kaname",
		"kaname:dev",
		"docker.io/prorobotech/kaname:main-abc1234",
		"registry.example.invalid:5000/pro-robotech/kaname:0.1.0",
		"docker.io/prorobotech/kaname@sha256:0123456789abcdef",
	} {
		findings, _ := auditProfileImageNames([]imageNameDecl{{
			where: "профиль:kaname.image.repository", section: "kaname", ref: ref,
		}})
		require.Empty(t, findings, "законная форма записи %q обязана молчать", ref)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось 2 — КАНОНИЧЕСКОЕ ИМЯ ЧУЖОЙ ЧАСТИ. Оси каноничности оно невидимо by
// construction: круг «имя → часть → имя» у него замкнут. Ловит его только
// согласие с местом.

func TestInjection_CanonicalNameOfAnotherPartIsAFinding(t *testing.T) {
	findings, census := auditProfileImageNames([]imageNameDecl{{
		where:   "профиль:kaname.image.repository",
		section: "kaname",
		ref:     "docker.io/prorobotech/kacho-vpc:main-abc1234",
	}})

	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "профиль:kaname.image.repository")
	require.Contains(t, findings[0], `"vpc"`, "находка обязана назвать ЧУЖУЮ часть")
	require.Contains(t, findings[0], `"iam"`, "находка обязана назвать часть МЕСТА")
	require.Equal(t, 1, census.canonicalHits,
		"ось каноничности обязана эту ссылку ПРОПУСТИТЬ — иначе доказано не то")
}

func TestInjection_CanonicalNameInItsOwnSectionIsSilence(t *testing.T) {
	// Отличие от предыдущего РОВНО одно: секция, а не имя образа.
	findings, _ := auditProfileImageNames([]imageNameDecl{{
		where:   "профиль:kacho-nlb.image.repository",
		section: "kacho-nlb",
		ref:     "docker.io/prorobotech/kacho-nlb:main-abc1234",
	}})
	require.Empty(t, findings)
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось 3 — ИМЯ, КОТОРОГО ИСТОЧНИК ИМЁН НЕ УЗНАЁТ ВОВСЕ. Это главная форма
// ослепления: такое имя не отвергается — оно НЕ ВИДНО ни одной проверке,
// ключующейся на именах продукта.

func TestInjection_UnrecognisedNameOnAWorkloadPositionIsAFinding(t *testing.T) {
	findings, census := auditProfileImageNames([]imageNameDecl{{
		where:   "профиль:kaname.image.repository",
		section: "kaname",
		ref:     "docker.io/example/totally-unrelated:1.0",
	}})

	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "профиль:kaname.image.repository")
	require.Contains(t, findings[0], `"totally-unrelated"`)
	require.Contains(t, findings[0], `"kaname"`, "находка обязана назвать канон места")
	require.Equal(t, 0, census.productRefs,
		"ссылка НЕ узнана частью продукта — в этом и предмет находки")
}

func TestInjection_UnrecognisedNameOffAWorkloadPositionIsSilence(t *testing.T) {
	// Законный близнец: то же имя, отличие РОВНО в том, что место не рабочее —
	// боковой контейнер, база, поставщик личности. Требовать от них имени части
	// продукта значило бы краснеть на верном дереве.
	findings, _ := auditProfileImageNames([]imageNameDecl{{
		where:   "профиль:kaname.opaSidecar.image",
		section: "", // не рабочее место образа секции
		ref:     "openpolicyagent/opa:1.0.0-rootless",
	}})
	require.Empty(t, findings)
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось 4 — ДЕЙСТВУЮЩЕЕ ОБЪЯВЛЕНИЕ БЕЗ ОБРАЗА. Стенду нечего загружать. Отличать
// это от частичной накладки обязательно: профиль, накладывающий один тег,
// репозиторий наследует у слоя под собой и находкой не является.

func TestInjection_EffectiveDeclWithoutAnImageIsAFinding(t *testing.T) {
	findings, _ := auditProfileImageNames([]imageNameDecl{{
		where:     `стек "dev" (values.dev.yaml):kaname.image.repository`,
		section:   "kaname",
		ref:       "",
		effective: true,
	}})

	require.Len(t, findings, 1)
	require.Contains(t, findings[0], `стек "dev"`)
	require.Contains(t, findings[0], "не называет образа вовсе")
	require.Contains(t, findings[0], `"kaname"`)
}

func TestInjection_PartialOverrideWithoutAnImageIsSilence(t *testing.T) {
	// Отличие РОВНО одно: объявление не действующее, а файловое.
	findings, _ := auditProfileImageNames([]imageNameDecl{{
		where:     "helm/umbrella/values.fe3455-prod.yaml:kaname.image.repository",
		section:   "kaname",
		ref:       "",
		effective: false,
	}})
	require.Empty(t, findings,
		"профиль, накладывающий один тег, находкой не является — на этом дереве таких четыре")
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось 5 — ПЕРЕПИСЬ. «Ноль находок» обязано быть отличимо от «ноль прочитанного»,
// и различие это обязано быть ВИДНО в числах, а не подразумеваться.

func TestInjection_CensusCountsWhatWasRead(t *testing.T) {
	findings, census := auditProfileImageNames([]imageNameDecl{
		{where: "a", section: "kaname", ref: "kaname:dev"},
		{where: "b", section: "", ref: "openpolicyagent/opa:1.0.0"},
		{where: "c", section: "", ref: "docker.io/prorobotech/kacho-vpc:main-abc"},
	})
	require.Empty(t, findings)
	require.Equal(t, 3, census.refs, "перепись обязана считать ВСЕ рассмотренные ссылки")
	require.Equal(t, 2, census.productRefs, "чужой образ частью продукта не считается")
	require.Equal(t, 1, census.workloadPos)
	require.Equal(t, 1, census.sectionParts)
	require.Equal(t, 2, census.canonicalHits)
}

func TestInjection_EmptyInputProducesNoSilentGreen(t *testing.T) {
	// Пустой вход даёт пустую перепись — и именно по ней тест дерева отказывает.
	// Утверждается здесь, потому что «ноль находок на пустом входе» и есть та
	// форма вакуумного зелёного, ради которой перепись заведена.
	findings, census := auditProfileImageNames(nil)
	require.Empty(t, findings)
	require.Zero(t, census.refs)
	require.Zero(t, census.productRefs)
	require.Zero(t, census.sectionParts)
}

// ─────────────────────────────────────────────────────────────────────────────
// Разбор ссылки. Форма записи — не предмет проверки, но распознаватель, не
// знающий одной из форм, даёт не красное и не зелёное, а МОЛЧАНИЕ.

func TestInjection_ImageReferenceIsParsedInEveryFormTheTreeUses(t *testing.T) {
	for ref, want := range map[string]string{
		"kaname":                       "kaname",
		"kaname:dev":                   "kaname",
		"docker.io/prorobotech/kaname": "kaname",
		"docker.io/prorobotech/kaname:main-abc1234":    "kaname",
		"registry.example.invalid:5000/x/kaname:0.1.0": "kaname",
		"docker.io/prorobotech/kaname@sha256:0123abcd": "kaname",
		"openpolicyagent/opa:1.0.0-rootless":           "opa",
		"docker.io/bitnamilegacy/postgresql:16":        "postgresql",
	} {
		got, ok := imageRepoLastSegment(ref)
		require.True(t, ok, "ссылка %q обязана быть разобрана", ref)
		require.Equal(t, want, got, "ссылка %q", ref)
	}
	for _, ref := range []string{"", "   "} {
		_, ok := imageRepoLastSegment(ref)
		require.False(t, ok, "пустая ссылка образом не является: %q", ref)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Предпосылка самой проверки: перечень профилей ВЫВОДИТСЯ обходом дерева.
// Выписанный разошёлся бы с деревом молча — и разошёлся бы там, где завели
// новый профиль.

func TestInjection_ProfileRosterIsDerivedFromTheTree(t *testing.T) {
	profiles := imageNameProfiles(t)
	require.NotEmpty(t, profiles, "обход профилей пуст — предпосылка проверки исчезла")

	var umbrella, charts int
	for _, p := range profiles {
		if strings.HasPrefix(p.path, umbrellaDir) && p.section == "" {
			umbrella++
			require.Equal(t, 1, p.workloadDepth,
				"у профиля умбреллы рабочее место образа лежит под ключом секции")
			continue
		}
		charts++
		require.NotEmpty(t, p.section,
			"у значений чарта секция обязана быть взята из его Chart.yaml (%s)", p.path)
		require.Equal(t, 0, p.workloadDepth,
			"у значений чарта рабочее место образа лежит в корне (%s)", p.path)
	}
	require.NotZero(t, umbrella, "профили умбреллы обязаны быть найдены")
	require.NotZero(t, charts, "значения чартов частей обязаны быть найдены")
}
