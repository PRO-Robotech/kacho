// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subchartdup_test.go — гейт против сабчарта, существующего в `charts/` дважды.
//
// ПРЕДМЕТ. `charts/` — каталог, в который helm МАТЕРИАЛИЗУЕТ зависимости. Если
// зависимость объявлена как `file://./charts/<имя>`, то её источник лежит там же,
// куда helm положит собранный архив, и после `helm dep update` чарт существует в
// `charts/` дважды: каталогом-исходником и архивом.
//
// Какая из двух копий попадёт в рендер — НЕ ОПРЕДЕЛЕНО. Замерено на неизменном
// дереве, шесть рендеров подряд: каталог выиграл 5 раз, архив 1 раз. То есть два
// рендера одного и того же дерева могли отличаться, а правка сабчарта могла в
// рендер не попасть.
//
// ЧЕМ ЭТО СТОИЛО. На этом ослеп гейт привязки пода к содержимому образа
// (deploy/tests/helm/image-rollout-binding-test.sh). Его самопроверка вносит
// дефект — снимает привязку у одного чарта — и требует, чтобы гейт покраснел.
// Гейт сравнивает шаблоны пода из ДВУХ рендеров, и расхождение копий давало
// «шаблон изменился», то есть выглядело как доказательство привязки. Гейт
// отчитывался зелёным о workload'е, у которого привязки в тот момент не было.
// Самопроверка это и показала — единственная причина, по которой класс нашёлся.
//
// ПОЧЕМУ ЗАПРЕТ СФОРМУЛИРОВАН НА ОБЪЯВЛЕНИИ, А НЕ НА НАЛИЧИИ ДВУХ КОПИЙ. Архивы
// не версионируются и на момент `go test` их в дереве нет вовсе — проверка «в
// charts/ лежат и каталог, и архив» здесь структурно невыполнима и молчала бы
// всегда. Проверяется ПРИЧИНА, которая в дереве есть и читается статически:
// объявление, просящее helm материализовать чарт туда, где уже лежит его источник.
//
// Правильная провязка вендоренного сабчарта — НЕ ОБЪЯВЛЯТЬ ЕГО ВОВСЕ: helm
// загружает из `charts/` всё, что является чартом. Объявление нужно только для
// внешнего репозитория, alias, condition или ограничения версии.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// umbrellaChart — единственная умбрелла дерева.
const umbrellaChart = "deploy/helm/umbrella/Chart.yaml"

// selfReferentialDeps возвращает имена зависимостей, чей `repository` указывает
// ВНУТРЬ каталога charts/ самой умбреллы.
//
// Разбор построчный, а не через YAML-библиотеку, и это осознанно: пакет
// repohygiene не тянет зависимостей ради гейтов, а форма, которую надо увидеть,
// однострочная — `repository: file://./charts/<имя>`. Имя берётся из последнего
// сегмента пути, а не из соседнего поля `name`: сравнивать надо именно то, куда
// helm положит архив.
func selfReferentialDeps(t *testing.T, raw string) []string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(raw, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "#") {
			continue // проза шапки описывает запрет и не является объявлением
		}
		const key = "repository:"
		i := strings.Index(s, key)
		if i != 0 {
			continue
		}
		repo := strings.TrimSpace(s[len(key):])
		repo = strings.Trim(repo, `"'`)
		for _, pref := range []string{"file://./charts/", "file://charts/"} {
			if strings.HasPrefix(repo, pref) {
				name := strings.Trim(strings.TrimPrefix(repo, pref), "/")
				if name != "" {
					found = append(found, name)
				}
			}
		}
	}
	return found
}

func TestNoSubchartMaterialisedOverItsOwnSource(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, umbrellaChart)
	raw, err := os.ReadFile(path)
	if err != nil {
		// Предпосылка гейта — умбрелла существует. Её исчезновение это находка,
		// а не повод промолчать.
		t.Fatalf("не прочитан %s: %v — предпосылка гейта не выполняется", umbrellaChart, err)
	}

	// Перепись: гейт обязан утверждать, что он что-то прочитал. Пустой или
	// нечитаемый файл дал бы «ноль нарушений», неотличимый от чистоты.
	if !strings.Contains(string(raw), "dependencies:") {
		t.Fatalf("в %s не найдено секции dependencies — разбор не о том файле", umbrellaChart)
	}

	for _, name := range selfReferentialDeps(t, string(raw)) {
		t.Errorf("%s: зависимость '%s' объявлена как file://./charts/%s — это просьба к helm "+
			"МАТЕРИАЛИЗОВАТЬ чарт туда, где уже лежит его исходник. После `helm dep update` "+
			"чарт будет в charts/ дважды (каталог + архив), и какая копия попадёт в рендер — "+
			"не определено: рендеры одного дерева начнут расходиться, а правка сабчарта может "+
			"в рендер не попасть.\n"+
			"Починка: УДАЛИТЬ объявление. Чарт, лежащий в charts/, helm загружает и без него; "+
			"объявление нужно только для внешнего репозитория, alias, condition или пина версии.",
			umbrellaChart, name, name)
	}
}

// TestSelfReferentialDepDetectorSeesTheForm — инъекция в обе стороны.
//
// Без неё гейт ловил бы форму, а не существо: первая же законная зависимость
// на СОСЕДНИЙ каталог (`file://../../../services/vpc/deploy` — их в умбрелле
// большинство) обязана оставлять его молчащим, иначе гейт отключат при первом
// ложном срабатывании.
func TestSelfReferentialDepDetectorSeesTheForm(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []string
	}{
		{
			name: "внутрь своего charts/ — находка",
			yaml: "dependencies:\n  - name: kacho-geo\n    repository: file://./charts/kacho-geo\n",
			want: []string{"kacho-geo"},
		},
		{
			name: "та же форма без ./ — находка",
			yaml: "dependencies:\n  - name: x\n    repository: file://charts/x\n",
			want: []string{"x"},
		},
		{
			name: "сосед по дереву — законно, молчит",
			yaml: "dependencies:\n  - name: vpc\n    repository: file://../../../services/vpc/deploy\n",
			want: nil,
		},
		{
			name: "внешний репозиторий — законно, молчит",
			yaml: "dependencies:\n  - name: hydra\n    repository: https://k8s.ory.sh/helm/charts\n",
			want: nil,
		},
		{
			name: "запрещённая форма В КОММЕНТАРИИ — не объявление, молчит",
			yaml: "dependencies:\n  # так делать нельзя: repository: file://./charts/kacho-geo\n  - name: vpc\n    repository: file://../x\n",
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := selfReferentialDeps(t, c.yaml)
			if len(got) != len(c.want) {
				t.Fatalf("нашлось %v, ожидалось %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("нашлось %v, ожидалось %v", got, c.want)
				}
			}
		})
	}
}
