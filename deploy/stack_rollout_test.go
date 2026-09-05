// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stack_rollout_test.go — у КАЖДОГО стенда таблицы есть исполнимое место,
// которое его раскатывает, и это место читает цепочку ИЗ ТАБЛИЦЫ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// stack_table_test.go рядом держит, что цепочка объявлена в ОДНОМ месте и что
// таблица знает каждый профиль дерева. Обе проверки судят ОБЪЯВЛЕНИЕ — и обе
// остаются зелёными, когда объявленную цепочку не раскатывает никто.
//
// Замер на release/identity-3 (2026-08-24) показал ровно это: из шести стендов
// таблицы раскатывающее место было у трёх. У prod, prorobotech и a8f60d команда
// `helm upgrade` жила КОММЕНТАРИЕМ в шапке собственного профиля — то есть её
// набирали рукой. Комментарий исполнить нельзя, и проверки его не читают
// намеренно (проза о соседних профилях законна), поэтому расхождение
// комментария с таблицей не даёт красного НИ ОДНОЙ проверке.
//
// Цена наблюдалась вживую: ручка, объявленная в профиле, до кластера не
// доехала, потому что раскатывавший повторил прежнюю команду. Страж старта
// отказал, реестр остался работать на ПРЕДЫДУЩЕМ образе, а стенд выглядел
// выкаченным полностью.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ РАСКАТЫВАЮЩИМ МЕСТОМ
//
// Отслеживаемый НЕ-тестовый файл, который делает ОБА дела сразу:
//  1. резолвит цепочку стенда через единственный читатель таблицы
//     (`stacks.sh --args|--chain <имя>` либо `$(call STACK_ARGS,<имя>)`);
//  2. зовёт `helm upgrade|install` в своей ИСПОЛНЯЕМОЙ части.
//
// Оба условия нужны вместе. Только (1) — это гейт: он читает цепочку и ничего
// не раскатывает. Только (2) — это ручная команда: она раскатывает мимо
// таблицы, а такую копию уже ловит TestNoSecondCopyOfAStackChain.
//
// Место бывает ИМЕНОВАННЫМ (называет стенд литералом) либо ОБЩИМ (принимает имя
// параметром). Общее засчитывается всем стендам сразу — но лишь тогда, когда
// множество принимаемых имён оно ВЫВОДИТ из таблицы, а не выписывает: место,
// принимающее выписанный список, расходится с таблицей ровно так же, как
// расходилась команда в комментарии.
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// helmRollout — глагол helm, который МЕНЯЕТ кластер. `template`/`lint`/`dep`
// сюда не входят by construction: они кластера не касаются, и файл, который
// только рендерит, раскатывающим местом не является.
var helmRollout = regexp.MustCompile(`helm\s+(upgrade|install)\b`)

// chainReaderGeneric — резолв цепочки, имя стенда в котором задано ПАРАМЕТРОМ,
// а не литералом (`--args $(1)`, `--chain "$STACK"`, `$(call STACK_ARGS,$(STACK))`).
var chainReaderGeneric = regexp.MustCompile(
	`stacks\.sh"?\s+--(?:args|chain)\s+["']?\$|STACK_ARGS,\s*\$`)

// tableDerivedNames — множество принимаемых имён ВЫВЕДЕНО из таблицы.
// Единственный способ его вывести в этом дереве — спросить у читателя таблицы.
//
// ГРАНИЦА, названная честно: совпадение ищется по ФАЙЛУ, а не по телу цели.
// Значит гейт держит «место живёт в файле, который выводит имена из таблицы», а
// не «именно эта цель сверяется с таблицей». Внутри одного файла связь между
// выводом имён и проверкой имени гейт не доказывает — её доказывает инъекция
// (TestRolloutSitePredicates_SelfTest, пункт «д») и прогон самой цели с именем
// вне таблицы. Точнее — значило бы разбирать тело цели make, а это второй
// разборщик Makefile в дереве рядом с уже существующим; цена выше свойства.
var tableDerivedNames = regexp.MustCompile(`stacks\.sh"?\s+--names`)

// chainReaderFor строит предикат «этот текст резолвит цепочку ИМЕННО стенда name».
//
// Имя завершается ГРАНИЦЕЙ, и это не педантизм. Без якоря `--args dev`
// совпадает с `--args dev-prod`, потому что дефис — не словесный символ:
// стенд dev засчитывался бы раскатанным по чужой строке. Класс наблюдался
// на первом же прогоне переписи по этой задаче.
func chainReaderFor(name string) *regexp.Regexp {
	q := regexp.QuoteMeta(name)
	return regexp.MustCompile(
		`STACK_ARGS,\s*` + q + `\s*\)` + // $(call STACK_ARGS,dev)
			`|--(?:args|chain)\s+["']?` + q + `(?:["' ]|$)`) // --args dev / --chain "dev" ' '
}

// executablePart — исполняемая часть файла: строки-комментарии вырезаны
// целиком. Гейт обязан читать то, что ИСПОЛНЯЕТСЯ: команда выкатки, набранная
// в комментарии, — ровно тот дефект, ради которого эта проверка написана, и
// засчитывать её за раскатывающее место значило бы объявить дефект нормой.
func executablePart(body string) string {
	var out []string
	for _, raw := range strings.Split(body, "\n") {
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") {
			continue
		}
		out = append(out, raw)
	}
	return strings.Join(out, "\n")
}

// rolloutSiteCorpus — отслеживаемые НЕ-тестовые файлы, способные раскатать
// стенд. Тесты и инъекторы дефектов исключены by construction: они читают
// цепочку законно и кластер продукта не поднимают, а засчитанные за
// раскатывающее место они зеленили бы проверку на дереве, где не раскатывает
// никто.
func rolloutSiteCorpus(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, dir := range []string{"deploy", ".github/workflows"} {
		root := filepath.Join(repoRoot, dir)
		files, err := treecorpus.Under(root)
		if err != nil {
			t.Fatalf("состав %s: %v — «ноль находок» стало бы свойством рабочего каталога", root, err)
		}
		for _, p := range files {
			base := filepath.Base(p)
			if strings.HasSuffix(base, "_test.go") ||
				strings.HasSuffix(base, "-test.sh") ||
				strings.HasPrefix(base, "inject-") ||
				strings.Contains(p, string(filepath.Separator)+"tests"+string(filepath.Separator)) {
				continue
			}
			if strings.HasSuffix(base, ".sh") || base == "Makefile" ||
				strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml") {
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// genericRolloutSites — общие раскатывающие места: зовут helm, резолвят цепочку
// по параметру И выводят множество имён из таблицы.
func genericRolloutSites(t *testing.T, corpus []string) []string {
	t.Helper()
	var out []string
	for _, p := range corpus {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		exec := executablePart(string(raw))
		if helmRollout.MatchString(exec) &&
			chainReaderGeneric.MatchString(exec) &&
			tableDerivedNames.MatchString(exec) {
			rel, _ := filepath.Rel(mustAbs(t, repoRoot), p)
			out = append(out, rel)
		}
	}
	return out
}

// namedRolloutSites — раскатывающие места, называющие стенд литералом.
func namedRolloutSites(t *testing.T, corpus []string, name string) []string {
	t.Helper()
	reader := chainReaderFor(name)
	var out []string
	for _, p := range corpus {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		exec := executablePart(string(raw))
		if helmRollout.MatchString(exec) && reader.MatchString(exec) {
			rel, _ := filepath.Rel(mustAbs(t, repoRoot), p)
			out = append(out, rel)
		}
	}
	return out
}

// recordedManualStands — стенды, раскатываемые рукой ОСОЗНАННО. Запись несёт
// причину И предикат, по которому истекает сама.
//
// Пуст намеренно: пустая ведомость — это ЦЕЛЬ, а не поломка, и проверка на ней
// обязана проходить, объявив перепись. Проба, падающая на пустой ведомости,
// подталкивала бы держать запись ради зелёного.
var recordedManualStands = map[string]struct {
	why     string
	subject func(t *testing.T) (bool, string)
}{}

func TestEveryStackHasARolloutSiteReadingTheTable(t *testing.T) {
	chains := deployStacks(t)
	corpus := rolloutSiteCorpus(t)
	generic := genericRolloutSites(t, corpus)

	// Предпосылка: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	if len(corpus) == 0 || len(chains) == 0 {
		t.Fatalf("обход ничего не прочитал: файлов в корпусе=%d, стеков в таблице=%d — "+
			"предикат перестал узнавать дерево, а не дерево стало чистым", len(corpus), len(chains))
	}

	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)

	covered, manual, missing := 0, 0, 0
	for _, name := range names {
		// Именованное место предпочитается общему, и это не косметика отчёта:
		// стенд со своим скриптом раскатывается ИМ, а не общей целью, и назвать
		// его покрытым общей значило бы сказать о дереве неправду.
		if named := namedRolloutSites(t, corpus, name); len(named) > 0 {
			covered++
			t.Logf("стенд %-12s раскатывает своё место: %s", name, strings.Join(named, ", "))
			continue
		}
		if len(generic) > 0 {
			covered++
			t.Logf("стенд %-12s раскатывает общее место: %s", name, strings.Join(generic, ", "))
			continue
		}
		if ex, recorded := recordedManualStands[name]; recorded {
			manual++
			t.Logf("стенд %-12s раскатывается рукой осознанно: %s", name, ex.why)
			continue
		}
		missing++
		t.Errorf("стенд %q объявлен цепочкой %s, но раскатывать его НЕЧЕМ: "+
			"ни одно исполнимое место дерева не резолвит его цепочку через таблицу и не зовёт helm. "+
			"Значит команда выкатки набирается рукой, и ручка, объявленная в профиле, доедет до "+
			"кластера только если о ней вспомнят. Заведи раскатывающее место (общее — "+
			"`make -C deploy stack-up STACK=%s`) либо запиши стенд в recordedManualStands "+
			"с причиной и предикатом истечения",
			name, strings.Join(chains[name], ","), name)
	}

	t.Logf("перепись: стеков в таблице — %d; раскатываемых — %d; рукой по записи — %d; "+
		"без раскатывающего места — %d; файлов осмотрено — %d; общих мест — %d (%s)",
		len(chains), covered, manual, missing, len(corpus), len(generic), strings.Join(generic, ", "))
}

// Записанное ручное исключение обязано иметь предмет.
func TestRecordedManualStands_StillHaveASubject(t *testing.T) {
	chains := deployStacks(t)
	for name, ex := range recordedManualStands {
		if _, ok := chains[name]; !ok {
			t.Errorf("записи %q больше нечего исключать — такого стенда в таблице нет. "+
				"Удали её: исключение, пережившее свой предмет, разрешает то, чего нет", name)
			continue
		}
		ok, why := ex.subject(t)
		if !ok {
			t.Errorf("запись %q потеряла основание: %s (было: %s)", name, why, ex.why)
			continue
		}
		t.Logf("запись %q: предмет есть (%s)", name, why)
	}
	t.Logf("записанных ручных стендов — %d", len(recordedManualStands))
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на входе той же формы, что в дереве.

// Граница имени. Без якоря `--args dev` совпадает с `--args dev-prod`: дефис не
// словесный символ, поэтому `\b` его не отсекает, а подстрока — тем более.
// Стенд засчитывался бы раскатанным ПО ЧУЖОЙ СТРОКЕ, и «раскатывать нечем»
// стало бы неотличимо от «раскатывает сосед». Класс наблюдался на первом же
// прогоне переписи по этой задаче — предикат из тела задачи дал dev=2, тогда
// как раскатывающее место у dev одно.
func TestChainReaderFor_AnchorsAtTheNameBoundary(t *testing.T) {
	// (а) законные формы записи ИМЕННО этого стенда — обязаны совпасть.
	for _, ok := range []string{
		"\t  $(call STACK_ARGS,dev) \\",
		"bash tests/helm/stacks.sh --args dev ./helm/umbrella",
		`FE="$(bash stacks.sh --chain dev ' ')"`,
		`bash tests/helm/stacks.sh --args "dev" ./helm/umbrella`,
	} {
		if !chainReaderFor("dev").MatchString(ok) {
			t.Errorf("резолв цепочки dev не опознан: %q", ok)
		}
	}

	// (б) законный БЛИЗНЕЦ — соседний стенд, чьё имя начинается тем же
	//     префиксом. Молчит: иначе dev «раскатывался» бы строкой dev-prod.
	for _, twin := range []string{
		"\t  $(call STACK_ARGS,dev-prod) \\",
		"bash tests/helm/stacks.sh --args dev-prod ./helm/umbrella",
		`bash tests/helm/stacks.sh --args "dev-prod" ./helm/umbrella`,
		"bash tests/helm/stacks.sh --chain devx ' '",
	} {
		if chainReaderFor("dev").MatchString(twin) {
			t.Errorf("строка соседнего стенда засчитана стенду dev: %q — "+
				"предикат сверяет подстроку без якоря по границе имени", twin)
		}
	}

	// (в) обратная сторона той же границы: dev-prod не опознаётся по строке dev.
	if chainReaderFor("dev-prod").MatchString("$(call STACK_ARGS,dev)") {
		t.Error("строка dev засчитана стенду dev-prod")
	}
}

// Раскатывающее место — это ДВА условия сразу. Проверка каждого по отдельности
// зеленела бы на гейте (читает цепочку, ничего не раскатывает) и на ручной
// команде (раскатывает мимо таблицы).
func TestRolloutSitePredicates_SelfTest(t *testing.T) {
	const (
		reads   = "\targs=$(bash tests/helm/stacks.sh --args prod ./helm/umbrella)\n"
		runs    = "\thelm upgrade --install kacho-umbrella ./helm/umbrella -n kacho $$args\n"
		names   = "\tknown=$(bash tests/helm/stacks.sh --names)\n"
		generic = "\targs=$(bash tests/helm/stacks.sh --args \"$(STACK)\" ./helm/umbrella)\n"
	)

	// (а) внесённый дефект: команда выкатки живёт КОММЕНТАРИЕМ — ровно та форма,
	//     ради которой гейт написан. Исполняемая часть пуста ⇒ местом не является.
	commented := "#   helm upgrade --install kacho-umbrella ./helm/umbrella \\\n" +
		"#     -f ./helm/umbrella/values.prod.yaml\n"
	if e := executablePart(commented); helmRollout.MatchString(e) {
		t.Errorf("команда в комментарии засчитана за исполняемую: %q", e)
	}

	// (б) законный близнец — гейт: цепочку читает, helm не зовёт.
	if e := executablePart(reads); helmRollout.MatchString(e) {
		t.Error("файл, только читающий цепочку, принят за раскатывающий")
	}
	if !chainReaderFor("prod").MatchString(executablePart(reads)) {
		t.Error("резолв цепочки prod не опознан в исполняемой части")
	}

	// (в) законный близнец — ручная команда: helm зовёт, таблицу не читает.
	//
	// Имена профилей здесь НАМЕРЕННО вымышленные. Соседний гейт
	// (TestNoSecondCopyOfAStackChain) считает две РЕАЛЬНЫХ строки профилей в
	// одной логической строке второй копией цепочки — и он прав: фикстура с
	// настоящими именами неотличима от выписанного состава стенда. Он поймал
	// эту фикстуру в первой редакции файла; чиню фикстуру, а не расширяю его
	// список исключений — исключение стоило бы дороже, чем два вымышленных имени.
	hand := "\thelm upgrade --install kacho-umbrella . -f values.alpha.yaml -f values.beta.yaml\n"
	if chainReaderFor("prod").MatchString(hand) {
		t.Error("выписанная руками цепочка принята за чтение таблицы")
	}

	// (г) оба условия вместе — место засчитывается.
	if e := executablePart(reads + runs); !(helmRollout.MatchString(e) && chainReaderFor("prod").MatchString(e)) {
		t.Error("файл, читающий цепочку И зовущий helm, не признан раскатывающим местом")
	}

	// (д) ОБЩЕЕ место требует ТРЁХ условий. Снятие вывода имён из таблицы —
	//     внесённый дефект: место с выписанным списком расходится с таблицей
	//     ровно так же, как расходилась команда в комментарии.
	full := executablePart(generic + names + runs)
	if !(helmRollout.MatchString(full) && chainReaderGeneric.MatchString(full) && tableDerivedNames.MatchString(full)) {
		t.Error("общее место не опознано при всех трёх условиях")
	}
	without := executablePart(generic + runs) // множество имён больше не выводится
	if tableDerivedNames.MatchString(without) {
		t.Error("место без вывода имён из таблицы засчитано общим — тогда выписанный " +
			"список имён проходил бы за чтение таблицы")
	}

	// (е) `helm template` кластера не касается и раскатывающим местом не делает.
	if helmRollout.MatchString("\thelm template kacho-umbrella . -f values.prod.yaml\n") {
		t.Error("helm template принят за раскатку — гейт стал бы засчитывать рендер")
	}
}

// Предикаты обязаны узнавать НАСТОЯЩЕЕ дерево, а не только синтетику.
func TestRolloutPredicates_RecogniseTheRealTree(t *testing.T) {
	corpus := rolloutSiteCorpus(t)
	if len(corpus) == 0 {
		t.Fatal("корпус пуст — «ноль находок» стало бы свойством рабочего каталога")
	}
	root := mustAbs(t, repoRoot)
	want := map[string]bool{
		filepath.Join("deploy", "Makefile"):                              false,
		filepath.Join("deploy", "helm", "umbrella", "cutover-fe3455.sh"): false,
	}
	for _, p := range corpus {
		if rel, err := filepath.Rel(root, p); err == nil {
			if _, ok := want[rel]; ok {
				want[rel] = true
			}
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("корпус не узнаёт живое раскатывающее место %s (файлов в корпусе %d)", name, len(corpus))
		}
	}

	// Отрицание — только в паре с положительным: корпус, в который попадает
	// что угодно, зеленит проверку выше. Тесты и инъекторы дефектов читают
	// цепочку законно и раскатывающими местами не являются.
	for _, p := range corpus {
		base := filepath.Base(p)
		if strings.HasSuffix(base, "-test.sh") || strings.HasSuffix(base, "_test.go") ||
			strings.HasPrefix(base, "inject-") {
			rel, _ := filepath.Rel(root, p)
			t.Errorf("%s попал в корпус раскатывающих мест — гейт засчитал бы проверку за раскатку", rel)
		}
	}
	t.Logf("перепись: файлов в корпусе раскатывающих мест — %d", len(corpus))
}
