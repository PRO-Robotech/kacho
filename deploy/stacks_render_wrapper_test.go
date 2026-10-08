// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stacks_render_wrapper_test.go — тестовая обёртка Go над таблицей цепочек,
// дописывающая к цепочке `prod` СЛОЙ ОПЕРАТОРА из каталога образцов, и её пробы
// (NTF-1, полоса D9; замысел З28 «Образец узла — в фикстуре гейта», Д48,
// CX1-86 (б), CX1-87, CX1-92, N11 · CX2-58, N12, N24).
//
// # Зачем обёртка, а не слой в общем читателе
//
// Общий читатель таблицы — `deployStacks(t)` (и шелл-двойник `stacks.sh`) —
// отдаёт строку `deploy/stacks.txt` ДОСЛОВНО: его же зовут рецепты установки, и
// слой в нём попал бы в настоящую установку `prod`. Узел почты в поставляемый
// `values.prod.yaml` не кладётся (Д48): образец в профиле стал бы узлом, который
// оператор не задавал. Поэтому слой оператора подставляет ТОЛЬКО эта обёртка, и
// видна она только гейтам пакета `deploy_test`.
//
// # Контракт (один на обе обёртки — Go и `tests/helm/lib/render-chain.sh`)
//
//	(1) цепочка не `prod` с образцом → строка равна общему читателю;
//	(2) `prod` с образцом → строка общего читателя и элемент образца ПОСЛЕДНИМ;
//	(3) `prod` без образца либо с пустым именем → отказ;
//	(4) имя вне каталога (`.`, `../mail-node/operator.yaml`, подкаталог,
//	    несуществующее, `../notify-standalone/values.yaml`) → отказ с перечнем
//	    каталога.
//
// Принадлежность — точным совпадением с перечнем ОБЫЧНЫХ файлов каталога, а не
// проверкой «путь существует»: `.` и подкаталог существуют, а образцом не
// являются (N12).
//
// Элемент образца пишется ОТНОСИТЕЛЬНО `umbrellaDir` — той же базы, от которой
// вызывающие соединяют профили цепочки (`os.Stat(filepath.Join(umbrellaDir, p))`
// в `deployStacks`, `renderStack`). Строкой он не записан: выводится
// `filepath.Rel` из положения каталога образцов (CX1-92 (б)).
package deploy_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// mailNodeSamplesDir — каталог образцов слоя оператора `prod`, относительно
// `deploy/`. Один на гейты Go и шелла; копий в гейтах нет (CX1-87 (б)).
const mailNodeSamplesDir = "testdata/mail-node"

// prodChainName — цепочка, к которой обёртка дописывает слой. Одна точка
// знания «какой цепочке нужен слой» (CX1-92 (в)).
const prodChainName = "prod"

// errNotASample — имя, не принадлежащее каталогу образцов.
var errNotASample = errors.New("образец не принадлежит каталогу образцов")

// mailNodeSampleNames — имена ОБЫЧНЫХ файлов каталога образцов, по алфавиту.
// Пустой каталог — отказ, а не «образцов нет»: обход, объявивший полноту на
// нуле прочитанного, судил бы ничего.
func mailNodeSampleNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("каталог образцов %s не читается: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("в каталоге образцов %s нет ни одного файла — обходить нечего", dir)
	}
	sort.Strings(names)
	return names, nil
}

// layerOperatorSample — ядро обёртки: копия таблицы, где к цепочке `prod`
// последним элементом дописан образец `sample` каталога `samplesDir`.
// Прочие цепочки равны общему читателю.
func layerOperatorSample(stacks map[string][]string, samplesDir, sample string) (map[string][]string, error) {
	names, err := mailNodeSampleNames(samplesDir)
	if err != nil {
		return nil, err
	}
	member := false
	for _, n := range names {
		if n == sample {
			member = true
			break
		}
	}
	if !member {
		return nil, fmt.Errorf("%w: %q не файл каталога %s; в каталоге: %s",
			errNotASample, sample, samplesDir, strings.Join(names, ", "))
	}
	prod, ok := stacks[prodChainName]
	if !ok {
		return nil, fmt.Errorf("цепочки %q в таблице нет — слой дописывать некуда", prodChainName)
	}
	elem, err := filepath.Rel(umbrellaDir, filepath.Join(samplesDir, sample))
	if err != nil {
		return nil, fmt.Errorf("путь образца %q от %s не выводится: %w", sample, umbrellaDir, err)
	}
	out := make(map[string][]string, len(stacks))
	for name, chain := range stacks {
		out[name] = append([]string(nil), chain...)
	}
	out[prodChainName] = append(append([]string(nil), prod...), elem)
	return out, nil
}

// deployStacksForRender — таблица цепочек для ГЕЙТА рендера: цепочка `prod`
// несёт слой оператора `sample` из каталога образцов. Для рецептов установки не
// годится by construction — их читатель `deployStacks`.
//
// Отказ таблицы приходит `t.Fatalf` из `deployStacks`; второго пути отказа у
// обёртки нет. Образец — обязательный параметр: пустое имя отвергается
// проверкой принадлежности.
func deployStacksForRender(t *testing.T, sample string) map[string][]string {
	t.Helper()
	out, err := layerOperatorSample(deployStacks(t), mailNodeSamplesDir, sample)
	if err != nil {
		t.Fatalf("обёртка рендера цепочек: %v", err)
	}
	return out
}

// chainElementsResolve — каждый элемент цепочки существует через ТУ ЖЕ
// операцию соединения, что у вызывающих: `filepath.Join(umbrellaDir, p)`.
func chainElementsResolve(chain []string) error {
	var missing []string
	for _, p := range chain {
		if _, err := os.Stat(filepath.Join(umbrellaDir, p)); err != nil {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("элементы цепочки не разрешаются от %s: %s",
			umbrellaDir, strings.Join(missing, ", "))
	}
	return nil
}

// tableChainLine — строка `<имя>:` таблицы, прочитанная НЕ общим читателем:
// сверка читателя с самим собой ничего бы не доказала.
func tableChainLine(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(stacksTable)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: таблица %s не читается: %v", stacksTable, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, name+":"); ok {
			return strings.Split(rest, ",")
		}
	}
	t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: строки %q в %s нет", name+":", stacksTable)
	return nil
}

// commonReaderCarriesNoLayer — судящая функция пробы «общий читатель без
// слоя»: цепочка, отданная общим читателем, равна строке таблицы дословно.
func commonReaderCarriesNoLayer(reader string, table, got []string) error {
	if !reflect.DeepEqual(table, got) {
		return fmt.Errorf("общий читатель %s отдал %v, а строка таблицы — %v: "+
			"слой в общем читателе попал бы в настоящую установку", reader, got, table)
	}
	return nil
}

// shellStacksArgsChain — `stacks.sh --args <имя>` как список профилей.
func shellStacksArgsChain(t *testing.T, name string) []string {
	t.Helper()
	out, err := exec.Command("bash", "tests/helm/stacks.sh", "--args", name).Output() // #nosec G204 -- фиксированный скрипт дерева
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: stacks.sh --args %s: %v", name, err)
	}
	f := strings.Fields(string(out))
	var chain []string
	for i := 0; i < len(f); i++ {
		if f[i] == "-f" && i+1 < len(f) {
			chain = append(chain, f[i+1])
			i++
			continue
		}
		t.Fatalf("stacks.sh --args %s: слово %q вне формы `-f <профиль>`", name, f[i])
	}
	return chain
}

// TestRenderWrapper_CommonReadersCarryNoLayer — общие читатели таблицы отдают
// строку `prod:` дословно; инъекция слоя в общий читатель — красный.
func TestRenderWrapper_CommonReadersCarryNoLayer(t *testing.T) {
	table := tableChainLine(t, prodChainName)
	readers := map[string][]string{
		"deployStacks(t)[\"prod\"]": deployStacks(t)[prodChainName],
		"stacks.sh --args prod":     shellStacksArgsChain(t, prodChainName),
	}
	for name, got := range readers {
		if err := commonReaderCarriesNoLayer(name, table, got); err != nil {
			t.Error(err)
		}
	}
	// Инъекция: общий читатель, отдающий цепочку со слоем, — красный.
	layered := deployStacksForRender(t, "operator.yaml")[prodChainName]
	if err := commonReaderCarriesNoLayer("инъекция", table, layered); err == nil {
		t.Errorf("инъекция «слой в общем читателе» (%v) не дала красного — проба не способна упасть", layered)
	}
	t.Logf("общих читателей сверено %d со строкой %s:%s; инъекция слоя — красный",
		len(readers), prodChainName, strings.Join(table, ","))
}

// TestRenderWrapper_ContractCases — один набор случаев контракта (1)–(4).
func TestRenderWrapper_ContractCases(t *testing.T) {
	stacks := deployStacks(t)
	layered := deployStacksForRender(t, "operator.yaml")

	// (1) Цепочки не `prod` равны общему читателю.
	others := 0
	for name, chain := range stacks {
		if name == prodChainName {
			continue
		}
		others++
		if !reflect.DeepEqual(layered[name], chain) {
			t.Errorf("(1) цепочка %s: обёртка отдала %v, общий читатель — %v", name, layered[name], chain)
		}
	}
	if others == 0 {
		t.Fatal("(1) НЕ ВЫПОЛНИЛОСЬ: цепочек, кроме prod, ноль")
	}

	// (2) `prod` — строка общего читателя и образец ПОСЛЕДНИМ.
	prod := layered[prodChainName]
	base := stacks[prodChainName]
	if len(prod) != len(base)+1 || !reflect.DeepEqual(prod[:len(base)], base) {
		t.Fatalf("(2) prod: обёртка отдала %v, ожидалось %v и образец последним", prod, base)
	}
	last := prod[len(prod)-1]
	if got, want := filepath.Clean(filepath.Join(umbrellaDir, last)),
		filepath.Join(mailNodeSamplesDir, "operator.yaml"); got != want {
		t.Errorf("(2) prod: элемент образца %q разрешается в %s, а не в %s", last, got, want)
	}

	// (3) и (4) — отказ с перечнем каталога. Подкаталог — во временной копии
	// каталога: в каталоге дерева подкаталога нет и заводить его ради пробы
	// значило бы править вход чужих гейтов.
	tmp := t.TempDir()
	withSub := filepath.Join(tmp, "mail-node")
	if err := os.MkdirAll(filepath.Join(withSub, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withSub, "operator.yaml"), []byte("global: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	refusals := []struct {
		label, dir, sample string
	}{
		{"(3) пустое имя", mailNodeSamplesDir, ""},
		{"(4) точка", mailNodeSamplesDir, "."},
		{"(4) выход из каталога", mailNodeSamplesDir, "../mail-node/operator.yaml"},
		{"(4) несуществующее", mailNodeSamplesDir, "absent.yaml"},
		{"(4) файл ноги notify", mailNodeSamplesDir, "../notify-standalone/values.yaml"},
		{"(4) подкаталог", withSub, "sub"},
	}
	for _, c := range refusals {
		_, err := layerOperatorSample(stacks, c.dir, c.sample)
		switch {
		case err == nil:
			t.Errorf("%s: %q принят обёрткой", c.label, c.sample)
		case !errors.Is(err, errNotASample):
			t.Errorf("%s: отказ не по принадлежности: %v", c.label, err)
		case !strings.Contains(err.Error(), "в каталоге: "):
			t.Errorf("%s: отказ без перечня каталога: %v", c.label, err)
		}
	}
	t.Logf("контракт: (1) цепочек не prod %d, (2) prod +%s, (3)–(4) отказов %d",
		others, last, len(refusals))
}

// TestRenderWrapper_ElementsResolveThroughTheCallersJoin — каждый элемент
// цепочки `prod` от обёртки существует через соединение вызывающих;
// инъекция — элемент образца от корня репозитория — красный с его именем.
func TestRenderWrapper_ElementsResolveThroughTheCallersJoin(t *testing.T) {
	prod := deployStacksForRender(t, "operator.yaml")[prodChainName]
	if err := chainElementsResolve(prod); err != nil {
		t.Errorf("цепочка prod обёртки: %v", err)
	}
	bad := "deploy/" + mailNodeSamplesDir + "/operator.yaml"
	injected := append(append([]string(nil), prod[:len(prod)-1]...), bad)
	err := chainElementsResolve(injected)
	if err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("инъекция «элемент от корня репозитория» не дала красного с именем %s: %v", bad, err)
	}
	t.Logf("элементов цепочки prod разрешено %d; инъекция — красный с именем элемента", len(prod))
}

// TestRenderWrapper_EmptyCatalogRefuses — пустой каталог образцов — отказ.
func TestRenderWrapper_EmptyCatalogRefuses(t *testing.T) {
	if _, err := mailNodeSampleNames(t.TempDir()); err == nil {
		t.Fatal("пустой каталог образцов принят — обход отчитался бы на нуле прочитанного")
	}
	names, err := mailNodeSampleNames(mailNodeSamplesDir)
	if err != nil {
		t.Fatalf("каталог дерева: %v", err)
	}
	t.Logf("образцов в %s: %d (%s)", mailNodeSamplesDir, len(names), strings.Join(names, ", "))
}

// umbrellaRootKeys — корневые ключи, которые рендер зонтика ЧИТАЕТ: ключи
// `values.yaml` зонтика, имена и алиасы зависимостей `Chart.yaml` и `global`.
func umbrellaRootKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{"global": true}
	for k := range readYAML(t, filepath.Join(umbrellaDir, "values.yaml")) {
		keys[k] = true
	}
	chart := readYAML(t, filepath.Join(umbrellaDir, "Chart.yaml"))
	deps, _ := chart["dependencies"].([]any)
	if len(deps) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в %s/Chart.yaml зависимостей ноль", umbrellaDir)
	}
	for _, d := range deps {
		m, _ := d.(map[string]any)
		for _, f := range []string{"name", "alias"} {
			if s, ok := m[f].(string); ok && s != "" {
				keys[s] = true
			}
		}
	}
	return keys
}

// sampleRootKeysOutsideUmbrella — судящая функция границы формы ключа (N24):
// корневой ключ файла `mail-node/*`, которого рендер зонтика не читает, —
// значение принятое и проигнорированное.
func sampleRootKeysOutsideUmbrella(samples map[string]map[string]any, allowed map[string]bool) []string {
	var out []string
	for file, doc := range samples {
		for k := range doc {
			if !allowed[k] {
				out = append(out, fmt.Sprintf("%s: корневой ключ %q рендером зонтика не читается", file, k))
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestMailNodeSamplesSpeakTheUmbrellaForm — корневые ключи каждого образца —
// из формы зонтика; инъекция — корневая строка ручки прыжков края — красный.
func TestMailNodeSamplesSpeakTheUmbrellaForm(t *testing.T) {
	allowed := umbrellaRootKeys(t)
	names, err := mailNodeSampleNames(mailNodeSamplesDir)
	if err != nil {
		t.Fatal(err)
	}
	samples := map[string]map[string]any{}
	keys := 0
	for _, n := range names {
		doc := readYAML(t, filepath.Join(mailNodeSamplesDir, n))
		samples[n] = doc
		keys += len(doc)
	}
	if keys == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: у образцов ноль корневых ключей — судить нечего")
	}
	for _, f := range sampleRootKeysOutsideUmbrella(samples, allowed) {
		t.Error(f)
	}

	// Инъекция: корневая форма ручки прыжков (читает её чарт края БЕЗ зонтика)
	// в образце — красный с именем файла и ключа. Близнец — форма зонтика.
	injected := map[string]map[string]any{"operator.yaml": {"global": map[string]any{}, "trustedHops": 1}}
	if got := sampleRootKeysOutsideUmbrella(injected, allowed); len(got) != 1 ||
		!strings.Contains(got[0], "operator.yaml") || !strings.Contains(got[0], "trustedHops") {
		t.Errorf("инъекция корневой строки ручки не дала красного с именем файла и ключа: %v", got)
	}
	twin := map[string]map[string]any{"operator.yaml": {"global": map[string]any{},
		"api-gateway": map[string]any{"trustedHops": 1}}}
	if got := sampleRootKeysOutsideUmbrella(twin, allowed); len(got) != 0 {
		t.Errorf("близнец формы зонтика дал находку: %v", got)
	}
	t.Logf("образцов %d, корневых ключей %d, допустимых корневых ключей зонтика %d; "+
		"инъекция — красный, близнец — молчание", len(names), keys, len(allowed))
}

// renderGateOperatorSample — образец слоя оператора, который гейты рендера
// цепочек под тегом helmcharts дописывают к `prod`: поставка не несёт ни узла
// почты (Д48), ни числа доверенных прыжков края (приёмка NTF-2 Р8, Д51), и
// рендер `prod` без слоя оператора отказывает. Один образец на все гейты тега:
// выбор образца — не предмет этих гейтов.
const renderGateOperatorSample = "operator.yaml"
