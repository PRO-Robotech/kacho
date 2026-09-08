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
	"fmt"
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
	"services/iam/internal/apps/kaname/config",
	// Край. Он не лежит под services/, и именно поэтому его окно не попало в
	// перепись: все корни обхода начинались с services/, так что процесс, через
	// который проходит КАЖДЫЙ внешний запрос, не был прочитан ни одной из
	// проверок — ни разу, ни одним файлом.
	"gateway/internal/config",
}

// TestRevocationWindowIsDeclaredPolicy — окно отзыва объявлено в одном месте, и
// дерево ему соответствует.
func TestRevocationWindowIsDeclaredPolicy(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// Здесь стояли перечень композиционных площадок и проверка
// TestInheritedWindowsAreDeclared — они требовали, чтобы держатель окна БЕЗ
// своей ручки был записан в pkg/authz.RevocationPolicy.Inherited.
//
// Сняты вместе с предметом (задача #2298). Предмета было два, и не стало обоих:
// перечень площадок стоял пустым (собственные фабрики звена сняты у всех, кеш
// строит носитель контура), а само состояние «держатель без своей ручки»
// объявлено недостижимым — процесс, не назвавший величину окна, не поднимается
// (`pkg/servicecontract`), и его находит TestEveryVerdictCacheProcessDeclaresItsOwnKnob
// ниже. То есть проверка требовала записывать состояние, которое соседняя
// проверка в том же файле объявляет находкой: возможность, объявленная и
// неисполнимая ни при каком входе.
//
// Утверждение о держателе без своей ручки в дереве теперь ОДНО, и живёт оно
// ниже.

// TestEveryVerdictCacheServiceIsDeclared — процесс, ДЕРЖАЩИЙ окно отзыва,
// объявлен политикой, каким бы именем он свою ручку ни назвал.
//
// Перепись по ИМЕНАМ РУЧЕК — закрытый словарь, и потому она по построению не
// видит процесс, приехавший с новым именем. Здесь вопрос задан иначе — «держит
// ли этот процесс окно вообще», — поэтому имя ручки роли не играет.
//
// Ровно настолько, и не дальше; границ у вопроса ТРИ, и все три названы, потому
// что каждая однажды была обещана прочь.
//
//  1. «Строит ли кеш» — не то же, что «держит окно». Конструктор интерсептора
//     заводил кеш за молчащего вызывающего, и тогда в исходнике сервиса не
//     оставалось ничего, что обход мог бы найти. Прежняя редакция этого
//     комментария обещала поймать седьмой сервис «в день появления» — обещание
//     было неверно, и неверно в худшую сторону: проверка читала такой файл,
//     засчитывала его в «осмотрено» и объявляла чистым. Ловит это
//     TestNoServiceTakesTheWindowImplicitly ниже; отказ в старте на неназванный
//     кеш даёт сам конструктор.
//  2. Форм владения ДВЕ, и вторая — большинство дерева. Процесс, отдавший окно
//     дескриптору носителя, не строит ничего, и обход, спрашивавший про
//     конструктор, молчал о нём. Форму знает
//     revocationwindowgate.ScanWindowOwnership; перепись печатает величину по
//     каждой, а ноль по любой — отказ предпосылки.
//  3. Обе формы — словари (имён конструкторов и имени поля). Единица переписи
//     здесь ПРОЦЕСС, и процесс, уже засчитанный, вторым окном её не двигает.
//     Величину спрашивает без словаря TestEveryAuthzWindowKnobIsDeclared, у
//     которого единица — ОКНО.
func TestEveryVerdictCacheServiceIsDeclared(t *testing.T) {
	t.Parallel()
	held, filesRead, err := verdictCacheHoldersUnder(repoRoot(t))
	if err != nil {
		t.Fatalf("%v", err)
	}

	declared := map[string]bool{}
	for key := range authz.RevocationPolicy.Windows {
		declared[strings.SplitN(key, " ", 2)[0]] = true
	}

	byCtor, byDescriptor := ownershipByForm(held)
	t.Logf("осмотрено: файлов процессов прочитано=%d, процессов держит окно=%d "+
		"(формой «%s»=%d, формой «%s»=%d), процессов объявлено политикой=%d, "+
		"распознаваемых конструкторов=%v, поле дескриптора=%q",
		filesRead, len(held),
		revocationwindowgate.OwnershipFormNames()[0], byCtor,
		revocationwindowgate.OwnershipFormNames()[1], byDescriptor,
		len(declared), revocationwindowgate.VerdictCacheCtorNames(),
		revocationwindowgate.DescriptorWindowField)

	assertHolderCensusIsNotVacuous(t, filesRead, held)

	for _, svc := range undeclaredHolders(held, declared) {
		t.Errorf("процесс держит кеш вердиктов, но политикой не объявлен: «%s» (%s).\n"+
			"Кешируется положительный вердикт ⇒ у процесса есть окно отзыва. "+
			"Внеси его в pkg/authz.RevocationPolicy.Windows вместе с именем СВОЕЙ ручки: "+
			"держателя без собственной ручки политика законным не объявляет, и второго "+
			"множества разрешённых у неё нет.", svc, formsOf(held[svc]))
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
	t.Parallel()
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
	t.Parallel()
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
		"KANAME_INTROSPECTION_CACHE_TTL",
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
	t.Parallel()
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
	t.Parallel()
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
// своей ручки, и когда запись Windows потеряла процесс.
//
// # Выход ОДИН — ручка; ведомости исключений у этого правила нет
//
// Здесь стояло «либо завести ручку, либо записать исключение осознанно».
// Ведомости исключений у гейта не было ни одной, и обещание проверки простить
// то, чего она не умеет, — тот самый класс, который этот файл ловит в других
// местах. Хуже: «исключение» тогда указывало на карту
// pkg/authz.RevocationPolicy.Inherited, а площадка, записанная туда, ЭТИМ ЖЕ
// гейтом становилась находкой — множество разрешённых он строит только из
// Windows. Одно состояние было объявлено законным и запрещено by construction.
//
// Карта снята (задача #2298), обещание — вместе с ней. Выход из красного один:
// своя ручка. Он не сужение, а совпадение с решением, уже стоящим в дереве, —
// pkg/servicecontract объявляет у окна отзыва, что «умолчания быть не может», и
// ОТКАЗЫВАЕТ В СТАРТЕ процессу, не назвавшему величину. Понадобится исключение —
// заводить придётся ведомость, каждая запись которой несёт обоснование и
// истекает сама; сегодня у такой ведомости нет ни одного кандидата: ручку имеют
// все восемь держателей дерева.
//
// # Вердикт живёт в функции, а не в этой пробе
//
// Утверждение сегодня МОЛЧИТ — все держатели свою ручку имеют, — поэтому его
// способность падать зелёным прогоном не доказывается никак. Вердикт и тексты
// находок вынесены в revocationwindowgate.JudgeOwnKnobs, которому вход можно
// подать; инъекция в обе стороны — в revocationwindowownknob_injection_test.go.
func TestEveryVerdictCacheProcessDeclaresItsOwnKnob(t *testing.T) {
	t.Parallel()
	held, filesRead, err := verdictCacheHoldersUnder(repoRoot(t))
	if err != nil {
		t.Fatalf("%v", err)
	}

	withOwnKnob := map[string]bool{}
	for key := range authz.RevocationPolicy.Windows {
		withOwnKnob[strings.SplitN(key, " ", 2)[0]] = true
	}

	byCtor, byDescriptor := ownershipByForm(held)
	verdict := revocationwindowgate.JudgeOwnKnobs(held, withOwnKnob)
	t.Logf("осмотрено: файлов процессов прочитано=%d, процессов держит окно=%d "+
		"(формой «%s»=%d, формой «%s»=%d), процессов со своей ручкой=%d; "+
		"держателей без ручки=%d, ручек без держателя=%d",
		filesRead, len(held),
		revocationwindowgate.OwnershipFormNames()[0], byCtor,
		revocationwindowgate.OwnershipFormNames()[1], byDescriptor,
		len(withOwnKnob),
		len(verdict.HoldersWithoutKnob), len(verdict.KnobsWithoutHolder))

	assertHolderCensusIsNotVacuous(t, filesRead, held)

	for _, svc := range verdict.HoldersWithoutKnob {
		t.Errorf("%s", revocationwindowgate.HolderWithoutKnobFinding(svc, formsOf(held[svc])))
	}
	// Обратная сторона: запись Windows, под которой в дереве нет процесса,
	// строящего кеш вердиктов, — находка. Иначе перепись переживёт свой предмет.
	for _, svc := range verdict.KnobsWithoutHolder {
		t.Errorf("%s", revocationwindowgate.KnobWithoutHolderFinding(svc))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Один обход, две формы владения, перепись ПО КАЖДОЙ
// ────────────────────────────────────────────────────────────────────────────

// verdictCacheHoldersUnder — процессы дерева root, держащие окно отзыва, форма
// владения по каждому и число прочитанных файлов.
//
// # Единица — ПРОЦЕСС, а не каталог под services/
//
// Край живёт вне services/, и обход, начинавшийся с одного этого каталога, не
// мог его увидеть в принципе.
//
// # Обход ОДИН, и это предмет, а не оформление
//
// Проверок о «кто держит окно» две, и до 2026-09-08 у каждой был свой обход:
// одна знала обе формы владения, другая — только вызов конструктора. Формально
// это был дубль, помеченный директивой линтера; по существу — два места об
// одном предмете, из которых верно одно. Разошлись они молча и сильно: в одном
// прогоне переписи печатали 3 и 8, и меньшая принадлежала проверке, чьё имя
// обещает «каждый процесс с кешем вердиктов объявлен». Заметить потерю было
// нечем — её
// предпосылка («хоть одна площадка найдена») выполнялась на трёх, а «не нашла»
// и «нечего искать» с этой стороны выглядят одинаково.
//
// Формы распознаёт `revocationwindowgate.ScanWindowOwnership`, и он же
// единственный, кто их знает.
func verdictCacheHoldersUnder(root string) (map[string]revocationwindowgate.WindowOwnership, int, error) {
	processes := map[string]string{}
	servicesDir := filepath.Join(root, "services")
	svcEntries, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, 0, fmt.Errorf("предпосылка гейта нарушена: каталог services/ не читается: %w", err)
	}
	for _, svc := range svcEntries {
		if svc.IsDir() {
			processes[svc.Name()] = filepath.Join(servicesDir, svc.Name())
		}
	}
	gatewayDir := filepath.Join(root, "gateway")
	if _, serr := os.Stat(gatewayDir); serr != nil {
		return nil, 0, fmt.Errorf("предпосылка гейта нарушена: дерево края gateway/ не читается: %w", serr)
	}
	processes[gatewayProcess] = gatewayDir

	filesRead := 0
	held := map[string]revocationwindowgate.WindowOwnership{}
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
			own, perr := revocationwindowgate.ScanWindowOwnership(p, string(src))
			if perr != nil {
				return perr //nolint:wrapcheck // parse error propagates as-is
			}
			if own.Any() {
				held[name] = held[name].Merge(own)
			}
			return nil
		})
		if werr != nil {
			return nil, 0, fmt.Errorf("обход %s: %w", dir, werr)
		}
	}
	return held, filesRead, nil
}

// ownershipByForm — сколько процессов держат окно КАЖДОЙ формой.
//
// Величины считаются порознь и печатаются порознь. Сумма их не заменяет:
// «форма 1 исчезла» и «площадок стало меньше» дают одинаковую сумму, а означают
// разное — первое есть ослепшая половина распознавателя.
func ownershipByForm(held map[string]revocationwindowgate.WindowOwnership) (byCtor, byDescriptor int) {
	for _, own := range held {
		if own.ByConstructor {
			byCtor++
		}
		if own.ByDescriptor {
			byDescriptor++
		}
	}
	return byCtor, byDescriptor
}

// formsOf — как именно площадка держит окно, для текста находки. Находка,
// называющая процесс и не называющая форму, посылает читателя искать вызов
// конструктора там, где его нет вовсе.
func formsOf(own revocationwindowgate.WindowOwnership) string {
	names := revocationwindowgate.OwnershipFormNames()
	var forms []string
	if own.ByConstructor {
		forms = append(forms, names[0])
	}
	if own.ByDescriptor {
		forms = append(forms, names[1])
	}
	if len(forms) == 0 {
		return "форма не установлена"
	}
	return strings.Join(forms, ", ")
}

// assertHolderCensusIsNotVacuous — предпосылка обхода, ПО КАЖДОЙ форме.
//
// Общая предпосылка («хоть одна площадка найдена») слепа ровно к тому случаю,
// ради которого этот файл переписан: половина распознавателя умирает, вторая
// продолжает находить свои площадки, и перепись выглядит здоровой. Поэтому ноль
// по ЛЮБОЙ форме — отказ: он означает либо переименованный конструктор, либо
// переименованное поле дескриптора, и в обоих случаях молчание проверки
// перестало значить «чисто».
func assertHolderCensusIsNotVacuous(t *testing.T, filesRead int, held map[string]revocationwindowgate.WindowOwnership) {
	t.Helper()
	if why := holderCensusVacuity(filesRead, held); why != "" {
		t.Fatalf("предпосылка гейта нарушена: %s", why)
	}
}

// holderCensusVacuity — причина, по которой перепись держателей окна ничего не
// измерила, либо пустая строка.
//
// Отделено от утверждения намеренно: предпосылку, роняющую прогон, иначе нельзя
// проверить инъекцией — а проверка, чью способность падать не доказали,
// неотличима от вечно-зелёной. Текст находки живёт здесь в единственном
// экземпляре, поэтому инъекция судит ровно то, что прочтёт человек.
func holderCensusVacuity(filesRead int, held map[string]revocationwindowgate.WindowOwnership) string {
	if filesRead == 0 {
		return "не прочитано ни одного файла процессов"
	}
	byCtor, byDescriptor := ownershipByForm(held)
	names := revocationwindowgate.OwnershipFormNames()
	if byCtor == 0 {
		return fmt.Sprintf("прочитано %d файлов, но формой «%s» окно не держит НИ ОДИН процесс. "+
			"Конструктор переименован либо все площадки перешли на дескриптор — реши, что из "+
			"двух: в первом случае половина распознавателя ослепла и молчит, во втором форму "+
			"надо снять вместе с её предметом. Распознаваемые конструкторы: %v",
			filesRead, names[0], revocationwindowgate.VerdictCacheCtorNames())
	}
	if byDescriptor == 0 {
		return fmt.Sprintf("прочитано %d файлов, но формой «%s» окно не держит НИ ОДИН процесс. "+
			"Поле %q переименовано либо дескриптор носителя больше не несёт окна — в первом "+
			"случае из-под наблюдения ушло большинство площадок дерева, и ушло молча",
			filesRead, names[1], revocationwindowgate.DescriptorWindowField)
	}
	return ""
}

// undeclaredHolders — процессы, держащие окно и не объявленные политикой.
func undeclaredHolders(held map[string]revocationwindowgate.WindowOwnership, declared map[string]bool) []string {
	var out []string
	for svc := range held {
		if !declared[svc] {
			out = append(out, svc)
		}
	}
	sort.Strings(out)
	return out
}
