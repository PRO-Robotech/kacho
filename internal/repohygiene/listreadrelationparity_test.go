// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listreadrelationparity_test.go — гейт против списочного предиката, дизъюнктного
// с отношением чтения.
//
// # Что охраняется
//
// Публичный List читает страницу курсором из своей БД и спрашивает модель, какие
// id этой страницы видны вызывающему. Отношение, которым задан ЭТОТ вопрос, обязано
// совпадать с отношением, которым per-RPC Check гейтит одиночный Get того же
// ресурса. Пока они расходятся, объект попадает в страницу по одному отношению, а
// читается по другому — и вызывающий узнаёт о существовании объекта, которого не
// вправе читать. Это не только оракул существования: List возвращает ТО ЖЕ
// сообщение, что и Get, поэтому членство в странице раскрывает содержимое целиком.
//
// Расхождение измерено, а не предположено (ревизия bdafe2c4): три сервиса
// спрашивали страницу союзом `viewer ∪ v_list`, а Get гейтили `v_get`. В модели
// это РАЗНЫЕ множества — ярусные (`viewer`/`editor`/`admin`) и глагольные (`v_*`)
// отношения развязаны намеренно, как анти-over-grant guard (fga_model.fga,
// миграция iam 0040), — поэтому расхождение работало в обе стороны: держатель
// яруса без глагола видел чужое содержимое, держатель глагола без яруса не находил
// собственный читаемый ресурс в своём же списке.
//
// # Почему предмет берётся из КАТАЛОГА
//
// Отношение чтения не выписывается здесь рукой: рукописный список расходится с
// деревом — это свойство механизма, а не аккуратности автора. Оно читается из
// сгенерированного каталога прав (той же копии, которую исполняет шлюз), по записи
// `<Service>/Get` того ресурса. Сменится гейт чтения — гейт увидит это сам.
//
// Перечень сервисов тоже ВЫВОДИТСЯ: под гейт попадает всякий, кто объявил
// `visibilityRelations`. Новый сервис с пообъектным сужением списка охватывается по
// построению, а не после того, как кто-то вспомнит дописать его в перечень.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// visibilityVarName — имя переменной, в которой сервис объявляет предикат членства
// страницы. Имя — часть ПРЕДПОСЫЛКИ гейта: переименуют её, и осматривать станет
// нечего (см. TestListReadRelationParity_PremiseHolds).
const visibilityVarName = "visibilityRelations"

// clusterScopeObjectType — якорь глобального справочника. Записи каталога с этим
// scope'ом из сверки исключены: это не тенантские объекты с индивидуальными
// владельцами, их List гейтится на уровне кластера и пообъектным сужением не
// проходит вовсе (`MachineTypeService` — project-scope EXEMPT). Исключение
// проверяемо: TestListReadRelationParity_PremiseHolds требует, чтобы у него был
// предмет, иначе запись переживёт то, ради чего написана.
const clusterScopeObjectType = "cluster"

// catalogFqnDomainRe вытаскивает домен из полного имени метода каталога
// (`kacho.cloud.<domain>.v1.<Service>/<Method>`).
var catalogFqnDomainRe = regexp.MustCompile(`^kacho\.cloud\.([a-z0-9]+)\.v1\.([A-Za-z0-9]+)/([A-Za-z0-9]+)$`)

// permissionMapDomainRe вытаскивает домен из ключа карты прав сервиса
// (`/kacho.cloud.<domain>.v1.<Service>/<Method>`).
var permissionMapDomainRe = regexp.MustCompile(`/kacho\.cloud\.([a-z0-9]+)\.v1\.`)

// catalogEntry — запись сгенерированного каталога прав.
type catalogEntry struct {
	FQN              string `json:"fqn"`
	RequiredRelation string `json:"required_relation"`
	ScopeExtractor   struct {
		ObjectType string `json:"object_type"`
	} `json:"scope_extractor"`
}

// listFilterService — сервис, объявивший пообъектное сужение списка.
type listFilterService struct {
	name       string   // каталог в services/
	filterFile string   // путь к объявлению, для координаты в отказе
	pageRels   []string // предикат членства страницы
	domain     string   // домен proto, выведенный из карты прав сервиса
}

// readRelationsOfDomain — отношения, которыми каталог гейтит ПООБЪЕКТНОЕ чтение
// ресурсов домена, вместе с методами, из которых они взяты.
type readRelationsOfDomain struct {
	relations map[string][]string // отношение → методы, его объявившие
	scanned   int                 // сколько записей `/Get` домена осмотрено
}

// parityFinding — одно расхождение.
type parityFinding struct {
	service   string
	file      string
	pageRels  []string
	readRels  map[string][]string
	forDomain string
}

func (f parityFinding) String() string {
	var reads []string
	for rel, methods := range f.readRels {
		sort.Strings(methods)
		shown := methods
		if len(shown) > 3 {
			shown = append(append([]string{}, shown[:3]...), fmt.Sprintf("…+%d", len(methods)-3))
		}
		reads = append(reads, fmt.Sprintf("%s (%s)", rel, strings.Join(shown, ", ")))
	}
	sort.Strings(reads)
	return fmt.Sprintf(
		"%s: страница спрашивается {%s}, а чтение домена %q гейтится {%s}\n"+
			"  объявление: %s\n"+
			"  следствие: объект попадает в страницу по одному отношению, а читается по другому — "+
			"вызывающий узнаёт о существовании объекта, который ему не отдаст Get, и получает его содержимое (List возвращает то же сообщение)",
		f.service, strings.Join(f.pageRels, ", "), f.forDomain, strings.Join(reads, "; "), f.file)
}

// findParityViolations — ЧИСТОЕ ядро гейта: сверяет предикат страницы с отношениями
// чтения. Вынесено отдельно, чтобы инъекция проверяла ровно то, что исполняется на
// дереве, а не его пересказ (TestListReadRelationParity_GateDiscriminates).
func findParityViolations(services []listFilterService, reads map[string]readRelationsOfDomain) []parityFinding {
	var out []parityFinding
	for _, s := range services {
		r, ok := reads[s.domain]
		if !ok || len(r.relations) == 0 {
			continue // домен без пообъектного чтения — сверять не с чем (перепись это назовёт)
		}
		diverged := false
		for _, pr := range s.pageRels {
			if _, hit := r.relations[pr]; !hit {
				diverged = true
			}
		}
		// Обратная сторона: отношение чтения, которого страница не спрашивает,
		// делает собственный читаемый ресурс невидимым в своём же списке.
		for rel := range r.relations {
			found := false
			for _, pr := range s.pageRels {
				if pr == rel {
					found = true
				}
			}
			if !found {
				diverged = true
			}
		}
		if diverged {
			out = append(out, parityFinding{
				service: s.name, file: s.filterFile, pageRels: s.pageRels,
				readRels: r.relations, forDomain: s.domain,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].service < out[j].service })
	return out
}

// discoverListFilterServices обходит services/ и собирает тех, кто объявил предикат
// членства страницы.
func discoverListFilterServices(t *testing.T, root string) []listFilterService {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatalf("read services/: %v", err)
	}
	var out []listFilterService
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		svcDir := filepath.Join(root, "services", e.Name())
		var found *listFilterService
		_ = filepath.WalkDir(svcDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || found != nil {
				return nil //nolint:nilerr // недоступный подкаталог не должен ронять обход
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rels, ok := parseVisibilityRelations(path)
			if !ok {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			found = &listFilterService{name: e.Name(), filterFile: rel, pageRels: rels}
			return nil
		})
		if found == nil {
			continue
		}
		found.domain = deriveServiceDomain(svcDir)
		out = append(out, *found)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// parseVisibilityRelations разбирает `var visibilityRelations = [...]string{…}`
// РАЗБОРОМ AST, а не текстом: строковый литерал в комментарии или в соседнем тесте
// не должен ни находиться, ни маскировать объявление.
func parseVisibilityRelations(path string) ([]string, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, false
	}
	var out []string
	var ok bool
	ast.Inspect(f, func(n ast.Node) bool {
		vs, is := n.(*ast.ValueSpec)
		if !is {
			return true
		}
		for i, name := range vs.Names {
			if name.Name != visibilityVarName || i >= len(vs.Values) {
				continue
			}
			lit, is := vs.Values[i].(*ast.CompositeLit)
			if !is {
				continue
			}
			ok = true
			for _, el := range lit.Elts {
				bl, is := el.(*ast.BasicLit)
				if !is || bl.Kind != token.STRING {
					continue
				}
				if s, err := strconv.Unquote(bl.Value); err == nil {
					out = append(out, s)
				}
			}
		}
		return true
	})
	sort.Strings(out)
	return out, ok
}

// deriveServiceDomain выводит домен proto из карты прав сервиса — того файла, где
// перечислены его собственные RPC. Догадка по имени каталога здесь не годится:
// `services/nlb` обслуживает домен `loadbalancer`.
func deriveServiceDomain(svcDir string) string {
	counts := map[string]int{}
	_ = filepath.WalkDir(svcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "permission_map.go" {
			return nil //nolint:nilerr // недоступный подкаталог не должен ронять обход
		}
		b, rerr := os.ReadFile(path) //nolint:gosec // путь получен обходом дерева репозитория
		if rerr != nil {
			return nil
		}
		for _, m := range permissionMapDomainRe.FindAllStringSubmatch(string(b), -1) {
			counts[m[1]]++
		}
		return nil
	})
	best, bestN := "", 0
	for dom, n := range counts {
		if n > bestN || (n == bestN && dom < best) {
			best, bestN = dom, n
		}
	}
	return best
}

// loadReadRelations читает из каталога отношения ПООБЪЕКТНОГО чтения по доменам.
func loadReadRelations(t *testing.T, root string) map[string]readRelationsOfDomain {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, catalogEmbedPath))
	if err != nil {
		t.Fatalf("прочитать каталог %s: %v", catalogEmbedPath, err)
	}
	var entries []catalogEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("разобрать каталог %s: %v", catalogEmbedPath, err)
	}
	if len(entries) == 0 {
		t.Fatalf("каталог %s пуст — сверять не с чем", catalogEmbedPath)
	}
	out := map[string]readRelationsOfDomain{}
	for _, e := range entries {
		m := catalogFqnDomainRe.FindStringSubmatch(e.FQN)
		if m == nil || m[3] != "Get" || strings.HasPrefix(m[2], "Internal") {
			continue
		}
		ot := e.ScopeExtractor.ObjectType
		if ot == "" || ot == clusterScopeObjectType || e.RequiredRelation == "" {
			continue
		}
		dom := m[1]
		cur, ok := out[dom]
		if !ok {
			cur = readRelationsOfDomain{relations: map[string][]string{}}
		}
		cur.relations[e.RequiredRelation] = append(cur.relations[e.RequiredRelation], m[2])
		cur.scanned++
		out[dom] = cur
	}
	return out
}

// TestListReadRelationParity — сам гейт.
func TestListReadRelationParity(t *testing.T) {
	root := repoRoot(t)
	services := discoverListFilterServices(t, root)
	reads := loadReadRelations(t, root)

	// Перепись — ОТДЕЛЬНОЕ утверждение: «ноль расхождений» обязано быть отличимо
	// от «ноль осмотренного».
	var joined int
	for _, s := range services {
		if r, ok := reads[s.domain]; ok {
			joined += r.scanned
		}
		t.Logf("осмотрено: %-9s домен=%-13s страница={%s} объявление=%s",
			s.name, s.domain, strings.Join(s.pageRels, ", "), s.filterFile)
	}
	t.Logf("перепись: сервисов с пообъектным сужением списка = %d; записей `/Get` каталога, с которыми была сверка = %d",
		len(services), joined)

	if findings := findParityViolations(services, reads); len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "предикат списка разошёлся с отношением чтения (%d):\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(&b, "\n%s\n", f)
		}
		b.WriteString("\nЧТО ДЕЛАТЬ: сузить предикат страницы до отношения, которым гейтится Get того же ресурса " +
			"(`visibilityRelations`), либо — если список ШИРЕ чтения намеренно — записать это решением с обоснованием " +
			"и привести List к выдаче, которая содержимого не раскрывает. Расширять чтение под список — неверное направление.")
		t.Error(b.String())
	}
}

// TestListReadRelationParity_PremiseHolds — гейт проверяет СВОЮ предпосылку.
//
// Каждый запрет опирается на факт о дереве. Факт меняется — запрет тихо становится
// ложью, и первым это заметит не тот, кто должен.
func TestListReadRelationParity_PremiseHolds(t *testing.T) {
	root := repoRoot(t)
	services := discoverListFilterServices(t, root)

	if len(services) == 0 {
		t.Fatalf("ни один сервис не объявляет %q — гейт осматривает пустоту. "+
			"Либо переменную переименовали (почини имя здесь), либо пообъектное сужение списка снято отовсюду "+
			"(тогда гейт больше не нужен)", visibilityVarName)
	}
	for _, s := range services {
		if s.domain == "" {
			t.Errorf("%s: домен не выведен из карты прав — сверять с каталогом нечем "+
				"(нет permission_map.go либо ключи не в форме /kacho.cloud.<domain>.v1.…)", s.name)
		}
		if len(s.pageRels) == 0 {
			t.Errorf("%s: %s объявлен, но пуст — предикат страницы, не спрашивающий ничего, "+
				"это не сужение, а либо всё, либо ничего", s.name, visibilityVarName)
		}
	}

	reads := loadReadRelations(t, root)
	for _, s := range services {
		if r, ok := reads[s.domain]; !ok || r.scanned == 0 {
			t.Errorf("%s: в каталоге нет ни одной пообъектной записи `/Get` домена %q — "+
				"сверка для этого сервиса ПУСТА, и его расхождение гейт бы не увидел", s.name, s.domain)
		}
	}

	// Исключение живёт, пока у него есть предмет: cluster-scoped чтение
	// исключается из сверки, и в каталоге такие записи обязаны быть — иначе
	// исключение молча унаследует слепое пятно.
	b, err := os.ReadFile(filepath.Join(root, catalogEmbedPath))
	if err != nil {
		t.Fatalf("прочитать каталог: %v", err)
	}
	var entries []catalogEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("разобрать каталог: %v", err)
	}
	var clusterGets int
	for _, e := range entries {
		m := catalogFqnDomainRe.FindStringSubmatch(e.FQN)
		if m != nil && m[3] == "Get" && e.ScopeExtractor.ObjectType == clusterScopeObjectType {
			clusterGets++
		}
	}
	if clusterGets == 0 {
		t.Errorf("исключение %q больше нечего исключать: в каталоге нет ни одной записи `/Get` с этим scope'ом. "+
			"Запись, пережившая свой предмет, — находка: сними её, иначе она унаследует чужое слепое пятно",
			clusterScopeObjectType)
	}
}

// TestListReadRelationParity_GateDiscriminates — инъекция в ОБЕ стороны.
//
// Гейт, который только срабатывает, ничего не говорит о том, что он пропускает.
// Здесь ядро гейта прогоняется на синтетических входах: настоящее расхождение
// обязано быть НАЗВАНО вместе с координатой, а законный близнец той же формы —
// пройти молча.
func TestListReadRelationParity_GateDiscriminates(t *testing.T) {
	reads := map[string]readRelationsOfDomain{
		"vpc": {relations: map[string][]string{"v_get": {"SubnetService", "NetworkService"}}, scanned: 2},
		"storage": {relations: map[string][]string{
			"viewer": {"VolumeService"}}, scanned: 1},
	}

	t.Run("расхождение краснеет и называет координату", func(t *testing.T) {
		got := findParityViolations([]listFilterService{{
			name: "vpc", filterFile: "services/vpc/internal/authzfilter/filter.go",
			pageRels: []string{"v_list", "viewer"}, domain: "vpc",
		}}, reads)
		if len(got) != 1 {
			t.Fatalf("расхождение {viewer,v_list} против чтения {v_get} обязано быть находкой; findings=%d", len(got))
		}
		msg := got[0].String()
		for _, want := range []string{"services/vpc/internal/authzfilter/filter.go", "v_get", "viewer", "v_list"} {
			if !strings.Contains(msg, want) {
				t.Errorf("в отказе нет %q — по такому сообщению не найти, что чинить:\n%s", want, msg)
			}
		}
	})

	t.Run("законный близнец той же формы молчит", func(t *testing.T) {
		// Та же форма (сервис + предикат + домен), но предикат СОВПАДАЕТ с чтением.
		// Без этой половины гейт ловил бы форму, а не существо.
		if got := findParityViolations([]listFilterService{{
			name: "vpc", filterFile: "services/vpc/internal/authzfilter/filter.go",
			pageRels: []string{"v_get"}, domain: "vpc",
		}}, reads); len(got) != 0 {
			t.Errorf("совпадающий предикат не может быть находкой; получено: %v", got)
		}
		if got := findParityViolations([]listFilterService{{
			name: "storage", filterFile: "services/storage/internal/authzfilter/filter.go",
			pageRels: []string{"viewer"}, domain: "storage",
		}}, reads); len(got) != 0 {
			t.Errorf("сервис, чьё чтение гейтится ярусом, при совпадающем предикате обязан молчать; получено: %v", got)
		}
	})

	t.Run("недостача тоже находка", func(t *testing.T) {
		// Обратная сторона: страница УЖЕ чтения — собственный читаемый ресурс
		// не виден в своём же списке. Одно направление проверять нельзя.
		if got := findParityViolations([]listFilterService{{
			name: "vpc", filterFile: "f.go", pageRels: []string{}, domain: "vpc",
		}}, reads); len(got) != 1 {
			t.Errorf("пустой предикат при непустом чтении обязан быть находкой; findings=%d", len(got))
		}
	})
}
