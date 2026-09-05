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
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
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
	"services/geo/internal/apps/kacho/config",
	// Владелец модели. До собственной двери окна у него не было ВООБЩЕ — он не
	// задавал пообъектного вопроса на своих слушателях, полагаясь на край, — и
	// потому его каталог объявлений в перепись не входил. Дверь завела окно;
	// каталог входит вместе с ним.
	"services/iam/internal/apps/kacho/config",
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
//
// СЕГОДНЯ КАРТА ПУСТА, и это измерение, а не забытый перечень. Площадок этого
// вида в дереве не осталось: собственные фабрики звена сняты у всех — кеш
// вердиктов строит носитель (`pkg/servicehost`) по значению `Spec.CacheWindow`,
// которое приезжает из СВОЕЙ ручки каждого сервиса. Уходили они по одной —
// сперва geo и storage, последним compute, — и каждый раз запись, оставленная
// здесь, искала бы удалённый файл и роняла гейт на предпосылке, то есть на
// ВЕРНОМ дереве.
//
// Карта остаётся, потому что её предмет — не «эти трое», а вид площадки:
// вернётся фабрика, строящая кеш сама, — её место здесь. И пустота не делает
// проверку слепой: место, берущее окно неявно, ловит обход дерева
// (TestNoServiceTakesTheWindowImplicitly), который перечня не спрашивает.
var checkFactoryFiles = map[string]string{}

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

	// «Ни одной прочитанной площадки» перестало быть признаком сломанного
	// обхода: площадок этого вида в дереве нет вовсе (см. checkFactoryFiles).
	// Требовать «прочитай хотя бы одну» значило бы держать проверку, которую
	// можно починить только заведением новой фабрики — то есть ровно того
	// дефекта, ради устранения которого площадки и уходили.
	//
	// Обход дерева при этом никуда не делся: неявное окно ловит
	// TestNoServiceTakesTheWindowImplicitly, а обратная сторона — запись
	// Inherited без площадки — проверяется ниже и на пустой карте работает
	// строже всего: там ЛЮБАЯ запись оказывается без предмета.
	if len(checkFactoryFiles) > 0 && filesRead == 0 {
		t.Fatalf("предпосылка гейта нарушена: перечень площадок непуст (%d), "+
			"но не прочитано ни одной", len(checkFactoryFiles))
	}
	// «Ни одной унаследованной площадки» — законное состояние дерева, а не
	// сломанный обход: сегодня все площадки завели собственные ручки. Предпосылка
	// проверяется одна — что композиционные площадки ПРОЧИТАНЫ (выше); требовать
	// сверх этого «найди хотя бы одну» значило бы держать проверку, которую можно
	// починить только заведением нового дефекта.
	//
	// Двусторонность от этого не теряется: площадка без записи роняет гейт выше,
	// запись без площадки — ниже, и обе ветки исполняются, как только восьмой
	// сервис возьмёт окно умолчанием.

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
	for name, dir := range processes { //nolint:dupl // перепись процессов, см. verdictCacheProcesses
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
		filesRead, len(building), len(declared), revocationwindowgate.VerdictCacheCtorNames())

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
// Второе окно у процесса, который уже переписан
// ────────────────────────────────────────────────────────────────────────────

// TestEveryAuthzWindowKnobIsDeclared — единица переписи здесь ОКНО, а не процесс.
//
// Проверка выше спрашивает «строит ли этот процесс кеш вердиктов» и отвечает
// один раз на процесс. Поэтому процесс, единожды в перепись попавший, мог
// завести ВТОРОЕ окно любой величины под ручкой, которой никто не перечислял, —
// и красного не было бы ни от чего: ни от конструктора (он уже засчитан), ни от
// сверки значений (она ходит по закрытому словарю имён).
//
// Здесь вопрос задан без словаря имён — по ФОРМЕ ручки — и по всему дереву, а
// не по перечню каталогов конфигурации: перечень каталогов был бы третьим
// местом того же класса, где ручка, объявленная не там, невидима.
func TestEveryAuthzWindowKnobIsDeclared(t *testing.T) {
	root := repoRoot(t)
	files, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("предпосылка гейта нарушена: состав дерева не читается: %v", err)
	}

	declared := map[string]bool{}
	for _, k := range revocationwindowgate.KnobNames() {
		declared[k] = true
	}

	read := 0
	type hit struct{ knob, file string }
	var undeclared []hit
	seenDeclared := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatalf("прочитать %s: %v", f, rerr)
		}
		read++
		knobs, serr := revocationwindowgate.ScanWindowKnobNames(f, string(src))
		if serr != nil {
			continue // неразбираемый файл ловит собственный страж дерева
		}
		rel, _ := filepath.Rel(root, f)
		for _, k := range knobs {
			if declared[k] {
				seenDeclared[k] = true
				continue
			}
			undeclared = append(undeclared, hit{k, filepath.ToSlash(rel)})
		}
	}

	t.Logf("осмотрено: отслеживаемых .go прочитано=%d; ручек формы «размеряет окно вердикта»: "+
		"объявленных найдено=%d из %d, необъявленных=%d",
		read, len(seenDeclared), len(declared), len(undeclared))

	if read == 0 {
		t.Fatalf("предпосылка гейта нарушена: не прочитано ни одного файла")
	}
	if len(seenDeclared) == 0 {
		t.Fatalf("предпосылка гейта нарушена: прочитано %d файлов, но ни одна из %d объявленных "+
			"ручек не найдена формой. Либо ручки переименованы, либо форма перестала их описывать — "+
			"в обоих случаях эта проверка молчала бы и на настоящей находке", read, len(declared))
	}
	for _, h := range undeclared {
		t.Errorf("%s: ручка %q по форме размеряет окно кеша вердиктов авторизации, но политикой "+
			"НЕ объявлена.\n"+
			"  Процесс, единожды попавший в перепись конструкторов, добавляет такое окно молча: "+
			"«строит ли кеш» отвечено один раз, а величина сверяется по закрытому словарю имён, "+
			"в котором этой ручки нет.\n"+
			"  ЧТО ДЕЛАТЬ: внести окно в pkg/authz.RevocationPolicy (Windows) и имя ручки — в "+
			"knobNames пакета гейта, чтобы её значение сверялось с политикой; либо, если ручка "+
			"размеряет НЕ вердикт авторизации, переименовать её так, чтобы она этого не заявляла",
			h.file, h.knob)
	}
}

// TestKnobShapePredicateHasControlsBothWays — предикат формы измеряет свойство,
// а не собственную удобную половину.
//
// Предикат, проверенный в одну сторону, не измеряет ничего: он либо находит всё
// подряд, либо молчит на всём. Здесь обе половины утверждаются явно — каждая
// объявленная ручка обязана находиться, и каждая ручка соседних семей (сессия,
// повтор DPoP, кеш чужих фактов, сетевые сроки, размер кеша) обязана НЕ
// находиться.
func TestKnobShapePredicateHasControlsBothWays(t *testing.T) {
	declared := revocationwindowgate.KnobNames()
	if len(declared) == 0 {
		t.Fatal("предпосылка пробы нарушена: политика не объявляет ни одной ручки")
	}
	for _, k := range declared {
		if !revocationwindowgate.KnobSizesAuthzWindow(k) {
			t.Errorf("объявленная ручка %q формой НЕ распознаётся — предикат пропустил бы и её "+
				"необъявленного близнеца", k)
		}
	}

	// Отрицательный контроль. Каждая строка — из соседней семьи: их окна тоже
	// реальны, но ездят по другому пути отзыва (см. authz.RevocationPolicy,
	// «Что НЕ ездит по этому окну»), и путать их — та самая ошибка, которая
	// толкает уменьшать окно гранта вместо снятия учётных данных.
	for _, k := range []string{
		"KACHO_API_GATEWAY_SESSION_CACHE_TTL_SECONDS",
		"KACHO_API_GATEWAY_DPOP_REPLAY_TTL_SECONDS",
		"KACHO_IAM_INTROSPECTION_CACHE_TTL",
		"KACHO_VPC_PEER_PROJECT_CACHE_TTL",
		"http.client.timeout",
		"authz.cache-size",
	} {
		if revocationwindowgate.KnobSizesAuthzWindow(k) {
			t.Errorf("ручка %q признана размеряющей окно вердикта, хотя размеряет другое. "+
				"Предикат, красный на законной конструкции, отключают первым", k)
		}
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

// ────────────────────────────────────────────────────────────────────────────
// Окно у каждого, кто кеширует, — ВЫБРАННОЕ, а не унаследованное
// ────────────────────────────────────────────────────────────────────────────

// TestEveryVerdictCacheProcessDeclaresItsOwnKnob — процесс, держащий кеш
// вердиктов, обязан иметь СВОЮ ручку окна, а не брать умолчание платформы.
//
// Чем это отличается от TestEveryVerdictCacheServiceIsDeclared выше. Там вопрос
// «записан ли процесс в политике вообще», и ответ «да» даёт как Windows, так и
// Inherited. Разница между ними — не оформление: у площадки из Inherited окно
// отзыва принадлежит ПЛАТФОРМЕ. Оператор не может сузить его на конкретной
// посадке, а обсуждать нечего — в конфигурации сервиса искать нечего вовсе.
// Такое число невозможно ни отозвать, ни заметить при смене.
//
// Поэтому здесь требование строже и совпадает с каноном: у каждого, кто
// кеширует, окно объявлено ручкой. Три площадки (compute, geo, storage) до сих
// пор брали умолчание; значения при заведении ручек не менялись — изменилось то,
// что число стало выбранным.
//
// Гейт самоистекающий в обе стороны: он краснеет и когда процесс кеширует без
// своей ручки, и когда запись Windows потеряла процесс. Восьмой сервис, который
// решит взять умолчание, упрётся в красное и обязан будет либо завести ручку,
// либо записать исключение осознанно — то есть решением, а не умолчанием.
func TestEveryVerdictCacheProcessDeclaresItsOwnKnob(t *testing.T) {
	building, filesRead := verdictCacheProcesses(t)

	withOwnKnob := map[string]bool{}
	for key := range authz.RevocationPolicy.Windows {
		withOwnKnob[strings.SplitN(key, " ", 2)[0]] = true
	}

	t.Logf("осмотрено: файлов процессов прочитано=%d, процессов строит кеш вердиктов=%d, "+
		"процессов со своей ручкой=%d, площадок в Inherited=%d",
		filesRead, len(building), len(withOwnKnob), len(authz.RevocationPolicy.Inherited))

	if filesRead == 0 {
		t.Fatalf("предпосылка гейта нарушена: не прочитано ни одного файла процессов")
	}
	if len(building) == 0 {
		t.Fatalf("предпосылка гейта нарушена: прочитано %d файлов, но ни один процесс не строит "+
			"кеш вердиктов; конструктор переименован либо кеши переехали", filesRead)
	}

	var inherited []string
	for svc := range building {
		if !withOwnKnob[svc] {
			inherited = append(inherited, svc)
		}
	}
	sort.Strings(inherited)
	for _, svc := range inherited {
		t.Errorf("процесс держит кеш вердиктов, но своей ручки окна у него нет: «%s».\n"+
			"Окно отзыва этого процесса принадлежит платформе: оператор не может сузить его "+
			"на конкретной посадке, и в конфигурации сервиса о нём нет ни строки. Заведи ручку "+
			"KACHO_<SVC>_AUTHZ_CACHE_TTL и запись в pkg/authz.RevocationPolicy.Windows.", svc)
	}

	// Обратная сторона: запись Windows, под которой в дереве нет процесса,
	// строящего кеш вердиктов, — находка. Иначе перепись переживёт свой предмет.
	var stale []string
	for svc := range withOwnKnob {
		if !building[svc] {
			stale = append(stale, svc)
		}
	}
	sort.Strings(stale)
	for _, svc := range stale {
		t.Errorf("запись Windows без процесса: «%s» объявлен, но такого процесса, строящего кеш "+
			"вердиктов, в дереве нет. Кеш убран или процесс переименован — сними запись.", svc)
	}
}

// verdictCacheProcesses — процессы дерева, строящие кеш вердиктов, и число
// прочитанных файлов. Единица — ПРОЦЕСС, а не каталог под services/: край живёт
// вне services/, и обход, начинавшийся с одного этого каталога, не мог его
// увидеть в принципе.
func verdictCacheProcesses(t *testing.T) (map[string]bool, int) {
	t.Helper()
	root := repoRoot(t)

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

	filesRead := 0
	building := map[string]bool{}
	for name, dir := range processes {
		werr := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
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
			if found || declaresCacheWindowToHost(string(src)) {
				building[name] = true
			}
			return nil
		})
		if werr != nil {
			t.Fatalf("обход %s: %v", dir, werr)
		}
	}
	return building, filesRead
}

// declaresCacheWindowToHost — ВТОРАЯ форма владения кешем вердиктов: процесс не
// зовёт конструктор кеша сам, а отдаёт окно ДЕСКРИПТОРУ, и кеш строит носитель.
//
// Почему это засчитывается, а не читается как «кеша нет». Предмет гейта —
// «процесс, держащий кеш вердиктов, обязан иметь СВОЮ ручку окна отзыва», и он
// про ВЛАДЕНИЕ ЧИСЛОМ, а не про адрес вызова конструктора. У переведённого на
// носитель процесса окно по-прежнему его собственное: значение приезжает из его
// ручки в поле `CacheWindow`, а конструктор кеша просто переехал в одно место на
// все сервисы. Не засчитывать это значило бы объявить находкой ровно тот
// переезд, ради которого носитель и заводится, — и заодно потерять требование
// своей ручки для всех переведённых.
//
// Признаком взято ИМЯ ПОЛЯ в литерале: оно принадлежит закрытому набору осей
// дескриптора, поэтому совпасть случайно ему не с чем.
func declaresCacheWindowToHost(src string) bool {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		// Разбор здесь не обязан удаваться на любом файле дерева; молчание
		// безопасно, потому что первая форма (вызов конструктора) уже проверена.
		return false
	}
	declared := false
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "CacheWindow" {
				declared = true
				return false
			}
		}
		return true
	})
	return declared
}
