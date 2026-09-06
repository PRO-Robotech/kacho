// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// clusteranchorbeyondgo_injection_test.go — доказательство, что разбор написания
// якоря ВНЕ Go способен упасть, падает ТОЛЬКО на своём предмете и называет
// координату.
//
// Инъекция идёт ПО КАЖДОМУ ВИДУ файла, а не по одному образцу: виды чинятся
// по-разному (контракт — генерацией, манифест — рукой, коллекция — своим
// производителем), и распознаватель, ослепший на одном виде, дал бы по нему
// молчание, неотличимое от отсутствия предмета.
//
// Рядом с каждой инъекцией стоит ЗАКОННЫЙ БЛИЗНЕЦ — тот же вид файла с
// написанием, УЖЕ равным объявленному. Без него гейт ловил бы форму, а не
// расхождение, и первое же законное срабатывание его отключило бы.
//
// Отдельно доказано, что при законном близнеце ПЕРЕПИСЬ РАСТЁТ: близнец,
// который гейт не читает вовсе, молчит по той же причине, что и мёртвая
// проверка, и отличить это можно только числом осмотренного.

import (
	"strings"
	"testing"
)

// beyondGoDeclared — написание, объявленное в синтетическом мире.
const beyondGoDeclared = "cluster_kaname_root"

// beyondGoStale — прежнее написание: то, что обязано краснеть.
const beyondGoStale = "cluster_kacho_root"

// beyondGoSubjects — ПРЕДМЕТ инъекции: по одному файлу на КАЖДЫЙ вид, который
// встречается в настоящем дереве, и все они несут ОБЪЯВЛЕННОЕ написание.
//
// Отделены от близнецов намеренно: инъекция «по каждому виду» обязана бить в
// файл, у которого написание ЕСТЬ. Прежняя редакция брала первый файл вида из
// общей карты и на видах `md` и `yaml` попадала в близнеца — то есть в файл,
// где менять нечего, — и объявляла разбор ослепшим там, где он исправен.
func beyondGoSubjects() map[string]string {
	return map[string]string{
		"proto/kacho/cloud/iam/v1/cluster.proto": "" +
			"// Singleton cluster resource (`id = \"" + beyondGoDeclared + "\"`).\n" +
			"message Cluster { string id = 1; }\n",
		"proto/kacho/cloud/iam/v1/fga_model.fga": "" +
			"type cluster\n  relations\n    define system_admin: [user]\n" +
			"# якорь: " + beyondGoDeclared + "\n",
		"services/iam/manifest.yaml": "" +
			"seedGrants:\n  - tierId: " + beyondGoDeclared + "\n",
		"services/iam/internal/migrations/20260907000000_anchor.sql": "" +
			"UPDATE kaname.clusters SET id = '" + beyondGoDeclared + "';\n",
		"gateway/tests/newman/collections/cluster_admin.postman_collection.json": "" +
			"{\"item\":[{\"name\":\"" + beyondGoDeclared + "\"}]}\n",
		"gateway/tests/newman/cases/cluster_admin.py": "" +
			"CLUSTER_ANCHOR = \"" + beyondGoDeclared + "\"\n",
		"ui-future/shared/src/api/cluster.ts": "" +
			"export const CLUSTER_ID = \"" + beyondGoDeclared + "\";\n",
		"ui-future/shared/src/pages/system/ClusterAdminsPage.tsx": "" +
			"const scopeId = \"" + beyondGoDeclared + "\";\n",
		"services/iam/docs/content/api/role.mdx": "" +
			"Роли привязаны к кластеру (`" + beyondGoDeclared + "`).\n",
		"services/iam/MODEL-MANIFEST.md": "" +
			"    tierId: " + beyondGoDeclared + "\n",
		"terraform/modules/iam-access/tests/wiring.tftest.hcl": "" +
			"  scope_id = \"" + beyondGoDeclared + "\"\n",
		"deploy/scripts/seed-nlb-fixtures.sh": "" +
			"SCOPE_ID=" + beyondGoDeclared + "\n",
		"services/iam/schema/module-manifest.schema.json": "" +
			"{\"const\": \"" + beyondGoDeclared + "\"}\n",
	}
}

// beyondGoTwins — ЗАКОННЫЕ БЛИЗНЕЦЫ, обязанные молчать всегда, и двоичный файл,
// обязанный быть СОСЧИТАННЫМ и не прочитанным.
//
//   - прозаическое сокращение переживает переход и остаётся верным;
//   - соседний токен той же приставки якорем не является;
//   - якорь, собранный из частей, образцу не виден by construction;
//   - двоичному файлу текста нет: «ноль по нему» обязано быть отличимо от
//     «его не искали».
func beyondGoTwins() map[string]string {
	return map[string]string{
		"services/iam/docs/engineering/architecture/assignable-roles.md": "" +
			"| `cluster …root` | ✅ | ❌ |\n" +
			"Объект прав — `cluster:root` у чужого продукта, к якорю отношения не имеет.\n",
		"deploy/helm/umbrella/templates/anchor.yaml": "" +
			"  anchor: cluster_{{ .Values.brand }}_root\n",
	}
}

// beyondGoWorld — синтетическое дерево целиком: предметы плюс близнецы.
//
// Мир годен by construction: контроль обязан молчать, и молчать при непустой
// переписи.
func beyondGoWorld() map[string][]byte {
	out := map[string][]byte{}
	for k, v := range beyondGoSubjects() {
		out[k] = []byte(v)
	}
	for k, v := range beyondGoTwins() {
		out[k] = []byte(v)
	}
	out["ui-future/host/public/favicon.ico"] = []byte{0x00, 0x01, 0x02, 0x00}
	return out
}

// beyondGoKinds — виды, по которым идёт инъекция: путь-предмет на каждый.
//
// Перечень ВЫВОДИТСЯ из предметов выше, а не выписывается вторым местом: два
// перечня об одном предмете разошлись бы молча. Выбор пути внутри вида
// детерминирован (наименьший по порядку), иначе прогон судил бы разные файлы в
// разных запусках.
func beyondGoKinds(t *testing.T) map[string]string {
	t.Helper()
	byKind := map[string]string{}
	for path := range beyondGoSubjects() {
		kind := anchorFileKind(path)
		if cur, seen := byKind[kind]; !seen || path < cur {
			byKind[kind] = path
		}
	}
	return byKind
}

func beyondGoRun(
	t *testing.T, world map[string][]byte, ledger []ClusterAnchorBeyondGoException,
) ([]BeyondGoFinding, []BeyondGoLedgerFinding, BeyondGoCensus) {
	t.Helper()
	findings, ledgerFindings, census, err := FindClusterAnchorBeyondGo(world, beyondGoDeclared, ledger)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	return findings, ledgerFindings, census
}

// TestBeyondGoInjection_ControlIsSilent — контроль: целое дерево молчит, и объём
// осмотренного при этом НЕ ноль.
//
// Молчание на пустом обходе не отличалось бы от молчания мёртвой проверки.
func TestBeyondGoInjection_ControlIsSilent(t *testing.T) {
	findings, ledgerFindings, census := beyondGoRun(t, beyondGoWorld(), nil)

	if len(findings) != 0 {
		t.Fatalf("контроль дал расхождения: %+v", findings)
	}
	if len(ledgerFindings) != 0 {
		t.Fatalf("контроль дал находки ведомости: %+v", ledgerFindings)
	}
	if census.FilesRead == 0 || census.Occurrences == 0 || census.FilesWithAnchor == 0 {
		t.Fatalf("прочитано файлов %d, вхождений %d, файлов с якорем %d — "+
			"молчание при пустом обходе ничего не доказывает",
			census.FilesRead, census.Occurrences, census.FilesWithAnchor)
	}
	// Границы обязаны быть СОСЧИТАНЫ, а не подразумеваться.
	if census.FilesBinary != 1 {
		t.Errorf("двоичных файлов сосчитано %d, ожидался 1 — вид, который не читают, "+
			"обязан быть назван числом, иначе «ноль по нему» неотличимо от «его не искали»",
			census.FilesBinary)
	}
	if census.Assembled != 1 {
		t.Errorf("кандидатов «якорь собран из частей» сосчитано %d, ожидался 1", census.Assembled)
	}
	if census.Elided != 1 {
		t.Errorf("прозаических сокращений сосчитано %d, ожидалось 1", census.Elided)
	}
}

// TestBeyondGoInjection_StaleSpellingRedsPerKind — инъекция ПО КАЖДОМУ ВИДУ:
// прежнее написание краснеет и называет файл; вид при этом назван в находке.
//
// Прогонов столько, сколько видов: распознаватель, ослепший на одном виде, дал
// бы по нему молчание, неотличимое от отсутствия предмета.
func TestBeyondGoInjection_StaleSpellingRedsPerKind(t *testing.T) {
	kinds := beyondGoKinds(t)
	if len(kinds) < 12 {
		t.Fatalf("видов в синтетическом мире %d — меньше, чем в дереве (12); "+
			"инъекция «по каждому виду» стала бы инъекцией по части", len(kinds))
	}

	for kind, path := range kinds {
		t.Run(kind, func(t *testing.T) {
			world := beyondGoWorld()
			world[path] = []byte(strings.ReplaceAll(
				string(world[path]), beyondGoDeclared, beyondGoStale))

			findings, _, census := beyondGoRun(t, world, nil)

			if len(findings) != 1 {
				t.Fatalf("находок %d, ожидалась одна: %+v", len(findings), findings)
			}
			f := findings[0]
			if f.Path != path {
				t.Errorf("находка называет %q, ожидался %q — координата обязана вести "+
					"к виновнику, иначе гейт снимут как непонятный", f.Path, path)
			}
			if f.Kind != kind {
				t.Errorf("вид находки %q, ожидался %q", f.Kind, kind)
			}
			if f.Text != beyondGoStale || f.Declared != beyondGoDeclared {
				t.Errorf("находка не называет ОБЕ стороны расхождения: %q против %q",
					f.Text, f.Declared)
			}
			if f.Line < 1 {
				t.Errorf("строка находки %d — координата неполна", f.Line)
			}
			if census.Occurrences == 0 {
				t.Error("перепись пуста при находке — вердикт беспредметен")
			}
		})
	}
}

// TestBeyondGoInjection_RenamedTwinIsSilentAndCensusGrows — ЗАКОННЫЙ БЛИЗНЕЦ по
// каждому виду: файл, уже несущий объявленное написание, молчит — И перепись
// при этом РАСТЁТ.
//
// Без второй половины близнец, которого гейт не читает вовсе, молчал бы по той
// же причине, что мёртвая проверка.
func TestBeyondGoInjection_RenamedTwinIsSilentAndCensusGrows(t *testing.T) {
	base := beyondGoWorld()
	_, _, baseCensus := beyondGoRun(t, base, nil)

	for kind, path := range beyondGoKinds(t) {
		t.Run(kind, func(t *testing.T) {
			world := beyondGoWorld()
			twin := strings.Replace(path, anchorFileKind(path), "twin."+anchorFileKind(path), 1)
			world[twin] = world[path]

			findings, _, census := beyondGoRun(t, world, nil)

			if len(findings) != 0 {
				t.Fatalf("законный близнец покраснел: %+v — гейт ловит форму, а не расхождение",
					findings)
			}
			if census.Occurrences <= baseCensus.Occurrences {
				t.Fatalf("перепись вхождений не выросла (%d против %d) — близнец не прочитан, "+
					"и его молчание ничего не доказывает",
					census.Occurrences, baseCensus.Occurrences)
			}
			if census.OccurrencesByKind[kind] <= baseCensus.OccurrencesByKind[kind] {
				t.Errorf("перепись по виду %q не выросла (%d против %d)",
					kind, census.OccurrencesByKind[kind], baseCensus.OccurrencesByKind[kind])
			}
		})
	}
}

// TestBeyondGoInjection_DiacriticFormIsSeen — распознаватель знает ОБЕ латинские
// формы имени платформы.
//
// Поиск по одной недобирает МОЛЧА: недостача приходится ровно на прозу, где
// предмет объясняли словами, а не координатой.
func TestBeyondGoInjection_DiacriticFormIsSeen(t *testing.T) {
	world := beyondGoWorld()
	world["services/iam/docs/content/api/role.mdx"] = []byte(
		"Роли привязаны к кластеру (`cluster_kachō_root`).\n")

	findings, _, _ := beyondGoRun(t, world, nil)

	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна — диакритическая форма имени платформы "+
			"осталась невидимой, и «ноль находок» по ней означает «ноль прочитанного»: %+v",
			len(findings), findings)
	}
	if !strings.Contains(findings[0].Text, "kachō") {
		t.Errorf("находка не несёт диакритической формы: %q", findings[0].Text)
	}
}

// TestBeyondGoInjection_LedgerForgivesExactlyItsSubject — ведомость прощает
// названное вхождение и НЕ прощает соседнее.
func TestBeyondGoInjection_LedgerForgivesExactlyItsSubject(t *testing.T) {
	world := beyondGoWorld()
	world["services/iam/MODEL-MANIFEST.md"] = []byte("    tierId: " + beyondGoStale + "\n")
	world["services/iam/manifest.yaml"] = []byte("seedGrants:\n  - tierId: " + beyondGoStale + "\n")

	ledger := []ClusterAnchorBeyondGoException{
		{Path: "services/iam/MODEL-MANIFEST.md", Count: 1, Reason: "запись замера приёмки"},
	}
	findings, ledgerFindings, census := beyondGoRun(t, world, ledger)

	if len(ledgerFindings) != 0 {
		t.Fatalf("запись с живым предметом объявлена находкой: %+v", ledgerFindings)
	}
	if census.Forgiven != 1 {
		t.Errorf("прощено %d вхождений, ожидалось 1", census.Forgiven)
	}
	if len(findings) != 1 || findings[0].Path != "services/iam/manifest.yaml" {
		t.Fatalf("ведомость прощает шире своего предмета: %+v", findings)
	}
}

// TestBeyondGoInjection_LedgerWithNothingToForgiveIsAFinding — запись, которой
// нечего прощать, — НАХОДКА.
//
// Без этого ведомость не истекала бы: запись пережила бы свой предмет и
// продолжила прощать вперёд ту находку, ради которой гейт заведён.
func TestBeyondGoInjection_LedgerWithNothingToForgiveIsAFinding(t *testing.T) {
	cases := map[string]ClusterAnchorBeyondGoException{
		"файла нет в обходе": {
			Path:  "services/iam/docs/engineering/acceptance/never-existed.md",
			Count: 1, Reason: "запись пережила свой файл",
		},
		"файл есть, расхождений в нём ноль": {
			Path: "services/iam/manifest.yaml", Count: 1,
			Reason: "написание в файле уже сошлось с объявленным",
		},
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			findings, ledgerFindings, _ := beyondGoRun(
				t, beyondGoWorld(), []ClusterAnchorBeyondGoException{entry})

			if len(findings) != 0 {
				t.Errorf("инъекция уронила соседнее свойство: %+v — "+
					"инъекция обязана ронять только своё", findings)
			}
			if len(ledgerFindings) != 1 {
				t.Fatalf("находок ведомости %d, ожидалась одна: %+v", len(ledgerFindings), ledgerFindings)
			}
			if ledgerFindings[0].Path != entry.Path {
				t.Errorf("находка называет %q, ожидался %q", ledgerFindings[0].Path, entry.Path)
			}
			if ledgerFindings[0].Why == "" {
				t.Error("находка не называет, ЧЕМ запись негодна — читатель снимет гейт как непонятный")
			}
		})
	}
}

// TestBeyondGoInjection_LedgerCeilingIsAFinding — число в записи ТОЧНОЕ, а не
// потолок.
//
// Потолок не краснеет никогда, поэтому не истекает и прощает вперёд ту находку,
// ради которой гейт заведён.
func TestBeyondGoInjection_LedgerCeilingIsAFinding(t *testing.T) {
	world := beyondGoWorld()
	world["services/iam/MODEL-MANIFEST.md"] = []byte(
		"    tierId: " + beyondGoStale + "\n    scopeId: " + beyondGoStale + "\n")

	ledger := []ClusterAnchorBeyondGoException{
		{Path: "services/iam/MODEL-MANIFEST.md", Count: 5, Reason: "потолок «с запасом»"},
	}
	findings, ledgerFindings, census := beyondGoRun(t, world, ledger)

	if len(ledgerFindings) != 1 {
		t.Fatalf("находок ведомости %d, ожидалась одна: %+v", len(ledgerFindings), ledgerFindings)
	}
	if ledgerFindings[0].Want != 5 || ledgerFindings[0].Got != 2 {
		t.Errorf("находка не называет обе величины: записано %d, в дереве %d",
			ledgerFindings[0].Want, ledgerFindings[0].Got)
	}
	// Разошедшаяся запись не прощает НИЧЕГО: иначе она прятала бы координаты —
	// те самые строки, ради которых гейт заведён.
	if census.Forgiven != 0 {
		t.Errorf("прощено %d вхождений — устаревшая запись продолжает прятать места",
			census.Forgiven)
	}
	if len(findings) != 2 {
		t.Errorf("названо %d вхождений, ожидалось 2 — читатель получил одно число "+
			"вместо перечня мест: %+v", len(findings), findings)
	}
}

// TestBeyondGoInjection_PremiseFailuresAreRefusals — предпосылки гейта: он
// ОТКАЗЫВАЕТ на беспредметности, а не молчит.
//
// Молчание здесь было бы худшим исходом: «расхождений ноль» тривиально верно на
// дереве, которого не читали, и на образце, который предмета не узнаёт.
func TestBeyondGoInjection_PremiseFailuresAreRefusals(t *testing.T) {
	t.Run("написание не объявлено", func(t *testing.T) {
		_, _, _, err := FindClusterAnchorBeyondGo(beyondGoWorld(), "", nil)
		if err == nil {
			t.Fatal("гейт промолчал без объявленного написания")
		}
	})

	t.Run("объявленное написание образцу не подходит", func(t *testing.T) {
		_, _, _, err := FindClusterAnchorBeyondGo(beyondGoWorld(), "kaname_anchor", nil)
		if err == nil {
			t.Fatal("гейт промолчал на написании, которого его образец не узнаёт, — " +
				"он ослеп бы на весь предмет, оставаясь на вид рабочим")
		}
		if !strings.Contains(err.Error(), "kaname_anchor") {
			t.Errorf("отказ не называет объявленного написания: %v", err)
		}
	})

	t.Run("обход пуст", func(t *testing.T) {
		_, _, _, err := FindClusterAnchorBeyondGo(map[string][]byte{}, beyondGoDeclared, nil)
		if err == nil {
			t.Fatal("гейт промолчал на пустом обходе")
		}
	})

	t.Run("предмет не найден", func(t *testing.T) {
		world := map[string][]byte{
			"services/iam/manifest.yaml": []byte("seedGrants: []\n"),
		}
		_, _, census, err := FindClusterAnchorBeyondGo(world, beyondGoDeclared, nil)
		if err == nil {
			t.Fatal("гейт промолчал, не найдя ни одного вхождения — " +
				"«расхождений ноль» здесь тривиально верно")
		}
		if census.FilesRead != 1 {
			t.Errorf("перепись прочитанного %d, ожидалась 1 — отказ обязан назвать объём", census.FilesRead)
		}
	})
}
