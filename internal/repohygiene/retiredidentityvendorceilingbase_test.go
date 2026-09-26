// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// Пробы БАЗЫ на НАСТОЯЩИХ ветках git (#2864). Мир — синтетический репозиторий
// во временном каталоге: ствол `main`, линия-номер `100`, файлы с привязками и
// копия семейства файлов самого гейта по их путям в дереве. Суд — тот же путь,
// что у гейта по дереву ([vendorVerdictAt]).

// vendorProbeLine — строка, несущая имя издателя. НАСТОЯЩИЙ вход: пробы пишут её
// байты в файлы мира, и распознаватель судит их так же, как строку продукта.
// Объявлена одним местом, чтобы пробы не множили привязки дерева ради входа.
const vendorProbeLine = "image: oryd/hydra:v2.2.0"

// vendorOwnLine — законный близнец: та же форма, своё имя.
const vendorOwnLine = "image: prorobotech/platform:v1"

// vendorProbePin — пин рёбер мира. Такой версии нет нигде: пока пин не сдвинут,
// его только разбирают, и получить модуль по нему нельзя by construction.
const vendorProbePin = "v0.0.0-20000101000000-000000000000"

// vendorGit — git в корне мира; отказ инструмента — отказ пробы.
func vendorGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := gitenv.Command(root, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// vendorProbeFile — n строк привязки с различимыми хвостами и одна своя.
func vendorProbeFile(tag string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s # %s%d\n", vendorProbeLine, tag, i)
	}
	b.WriteString(vendorOwnLine + "\n")
	return b.String()
}

// vendorGateFamily — файлы семейства гейта из пакета: мир несёт их по тем же
// путям, чтобы «ветка их не трогает, и сведение в них не конфликтует» было
// утверждением о НАСТОЯЩИХ файлах, а не о пустом месте.
func vendorGateFamily(t *testing.T) map[string][]byte {
	t.Helper()
	names, err := filepath.Glob("retiredidentityvendorceiling*")
	if err != nil || len(names) == 0 {
		t.Fatalf("семейство гейта не найдено в каталоге пакета (%v): проверка НЕ ИСПОЛНЯЛАСЬ", err)
	}
	out := make(map[string][]byte, len(names))
	for _, n := range names {
		data, err := os.ReadFile(n) // #nosec G304 -- имя из glob каталога пакета
		if err != nil {
			t.Fatal(err)
		}
		out["internal/repohygiene/"+n] = data
	}
	return out
}

// vendorProbeWorld — ствол и линия `100` на одном коммите: go.mod с рёбрами к
// службе доступа и фундаменту, два файла с привязками (2 и 3 строки) и
// семейство гейта. Возвращает корень и имя этого коммита.
func vendorProbeWorld(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	vendorGit(t, root, "init", "--quiet", "-b", "main")
	vendorGit(t, root, "config", "user.email", "gate@example.invalid")
	vendorGit(t, root, "config", "user.name", "gate")
	vendorGit(t, root, "config", "commit.gpgsign", "false")

	vendorWriteAt(t, root, "go.mod", []byte("module synthetic\n\ngo 1.22\n\nrequire (\n"+
		"\t"+retiredVendorTreeModules[vendorTreeAccess]+" "+vendorProbePin+"\n"+
		"\t"+retiredVendorTreeModules[vendorTreeFoundation]+" "+vendorProbePin+"\n)\n"))
	vendorWriteAt(t, root, "deploy/a.yaml", []byte(vendorProbeFile("a", 2)))
	vendorWriteAt(t, root, "deploy/b.yaml", []byte(vendorProbeFile("b", 3)))
	for rel, data := range vendorGateFamily(t) {
		vendorWriteAt(t, root, rel, data)
	}
	vendorGit(t, root, "add", "-A")
	vendorGit(t, root, "commit", "--quiet", "-m", "база")
	vendorGit(t, root, "branch", "100")
	return root, vendorGit(t, root, "rev-parse", "HEAD")
}

// vendorProbePinned — деревья службы доступа и фундамента: непустые, без
// привязок, одни и те же на обоих концах (пин не сдвигается).
func vendorProbePinned() map[string]vendorTreeCorpus {
	return map[string]vendorTreeCorpus{
		vendorTreeAccess:     vendorCorpusOf(map[string]string{"go.mod": "module x\n"}, nil, nil, nil, 2),
		vendorTreeFoundation: vendorCorpusOf(map[string]string{"go.mod": "module y\n"}, nil, nil, nil, 2),
	}
}

// vendorProbeVerdict — весь путь гейта над миром на его текущем HEAD.
func vendorProbeVerdict(t *testing.T, root string) vendorVerdict {
	t.Helper()
	var rels []string
	for _, rel := range strings.Split(vendorGit(t, root, "ls-files", "-z"), "\x00") {
		if rel != "" {
			rels = append(rels, rel)
		}
	}
	v, err := vendorVerdictAt(root, rels, vendorProbePinned())
	if err != nil {
		t.Fatalf("суд мира не исполнился: %v", err)
	}
	d := v.Deltas[vendorTreePlatform]
	t.Logf("база %s (%s): на базе %d · в изменении %d · прирост %d · убыль %d · находок %d",
		vendorShort(v.Rev), v.How, d.Base, d.Head, len(d.Added), len(d.Removed), len(v.Findings))
	return v
}

// vendorCommitFile — перезаписать файл мира и закоммитить.
func vendorCommitFile(t *testing.T, root, rel, body, msg string) {
	t.Helper()
	vendorWriteAt(t, root, rel, []byte(body))
	vendorGit(t, root, "commit", "--quiet", "-am", msg)
}

// TestRetiredVendorCeiling_TwoRemovingBranchesMergeWithoutTouchingTheGate —
// ПРЕДИКАТ 1 задачи #2864.
//
// Две ветки от одной базы снимают РАЗНОЕ число строк в разных файлах: одну и
// две. Каждая зелёная, не тронув ни одного файла гейта; они сливаются друг с
// другом и с базой без конфликта, и гейт на сведении зелёный, а его разность
// с базой — ровно сумма двух снятий. Прежняя форма здесь краснела на каждой
// ветке («число убыло») и требовала переписать одну и ту же строку: 1 против 2
// давало конфликт.
func TestRetiredVendorCeiling_TwoRemovingBranchesMergeWithoutTouchingTheGate(t *testing.T) {
	t.Parallel()
	root, fork := vendorProbeWorld(t)

	vendorGit(t, root, "switch", "--quiet", "-c", "9001", "100")
	vendorCommitFile(t, root, "deploy/a.yaml", vendorProbeFile("a", 1), "снята одна строка")
	vendorGit(t, root, "switch", "--quiet", "-c", "9002", "100")
	vendorCommitFile(t, root, "deploy/b.yaml", vendorProbeFile("b", 1), "сняты две строки")

	for lane, removed := range map[string]int{"9001": 1, "9002": 2} {
		vendorGit(t, root, "switch", "--quiet", lane)
		v := vendorProbeVerdict(t, root)
		if v.Rev != fork {
			t.Fatalf("ветка %s: база обязана быть точкой ответвления %s, выведено %s (%s)",
				lane, vendorShort(fork), vendorShort(v.Rev), v.How)
		}
		if len(v.Findings) != 0 {
			t.Fatalf("ветка %s сняла строки и ничего не правила — гейт обязан молчать: %v", lane, v.Findings)
		}
		if d := v.Deltas[vendorTreePlatform]; len(d.Removed) != removed || len(d.Added) != 0 {
			t.Fatalf("ветка %s: убыль %d · прирост %d, ждали %d · 0", lane, len(d.Removed), len(d.Added), removed)
		}
		if touched := vendorGit(t, root, "diff", "--name-only", fork, "HEAD", "--",
			"internal/repohygiene"); touched != "" {
			t.Fatalf("ветка %s тронула файлы гейта: %s", lane, touched)
		}
	}

	vendorGit(t, root, "switch", "--quiet", "100")
	for _, lane := range []string{"9001", "9002"} {
		if out, err := gitenv.Command(root, "merge", "--no-ff", "--quiet", "-m", "сведение "+lane, lane).CombinedOutput(); err != nil {
			t.Fatalf("сведение %s отказало — конфликт там, где гейт не должен был ничего требовать: %v\n%s",
				lane, err, out)
		}
	}
	if u := vendorGit(t, root, "diff", "--name-only", "--diff-filter=U"); u != "" {
		t.Fatalf("после сведения остались неслитые пути: %s", u)
	}
	if touched := vendorGit(t, root, "diff", "--name-only", fork, "HEAD", "--",
		"internal/repohygiene"); touched != "" {
		t.Fatalf("сведение тронуло файлы гейта: %s", touched)
	}

	// Вершина волны — слияние второй ветки: судится то, что оно внесло в волну,
	// то есть ровно две снятые строки второй ветки, против первого родителя.
	tip := vendorProbeVerdict(t, root)
	if !strings.Contains(tip.How, "HEAD^") || len(tip.Findings) != 0 {
		t.Fatalf("вершина-слияние судится против первого родителя и молчит: %s · %v", tip.How, tip.Findings)
	}
	if d := tip.Deltas[vendorTreePlatform]; len(d.Removed) != 2 || len(d.Added) != 0 {
		t.Fatalf("вклад последнего слияния — убыль 2, получено убыль %d · прирост %d", len(d.Removed), len(d.Added))
	}

	// Суд сведения — так, как его видит конвейер на запросе волны в её линию
	// `99`: голова запроса — слияние, первым родителем у которого база запроса.
	vendorGit(t, root, "branch", "99", fork)
	vendorGit(t, root, "switch", "--quiet", "--detach", "99")
	vendorGit(t, root, "merge", "--no-ff", "--quiet", "-m", "голова запроса волны", "100")
	v := vendorProbeVerdict(t, root)
	if v.Rev != fork {
		t.Fatalf("база запроса волны обязана быть %s, выведено %s (%s)", vendorShort(fork), vendorShort(v.Rev), v.How)
	}
	if len(v.Findings) != 0 {
		t.Fatalf("сведение двух снятий обязано быть зелёным: %v", v.Findings)
	}
	d := v.Deltas[vendorTreePlatform]
	if len(d.Removed) != 3 || len(d.Added) != 0 || d.Base-d.Head != 3 {
		t.Fatalf("разность сведения с базой: убыль %d · прирост %d · %d → %d, ждали убыль 3",
			len(d.Removed), len(d.Added), d.Base, d.Head)
	}
}

// TestRetiredVendorCeiling_GrowthBranchIsRedWithTheAddedLine — ПРЕДИКАТ 2.
//
// Ветка добавляет строку поставщика сверх базы и больше ничего: гейт краснеет,
// и находка называет координату добавленной строки — файл и номер.
func TestRetiredVendorCeiling_GrowthBranchIsRedWithTheAddedLine(t *testing.T) {
	t.Parallel()
	root, _ := vendorProbeWorld(t)

	vendorGit(t, root, "switch", "--quiet", "-c", "9003", "100")
	vendorCommitFile(t, root, "deploy/a.yaml", vendorProbeFile("a", 2)+vendorProbeLine+"\n", "прирост")

	v := vendorProbeVerdict(t, root)
	if len(v.Findings) != 1 || v.Findings[0].Tree != vendorTreePlatform || v.Findings[0].Kind != vendorFindingGrown {
		t.Fatalf("рост над базой обязан быть одной находкой платформы: %v", v.Findings)
	}
	d := v.Findings[0].Delta
	if d == nil || len(d.Added) != 1 || d.Added[0].File != "deploy/a.yaml" || d.Added[0].Line != 4 {
		t.Fatalf("находка обязана нести строку прироста deploy/a.yaml:4, получено %+v", d)
	}
	if text := v.Findings[0].String(); !strings.Contains(text, "deploy/a.yaml:4 · "+vendorAxisName) {
		t.Fatalf("текст находки обязан называть координату добавленной строки:\n%s", text)
	}
}

// TestRetiredVendorCeiling_GrowthTwinWithoutTheAddedLineIsSilent — ПРЕДИКАТ 3:
// законный близнец пробы роста. Ровно один факт другой — добавленная строка
// несёт своё имя, а не имя издателя.
func TestRetiredVendorCeiling_GrowthTwinWithoutTheAddedLineIsSilent(t *testing.T) {
	t.Parallel()
	root, _ := vendorProbeWorld(t)

	vendorGit(t, root, "switch", "--quiet", "-c", "9004", "100")
	vendorCommitFile(t, root, "deploy/a.yaml", vendorProbeFile("a", 2)+vendorOwnLine+"\n", "своя строка")

	v := vendorProbeVerdict(t, root)
	if len(v.Findings) != 0 {
		t.Fatalf("близнец без строки издателя обязан молчать: %v", v.Findings)
	}
	if d := v.Deltas[vendorTreePlatform]; len(d.Added) != 0 || d.Base != d.Head {
		t.Fatalf("близнец: прирост %d · %d → %d, ждали 0 и равные числа", len(d.Added), d.Base, d.Head)
	}
}

// TestRetiredVendorCeiling_MergeHeadIsJudgedAgainstItsFirstParent — БАЗА головы
// запроса слияния: слияние вне ссылок линии судится против ПЕРВОГО родителя.
//
// Линия ушла вперёд (своё снятие в b.yaml) после ответвления ветки; голова
// запроса — слияние вершины линии (первый родитель) с веткой (второй). База —
// вершина линии: суд видит ровно вклад ветки, а снятие линии не приписывается
// ни ей, ни запросу.
func TestRetiredVendorCeiling_MergeHeadIsJudgedAgainstItsFirstParent(t *testing.T) {
	t.Parallel()
	root, _ := vendorProbeWorld(t)

	vendorGit(t, root, "switch", "--quiet", "-c", "9001", "100")
	vendorCommitFile(t, root, "deploy/a.yaml", vendorProbeFile("a", 1), "ветка сняла строку")
	vendorGit(t, root, "switch", "--quiet", "100")
	vendorCommitFile(t, root, "deploy/b.yaml", vendorProbeFile("b", 2), "линия сняла строку")
	tip := vendorGit(t, root, "rev-parse", "HEAD")

	vendorGit(t, root, "switch", "--quiet", "--detach", "100")
	vendorGit(t, root, "merge", "--no-ff", "--quiet", "-m", "голова запроса", "9001")

	v := vendorProbeVerdict(t, root)
	if v.Rev != tip || !strings.Contains(v.How, "HEAD^") {
		t.Fatalf("база головы запроса обязана быть первым родителем %s, выведено %s (%s)",
			vendorShort(tip), vendorShort(v.Rev), v.How)
	}
	d := v.Deltas[vendorTreePlatform]
	if len(d.Removed) != 1 || d.Removed[0].File != "deploy/a.yaml" || len(d.Added) != 0 {
		t.Fatalf("вклад запроса — одна убыль в a.yaml, получено убыль %v · прирост %d", d.Removed, len(d.Added))
	}
}

// TestRetiredVendorCeiling_BranchThatMergedItsLineIsJudgedAgainstTheLineTip —
// БАЗА ветки, влившей вершину своей линии: база — эта вершина.
//
// Линия сняла строку; ветка влила её и добавила строку издателя. Против старой
// точки ответвления итог был бы нулём (−1 +1) — снятое линией простило бы рост
// ветки. Против влитой вершины это +1, и гейт краснеет.
func TestRetiredVendorCeiling_BranchThatMergedItsLineIsJudgedAgainstTheLineTip(t *testing.T) {
	t.Parallel()
	root, _ := vendorProbeWorld(t)

	vendorGit(t, root, "switch", "--quiet", "-c", "9005", "100")
	vendorGit(t, root, "switch", "--quiet", "100")
	vendorCommitFile(t, root, "deploy/b.yaml", vendorProbeFile("b", 2), "линия сняла строку")
	tip := vendorGit(t, root, "rev-parse", "HEAD")
	vendorGit(t, root, "switch", "--quiet", "9005")
	vendorGit(t, root, "merge", "--no-ff", "--quiet", "-m", "ветка влила линию", "100")
	vendorCommitFile(t, root, "deploy/a.yaml", vendorProbeFile("a", 2)+vendorProbeLine+"\n", "ветка добавила строку")

	v := vendorProbeVerdict(t, root)
	if v.Rev != tip {
		t.Fatalf("база ветки, влившей линию, обязана быть влитой вершиной %s, выведено %s (%s)",
			vendorShort(tip), vendorShort(v.Rev), v.How)
	}
	if len(v.Findings) != 1 {
		t.Fatalf("рост ветки не прощается снятым линией — ждали одну находку: %v", v.Findings)
	}
}

// TestRetiredVendorCeiling_LandedChangeIsJudgedAgainstItsParent — БАЗА отправки в
// ствол: `HEAD^`, судится посаженное изменение.
func TestRetiredVendorCeiling_LandedChangeIsJudgedAgainstItsParent(t *testing.T) {
	t.Parallel()
	root, fork := vendorProbeWorld(t)

	vendorCommitFile(t, root, "deploy/a.yaml", vendorProbeFile("a", 2)+vendorProbeLine+"\n", "в ствол")

	v := vendorProbeVerdict(t, root)
	if v.Rev != fork || !strings.Contains(v.How, "стволе") {
		t.Fatalf("база изменения в стволе обязана быть HEAD^ %s, выведено %s (%s)",
			vendorShort(fork), vendorShort(v.Rev), v.How)
	}
	if len(v.Findings) != 1 {
		t.Fatalf("рост, посаженный в ствол, обязан быть находкой: %v", v.Findings)
	}
}

// TestRetiredVendorCeiling_NoLineRefIsARefusal — ПРЕДПОСЫЛКА: без ссылки линии
// сравнивать не с чем, и это ОТКАЗ, а не «не выросло».
func TestRetiredVendorCeiling_NoLineRefIsARefusal(t *testing.T) {
	t.Parallel()
	root, _ := vendorProbeWorld(t)
	vendorGit(t, root, "branch", "-m", "main", "feature")
	vendorGit(t, root, "branch", "-D", "100")

	_, _, err := retiredVendorBaseRev(root)
	if !errors.Is(err, errVendorBase) {
		t.Fatalf("клон без ссылок линии обязан давать отказ базы, получено: %v", err)
	}

	// Близнец: та же история и ОДНА ссылка линии — база выводится.
	vendorGit(t, root, "branch", "100")
	vendorGit(t, root, "commit", "--quiet", "--allow-empty", "-m", "ветка")
	_, how, err := retiredVendorBaseRev(root)
	if err != nil {
		t.Fatalf("со ссылкой линии база обязана выводиться: %v", err)
	}
	t.Logf("база близнеца: %s", how)
}

// TestRetiredVendorCeiling_BaseSymlinkIsARefusal — символическая ссылка на базе
// среди расходящихся путей: ОТКАЗ, потому что читатель изменения ссылку
// разыменовывает, а объект базы — её текст. Близнец — тот же путь обычным
// файлом на базе.
func TestRetiredVendorCeiling_BaseSymlinkIsARefusal(t *testing.T) {
	t.Parallel()
	for _, link := range []bool{true, false} {
		root, _ := vendorProbeWorld(t)
		vendorGit(t, root, "switch", "--quiet", "-c", "9006", "100")
		if link {
			if err := os.Symlink("a.yaml", filepath.Join(root, "deploy/c.yaml")); err != nil {
				t.Fatal(err)
			}
		} else {
			vendorWriteAt(t, root, "deploy/c.yaml", []byte(vendorOwnLine+"\n"))
		}
		vendorGit(t, root, "add", "-A")
		vendorGit(t, root, "commit", "--quiet", "-m", "c на базе")
		vendorGit(t, root, "branch", "-f", "100")
		vendorGit(t, root, "switch", "--quiet", "-c", "9007", "100")
		vendorGit(t, root, "rm", "--quiet", "deploy/c.yaml")
		vendorCommitFile(t, root, "deploy/c.yaml", vendorOwnLine+"\n", "c обычным файлом")

		var rels []string
		for _, rel := range strings.Split(vendorGit(t, root, "ls-files", "-z"), "\x00") {
			if rel != "" {
				rels = append(rels, rel)
			}
		}
		_, err := vendorVerdictAt(root, rels, vendorProbePinned())
		switch {
		case link && !errors.Is(err, errVendorBase):
			t.Fatalf("символическая ссылка на базе обязана быть отказом, получено: %v", err)
		case !link && err != nil:
			t.Fatalf("близнец — обычный файл на базе — обязан судиться: %v", err)
		}
	}
}

// TestRetiredVendorCeiling_ShiftedPinIsNotTheSameTree — пин службы доступа на
// базе ДРУГОЙ: её дерево на базе берётся по пину БАЗЫ, а не с изменения. Один
// факт пары — сдвинут ли пин; близнец с тем же пином дерево получать не ходит и
// берёт дерево изменения.
func TestRetiredVendorCeiling_ShiftedPinIsNotTheSameTree(t *testing.T) {
	t.Parallel()

	head := map[string]vendorTreeCorpus{vendorTreePlatform: vendorCorpusOf(
		map[string]string{"go.mod": "module synthetic\n"}, nil, nil, nil, 1)}
	for tree, c := range vendorProbePinned() {
		head[tree] = c
	}
	atBasePin := vendorCorpusOf(map[string]string{"base-pin.go": "package x\n"}, nil, nil, nil, 1)
	const headPin = "v0.0.0-20000101000000-0000000000aa"

	for _, shifted := range []bool{true, false} {
		root, fork := vendorProbeWorld(t)
		gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		headGoMod := gomod
		if shifted {
			headGoMod = []byte(strings.Replace(string(gomod),
				retiredVendorTreeModules[vendorTreeAccess]+" "+vendorProbePin,
				retiredVendorTreeModules[vendorTreeAccess]+" "+headPin, 1))
		}
		var asked []string
		fetch := func(module, version string) (vendorTreeCorpus, error) {
			asked = append(asked, module+"@"+version)
			return atBasePin, nil
		}
		base, hows, err := retiredVendorBaseCorpora(root, fork, head, headGoMod, fetch)
		if err != nil {
			t.Fatalf("сдвинут=%t: дерево базы не собрано: %v", shifted, err)
		}
		t.Logf("сдвинут=%t: %s · запрошено %v", shifted, strings.Join(hows, " · "), asked)
		_, fromBasePin := base[vendorTreeAccess].Bodies["base-pin.go"]
		switch want := retiredVendorTreeModules[vendorTreeAccess] + "@" + vendorProbePin; {
		case shifted && (len(asked) != 1 || asked[0] != want || !fromBasePin):
			t.Fatalf("сдвинутый пин обязан дать дерево по пину БАЗЫ %s, запрошено %v", want, asked)
		case !shifted && (len(asked) != 0 || fromBasePin):
			t.Fatalf("тот же пин обязан дать дерево изменения, не запрашивая модуль: %v", asked)
		}
	}
}

// TestRetiredVendorGoModPinsAreTheGoToolReading — пины разбираются в памяти, и
// ответ обязан совпасть с тем, что про тот же файл говорит сама команда go
// (`go mod edit -json`): два независимых чтения одного go.mod этого дерева.
// Разойдутся — суд базы сравнивал бы деревья не по тем пинам, по которым
// собирается продукт.
//
// Предпосылка — непустой перечень, несущий каждое ребро ведомости: иначе
// «совпало» означало бы «оба прочли пустое». Близнец — файл, который команда go
// отвергает: он обязан дать отказ, а не пустой перечень.
func TestRetiredVendorGoModPinsAreTheGoToolReading(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRoot(t), "go.mod")
	gomod, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	got, err := vendorGoModPins(gomod)
	if err != nil {
		t.Fatalf("go.mod дерева не разобран: %v", err)
	}

	out, err := exec.Command("go", "mod", "edit", "-json", path).Output()
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: команда go не прочла %s: %v", path, err)
	}
	var tool struct {
		Require []struct{ Path, Version string }
	}
	if err := json.Unmarshal(out, &tool); err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: ответ команды go не разобран: %v", err)
	}
	want := make(map[string]string, len(tool.Require))
	for _, r := range tool.Require {
		want[r.Path] = r.Version
	}
	for tree, module := range retiredVendorTreeModules {
		if want[module] == "" {
			t.Fatalf("предпосылка не выполнена: у go.mod дерева нет ребра %s (дерево %s) — "+
				"сверка пинов шла бы не о тех модулях", module, tree)
		}
	}
	t.Logf("требований: по команде go %d, по разбору в памяти %d", len(want), len(got))

	var diff []string
	for module, version := range want {
		if got[module] != version {
			diff = append(diff, fmt.Sprintf("%s: команда go %q, в памяти %q", module, version, got[module]))
		}
	}
	for module, version := range got {
		if _, ok := want[module]; !ok {
			diff = append(diff, fmt.Sprintf("%s: команда go —, в памяти %q", module, version))
		}
	}
	if len(diff) > 0 {
		sort.Strings(diff)
		t.Fatalf("разбор в памяти расходится с командой go по %d требованиям:\n  %s",
			len(diff), strings.Join(diff, "\n  "))
	}

	broken := []byte("module x\n\nrequire (\n\tbroken\n)\n")
	if pins, err := vendorGoModPins(broken); err == nil {
		t.Fatalf("go.mod, который команда go отвергает, обязан дать отказ, а дал перечень %v", pins)
	}
}
