// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// gitrevcause_test.go — держатель двух свойств дома причин (gitrevcause.go):
// ПРИЧИНА ОСТАЁТСЯ ПРЕДЪЯВИМОЙ сквозь обёртку, и СЛОВО РЕМОНТА берётся из
// общего источника, а не переписывается рядом.

// gitRevToolAbsentChildEnv — маркер подпроцесса и одновременно путь дерева,
// которое подпроцесс спросит. Пустое значение означает «я не ребёнок».
const gitRevToolAbsentChildEnv = "KACHO_GITREV_TOOL_ABSENT_TREE"

// gitRevToolAbsentChildMark — префикс строки, которой ребёнок отчитывается.
const gitRevToolAbsentChildMark = "KACHO_GITREV_CAUSE="

// TestGitRevCauseToolAbsentChild — тело подпроцесса. Вне подпроцесса не делает
// ничего: это НЕ пропуск, а вторая половина одной пробы, и своего предмета у
// неё в родительском процессе нет.
//
// Подпроцесс нужен ровно потому, что «инструмента нет вовсе» — состояние
// ПРОЦЕССА, а не дерева: `exec.Command` ищет `git` в PATH того, кто его зовёт.
// Снять PATH в общем процессе нельзя — пробы этого пакета идут параллельно, и
// снятие уронило бы соседей на чужой причине.
func TestGitRevCauseToolAbsentChild(t *testing.T) {
	t.Parallel()
	dir := os.Getenv(gitRevToolAbsentChildEnv)
	if dir == "" {
		return
	}
	err := foreignIDPRevisionResolverAt(dir)("0123456789a")
	fmt.Printf("%ssentinel=%v notfound=%v exit=%v text=%q\n",
		gitRevToolAbsentChildMark,
		errors.Is(err, errForeignIDPTreeNotAsked),
		errors.Is(err, exec.ErrNotFound),
		errors.As(err, new(*exec.ExitError)),
		fmt.Sprint(err))
}

// gitRevScratchRepo — СВОЁ дерево под пробу: один пустой коммит.
//
// Вход отрицательной ветви создаётся, а не наследуется: `t.TempDir()` ложится
// туда, куда укажет TMPDIR, и система контроля версий поднимается по родителям —
// внутри нашего же дерева «не репозиторий» оказался бы репозиторием.
func gitRevScratchRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("каталог дерева пробы: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := gitenv.Command(dir, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v в дереве пробы: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", ".")
	run("-c", "user.email=probe@example.invalid", "-c", "user.name=probe",
		"commit", "-q", "--allow-empty", "-m", "only")
	return dir
}

// TestGitRevTreeNotAskedKeepsItsCauseInspectable — ПРИЧИНА, СПРЯТАННАЯ В ТЕКСТ,
// ПЕРЕСТАЁТ БЫТЬ ПРИЧИНОЙ.
//
// Внутри «дерево не спрошено» врезка разрешителя разводит ТРИ подслучая с
// разными ремонтами — каталога нет · он не репозиторий · инструмента нет вовсе.
// Разводит их СЛОВАМИ; сигналом их разводит только то, что исходная ошибка
// доехала до вызывающего обёрнутой. Отдать `%w` сентинелу и оставить причине
// `%v` значит объявить классификацию, которой код больше не даёт: три подслучая
// становятся одной строкой без единого предъявимого признака.
//
// Пара на каждый подслучай одно-фактна: дерево пробы одно и то же, меняется
// ровно одно обстоятельство — путь · наличие репозитория по пути · наличие
// инструмента у процесса.
func TestGitRevTreeNotAskedKeepsItsCauseInspectable(t *testing.T) {
	t.Parallel()
	repo := gitRevScratchRepo(t)

	// Законный близнец ВСЕЙ группы: дерево на месте, инструмент на месте,
	// ревизия своя — отказа нет вовсе. Без него красное ниже достигалось бы
	// отказом на чём угодно.
	head, err := gitenv.Command(repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("голова дерева пробы не снята: %v — близнец взять неоткуда", err)
	}
	if err := foreignIDPRevisionResolverAt(repo)(strings.TrimSpace(string(head))); err != nil {
		t.Fatalf("своя ревизия своего дерева не разрешилась: %v — близнец не зелёный", err)
	}

	// Подслучай 1 — КАТАЛОГА НЕТ. Смена рабочего каталога не удаётся ещё до
	// запуска инструмента, и причина приходит ошибкой файловой системы.
	t.Run("каталога нет", func(t *testing.T) {
		t.Parallel()
		err := foreignIDPRevisionResolverAt(filepath.Join(t.TempDir(), "нет-такого"))("0123456789a")
		if err == nil {
			t.Fatal("несуществующий каталог разрешил ревизию — fail-open")
		}
		t.Logf("текст отказа: %v", err)
		if !errors.Is(err, errForeignIDPTreeNotAsked) {
			t.Errorf("подслучай отнесён не к своей причине: %v", err)
		}
		var pathErr *fs.PathError
		if !errors.As(err, &pathErr) {
			t.Errorf("причина не предъявима: errors.As(*fs.PathError) ложно на %v — "+
				"«каталога нет» неотличимо от «он не репозиторий»", err)
		} else if pathErr.Op != "chdir" {
			t.Errorf("предъявлена не та ошибка файловой системы: op=%q", pathErr.Op)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("причина не предъявима: errors.Is(fs.ErrNotExist) ложно на %v", err)
		}
	})

	// Подслучай 2 — КАТАЛОГ ЕСТЬ, НО ОН НЕ РЕПОЗИТОРИЙ. Инструмент запускается и
	// отказывает сам, кодом 128.
	//
	// СОСТОЯНИЕ СОЗДАЁТСЯ, А НЕ НАСЛЕДУЕТСЯ, и голого `t.TempDir()` для этого
	// НЕ ДОСТАТОЧНО: система контроля версий поднимается по родителям, а
	// временный каталог ложится туда, куда укажет TMPDIR. Измерено на этой же
	// правке — с TMPDIR внутри рабочего каталога воркспейса (сам он рабочее
	// дерево git) «не репозиторий» оказывался репозиторием: код возврата 1
	// вместо 128, и подслучай уезжал в ЧУЖУЮ ветвь «объекта нет». Проба при
	// этом была зелёной при TMPDIR по умолчанию — то есть проходила по
	// обстоятельству среды, а не по свойству кода.
	//
	// Барьер — файл `.git`, указывающий в никуда: поиск репозитория
	// останавливается на нём, и отказ говорит дословно «не является
	// репозиторием git». Это ровно предмет подслучая, и он не зависит ни от
	// TMPDIR, ни от того, откуда запущен прогон.
	t.Run("не репозиторий", func(t *testing.T) {
		t.Parallel()
		notrepo := filepath.Join(t.TempDir(), "плоский-каталог")
		if err := os.MkdirAll(notrepo, 0o750); err != nil {
			t.Fatalf("каталог: %v", err)
		}
		if err := os.WriteFile(filepath.Join(notrepo, ".git"),
			[]byte("gitdir: /дерева-по-этому-пути-нет\n"), 0o600); err != nil {
			t.Fatalf("барьер поиска репозитория: %v", err)
		}
		// Предпосылка подслучая: по этому пути рабочего дерева НЕТ. Без неё
		// проба судила бы соседнюю причину и была бы зелёной не по своему
		// свойству.
		if err := gitenv.Command(notrepo, "rev-parse", "--is-inside-work-tree").Run(); err == nil {
			t.Fatal("каталог признан рабочим деревом git — условие подслучая не " +
				"создано, и его исход относился бы к другой причине")
		}
		err := foreignIDPRevisionResolverAt(notrepo)("0123456789a")
		if err == nil {
			t.Fatal("не-репозиторий разрешил ревизию — fail-open")
		}
		t.Logf("текст отказа: %v", err)
		if !errors.Is(err, errForeignIDPTreeNotAsked) {
			t.Errorf("подслучай отнесён не к своей причине: %v", err)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Errorf("причина не предъявима: errors.As(*exec.ExitError) ложно на %v — "+
				"«он не репозиторий» неотличимо от «каталога нет»", err)
		} else if exitErr.ExitCode() != 128 {
			t.Errorf("предъявлен код %d, а не 128", exitErr.ExitCode())
		}
		// Отделение от СОСЕДНЕЙ группы: код 1 — это «объекта нет», и он обязан
		// разбираться другой ветвью, а не этой.
		var fsErr *fs.PathError
		if errors.As(err, &fsErr) {
			t.Errorf("не-репозиторий предъявил ошибку файловой системы: %v", err)
		}
	})

	// Подслучай 3 — ИНСТРУМЕНТА НЕТ ВОВСЕ. Состояние процесса, не дерева:
	// создаётся подпроцессом с пустым PATH (см. [TestGitRevCauseToolAbsentChild]).
	t.Run("инструмента нет вовсе", func(t *testing.T) {
		t.Parallel()
		cmd := exec.Command(os.Args[0], "-test.run=^TestGitRevCauseToolAbsentChild$", "-test.v")
		env := []string{gitRevToolAbsentChildEnv + "=" + repo, "PATH="}
		for _, kv := range os.Environ() {
			if name, _, ok := strings.Cut(kv, "="); ok && (name == "PATH" || name == gitRevToolAbsentChildEnv) {
				continue
			}
			env = append(env, kv)
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("подпроцесс не отработал: %v\n%s — условие пробы не создано, и её "+
				"молчание ничего не значило бы", err, out)
		}
		var line string
		for _, l := range strings.Split(string(out), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), gitRevToolAbsentChildMark); ok {
				line = v
				break
			}
		}
		if line == "" {
			t.Fatalf("подпроцесс не отчитался; вывод:\n%s", out)
		}
		t.Logf("подпроцесс: %s", line)
		if !strings.Contains(line, "sentinel=true") {
			t.Errorf("отсутствие инструмента отнесено не к «дерево не спрошено»: %s", line)
		}
		if !strings.Contains(line, "notfound=true") {
			t.Errorf("причина не предъявима: errors.Is(exec.ErrNotFound) ложно — "+
				"«инструмента нет вовсе» неотличимо от «он не репозиторий»: %s", line)
		}
		// Отделение от подслучая 2: отсутствие инструмента кодом возврата не
		// сопровождается, инструмент не запускался.
		if !strings.Contains(line, "exit=false") {
			t.Errorf("отсутствие инструмента предъявило код возврата — значит "+
				"инструмент запускался, и предпосылка подслучая не выполнена: %s", line)
		}
	})
}

// TestGitRevRemedyVocabularyHasASingleSource — СЛОВО РЕМОНТА ОБЪЯВЛЕНО ОДИН РАЗ,
// И ОБЕ СТОРОНЫ БЕРУТ ЕГО ОТТУДА.
//
// Проверка ремонта подстрокой по ОДНОМУ слову ошибается в обе стороны:
// переименуешь слово — краснеет зря, хотя смысл цел; сохранишь слово и
// перевернёшь предложение — промолчит, хотя ремонта в отказе больше нет. Общий
// источник снимает первое (обе стороны двигаются вместе), целая клауза вместо
// слова — второе.
//
// Пара здесь на сам распознаватель, и он тот же, которым судятся настоящие
// отказы: инъекция, зовущая свою копию, доказывала бы свойство копии.
func TestGitRevRemedyVocabularyHasASingleSource(t *testing.T) {
	t.Parallel()

	// Законный близнец: текст собран ИЗ общего источника, проза вокруг него
	// своя и другая — распознаватель молчит.
	lawful := fmt.Errorf("%w: сначала %s, и только потом всё остальное",
		errForeignIDPRevisionUndelivered, gitRevRemedyCloneDepth)
	if !gitRevRemedyNamed(lawful, gitRevRemedyCloneDepth) {
		t.Errorf("отказ, собранный из общего источника, не признан называющим ремонт: %v", lawful)
	}

	// Дефект: СЛОВО сохранено, предложение переписано мимо источника —
	// распознаватель обязан покраснеть. Ровно этот вход проходил у проверки
	// подстрокой по слову.
	reworded := fmt.Errorf("%w: начните с клона побольше ГЛУБИНЫ, там разберётесь",
		errForeignIDPRevisionUndelivered)
	if !strings.Contains(reworded.Error(), "ГЛУБИНЫ") {
		t.Fatal("инъекция не воспроизводит свой предмет: слова в тексте нет, " +
			"и её молчание ничего не значило бы")
	}
	if gitRevRemedyNamed(reworded, gitRevRemedyCloneDepth) {
		t.Errorf("своя проза, сохранившая одно слово, прошла за общий источник: %v", reworded)
	}

	// Настоящие отказы разрешителя называют КАЖДЫЙ СВОЙ ремонт — и тем же
	// источником. Без этой половины пара доказывала бы свойство синтетики.
	origin, older, newer := foreignIDPScratchRepo(t)
	shallow := filepath.Join(t.TempDir(), "shallow")
	if out, err := gitenv.Command(filepath.Dir(shallow), "clone", "-q", "--depth=1",
		"file://"+origin, shallow).CombinedOutput(); err != nil {
		t.Fatalf("мелкий клон не создан (%v): %s — условие пробы не создано", err, out)
	}
	_ = newer

	absent := foreignIDPRevisionResolverAt(origin)("0123456789a")
	undelivered := foreignIDPRevisionResolverAt(shallow)(older)
	notasked := foreignIDPRevisionResolverAt(filepath.Join(t.TempDir(), "нет-такого"))(older)

	// ЧЕТВЁРТАЯ КЛАУЗА — у ДРУГОГО производителя, и без неё перепись ниже не
	// сошлась бы: словарь один на оба, а строка «DeclaredBase как want» до сих
	// пор не существовала нигде.
	rootCommit := gateCarrierNoParentRefusal("refs/remotes/origin/main", false, nil,
		errors.New("exit status 1"))

	rows := []struct {
		name string
		err  error
		want string
	}{
		{"объекта нет", absent, gitRevRemedyLedger},
		{"не довезён", undelivered, gitRevRemedyCloneDepth},
		{"дерево не спрошено", notasked, gitRevRemedyWorkingDir},
		{"родителя нет по существу", rootCommit, gitRevRemedyDeclaredBase},
	}

	// ПЕРЕПИСЬ ПО ОБЪЯВЛЕННОМУ СЛОВАРЮ, а не по памяти таблицы: у каждой клаузы
	// обязан быть ровно один производитель. Пятая клауза, заведённая завтра и
	// никем не произведённая, краснеет здесь, а не проходит молча.
	declared := gitRevRemedies()
	t.Logf("перепись: клауз объявлено %d · строк таблицы %d", len(declared), len(rows))
	if len(rows) != len(declared) {
		t.Fatalf("клауз объявлено %d, а судится %d — словарь и таблица разошлись; "+
			"несуженная клауза прошла бы молча", len(declared), len(rows))
	}
	covered := map[string]int{}
	for _, r := range rows {
		covered[r.want]++
	}
	for _, clause := range declared {
		if covered[clause] != 1 {
			t.Errorf("клауза %q произведена %d отказами, а обязана ровно одним — "+
				"либо её никто не называет, либо две причины дают один совет",
				clause, covered[clause])
		}
	}

	// ВЗАИМНОЕ ИСКЛЮЧЕНИЕ ПОЛНО В ОБЕ СТОРОНЫ И ВЫВЕДЕНО ИЗ СЛОВАРЯ, а не
	// выписано рядом: выписанный перечень «чужих» разойдётся со словарём молча,
	// и полнота будет верна ровно для тех клауз, о которых проба помнила.
	for _, c := range rows {
		if c.err == nil {
			t.Errorf("%s: отказа нет вовсе — предпосылка не выполнена", c.name)
			continue
		}
		t.Logf("%s: %v", c.name, c.err)
		if !gitRevRemedyNamed(c.err, c.want) {
			t.Errorf("%s: отказ не называет своего ремонта %q — читающий пойдёт чинить "+
				"не то: %v", c.name, c.want, c.err)
		}
		for _, other := range declared {
			if other == c.want {
				continue
			}
			if gitRevRemedyNamed(c.err, other) {
				t.Errorf("%s: отказ называет ЧУЖОЙ ремонт %q — две причины сошлись в "+
					"один совет: %v", c.name, other, c.err)
			}
		}
	}
}

// repohygienePackageName — имя пакета, который этот обход судит.
//
// Отдельным именем, потому что оно стоит в ТРЁХ ролях разом: отбор файлов,
// текст переписи и текст отказа. Выписанное трижды, оно разошлось бы молча.
const repohygienePackageName = "repohygiene"

// repohygienePackageCensus — что обход УВИДЕЛ и что из увиденного СУДИЛ.
//
// Две величины, а не одна, и это не педантизм: каталог и пакет — РАЗНЫЕ
// единицы, и пока перепись печатала одно число под ярлыком «файлов пакета», их
// расхождение было невидимо. Здесь оно проявляется само, без отдельной
// проверки: 832 в каталоге против 813 в пакете видно с первой строки лога.
type repohygienePackageCensus struct {
	// filesInDir — файлов `.go` в каталоге, сколько их ни есть.
	filesInDir int
	// filesInPkg — из них объявляющих [repohygienePackageName]. Только они
	// доходят до судьи.
	filesInPkg int
	// foreignPkgs — чужие пакеты ТОГО ЖЕ каталога: имя → файлов. Печатается
	// всегда: «чужих ноль» обязано быть отличимо от «чужих не считали».
	foreignPkgs map[string]int
}

func (c repohygienePackageCensus) String() string {
	return fmt.Sprintf("файлов .go в каталоге %d · из них пакета %q %d · чужих пакетов "+
		"того же каталога %d %v", c.filesInDir, repohygienePackageName, c.filesInPkg,
		len(c.foreignPkgs), c.foreignPkgs)
}

// repohygienePackageWalk — РАЗБОР ВСЕГО ПАКЕТА, а не одного файла и не всего
// каталога.
//
// # Единица обхода — ПАКЕТ
//
// Константа пакетного уровня видна всему пакету и объявляется в ЛЮБОМ его
// файле, поэтому перепись, читающая один файл, верна ровно до той минуты, пока
// автор пятой клаузы не положил её соседним файлом: «за пределами разбираемого
// файла находок ноль» было бы свойством СОВПАДЕНИЯ, а не построения. Измерено
// инъекцией 2026-09-22: пятая клауза в gatecarrierremoval.go оставила обе
// переписи зелёными.
//
// Ровно тот же довод отсекает и ЧУЖОЙ ПАКЕТ ИЗ ТОГО ЖЕ КАТАЛОГА. Рядом с
// `package repohygiene` в этом каталоге лежит `package repohygiene_test`
// (измерено: 813 файлов против 19), и его объявления этому пакету невидимы —
// одноимённая константа там ДРУГОЙ ПРЕДМЕТ, а вызов оттуда до неэкспортированного
// дома не дотянется вовсе. Прежняя редакция отсекала подкаталоги и не отсекала
// чужой пакет, хотя довод один: судящая перепись давала ЛОЖНУЮ находку на
// константе чужого пакета, а гейт дома предписывал ремонт, неисполнимый по
// построению (`undefined: gitRevParentArgv`) — измерено обеими инъекциями
// 2026-09-22. «Чужого не попалось» было свойством совпадения: сегодня таких имён
// в тех 19 файлах просто нет.
//
// Подкаталоги пропускаются по той же причине: `internal/repohygiene/artifactgates`
// есть отдельный пакет.
//
// Дерево целиком единицей тем более не является: одноимённая константа чужого
// пакета — другой предмет, и обход дерева дал бы ложные находки, а не более
// полный счёт.
func repohygienePackageWalk(t *testing.T, visit func(path string, fset *token.FileSet, file *ast.File)) repohygienePackageCensus {
	t.Helper()

	dir := filepath.Join(repoRoot(t), "internal", "repohygiene")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("читать каталог пакета %s: %v — перепись беспредметна", dir, err)
	}

	fset := token.NewFileSet()
	c := repohygienePackageCensus{foreignPkgs: map[string]int{}}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v — перепись беспредметна", path, err)
		}
		c.filesInDir++
		if file.Name == nil || file.Name.Name != repohygienePackageName {
			name := "<без объявления>"
			if file.Name != nil {
				name = file.Name.Name
			}
			c.foreignPkgs[name]++
			continue
		}
		c.filesInPkg++
		visit(path, fset, file)
	}

	// ПРЕДПОСЫЛКА ОБХОДА. Файлов В ПАКЕТЕ заведомо больше одного; единица
	// означала бы, что обход вернулся к чтению единственного дома — то есть
	// ровно к той слепоте, ради снятия которой он заведён, — и «ноль находок»
	// снова стало бы «мы не смотрели». Считается ПОСЛЕ отбора: до отбора это
	// число о каталоге, а судится пакет.
	if c.filesInPkg <= 1 {
		t.Fatalf("обход пакета %s дал файлов пакета %d (%s) — не больше, чем давало "+
			"чтение одного дома; это отказ обхода, а не пустой успех", dir, c.filesInPkg, c)
	}
	return c
}

// TestGitRevRemedyDictionaryIsCountedFromItsDeclarations — ПЕРЕЧЕНЬ И
// ОБЪЯВЛЕНИЯ — ДВА МЕСТА ОБ ОДНОМ ПРЕДМЕТЕ.
//
// [gitRevRemedies] перечисляет клаузы вручную, и разойтись с блоком констант он
// может молча: добавили пятую константу, в перечень не внесли — перепись судящая
// продолжает считать четыре и остаётся зелёной, ничего не зная о пятой.
//
// Спросить у Go «все константы с этим префиксом» нечем: отражения над
// объявлениями пакета нет. Поэтому счёт снимается РАЗБОРОМ — узлами объявления,
// а не поиском подстроки: комментарий, называющий имя клаузы, законен и за
// объявление не проходит.
//
// Разбор идёт по ВСЕМУ ПАКЕТУ и только по нему (см. [repohygienePackageWalk]).
// Первая редакция читала один файл, и её заголовок обещал шире тела: пятая
// клауза, объявленная соседним файлом того же пакета, проходила молча —
// измерено инъекцией, обе переписи напечатали «4 · 4». Вторая читала весь
// КАТАЛОГ, и тело обещало шире предмета: константа чужого пакета
// (`repohygiene_test`) давала ЛОЖНУЮ находку — словарём она не является и в
// [gitRevRemedies] попасть не может by construction.
func TestGitRevRemedyDictionaryIsCountedFromItsDeclarations(t *testing.T) {
	t.Parallel()

	names := map[string]string{}
	census := repohygienePackageWalk(t, func(path string, fset *token.FileSet, file *ast.File) {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, n := range vs.Names {
					if strings.HasPrefix(n.Name, "gitRevRemedy") {
						names[n.Name] = fmt.Sprintf("%s:%d", filepath.Base(path),
							fset.Position(n.Pos()).Line)
					}
				}
			}
		}
	})

	// Предпосылка: разбор обязан НАЙТИ объявления. Ноль имён означает, что дом
	// словаря переехал либо разбор перестал его видеть, — и то и другое находка,
	// а не повод молчать.
	if len(names) == 0 {
		t.Fatalf("в пакете (%s) не нашлось ни одного объявления `gitRevRemedy*` — дом "+
			"словаря переехал либо разбор ослеп; молчание здесь означало бы "+
			"«не смотрели»", census)
	}
	t.Logf("перепись: %s · объявлений `gitRevRemedy*` %d %v · строк перечня %d",
		census, len(names), names, len(gitRevRemedies()))

	if len(names) != len(gitRevRemedies()) {
		t.Errorf("объявлено констант %d, а перечень отдаёт %d — два места об одном "+
			"предмете разошлись, и несуженная клауза прошла бы молча: %v",
			len(names), len(gitRevRemedies()), names)
	}
}
