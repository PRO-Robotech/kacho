// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// gatecarrierbase_test.go — держатель одного свойства [gateCarrierBase]: КОГДА
// РОДИТЕЛЬ HEAD НЕ РАЗРЕШАЕТСЯ, ОТКАЗ НАЗЫВАЕТ ИСТИННУЮ ПРИЧИНУ.
//
// Причин две, и они лечатся противоположным: у HEAD действительно нет родителя
// (корневой коммит — сравнить не с чем) либо родитель есть у источника и просто
// не довезён мелким клоном (чинится глубиной). Код возврата их не разделяет —
// обе приходят единицей (замер в шапке gitrevcause.go), — и первая редакция
// объявляла второй случай первым: утверждала «у него НЕТ родителя» там, где
// родитель был, и ремонта не называла вовсе.

// gateBaseOriginRepo — источник под пробу: репозиторий с ветвью `main` и
// заданным числом пустых коммитов.
func gateBaseOriginRepo(t *testing.T, commits int) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "origin")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("каталог источника: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := gitenv.Command(dir, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v в источнике: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main", ".")
	for i := 0; i < commits; i++ {
		run("-c", "user.email=probe@example.invalid", "-c", "user.name=probe",
			"commit", "-q", "--allow-empty", "-m", "c")
	}
	return dir
}

// gateBaseClone — клон источника, мелкий или полный, с проверкой предпосылок:
// глубина именно та, что заказана, и ссылка линии в клоне ЕСТЬ — иначе разбор
// пошёл бы другой ветвью, и вердикт был бы не о том.
func gateBaseClone(t *testing.T, origin string, shallow bool) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "clone")
	args := []string{"clone", "-q"}
	if shallow {
		args = append(args, "--depth=1")
	}
	args = append(args, "file://"+origin, dst)
	if out, err := gitenv.Command(filepath.Dir(dst), args...).CombinedOutput(); err != nil {
		t.Fatalf("клон не создан (%v): %s — условие пробы не создано, и её молчание "+
			"ничего не значило бы", err, out)
	}
	if got := gitCloneIsShallow(dst); got != shallow {
		t.Fatalf("клон мелкий=%v, заказан мелкий=%v — предпосылка пробы не выполнена", got, shallow)
	}
	out, err := gitenv.Command(dst, "for-each-ref", "--format=%(refname)",
		"refs/remotes/origin/main").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		t.Fatalf("в клоне нет ссылки линии refs/remotes/origin/main (%v) — разбор "+
			"пошёл бы ветвью живой полосы, и проба судила бы не свой предмет", err)
	}
	if err := gitenv.Command(dst, "merge-base", "--is-ancestor", "HEAD",
		"refs/remotes/origin/main").Run(); err != nil {
		t.Fatalf("HEAD не признан принадлежащим линии (%v) — предпосылка пробы "+
			"не выполнена", err)
	}
	return dst
}

// gateBaseRequiresUnsetKnob — ручка, объявляющая базу точно, перебивает ВЕСЬ
// разбор. Если она выставлена в окружении прогона, проба судила бы первую
// строку функции, а не предмет.
func gateBaseRequiresUnsetKnob(t *testing.T) {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv(gateCarrierBaseEnv)); v != "" {
		t.Fatalf("%s=%s выставлена в окружении прогона — разбор базы не исполняется, "+
			"и молчание пробы ничего не значило бы. Это отказ ПРЕДПОСЫЛКИ, а не "+
			"отсутствие предмета", gateCarrierBaseEnv, v)
	}
}

// TestGateCarrierBaseTellsAnUndeliveredParentFromAMissingOne — МЕЛКИЙ КЛОН НЕ
// ОБЪЯВЛЯЕТСЯ КОРНЕВЫМ КОММИТОМ.
//
// Пара «мелкий против полного» одно-фактна: тот же источник, та же ссылка
// линии, та же голова — меняется ровно глубина клона.
func TestGateCarrierBaseTellsAnUndeliveredParentFromAMissingOne(t *testing.T) {
	t.Parallel()
	gateBaseRequiresUnsetKnob(t)

	origin := gateBaseOriginRepo(t, 2)

	// Законный близнец: полный клон того же источника родителя ЗНАЕТ, и отказа
	// нет вовсе. Без него красное ниже достигалось бы отказом на чём угодно.
	full := gateBaseClone(t, origin, false)
	rev, how, err := gateCarrierBase(full)
	if err != nil {
		t.Fatalf("полный клон не дал базы: %v — близнец не зелёный", err)
	}
	t.Logf("близнец: база %s (%s)", rev, how)
	if rev == "" {
		t.Error("полный клон дал пустую базу при отсутствии отказа — пустой успех")
	}

	// Отрицательный конец: тот же источник, мелкий клон. Родитель существует у
	// источника и просто не довезён.
	shallow := gateBaseClone(t, origin, true)
	_, _, err = gateCarrierBase(shallow)
	if err == nil {
		t.Fatal("мелкий клон дал базу, которой у него нет — fail-open")
	}
	t.Logf("текст отказа мелкому клону: %v", err)
	if !gitRevRemedyNamed(err, gitRevRemedyCloneDepth) {
		t.Errorf("отказ не называет ремонта глубиной клона — читающий пойдёт искать "+
			"несуществующую причину: %v", err)
	}
	if strings.Contains(err.Error(), "нет родителя") {
		t.Errorf("отказ УТВЕРЖДАЕТ отсутствие родителя, которого не проверял: родитель "+
			"есть у источника, он не довезён — это ложь о причине: %v", err)
	}
	// Предикат предъявимости — на НАСТОЯЩЕМ отказе целого производителя, а не
	// только на синтетике соседней пробы: цепь обязана доживать до вызывающего
	// через весь путь gateCarrierBase.
	if !errors.As(err, new(*exec.ExitError)) {
		t.Errorf("errors.As(*exec.ExitError) ложен на отказе НАСТОЯЩЕГО производителя — "+
			"причина пересказана прозой: %v", err)
	}
}

// TestGateCarrierBaseNamesARootCommitAsItIs — ВТОРАЯ ПОЛОВИНА ПАРЫ: когда
// родителя действительно нет, отказ говорит именно это и глубину НЕ поминает.
//
// Без неё правка выше была бы односторонней: «всегда вини глубину» прошло бы
// первую пробу и лгало бы ровно наоборот.
func TestGateCarrierBaseNamesARootCommitAsItIs(t *testing.T) {
	t.Parallel()
	gateBaseRequiresUnsetKnob(t)

	// Полный клон источника, у которого ОДИН коммит: родителя нет по существу.
	full := gateBaseClone(t, gateBaseOriginRepo(t, 1), false)
	_, _, err := gateCarrierBase(full)
	if err == nil {
		t.Fatal("корневой коммит дал базу — сравнивать было не с чем, а отказа нет")
	}
	t.Logf("текст отказа корневому коммиту: %v", err)
	if gitRevRemedyNamed(err, gitRevRemedyCloneDepth) {
		t.Errorf("ПОЛНОМУ клону велено чинить глубину — ремонт назван противоположный "+
			"истинному: %v", err)
	}
	// Причина утверждается КЛАУЗОЙ ОБЩЕГО СЛОВАРЯ, а не подстрокой по прозе.
	// Прежняя редакция искала слово «корневой» — то есть ровно тот поиск по
	// свободному тексту, ради снятия которого словарь заводился; и заведена она
	// была потому, что четвёртой клаузы в словаре не существовало, а ветвь
	// корневого коммита ремонта не называла вовсе.
	if !gitRevRemedyNamed(err, gitRevRemedyDeclaredBase) {
		t.Errorf("отказ не называет своего ремонта %q: %v", gitRevRemedyDeclaredBase, err)
	}
	if !errors.As(err, new(*exec.ExitError)) {
		t.Errorf("errors.As(*exec.ExitError) ложен на отказе НАСТОЯЩЕГО производителя — "+
			"причина пересказана прозой: %v", err)
	}
}

// TestGateCarrierNoParentRefusalCarriesTheToolsAnswer — ОТКАЗ НЕСЁТ ОТВЕТ
// ИНСТРУМЕНТА, А НЕ ТОЛЬКО СВОЙ ВЫВОД О НЁМ.
//
// Обе ветви УТВЕРЖДАЮТ причину по отдельному вопросу о глубине, а не по тому,
// что ответил `rev-parse`. Утверждение, выбросившее единственное своё
// опровержение, проверить нечем: при отказе иного рода — битый объект, права —
// код так же уверенно назовёт корневой коммит либо недовезённость. Сегодня этот
// вход недостижим (путь идёт после успешных `for-each-ref` и `merge-base`),
// поэтому ложного зелёного нет; недостижимость же есть свойство МАРШРУТА и
// переживёт его молча.
//
// Пара одно-фактна: те же ref, глубина и вывод — меняется ровно ошибка.
func TestGateCarrierNoParentRefusalCarriesTheToolsAnswer(t *testing.T) {
	t.Parallel()

	const ref = "refs/remotes/origin/main"

	// НАСТОЯЩИЙ отказ инструмента, а не сочинённый: `*exec.ExitError` берётся у
	// той же команды и того же кода 1, что приходит на этот путь в жизни. На
	// сочинённом `errors.New` предикат `errors.As` был бы ложен by construction,
	// и проба доказывала бы свойство своей выдумки.
	realExit := gateBaseRealExitError(t)
	var probe *exec.ExitError
	if !errors.As(realExit, &probe) {
		t.Fatalf("вход пробы не является отказом инструмента (%T) — условие не "+
			"создано, и её исход ничего не значил бы", realExit)
	}

	for _, shallow := range []bool{false, true} {
		// Законный близнец: достижимый сегодня вход — код 1 с пустым выводом.
		reachable := gateCarrierNoParentRefusal(ref, shallow, nil, realExit)

		// ПРЕДИКАТ ЗАКРЫТИЯ. Не «текст называет ответ инструмента» и не
		// вхождение подстроки: причина обязана быть ПРЕДЪЯВИМА на возвращённом
		// отказе. Текст, пересказавший ошибку прозой, полон и при этом обрывает
		// цепь — различить отказы можно было бы только чтением строки.
		if !errors.As(reachable, new(*exec.ExitError)) {
			t.Errorf("мелкий=%v: errors.As(*exec.ExitError) ЛОЖЕН на возвращённом "+
				"отказе — причина пересказана прозой, а цепь оборвана: %v", shallow, reachable)
		}
		if !errors.Is(reachable, realExit) {
			t.Errorf("мелкий=%v: errors.Is не находит исходной ошибки: %v", shallow, reachable)
		}

		// Тот же вход, ОДИН изменённый факт: отказ иного рода. Текст обязан
		// смениться и понести именно его.
		other := gateCarrierNoParentRefusal(ref, shallow, nil,
			errors.New("error: object file .git/objects/4b/825d is empty"))
		if !strings.Contains(other.Error(), "object file") {
			t.Errorf("мелкий=%v: отказ иного рода не доехал до читающего: %v", shallow, other)
		}
		if errors.As(other, new(*exec.ExitError)) {
			t.Errorf("мелкий=%v: отказ иного рода предъявлен как отказ инструмента: %v",
				shallow, other)
		}
		if reachable.Error() == other.Error() {
			t.Errorf("мелкий=%v: два разных ответа инструмента дали один текст — "+
				"утверждение о причине неопровержимо by construction: %q",
				shallow, reachable.Error())
		}

		// Вторая половина дизъюнкции: инструмент НЕ отказал, а ревизии не назвал.
		// Оборачивать нечего, и состояние названо ОБЪЯВЛЕННЫМ словом, а не
		// побочным выводом `%v` от nil: за литерал `<nil>`, не объявленный
		// нигде, утверждение держаться не вправе.
		silent := gateCarrierNoParentRefusal(ref, shallow, []byte("  \n"), nil)
		if !strings.Contains(silent.Error(), gateCarrierToolDidNotRefuse) {
			t.Errorf("мелкий=%v: пустой ответ без отказа не назван словом: %v", shallow, silent)
		}
		if errors.As(silent, new(*exec.ExitError)) {
			t.Errorf("мелкий=%v: отказа инструмента не было, а он предъявлен: %v", shallow, silent)
		}
		// Предикат печатается, а не только утверждается: читающий прогон видит
		// ОБА его значения и не обязан верить зелёному на слово.
		t.Logf("мелкий=%v · errors.As(*exec.ExitError): на отказе инструмента %v, "+
			"на «инструмент не отказал» %v", shallow,
			errors.As(reachable, new(*exec.ExitError)), errors.As(silent, new(*exec.ExitError)))
		if silent.Error() == reachable.Error() {
			t.Errorf("мелкий=%v: «инструмент не отказал» и «инструмент отказал» дали "+
				"один текст: %q", shallow, silent.Error())
		}

		// ОБЕ ВЕТВИ ОГОВОРЕНЫ ОДИНАКОВО. Классификация выведена из ОДНОГО
		// признака — глубины клона, — и опровергает её только приписанная
		// улика. Категорическое утверждение рядом с гадательным оставляло класс
		// закрытым на две трети: при отказе иного рода оно осталось бы ложным.
		// Асимметрия рождается молча, поэтому оговорка берётся из объявленного
		// источника, а не выписывается в каждой ветви своими словами.
		if !strings.Contains(reachable.Error(), gateCarrierHedgedCause(shallow)) {
			t.Errorf("мелкий=%v: ветвь утверждает причину КАТЕГОРИЧЕСКИ, опираясь на "+
				"один признак; оговоренной клаузы %q в отказе нет: %v",
				shallow, gateCarrierHedgedCause(shallow), reachable)
		}
	}

	// Ремонты двух ветвей ПРОТИВОПОЛОЖНЫ и не перепутаны, и исключение полно по
	// ВСЕМУ словарю, а не по одной соседней клаузе: выписанная рядом пара
	// разошлась бы со словарём молча.
	for _, c := range []struct {
		name    string
		shallow bool
		want    string
	}{
		{"корневой коммит", false, gitRevRemedyDeclaredBase},
		{"мелкий клон", true, gitRevRemedyCloneDepth},
	} {
		err := gateCarrierNoParentRefusal(ref, c.shallow, nil, realExit)
		if !gitRevRemedyNamed(err, c.want) {
			t.Errorf("%s: отказ не называет своего ремонта %q: %v", c.name, c.want, err)
		}
		for _, other := range gitRevRemedies() {
			if other == c.want {
				continue
			}
			if gitRevRemedyNamed(err, other) {
				t.Errorf("%s: отказ называет ЧУЖОЙ ремонт %q — две причины сошлись в "+
					"один совет: %v", c.name, other, err)
			}
		}
	}
}

// gateBaseRealExitError — НАСТОЯЩИЙ отказ `rev-parse`, какой приходит на этот
// путь: код 1 у дерева с одним коммитом, где родителя нет.
//
// Отдельным помощником, потому что подделать его нечем: `errors.New` даёт
// строку, на которой предикат предъявимости ложен by construction, и проба,
// построенная на ней, доказывала бы свойство своей выдумки.
func gateBaseRealExitError(t *testing.T) error {
	t.Helper()
	repo := gateBaseOriginRepo(t, 1)
	// ФИКСТУРА ОБЯЗАНА ВОСПРОИЗВОДИТЬ ТО, ЧТО ПРИХОДИТ НА БОЕВОЙ ПУТЬ, а не
	// «какой-нибудь отказ». Аргументы берутся из ОБЩЕГО ДОМА, а не переписываются
	// здесь второй раз: тождество двух записей прогоном не проверяется — код 1
	// приходит и от другой подкоманды, и от другого репозитория (измерено
	// инъекцией 2026-09-22: подмена на `cat-file -e <нет объекта>` оставила оба
	// стража ниже молчащими, `ok`, EXIT=0). Обход общего дома судит гейт
	// [TestGitRevParentQuestionHasASingleArgvHome].
	_, err := gitenv.Command(repo, gitRevParentArgv()...).Output()
	if err == nil {
		t.Fatal("дерево с одним коммитом назвало родителя HEAD — условие не создано")
	}
	// Два стража ниже судят не тождество команд, а ФОРМУ ОТВЕТА СРЕДЫ на ту же
	// команду: класс отказа и код. Признак — КОД 1: именно он неотличим у
	// корневого коммита и у недовезённого родителя, и именно ради него вся эта
	// развязка. Смена поведения `git` уехала бы сюда, и тогда вход пробы
	// перестал бы быть тем, что приходит на боевой путь.
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("на %v среда ответила не отказом инструмента (%T): %v — вход пробы "+
			"не тот, что приходит на боевой путь", gitRevParentArgv(), err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("на %v среда ответила кодом %d, а боевой путь приносит 1 — развязка "+
			"причин строилась на коде, которого больше нет", gitRevParentArgv(),
			exitErr.ExitCode())
	}
	return err
}

// gateCarrierHedgedCause — ОГОВОРЕННАЯ ПРИЧИНА ветви, собранная тем же
// шаблоном, что и производитель.
//
// Тем же, а не своей копией: утверждение, переписавшее склейку рядом, ловило бы
// переименование и молчало на подмене смысла — ровно та болезнь, от которой
// шаблон и заведён.
func gateCarrierHedgedCause(shallow bool) string {
	if shallow {
		return gateCarrierInferredCause(gateCarrierCauseUndelivered)
	}
	return gateCarrierInferredCause(gateCarrierCauseRootCommit)
}

// TestGateCarrierRefusalSaysOnlyWhatItChecked — ОТКАЗ УТВЕРЖДАЕТ РОВНО ТО, ЧТО
// ПРОВЕРИЛ.
//
// `gateCarrierToolDidNotRefuse` есть КОНЪЮНКЦИЯ: «инструмент не отказал» И
// «ревизии не назвал». Первое известно из `e`; второе — факт о ВЫВОДЕ, и
// функция его не проверяла, а утверждала. Измерено 2026-09-22: подан `e == nil`
// с непустым выводом — отказ напечатал
// `вывод "0123456789abcdef…", инструмент НЕ отказал и ревизии не назвал`,
// то есть противоречие в одном предложении, и ничто не покраснело.
//
// Пара одно-фактна: те же ref, глубина и ошибка — меняется ровно вывод.
func TestGateCarrierRefusalSaysOnlyWhatItChecked(t *testing.T) {
	t.Parallel()
	const ref = "refs/remotes/origin/main"
	const rev = "0123456789abcdef0123456789abcdef01234567"

	// Законный близнец: вывод ПУСТ — обе половины конъюнкции верны, и слово о
	// них законно.
	silent := gateCarrierNoParentRefusal(ref, false, []byte("  \n"), nil)
	t.Logf("e==nil, вывод ПУСТ: %v", silent)
	if !strings.Contains(silent.Error(), gateCarrierToolDidNotRefuse) {
		t.Errorf("пустой вывод без отказа не назван словом %q: %v",
			gateCarrierToolDidNotRefuse, silent)
	}
	if strings.Contains(silent.Error(), gateCarrierToolNamedARevision) {
		t.Errorf("пустой вывод объявлен НАЗВАННОЙ ревизией: %v", silent)
	}

	// Отрицательный конец: ровно один изменённый факт — вывод непуст.
	named := gateCarrierNoParentRefusal(ref, false, []byte(rev+"\n"), nil)
	t.Logf("e==nil, вывод НЕПУСТОЙ: %v", named)
	if strings.Contains(named.Error(), gateCarrierToolDidNotRefuse) {
		t.Errorf("отказ утверждает %q, НАПЕЧАТАВ рядом названную ревизию — "+
			"предложение противоречит себе: %v", gateCarrierToolDidNotRefuse, named)
	}
	if !strings.Contains(named.Error(), gateCarrierToolNamedARevision) {
		t.Errorf("названная ревизия не названа словом %q: %v",
			gateCarrierToolNamedARevision, named)
	}
	if !strings.Contains(named.Error(), rev) {
		t.Errorf("ответ инструмента до читающего не доехал: %v", named)
	}
	if silent.Error() == named.Error() {
		t.Errorf("два разных ответа инструмента дали один текст — утверждение о нём "+
			"неопровержимо by construction: %q", silent.Error())
	}
}

// TestGateCarrierParentUnresolvedGuardsBothHalves — СТРАЖ ВЫЗЫВАЮЩЕГО ЗАКРЕПЛЁН
// ПО ОБЕИМ СВОИМ ПОЛОВИНАМ.
//
// Вторую половину — «отказа не было, а ревизии не назвал» — не держала ни одна
// проба: `--verify --quiet` отвечает на несуществующий объект кодом 1, и пустой
// успех на боевом пути сегодня недостижим. Недостижимость есть свойство
// ИНСТРУМЕНТА, а не кода, и переживёт его молча; пустая строка, принятая за
// базу, увела бы сравнение в пустоту fail-open.
//
// Зовётся ТОТ ЖЕ код, что стоит у вызывающего, а не его копия рядом, и это
// проверяется переписью зовущих: копия доказывала бы свойство себя самой.
func TestGateCarrierParentUnresolvedGuardsBothHalves(t *testing.T) {
	t.Parallel()

	boom := errors.New("exit status 1")
	for _, c := range []struct {
		name   string
		parent []byte
		err    error
		want   bool
	}{
		{"ревизия названа, отказа нет", []byte("4374f8736\n"), nil, false},
		{"вывод пуст, отказа нет", []byte(""), nil, true},
		{"вывод — одни пробелы, отказа нет", []byte("  \n\t"), nil, true},
		{"ревизия названа, но инструмент отказал", []byte("4374f8736\n"), boom, true},
		{"вывод пуст и инструмент отказал", nil, boom, true},
	} {
		if got := gateCarrierParentUnresolved(c.parent, c.err); got != c.want {
			t.Errorf("%s: страж дал %v, ожидалось %v — половина дизъюнкции потеряна",
				c.name, got, c.want)
		}
	}

	// Страж обязан стоять у ВЫЗЫВАЮЩЕГО, а не только здесь: проба, судящая
	// функцию, которой никто не пользуется, зелена при любом коде боевого пути.
	prod, probe := packageCallSites(t, "gateCarrierParentUnresolved")
	t.Logf("перепись: зовущих `gateCarrierParentUnresolved` — боевых %d %v, пробных %d %v",
		len(prod), prod, len(probe), probe)
	if len(prod) == 0 {
		t.Errorf("страж `gateCarrierParentUnresolved` не зовёт НИ ОДИН боевой файл — " +
			"вызывающий держит условие своими руками, и проба судит чужой код")
	}
}

// TestGateCarrierHedgeIsBoundToItsCause — ОГОВОРКА СВЯЗАНА СО СВОИМ ПРЕДМЕТОМ
// ЦЕЛОЙ ФОРМУЛИРОВКОЙ, А НЕ ФРАГМЕНТОМ.
//
// Оговорка — грамматический модификатор, она втискивается ВНУТРЬ фразы, и
// утверждение о ней вырождалось в поиск подстроки `"скорее всего,"`. Измерено
// инъекцией 2026-09-22: категоричность вернули причине («родителя НЕТ по
// существу»), фрагмент оставили при другом существительном («точку сравнения
// скорее всего, вывести неоткуда») — все пять проб остались зелёными.
//
// Судятся три вещи, и каждая — свойство СКЛЕЙКИ, а не слова:
//
//  1. оговоренная причина своей ветви присутствует ЦЕЛИКОМ;
//  2. фрагмент оговорки встречается в отказе РОВНО ОДИН раз, и это вхождение —
//     то самое, что внутри клаузы: припаркованный при чужом существительном
//     второй фрагмент есть находка сам по себе;
//  3. оговорка обособлена С ОБЕИХ сторон — запятая слева приходит от
//     вызывающего, справа от шаблона, и односторонним обособление больше не
//     бывает («родителя скорее всего, нет» — прежняя редакция, обе ветви).
//
// Чужая оговоренная причина в отказе отсутствует: ветви лечатся
// противоположным, и сойтись в одном тексте им нельзя.
func TestGateCarrierHedgeIsBoundToItsCause(t *testing.T) {
	t.Parallel()
	const ref = "refs/remotes/origin/main"

	for _, shallow := range []bool{false, true} {
		text := gateCarrierNoParentRefusal(ref, shallow, nil, errors.New("exit status 1")).Error()
		mine, foreign := gateCarrierHedgedCause(shallow), gateCarrierHedgedCause(!shallow)
		t.Logf("мелкий=%v: %s", shallow, text)

		at := strings.Index(text, mine)
		if at < 0 {
			t.Errorf("мелкий=%v: оговоренной причины %q в отказе НЕТ — оговорка "+
				"оторвана от своего предмета: %q", shallow, mine, text)
			continue
		}
		if strings.Contains(text, foreign) {
			t.Errorf("мелкий=%v: отказ несёт и ЧУЖУЮ оговоренную причину %q — две "+
				"причины сошлись в одном тексте: %q", shallow, foreign, text)
		}
		if n := strings.Count(text, gateCarrierCauseHedge); n != 1 {
			t.Errorf("мелкий=%v: фрагмент оговорки %q встречается %d раз(а) — лишнее "+
				"вхождение стоит при чужом предмете: %q",
				shallow, gateCarrierCauseHedge, n, text)
		}
		if strings.Index(text, gateCarrierCauseHedge) != at {
			t.Errorf("мелкий=%v: первое вхождение оговорки — НЕ то, что внутри клаузы "+
				"%q: %q", shallow, mine, text)
		}
		if !strings.Contains(text, ", "+mine) {
			t.Errorf("мелкий=%v: оговорка обособлена ОДНОСТОРОННЕ — запятой слева нет: "+
				"%q", shallow, text)
		}
	}
}
