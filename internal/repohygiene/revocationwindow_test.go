// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocationwindow_test.go — гейт против окна отзыва, которое никто не выбирал.
//
// Положительный вердикт авторизации кешируется, отрицательный — никогда.
// Поэтому свежая ВЫДАЧА видна сразу, а ОТЗЫВ ждёт — ровно столько, сколько
// живёт запись, потому что иного пути её снять нет. Срок жизни записи и есть
// окно отзыва, и каждый сервис выбрал его сам.
//
// Это параметр безопасности, и до сих пор он таковым не объявлялся: шесть
// сервисов несли окно (пять по 5s, один по 2s), каждое — в своём комментарии в
// своём файле, и ни одно место не говорило, каким окну быть позволено и почему.
// Число, которого никто не выбирал, нельзя ни обсудить, ни отозвать, ни
// заметить, когда оно изменится.
//
// Гейт связывает дерево с ОДНИМ объявлением (`pkg/authz.RevocationPolicy`):
// каждая найденная площадка обязана быть в переписи политики, её значение —
// совпадать с тем, что реально написано в исходнике сервиса, и не превышать
// потолок. Смена умолчания без правки политики роняет проверку и называет оба
// числа — так изменение становится решением, а не дрейфом.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/tools/revocationwindowgate"
)

// revocationScanRoots — каталоги, в которых объявляются умолчания окон. Список
// узкий намеренно: гейт разбирает конфигурационные объявления, а не всё дерево,
// поэтому «сколько файлов прочитано» остаётся осмысленным числом.
var revocationScanRoots = []string{
	"services/vpc/internal/apps/kacho/config",
	"services/nlb/internal/apps/kacho/config",
	"services/registry/internal/apps/kacho/config",
	"services/compute/internal/config",
	"services/storage/internal/config",
	// Край. Он не лежит под services/, и именно поэтому его окно не попало в
	// перепись: все корни обхода начинались с services/, так что процесс, через
	// который проходит КАЖДЫЙ внешний запрос, не был прочитан ни одной из
	// проверок — ни разу, ни одним файлом.
	"gateway/internal/config",
}

// TestRevocationWindowIsDeclaredPolicy — окно отзыва объявлено в одном месте, и
// дерево ему соответствует.
func TestRevocationWindowIsDeclaredPolicy(t *testing.T) {
	root := repoRoot(t)
	rep := &revocationwindowgate.Report{}

	for _, rel := range revocationScanRoots {
		dir := filepath.Join(root, rel)
		service := serviceOfPath(rel)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("предпосылка гейта нарушена: каталог объявлений %s не читается: %v", rel, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			if err := revocationwindowgate.ScanFile(rep, service, filepath.Join(rel, e.Name()), string(src)); err != nil {
				t.Fatalf("%v", err)
			}
		}
	}

	// Перепись — до вердикта, и на каждом пути. «Ноль находок» обязано быть
	// отличимо от «ноль прочитанного».
	t.Logf("осмотрено: файлов разобрано=%d, площадок сопоставлено=%d, записей политики=%d",
		rep.FilesParsed, rep.SitesMatched, len(authz.RevocationPolicy.Windows))

	// Предпосылка: окно узнаётся по ОБЪЯВЛЕННОМУ имени ручки. Разобрали файлы,
	// но не нашли ни одной ручки — ручки переименованы либо конфиги переехали,
	// и молчать об этом нельзя.
	if rep.FilesParsed == 0 {
		t.Fatalf("предпосылка гейта нарушена: не разобрано ни одного файла объявлений")
	}
	if rep.SitesMatched == 0 {
		t.Fatalf("предпосылка гейта нарушена: разобрано %d файлов, но ни одна из известных ручек не найдена; "+
			"известные ручки: %v", rep.FilesParsed, revocationwindowgate.KnobNames())
	}

	ceiling := authz.RevocationPolicy.Ceiling
	declared := authz.RevocationPolicy.Windows

	seen := map[string]bool{}
	for _, s := range rep.Sites {
		key := s.Service + " " + s.Knob
		seen[key] = true

		want, ok := declared[key]
		if !ok {
			t.Errorf("окно не объявлено политикой: %s (%s:%d) держит %s, "+
				"но записи «%s» в pkg/authz.RevocationPolicy.Windows нет.\n"+
				"Окно отзыва — параметр безопасности: у него должен быть автор. "+
				"Внеси запись с обоснованием либо убери кеш.",
				key, s.File, s.Line, s.Window, key)
			continue
		}
		if s.Window != want {
			t.Errorf("окно разошлось с политикой: %s (%s:%d) держит %s, политика объявляет %s.\n"+
				"Смена окна отзыва — решение, а не правка умолчания: обнови "+
				"pkg/authz.RevocationPolicy вместе с исходником (или верни прежнее значение).",
				key, s.File, s.Line, s.Window, want)
		}
		if s.Window > ceiling {
			t.Errorf("окно превышает потолок политики: %s (%s:%d) держит %s при потолке %s.\n"+
				"Потолок — это обещание, которое платформа даёт про отзыв доступа.",
				key, s.File, s.Line, s.Window, ceiling)
		}
	}

	// Самоистечение: запись политики, которой больше нечего описывать, —
	// находка. Иначе перепись переживёт свой предмет и станет ложным
	// утверждением о дереве.
	var stale []string
	for key := range declared {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("запись политики без предмета: «%s» объявлена в "+
			"pkg/authz.RevocationPolicy.Windows, но такой площадки в дереве нет.\n"+
			"Ручку переименовали или кеш убрали — сними запись, иначе перепись "+
			"описывает мир, которого нет.", key)
	}
}

// TestCorelibDefaultIsTheDeclaredWindow — сервисы, передающие ttl≤0, берут
// окно из политики, а не из литерала, вкомпилированного в corelib.
//
// Три сервиса (compute, geo, storage) строят кеш как NewCache(0) и потому не
// имеют своего числа вовсе — их окно и есть значение по умолчанию. Пока это
// значение было безымянным литералом внутри NewCacheWithLimit, «окно отзыва
// этих трёх» не было записано нигде: ни в их конфиге, ни в политике.
func TestCorelibDefaultIsTheDeclaredWindow(t *testing.T) {
	c := authz.NewCache(0)
	if got := c.TTL(); got != authz.RevocationPolicy.Default {
		t.Errorf("ttl≤0 даёт %s, политика объявляет умолчанием %s.\n"+
			"Умолчание обязано читаться из объявленной политики: иначе окно "+
			"трёх сервисов, у которых своего числа нет, не записано нигде.",
			got, authz.RevocationPolicy.Default)
	}
	if authz.RevocationPolicy.Default > authz.RevocationPolicy.Ceiling {
		t.Errorf("умолчание %s превышает потолок %s",
			authz.RevocationPolicy.Default, authz.RevocationPolicy.Ceiling)
	}
}

// serviceOfPath — имя сервиса из пути вида services/<svc>/...
func serviceOfPath(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) >= 2 && parts[0] == "services" {
		return parts[1]
	}
	// Край живёт вне services/ и зовётся по имени своего бинаря
	// (gateway/cmd/api-gateway), а не по имени каталога: ключ переписи держит
	// то имя, которое оператор станет искать.
	if len(parts) >= 1 && parts[0] == "gateway" {
		return gatewayProcess
	}
	return rel
}

// gatewayProcess — имя края в переписи политики.
const gatewayProcess = "api-gateway"

// checkFactoryFiles — композиционные площадки, где строится кеш вердиктов
// пообъектной проверки. Сервис, у которого своего числа нет, строит его как
// NewCache(0) и берёт умолчание политики.
var checkFactoryFiles = map[string]string{
	"compute": "services/compute/internal/check/factory.go",
	"geo":     "services/geo/internal/check/factory.go",
	"storage": "services/storage/internal/check/factory.go",
}

// TestInheritedWindowsAreDeclared — сервис без своей ручки объявлен как таковой.
//
// Три сервиса берут окно из умолчания. Это не «отсутствие настройки», а само
// окно отзыва — и пока оно не записано, его смена не видна ниоткуда: в конфиге
// этих сервисов искать нечего. Проверка идёт в ОБЕ стороны, потому что
// одностороннее утверждение здесь зеленеет сильнее всего именно когда всё
// сломано: перепись без предмета так же плоха, как предмет без переписи.
func TestInheritedWindowsAreDeclared(t *testing.T) {
	root := repoRoot(t)

	filesRead := 0
	found := map[string]bool{}
	for service, rel := range checkFactoryFiles {
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("предпосылка гейта нарушена: композиционная площадка %s не читается: %v", rel, err)
		}
		sites, read, err := revocationwindowgate.ScanInherit(service, rel, string(src))
		if err != nil {
			t.Fatalf("%v", err)
		}
		filesRead += read
		for _, s := range sites {
			key := s.Service + " authz.cache-ttl"
			found[key] = true
			if !authz.RevocationPolicy.Inherited[key] {
				t.Errorf("окно унаследовано, но не объявлено: %s (%s:%d) строит кеш с ttl≤0 "+
					"и потому держит умолчание %s, но записи «%s» в "+
					"pkg/authz.RevocationPolicy.Inherited нет.\n"+
					"У этого сервиса своего числа нет — значит его окно не записано НИГДЕ, "+
					"пока не записано здесь.",
					key, s.File, s.Line, authz.RevocationPolicy.Default, key)
			}
		}
	}

	t.Logf("осмотрено: композиционных площадок прочитано=%d, унаследованных площадок найдено=%d, "+
		"записей Inherited=%d", filesRead, len(found), len(authz.RevocationPolicy.Inherited))

	if filesRead == 0 {
		t.Fatalf("предпосылка гейта нарушена: не прочитано ни одной композиционной площадки")
	}
	if len(found) == 0 {
		t.Fatalf("предпосылка гейта нарушена: прочитано %d площадок, но ни одна не строит кеш "+
			"с ttl≤0; конструктор переименован либо кеши переехали", filesRead)
	}

	// Обратная сторона: запись Inherited, под которой в дереве больше нет
	// площадки, — находка, а не безобидный остаток.
	var stale []string
	for key := range authz.RevocationPolicy.Inherited {
		if !found[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("запись Inherited без предмета: «%s» объявлена, но такой площадки "+
			"(NewCache с ttl≤0) в дереве нет. Сервис завёл свою ручку или кеш убран — "+
			"перенеси запись в Windows либо сними.", key)
	}
}

// TestEveryVerdictCacheServiceIsDeclared — сервис, строящий кеш вердиктов,
// объявлен политикой, каким бы именем он свою ручку ни назвал.
//
// Перепись по ИМЕНАМ РУЧЕК — закрытый словарь, и потому она по построению не
// видит сервис, приехавший с новым именем. Здесь вопрос задан без словаря —
// «строит ли этот сервис кеш вердиктов вообще», — поэтому сервис, который кеш
// СТРОИТ, ловится, не дожидаясь, пока кто-нибудь дополнит список.
//
// Ровно настолько, и не дальше. Прежняя редакция этого комментария обещала
// поймать седьмой сервис «в день появления»; обещание было неверно, и неверно
// в худшую сторону. Окно можно было получить, не построив кеш: конструктор
// интерсептора заводил его за молчащего вызывающего, и тогда в исходнике
// сервиса не оставалось ничего, что этот обход мог бы найти. Проверка читала
// такой файл, засчитывала его в «осмотрено» и объявляла чистым.
//
// Неназванный кеш ловит TestNoServiceTakesTheWindowImplicitly ниже; отказ в
// старте на него даёт сам конструктор.
func TestEveryVerdictCacheServiceIsDeclared(t *testing.T) {
	root := repoRoot(t)

	// Процессы, а не каталоги под services/. Край — такой же процесс с таким же
	// кешем вердиктов, но лежит вне services/, и обход, начинавшийся с одного
	// этого каталога, не мог его увидеть в принципе.
	processes := map[string]string{}
	servicesDir := filepath.Join(root, "services")
	svcEntries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("предпосылка гейта нарушена: каталог services/ не читается: %v", err)
	}
	for _, svc := range svcEntries {
		if svc.IsDir() {
			processes[svc.Name()] = filepath.Join(servicesDir, svc.Name())
		}
	}
	gatewayDir := filepath.Join(root, "gateway")
	if _, serr := os.Stat(gatewayDir); serr != nil {
		t.Fatalf("предпосылка гейта нарушена: дерево края gateway/ не читается: %v", serr)
	}
	processes[gatewayProcess] = gatewayDir

	declared := map[string]bool{}
	for key := range authz.RevocationPolicy.Windows {
		declared[strings.SplitN(key, " ", 2)[0]] = true
	}
	for key := range authz.RevocationPolicy.Inherited {
		declared[strings.SplitN(key, " ", 2)[0]] = true
	}

	filesRead := 0
	building := map[string]bool{}
	for name, dir := range processes {
		err := filepath.WalkDir(dir,
			func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err //nolint:wrapcheck // walk error propagates as-is
				}
				if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
					return nil
				}
				src, rerr := os.ReadFile(p)
				if rerr != nil {
					return rerr //nolint:wrapcheck // read error propagates as-is
				}
				filesRead++
				found, perr := revocationwindowgate.ScanConstructors(p, string(src))
				if perr != nil {
					return perr //nolint:wrapcheck // parse error propagates as-is
				}
				if found {
					building[name] = true
				}
				return nil
			})
		if err != nil {
			t.Fatalf("обход %s: %v", dir, err)
		}
	}

	t.Logf("осмотрено: файлов процессов прочитано=%d, процессов строит кеш вердиктов=%d, "+
		"процессов объявлено политикой=%d, распознаваемых локальных конструкторов=%v",
		filesRead, len(building), len(declared), revocationwindowgate.LocalVerdictCacheCtors())

	if filesRead == 0 {
		t.Fatalf("предпосылка гейта нарушена: не прочитано ни одного файла сервисов")
	}
	if len(building) == 0 {
		t.Fatalf("предпосылка гейта нарушена: прочитано %d файлов, но ни один сервис не строит "+
			"кеш вердиктов; конструктор переименован либо кеши переехали", filesRead)
	}

	var undeclared []string
	for svc := range building {
		if !declared[svc] {
			undeclared = append(undeclared, svc)
		}
	}
	sort.Strings(undeclared)
	for _, svc := range undeclared {
		t.Errorf("процесс строит кеш вердиктов, но политикой не объявлен: «%s».\n"+
			"Кешируется положительный вердикт ⇒ у процесса есть окно отзыва. "+
			"Внеси его в pkg/authz.RevocationPolicy (Windows — если у него своя ручка, "+
			"Inherited — если он берёт умолчание).", svc)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Окно, полученное МОЛЧА
// ────────────────────────────────────────────────────────────────────────────

// implicitScanRoots — деревья, в которых строится интерсептор. Оба, а не одно:
// composition root'ы живут в services/, но литерал опций в самом corelib
// подпадает под то же правило и молчаливого исключения не заслуживает.
// Край добавлен третьим по той же причине, по которой он был добавлен в
// revocationScanRoots: список корней, начинавшийся с services/, не мог увидеть
// процесс, который лежит не там. Сегодня край корневой интерсептор не строит,
// поэтому находок здесь от него не прибавится — но перечень корней не должен
// оставаться тем местом, где край снова окажется невидим.
var implicitScanRoots = []string{"services", "pkg", "gateway"}

// TestNoServiceTakesTheWindowImplicitly — ни одна площадка не получает окно
// отзыва, не назвав кеш.
//
// Три проверки выше меряют окно у тех, кто кеш СТРОИТ, и на этом строилось
// обещание пакета гейта: «спросим, строит ли сервис кеш вердиктов, — словаря не
// нужно, и седьмой сервис поймается в день появления». Для явного пути это
// верно. Для неявного было неверно: конструктор интерсептора принимал кеш
// полем, а незаполненное поле заводил сам — сервис получал полноценное окно, ни
// разу кеш не назвав, и в его исходнике не оставалось строки, по которой
// перепись могла бы его найти. Гейт при этом читал такой файл, засчитывал его в
// «осмотрено» и объявлял чистым.
//
// Проверено инъекцией: седьмой сервис по неявному пути проходил все три
// проверки зелёным, и число прочитанных файлов при этом росло на единицу.
//
// Исчерпывающий отказ живёт в конструкторе (`authz.NewInterceptor` отказывает в
// старте на неназванный кеш, какой бы формой опции ни собрали). Этот гейт нужен
// раньше и точнее: он называет файл и строку тогда, когда процесс ещё никто не
// поднимал.
func TestNoServiceTakesTheWindowImplicitly(t *testing.T) {
	root := repoRoot(t)

	filesRead := 0
	literalsSeen := 0
	var sites []revocationwindowgate.ImplicitSite

	for _, rel := range implicitScanRoots {
		base := filepath.Join(root, rel)
		if _, err := os.Stat(base); err != nil {
			t.Fatalf("предпосылка гейта нарушена: дерево %s не читается: %v", rel, err)
		}
		err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err //nolint:wrapcheck // walk error propagates as-is
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			src, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr //nolint:wrapcheck // read error propagates as-is
			}
			filesRead++
			relPath, rerr := filepath.Rel(root, p)
			if rerr != nil {
				relPath = p
			}
			rep, perr := revocationwindowgate.ScanImplicitSites(serviceOfPath(relPath), relPath, string(src))
			if perr != nil {
				return perr //nolint:wrapcheck // parse error propagates as-is
			}
			literalsSeen += rep.LiteralsSeen
			sites = append(sites, rep.Sites...)
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", rel, err)
		}
	}

	// Перепись — до вердикта и на каждом пути.
	t.Logf("осмотрено: файлов прочитано=%d, литералов InterceptorOptions=%d, площадок без имени кеша=%d",
		filesRead, literalsSeen, len(sites))

	if filesRead == 0 {
		t.Fatalf("предпосылка гейта нарушена: не прочитано ни одного файла")
	}
	// Предпосылка предиката: опции доезжают до конструктора ЛИТЕРАЛОМ. Ноль
	// литералов означает, что дерево перешло на иную форму сборки опций, —
	// тогда молчание этой проверки не значит «чисто», и говорить об этом
	// обязана она сама, а не следующий, кто на неё понадеется.
	if literalsSeen == 0 {
		t.Fatalf("предпосылка гейта нарушена: прочитано %d файлов, но ни одного литерала "+
			"authz.InterceptorOptions не встретилось; опции собирают иначе (переменная + присвоение полей) "+
			"либо тип переименован — предикат «литерал называет кеш» больше ничего не проверяет",
			filesRead)
	}

	sort.Slice(sites, func(i, j int) bool {
		if sites[i].File != sites[j].File {
			return sites[i].File < sites[j].File
		}
		return sites[i].Line < sites[j].Line
	})
	for _, s := range sites {
		t.Errorf("окно отзыва получено молча: %s:%d (сервис «%s») строит интерсептор, "+
			"не назвав кеш вердиктов.\n"+
			"Кешируется положительный вердикт ⇒ у площадки ЕСТЬ окно отзыва, но оно не попадает "+
			"ни в перепись pkg/authz.RevocationPolicy, ни под её потолок.\n"+
			"Назови кеш: своё окно — NewCache(ttl) плюс запись в Windows; умолчание политики — "+
			"NewCache(0) плюс запись в Inherited.",
			s.File, s.Line, s.Service)
	}
}

// TestNoCallSiteTakesTheWindowUnprovably — ни один вызов конструктора не
// получает окно отзыва так, чтобы имя кеша нельзя было предъявить.
//
// Зачем вторая проверка рядом с предыдущей. Предыдущая берёт предметом ЛИТЕРАЛ
// `InterceptorOptions`, и берёт его по верному доводу: дерево пользуется двумя
// формами, и предикат по аргументу вызова пропускал бы ту, где литерал вынесен
// в переменную. Неверным было следствие — «формы без литерала в дереве нет,
// значит литерала достаточно». Предпосылка эта у предыдущей проверки записана
// (`literalsSeen == 0` — нарушенная предпосылка), но записана СУММАРНО по
// дереву: пока хоть одна площадка собирает опции литералом, а их восемь, она не
// сработает никогда. Предпосылка объявлена про каждую площадку, а проверяется
// про дерево целиком.
//
// Проверено инъекцией на настоящем дереве: седьмой сервис, строящий интерсептор
// из `var o authz.InterceptorOptions` с присвоением полей, проходил ВСЕ четыре
// проверки зелёным, и число прочитанных файлов при этом росло на единицу — файл
// был прочитан и объявлен чистым.
//
// Здесь предмет — ВЫЗОВ. Вопрос к каждому: доказуемо ли, что кеш назван? Форма,
// в которой это доказать нечем, — находка, потому что «не смог посмотреть» не
// есть «чисто».
func TestNoCallSiteTakesTheWindowUnprovably(t *testing.T) {
	root := repoRoot(t)

	filesRead := 0
	callsSeen := 0
	var sites []revocationwindowgate.ImplicitSite

	for _, rel := range implicitScanRoots {
		base := filepath.Join(root, rel)
		if _, err := os.Stat(base); err != nil {
			t.Fatalf("предпосылка гейта нарушена: дерево %s не читается: %v", rel, err)
		}
		err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err //nolint:wrapcheck // walk error propagates as-is
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			src, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr //nolint:wrapcheck // read error propagates as-is
			}
			filesRead++
			relPath, rerr := filepath.Rel(root, p)
			if rerr != nil {
				relPath = p
			}
			rep, perr := revocationwindowgate.ScanInterceptorCalls(serviceOfPath(relPath), relPath, string(src))
			if perr != nil {
				return perr //nolint:wrapcheck // parse error propagates as-is
			}
			callsSeen += rep.CallsSeen
			sites = append(sites, rep.Sites...)
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", rel, err)
		}
	}

	// Перепись — до вердикта и на каждом пути.
	t.Logf("осмотрено: файлов прочитано=%d, вызовов authz.NewInterceptor=%d, площадок без доказуемого имени кеша=%d",
		filesRead, callsSeen, len(sites))

	if filesRead == 0 {
		t.Fatalf("предпосылка гейта нарушена: не прочитано ни одного файла")
	}
	// Предпосылка ЭТОГО предиката — про сам предмет, а не про одну из его форм:
	// ноль вызовов конструктора означает, что интерсептор собирают иначе либо
	// конструктор переименован, и тогда молчание проверки не значит «чисто».
	if callsSeen == 0 {
		t.Fatalf("предпосылка гейта нарушена: прочитано %d файлов, но ни одного вызова "+
			"authz.NewInterceptor не встретилось; конструктор переименован либо интерсептор "+
			"собирают иначе — предикат «вызов доказуемо называет кеш» больше ничего не проверяет",
			filesRead)
	}

	sort.Slice(sites, func(i, j int) bool {
		if sites[i].File != sites[j].File {
			return sites[i].File < sites[j].File
		}
		return sites[i].Line < sites[j].Line
	})
	for _, s := range sites {
		t.Errorf("окно отзыва получено недоказуемо: %s:%d (сервис «%s») строит интерсептор, "+
			"и назван ли кеш вердиктов — по этому файлу установить нельзя.\n"+
			"Кешируется положительный вердикт ⇒ у площадки ЕСТЬ окно отзыва, но ни перепись "+
			"литералов, ни перепись pkg/authz.RevocationPolicy его не видят.\n"+
			"Назови кеш там, где видно: литералом опций (Cache: …) либо присвоением "+
			"(opts.Cache = authz.NewCache(…)) в той же функции.",
			s.File, s.Line, s.Service)
	}
}
