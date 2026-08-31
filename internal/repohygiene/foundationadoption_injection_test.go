// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// foundationadoption_injection_test.go — доказательство того, что перепись
// усыновления УМЕЕТ краснеть и УМЕЕТ молчать.
//
// Инъекция в обе стороны по каждому свойству, которое гейт держит. Без второй
// стороны проба ловила бы форму, а не существо, и первый же ложный срабат её
// отключил бы.
//
//	краснеет  · место сборки сняло провязку — находка НАЗЫВАЕТ ЕГО ИМЯ И КООРДИНАТУ
//	молчит    · соседнее место того же каталога, провязку сохранившее
//	краснеет  · то же на НАСТОЯЩЕМ исходнике: у сервиса с двумя серверами звено
//	            снято у второго, первый цел
//	молчит    · слушатель, усыновивший через посредника
//	краснеет  · пропуск, чья возможность усыновлена всеми названными единицами
//	молчит    · пропуск, названный каталогом, где усыновило лишь одно место из двух
//	краснеет  · запись «предмета нет», опровергнутая усыновлением
//	краснеет  · запись про слушателя, которого в дереве нет
//	краснеет  · пропуск без номера задачи
//	краснеет  · запись «не несёт ни один» при первом усыновившем
//	краснеет  · заявление посредника, пережившее предмет
//	молчит    · заявление посредника, у которого предмет есть
//	краснеет  · обёртка, не оборачивающая сборку сервера
//	краснеет  · обёртка, которую никто не зовёт
//	краснеет  · обёртка, поглотившая слушателя целиком
//	молчит    · обёртка с предметом
//	краснеет  · возможность по месту сборки, опознаваемая только импортом
//	краснеет  · возможность без объявленной единицы счёта
//	краснеет  · возможность, объявленная обязательной и отсутствующая в фундаменте
//	краснеет  · слушателей ноль — перепись беспредметна
//	молчит    · упоминание возможности в КОММЕНТАРИИ усыновлением не считается —
//	            и это проверено на НАСТОЯЩЕМ файле дерева, найденном ПОИСКОМ
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// injCap — возможность-функция для синтетики. Форма та же, что у настоящих:
// пакет большой, признак — вызванный символ, единица — место сборки.
func injCap() FoundationCapability {
	return FoundationCapability{
		Name: "звено", Pkg: "pkg/fake",
		Unit:    FoundationUnitListener,
		Symbols: []string{"fake.Wire"},
	}
}

// injTree раскладывает синтетическое дерево: фундамент, посредник и слушателей.
// Тела файлов подаются как есть — гейт разбирает их синтаксисом.
func injTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// injBase — дерево той самой формы, на которой сломалась первая редакция гейта:
// у слушателя A ДВА независимо собранных сервера в одном каталоге, у B — один,
// собранный через посредника. Оба A-сервера провязаны, поэтому это законный
// близнец: гейт обязан молчать.
func injBase() map[string]string {
	return map[string]string{
		// Фундамент: возможность объявлена здесь.
		"pkg/fake/wire.go": "package fake\n\nfunc Wire() int { return 1 }\n",
		// Посредник и обёртка в одном лице: ставит возможность за вызывающего и
		// сам собирает сервер.
		"pkg/host/serve.go": "package host\n\nimport (\n\t\"google.golang.org/grpc\"\n\t\"x/pkg/fake\"\n)\n\n" +
			"func Serve() *grpc.Server {\n\t_ = fake.Wire()\n\treturn grpc.NewServer()\n}\n",
		// Слушатель A — ДВА сервера своей проводкой.
		"services/a/main.go": "package main\n\nimport (\n\t\"google.golang.org/grpc\"\n\t\"x/pkg/fake\"\n)\n\n" +
			"func main() {\n" +
			"\tpubChain := []int{fake.Wire()}\n" +
			"\tpubSrv := grpc.NewServer(pubChain...)\n" +
			"\tintChain := []int{fake.Wire()}\n" +
			"\tintSrv := grpc.NewServer(intChain...)\n" +
			"\t_, _ = pubSrv, intSrv\n}\n",
		// Слушатель B — через посредника.
		"services/b/main.go": "package main\n\nimport \"x/pkg/host\"\n\n" +
			"func main() { srv := host.Serve(); _ = srv }\n",
	}
}

// injDropInternalWiring снимает провязку у ВТОРОГО сервера слушателя A и
// оставляет первый целым. Это синтетическая форма опыта рецензента.
func injDropInternalWiring(files map[string]string) {
	files["services/a/main.go"] = strings.Replace(files["services/a/main.go"],
		"intChain := []int{fake.Wire()}", "intChain := []int{}", 1)
}

func injRoster() FoundationRoster {
	return FoundationRoster{
		Capabilities: []FoundationCapability{injCap()},
		Wrappers: []FoundationWrapper{
			{Dir: "pkg/host", Entry: "host.Serve", Why: "синтетическая обёртка"},
		},
		Providers: []FoundationProvider{
			{Name: "pkg/host", Entry: "host.Serve", Carries: []string{"звено"}},
		},
	}
}

// injSurvey — перечень, места сборки и разборы по синтетическому дереву.
type injSurvey struct {
	Dirs    []string
	Scans   map[string]*FoundationScan
	Sites   []FoundationSite
	Markers []string
	Prov    map[string]*FoundationScan
	Wrap    map[string]*FoundationScan
}

func injScan(t *testing.T, root string, r FoundationRoster) injSurvey {
	t.Helper()
	sv := injSurvey{
		Markers: serverSiteMarkers([]string{"grpc.NewServer"}, r.Wrappers),
		Prov:    map[string]*FoundationScan{},
		Wrap:    map[string]*FoundationScan{},
	}
	var err error
	sv.Dirs, sv.Scans, _, err = DiscoverListeners(root, sv.Markers)
	if err != nil {
		t.Fatalf("перепись слушателей: %v", err)
	}
	if sv.Sites, err = DiscoverServerSites(root, sv.Dirs, sv.Markers, r.Wrappers); err != nil {
		t.Fatalf("перепись мест сборки: %v", err)
	}
	for _, p := range r.Providers {
		s, serr := ScanGoTree(filepath.Join(root, filepath.FromSlash(p.Name)))
		if serr != nil {
			t.Fatalf("посредник %s: %v", p.Name, serr)
		}
		sv.Prov[p.Name] = s
	}
	for _, w := range r.Wrappers {
		s, serr := ScanGoTree(filepath.Join(root, filepath.FromSlash(w.Dir)))
		if serr != nil {
			t.Fatalf("обёртка %s: %v", w.Dir, serr)
		}
		sv.Wrap[w.Dir] = s
	}
	return sv
}

// injRun прогоняет перепись по синтетическому дереву и отдаёт её итог.
func injRun(t *testing.T, root string, r FoundationRoster) FoundationCensus {
	t.Helper()
	sv := injScan(t, root, r)
	return r.Adjudicate(sv.Dirs, sv.Scans, sv.Sites, sv.Prov)
}

// TestFoundationGateStaysSilentOnLegitimateAdoption — ЗАКОННЫЙ БЛИЗНЕЦ.
//
// Все три места сборки усыновили, и одно из них — ЧЕРЕЗ ПОСРЕДНИКА, то есть
// имени возможности в его каталоге нет вовсе. Именно на нём наивный предикат
// («упомянул ли слушатель») дал бы находку, и она была бы ложной.
func TestFoundationGateStaysSilentOnLegitimateAdoption(t *testing.T) {
	cen := injRun(t, injTree(t, injBase()), injRoster())
	if len(cen.Listeners) != 2 || len(cen.Sites) != 3 {
		t.Fatalf("вход построен неверно: каталогов %d (нужно 2), мест сборки %d (нужно 3) — "+
			"без каталога с ДВУМЯ местами проба не различает единицу счёта",
			len(cen.Listeners), len(cen.Sites))
	}
	if cen.Carried != 3 || len(cen.Findings) != 0 || len(cen.Stale) != 0 {
		t.Fatalf("гейт краснеет на законном усыновлении: %s\nнаходки: %v\nистёкшие: %v",
			cen, cen.Findings, cen.Stale)
	}
	t.Logf("законный близнец: %s", cen)
}

// TestFoundationGateCountsBySiteNotByDirectory — ПОПРАВКА ЕДИНИЦЫ СЧЁТА.
//
// Ровно тот опыт, которым перепись была опровергнута: у каталога два сервера,
// звено снято у ОДНОГО. Проба утверждает обе стороны сразу:
//
//	находка есть и называет ПОСТРАДАВШЕЕ место (а не каталог и не соседа);
//	объединение по каталогу на этом же входе по-прежнему отвечает «несёт» —
//	то есть прежняя единица счёта промолчала бы, и это не догадка, а замер.
func TestFoundationGateCountsBySiteNotByDirectory(t *testing.T) {
	files := injBase()
	injDropInternalWiring(files)
	root := injTree(t, files)
	r := injRoster()

	sv := injScan(t, root, r)
	cen := r.Adjudicate(sv.Dirs, sv.Scans, sv.Sites, sv.Prov)

	if len(cen.Findings) != 1 {
		t.Fatalf("снятая у одного из двух серверов провязка не дала ровно одной находки: %s\n%v",
			cen, cen.Findings)
	}
	f := cen.Findings[0]
	if !strings.HasPrefix(f.Listener, "services/a#") || !strings.Contains(f.Listener, "intSrv") {
		t.Fatalf("находка называет %q, а провязку снял второй сервер services/a: координата "+
			"уводит не туда", f.Listener)
	}
	if !strings.Contains(f.Detail, "main.go:") {
		t.Fatalf("текст находки не несёт координаты файла и строки: %q — по каталогу виновника "+
			"среди двух серверов не найти", f.Detail)
	}
	if cen.Carried != 2 {
		t.Fatalf("уцелевшие места перестали считаться усыновившими: %s — проба покраснела на "+
			"всём и виновника не различает", cen)
	}

	// Обратная сторона того же входа: прежняя единица (каталог) молчит.
	// Без этого утверждения нельзя сказать, что находку даёт СМЕНА ЕДИНИЦЫ, а не
	// что-нибудь ещё, изменившееся заодно.
	if !sv.Scans["services/a"].Direct(injCap()) {
		t.Fatalf("объединение по каталогу перестало видеть звено — значит вход не тот: он обязан " +
			"оставлять каталогу уцелевшую проводку соседнего сервера, иначе прежняя единица " +
			"покраснела бы и сама, и опыт ничего не доказывает")
	}
	t.Logf("по каталогу: несёт (звено осталось у соседнего сервера); по месту сборки: %s", f.Detail)
}

// TestFoundationGateRedensWhenARealListenerLosesItsWiring — то же на НАСТОЯЩЕМ
// исходнике, а не на синтетике.
//
// Синтетика доказывает механику. Она не доказывает, что механика приложена к
// настоящему дереву верно: срез провязки настоящего композиционного корня — это
// цепочки в переменных, соседние функции пакета и посредник, и синтетика ни
// одного из этих случаев не содержит.
//
// Поэтому проба берёт каталог, у которого мест сборки БОЛЬШЕ ОДНОГО, копирует
// пакет его композиционного корня во временное дерево, вырезает вызовы звена,
// относящиеся ко ВТОРОМУ месту, и разбирает получившийся исходник заново.
// Ожидание точное: ровно одно место теряет возможность, соседнее — сохраняет.
func TestFoundationGateRedensWhenARealListenerLosesItsWiring(t *testing.T) {
	root := repoRoot(t)
	r := foundationRoster()
	sv := foundationSurveyTree(t, root, r)

	cap := injPickListenerCapability(t, r)
	dir, sites, neighbour, victim := injPickTwoSitesInOneFile(t, sv.Sites)

	tmp, removed := injCopyPkgDroppingWiring(t, root, victim, neighbour, cap)
	if removed == 0 {
		t.Fatalf("между местами %s и %s не нашлось ни одной строки с вызовом %q: вырезать нечего, "+
			"и проба ничего бы не доказала", neighbour.ID, victim.ID, cap.Name)
	}

	cut, err := DiscoverServerSites(tmp, []string{dir}, sv.Markers, r.Wrappers)
	if err != nil {
		t.Fatalf("места сборки во временном дереве не разобрались: %v", err)
	}
	if len(cut) != len(sites) {
		t.Fatalf("во временном дереве мест сборки %d, а в настоящем у %s — %d: вырезание задело "+
			"не то", len(cut), dir, len(sites))
	}

	var lost, kept []string
	for _, s := range cut {
		if r.Reach(s.Slice, sv.ProviderScan)[cap.Name] {
			kept = append(kept, s.ID)
		} else {
			lost = append(lost, s.ID)
		}
	}
	if len(lost) != 1 || lost[0] != victim.ID {
		t.Fatalf("вырезано %d строк провязки у %s, а возможность потеряли %v (сохранили %v): "+
			"срез провязки не различает соседние серверы одного каталога",
			removed, victim.ID, lost, kept)
	}
	if len(kept) == 0 {
		t.Fatalf("возможность потеряли ВСЕ места каталога %s: вырезание задело общий источник, "+
			"и проба не отличает «нашёл виновника» от «покраснел на всём»", dir)
	}

	// Вердикт переписи на этом дереве: ровно одна находка, и она называет
	// пострадавшее место с координатой.
	swapped := append([]FoundationSite(nil), sv.Sites...)
	for i := range swapped {
		for _, s := range cut {
			if swapped[i].ID == s.ID {
				swapped[i] = s
			}
		}
	}
	cen := r.Adjudicate(sv.Dirs, sv.Scans, swapped, sv.ProviderScan)
	if len(cen.Findings) != 1 || cen.Findings[0].Listener != victim.ID {
		t.Fatalf("перепись на настоящем исходнике без провязки дала %d находок (%v), а ждали одну "+
			"про %s", len(cen.Findings), cen.Findings, victim.ID)
	}

	// И контроль в обратную сторону, на том же входе: объединение по каталогу
	// молчит. Это и есть замер, которым опровергнута прежняя единица счёта.
	whole, err := ScanGoTree(filepath.Join(tmp, filepath.FromSlash(dir)))
	if err != nil {
		t.Fatal(err)
	}
	if !whole.Direct(cap) {
		t.Fatalf("объединение по каталогу %s перестало видеть %q — вход не тот: у соседнего "+
			"сервера провязка обязана уцелеть", dir, cap.Name)
	}
	t.Logf("настоящий исходник %s: вырезано строк %d → по каталогу «несёт», по месту сборки "+
		"находка: %s", victim.File, removed, cen.Findings[0].Detail)
}

// injPickListenerCapability — возможность, считаемая по месту сборки, выбранная
// по СОДЕРЖАНИЮ набора, а не выписанная именем: выписанное имя устарело бы
// вместе с первой же правкой набора, и проба падала бы на своей фикстуре.
func injPickListenerCapability(t *testing.T, r FoundationRoster) FoundationCapability {
	t.Helper()
	for _, c := range r.Capabilities {
		if c.Unit == FoundationUnitListener && len(c.Symbols) > 0 {
			return c
		}
	}
	t.Fatalf("в наборе нет ни одной возможности, считаемой по месту сборки: проба про единицу " +
		"счёта потеряла предмет — либо все возможности стали процессными, либо единица снята")
	return FoundationCapability{}
}

// injPickTwoSitesInOneFile — каталог, у которого ДВА места сборки стоят в ОДНОМ
// файле, и сама эта пара.
//
// Выбор по содержанию переписи, а не по выписанному имени: выписанное устарело бы
// вместе с первой правкой раскладки. Одного файла требует способ вырезания —
// провязка второго сервера ищется между строкой первой сборки и строкой второй.
//
// Не нашли такой пары — это ОТКАЗ, а не пропуск. Пропуск здесь означал бы, что
// контроль над единицей счёта исчезает молча при перестановке файлов, то есть
// при самой вероятной форме правки; ровно за это правило снимает t.Skip у
// фикстур (`testing.md` §«Всякий механизм исключения обязан истекать сам»).
func injPickTwoSitesInOneFile(t *testing.T, sites []FoundationSite) (
	string, []FoundationSite, FoundationSite, FoundationSite) {

	t.Helper()
	byDir := map[string][]FoundationSite{}
	var order []string
	for _, s := range sites {
		if _, ok := byDir[s.Dir]; !ok {
			order = append(order, s.Dir)
		}
		byDir[s.Dir] = append(byDir[s.Dir], s)
	}
	sort.Strings(order)
	multi := 0
	for _, d := range order {
		if len(byDir[d]) > 1 {
			multi++
		}
		byFile := map[string][]FoundationSite{}
		for _, s := range byDir[d] {
			byFile[s.File] = append(byFile[s.File], s)
		}
		var files []string
		for f := range byFile {
			files = append(files, f)
		}
		sort.Strings(files)
		for _, f := range files {
			in := byFile[f]
			if len(in) < 2 {
				continue
			}
			sort.Slice(in, func(i, j int) bool { return in[i].Line < in[j].Line })
			return d, byDir[d], in[len(in)-2], in[len(in)-1]
		}
	}
	t.Fatalf("в дереве не осталось файла, где собираются ДВА сервера одного каталога: предмет "+
		"этой пробы исчез. Это не повод её пропустить — либо раскладка изменилась и пробу надо "+
		"переписать под новую, либо перепись мест сломалась. Мест всего %d, каталогов с более "+
		"чем одним местом %d", len(sites), multi)
	return "", nil, FoundationSite{}, FoundationSite{}
}

// injCopyPkgDroppingWiring копирует пакет композиционного корня во временное
// дерево, вырезая строки с вызовом возможности между СОСЕДНИМ и ПОСТРАДАВШИМ
// местом сборки.
//
// Граница взята по номерам строк самих мест: провязка сервера пишется между
// предыдущей сборкой и своей. Если раскладка исходника когда-нибудь перестанет
// быть такой, проба скажет об этом отказом (вырезано ноль либо потеряли не то),
// а не пройдёт тихо.
func injCopyPkgDroppingWiring(t *testing.T, root string, victim, neighbour FoundationSite,
	cap FoundationCapability) (string, int) {

	t.Helper()
	tmp := t.TempDir()
	pkgRel := filepath.Dir(victim.File)
	srcDir := filepath.Join(root, filepath.FromSlash(pkgRel))
	dstDir := filepath.Join(tmp, filepath.FromSlash(pkgRel))
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, rerr := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if rerr != nil {
			t.Fatal(rerr)
		}
		out := string(body)
		if filepath.Join(pkgRel, e.Name()) == filepath.FromSlash(victim.File) {
			out, removed = injDropCallsInRange(string(body), neighbour.Line, victim.Line, cap.Symbols)
		}
		if werr := os.WriteFile(filepath.Join(dstDir, e.Name()), []byte(out), 0o644); werr != nil {
			t.Fatal(werr)
		}
	}
	return tmp, removed
}

// injDropCallsInRange убирает строки, чей текст содержит вызов одного из
// символов, в открытом диапазоне строк (from, to).
//
// Сверка идёт подстрокой, и предмет у формы узкий: символы — селекторы вида
// `grpcsrv.NewAdmission`, поэтому хвостом чужого имени совпадение станет только
// при псевдониме импорта, кончающемся на `grpcsrv`. Вход синтетический, его
// строит эта же проба. Замер по 4984 файлам Go: хвостов ноль.
func injDropCallsInRange(body string, from, to int, symbols []string) (string, int) {
	lines := strings.Split(body, "\n")
	var out []string
	removed := 0
	for i, ln := range lines {
		n := i + 1
		drop := false
		if n > from && n < to {
			for _, sym := range symbols {
				if strings.Contains(ln, sym+"(") {
					drop = true
					break
				}
			}
		}
		if drop {
			removed++
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n"), removed
}

// TestFoundationLedgerEntriesExpireOnTheirOwn — САМОИСТЕЧЕНИЕ обеих ведомостей.
//
// Каждая форма записи проверяется отдельно: одна проверка на все сразу не
// отличила бы «истекает пропуск» от «истекает что-нибудь».
func TestFoundationLedgerEntriesExpireOnTheirOwn(t *testing.T) {
	root := injTree(t, injBase()) // дерево, где ВСЕ места усыновили

	cases := []struct {
		name   string
		mutate func(*FoundationRoster)
		want   string
	}{
		{
			name: "пропуску нечего исключать",
			mutate: func(r *FoundationRoster) {
				r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Listener: "services/a", Issue: 1}}
			},
			want: "больше нечего исключать",
		},
		{
			name: "запись «нет предмета» опровергнута усыновлением",
			mutate: func(r *FoundationRoster) {
				r.NoSubject = []FoundationNoSubject{{Capability: "звено", Listener: "services/b", Why: "нечего сужать"}}
			},
			want: "опровергнута деревом",
		},
		{
			name: "запись про слушателя, которого нет",
			mutate: func(r *FoundationRoster) {
				r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Listener: "services/zzz", Issue: 1}}
			},
			want: "такого в дереве нет",
		},
		{
			name: "запись про возможность, которой нет в наборе",
			mutate: func(r *FoundationRoster) {
				r.Ledger = []FoundationLedgerEntry{{Capability: "чего-нибудь", Listener: "services/a", Issue: 1}}
			},
			want: "такой в наборе нет",
		},
		{
			name: "пропуск без номера задачи",
			mutate: func(r *FoundationRoster) {
				r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Listener: "services/a"}}
			},
			want: "не называет задачи",
		},
		{
			name: "отсутствие предмета без причины",
			mutate: func(r *FoundationRoster) {
				r.NoSubject = []FoundationNoSubject{{Capability: "звено", Listener: "services/a"}}
			},
			want: "не названо причиной",
		},
		{
			name: "запись «не несёт ни один» при первом усыновившем",
			mutate: func(r *FoundationRoster) {
				r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Issue: 1}}
			},
			want: "уже несёт",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := injRoster()
			tc.mutate(&r)
			cen := injRun(t, root, r)
			if len(cen.Stale) == 0 {
				t.Fatalf("запись, которой нечего исключать, не найдена: %s — значит ведомость "+
					"не самоистекает и переживёт свой предмет", cen)
			}
			joined := strings.Join(cen.Stale, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("истечение названо не тем: жду %q, получено:\n%s", tc.want, joined)
			}
		})
	}

	// Обратная сторона: ведомость, у которой предмет ЕСТЬ, молчит. Без этой
	// проверки все семь выше зеленели бы и на гейте, который краснеет на всякой
	// записи вообще.
	t.Run("живая запись молчит", func(t *testing.T) {
		files := injBase()
		files["services/a/main.go"] = "package main\n\nimport \"google.golang.org/grpc\"\n\n" +
			"func main() { srv := grpc.NewServer(); _ = srv }\n"
		r := injRoster()
		r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Listener: "services/a", Issue: 1, Why: "предмет есть"}}
		cen := injRun(t, injTree(t, files), r)
		if len(cen.Stale) != 0 || len(cen.Findings) != 0 {
			t.Fatalf("живой пропуск объявлен истёкшим: %s\nистёкшие: %v", cen, cen.Stale)
		}
		if cen.Excused != 1 {
			t.Fatalf("живой пропуск не засчитан записанным: %s", cen)
		}
	})

	// Запись, названная КАТАЛОГОМ, покрывает несколько мест сборки и истекает
	// только когда усыновили ВСЕ. Это отдельное свойство: мерка «истекает на
	// первом усыновившем» сняла бы запись, у которой второй сервер ещё не
	// провязан, и он остался бы находкой без объяснения — то есть починка
	// одного места ломала бы прогон.
	t.Run("запись каталога жива, пока усыновили не все его места", func(t *testing.T) {
		files := injBase()
		injDropInternalWiring(files)
		r := injRoster()
		r.Ledger = []FoundationLedgerEntry{{Capability: "звено", Listener: "services/a", Issue: 1,
			Why: "второй сервер каталога ещё не провязан"}}
		cen := injRun(t, injTree(t, files), r)
		if len(cen.Stale) != 0 {
			t.Fatalf("запись каталога объявлена истёкшей, хотя одно из двух его мест возможность "+
				"не несёт: %v", cen.Stale)
		}
		if len(cen.Findings) != 0 {
			t.Fatalf("запись каталога не покрыла своё место сборки: %v", cen.Findings)
		}
		if cen.Carried != 2 || cen.Excused != 1 {
			t.Fatalf("числа переписи не показывают частичность: %s — а именно ради их подвижности "+
				"единица счёта и переделана", cen)
		}
		t.Logf("частичная провязка видна числами: %s", cen)
	})
}

// TestFoundationProviderClaimIsVerifiedAgainstTheTree — предпосылка гейта.
func TestFoundationProviderClaimIsVerifiedAgainstTheTree(t *testing.T) {
	scan := func(t *testing.T, root, dir string) map[string]*FoundationScan {
		t.Helper()
		s, err := ScanGoTree(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			t.Fatal(err)
		}
		return map[string]*FoundationScan{dir: s}
	}

	t.Run("заявление с предметом молчит", func(t *testing.T) {
		root := injTree(t, injBase())
		r := injRoster()
		if bad := r.VerifyProviderClaims(scan(t, root, "pkg/host")); len(bad) != 0 {
			t.Fatalf("верное заявление объявлено устаревшим: %v", bad)
		}
	})

	t.Run("заявление пережило предмет — краснеет", func(t *testing.T) {
		files := injBase()
		// Посредник перестал ставить возможность, а заявление осталось.
		files["pkg/host/serve.go"] = "package host\n\nimport \"google.golang.org/grpc\"\n\n" +
			"func Serve() *grpc.Server { return grpc.NewServer() }\n"
		root := injTree(t, files)
		r := injRoster()
		bad := r.VerifyProviderClaims(scan(t, root, "pkg/host"))
		if len(bad) != 1 || !strings.Contains(bad[0], "pkg/host") || !strings.Contains(bad[0], "звено") {
			t.Fatalf("устаревшее заявление не поймано либо названо без координаты: %v", bad)
		}
		t.Logf("%s", bad[0])
	})

	t.Run("носителем объявлено то, чего нет в наборе", func(t *testing.T) {
		root := injTree(t, injBase())
		r := injRoster()
		r.Providers[0].Carries = []string{"чего-нибудь"}
		bad := r.VerifyProviderClaims(scan(t, root, "pkg/host"))
		if len(bad) != 1 || !strings.Contains(bad[0], "такой возможности в наборе нет") {
			t.Fatalf("несуществующая возможность в заявлении не поймана: %v", bad)
		}
	})
}

// TestFoundationWrapperDeclarationIsVerifiedAgainstTheTree — объявление обёртки
// есть ПОСЛАБЛЕНИЕ, и оно обязано истекать само.
func TestFoundationWrapperDeclarationIsVerifiedAgainstTheTree(t *testing.T) {
	t.Run("обёртка с предметом молчит", func(t *testing.T) {
		root := injTree(t, injBase())
		r := injRoster()
		sv := injScan(t, root, r)
		if bad := r.VerifyWrappers(sv.Markers, sv.Scans, sv.Wrap); len(bad) != 0 {
			t.Fatalf("верное объявление обёртки объявлено беспредметным: %v", bad)
		}
	})

	t.Run("обёртка не оборачивает сборку сервера — краснеет", func(t *testing.T) {
		files := injBase()
		// Посредник остался носителем, но сервер собирать перестал.
		files["pkg/host/serve.go"] = "package host\n\nimport \"x/pkg/fake\"\n\n" +
			"func Serve() int { return fake.Wire() }\n"
		root := injTree(t, files)
		r := injRoster()
		sv := injScan(t, root, r)
		bad := r.VerifyWrappers(sv.Markers, sv.Scans, sv.Wrap)
		if len(bad) != 1 || !strings.Contains(bad[0], "сборки сервера в её каталоге нет") {
			t.Fatalf("обёртка без предмета не поймана: %v", bad)
		}
		t.Logf("%s", bad[0])
	})

	t.Run("обёртку никто не зовёт — краснеет", func(t *testing.T) {
		files := injBase()
		// Слушатель B перестал звать обёртку и собирает сервер сам.
		files["services/b/main.go"] = "package main\n\nimport (\n\t\"google.golang.org/grpc\"\n\t\"x/pkg/fake\"\n)\n\n" +
			"func main() { srv := grpc.NewServer(fake.Wire()); _ = srv }\n"
		root := injTree(t, files)
		r := injRoster()
		sv := injScan(t, root, r)
		bad := r.VerifyWrappers(sv.Markers, sv.Scans, sv.Wrap)
		if len(bad) != 1 || !strings.Contains(bad[0], "не зовёт никто") {
			t.Fatalf("обёртка без вызывающих не поймана: %v", bad)
		}
		t.Logf("%s", bad[0])
	})

	t.Run("обёртка поглотила слушателя целиком — краснеет", func(t *testing.T) {
		root := injTree(t, injBase())
		r := injRoster()
		// Каталог слушателя объявлен обёрткой: его места сборки исчезли бы из
		// переписи, и он выглядел бы усыновившим всё.
		r.Wrappers = append(r.Wrappers, FoundationWrapper{
			Dir: "services/a", Entry: "a.Serve", Why: "попытка вывести слушателя из переписи"})
		markers := serverSiteMarkers([]string{"grpc.NewServer"}, r.Wrappers)
		dirs, _, _, err := DiscoverListeners(root, markers)
		if err != nil {
			t.Fatal(err)
		}
		_, err = DiscoverServerSites(root, dirs, markers, r.Wrappers)
		if err == nil {
			t.Fatalf("каталог с уликой сборки, у которого не осталось ни одного места, прошёл " +
				"молча: объявлением обёртки можно вывести слушателя из переписи")
		}
		if !strings.Contains(err.Error(), "services/a") {
			t.Fatalf("отказ не называет поглощённый каталог: %v", err)
		}
		t.Logf("%v", err)
	})
}

// TestFoundationRosterRefusesCapabilityWithoutSubject — возможность, объявленная
// обязательной, обязана существовать, быть видимой и знать свою единицу счёта.
func TestFoundationRosterRefusesCapabilityWithoutSubject(t *testing.T) {
	root := injTree(t, injBase())

	t.Run("настоящая возможность молчит", func(t *testing.T) {
		if bad := injRoster().VerifyCapabilities(root); len(bad) != 0 {
			t.Fatalf("существующая возможность объявлена отсутствующей: %v", bad)
		}
	})

	t.Run("каталога в фундаменте нет — краснеет", func(t *testing.T) {
		r := injRoster()
		r.Capabilities[0].Pkg = "pkg/нет-такого"
		bad := r.VerifyCapabilities(root)
		if len(bad) != 1 || !strings.Contains(bad[0], "в фундаменте нет") {
			t.Fatalf("отсутствующая возможность не поймана: %v", bad)
		}
	})

	t.Run("признака усыновления нет — краснеет", func(t *testing.T) {
		r := injRoster()
		r.Capabilities[0].Symbols = nil
		bad := r.VerifyCapabilities(root)
		if len(bad) == 0 || !strings.Contains(strings.Join(bad, "\n"), "ни одного признака усыновления") {
			t.Fatalf("возможность без признака не поймана: %v", bad)
		}
	})

	t.Run("единица счёта не объявлена — краснеет", func(t *testing.T) {
		r := injRoster()
		r.Capabilities[0].Unit = ""
		bad := r.VerifyCapabilities(root)
		if len(bad) != 1 || !strings.Contains(bad[0], "не объявлена единица счёта") {
			t.Fatalf("возможность без единицы счёта не поймана: %v", bad)
		}
		t.Logf("%s", bad[0])
	})

	t.Run("возможность по месту сборки, видимая только импортом — краснеет", func(t *testing.T) {
		r := injRoster()
		r.Capabilities[0].Symbols = nil
		r.Capabilities[0].ImportPath = "x/pkg/fake"
		bad := r.VerifyCapabilities(root)
		if len(bad) != 1 || !strings.Contains(bad[0], "до места сборки он не доезжает") {
			t.Fatalf("несочетаемая пара «единица — признак» не поймана: %v", bad)
		}
		t.Logf("%s", bad[0])
	})
}

// TestFoundationCensusRefusesWhenThereAreNoListeners — «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
func TestFoundationCensusRefusesWhenThereAreNoListeners(t *testing.T) {
	root := injTree(t, map[string]string{
		"pkg/fake/wire.go":  "package fake\n\nfunc Wire() int { return 1 }\n",
		"pkg/host/serve.go": "package host\n\nimport \"x/pkg/fake\"\n\nfunc Serve() int { return fake.Wire() }\n",
		// Каталог есть, исходники есть, слушателя не поднимает.
		"tools/lint/main.go": "package main\n\nfunc main() {}\n",
	})
	listeners, _, cand, err := DiscoverListeners(root, []string{"grpc.NewServer", "host.Serve"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listeners) != 0 {
		t.Fatalf("каталог без слушателя признан слушателем: %v", listeners)
	}
	if len(cand) == 0 {
		t.Fatalf("кандидатов рассмотрено ноль — перепись не отличит «не нашли» от «не смотрели»")
	}
	t.Logf("кандидатов %d, слушателей 0 — перепись беспредметна и обязана отказать", len(cand))
}

// TestFoundationAdoptionIgnoresMentionsInComments — контроль на НАСТОЯЩЕМ дереве.
//
// В дереве есть пара, ради которой признак читается синтаксисом: файл,
// называющий звено ПРОЗОЙ, и композиционный корень, зовущий его по-настоящему.
// Текстовый предикат не отличил бы одно от другого, и гейт зеленел бы на
// собственном объяснении — записанный класс.
//
// Производитель входа ищется ПО ДЕРЕВУ, а не выписан координатой. Выписанная
// координата — фикстура, которая переживает переименование файла молча: прежняя
// редакция этой пробы при исчезновении файла делала t.Skip, то есть весь
// контроль исчезал без следа при самой вероятной форме правки. Пропуск здесь
// запрещён: не нашли производителя — это отказ.
func TestFoundationAdoptionIgnoresMentionsInComments(t *testing.T) {
	root := repoRoot(t)
	recovery := FoundationCapability{
		Name: "восстановление после паники", Pkg: "pkg/grpcsrv",
		Unit:    FoundationUnitListener,
		Symbols: []string{"grpcsrv.UnaryPanicRecovery", "grpcsrv.StreamPanicRecovery"},
	}

	r := foundationRoster()
	sv := foundationSurveyTree(t, root, r)
	var mentionOnly, realCall []string
	files := 0
	for _, dir := range append(append([]string(nil), sv.Dirs...), "pkg") {
		err := rootedWalk(filepath.Join(root, filepath.FromSlash(dir)),
			func(rel string) bool {
				return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
			},
			func(abs string, body []byte) error {
				files++
				text := string(body)
				mentions := false
				for _, sym := range recovery.Symbols {
					if strings.Contains(text, sym[strings.Index(sym, ".")+1:]) {
						mentions = true
						break
					}
				}
				if !mentions {
					return nil
				}
				s, serr := scanGoFile(abs, body)
				if serr != nil {
					return serr
				}
				rel := strings.TrimPrefix(strings.TrimPrefix(abs, root), "/")
				if s.Direct(recovery) {
					realCall = append(realCall, rel)
				} else {
					mentionOnly = append(mentionOnly, rel)
				}
				return nil
			})
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(mentionOnly) == 0 {
		t.Fatalf("в дереве не нашлось ни одного прод-файла, который называет звено ТЕКСТОМ и не "+
			"зовёт его: производителя входа не осталось, и контроль «комментарий не есть "+
			"провязка» ничего не доказывает. Прочитано прод-файлов %d, вызывают по-настоящему %d",
			files, len(realCall))
	}
	// Положительный контроль: настоящий вызов читается. Без него «не засчитал»
	// было бы неотличимо от «не читает вовсе».
	if len(realCall) == 0 {
		t.Fatalf("настоящих вызовов звена не прочитано ни одного при %d прод-файлах: признак не "+
			"читает НИЧЕГО, и отрицание выше зеленеет на всём", files)
	}
	sort.Strings(mentionOnly)
	sort.Strings(realCall)
	t.Logf("прод-файлов прочитано %d: называют звено только текстом %d (напр. %s), зовут "+
		"по-настоящему %d (напр. %s)",
		files, len(mentionOnly), mentionOnly[0], len(realCall), realCall[0])
}

// TestFoundationGateRedensOnTheRealTreeWhenAnEntryIsRemoved — инъекция
// НАСТОЯЩИМ входом в ведомость.
//
// Синтетическое дерево доказывает механику. Оно не доказывает, что механика
// приложена к настоящему дереву верно: перечень слушателей, признаки
// возможностей и цепочка посредников — всё это могло разойтись с деревом, и
// синтетика этого не заметит, потому что построена по тем же представлениям.
//
// Поэтому эта половина берёт БОЕВОЙ набор и снимает из его ведомости ОДНУ
// запись. Ожидание точное: находка ровно одна на каждую единицу, которую запись
// прикрывала, и все они называют ту самую возможность.
func TestFoundationGateRedensOnTheRealTreeWhenAnEntryIsRemoved(t *testing.T) {
	root := repoRoot(t)
	base := foundationRoster()
	sv := foundationSurveyTree(t, root, base)

	// Снимаемая запись выбирается по СОДЕРЖАНИЮ ведомости, а не выписывается
	// координатой: выписанная устарела бы вместе с первой же правкой набора, и
	// проба падала бы на своей фикстуре вместо предмета.
	victim := base.Ledger[0]
	trimmed := base
	trimmed.Ledger = append([]FoundationLedgerEntry(nil), base.Ledger[1:]...)

	full := base.Adjudicate(sv.Dirs, sv.Scans, sv.Sites, sv.ProviderScan)
	if len(full.Findings) != 0 || len(full.Stale) != 0 {
		t.Fatalf("боевой набор не зелёный — инъекция мерила бы разницу от красного: %s", full)
	}

	cut := trimmed.Adjudicate(sv.Dirs, sv.Scans, sv.Sites, sv.ProviderScan)
	if len(cut.Findings) == 0 {
		t.Fatalf("снятие записи %q не дало ни одной находки: ведомость ничего не прикрывает, "+
			"то есть перепись зелёная независимо от неё", victim.Capability)
	}
	for _, f := range cut.Findings {
		if f.Capability != victim.Capability {
			t.Fatalf("снята запись про %q, а находка про %q — перепись отвечает не про то, "+
				"что у неё спросили", victim.Capability, f.Capability)
		}
		if victim.Listener != "" && !strings.HasPrefix(f.Listener, victim.Listener) {
			t.Fatalf("снята запись про слушателя %q, а находка про %q", victim.Listener, f.Listener)
		}
	}
	t.Logf("снята запись %q/%s (задача #%d) → находок %d, первая: %s",
		victim.Capability, listenerOrAll(victim.Listener), victim.Issue,
		len(cut.Findings), cut.Findings[0].Detail)
}
