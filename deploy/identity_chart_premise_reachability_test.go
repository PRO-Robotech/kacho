// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_chart_premise_reachability_test.go — раскол по тегу сборки
// `helmcharts` держится проверками, а не памятью автора.
//
// # Предмет
//
// Часть проверок каталога выведена из общего прогона тегом сборки: их условие
// создаёт не всякое задание. Это законный исход («своё задание, создающее
// условие»), но у него два способа стать ложью, и оба невидимы изнутри самого
// тега:
//
//  1. тег никто не зовёт — файл перестаёт компилироваться где бы то ни было, и
//     проверка исчезает из конвейера БЕЗ единого красного;
//  2. предпосылка раскола исчезла — условие создаёт уже любое задание, раскол
//     превратился в необъяснимое исключение, и следующий читатель снимет его
//     наугад либо, хуже, объяснит фактом, которого нет.
//
// Обе проверки живут ЗДЕСЬ, вне тега: проверка под собственным тегом о
// собственном невызове молчит by construction.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОД ОДНИМ ИМЕНЕМ ТЕГА ЖИЛИ ДВЕ ПРЕДПОСЫЛКИ, И ОДНА УЖЕ УМЕРЛА
//
// Было: «архивы чартов не отслеживаются git, их подкачивает `helm dependency
// build`». Это держало ОБА вида проверок — и читающие архив, и рендерящие
// умбреллу, — потому что обоим нужны были материализованные зависимости.
//
// Задача #2256 вендорила ВНЕШНИЕ архивы точным пином (сеть лежала на пути к
// вердикту). Для проверок, читающих архив, условие стало создаваться обычным
// клоном — предпосылка умерла, и они возвращены в общий прогон. Прежняя
// редакция этого файла ловила ровно тот момент: TestChartArchivesAreStillUntracked
// покраснел, как и был задуман, и сам назвал исход.
//
// Осталась ВТОРАЯ предпосылка, и она про другое: рендер умбреллы требует
// ЛОКАЛЬНЫХ сабчартов, а они собираются из исходников этого же дерева и в git
// не вендорятся ОСОЗНАННО (вендоренная копия означала бы «правка есть в файле,
// а в стенд не попала»). Её и сторожит TestUmbrellaRenderStillNeedsMaterializedDeps.
//
// Урок, ради которого это записано: раскол, объяснённый ОДНОЙ фразой, но
// опирающийся на ДВА факта, переживает смерть любого из них незаметно —
// оставшийся факт продолжает делать раскол на вид оправданным.
package deploy_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// ciWorkflow — конвейер, читаемый от каталога deploy.
const ciWorkflow = "../.github/workflows/ci.yaml"

// TestChartPremiseIsActuallyInvoked — тег `helmcharts` зовётся из конвейера.
//
// Без этой проверки вывод файла из общего прогона неотличим от его удаления:
// `go test ./...` тегированный файл не компилирует, поэтому ни ошибка сборки,
// ни красный тест о пропаже не сообщат.
func TestChartPremiseIsActuallyInvoked(t *testing.T) {
	raw, err := os.ReadFile(ciWorkflow)
	if err != nil {
		t.Fatalf("конвейер %s не читается (%v) — предпосылка проверки исчезла, "+
			"а не тег стал достижим", ciWorkflow, err)
	}
	body := string(raw)

	var invoking []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "-tags helmcharts") {
			continue
		}
		if !strings.Contains(line, "./deploy") {
			continue
		}
		invoking = append(invoking, strings.TrimSpace(line))
	}

	t.Logf("осмотрено: строк конвейера %d, зовущих тег на пакете deploy %d",
		strings.Count(body, "\n")+1, len(invoking))

	if len(invoking) == 0 {
		t.Fatalf("в %s нет шага, зовущего `go test -tags helmcharts ./deploy/...`: проверка "+
			"умолчаний чужого чарта выведена из общего прогона и НЕ добрана отдельным заданием — "+
			"то есть снята целиком, оставаясь на вид написанной. Либо верните шаг, либо снимите "+
			"тег вместе с файлом", ciWorkflow)
	}
	for _, l := range invoking {
		t.Logf("зов тега: %s", l)
	}
}

// TestUmbrellaRenderStillNeedsMaterializedDeps — у ОСТАВШЕЙСЯ предпосылки тега
// есть предмет.
//
// Проверки под `helmcharts`, которые рендерят умбреллу, оправданы ровно одним
// фактом: рендер из свежего клона НЕВОЗМОЖЕН, потому что ЛОКАЛЬНЫЕ сабчарты
// (`repository: file://…`) в git не вендорятся — их собирает `helm dependency
// build` из исходников этого же дерева. Пока хотя бы один такой сабчарт
// отсутствует в `charts/`, условие рендера создаёт не всякое задание, и раскол
// объясним.
//
// САМОИСТЕЧЕНИЕ: вендорят локальные сабчарты — предмет тега исчезает так же,
// как исчез предмет прежней проверки (внешние архивы, #2256), и эта проба
// краснеет РАНЬШЕ, чем раскол успеет стать традицией.
//
// Почему предикат про ЛОКАЛЬНЫЕ, а не «архивы вообще»: внешние теперь
// отслеживаются осознанно, и проверка «в charts/ ничего не отслеживается»
// краснела бы на исправном дереве вечно — то есть стала бы ровно тем, что
// корпус зовёт проверкой, не способной позеленеть.
func TestUmbrellaRenderStillNeedsMaterializedDeps(t *testing.T) {
	chartYAML := filepath.Join(umbrellaDir, "Chart.yaml")
	raw, err := os.ReadFile(chartYAML)
	if err != nil {
		t.Fatalf("%s не читается (%v) — предпосылка проверки исчезла, а не раскол "+
			"потерял предмет", chartYAML, err)
	}

	var chart struct {
		Dependencies []struct {
			Name       string `yaml:"name"`
			Repository string `yaml:"repository"`
		} `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(raw, &chart); err != nil {
		t.Fatalf("%s не разбирается: %v", chartYAML, err)
	}

	// Локальные сабчарты — по репозиторию, а не по имени: имя ничего не
	// обещает, а `file://` есть объявление «собирается из исходников дерева».
	local := map[string]bool{}
	for _, d := range chart.Dependencies {
		if strings.HasPrefix(d.Repository, "file://") && d.Name != "" {
			local[d.Name] = true
		}
	}
	if len(local) == 0 {
		t.Fatalf("%s не объявляет НИ ОДНОЙ локальной зависимости (`file://`) — "+
			"предпосылка проверки исчезла: обход пуст, и вердикт беспредметен", chartYAML)
	}

	tracked, err := treecorpus.Under(filepath.Join(umbrellaDir, "charts"))
	if err != nil {
		t.Fatalf("состав charts/: %v", err)
	}

	// Материализован = его каталог или его архив отслеживается git.
	materialised := map[string]bool{}
	for name := range local {
		for _, p := range tracked {
			base := filepath.Base(p)
			if strings.Contains(p, "/"+name+"/") ||
				(strings.HasPrefix(base, name+"-") && strings.HasSuffix(base, ".tgz")) {
				materialised[name] = true
				break
			}
		}
	}

	missing := make([]string, 0, len(local))
	for name := range local {
		if !materialised[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	t.Logf("осмотрено: локальных сабчартов объявлено %d, отслеживается git %d, "+
		"НЕ материализовано %d %v", len(local), len(materialised), len(missing), missing)

	if len(missing) == 0 {
		t.Errorf("ВСЕ %d локальных сабчартов умбреллы отслеживаются git — рендер умбреллы "+
			"больше не требует `helm dependency build`, то есть его условие создаёт любое "+
			"задание. Оставшийся предмет тега `helmcharts` исчез: верните проверки рендера "+
			"в общий прогон и снимите тег, иначе исключение переживёт своё основание. "+
			"Если же локальные сабчарты вендорены НАМЕРЕННО — это отдельное решение, и оно "+
			"противоречит deploy/.gitignore, где вендоринг локальных прямо назван запрещённым",
			len(local))
	}
}
