// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_ns_overlay_test.go — СЛОЙ СТЕНДА ПРОБ (stand.yaml, scripts/stand-ns.sh)
// ПОВЕРХ КАЖДОЙ ЦЕПОЧКИ ТАБЛИЦЫ НЕ ОБЪЯВЛЯЕТ ПЛОСКОСТИ ДАННЫХ ХРАНИЛИЩА (kacho#3222).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Боевой слой (`values.prod.yaml`) объявляет хранилищу вид плоскости данных
// CEPH_RBD, и цепочки на нём (`prod`, `own`, `fe3455`) сверщик хранилища
// запускают. Стенд проб в своём пространстве имён кластера блочного хранения не
// несёт: посев каталога ссылается на ключ `kacho-storage-ceph-keyring` и файл
// конфигурации кластера, которых на стенде нет, а чарт службы тома под каталог
// ссылок не монтирует. Сверщик на таком стенде каждый проход берёт том и
// отвечает «credentials reference … is not resolvable» — ресурс стоит в
// CREATING, и шард compute-storage судит отказ доводки, а не свой предмет
// (наблюдалось на стенде t3128-final, цепочка `own`).
//
// Ключ, положенный посевом, отказа не снимает, а переносит на следующее звено:
// адаптер читает ещё и конфигурацию кластера, а кластера нет. Поэтому слой
// стенда говорит то, что стенд НЕСЁТ, — вид пуст, как у стенда наборов
// конвейера (`values.dev-prod.yaml`). Пустой вид — законная посадка продукта
// только управляющей плоскости (страж старта хранилища, `config/validate.go`),
// а не послабление: ни режима, ни mTLS, ни `sslmode` слой не трогает.
//
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ. Что шард на стенде проб зеленеет — это судит сам
// прогон шарда. Здесь — что слой стенда, сложенный поверх любой цепочки,
// оставляет вид пустым, и что без слоя вид непуст хотя бы у одной цепочки
// (иначе проба судила бы пустое).
package deploy_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// standNsScript — скрипт стенда проб; функции слоя зовутся из него же, без
// второй копии текста слоя в пробе.
const standNsScript = "scripts/stand-ns.sh"

// standNsDispatch — начало разбора режима в конце скрипта. Функции скрипта
// исполняются в пробе без него: режима не названо, и разбор отказал бы.
const standNsDispatch = "\ncase \"${1:-}\" in\n"

// standNsHere — строка, которой скрипт выводит свой каталог из своего пути.
// Из потока ввода пути у скрипта нет, поэтому проба ставит каталог явно.
const standNsHere = `HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"`

// standNsCall исполняет функции скрипта стенда без кластера и без разбора
// режима: текст скрипта до разбора, каталог скрипта — абсолютным путём вместо
// вывода из пути, затем вызов. Код и вывод — вызова; отказ скрипта на верхнем
// уровне (stderr) — находка, а не шум.
func standNsCall(t *testing.T, call string) (string, error) {
	t.Helper()
	raw, err := os.ReadFile(standNsScript)
	if err != nil {
		t.Fatalf("скрипт стенда %s не читается: %v", standNsScript, err)
	}
	text := string(raw)
	if n := strings.Count(text, standNsDispatch); n != 1 {
		t.Fatalf("в %s начало разбора режима %q встречено %d раз, а не 1 — функции не отделить от разбора",
			standNsScript, strings.TrimSpace(standNsDispatch), n)
	}
	if n := strings.Count(text, standNsHere); n != 1 {
		t.Fatalf("в %s строка каталога скрипта %q встречена %d раз, а не 1 — каталог не поставить", standNsScript, standNsHere, n)
	}
	scripts, err := filepath.Abs(filepath.Dir(standNsScript))
	if err != nil {
		t.Fatal(err)
	}
	lib := strings.Replace(text[:strings.Index(text, standNsDispatch)], standNsHere, "HERE="+shellQuote(scripts), 1)
	var stderr strings.Builder
	cmd := exec.Command("bash", "-s")
	cmd.Stdin = strings.NewReader(lib + "\n" + call + "\n")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if stderr.Len() > 0 {
		t.Errorf("функции скрипта стенда писали в поток ошибок:\n%s", stderr.String())
	}
	return string(out), err
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// standOverlay — слой стенда проб, разобранный. Аргументы — те, что подставляет
// подъём: пространство, пространство ресурсов cert-manager, класс томов, порт.
func standOverlay(t *testing.T) map[string]any {
	t.Helper()
	out, err := standNsCall(t, "stand_overlay t3102-probe cm-ns on 20001")
	if err != nil {
		t.Fatalf("слой стенда не выведен: %v\n%s", err, out)
	}
	var tree map[string]any
	if err := yaml.Unmarshal([]byte(out), &tree); err != nil {
		t.Fatalf("слой стенда не разобран: %v\n%s", err, out)
	}
	return tree
}

// chainWith — слитые значения цепочки стека; overlay, если не nil, ложится
// последним, как слой стенда у подъёма.
func chainWith(t *testing.T, chain []string, overlay map[string]any) map[string]any {
	t.Helper()
	merged := readYAML(t, umbrellaDir+"/values.yaml")
	for _, p := range chain {
		merged = mergeValues(merged, readYAML(t, umbrellaDir+"/"+p))
	}
	if overlay != nil {
		merged = mergeValues(merged, overlay)
	}
	return merged
}

// blockBackendKind — вид плоскости данных хранилища в слитых значениях; ключа
// нет — пусто (умолчание чарта службы — пустой вид).
func blockBackendKind(merged map[string]any) string {
	v, ok := lookup(merged, "storage", "config", "blockBackend", "kind")
	if !ok || v == nil {
		return ""
	}
	s, isStr := v.(string)
	if !isStr {
		return fmt.Sprintf("%v (не строка)", v)
	}
	return s
}

// judgeProbeStandDataPlane — находки: цепочки, на которых стенд проб объявил бы
// плоскость данных, которой у него нет.
func judgeProbeStandDataPlane(t *testing.T, stacks map[string][]string, overlay map[string]any) []string {
	t.Helper()
	var out []string
	for name, chain := range stacks {
		if k := blockBackendKind(chainWith(t, chain, overlay)); k != "" {
			out = append(out, name+": вид плоскости данных хранилища «"+k+"» — кластера блочного хранения и "+
				"ключа к нему у стенда проб нет, сверщик держал бы каждый том в CREATING")
		}
	}
	return out
}

func TestProbeStandOverlayDeclaresNoStorageDataPlane(t *testing.T) {
	stacks := deployStacks(t)
	overlay := standOverlay(t)
	for _, f := range judgeProbeStandDataPlane(t, stacks, overlay) {
		t.Error(f)
	}
	t.Logf("перепись: цепочек таблицы со слоем стенда — %d", len(stacks))
}

// Близнец: без слоя стенда хотя бы одна цепочка таблицы вид объявляет — иначе
// проба выше судила бы цепочки, где пустой вид и так стоит.
func TestProbeStandDataPlaneJudgeIsAbleToFail(t *testing.T) {
	stacks := deployStacks(t)
	declaring := judgeProbeStandDataPlane(t, stacks, nil)
	if len(declaring) == 0 {
		t.Fatal("предпосылка пробы: ни одна цепочка таблицы вида плоскости данных не объявляет — судить нечего")
	}
	t.Logf("без слоя стенда вид объявляют %d цепочек: %v", len(declaring), declaring)
}
