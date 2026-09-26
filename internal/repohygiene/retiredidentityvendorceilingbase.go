// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// retiredidentityvendorceilingbase.go — БАЗА потолка привязок к снимаемому
// издателю (задача #2864): с чем сравнивается дерево изменения.
//
// Живёт в НЕ-тестовом файле намеренно: пробы на настоящих ветках git зовут ТЕ
// ЖЕ функции, что гейт по дереву.
//
// ─────────────────────────────────────────────────────────────────────────────
// БАЗА — ТОЧКА, ОТ КОТОРОЙ ОТОШЛО ТЕКУЩЕЕ ИЗМЕНЕНИЕ
//
// Ветки линии — ствол `main` и ветки-номера (задача, волна, эпик: одна форма
// имени, правило 2026-09-22; та же форма `[0-9]+`, что у фильтра конвейера).
// Различить задачу и волну по имени нельзя, поэтому линия изменения узнаётся не
// по имени, а по ПЕРВОРОДИТЕЛЬСКОЙ ЦЕПИ HEAD: у слияния первым родителем стоит
// линия, в которую вливают, — и у `git merge --no-ff <задача>` в ветке волны, и
// у головы запроса слияния, которую строит конвейер (первый родитель — база
// запроса).
//
// Правило одно:
//
//   - HEAD уже лежит в стволе (отправка в `main`) — база `HEAD^`: судится
//     посаженное изменение, остальное судилось, когда было вершиной;
//   - HEAD — СЛИЯНИЕ, не лежащее ни в одной ссылке линии (голова запроса
//     слияния; неотправленное `merge --no-ff` задачи в волну) — база `HEAD^`:
//     судится то, что слияние вносит в первого родителя, то есть в линию;
//   - иначе — ссылки линии, НЕ содержащие HEAD. Для каждой считается, сколько
//     коммитов первородительской цепи HEAD в ней нет (`rev-list --first-parent
//     --count HEAD ^<ссылка>`), и берётся ссылка с наименьшим числом: её цепь HEAD
//     встречает первой. Базой служит `merge-base HEAD <ссылка>`. При равенстве —
//     ссылка с НОВЕЙШЕЙ точкой слияния: ветка, влившая вершину своей линии, судится
//     против этой вершины, а не против старого ответвления, иначе снятое линией
//     засчиталось бы ветке.
//
// Что это даёт на каждом месте прогона:
//
//	голова запроса слияния      первый родитель — база запроса: судится ровно дельта запроса
//	слияние задачи в волну      база — прежняя вершина волны: судится вклад задачи
//	живая ветка задачи          база — точка ответвления от волны: судится вся ветка
//	ветка, влившая свою линию   база — влитая вершина линии, а не старое ответвление
//	отправка в ствол            база — `HEAD^`: судится посаженное изменение
//
// Ссылки берутся у git (`for-each-ref`), а не выписываются. Сперва
// `refs/remotes/origin/*` — то, что видит конвейер; локальные ветки — только
// когда origin не дал ни одной: иначе ветка соседа в клоне двигала бы базу.
//
// ГРАНИЦЫ ПРАВИЛА названы, а не умолчаны:
//
//  1. Прогон на вершине УЖЕ отправленной линии (не ствола) судит её отрезок
//     после последнего ответвления соседней ветки-номера, а не всю линию. На
//     вердикт конвейера это не влияет: голова запроса в ссылках не лежит никогда.
//  2. Ветка-номер, влившая НЕ вершину, а середину текущей ветки, придвигает
//     базу к HEAD. Задачи сводятся в волну, а не друг в друга, и сегодня таких
//     ссылок нет; признак — `база …` в переписи прогона.
//  3. HEAD, который САМ есть слияние вершины линии в ветку задачи (ветка
//     только что обновилась и своих коммитов поверх ещё не несёт), судится
//     вкладом этого слияния, то есть работой линии, а не задачи. Первый же
//     коммит задачи поверх возвращает суд ко всей ветке.
//  4. Клон без истории до базы (мелкий) базы не даёт: это ОТКАЗ, а не
//     зелёное, и чинится глубиной клона.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДЕРЕВО БАЗЫ ЧИТАЕТСЯ ТЕМ ЖЕ КЛАССИФИКАТОРОМ
//
// Платформа на базе — дерево изменения, в котором расходящиеся пути
// (`git diff --raw <база>`, рабочее дерево против базы) заменены их объектами
// на базе. Байты раскладываются по категориям ТЕМ ЖЕ классификатором
// (`vendorTreeCorpus.add`), что читает дерево изменения, поэтому неизменённый
// файл на обоих концах — одно и то же. Служба доступа и фундамент на базе
// читаются по пину `go.mod` БАЗЫ: пин тот же — дерево то же, пин другой — модуль
// этой версии из кэша модулей.
//
// Символическая ссылка среди расходящихся путей базы — ОТКАЗ: читатель дерева
// изменения её разыменовывает, а объект базы — это текст ссылки, и сравнение
// было бы о разном. В дереве платформы символических ссылок ноль
// (`git ls-files -s | awk '$1==120000'`).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// errVendorBase — базу вывести или прочитать нечем. Третья категория: не
// зелёное и не красное, сравнивать не с чем.
var errVendorBase = errors.New("база сравнения не установлена")

// errVendorUnreadable — путь есть, а прочитать его содержимое нечем (ссылка на
// подмодуль): он судится путём, как нечитаемый путь дерева изменения.
var errVendorUnreadable = errors.New("содержимое пути не читается")

// retiredVendorLineRef — ФОРМА имени линии: ствол и ветки-номера, у origin и
// локально. Группа 1 — ярус, группа 2 — имя линии.
var retiredVendorLineRef = regexp.MustCompile(`^refs/(remotes/origin|heads)/(main|[0-9]+)$`)

// retiredVendorBaseRev — база текущего изменения и СЛОВЕСНОЕ описание того, как
// она получена: перепись обязана называть, с чем сравнивали, иначе «не выросло»
// неотличимо от «сравнивали не с тем».
func retiredVendorBaseRev(root string) (rev, how string, err error) {
	out, err := gitenv.Command(root, "rev-list", "--first-parent", "HEAD").Output()
	if err != nil {
		return "", "", fmt.Errorf("%w: первородительская цепь HEAD не читается: %w", errVendorBase, err)
	}
	chain := strings.Fields(string(out))
	if len(chain) == 0 {
		return "", "", fmt.Errorf("%w: первородительская цепь HEAD пуста", errVendorBase)
	}

	out, err = gitenv.Command(root, "for-each-ref", "--format=%(refname)",
		"refs/remotes/origin", "refs/heads").Output()
	if err != nil {
		return "", "", fmt.Errorf("%w: ссылки клона не перечисляются: %w", errVendorBase, err)
	}
	all := strings.Fields(string(out))
	var origin, local []string
	for _, ref := range all {
		m := retiredVendorLineRef.FindStringSubmatch(ref)
		switch {
		case m == nil:
		case m[1] == "remotes/origin":
			origin = append(origin, ref)
		default:
			local = append(local, ref)
		}
	}
	candidates := origin
	if len(candidates) == 0 {
		candidates = local
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("%w: ни одной ссылки линии (ствол или ветка-номер) — "+
			"осмотрено ссылок клона %d; сравнить не с чем", errVendorBase, len(all))
	}

	type pick struct {
		ref, mb string
		ahead   int
	}
	var best *pick
	trunk, holder := "", ""
	for _, ref := range candidates {
		cnt, e := gitenv.Command(root, "rev-list", "--first-parent", "--count", "HEAD", "^"+ref).Output()
		if e != nil {
			return "", "", fmt.Errorf("%w: цепь HEAD против %s не считается: %w", errVendorBase, ref, e)
		}
		ahead, e := strconv.Atoi(strings.TrimSpace(string(cnt)))
		if e != nil {
			return "", "", fmt.Errorf("%w: число коммитов HEAD против %s не прочитано: %q",
				errVendorBase, ref, cnt)
		}
		if ahead == 0 {
			// HEAD уже лежит в этой ссылке: её точкой слияния был бы сам HEAD.
			holder = ref
			if strings.HasSuffix(ref, "/main") {
				trunk = ref
			}
			continue
		}
		if ahead >= len(chain) {
			continue // с цепью HEAD эта ссылка не пересекается
		}
		out, e := gitenv.Command(root, "merge-base", "HEAD", ref).Output()
		if e != nil {
			continue
		}
		p := pick{ref: ref, mb: strings.TrimSpace(string(out)), ahead: ahead}
		switch {
		case best == nil, p.ahead < best.ahead:
			best = &p
		case p.ahead == best.ahead && p.mb != best.mb &&
			gitenv.Command(root, "merge-base", "--is-ancestor", best.mb, p.mb).Run() == nil:
			// Равное расстояние по цепи, но точка слияния НОВЕЕ: линия, чья
			// вершина уже влита в изменение, — база именно она, а не старое
			// ответвление, иначе снятое ею засчиталось бы изменению.
			best = &p
		}
	}

	parents, err := gitenv.Command(root, "rev-list", "--parents", "-n", "1", "HEAD").Output()
	if err != nil {
		return "", "", fmt.Errorf("%w: родители HEAD не читаются: %w", errVendorBase, err)
	}
	merge := len(strings.Fields(string(parents))) > 2
	switch {
	case trunk != "", merge && holder == "":
		why := fmt.Sprintf("изменение уже в стволе %s", trunk)
		if trunk == "" {
			why = "HEAD — слияние вне ссылок линии: судится то, что оно вносит в первого " +
				"родителя, то есть в линию"
		}
		if len(chain) < 2 {
			return "", "", fmt.Errorf("%w: %s, а первого родителя у HEAD в этом клоне нет — "+
				"корневой коммит либо мелкий клон", errVendorBase, why)
		}
		return chain[1], fmt.Sprintf("HEAD^ %s (%s)", vendorShort(chain[1]), why), nil
	case best == nil:
		return "", "", fmt.Errorf("%w: ни одна из %d ссылок линии не пересекается с "+
			"первородительской цепью HEAD (длина %d) — клон мелкий либо линия чужая; "+
			"чинится глубиной клона, а не терпимостью гейта", errVendorBase, len(candidates), len(chain))
	}
	return best.mb, fmt.Sprintf("%s — точка слияния с %s (коммитов первородительской "+
		"цепи HEAD вне линии %d)", vendorShort(best.mb), best.ref, best.ahead), nil
}

func vendorShort(sha string) string { return sha[:min(len(sha), 11)] }

// vendorRawChange — одна запись `git diff --raw`: путь и его вид на базе.
type vendorRawChange struct {
	OldMode, OldSHA, Path string
}

// vendorRawChanges — расходящиеся пути рабочего дерева против ревизии.
// `--no-renames` намеренно: спрашивается «что лежит по этому пути на базе», а
// не «куда уехало содержимое».
func vendorRawChanges(root, rev string) ([]vendorRawChange, error) {
	out, err := gitenv.Command(root, "diff", "--raw", "-z", "--no-renames", "--no-abbrev",
		"--no-ext-diff", rev, "--").Output()
	if err != nil {
		return nil, fmt.Errorf("%w: разность с %s не читается: %w", errVendorBase, rev, err)
	}
	fields := strings.Split(string(out), "\x00")
	var changes []vendorRawChange
	for i := 0; i+1 < len(fields); i += 2 {
		meta := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(meta) < 5 {
			return nil, fmt.Errorf("%w: запись разности не разобрана: %q", errVendorBase, fields[i])
		}
		changes = append(changes, vendorRawChange{OldMode: meta[0], OldSHA: meta[2], Path: fields[i+1]})
	}
	return changes, nil
}

// vendorReadBlobs — содержимое объектов по их именам одним процессом
// `git cat-file --batch`.
func vendorReadBlobs(root string, shas []string) (map[string][]byte, error) {
	got := make(map[string][]byte, len(shas))
	if len(shas) == 0 {
		return got, nil
	}
	cmd := gitenv.Command(root, "cat-file", "--batch")
	cmd.Stdin = strings.NewReader(strings.Join(shas, "\n") + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: cat-file не запущен: %w", errVendorBase, err)
	}
	r := bufio.NewReader(stdout)
	var readErr error
	for range shas {
		header, err := r.ReadString('\n')
		if err != nil {
			readErr = fmt.Errorf("%w: ответ cat-file оборван: %w", errVendorBase, err)
			break
		}
		f := strings.Fields(header)
		if len(f) != 3 || f[1] != "blob" {
			readErr = fmt.Errorf("%w: объект базы не прочитан: %q", errVendorBase, strings.TrimSpace(header))
			break
		}
		size, err := strconv.Atoi(f[2])
		if err != nil {
			readErr = fmt.Errorf("%w: размер объекта не прочитан: %q", errVendorBase, header)
			break
		}
		data := make([]byte, size+1) // содержимое и завершающий перевод строки
		if _, err := io.ReadFull(r, data); err != nil {
			readErr = fmt.Errorf("%w: объект %s оборван: %w", errVendorBase, f[0], err)
			break
		}
		got[f[0]] = data[:size]
	}
	_, _ = io.Copy(io.Discard, r)
	if err := cmd.Wait(); err != nil && readErr == nil {
		readErr = fmt.Errorf("%w: cat-file: %w: %s", errVendorBase, err, strings.TrimSpace(stderr.String()))
	}
	return got, readErr
}

// drop — путь уходит из корпуса, в какой бы категории ни лежал. Ложь — его не
// было.
func (corpus *vendorTreeCorpus) drop(rel string) bool {
	if body, ok := corpus.Bodies[rel]; ok {
		delete(corpus.Bodies, rel)
		corpus.LinesRead -= strings.Count(body, "\n") + 1
		corpus.Walked--
		return true
	}
	if _, ok := corpus.Blobs[rel]; ok {
		delete(corpus.Blobs, rel)
		corpus.Walked--
		return true
	}
	for i, a := range corpus.Archives {
		if a.Name == rel {
			corpus.Archives = append(corpus.Archives[:i:i], corpus.Archives[i+1:]...)
			corpus.Walked--
			return true
		}
	}
	for i, p := range corpus.Prose {
		if p == rel {
			corpus.Prose = append(corpus.Prose[:i:i], corpus.Prose[i+1:]...)
			corpus.Walked--
			return true
		}
	}
	return false
}

// vendorCorpusCopy — копия корпуса; строки содержимого общие, карты и срезы свои.
func vendorCorpusCopy(c vendorTreeCorpus) vendorTreeCorpus {
	out := vendorTreeCorpus{
		Bodies:    make(map[string]string, len(c.Bodies)),
		Blobs:     make(map[string]string, len(c.Blobs)),
		Archives:  append([]vendorArchive(nil), c.Archives...),
		Prose:     append([]string(nil), c.Prose...),
		LinesRead: c.LinesRead,
		Walked:    c.Walked,
	}
	if out.Walked == 0 {
		out.Walked = len(c.Bodies) + len(c.Blobs) + len(c.Archives) + len(c.Prose)
	}
	for k, v := range c.Bodies {
		out.Bodies[k] = v
	}
	for k, v := range c.Blobs {
		out.Blobs[k] = v
	}
	return out
}

// retiredVendorPlatformBase — дерево платформы на базе и число расходящихся
// путей: дерево изменения, в котором каждый расходящийся путь заменён его
// объектом на базе либо снят, если на базе его нет.
func retiredVendorPlatformBase(root, rev string, head vendorTreeCorpus) (vendorTreeCorpus, int, error) {
	changes, err := vendorRawChanges(root, rev)
	if err != nil {
		return vendorTreeCorpus{}, 0, err
	}
	var shas []string
	for _, ch := range changes {
		switch ch.OldMode {
		case "000000", "160000":
		case "120000":
			return vendorTreeCorpus{}, 0, fmt.Errorf("%w: путь %s на базе — символическая ссылка: "+
				"читатель дерева изменения её разыменовывает, а объект базы — текст ссылки, "+
				"и сравнение было бы о разном", errVendorBase, ch.Path)
		default:
			shas = append(shas, ch.OldSHA)
		}
	}
	blobs, err := vendorReadBlobs(root, shas)
	if err != nil {
		return vendorTreeCorpus{}, 0, err
	}
	base := vendorCorpusCopy(head)
	for _, ch := range changes {
		base.drop(ch.Path)
		switch ch.OldMode {
		case "000000": // на базе пути нет
		case "160000":
			base.add(ch.Path, nil, errVendorUnreadable)
		default:
			base.add(ch.Path, blobs[ch.OldSHA], nil)
		}
	}
	return base, len(changes), nil
}

// vendorGoModPins — версии требований модуля, разобранные самим инструментом
// (`go mod edit -json`), а не выражением по тексту.
func vendorGoModPins(gomod []byte) (map[string]string, error) {
	dir, err := os.MkdirTemp("", "vendor-gomod-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, gomod, 0o600); err != nil {
		return nil, err
	}
	out, err := exec.Command("go", "mod", "edit", "-json", path).Output() // #nosec G204 -- путь создан здесь
	if err != nil {
		return nil, fmt.Errorf("разбор go.mod: %w", err)
	}
	var parsed struct {
		Require []struct{ Path, Version string }
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("разбор go.mod: %w", err)
	}
	pins := make(map[string]string, len(parsed.Require))
	for _, r := range parsed.Require {
		pins[r.Path] = r.Version
	}
	return pins, nil
}

// vendorModuleDirAt — каталог модуля нужной версии в кэше модулей.
func vendorModuleDirAt(root, module, version string) (string, error) {
	cmd := exec.Command("go", "mod", "download", "-json", module+"@"+version) // #nosec G204 -- модуль из закрытого перечня
	cmd.Dir = root
	out, err := cmd.Output()
	var got struct{ Dir, Error string }
	_ = json.Unmarshal(out, &got)
	if err != nil {
		return "", fmt.Errorf("%w: модуль %s@%s не получен: %w (%s)", errVendorBase, module, version, err, got.Error)
	}
	if got.Dir == "" {
		return "", fmt.Errorf("%w: модуль %s@%s получен без каталога (%s)", errVendorBase, module, version, got.Error)
	}
	return got.Dir, nil
}

// vendorModuleCorpus — дерево модуля целиком, кроме `.git`.
func vendorModuleCorpus(dir string) (vendorTreeCorpus, error) {
	var rels []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return vendorTreeCorpus{}, fmt.Errorf("обход %s: %w", dir, err)
	}
	return vendorCorpusFromPaths(dir, rels), nil
}

// retiredVendorBaseCorpora — три дерева на базе и по строке о каждом: как оно
// получено. head — деревья изменения, headGoMod — `go.mod` изменения.
func retiredVendorBaseCorpora(
	root, rev string, head map[string]vendorTreeCorpus, headGoMod []byte,
) (map[string]vendorTreeCorpus, []string, error) {
	platform, changed, err := retiredVendorPlatformBase(root, rev, head[vendorTreePlatform])
	if err != nil {
		return nil, nil, err
	}
	base := map[string]vendorTreeCorpus{vendorTreePlatform: platform}
	hows := []string{fmt.Sprintf("%s: расходящихся путей %d", vendorTreePlatform, changed)}

	baseGoMod, err := gitenv.Command(root, "show", rev+":go.mod").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: go.mod базы не читается: %w", errVendorBase, err)
	}
	basePins, err := vendorGoModPins(baseGoMod)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errVendorBase, err)
	}
	headPins, err := vendorGoModPins(headGoMod)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errVendorBase, err)
	}
	for _, tree := range retiredVendorTrees {
		module, ok := retiredVendorTreeModules[tree]
		if !ok {
			continue
		}
		was, now := basePins[module], headPins[module]
		switch {
		case was == "":
			return nil, nil, fmt.Errorf("%w: на базе ребра %s нет — дерево %s сравнить не с чем",
				errVendorBase, module, tree)
		case was == now:
			base[tree] = head[tree]
			hows = append(hows, fmt.Sprintf("%s: пин тот же %s", tree, now))
		default:
			dir, err := vendorModuleDirAt(root, module, was)
			if err != nil {
				return nil, nil, err
			}
			c, err := vendorModuleCorpus(dir)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %w", errVendorBase, err)
			}
			base[tree] = c
			hows = append(hows, fmt.Sprintf("%s: пин базы %s, изменения %s", tree, was, now))
		}
	}
	return base, hows, nil
}

// vendorVerdict — исход всего пути гейта над одним корнем git.
type vendorVerdict struct {
	Rev, How string
	BaseHows []string
	Findings []vendorCeilingFinding
	Census   map[string]vendorTreeCensus
	Bindings []vendorBinding
	Deltas   map[string]vendorTreeDelta
}

// vendorVerdictAt — весь путь гейта над корнем git: деревья изменения, база, её
// деревья и суждение. Его зовут и гейт по дереву, и пробы на настоящих ветках
// git. Дерево платформы подаётся перечнем путей индекса (rels), деревья служб —
// готовыми корпусами по пину изменения.
func vendorVerdictAt(root string, rels []string, pinned map[string]vendorTreeCorpus) (vendorVerdict, error) {
	head := map[string]vendorTreeCorpus{vendorTreePlatform: vendorCorpusFromPaths(root, rels)}
	for tree, c := range pinned {
		head[tree] = c
	}
	var v vendorVerdict
	var err error
	if v.Rev, v.How, err = retiredVendorBaseRev(root); err != nil {
		return v, err
	}
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod")) // #nosec G304 -- корень дерева под судом
	if err != nil {
		return v, fmt.Errorf("%w: go.mod изменения не читается: %w", errVendorBase, err)
	}
	base, hows, err := retiredVendorBaseCorpora(root, v.Rev, head, gomod)
	if err != nil {
		return v, err
	}
	v.BaseHows = hows
	v.Findings, v.Census, v.Bindings, v.Deltas, err = judgeRetiredVendorAgainstBase(head, base)
	return v, err
}
