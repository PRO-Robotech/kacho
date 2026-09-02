// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_pair_agreement_test.go — секция `manifests` профиля есть
// ПАРА, и профиль не вправе объявить её половиной (задача #1924).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Страж старта службы отвергает неполную пару в обе стороны
// (`ManifestsConfig.Validate`): «опираемся и не сказали, откуда читать» и
// «сказали откуда и объявили, что не опираемся». Это верное место для отказа —
// и САМОЕ ДОРОГОЕ для его обнаружения: половина пары, уехавшая в профиль,
// доживает до kubelet и проявляется отказом старта на поднимаемой площадке.
//
// Здесь та же пара судится ПО ОБЪЯВЛЕНИЮ, до всякого рендера и до всякого
// кластера: профиль читается тем же порядком слияния, каким его читает helm —
// последнее объявление в цепочке выигрывает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПО ОБЪЯВЛЕНИЮ, А НЕ ПО РЕНДЕРУ
//
// Рендер требует helm, а его в харнессе нет; сверх того рендер собирается из
// упакованных зависимостей, и скопированный из соседнего клона `.tgz` дал бы
// вердикт о ЧУЖОМ дереве. Объявление профиля — то, что правит человек, и то,
// что уезжает в поставку.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТА ПРОВЕРКА НЕ СУДИТ
//
// Она не требует доставки НИ ОТ ОДНОГО стенда: «доставка не заведена» —
// решение посадки, и оно законно (так же считает проверка производителя). Она
// судит только СОГЛАСИЕ двух половин между собой.

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// manifestPairDecl — то, что цепочка профилей сказала о паре.
//
// Признак объявленности хранится отдельно от значения: «ключ не объявлен» и
// «объявлен пустым» — разные утверждения, и второе есть решение посадки.
type manifestPairDecl struct {
	Name         string
	NameSeen     bool
	Required     bool
	RequiredSeen bool
}

// mergeManifestPairDecl читает цепочку профилей ТЕМ ЖЕ порядком, каким её
// сливает helm: последнее объявление ключа выигрывает.
//
// Профили передаются телами, а не путями, чтобы проверку можно было прогнать на
// синтетике: доказательство способности упасть не вправе зависеть от того, что
// сегодня лежит в дереве.
func mergeManifestPairDecl(bodies [][]byte) (manifestPairDecl, error) {
	var out manifestPairDecl
	for _, raw := range bodies {
		var tree map[string]any
		if err := yaml.Unmarshal(raw, &tree); err != nil {
			return out, fmt.Errorf("профиль не разобран: %w", err)
		}
		if v, ok := nestedString(tree, "kacho-iam", "manifests", "configMapName"); ok {
			out.Name, out.NameSeen = v, true
		}
		if v, ok := nestedString(tree, "kacho-iam", "manifests", "required"); ok {
			out.Required, out.RequiredSeen = v == "true", true
		}
	}
	return out, nil
}

// manifestPairFinding — половина пары, названная стендом и стороной.
type manifestPairFinding struct {
	Stack string
	Text  string
}

// auditManifestPairAgreement — судья пары. Отдельная функция, а не тело пробы:
// её зовут и обход дерева, и инъекция синтетикой.
//
// Умолчание чарта (`configMapName: ""`, `required: false`) согласовано, поэтому
// цепочка, не объявившая НИ ОДНОЙ половины, находкой не является: она наследует
// согласованную пару целиком.
func auditManifestPairAgreement(stacks map[string][][]byte) ([]manifestPairFinding, string, error) {
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var findings []manifestPairFinding
	declaring, profiles := 0, 0
	for _, stack := range names {
		profiles += len(stacks[stack])
		decl, err := mergeManifestPairDecl(stacks[stack])
		if err != nil {
			return nil, "", fmt.Errorf("стенд %s: %w", stack, err)
		}
		if decl.Name != "" {
			declaring++
		}
		switch {
		case decl.Name != "" && !decl.Required:
			findings = append(findings, manifestPairFinding{Stack: stack, Text: fmt.Sprintf(
				"стенд %s: `manifests.configMapName` назван (%s), а `manifests.required` "+
					"не объявлен — половина пары. Страж старта службы отвергнет такую посадку "+
					"на kubelet: чтение включает сам каталог, и сорванную доставку процесс "+
					"отвергает независимо от `required`, поэтому объявленное «не опираемся» "+
					"не исполняется ничем. Либо `required: true`, либо имя пусто (kacho#1924)",
				stack, decl.Name)})
		case decl.Name == "" && decl.Required:
			findings = append(findings, manifestPairFinding{Stack: stack, Text: fmt.Sprintf(
				"стенд %s: `manifests.required` объявлен, а `manifests.configMapName` пуст — "+
					"половина пары. Посадка опирается на манифесты модулей и не сказала, "+
					"откуда их читать; страж старта откажет в пуске (kacho#1875)", stack)})
		}
	}
	census := fmt.Sprintf("осмотрено: стендов %d, профилей в цепочках %d, "+
		"доставку объявляют %d; половин пары %d", len(names), profiles, declaring, len(findings))
	return findings, census, nil
}

// treeManifestPairStacks — цепочки стендов ТЕЛАМИ профилей, прочитанными из
// дерева.
func treeManifestPairStacks(t *testing.T) map[string][][]byte {
	t.Helper()
	out := map[string][][]byte{}
	for stack, chain := range deployStacks(t) {
		bodies := make([][]byte, 0, len(chain))
		for _, p := range chainPaths(chain) {
			// #nosec G304 -- путь собран из константы umbrellaDir и цепочки,
			// прочитанной из таблицы стеков.
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("профиль %s не прочитан: %v", p, err)
			}
			bodies = append(bodies, raw)
		}
		out[stack] = bodies
	}
	return out
}

// TestEveryStackDeclaresTheManifestPairWhole — ни один профиль дерева не несёт
// половины пары.
func TestEveryStackDeclaresTheManifestPairWhole(t *testing.T) {
	stacks := treeManifestPairStacks(t)
	findings, census, err := auditManifestPairAgreement(stacks)
	if err != nil {
		t.Fatalf("обход не состоялся: %v", err)
	}
	t.Log(census)
	if len(stacks) == 0 {
		t.Fatal("стендов ноль — «половин пары нет» здесь означало бы «сверять было нечего»")
	}
	for _, f := range findings {
		t.Error(f.Text)
	}
}

// TestManifestPairAuditFindsEitherHalf — ИНЪЕКЦИЯ: половина пары краснеет, и
// краснеет С ИМЕНЕМ СТЕНДА, а обе законные формы молчат.
//
// Вход синтетический намеренно: доказательство способности упасть не вправе
// зависеть от того, что сегодня лежит в дереве, — иначе оно исчезнет вместе с
// починкой дерева.
func TestManifestPairAuditFindsEitherHalf(t *testing.T) {
	body := func(name, required string) []byte {
		var b strings.Builder
		b.WriteString("kacho-iam:\n  manifests:\n")
		if name != "-" {
			fmt.Fprintf(&b, "    configMapName: %q\n", name)
		}
		if required != "-" {
			fmt.Fprintf(&b, "    required: %s\n", required)
		}
		return []byte(b.String())
	}

	t.Run("имя названо, опоры нет — находка", func(t *testing.T) {
		findings, census, err := auditManifestPairAgreement(map[string][][]byte{
			"проба": {body("kacho-module-manifests", "false")},
		})
		if err != nil {
			t.Fatalf("обход не состоялся: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("находок %d, ожидалась одна (%s)", len(findings), census)
		}
		if findings[0].Stack != "проба" || !strings.Contains(findings[0].Text, "configMapName") {
			t.Fatalf("находка не называет стенд и половину: %+v", findings[0])
		}
	})

	t.Run("опора объявлена, имени нет — находка", func(t *testing.T) {
		findings, _, err := auditManifestPairAgreement(map[string][][]byte{
			"проба": {body("", "true")},
		})
		if err != nil {
			t.Fatalf("обход не состоялся: %v", err)
		}
		if len(findings) != 1 || !strings.Contains(findings[0].Text, "required") {
			t.Fatalf("вторая половина не найдена либо не названа: %+v", findings)
		}
	})

	// ЗАКОННЫЕ БЛИЗНЕЦЫ. Без них краснота выше неотличима от судьи, отвергающего
	// всякий вход.
	twins := []struct {
		name  string
		chain [][]byte
	}{
		{"пара объявлена целиком", [][]byte{body("kacho-module-manifests", "true")}},
		{"доставка не заведена осознанно", [][]byte{body("", "false")}},
		{"цепочка не объявляет ни одной половины", [][]byte{[]byte("kacho-iam: {}\n")}},
		{"последнее объявление цепочки достраивает пару", [][]byte{
			body("kacho-module-manifests", "-"), body("-", "true"),
		}},
	}
	for _, tw := range twins {
		t.Run("близнец: "+tw.name, func(t *testing.T) {
			findings, census, err := auditManifestPairAgreement(map[string][][]byte{"проба": tw.chain})
			if err != nil {
				t.Fatalf("обход не состоялся: %v", err)
			}
			if len(findings) != 0 {
				t.Fatalf("законный близнец покраснел — судья ловит форму, а не существо: %v (%s)",
					findings, census)
			}
		})
	}
}
