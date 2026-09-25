// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// foreignIDPNameLedger — ВЕДОМОСТЬ: области, где наши имена сегодня носят
// название чужого поставщика личности, с ТОЧНЫМ их числом.
//
// Ведомость — не список прощённых. Предмет гейта — не наличие имён, а то, что
// они растут и переживают поставщика незамеченными. У каждой записи стоит
// `Until` — факт о дереве, при котором её снимают; без него запись бессрочна.
//
// Число каждой записи снято обходом этого же дерева и обязано СОКРАЩАТЬСЯ.
// Расхождение ловится в обе стороны: вверх — поверхность выросла; вниз — запись
// пережила часть предмета, и её надо переписать.
//
// РЕВИЗИЯ СТОИТ У КАЖДОЙ ЗАПИСИ (`Measured`), а не общей строкой в этой шапке.
// Общая строка была ложью ровно в тот день, когда одну запись переписали, а
// шапку — нет. И она уже один раз рассудила чужую работу: числа были сняты
// обходом bec320cf47d, а судили дерево СВЕДЁННОЙ волны — и разошлись ровно на
// то, что принесло сведение. Ведомость одной полосы судила код другой, и порознь
// ни одна из них не была виновата.
var foreignIDPNameLedger = []ForeignIDPNameLedgerEntry{
	{
		Area: "gateway/",
		// Снято обходом головы ветки #2842 (было 94 на 432b3278151, до того 102
		// на 5b20df5c638): поле якоря хопа за наборами ключей названо семейством
		// транспорта хопа (`JWKSCAFile`), а не поставщиком.
		Names:    93,
		Measured: "c92fa72884f",
		Why: "край — единственное место, где платформа РАЗГОВАРИВАЕТ с поставщиком " +
			"по его протоколу: сессия входа, интроспекция, снятие сессии. Имена здесь " +
			"расходятся на две половины: координаты разговора (их снимут вместе с " +
			"полосой) и фикстуры проб, носящие название без нужды",
		Until: "полоса развода ручки посадки края закончена и в `gateway/**` не " +
			"осталось ни одного объявленного имени этой оси",
	},
	{
		Area: "deploy/",
		// 2, а не 3: число снято обходом 001fc0ced9a — головы ветки #2732 после
		// вливания предиката четырёх стражей личности на нашем признаке посадки.
		// Он снял переменную `kratosEnabled` (образец отбора стендов по флагу
		// чужого подчарта) вместе с самим отбором — снятие, а не переименование.
		Names:    2,
		Measured: "001fc0ced9a",
		Why: "пробы посадки судят профили, где поставщик объявлен подчартом: имя " +
			"ручки профиля попало в имя переменной пробы",
		Until: "подчарт поставщика снят с профилей посадки",
	},
	{
		Area:     "terraform/",
		Names:    1,
		Measured: "5b20df5c638",
		Why: "поле отображения ответа края `json:\"hydraClientId\"`: имя поля Go " +
			"повторяет имя поля контракта, и расхождение между ними читалось бы как " +
			"ошибка отображения",
		Until: "поле контракта `hydraClientId` переименовано на крае",
	},
}

// foreignIDPNameSources — дерево Go, спрошенное У ИНДЕКСА, включая пробы:
// предмет гейта живёт как раз в них.
//
// Обход диска не знает правил игнорирования и судил бы чужой рабочий каталог —
// произведённые файлы, чужие копии, остатки прогонов.
//
// Отсев — СВОЙ (ForeignIDPNameSkipRules), не унаследованный: чужой перечень под
// чужой предмет предикатом этого гейта не является. Отсеянное возвращается
// числом и по правилам, потому что «там ничего нет» обязано быть отличимо от
// «туда не смотрели».
func foreignIDPNameSources(t *testing.T) (map[string]string, int, int, []ForeignIDPNameSkipCount) {
	t.Helper()
	root := repoRoot(t)
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}
	sources := map[string]string{}
	listed, skipped := 0, 0
	byRule := map[string]int{}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		listed++
		if rule := ForeignIDPNameSkipRuleFor(rel); rule != "" {
			skipped++
			byRule[rule]++
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		sources[rel] = string(b)
	}
	counts := make([]ForeignIDPNameSkipCount, 0, len(ForeignIDPNameSkipRules))
	for _, r := range ForeignIDPNameSkipRules {
		counts = append(counts, ForeignIDPNameSkipCount{Rule: r.Name, N: byRule[r.Name]})
	}
	return sources, listed, skipped, counts
}

// TestForeignIDPNameIsBoundedByTheLedger — наши имена не носят названия чужого
// поставщика личности вне ведомости, и это утверждение О ДЕРЕВЕ.
//
// Разбор класса и граница предиката — в шапке foreignidpname.go. Здесь только
// обход дерева и вердикт.
func TestForeignIDPNameIsBoundedByTheLedger(t *testing.T) {
	t.Parallel()
	sources, listed, skipped, byRule := foreignIDPNameSources(t)

	findings, census, err := JudgeForeignIDPNames(sources, foreignIDPNameLedger)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	census.Listed, census.Skipped, census.SkippedBy = listed, skipped, byRule
	t.Log(census.String())

	// Отсеянное обязано СХОДИТЬСЯ: число, которое не сходится с предложенным и
	// прочитанным, — украшение, а не перепись.
	if listed != len(sources)+skipped {
		t.Fatalf("перепись не сходится: предложено %d, прочитано %d, отсеяно %d",
			listed, len(sources), skipped)
	}

	// Предпосылка: гейт обязан ОТКАЗЫВАТЬ на беспредметности, а не молчать.
	// Ноль разобранных файлов снаружи неотличим от «имён нет».
	if census.Files == 0 {
		t.Fatal("разобрано ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if census.Idents == 0 {
		t.Fatal("осмотрено ноль объявленных имён — разбор не дошёл до исполняемой части")
	}

	for _, f := range findings {
		switch f.Kind {
		case ForeignIDPNameUnledgered:
			t.Errorf("%s:%d: имя %q — %s.\n"+
				"Координаты поставщика (адрес, путь его API, имя ручки посадки) законны и "+
				"пишутся строковым литералом — их держат providersurface.go и "+
				"retiredissuerclaim.go. Здесь же НАШЕ имя носит его название: снимут "+
				"поставщика — имя останется ложью, которую компилятор не заметит.\n"+
				"Исходов три: снять вместе с предметом одним изменением · перевести на "+
				"производимый деревом признак (`testLegacyIss` рядом) · переутвердить новое "+
				"свойство того же предмета. «Оставить как есть» исходом не является — для "+
				"этого есть foreignIDPNameLedger, и у каждой записи стоит предикат снятия",
				f.File, f.Line, f.Name, f.Detail)
		case ForeignIDPNameCountDrift:
			t.Errorf("область %s: %s — %s.\n"+
				"Число в ведомости ТОЧНОЕ, а не потолок: потолок прощает рост до себя и "+
				"перестаёт быть наблюдением", f.File, f.Kind, f.Detail)
		case ForeignIDPNameStale:
			t.Errorf("область %s: %s — %s.\n"+
				"Ведомость обязана сокращаться вместе с деревом: запись, которой нечего "+
				"называть, молча разрешит следующее имя в этой области", f.File, f.Kind, f.Detail)
		default:
			t.Errorf("%s:%d: неизвестный вид находки %q", f.File, f.Line, f.Kind)
		}
	}
}

// TestForeignIDPNameLedgerPremiseHolds — предпосылка ведомости: она не пуста и
// её записи не вакуумны.
//
// Пустая ведомость при нуле имён — ЦЕЛЬ, а не поломка: проба, падающая на
// достижении собственной цели, подталкивает держать запись ради зелёного.
// Поэтому пустая ведомость здесь ПРОХОДИТ, объявляя перепись.
func TestForeignIDPNameLedgerPremiseHolds(t *testing.T) {
	t.Parallel()
	sources, listed, skipped, byRule := foreignIDPNameSources(t)
	hits, census, err := CollectForeignIDPNames(sources)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	census.Listed, census.Skipped, census.SkippedBy = listed, skipped, byRule
	t.Logf("%s; записей ведомости %d", census.String(), len(foreignIDPNameLedger))

	// СОСТАВ, а не только сумма: ведомость держит число, и замещение внутри
	// области («одно имя ушло, другое пришло») прошло бы молча. Здесь оно видно
	// глазами на обзоре диффа — ровно то, ради чего полоса и заведена.
	for _, row := range ForeignIDPNameComposition(hits) {
		t.Log(row)
	}

	if len(foreignIDPNameLedger) == 0 {
		t.Log("ведомость пуста — исход, к которому гейт ведёт; проверять нечего")
		if census.Names != 0 {
			t.Errorf("ведомость пуста, а имён в дереве %d — этого состояния быть не может",
				census.Names)
		}
		return
	}
	// Число о дереве верно НА РЕВИЗИИ, а не вообще. Голое число уже один раз
	// рассудило чужую работу: снятое обходом одной головы, оно судило дерево
	// сведённой — и разошлось ровно на то, что принесло сведение.
	for _, gap := range ForeignIDPNameProvenanceGaps(foreignIDPNameLedger, foreignIDPRevisionResolver(t)) {
		t.Errorf("%s.\nРасхождение читается как рост поверхности только тогда, когда "+
			"видно, ОТКУДА взято прежнее число", gap)
	}

	for _, e := range foreignIDPNameLedger {
		if strings.TrimSpace(e.Until) == "" {
			t.Errorf("запись ведомости %q без предиката снятия — она бессрочна, и снять "+
				"её будет некому", e.Area)
		}
		if e.Names <= 0 {
			t.Errorf("запись ведомости %q объявляет имён %d — запись, которой нечего "+
				"называть, заводить нельзя", e.Area, e.Names)
		}
		got := 0
		for _, h := range hits {
			if foreignIDPArea(h.File) == e.Area {
				got++
			}
		}
		if got == 0 {
			t.Errorf("запись ведомости %q не накрывает ни одного имени — она вакуумна",
				e.Area)
		}
	}
}

// foreignIDPRevisionResolver — РАЗРЕШИТЕЛЬ РЕВИЗИИ для этого дерева.
func foreignIDPRevisionResolver(t *testing.T) ForeignIDPNameRevisionResolver {
	t.Helper()
	return foreignIDPRevisionResolverAt(repoRoot(t))
}

// Три исхода разрешителя — СИГНАЛАМИ, а не подстрокой.
//
// Текст пишется человеку и будет переписан; проба, утверждающая его дословно,
// краснеет на правке формулировки и молчит на подмене смысла. Классификацию
// проверяют сигналом, а ТЕКСТ — отдельно и по тому, что в нём обязано быть:
// названному РЕМОНТУ.
var (
	// errForeignIDPRevisionAbsent — объекта нет, и клон полный: ремонт в ведомости.
	errForeignIDPRevisionAbsent = errors.New("дерево не знает объекта-коммита")
	// errForeignIDPRevisionUndelivered — объекта нет, НО дерево мелкое: объект
	// может существовать у источника. Ремонт начинается с глубины клона.
	errForeignIDPRevisionUndelivered = errors.New("объекта-коммита нет, а дерево МЕЛКОЕ")
	// errForeignIDPTreeNotAsked — дерева не спросили вовсе.
	errForeignIDPTreeNotAsked = errors.New("дерево не спрошено")
)

// Вопрос о глубине клона живёт в ОБЩЕМ доме — [gitCloneIsShallow] в
// gitrevcause.go, — вместе со словарём ремонта и замером, из которого оба
// выведены. Своя копия здесь была вторым ИСТОЧНИКОМ: тот же механизм
// `rev-parse --verify` стоит в дереве не в одном месте, и сойтись копиям нечем.

// foreignIDPRevisionResolverAt — спрашивает систему контроля версий, знает ли
// дерево по этому пути названный объект-коммит.
//
// Форма без разрешимости не судит ничего: выдуманный набор шестнадцатеричных
// знаков и ревизия ЧУЖОГО дерева выглядят как ревизия и ею не являются. Ровно
// это и есть та ложь, которой красное полосы началось, — число с чужой головы,
// объявленное снятым здесь.
//
// ПРИЧИН ТРИ, И ОНИ НЕ ОДНО И ТО ЖЕ. Направление у всех одно — fail-closed, —
// но РЕМОНТ у них разный, и одна формулировка на всех разворачивает читающего:
//
//	объекта нет           — ведомость называет то, чего в дереве не существует;
//	                        чинится записью в ведомости;
//	объект не довезён     — дерево МЕЛКОЕ, и объект может существовать у
//	                        источника; чинится глубиной клона, а ведомость,
//	                        возможно, верна. Этот случай неотличим от первого
//	                        по коду возврата, поэтому глубина спрашивается
//	                        ОТДЕЛЬНО, до классификации;
//	дерево не спрошено    — каталога нет, он не репозиторий, инструмента нет
//	                        вовсе; чинится рабочим каталогом.
//
// У ТРЕТЬЕЙ ПРИЧИНЫ ТРИ ПОДСЛУЧАЯ, И РАЗВОДИТ ИХ НЕ ЭТОТ ТЕКСТ, А ОБЁРТКА.
// Ремонт у них общий — рабочий каталог, — но чинят его разным, и сказать
// читающему, ЧТО именно не так, может только исходная ошибка, доехавшая до него
// предъявимой. Поэтому `%w` здесь ДВА: один сентинелу (классификация), второй
// причине (различение подслучая), и Go принимает их в одном вызове:
//
//	каталога нет           — *fs.PathError, op=chdir, errors.Is(fs.ErrNotExist);
//	он не репозиторий      — *exec.ExitError с кодом 128;
//	инструмента нет вовсе  — *exec.Error, errors.Is(exec.ErrNotFound).
//
// Прежняя редакция отдала `%w` сентинелу и оставила причине `%v`: текст
// по-прежнему называл три подслучая, а код не давал НИ ОДНОГО признака, по
// которому их различить, — то есть врезка обещала различение, которого больше
// не было. Держит это [TestGitRevTreeNotAskedKeepsItsCauseInspectable], парой на
// каждый подслучай; третий создаётся подпроцессом с пустым PATH — «создать это
// состояние пробой нечем» было неверно, отсутствие инструмента есть состояние
// ПРОЦЕССА, и подпроцесс его создаёт.
func foreignIDPRevisionResolverAt(root string) ForeignIDPNameRevisionResolver {
	return func(rev string) error {
		err := gitenv.Command(root, "rev-parse", "--verify", "--quiet", rev+"^{commit}").Run()
		if err == nil {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			if gitCloneIsShallow(root) {
				return fmt.Errorf("%w: ревизия %s — объект может существовать у "+
					"источника и просто не быть довезён; %s, и только если ревизия не "+
					"найдётся и полным клоном — из записи ведомости",
					errForeignIDPRevisionUndelivered, rev, gitRevRemedyCloneDepth)
			}
			return fmt.Errorf("%w %s, и клон ПОЛНЫЙ: %s",
				errForeignIDPRevisionAbsent, rev, gitRevRemedyLedger)
		}
		return fmt.Errorf("%w о ревизии %s — система контроля версий не ответила (%w); "+
			"это НЕ «число не подтвердилось», а «мы не спросили»; %s",
			errForeignIDPTreeNotAsked, rev, err, gitRevRemedyWorkingDir)
	}
}

// foreignIDPHeadRevision — голова этого дерева сокращением в одиннадцать знаков:
// законный близнец для отрицательных проб разрешителя.
func foreignIDPHeadRevision(t *testing.T) string {
	t.Helper()
	out, err := gitenv.Command(repoRoot(t), "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("голова дерева не установлена: %v — близнец взять неоткуда", err)
	}
	full := strings.TrimSpace(string(out))
	if len(full) < 11 {
		t.Fatalf("голова дерева записана как %q — сократить до одиннадцати нечем", full)
	}
	return full[:11]
}

// TestForeignIDPNameSkipRulesHaveASubject — у каждого правила отсева есть что
// отсеивать.
//
// Правило, которому нечего исключать, — находка, а не безобидная строка: оно
// сужает обход обещанием, которого никто не проверял, и переживает свой предмет
// молча. Ровно так унаследованный отсев лицензионного гейта отсеивал ноль
// файлов, оставаясь в коде.
func TestForeignIDPNameSkipRulesHaveASubject(t *testing.T) {
	t.Parallel()
	_, listed, skipped, byRule := foreignIDPNameSources(t)
	t.Logf("предложено путей %d · отсеяно путей %d · правил отсева %d",
		listed, skipped, len(ForeignIDPNameSkipRules))

	if len(ForeignIDPNameSkipRules) == 0 {
		t.Log("правил отсева ноль — судится ВСЁ предложенное; исход законный")
		if skipped != 0 {
			t.Errorf("правил ноль, а отсеяно %d путей — этого состояния быть не может", skipped)
		}
		return
	}
	for _, c := range byRule {
		if c.N == 0 {
			t.Errorf("правило отсева %q не отсеяло ни одного пути — ему нечего "+
				"исключать, и оно обязано быть снято вместе со своим предметом", c.Rule)
		}
	}
	for _, r := range ForeignIDPNameSkipRules {
		if strings.TrimSpace(r.Why) == "" {
			t.Errorf("правило отсева %q без основания — сужение обхода без довода", r.Name)
		}
	}
}

// TestForeignIDPRevisionResolverRefusesAnAbsentObject — ОТРИЦАТЕЛЬНАЯ ВЕТВЬ
// НАСТОЯЩЕГО разрешителя.
//
// На живой ведомости он зовётся только положительно: все её ревизии разрешимы,
// и текст, который увидит читающий красное, не исполняется ни разу. Инъекция
// рядом гоняет СИНТЕТИЧЕСКИЙ разрешитель и об этом тексте ничего не говорит.
//
// Текст находки объявлен на этой полосе частью свойства — значит и здесь он
// доказывается исполнением, а не чтением.
func TestForeignIDPRevisionResolverRefusesAnAbsentObject(t *testing.T) {
	t.Parallel()
	resolve := foreignIDPRevisionResolver(t)

	// Законный близнец: голова этого дерева разрешается.
	head := foreignIDPHeadRevision(t)
	if err := resolve(head); err != nil {
		t.Fatalf("голова дерева %s не разрешилась: %v — близнец не зелёный, "+
			"и отказ ниже падал бы на чём угодно", head, err)
	}

	// Дефект: форма та же, объекта нет.
	const absent = "0123456789a"
	if len(absent) != len(head) {
		t.Fatalf("концы пары разной длины (%d против %d) — менялся бы не один факт",
			len(absent), len(head))
	}
	err := resolve(absent)
	if err == nil {
		t.Fatal("ревизия, которой в дереве нет, разрешена — отказ fail-open")
	}
	t.Logf("текст отказа: %v", err)
	t.Logf("рабочая копия мелкая: %v", gitCloneIsShallow(repoRoot(t)))
	if !strings.Contains(err.Error(), absent) {
		t.Errorf("отказ не называет ревизии: %v", err)
	}
	// ИСХОД ЗАВИСИТ ОТ СРЕДЫ, и это утверждается, а не подразумевается: рабочие
	// копии этого воркспейса — МЕЛКИЕ клоны (измерено `rev-parse
	// --is-shallow-repository`: true), и тогда отсутствующий объект обязан
	// разбираться как «мог не быть довезён», а не как «перепишите ведомость».
	// На полном клоне — наоборот. Проба требует ТОГО ИЗ ДВУХ, что отвечает
	// среде, и не молчит ни в одной из них.
	if gitCloneIsShallow(repoRoot(t)) {
		if !errors.Is(err, errForeignIDPRevisionUndelivered) {
			t.Errorf("мелкая рабочая копия отнесена не к своей причине: %v", err)
		}
	} else if !errors.Is(err, errForeignIDPRevisionAbsent) {
		t.Errorf("полная рабочая копия отнесена не к своей причине: %v", err)
	}
	if errors.Is(err, errForeignIDPTreeNotAsked) {
		t.Errorf("спрошенное дерево названо неспрошенным: %v", err)
	}
}

// foreignIDPScratchRepo — СВОЁ дерево под пробу: два пустых коммита.
//
// Вход отрицательных проб обязан СОЗДАВАТЬСЯ, а не наследоваться из окружения.
// Прежняя редакция брала `t.TempDir()` как «не репозиторий» — и это было
// наследование: временный каталог ложится туда, куда укажет TMPDIR, система
// контроля версий поднимается вверх по родителям, и внутри нашего же дерева
// она находила репозиторий. Проба краснела на ИСПРАВНОМ дереве от чужой
// настройки, и прогон группы увёл бы разбор.
func foreignIDPScratchRepo(t *testing.T) (dir, older, newer string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "origin")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("каталог дерева пробы: %v", err)
	}
	run := func(args ...string) string {
		t.Helper()
		out, err := gitenv.Command(dir, args...).Output()
		if err != nil {
			t.Fatalf("git %v в дереве пробы: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", ".")
	commit := []string{"-c", "user.email=probe@example.invalid", "-c", "user.name=probe",
		"commit", "-q", "--allow-empty", "-m"}
	run(append(append([]string{}, commit...), "older")...)
	older = run("rev-parse", "HEAD")
	run(append(append([]string{}, commit...), "newer")...)
	newer = run("rev-parse", "HEAD")
	return dir, older, newer
}

// TestForeignIDPRevisionResolverSeparatesItsCauses — ПРИЧИН ТРИ, и одна на всех
// формулировка превращает «дерево не спросили» в «число не подтвердилось».
//
// Вход у обоих концов СВОЙ: дерево пробы против пути, которого нет. Каталог,
// которого нет, система контроля версий не может ни открыть, ни унаследовать у
// родителя — исход не зависит от того, где лежит TMPDIR.
func TestForeignIDPRevisionResolverSeparatesItsCauses(t *testing.T) {
	t.Parallel()
	dir, older, _ := foreignIDPScratchRepo(t)

	// Законный близнец ОТРИЦАТЕЛЬНОЙ ветви: то же построение, тот же путь —
	// дерево есть, ревизия своя, отказа нет. Без него красное ниже
	// достигалось бы отказом на чём угодно.
	if err := foreignIDPRevisionResolverAt(dir)(older); err != nil {
		t.Fatalf("своя ревизия своего дерева не разрешилась: %v — близнец не зелёный", err)
	}

	absent := foreignIDPRevisionResolverAt(dir)("0123456789a")
	notree := foreignIDPRevisionResolverAt(filepath.Join(t.TempDir(), "дерева-тут-нет"))(older)
	if absent == nil || notree == nil {
		t.Fatalf("обе ветви обязаны отказывать: объект %v, дерево %v", absent, notree)
	}
	t.Logf("объекта нет:        %v", absent)
	t.Logf("дерево не спрошено: %v", notree)
	if absent.Error() == notree.Error() {
		t.Fatalf("две разные причины дали один текст %q — читающий не узнает, "+
			"чинить ведомость или рабочий каталог", absent.Error())
	}
	if !errors.Is(notree, errForeignIDPTreeNotAsked) {
		t.Errorf("неспрошенное дерево названо не своей причиной: %v", notree)
	}
	if !errors.Is(absent, errForeignIDPRevisionAbsent) {
		t.Errorf("отсутствующий объект полного дерева назван не своей причиной: %v", absent)
	}
	// Текст проверяется отдельно от классификации: человеку нужен РЕМОНТ. Слово
	// ремонта берётся из ОБЩЕГО источника (gitrevcause.go), а не выписывается
	// подстрокой здесь: подстрока по слову краснеет на переименовании и молчит
	// на переписанном мимо источника предложении.
	if !gitRevRemedyNamed(notree, gitRevRemedyWorkingDir) {
		t.Errorf("отказ не называет ремонта %q: %v", gitRevRemedyWorkingDir, notree)
	}
	if !gitRevRemedyNamed(absent, gitRevRemedyLedger) {
		t.Errorf("отказ не называет ремонта %q: %v", gitRevRemedyLedger, absent)
	}
}

// TestForeignIDPRevisionResolverDoesNotSendAShallowCloneToRewriteTheLedger —
// МЕЛКИЙ КЛОН РАЗВОРАЧИВАЛ РЕМОНТ.
//
// Код возврата 1 при молчаливом `--verify --quiet` даёт И «объекта нет вовсе»,
// И «объект существует, но не довезён мелким клоном»: разделителя из одного
// числа не существует. Прежняя редакция относила мелкий клон ко ВТОРОЙ ветви
// («дерево не спрошено, чинится глубиной клона»), а получала ПЕРВУЮ, чей ремонт
// — «перепишите число в ведомости». Читающему красное велели переписать
// ПРАВИЛЬНОЕ число, а текст про глубину клона при мелком клоне не печатался
// никогда.
//
// ОБА КОНЦА ПАРЫ СПРАШИВАЮТ ОДНО ДЕРЕВО — МЕЛКИЙ КЛОН, — И ОТЛИЧАЮТСЯ РОВНО
// РЕВИЗИЕЙ. Прежняя редакция ставила близнецом ПОЛНЫЙ origin: менялись два
// факта разом — и дерево, и (через него) довезённость, — и пара доказывала не
// то свойство. Разрешитель, отказывающий в мелком дереве ЧЕМУ УГОДНО, не
// спрашивая про ревизию, прошёл бы её целиком: близнец на полном дереве зелен,
// отрицательный конец красен, различение не проверено ничем. Ровно этот
// разрешитель и подан ниже инъекцией.
//
// Ревизию для однофактного близнеца даёт [foreignIDPScratchRepo]: `newer` —
// голова, её мелкий клон довозит; `older` — её родитель, его не довозит.
func TestForeignIDPRevisionResolverDoesNotSendAShallowCloneToRewriteTheLedger(t *testing.T) {
	t.Parallel()
	origin, older, newer := foreignIDPScratchRepo(t)

	// Предпосылка, а НЕ близнец: `older` — настоящий объект, и полное дерево
	// его знает. Без неё «не довезён» было бы неотличимо от «выдуман».
	if err := foreignIDPRevisionResolverAt(origin)(older); err != nil {
		t.Fatalf("полное дерево не знает своей ревизии: %v — предпосылка не выполнена", err)
	}

	shallow := filepath.Join(t.TempDir(), "shallow")
	if out, err := gitenv.Command(filepath.Dir(shallow), "clone", "-q", "--depth=1",
		"file://"+origin, shallow).CombinedOutput(); err != nil {
		t.Fatalf("мелкий клон не создан (%v): %s — условие пробы не создано, и её "+
			"молчание ничего не значило бы", err, out)
	}
	if !gitCloneIsShallow(shallow) {
		t.Fatal("клон не признан мелким — предпосылка пробы не выполнена")
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: то же мелкое дерево, довезённая ревизия — молчит.
	if err := foreignIDPRevisionResolverAt(shallow)(newer); err != nil {
		t.Fatalf("мелкий клон не знает СВОЕЙ головы %s: %v — близнец не зелёный, и "+
			"красное ниже достигалось бы отказом на чём угодно", newer, err)
	}

	// Отрицательный конец: то же дерево, ревизия НЕ довезённая.
	err := foreignIDPRevisionResolverAt(shallow)(older)
	if err == nil {
		t.Fatal("мелкий клон принял ревизию, которой у него нет — fail-open")
	}
	t.Logf("текст отказа мелкому клону: %v", err)
	if !errors.Is(err, errForeignIDPRevisionUndelivered) {
		t.Errorf("мелкий клон отнесён не к своей причине — читающий пойдёт "+
			"править ПРАВИЛЬНОЕ число: %v", err)
	}
	if errors.Is(err, errForeignIDPRevisionAbsent) {
		t.Errorf("мелкому клону велено чинить ведомость: %v", err)
	}
	if !gitRevRemedyNamed(err, gitRevRemedyCloneDepth) {
		t.Errorf("отказ не называет ремонта %q: %v", gitRevRemedyCloneDepth, err)
	}

	// ИНЪЕКЦИЯ, роняющая ТОЛЬКО проверяемое: разрешитель, который винит глубину
	// во всём подряд. Прежняя пара его не отличала; эта — обязана, и отличает
	// она его именно на близнеце.
	blanket := func(rev string) error {
		if gitCloneIsShallow(shallow) {
			return fmt.Errorf("%w: ревизия %s — %s",
				errForeignIDPRevisionUndelivered, rev, gitRevRemedyCloneDepth)
		}
		return nil
	}
	if blanket(older) == nil {
		t.Fatal("инъекция не воспроизводит свой предмет на отрицательном конце — " +
			"её молчание на близнеце ничего не значило бы")
	}
	if blanket(newer) == nil {
		t.Error("разрешитель, винящий глубину во всём подряд, прошёл близнеца — " +
			"пара меняет не один факт и различения не проверяет")
	}
}

// TestForeignIDPProvenanceGapDoesNotOutrankItsNestedCause — СУДЬЯ НЕ НАЗЫВАЕТ
// ПРИЧИНЫ ПОВЕРХ РАЗРЕШИТЕЛЯ.
//
// Причины разведены у разрешителя — и не были разведены у судьи: внешний текст
// пропуска утверждал «число снято обходом ЧУЖОЙ головы либо ревизия названа
// неверно», тогда как вложенная ошибка могла говорить прямо обратное — «дерево
// не спрошено». Противоречие создавалось в одной строке вывода.
func TestForeignIDPProvenanceGapDoesNotOutrankItsNestedCause(t *testing.T) {
	t.Parallel()
	resolve := func(string) error {
		return errors.New("дерево не спрошено о ревизии — система контроля версий не ответила")
	}
	gaps := ForeignIDPNameProvenanceGaps([]ForeignIDPNameLedgerEntry{{
		Area: "gateway/", Names: 2, Why: "w", Until: "u", Measured: "5b20df5c638",
	}}, resolve)
	if len(gaps) != 1 {
		t.Fatalf("пропусков %d, ожидался 1", len(gaps))
	}
	t.Logf("текст пропуска: %s", gaps[0])
	if !strings.Contains(gaps[0], "дерево не спрошено") {
		t.Errorf("текст судьи не донёс причины разрешителя: %q", gaps[0])
	}
	for _, claim := range []string{"ЧУЖОЙ головы", "названа неверно"} {
		if strings.Contains(gaps[0], claim) {
			t.Errorf("судья назвал причину %q поверх вложенной: %q", claim, gaps[0])
		}
	}
}
