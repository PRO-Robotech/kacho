// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// providerSurfaceLedger — ВЕДОМОСТЬ: места прод-кода, которым сегодня разрешено
// говорить с внешним поставщиком удостоверений, и то, о чём каждое с ним
// говорит.
//
// Ведомость — НЕ послабление и не список прощённых. Послабление накрывает
// нарушение; здесь накрывать нечего: поставщик жив по решению, и разговор с ним
// законен, пока фаза Ф4 (задача #900) не сняла его целиком. Предмет ведомости —
// РОСТ поверхности, а не её наличие.
//
// Ведомость обязана СОКРАЩАТЬСЯ. Запись, которой больше нечего называть, —
// находка (ProviderFindingStale), поэтому снятие кода заставляет снять и запись:
// одно без другого не зеленеет.
//
// У каждой записи стоит `Until` — факт о дереве, при котором её снимают. Это не
// украшение: без него запись бессрочна, и снять её будет некому.
// providerSurfaceLedger — ведомость разрешённых разговоров с внешним
// поставщиком удостоверений.
//
// ОНА ПУСТА, И ЭТО ЦЕЛЬ, А НЕ ПОЛОМКА. Три записи края — страж старта полосы
// интроспекции, её диагностика и снятие сессии входа на выходе человека —
// ИСТЕКЛИ вместе со своим предметом: ни один файл прод-кода края больше не
// говорит с поставщиком по его API, потому что самих полос нет. Ведомость
// обязана СОКРАЩАТЬСЯ по мере снятия, и сокращение дошло до нуля.
//
// Что при этом становится с гейтом. Он перестаёт быть ведомостью и становится
// ЗАПРЕТОМ: любой разговор с поставщиком в прод-коде — находка вида
// ProviderFindingUnledgered, без исключений. Молчание гейта на пустой ведомости
// не вакуумно — способность упасть доказывается инъекцией на синтетике
// (providersurface_injection_test.go), а объём осмотренного печатается
// переписью.
var providerSurfaceLedger = []ProviderLedgerEntry{}

// providerSurfaceExemptions — послабления гейта.
//
// Послабление — НЕ ведомость. Ведомость называет законный разговор с
// поставщиком; послабление снимает файл с рассмотрения целиком, и потому у него
// обязан быть предикат снятия и проба, что предмет ещё есть.
var providerSurfaceExemptions = []struct {
	// Prefix — путь либо его начало.
	Prefix string
	// Why — почему исключено.
	Why string
	// Until — при каком факте о дереве запись обязана быть снята.
	Until string
}{
	{
		Prefix: "internal/repohygiene/providersurface.go",
		Why: "здесь живёт САМ СЛОВАРЬ путей поставщика: гейт разбирает строковые " +
			"литералы, а словарь и есть перечень строковых литералов. Без послабления " +
			"гейт находит собственное объявление — то есть краснеет на исправном дереве " +
			"и снимается первым же обходом. Прятать словарь склейкой по частям нельзя: " +
			"проверка, спрятавшаяся от себя самой, перестаёт быть читаемой",
		Until: "словарь перестал быть перечнем строковых литералов — например, " +
			"переехал в отдельные данные, которых разбор исполняемой части не читает",
	},
}

func exemptFromProviderSurface(path string) bool {
	for _, e := range providerSurfaceExemptions {
		if strings.HasPrefix(path, e.Prefix) {
			return true
		}
	}
	return false
}

// providerSurfaceSources — непроверочное дерево Go, спрошенное У ИНДЕКСА.
//
// Обход диска не знает правил игнорирования и судит чужой рабочий каталог —
// произведённые файлы, чужие копии, остатки прогонов.
func providerSurfaceSources(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}
	sources := map[string]string{}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || skipPath(rel) || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		sources[rel] = string(b)
	}
	return sources
}

// providerSurfaceDenominator — ЗНАМЕНАТЕЛЬ обхода, снятый вторым выражением.
//
// Разбор пар «первое выражение против второго» — в шапке
// providerwalkverdict.go. Здесь только добыча:
//
//   - состав дерева спрашивается у КОММИТА (`git ls-tree -r HEAD`), тогда как
//     сам обход берёт его у ИНДЕКСА (`git ls-files`). Одинаково сломаться эти
//     два вопроса не могут: между ними лежит вся незакоммиченная работа;
//   - литералы считаются ЛЕКСИЧЕСКИМ проходом, без синтаксического дерева;
//   - словарь проверяется положительным контролем на синтетическом входе.
func providerSurfaceDenominator(
	t *testing.T, sources map[string]string, exempt func(path string) bool,
) ProviderWalkDenominator {
	t.Helper()
	root := repoRoot(t)

	out, err := gitenv.Command(root, "ls-tree", "-r", "-z", "--name-only", "HEAD").Output()
	if err != nil {
		t.Fatalf("git ls-tree HEAD: %v — знаменатель обхода не установлен, и «ноль "+
			"находок» здесь означало бы «ноль прочитанного»", err)
	}
	denom := ProviderWalkDenominator{DictionaryPaths: len(ProviderSurfaces)}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		denom.CommitGoFiles++
		if skipPath(rel) {
			denom.SkippedGoFiles++
		}
	}

	lexFiles, lexLiterals, lexErr := MeasureLiteralsLexically(sources, exempt)
	if lexErr != nil {
		t.Fatalf("лексический проход: %v", lexErr)
	}
	denom.LexicalLiterals = lexLiterals

	found, missed, reachErr := MeasureRecogniserReach(ProviderSurfaces)
	if reachErr != nil {
		t.Fatalf("положительный контроль распознавателя: %v", reachErr)
	}
	denom.RecogniserReach = found

	t.Logf("ЗНАМЕНАТЕЛЬ ОБХОДА (второе выражение): непроверочных файлов Go в КОММИТЕ %d "+
		"(из них отброшено правилом игнорирования %d); строковых литералов по "+
		"ЛЕКСИЧЕСКОМУ проходу %d в %d файлах; путей словаря %d, из них распознаватель "+
		"находит на синтетическом входе %d (не найдены: %v)",
		denom.CommitGoFiles, denom.SkippedGoFiles, denom.LexicalLiterals, lexFiles,
		denom.DictionaryPaths, denom.RecogniserReach, missed)
	return denom
}

// TestProviderSurfaceIsBoundedByTheLedger — поверхность к внешнему поставщику
// удостоверений ограничена ведомостью, и это утверждение О ДЕРЕВЕ.
//
// Разбор класса и граница предиката — в шапке providersurface.go. Здесь только
// обход дерева и вердикт.
func TestProviderSurfaceIsBoundedByTheLedger(t *testing.T) {
	t.Parallel()
	sources := providerSurfaceSources(t)

	findings, census, err := FindProviderSurface(sources, providerSurfaceLedger, exemptFromProviderSurface)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	t.Logf("ОБХОД (первое выражение): осмотрено непроверочных файлов Go: %d "+
		"(снято послаблением: %d); строковых литералов: %d; мест разговора с "+
		"поставщиком: %d в %d файлах; записей ведомости: %d (объявлений поверхностей: "+
		"%d); файлов, называющих поставщика ТОЛЬКО в прозе: %d",
		census.Files, census.Exempt, census.Literals, census.Reaches, census.Carriers,
		census.LedgerEntries, census.LedgerSurfaces, census.ProseMentions)

	denom := providerSurfaceDenominator(t, sources, exemptFromProviderSurface)
	verdict := JudgeProviderWalk(census, denom)
	t.Logf("ИСХОД ОБХОДА: %s", verdict.Outcome)

	// Исходов три, и третий — не зелёный. «Проверено N, находок 0» и «посмотреть
	// не смог» обязаны различаться: пустой результат — законный вердикт только
	// при доказанном знаменателе обхода.
	if verdict.Outcome == ProviderOutcomeBlind {
		for _, reason := range verdict.Blind {
			t.Errorf("обход не состоялся: %s", reason)
		}
		t.Fatalf("исход %q — у гейта НЕТ вердикта о поверхности, и его молчание ничего "+
			"не утверждает. Причин названо: %d. Пока хоть одна из них жива, «находок "+
			"ноль» неотличимо от «искать было нечем»", verdict.Outcome, len(verdict.Blind))
	}
	if verdict.Outcome == ProviderOutcomeNoSurface {
		// ЦЕЛЬ фазы Ф4 (задача #900), а не поломка: проба, падающая на достижении
		// собственной цели, подталкивает держать запись ведомости ради зелёного.
		// Исход назван отдельно именно потому, что знаменатель обхода доказан выше:
		// иначе он был бы неотличим от ослепшего гейта.
		t.Logf("поверхности нет: мест разговора с поставщиком ноль и ведомость пуста "+
			"при знаменателе обхода — файлов Go %d, строковых литералов %d, путей "+
			"словаря %d (все доказанно находимы). Это исход, к которому ведёт задача #900",
			denom.CommitGoFiles, denom.LexicalLiterals, denom.DictionaryPaths)
	}

	for _, f := range findings {
		switch f.Kind {
		case ProviderFindingUnledgered:
			t.Errorf("%s:%d: разговор с внешним поставщиком по %q (%s) — %s.\n"+
				"Платформа переезжает на СВОЮ чеканку (эпик #896); поверхность к поставщику "+
				"обязана сокращаться, а не расти. Новое место либо не нужно — тогда его нет, "+
				"либо нужно — тогда оно названо в providerSurfaceLedger вместе с предикатом "+
				"своего снятия",
				f.File, f.Line, f.Surface, f.Detail, f.Kind)
		case ProviderFindingUndeclared:
			t.Errorf("%s:%d: %s — за этим местом объявлены другие поверхности, а оно просит "+
				"%q (%s).\nИменно так поверхность и растёт: «ещё один вызов туда же». "+
				"Объявите поверхность за местом либо не заводите вызов",
				f.File, f.Line, f.Kind, f.Surface, f.Detail)
		case ProviderFindingStale:
			t.Errorf("%s: %s — %s.\nВедомость обязана сокращаться вместе с кодом: запись, "+
				"которой нечего называть, молча разрешит следующий разговор в этом файле",
				f.File, f.Kind, f.Detail)
		default:
			t.Errorf("%s:%d: неизвестный вид находки %q", f.File, f.Line, f.Kind)
		}
	}
}

// TestProviderSurfaceExemptionsStillHaveASubject — послабление живёт, пока у него
// есть предмет.
//
// «Предмет» здесь — НЕ «под префиксом лежит файл». Такой предикат зеленел бы на
// послаблении, которому нечего исключать: файл существует, разговоров в нём нет,
// а запись стоит и молча накроет следующую слепую зону.
//
// Предмет — «без этой записи гейт нашёл бы ЗДЕСЬ находку». Поэтому разбор
// прогоняется БЕЗ послаблений, и от каждой записи требуется хотя бы одна
// находка под её префиксом.
func TestProviderSurfaceExemptionsStillHaveASubject(t *testing.T) {
	t.Parallel()
	sources := providerSurfaceSources(t)

	bare, census, err := FindProviderSurface(sources, providerSurfaceLedger, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	t.Logf("разбор без послаблений: файлов %d, находок %d, послаблений объявлено %d",
		census.Files, len(bare), len(providerSurfaceExemptions))

	if len(providerSurfaceExemptions) == 0 {
		// Пустой перечень — цель, а не поломка.
		t.Log("послаблений ноль — исключать нечего, и это исход, к которому проба ведёт")
		return
	}
	for _, e := range providerSurfaceExemptions {
		covered := 0
		for _, f := range bare {
			if strings.HasPrefix(f.File, e.Prefix) {
				covered++
			}
		}
		if covered == 0 {
			t.Errorf("послабление %q не исключает НИ ОДНОЙ находки — предмета у него нет, "+
				"и оно обязано быть снято. Оставленное, оно молча накроет следующую слепую "+
				"зону.\nПричина записи: %s\nПредикат снятия: %s",
				e.Prefix, e.Why, e.Until)
			continue
		}
		t.Logf("послабление %q: находок под ним %d", e.Prefix, covered)
	}
}

// providerSurfaceWitness — НЕЗАВИСИМЫЙ свидетель: пути API внешнего поставщика,
// выписанные здесь литералом, а НЕ взятые из `ProviderSurfaces`.
//
// Второе объявление одного предмета обычно находка, и здесь оно намеренное —
// потому что предмет у положительного контроля другой. Контроль, берущий
// ожидание из проверяемого словаря, тавтологичен: переименуй запись словаря, и
// контроль подложит носитель с НОВЫМ путём, найдёт его и объявит распознаватель
// здоровым — ровно на той форме слепоты, которая в дереве и случается (путь у
// поставщика поехал, словарь не обновили).
//
// Поэтому свидетель заморожен. Расхождение свидетеля со словарём читается так:
//
//   - путь свидетеля не находится настоящим обходом ⇒ НАХОДКА: этим путём
//     поверхность больше не обнаруживается;
//   - путь словаря не назван свидетелем ⇒ не находка, а рост: словарь вправе
//     пополняться, и контроль о новых путях просто печатает число.
//
// ПРЕДИКАТ СНЯТИЯ свидетеля: он уходит вместе с самим гейтом ведомости — то
// есть когда поставщика в дереве не останется ВОВСЕ и снимать станет нечего.
// Пока гейт жив, свидетель обязан быть непуст: пустой свидетель — это и есть
// слепота, только объявленная.
var providerSurfaceWitness = []string{
	"/admin/clients",
	"/admin/oauth2/introspect",
	"/admin/oauth2/auth/sessions/login",
	"/admin/trust/grants/jwt-bearer/issuers",
	"/oauth2/token",
}

// TestProviderSurfaceGate_IsStillAbleToFindOnTheRealWalk — ПОЛОЖИТЕЛЬНЫЙ
// КОНТРОЛЬ гейта ведомости, и он переживает опустевшую ведомость.
//
// # Что он закрывает
//
// Молчание гейта выше читается как «поверхности к поставщику нет». Оно значит
// это ровно до тех пор, пока распознаватель СПОСОБЕН её найти. Предпосылка гейта
// требует непустого ОБХОДА — прочитанных файлов и литералов, — а не работающего
// распознавания, и разница между ними ИЗМЕРЕНА, а не предположена: на дереве из
// одного файла с настоящим путём поставщика гейт даёт одну находку; опустошив
// словарь `ProviderSurfaces`, получаем ноль находок при тех же «файлов 1,
// литералов 1» — зелёный вердикт с выполненной предпосылкой.
//
// Пока ведомость НЕПУСТА, слепоту ловит она сама: ослепший распознаватель
// перестаёт видеть названные ею места, и они становятся находками
// ProviderFindingStale. Но пустая ведомость — это ЦЕЛЬ, и в тот день этот
// сторож исчезнет вместе с записями. Останется пара «ноль находок / непустой
// обход», в которой «поверхность пуста» и «гейт ослеп» неразличимы. Здесь
// заводится третья величина, которая их различает.
//
// # Почему контроль идёт по НАСТОЯЩЕМУ обходу
//
// Синтетические пробы соседнего файла доказывают, что РАЗБОР способен упасть.
// Они не доказывают, что способен упасть ОБХОД: между ними стоят состав дерева,
// перечень послаблений и порог исполняемой части, и ослепнуть можно на каждом.
// Поэтому здесь тот же источник, те же послабления и та же ведомость, что у
// гейта, — и в них подкладывается один синтетический носитель на каждый путь
// СВИДЕТЕЛЯ.
func TestProviderSurfaceGate_IsStillAbleToFindOnTheRealWalk(t *testing.T) {
	t.Parallel()
	sources := providerSurfaceSources(t)
	if len(sources) == 0 {
		t.Fatal("прочитано ноль файлов — контролировать нечего")
	}
	if len(providerSurfaceWitness) == 0 {
		t.Fatal("свидетель пуст — контроль объявлен и не контролирует ничего")
	}

	// Основание берётся ТЕМ ЖЕ обходом, а не длиной карты источников: часть
	// файлов снимается послаблением и до разбора не доезжает, поэтому «сколько
	// дали» и «сколько прочитано» — разные величины, и сверять подложенный
	// носитель надо со второй.
	baseFindings, baseCensus, baseErr := FindProviderSurface(
		sources, providerSurfaceLedger, exemptFromProviderSurface)
	if baseErr != nil {
		t.Fatalf("разбор основания: %v", baseErr)
	}
	if baseCensus.Files == 0 {
		t.Fatal("обход прочитал ноль файлов — контролировать нечего")
	}

	// Носитель кладётся по пути, которого ведомость НЕ называет и послабление НЕ
	// исключает: иначе находку погасит законное разрешение, а не слепота.
	const probe = "internal/repohygiene/zz_positive_control_probe_that_is_not_in_the_tree.go"
	for _, e := range providerSurfaceLedger {
		if e.File == probe {
			t.Fatalf("путь контроля %s назван ведомостью — находку погасит разрешение", probe)
		}
	}
	if exemptFromProviderSurface(probe) {
		t.Fatalf("путь контроля %s снят послаблением — до разбора он не доедет", probe)
	}
	for _, f := range baseFindings {
		if f.File == probe {
			t.Fatalf("обход нашёл носитель контроля ДО того, как его подложили: %+v", f)
		}
	}

	found := 0
	for _, path := range providerSurfaceWitness {
		injected := make(map[string]string, len(sources)+1)
		for k, v := range sources {
			injected[k] = v
		}
		injected[probe] = "package repohygiene\n\nconst probeSurface = \"" + path + "\"\n"

		findings, census, err := FindProviderSurface(
			injected, providerSurfaceLedger, exemptFromProviderSurface)
		if err != nil {
			t.Fatalf("разбор при контроле пути %q: %v", path, err)
		}
		if census.Files != baseCensus.Files+1 {
			t.Fatalf("обход прочитал %d файлов, ожидалось %d — носитель контроля до разбора "+
				"не доехал", census.Files, baseCensus.Files+1)
		}
		if !injectFinding(findings, probe, ProviderFindingUnledgered, path) {
			t.Errorf("путь поставщика %q НЕ НАЙДЕН настоящим обходом.\n"+
				"Значит этим путём поверхность больше не обнаруживается, и «находок ноль» "+
				"у гейта ведомости про него не утверждает НИЧЕГО. Слепая зона хуже снятого "+
				"гейта: снятый виден в диффе, слепой — зелен.\n"+
				"Исход: либо вернуть путь в ProviderSurfaces, либо снять его у свидетеля — "+
				"и тогда сказать, чем теперь обнаруживается эта поверхность",
				path)
			continue
		}
		found++
	}

	// Рост словаря находкой НЕ является: он вправе пополняться, и свидетель за
	// ним не обязан поспевать. Число печатается, чтобы расхождение было видно
	// глазу, а не подразумевалось.
	beyond := 0
	for _, sfc := range ProviderSurfaces {
		known := false
		for _, w := range providerSurfaceWitness {
			if w == sfc.Path {
				known = true
				break
			}
		}
		if !known {
			beyond++
			t.Logf("путь словаря %q свидетелем не назван — это рост словаря, не находка", sfc.Path)
		}
	}

	t.Logf("перепись положительного контроля: путей у свидетеля %d, обнаружено настоящим "+
		"обходом %d; путей в словаре %d (сверх свидетеля %d); файлов дерева в обходе %d; "+
		"записей ведомости %d",
		len(providerSurfaceWitness), found, len(ProviderSurfaces), beyond,
		baseCensus.Files, len(providerSurfaceLedger))
}
