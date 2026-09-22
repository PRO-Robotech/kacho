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
		// 102, а не 99: число 99 снято обходом bec320cf47d — головы ДО того, как
		// в волну свели полосу носителя сессии края. Сведение принесло ровно три
		// имени, и все три в этой области: метод дублёра `LookupOrUpsertFromKratos`,
		// реализующий порт `KratosSubjectLookuper`, и два параметра `kratosURL` у
		// оконных помощников проб полос. Снять их порознь нельзя: метод дублёра
		// носит имя порта, а не своё, и переименование увело бы производство на
		// другую ветвь резолва МОЛЧА — обе пробы окна перехода остались бы
		// зелёными, перестав наблюдать заведение зеркала, ради которого заведены.
		Names:    102,
		Measured: "5b20df5c638",
		Why: "край — единственное место, где платформа РАЗГОВАРИВАЕТ с поставщиком " +
			"по его протоколу: сессия входа, интроспекция, снятие сессии. Имена здесь " +
			"расходятся на две половины: координаты разговора (их снимут вместе с " +
			"полосой) и фикстуры проб, носящие название без нужды",
		Until: "полоса развода ручки посадки края закончена и в `gateway/**` не " +
			"осталось ни одного объявленного имени этой оси",
	},
	{
		Area:     "deploy/",
		Names:    3,
		Measured: "5b20df5c638",
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

// foreignIDPRevisionResolverAt — спрашивает систему контроля версий, знает ли
// дерево по этому пути названный объект-коммит.
//
// Форма без разрешимости не судит ничего: выдуманный набор шестнадцатеричных
// знаков и ревизия ЧУЖОГО дерева выглядят как ревизия и ею не являются. Ровно
// это и есть та ложь, которой красное полосы началось, — число с чужой головы,
// объявленное снятым здесь.
//
// ПРИЧИН ОТКАЗА ТРИ, И ОНИ НЕ ОДНО И ТО ЖЕ. Направление у всех одно —
// fail-closed, — но чинятся они по-разному, и одна формулировка на всех
// превращает «дерево не спросили» в «число не подтвердилось»:
//
//	объекта нет      — ведомость называет то, чего в дереве не существует;
//	                   чинится записью в ведомости. Признак узкий: код возврата
//	                   1 при молчаливом `--verify --quiet`;
//	дерево не спрошено — каталог не репозиторий, клон мелкий, инструмента нет
//	                   вовсе; чинится рабочим каталогом или глубиной клона.
//	                   Сюда же намеренно отнесён отсутствующий инструмент:
//	                   создать это состояние пробой нечем, а исход тот же —
//	                   мы НЕ СПРОСИЛИ, и «не подтвердилось» было бы ложью.
func foreignIDPRevisionResolverAt(root string) ForeignIDPNameRevisionResolver {
	return func(rev string) error {
		err := gitenv.Command(root, "rev-parse", "--verify", "--quiet", rev+"^{commit}").Run()
		if err == nil {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return fmt.Errorf("дерево не знает объекта-коммита %s", rev)
		}
		return fmt.Errorf("дерево не спрошено о ревизии %s — система контроля версий "+
			"не ответила (%w); это НЕ «число не подтвердилось», а «мы не спросили», "+
			"и чинится рабочим каталогом либо глубиной клона", rev, err)
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
	if !strings.Contains(err.Error(), absent) {
		t.Errorf("отказ не называет ревизии: %v", err)
	}
	if !strings.Contains(err.Error(), "не знает") {
		t.Errorf("отказ не называет ПРИЧИНЫ «объекта нет»: %v", err)
	}
}

// TestForeignIDPRevisionResolverSeparatesItsCauses — ПРИЧИН ТРИ, и одна на всех
// формулировка превращает «дерево не спросили» в «числа не подтвердились».
//
// Направление у обеих ветвей одно — fail-closed, — но чинятся они по-разному:
// отсутствующий объект правится записью в ведомости, неспрошенное дерево —
// глубиной клона или рабочим каталогом. Третья причина (инструмента нет вовсе)
// неотличима здесь от второй намеренно: создать её пробой нечем, и она сказана
// той же формулировкой «дерево не спрошено».
func TestForeignIDPRevisionResolverSeparatesItsCauses(t *testing.T) {
	t.Parallel()
	head := foreignIDPHeadRevision(t)

	absent := foreignIDPRevisionResolverAt(repoRoot(t))("0123456789a")
	notree := foreignIDPRevisionResolverAt(t.TempDir())(head)
	if absent == nil || notree == nil {
		t.Fatalf("обе ветви обязаны отказывать: объект %v, дерево %v", absent, notree)
	}
	t.Logf("объекта нет:      %v", absent)
	t.Logf("дерево не спрошено: %v", notree)
	if absent.Error() == notree.Error() {
		t.Fatalf("две разные причины дали один текст %q — читающий не узнает, "+
			"чинить ведомость или рабочий каталог", absent.Error())
	}
	if !strings.Contains(notree.Error(), "не спрошено") {
		t.Errorf("неспрошенное дерево названо не своей причиной: %v", notree)
	}
}
