// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// kanamenameresidue_injection_test.go — доказательство, что держатель остатка
// имени способен упасть, падает ТОЛЬКО на своём предмете, называет координату и
// читает ОБЕ латинские формы записи имени.
//
// Инъекция идёт ПО КАЖДОЙ полосе, а не по одному образцу: полосы чинятся
// по-разному (контракт — генерацией, схема — новой миграцией, витрина — рукой),
// и распознаватель, ослепший на одной, дал бы по ней молчание, неотличимое от
// отсутствия предмета.
//
// Каждая инъекция меняет РОВНО ОДИН факт против общего мира: прибавляет одно
// вхождение своей полосы. Прогонов поэтому три, а не два: контроль (мир как
// есть — все полосы на своих числах) · инъекция полосы (растёт ТОЛЬКО она) ·
// инъекция законного близнеца (не растёт НИЧТО). Без третьего молчание
// держателя на своём имени продукта неотличимо от молчания мёртвой проверки.

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// nameResidueSubjects — ПРЕДМЕТ инъекции: по одному файлу на каждую полосу,
// каждый несёт РОВНО ОДНО вхождение имени платформы своей формы.
//
// Ключ — полоса, поэтому перечень полос под инъекцией ВЫВОДИТСЯ отсюда, а не
// выписывается вторым местом: два перечня об одном предмете разошлись бы молча.
func nameResidueSubjects() map[string]struct{ Path, Body string } {
	return map[string]struct{ Path, Body string }{
		laneContractCoordinate: {
			"proto/kacho/cloud/iam/v1/probe_service.proto",
			"package kacho.cloud.iam.v1;\n",
		},
		laneSchemaName: {
			"services/iam/internal/migrations/20260907000000_probe.sql",
			"SET search_path TO kacho_iam, public;\n",
		},
		laneDatabaseName: {
			"services/iam/deploy/probe-values.yaml",
			"dsn: postgres://host/kacho_iam\n",
		},
		laneQualifiedTable: {
			"services/iam/internal/repo/kaname/pg/probe_repo.go",
			"const q = `SELECT id FROM kacho_iam.users`\n",
		},
		laneEnvKnob: {
			"services/iam/docs/content/install/probe.mdx",
			"Задайте `KACHO_IAM_DB_HOST`.\n",
		},
		laneChartKnob: {
			"deploy/helm/umbrella/charts/kaname/templates/probe.yaml",
			"  enabled: .Values.global.kacho.identity.enabled\n",
		},
		laneClaimAssertion: {
			"services/iam/internal/apps/kaname/api/probe/claims.go",
			"const claim = \"kacho_principal_id\"\n",
		},
		laneIdentityHeader: {
			"services/iam/internal/apps/kaname/api/probe/headers.go",
			"const header = \"x-kacho-principal-id\"\n",
		},
		laneClusterAnchor: {
			"services/iam/MODEL-MANIFEST-probe.md",
			"| tierId | cluster_kacho_root |\n",
		},
		laneSchemaPrefixKin: {
			"services/iam/internal/observability/metrics/probe.go",
			"const channel = \"kacho_iam_subject_outbox_added\"\n",
		},
		laneDomainAddress: {
			"services/iam/docs/content/api/probe.mdx",
			"Адрес края — `https://api.kacho.cloud/iam/v1`.\n",
		},
		laneObjectName: {
			"services/iam/deploy/probe-image.yaml",
			"image: kacho-migrator:latest\n",
		},
		laneBrandInText: {
			"services/iam/docs/content/probe-brand.mdx",
			"Платформа Kachō остаётся владельцем консоли.\n",
		},
		lanePlatformName: {
			"services/iam/deploy/probe-namespace.yaml",
			"namespace: kacho\n",
		},
		borderUnknownForm: {
			"services/iam/docs/engineering/probe-unknown.md",
			"Приставка клейм записывается как kacho_ и дальше имя.\n",
		},
	}
}

// nameResidueBorderTwins — ЗАКОННЫЕ близнецы, которые обязаны быть СОСЧИТАНЫ
// границей и не признаны ни одной осью.
//
// Без них держатель ловил бы форму, а не предмет: путь модуля фундамента, чужой
// контракт, чужой модуль, одобренная приёмка и функция общего фундамента несут
// имя платформы законно, и первое же срабатывание на них его бы отключило.
func nameResidueBorderTwins() map[string]struct{ Path, Body string } {
	return map[string]struct{ Path, Body string }{
		borderFoundationModule: {
			"services/iam/internal/apps/kaname/api/probe/import.go",
			"import \"github.com/PRO-Robotech/kacho/pkg/ids\"\n",
		},
		borderFoundationContract: {
			"proto/kacho/cloud/iam/v1/probe_import.proto",
			"import \"kacho/cloud/operation/operation.proto\";\n",
		},
		borderForeignModule: {
			"services/iam/docs/engineering/probe-edges.md",
			"Ребро к kacho-vpc остаётся односторонним.\n",
		},
		borderApprovedAcceptance: {
			"services/iam/docs/engineering/acceptance/probe-acceptance.md",
			"Замер: `git grep kacho_iam.roles` даёт 64.\n",
		},
		borderFoundationFunction: {
			"services/iam/internal/repo/kaname/pg/probe_quota.go",
			"const fn = \"kacho_quota_refuse\"\n",
		},
	}
}

// nameResidueOwnNameTwin — файл, где продукт называет себя СВОИМ именем.
// Держатель обязан молчать о нём целиком: ни одной полосы, ни одной границы.
const nameResidueOwnNameTwinPath = "services/iam/internal/apps/kaname/probe_own_name.go"

const nameResidueOwnNameTwinBody = "" +
	"// Продукт называет себя своим именем: kaname.\n" +
	"const schema = \"kaname\"\n" +
	"const claim = \"kaname_principal_id\"\n" +
	"const header = \"x-kaname-principal-id\"\n" +
	"const image = \"kaname-migrator:latest\"\n" +
	"const addr = \"https://api.kaname.cloud/iam/v1\"\n"

// nameResidueWorld — синтетическое дерево целиком.
func nameResidueWorld() map[string][]byte {
	out := map[string][]byte{}
	for _, s := range nameResidueSubjects() {
		out[s.Path] = []byte(s.Body)
	}
	for _, s := range nameResidueBorderTwins() {
		out[s.Path] = []byte(s.Body)
	}
	out[nameResidueOwnNameTwinPath] = []byte(nameResidueOwnNameTwinBody)
	out["services/iam/probe-binary.bin"] = []byte{0x00, 0x01, 0x02}
	return out
}

// nameResidueLanes — вердикт держателя на заданном мире: полоса → вхождений.
func nameResidueLanes(t *testing.T, world map[string][]byte) map[string]int {
	t.Helper()
	_, ledgerFindings, census, err := FindKanameNameResidue(world, nil, nil)
	if err != nil {
		t.Fatalf("разбор синтетического мира: %v", err)
	}
	if len(ledgerFindings) != 0 {
		t.Fatalf("на мире без ведомостей находок ведомости быть не может, их %d",
			len(ledgerFindings))
	}
	out := map[string]int{}
	for lane := range kanameLanes {
		out[lane] = census.FoundByLane[lane]
	}
	return out
}

// TestKanameNameResidueControlWorldIsSound — контроль: в мире по одному
// вхождению на полосу каждая полоса даёт РОВНО ЕДИНИЦУ.
//
// Прогон первый из трёх. Без него «инъекция покрасила полосу» ничего не
// доказывает: полоса могла краснеть и до неё.
func TestKanameNameResidueControlWorldIsSound(t *testing.T) {
	got := nameResidueLanes(t, nameResidueWorld())
	subjects := nameResidueSubjects()
	twins := nameResidueBorderTwins()

	for lane := range kanameLanes {
		want := 0
		if _, isSubject := subjects[lane]; isSubject {
			want = 1
		}
		if _, isTwin := twins[lane]; isTwin {
			want = 1
		}
		if got[lane] != want {
			t.Errorf("контроль: полоса %q даёт %d вхождений, ожидалось %d — "+
				"мир негоден, и всякая инъекция по нему недоказательна",
				lane, got[lane], want)
		}
	}

	// Каждая полоса обязана иметь предмет ЛИБО в перечне предметов, ЛИБО в
	// перечне близнецов границы: полоса без своего файла в синтетике не
	// доказана, и её молчание неотличимо от молчания мёртвой проверки.
	for lane, lg := range kanameLanes {
		_, isSubject := subjects[lane]
		_, isTwin := twins[lane]
		if !isSubject && !isTwin {
			t.Errorf("полоса %q (ось %q) не имеет предмета в синтетическом мире — "+
				"способность держателя упасть на ней НЕ доказана", lane, lg.Axis)
		}
	}
	t.Logf("полос %d; предметов %d; близнецов границы %d",
		len(kanameLanes), len(subjects), len(twins))
}

// TestKanameNameResidueInjectionRaisesOnlyItsOwnLane — прогон второй из трёх:
// внесённое вхождение поднимает ТОЛЬКО свою полосу.
//
// Инъекция обязана ронять лишь проверяемое. Форма «завести ещё один файл со
// своим вхождением» выбрана потому, что она меняет ровно один факт: у мира
// прибавляется одно вхождение известной формы, и больше ничего.
func TestKanameNameResidueInjectionRaisesOnlyItsOwnLane(t *testing.T) {
	base := nameResidueLanes(t, nameResidueWorld())

	for lane, subject := range nameResidueSubjects() {
		t.Run(lane, func(t *testing.T) {
			world := nameResidueWorld()
			// Файл-близнец кладётся В ТОТ ЖЕ каталог, что и предмет: часть полос
			// (ключ профиля и шаблона) читается только внутри чарта оператора, и
			// инъекция «куда попало» измеряла бы место, а не форму.
			world[nameResidueSiblingPath(subject.Path)] = []byte(subject.Body)
			got := nameResidueLanes(t, world)

			if got[lane] != base[lane]+1 {
				t.Fatalf("инъекция формы полосы %q не подняла её: было %d, стало %d — "+
					"распознаватель этой формы НЕ читает, и по ней «ноль находок» "+
					"означает «ноль прочитанного»", lane, base[lane], got[lane])
			}
			for other := range kanameLanes {
				if other == lane {
					continue
				}
				if got[other] != base[other] {
					t.Errorf("инъекция полосы %q сдвинула ЧУЖУЮ полосу %q (%d → %d) — "+
						"красное пришло бы от соседа, и доказательство недействительно",
						lane, other, base[other], got[other])
				}
			}
		})
	}
}

// nameResidueSiblingPath — путь файла-близнеца рядом с предметом: то же имя с
// пометкой, тот же каталог и то же расширение.
func nameResidueSiblingPath(path string) string {
	dot := strings.LastIndex(path, ".")
	slash := strings.LastIndex(path, "/")
	if dot <= slash {
		return path + "-injected"
	}
	return path[:dot] + "-injected" + path[dot:]
}

// TestKanameNameResidueOwnProductNameStaysSilent — прогон третий из трёх:
// законный близнец, где продукт называет себя СВОИМ именем, не двигает ничего.
//
// Без него держатель ловил бы «слово похожей формы», а не имя платформы, и
// первое же срабатывание на собственном имени продукта его бы отключило.
func TestKanameNameResidueOwnProductNameStaysSilent(t *testing.T) {
	base := nameResidueLanes(t, nameResidueWorld())

	world := nameResidueWorld()
	world[nameResidueSiblingPath(nameResidueOwnNameTwinPath)] = []byte(nameResidueOwnNameTwinBody)
	got := nameResidueLanes(t, world)

	if strings.Contains(strings.ToLower(nameResidueOwnNameTwinBody), "kach") {
		t.Fatalf("близнец сам несёт имя платформы — он не близнец, а предмет")
	}
	for lane := range kanameLanes {
		if got[lane] != base[lane] {
			t.Errorf("файл, где продукт зовётся СВОИМ именем, сдвинул полосу %q "+
				"(%d → %d) — держатель ловит форму, а не предмет", lane, base[lane], got[lane])
		}
	}
}

// TestKanameNameResidueReadsTheMacronFormAnAsciiPredicateMisses — доказательство
// по каждой ФОРМЕ записи отдельно.
//
// Ради этой пробы держатель и написан посимвольно: односторонний предикат
// теряет диакритическую форму МОЛЧА, и потеря приходится на прозу — туда, где
// бренд платформы стоит как своё имя продукта.
func TestKanameNameResidueReadsTheMacronFormAnAsciiPredicateMisses(t *testing.T) {
	const macronOnly = "Консоль остаётся частью Kachō.\n"
	const asciiOnly = "Консоль остаётся частью Kacho.\n"

	// Предпосылка пробы: односторонний предикат этого текста НЕ видит.
	if strings.Contains(strings.ToLower(macronOnly), "kacho") {
		t.Fatal("фикстура диакритической формы содержит и обычную — проба измеряла бы не то")
	}
	if !strings.Contains(strings.ToLower(asciiOnly), "kacho") {
		t.Fatal("фикстура обычной формы обычной формы не содержит — мир негоден")
	}

	base := nameResidueWorld()
	_, _, censusBase, err := FindKanameNameResidue(base, nil, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	for name, body := range map[string]string{
		"обычная форма":        asciiOnly,
		"диакритическая форма": macronOnly,
	} {
		world := nameResidueWorld()
		world["services/iam/docs/content/probe-form.mdx"] = []byte(body)
		_, _, census, err := FindKanameNameResidue(world, nil, nil)
		if err != nil {
			t.Fatalf("%s: разбор: %v", name, err)
		}
		if census.Occurrences != censusBase.Occurrences+1 {
			t.Errorf("%s: вхождений было %d, стало %d — форма осталась вне наблюдения",
				name, censusBase.Occurrences, census.Occurrences)
		}
	}

	world := nameResidueWorld()
	world["services/iam/docs/content/probe-form.mdx"] = []byte(macronOnly)
	_, _, census, err := FindKanameNameResidue(world, nil, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if census.OccurrencesMacron != censusBase.OccurrencesMacron+1 {
		t.Errorf("диакритических вхождений было %d, стало %d",
			censusBase.OccurrencesMacron, census.OccurrencesMacron)
	}
	if census.FilesMacronOnly != censusBase.FilesMacronOnly+1 {
		t.Errorf("файлов, невидимых ASCII-предикату, было %d, стало %d — "+
			"перепись слепой зоны не растёт, и её число ничего не измеряет",
			censusBase.FilesMacronOnly, census.FilesMacronOnly)
	}
}

// TestKanameNameResidueStayLedgerExpiresWithItsSubject — ведомость решённого
// остаться прощает точное число и истекает вместе с предметом.
func TestKanameNameResidueStayLedgerExpiresWithItsSubject(t *testing.T) {
	subject := nameResidueSubjects()[laneSchemaName]

	cases := []struct {
		name   string
		stay   []NameResidueStay
		want   string
		silent bool
	}{
		{
			name: "число сошлось — прощает",
			stay: []NameResidueStay{{
				Path: subject.Path, Lane: laneSchemaName, Count: 1,
				Reason: "предмет файла есть само переименование",
			}},
			silent: true,
		},
		{
			name: "число разошлось — находка",
			stay: []NameResidueStay{{
				Path: subject.Path, Lane: laneSchemaName, Count: 2,
				Reason: "предмет файла есть само переименование",
			}},
			want: "ведомость разошлась с фактом",
		},
		{
			name: "предмета нет — находка",
			stay: []NameResidueStay{{
				Path: "services/iam/probe-absent.go", Lane: laneSchemaName, Count: 1,
				Reason: "предмет файла есть само переименование",
			}},
			want: "пережила свой предмет",
		},
		{
			name: "полоса другая — предмета нет, находка",
			stay: []NameResidueStay{{
				Path: subject.Path, Lane: laneClaimAssertion, Count: 1,
				Reason: "предмет файла есть само переименование",
			}},
			want: "пережила свой предмет",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ledgerFindings, census, err := FindKanameNameResidue(nameResidueWorld(), tc.stay, nil)
			if err != nil {
				t.Fatalf("разбор: %v", err)
			}
			if tc.silent {
				if len(ledgerFindings) != 0 {
					t.Fatalf("ведомость с сошедшимся числом дала находки: %+v", ledgerFindings)
				}
				if census.ForgivenByLane[laneSchemaName] != 1 {
					t.Fatalf("прощено %d вхождений вместо одного",
						census.ForgivenByLane[laneSchemaName])
				}
				if census.FoundByLane[laneSchemaName] != 0 {
					t.Fatalf("прощённое вхождение осталось находкой (%d)",
						census.FoundByLane[laneSchemaName])
				}
				return
			}
			if len(ledgerFindings) == 0 {
				t.Fatalf("ведомость обязана была дать находку %q, а промолчала", tc.want)
			}
			joined := ""
			for _, lf := range ledgerFindings {
				joined += lf.Why + "\n"
			}
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("находка не называет причину %q; получено:\n%s", tc.want, joined)
			}
		})
	}
}

// TestKanameNameResidueDebtLedgerIsExactInBothDirections — ведомость остатка
// краснеет и на РОСТЕ, и на СНИЖЕНИИ.
//
// Снижение — не поломка, а сигнал, что ведомость отстала: опустить число обязано
// то же изменение, которое остаток сняло. Потолок (`не больше чем`) запрещён:
// он не краснеет никогда и потому не истекает.
func TestKanameNameResidueDebtLedgerIsExactInBothDirections(t *testing.T) {
	world := nameResidueWorld()
	base := nameResidueLanes(t, world)

	exact := []NameResidueDebt{{
		Lane: laneClaimAssertion, Occurrences: base[laneClaimAssertion], Files: 1,
		Owner: "проба",
	}}

	t.Run("сошлось — молчит", func(t *testing.T) {
		_, ledgerFindings, _, err := FindKanameNameResidue(world, nil, exact)
		if err != nil {
			t.Fatalf("разбор: %v", err)
		}
		for _, lf := range ledgerFindings {
			if lf.Lane == laneClaimAssertion {
				t.Fatalf("сошедшаяся строка дала находку: %+v", lf)
			}
		}
	})

	t.Run("вырос — находка", func(t *testing.T) {
		grown := nameResidueWorld()
		claim := nameResidueSubjects()[laneClaimAssertion]
		grown[nameResidueSiblingPath(claim.Path)] = []byte(claim.Body)
		_, ledgerFindings, _, err := FindKanameNameResidue(grown, nil, exact)
		if err != nil {
			t.Fatalf("разбор: %v", err)
		}
		if !ledgerFindingSays(ledgerFindings, laneClaimAssertion, "остаток ВЫРОС") {
			t.Fatalf("рост остатка не назван находкой: %+v", ledgerFindings)
		}
	})

	t.Run("снизился, ведомость отстала — находка", func(t *testing.T) {
		stale := []NameResidueDebt{{
			Lane: laneClaimAssertion, Occurrences: base[laneClaimAssertion] + 5, Files: 1,
			Owner: "проба",
		}}
		_, ledgerFindings, _, err := FindKanameNameResidue(world, nil, stale)
		if err != nil {
			t.Fatalf("разбор: %v", err)
		}
		if !ledgerFindingSays(ledgerFindings, laneClaimAssertion, "ведомость отстала") {
			t.Fatalf("отставшая ведомость не названа находкой: %+v", ledgerFindings)
		}
	})

	t.Run("полоса с остатком без строки — находка", func(t *testing.T) {
		_, ledgerFindings, _, err := FindKanameNameResidue(world, nil, exact)
		if err != nil {
			t.Fatalf("разбор: %v", err)
		}
		if !ledgerFindingSays(ledgerFindings, laneSchemaName, "строки в ведомости у неё НЕТ") {
			t.Fatalf("незаписанный остаток не назван находкой: %+v", ledgerFindings)
		}
	})
}

func ledgerFindingSays(findings []NameResidueLedgerFinding, lane, want string) bool {
	for _, lf := range findings {
		if lf.Lane == lane && strings.Contains(lf.Why, want) {
			return true
		}
	}
	return false
}

// TestKanameNameResidueRefusesOnBeingBeyondItsPredicate — держатель ОТКАЗЫВАЕТ
// там, где вердикт был бы беспредметен, а не выдаёт тихий ноль.
func TestKanameNameResidueRefusesOnBeingBeyondItsPredicate(t *testing.T) {
	cases := []struct {
		name  string
		world map[string][]byte
		stay  []NameResidueStay
		debt  []NameResidueDebt
		want  string
	}{
		{
			name:  "обход пуст",
			world: map[string][]byte{},
			want:  "ноль файлов",
		},
		{
			name: "предмета в дереве нет",
			world: map[string][]byte{
				"services/iam/probe.go": []byte("const product = \"kaname\"\n"),
			},
			want: "НИ ОДНОГО вхождения",
		},
		{
			name:  "ведомость называет несуществующую полосу",
			world: nameResidueWorld(),
			stay: []NameResidueStay{{
				Path: "services/iam/probe.go", Lane: "такой полосы нет", Count: 1, Reason: "проба",
			}},
			want: "которой у распознавателя нет",
		},
		{
			name:  "ведомость прощает ноль вхождений",
			world: nameResidueWorld(),
			stay: []NameResidueStay{{
				Path: "services/iam/probe.go", Lane: laneSchemaName, Count: 0, Reason: "проба",
			}},
			want: "нечего прощать by construction",
		},
		{
			name:  "ведомость без причины",
			world: nameResidueWorld(),
			stay: []NameResidueStay{{
				Path: "services/iam/probe.go", Lane: laneSchemaName, Count: 1, Reason: "  ",
			}},
			want: "без причины",
		},
		{
			name:  "ведомость называет пару дважды",
			world: nameResidueWorld(),
			stay: []NameResidueStay{
				{Path: "services/iam/probe.go", Lane: laneSchemaName, Count: 1, Reason: "проба"},
				{Path: "services/iam/probe.go", Lane: laneSchemaName, Count: 2, Reason: "проба"},
			},
			want: "объявлена дважды",
		},
		{
			name:  "строка остатка без владельца",
			world: nameResidueWorld(),
			debt:  []NameResidueDebt{{Lane: laneSchemaName, Occurrences: 1, Files: 1}},
			want:  "не называет владельца",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := FindKanameNameResidue(tc.world, tc.stay, tc.debt)
			if err == nil {
				t.Fatalf("держатель промолчал там, где вердикт беспредметен; ждали отказ про %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("отказ не называет предмет %q: %v", tc.want, err)
			}
		})
	}
}

// TestKanameNameResidueEveryAxisHasASubjectInTheInjection — перечень осей под
// инъекцией ВЫВОДИТСЯ из перечня полос, а не выписывается по памяти.
//
// Появись седьмая ось — доказательства по ней не было бы, и её молчание не
// отличалось бы от молчания мёртвой проверки.
func TestKanameNameResidueEveryAxisHasASubjectInTheInjection(t *testing.T) {
	subjects := nameResidueSubjects()
	covered := map[NameResidueAxis]int{}
	for lane := range subjects {
		covered[kanameLanes[lane].Axis]++
	}
	for _, axis := range KanameAxes {
		if covered[axis] == 0 {
			t.Errorf("ось %q не имеет ни одного предмета в инъекции — способность "+
				"держателя упасть на ней НЕ доказана", axis)
		}
	}
	var names []string
	for _, axis := range KanameAxes {
		names = append(names, fmt.Sprintf("%s: %d", axis, covered[axis]))
	}
	sort.Strings(names)
	t.Logf("предметов инъекции по осям — %s", strings.Join(names, "; "))
	if len(KanameAxes) != 6 {
		t.Fatalf("осей объявлено %d, а директива владельца называет шесть", len(KanameAxes))
	}
}
