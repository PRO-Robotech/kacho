// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogparity_test.go — гейты шва «каталог края ↔ право, которое исполняет
// сервис».
//
// ЧТО ЗДЕСЬ ИЗМЕНИЛОСЬ И ПОЧЕМУ ФАЙЛ ПЕРЕПИСАН. Прежде на один вызов было ДВА
// объявления: строка каталога, сгенерированная из аннотаций proto, и рукописная
// карта прав сервиса. Пока их два, действующим требованием оказывается их
// пересечение, и оно не записано ни в одном документе, по которому выдают права.
// Гейты этого файла сверяли объявления между собой и вели репо-широкий реестр
// расхождений.
//
// Второго объявления больше нет: карта каждого сервиса ВЫВОДИТСЯ из тех же
// аннотаций (`pkg/authz/catalogderive`). Поэтому прежние гейты потеряли предмет —
// сверять карту с каталогом стало сверкой значения с самим собой. Вместо них
// здесь стоят три утверждения о том, что в этой схеме ЕЩЁ МОЖЕТ разойтись:
//
//  1. КАТАЛОГ ПРОТИВ АННОТАЦИЙ. Каталог — закоммиченный файл, аннотации — proto.
//     Правка proto без перегенерации оставляет край исполняющим устаревшее
//     правило, а сервис — новое. Прежде это ловил только CI-таргет, требующий
//     `buf`; здесь то же утверждение доступно обычным `go test`.
//  2. ВОЗВРАТ ВТОРОГО ОБЪЯВЛЕНИЯ. Карта, собранная литералом вместо вывода,
//     возвращает ровно ту болезнь, ради которой всё делалось, и выглядит при
//     этом как обычный код.
//  3. ПОЛОСА `scope_filtered` БЕЗ ИСПОЛНИТЕЛЯ. Строка каталога с этой полосой —
//     ПОЛОЖИТЕЛЬНОЕ обещание: край перестаёт спрашивать модель, потому что
//     сужает владелец. Домен, который не провязывает выведенную карту в цепочку
//     интерсепторов, этого обещания не подтверждает ничем, что видно снаружи.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/api"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
)

// catalogEmbedPath / iamCatalogMirrorPath — две вшитые копии каталога. Первая —
// та, которую исполняет шлюз; вторая — зеркало, вшитое в iam. Гейты ниже говорят
// «каталог» в единственном числе, и это осмысленно ровно пока копии совпадают
// побайтово.
const (
	catalogEmbedPath     = "gateway/internal/middleware/embed/permission_catalog.json"
	iamCatalogMirrorPath = "services/iam/internal/apps/kacho/seed/embedded/permission_catalog.json"
)

// catalogProtoPackages — proto-пакеты, чьи RPC попадают в каталог.
//
// Перечень выписан, а не выведен, и это осознанно: вывести его можно было бы
// только из самого каталога, и тогда «в каталоге нет целого домена» стало бы
// невыразимым — гейт молча перестал бы рассматривать то, чего в файле нет.
// Появился домен — строка добавляется сюда, и до тех пор гейт краснеет на его
// строках как на незаявленных.
var catalogProtoPackages = []string{
	"kacho.cloud.iam.v1",
	"kacho.cloud.vpc.v1",
	"kacho.cloud.compute.v1",
	"kacho.cloud.storage.v1",
	"kacho.cloud.loadbalancer.v1",
	"kacho.cloud.registry.v1",
	"kacho.cloud.geo.v1",
	// Пакет ОБЩЕЙ формы ответа о квотах. Долгое время он нёс только сообщение и
	// потому в этом перечне не значился; с задачи #484 в нём объявлена служба —
	// чтение квот, носителем которых является личность, — и её аннотация обязана
	// сверяться так же, как всякая другая. Пропусти его здесь, и строка каталога
	// осталась бы без источника, а гейт назвал бы находкой сам каталог.
	"kacho.cloud.quota.v1",
	"kacho.cloud.operation",
	// Пакет ОБЩЕЙ формы подписки. До kacho#1018 он нёс только сообщения и потому
	// здесь не значился; с этой задачи в нём объявлен глагол платформы —
	// единственный на всю подписку, — и его аннотация обязана сверяться так же,
	// как всякая другая. Пропусти его здесь, и строка каталога осталась бы без
	// источника, а гейт назвал бы находкой сам каталог.
	"kacho.cloud.subscription",
}

// domainsWithoutAWiredMap — домены, у которых каталог несёт строки
// `scope_filtered`, но выведенная карта НЕ провязывается в цепочку
// интерсепторов.
//
// Ровно одна запись, и у неё есть причина, а не отговорка: iam сам является
// сервисом авторизации. Провязать ему пообъектный Check значило бы заставить его
// спрашивать самого себя на каждом вызове; проектный принципал получал бы отказ
// на собственных ресурсах — это уже случалось и записано. Его девять строк
// сужаются в хендлерах (`services/iam/internal/authzfilter`), и подтверждение
// этому — пробы того пакета, а не запись в карте.
//
// Перечень самоистекает: провязали карту — запись обязана уйти, иначе она
// объявляет исключением то, чего больше нет. Это уже сработало: здесь стояла
// вторая запись — общая форма подписки. Она была законной, пока её глагол не был
// смонтирован ни одним композиционным корнем; владельцы журналов его
// смонтировали и объявили ему проводку сужателя в своих дескрипторах, домен
// перестал быть непровязанным, и анализатор уронил прогон на записи сам.
var domainsWithoutAWiredMap = []string{
	"kacho.cloud.iam.v1",
}

// TestCatalogMatchesTheAnnotationsItWasGeneratedFrom — закоммиченный каталог
// обязан совпадать с аннотациями, из которых он порождён, по КАЖДОЙ строке и в
// обе стороны.
//
// Что ловится: правка `.proto` без `make -C gateway permission-catalog-apply`.
// Тогда край исполняет старое правило, а сервис — новое (его карта выводится из
// аннотаций), и расходятся они молча: обе стороны выглядят исправными, а
// действующим требованием становится их пересечение.
//
// Что делать, если гейт сработал, — ровно два исхода: перегенерировать каталог
// (обе копии) либо вернуть аннотацию. «Поправить JSON руками» исходом не
// является: он генерируется, и правка переживёт ровно до следующей генерации.
func TestCatalogMatchesTheAnnotationsItWasGeneratedFrom(t *testing.T) {
	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, catalogEmbedPath))
	if err != nil {
		t.Fatalf("не прочитан вшитый каталог %s: %v", catalogEmbedPath, err)
	}
	var rows []catalogderive.Entry
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("разбор каталога %s: %v", catalogEmbedPath, err)
	}
	if len(rows) == 0 {
		t.Fatalf("каталог пуст — гейт беспредметен")
	}
	byFQN := make(map[string]catalogderive.Entry, len(rows))
	for _, r := range rows {
		byFQN["/"+r.FQN] = r
	}

	seen := map[string]bool{}
	var mismatches []string
	methods := 0
	catalogderive.RangeAnnotated(catalogProtoPackages, func(fullMethod string, _ protoreflect.MethodDescriptor, a catalogderive.Annotations) {
		methods++
		seen[fullMethod] = true
		row, ok := byFQN[fullMethod]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: RPC аннотирован, но строки в каталоге нет", fullMethod))
			return
		}
		for _, d := range diffAnnotationAgainstRow(fullMethod, a, row) {
			mismatches = append(mismatches, d)
		}
	})
	if methods == 0 {
		t.Fatalf("обход аннотаций не нашёл ни одного RPC — либо стабы не слинкованы, либо перечень "+
			"пакетов (%d) назван неверно. Гейт в таком состоянии не утверждает ничего.",
			len(catalogProtoPackages))
	}
	for fqn := range byFQN {
		if !seen[fqn] {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: строка каталога есть, а RPC с такой аннотацией в дереве нет", fqn))
		}
	}
	sort.Strings(mismatches)
	for _, m := range mismatches {
		t.Errorf("каталог разошёлся с аннотациями, из которых он порождён:\n  %s\n\n"+
			"Исходы: перегенерировать каталог (`make -C gateway permission-catalog-apply` + "+
			"синхронизация копии iam) либо вернуть аннотацию. Правка JSON руками исходом не "+
			"является — он порождаемый.", m)
	}

	// Обе вшитые копии обязаны совпадать побайтово: гейты говорят «каталог» в
	// единственном числе, и при разъезде копий неясно, по какой из них выданы
	// права.
	mirror, err := os.ReadFile(filepath.Join(root, iamCatalogMirrorPath))
	if err != nil {
		t.Fatalf("не прочитано зеркало каталога %s: %v", iamCatalogMirrorPath, err)
	}
	if string(raw) != string(mirror) {
		t.Errorf("две вшитые копии каталога разошлись:\n  %s\n  %s", catalogEmbedPath, iamCatalogMirrorPath)
	}

	t.Logf("перепись: строк каталога %d, аннотированных RPC %d в %d пакетах, расхождений %d; "+
		"копий сверено 2, байт %d",
		len(rows), methods, len(catalogProtoPackages), len(mismatches), len(raw))
}

// diffAnnotationAgainstRow сверяет одну аннотацию с одной строкой каталога по
// всем осям, которые каталог несёт.
func diffAnnotationAgainstRow(fqn string, a catalogderive.Annotations, row catalogderive.Entry) []string {
	var out []string
	add := func(axis, want, got string) {
		out = append(out, fmt.Sprintf("%s: %s — аннотация %q, каталог %q", fqn, axis, want, got))
	}
	if a.Permission != row.Permission {
		add("permission", a.Permission, row.Permission)
	}
	if a.RequiredRelation != row.RequiredRelation {
		add("required_relation", a.RequiredRelation, row.RequiredRelation)
	}
	if a.ScopeObjectType != row.ScopeExtractor.ObjectType {
		add("scope_extractor.object_type", a.ScopeObjectType, row.ScopeExtractor.ObjectType)
	}
	if a.ScopeFromRequestField != row.ScopeExtractor.FromRequestField {
		add("scope_extractor.from_request_field", a.ScopeFromRequestField, row.ScopeExtractor.FromRequestField)
	}
	if a.ScopeObjectTypeFromRequest != row.ScopeExtractor.ObjectTypeFromRequestField {
		add("scope_extractor.object_type_from_request_field",
			a.ScopeObjectTypeFromRequest, row.ScopeExtractor.ObjectTypeFromRequestField)
	}
	if a.HideExistence != row.HideExistence {
		add("hide_existence", fmt.Sprint(a.HideExistence), fmt.Sprint(row.HideExistence))
	}
	if a.ScopeFiltered != row.ScopeFiltered {
		add("scope_filtered", fmt.Sprint(a.ScopeFiltered), fmt.Sprint(row.ScopeFiltered))
	}
	return out
}

// TestNoServiceDeclaresItsPermissionsASecondTime — карта прав обязана выводиться,
// а не собираться литералом.
//
// Литеральная карта возвращает то самое второе объявление: она выглядит обычным
// кодом, компилируется, проходит обзор диффа — и с этого момента действующим
// требованием снова становится пересечение двух объявлений, не записанное нигде.
//
// Что делать, если гейт сработал: собрать карту через `catalogderive.MustDerive`
// от списка proto-пакетов сервиса, а требуемое отношение объявить аннотацией
// метода в proto.
func TestNoServiceDeclaresItsPermissionsASecondTime(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	rels := make([]string, 0, tt.count())
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	fset := token.NewFileSet()
	var literals []string
	derived, scanned := 0, 0
	for _, rel := range rels {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		// Каталога документации в этом перечне нет намеренно: обход читает только
		// не-тестовые `.go`, а их под `*/docs/` ноль. Стоявшее здесь исключение
		// `docs-site/` было к тому же дано префиксом от корня репозитория, где
		// каталога с таким именем не существовало никогда, — оно не исключало
		// ничего ни одного дня. Исключение живёт, пока у него есть предмет.
		if strings.HasPrefix(rel, "pkg/api/") || strings.HasPrefix(rel, "pkg/authz/catalogderive/") ||
			strings.HasPrefix(rel, "vendor/") || strings.HasPrefix(rel, "ui-future/") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, 0)
		if perr != nil {
			continue
		}
		scanned++
		lits, calls := inspectRPCMapConstruction(f)
		derived += calls
		for range lits {
			literals = append(literals, rel)
		}
	}

	if scanned == 0 {
		t.Fatalf("обход не прочитал ни одного не-тестового файла — гейт не утверждает ничего")
	}
	// Положительный контроль: вывод обязан быть НАЙДЕН. Ноль найденных вызовов
	// означает, что распознавание сломалось, и тогда «ноль литералов» — не
	// свойство дерева, а слепота гейта.
	if derived == 0 {
		t.Fatalf("ни одного вывода карты (`catalogderive.Derive`/`MustDerive`) не найдено в %d "+
			"файлах — распознавание сломано либо вывод сняли; в обоих случаях молчание про "+
			"литералы ничего не значит", scanned)
	}

	slices.Sort(literals)
	literals = slices.Compact(literals)
	for _, rel := range literals {
		t.Errorf("карта прав собрана литералом, а не выведена из аннотаций:\n  %s\n\n"+
			"Это второе объявление того же права: с ним действующим требованием снова "+
			"становится пересечение карты и каталога, а пересечение не записано ни в одном "+
			"документе, по которому выдают права. Собери карту через "+
			"`catalogderive.MustDerive(protoPackages...)`, а отношение объяви аннотацией метода.", rel)
	}

	// Перепись печатает объём ПО КАЖДОЙ ФОРМЕ отдельно (#1813). Одно число
	// скрыло бы ровно тот случай, ради которого вторая форма заведена: «файлов
	// Go прочитано много, находок ноль» читалось бы как вердикт и о манифестах,
	// тогда как YAML этот обход не читает НИ ПРИ КАКОМ УСЛОВИИ — его читает
	// TestManifestIsNotASecondDeclarationOfARight, и сегодня манифестов в дереве
	// ноль, то есть у второй формы предмета нет вовсе (третья категория).
	manifests := 0
	for rel := range tt.files {
		if filepath.Base(rel) == manifestBaseName {
			manifests++
		}
	}
	t.Logf("перепись: прочитано не-тестовых файлов Go %d · манифестов %d, "+
		"выводов карты %d, литеральных карт %d",
		scanned, manifests, derived, len(literals))
	if manifests == 0 {
		t.Logf("манифестов НОЛЬ — форма YAML предмета не имела: это третья категория " +
			"(«не с чем сверять»), и в «находок ноль» она НЕ засчитывается")
	}
}

// inspectRPCMapConstruction — сколько в файле литералов `authz.RPCMap{…}` с
// записями и сколько вызовов вывода.
//
// Пустой литерал (`authz.RPCMap{}`) находкой НЕ считается: им пользуются
// фикстуры и конструкторы, и он ничего не объявляет о правах.
func inspectRPCMapConstruction(f *ast.File) (literals int, deriveCalls int) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			sel, ok := node.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "RPCMap" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "authz" && len(node.Elts) > 0 {
				literals++
			}
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil {
				return true
			}
			if sel.Sel.Name != "Derive" && sel.Sel.Name != "MustDerive" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "catalogderive" {
				deriveCalls++
			}
		}
		return true
	})
	return literals, deriveCalls
}

// TestScopeFilteredRowsBelongToADomainThatEnforcesThem — полоса `scope_filtered`
// снимает вопрос края на ПОЛОЖИТЕЛЬНОМ обещании: сужает владелец. Домен,
// который не провязывает свою карту в цепочку интерсепторов, этого обещания
// ничем видимым не подтверждает.
//
// Что делать, если гейт сработал, — ровно три исхода:
//
//  1. домен действительно сужает, но карта не провязана -> провязать её
//     (`check.NewInterceptor` в композиционном корне);
//  2. домен НЕ сужает -> снять `scope_filtered` с аннотации и перегенерировать
//     каталог: край перестал спрашивать модель под обещание, которого нет;
//  3. провязать нельзя по существу -> внести домен в domainsWithoutAWiredMap с
//     разбором, чем именно держится сужение.
func TestScopeFilteredRowsBelongToADomainThatEnforcesThem(t *testing.T) {
	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, catalogEmbedPath))
	if err != nil {
		t.Fatalf("не прочитан вшитый каталог %s: %v", catalogEmbedPath, err)
	}
	var rows []catalogderive.Entry
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("разбор каталога %s: %v", catalogEmbedPath, err)
	}

	scopeFilteredByPackage := map[string]int{}
	for _, r := range rows {
		if !r.ScopeFiltered {
			continue
		}
		scopeFilteredByPackage[protoPackageOfFQN(r.FQN)]++
	}
	if len(scopeFilteredByPackage) == 0 {
		t.Fatalf("в каталоге нет ни одной строки `scope_filtered` — гейт беспредметен. Либо полоса "+
			"снята (тогда удали и его, и %s), либо изменился ключ поля.", "domainsWithoutAWiredMap")
	}

	wired := wiredMapPackages(t, root)
	if len(wired) == 0 {
		t.Fatalf("не найдено ни одного домена с провязанной картой — распознавание сломано; " +
			"иначе гейт объявил бы необеспеченными ВСЕ строки полосы, что было бы неверно")
	}

	var unwired []string
	for pkg := range scopeFilteredByPackage {
		if !wired[pkg] {
			unwired = append(unwired, pkg)
		}
	}
	sort.Strings(unwired)

	pinned := slices.Clone(domainsWithoutAWiredMap)
	sort.Strings(pinned)

	for _, pkg := range unwired {
		if !slices.Contains(pinned, pkg) {
			t.Errorf("каталог обещает `scope_filtered` в домене %s (%d строк), но выведенная карта "+
				"этого домена не провязана ни в одну цепочку интерсепторов:\n\n"+
				"Край на этом обещании ПЕРЕСТАЁТ спрашивать модель. Исходы: провязать карту / "+
				"снять полосу с аннотации и перегенерировать каталог / внести домен в "+
				"domainsWithoutAWiredMap с разбором.", pkg, scopeFilteredByPackage[pkg])
		}
	}
	for _, pkg := range pinned {
		if !slices.Contains(unwired, pkg) {
			t.Errorf("запись пережила свой предмет — домен %s больше не является непровязанным:\n\n"+
				"Удали его из domainsWithoutAWiredMap, иначе перечень объявляет исключением то, "+
				"чего нет, и слепую зону унаследует следующий.", pkg)
		}
	}

	t.Logf("перепись: строк каталога %d, из них scope_filtered %d в %d доменах; доменов с "+
		"провязанной картой %d; непровязанных с полосой %d (внесено с разбором %d)",
		len(rows), countScopeFiltered(rows), len(scopeFilteredByPackage), len(wired),
		len(unwired), len(pinned))
}

func countScopeFiltered(rows []catalogderive.Entry) int {
	n := 0
	for _, r := range rows {
		if r.ScopeFiltered {
			n++
		}
	}
	return n
}

// protoPackageOfFQN — "kacho.cloud.vpc.v1.NetworkService/List" → "kacho.cloud.vpc.v1".
func protoPackageOfFQN(fqn string) string {
	svc := fqn
	if i := strings.Index(fqn, "/"); i >= 0 {
		svc = fqn[:i]
	}
	if i := strings.LastIndex(svc, "."); i >= 0 {
		return svc[:i]
	}
	return svc
}

// wiredMapPackages — proto-пакеты, чью выведенную карту сервис действительно
// вручает звену решения о доступе.
//
// Признак — не «в дереве есть файл с картой», а «карта попадает в
// authz.InterceptorOptions». Первое переживает снятие проводки и осталось бы
// зелёным ровно там, где защита выключена; второе — нет.
//
// # Две законные формы, и обе читаются
//
// (а) СОБСТВЕННАЯ фабрика звена: пакет объявляет `protoPackages`, выводит по ним
// карту и вручает её `authz.InterceptorOptions{Map: …}` в том же каталоге.
//
// (б) НОСИТЕЛЬ контура (`pkg/servicehost`): композиционный корень отдаёт ему оба
// регистратора, а карту носитель выводит САМ — из имён служб, снятых у серверов
// после регистрации, — и сам же вручает её звену. Собственного объявления
// `protoPackages` у такого сервиса нет и быть не должно: второй источник домена
// как раз и убирался. Признать эту форму обязательно — иначе гейт объявил бы
// необеспеченной полосу сервиса, у которого сужение вдобавок ПРОВЕРЯЕТСЯ отказом
// старта (О3: каталог объявил метод сужаемым, а проводки сужателя в дескрипторе
// нет ⇒ процесс не поднимается), то есть держится строже, чем в форме (а).
//
// Домены формы (б) выводятся из ЗАРЕГИСТРИРОВАННЫХ служб (`…Register<X>Server`), а
// не из импортов: импорт стабов бывает и у клиента соседа (storage импортирует
// стабы iam, чтобы его СПРАШИВАТЬ), и по импортам домен iam оказался бы «провязан»
// чужим сервисом — то есть исключение, у которого предмет есть, объявилось бы
// истёкшим.
func wiredMapPackages(t *testing.T, root string) map[string]bool {
	t.Helper()

	tt := newTrackedTree(t, root)
	rels := make([]string, 0, tt.count())
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	fset := token.NewFileSet()
	// Пакет каталога (директория) → объявленные им proto-пакеты.
	declared := map[string][]string{}
	wiredDirs := map[string]bool{}
	// Форма (б): каталог → домены зарегистрированных им служб, и признак того,
	// что этот каталог отдаёт регистраторы носителю.
	registered := map[string][]string{}
	carrierDirs := map[string]bool{}

	for _, rel := range rels {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") ||
			!strings.HasPrefix(rel, "services/") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, 0)
		if perr != nil {
			continue
		}
		dir := filepath.Dir(rel)
		if pkgs := declaredProtoPackages(f); len(pkgs) > 0 {
			declared[dir] = append(declared[dir], pkgs...)
		}
		if mapHandedToInterceptor(f) {
			wiredDirs[dir] = true
		}
		if handsRegistrarsToTheCarrier(f) {
			carrierDirs[dir] = true
		}
		if pkgs := registeredProtoPackages(f); len(pkgs) > 0 {
			registered[dir] = append(registered[dir], pkgs...)
		}
	}

	out := map[string]bool{}
	for dir, pkgs := range declared {
		if !wiredDirs[dir] {
			continue
		}
		for _, p := range pkgs {
			out[p] = true
		}
	}
	for dir, pkgs := range registered {
		if !carrierDirs[dir] {
			continue
		}
		for _, p := range pkgs {
			out[p] = true
		}
	}
	return out
}

// handsRegistrarsToTheCarrier — файл отдаёт слушатели носителю контура.
func handsRegistrarsToTheCarrier(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Serve" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "servicehost" {
			found = true
		}
		return true
	})
	return found
}

// apiStubImportPrefix — путь сгенерённых стабов. Домен восстанавливается из
// хвоста пути заменой разделителя: `kacho/cloud/storage/v1` → `kacho.cloud.storage.v1`.
const apiStubImportPrefix = "github.com/PRO-Robotech/kacho/pkg/api/"

// registeredProtoPackages — домены служб, которые файл РЕГИСТРИРУЕТ на сервере.
//
// Единица — вызов `<стабы>.Register…Server(...)`: служить RPC и не
// зарегистрировать его невозможно, это одна операция. Импорт стабов таким
// признаком не является — его делает и клиент соседа.
func registeredProtoPackages(f *ast.File) []string {
	local := map[string]string{} // локальное имя пакета → proto-пакет
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.HasPrefix(path, apiStubImportPrefix) {
			continue
		}
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		local[name] = strings.ReplaceAll(strings.TrimPrefix(path, apiStubImportPrefix), "/", ".")
	}
	if len(local) == 0 {
		return nil
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil ||
			!strings.HasPrefix(sel.Sel.Name, "Register") || !strings.HasSuffix(sel.Sel.Name, "Server") {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok {
			if proto, known := local[pkg.Name]; known {
				out = append(out, proto)
			}
		}
		return true
	})
	return out
}

// declaredProtoPackages — строковые элементы объявления `protoPackages`.
func declaredProtoPackages(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if name.Name != "protoPackages" || i >= len(vs.Values) {
				continue
			}
			lit, ok := vs.Values[i].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, el := range lit.Elts {
				if bl, ok := el.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					out = append(out, strings.Trim(bl.Value, `"`))
				}
			}
		}
		return true
	})
	return out
}

// mapHandedToInterceptor — файл передаёт карту в `authz.InterceptorOptions{Map: …}`.
func mapHandedToInterceptor(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "InterceptorOptions" {
			return true
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Map" {
				found = true
			}
		}
		return true
	})
	return found
}
