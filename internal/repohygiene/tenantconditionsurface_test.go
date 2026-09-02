// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tenantconditionsurface_test.go — тенантская поверхность условного доступа снята
// и не возвращается по частям.
//
// Под словом «условие» в дереве живут ДВЕ разные вещи, и их легко перепутать:
//
//   - условие на кортеже — `condition …` в модели прав. Ключ условия сервер
//     берёт сам, из запроса он вырезается. Это ЖИВОЕ;
//
//     Здесь же стояла вторая координата живого — `Tuple.condition` на внутреннем
//     листенере. Её больше нет: служба администрирования внешнего движка прав
//     снята целиком стадией S6 (kacho#747) вместе со своим файлом контракта, а с
//     ней ушли `TupleCondition` и единственный читатель `BuiltinCondition`.
//     Положительная половина сузилась до модели — и это НЕ ослабление гейта, а
//     истечение фикстуры вместе с её предметом: утверждать «TupleCondition на
//     месте» о файле, которого нет, значит краснеть на достижении чужой цели;
//
//   - тенантская поверхность — ресурс условия со своим сервисом, наложение на
//     привязку и подстрочный вычислитель. Это снято.
//
// Поэтому гейт состоит из ДВУХ половин, и одна без другой ничего не значит:
// отрицательная (снятого нет) и положительная (живое на месте, с точным числом).
// Одиночное «условий не осталось» зеленело бы сильнее всего ровно тогда, когда
// снесено и живое тоже.
//
// Предпосылка гейта проверяется отдельно: каждый читаемый файл обязан не только
// существовать, но и нести опознавательный маркер своего жанра. Иначе «записей не
// найдено» становится неотличимо от «я читаю не тот файл», и оба исхода дают один
// и тот же зелёный. По той же причине гейт печатает ПЕРЕПИСЬ — сколько файлов он
// прочитал: ноль находок обязано быть отличимо от нуля прочитанного.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ── что снято ────────────────────────────────────────────────────────────────

// retiredPaths — файлы и каталоги, существовавшие ТОЛЬКО ради снятой поверхности.
// Путь, вернувшийся в дерево, — возврат поверхности, а не «похожий файл».
var retiredPaths = []string{
	"proto/kacho/cloud/iam/v1/condition.proto",
	"proto/kacho/cloud/iam/v1/conditions_service.proto",
	"proto/kacho/cloud/iam/v1/access_binding_condition.proto",
	"pkg/api/kacho/cloud/iam/v1/condition.pb.go",
	"pkg/api/kacho/cloud/iam/v1/access_binding_condition.pb.go",
	"pkg/api/kacho/cloud/iam/v1/conditions_service.pb.go",
	"pkg/api/kacho/cloud/iam/v1/conditions_service.pb.gw.go",
	"pkg/api/kacho/cloud/iam/v1/conditions_service_grpc.pb.go",
	"services/iam/internal/apps/kacho/api/conditions",
	"services/iam/internal/repo/kacho/condition",
	"services/iam/internal/repo/kacho/pg/conditions_repo.go",
	"services/iam/internal/service/conditions_crud_service.go",
	"services/iam/internal/service/conditions_evaluator.go",
	"services/iam/internal/service/conditions_audit.go",
	"services/iam/internal/domain/condition.go",
	"services/iam/internal/domain/access_binding_condition.go",
	"services/iam/tests/newman/cases/iam-condition.py",
	"services/iam/tests/newman/collections/iam-condition.postman_collection.json",
	"services/iam/docs/engineering/components/09-conditions.md",
	"services/iam/tools/auditlistfilter/exclusion_expiry_test.go",
}

// surfaceFile — файл, в котором снятая поверхность была бы объявлена, и маркер,
// доказывающий, что читается именно он.
type surfaceFile struct {
	path    string
	genre   string // маркер жанра — присутствует независимо от снятия
	forbid  string // подстрока, которой после снятия быть не должно
	whatFor string
}

var surfaceFiles = []surfaceFile{
	{
		path:    "gateway/internal/middleware/rest_route_table_gen.go",
		genre:   "kacho.cloud.iam.v1.AccessBindingService/",
		forbid:  "ConditionsService/",
		whatFor: "таблица REST-маршрутов края",
	},
	{
		path:    "gateway/internal/allowlist/list.go",
		genre:   "/kacho.cloud.iam.v1.AccessBindingService/",
		forbid:  "ConditionsService/",
		whatFor: "список обхода края",
	},
	{
		path:    "gateway/internal/middleware/embed/permission_catalog.json",
		genre:   "kacho.cloud.iam.v1.AccessBindingService/",
		forbid:  "ConditionsService/",
		whatFor: "каталог прав, вшитый в край",
	},
	{
		path:    "services/iam/internal/apps/kacho/seed/embedded/permission_catalog.json",
		genre:   "kacho.cloud.iam.v1.AccessBindingService/",
		forbid:  "ConditionsService/",
		whatFor: "каталог прав, вшитый в iam (вторая копия)",
	},
	{
		path:    "gateway/internal/restmux/mux.go",
		genre:   "RegisterAccessBindingServiceHandlerFromEndpoint",
		forbid:  "RegisterConditionsServiceHandlerFromEndpoint",
		whatFor: "регистрация REST-мультиплексора",
	},
	{
		// Карта типов прав ПОРОЖДАЕТСЯ из манифестов модулей (#1092) и лежит в
		// порождённом файле; координата переехала вместе с ней. Маркер жанра —
		// живой тип этой же карты, поэтому предпосылка отказывает, если карта
		// переедет снова.
		path:    "services/iam/internal/authzmap/tables_gen.go",
		genre:   "iam_access_binding",
		forbid:  "iam_condition",
		whatFor: "карта типов прав (порождается из манифестов)",
	},
	{
		path:    "services/iam/internal/authzcascade/authzcascade.go",
		genre:   "iam_access_binding",
		forbid:  "iam_condition",
		whatFor: "каскад разрешения объекта в аккаунт",
	},
	{
		path:    "services/iam/tools/audit-list-filter.sh",
		genre:   "--root=",
		forbid:  "--allow=conditions",
		whatFor: "прогонщик гейта сужения списка",
	},
	{
		path:    "services/iam/internal/dto/toproto/access_binding.go",
		genre:   "ScopeType",
		forbid:  "ConditionId",
		whatFor: "проекция привязки в ответ",
	},
	{
		path:    "services/iam/internal/domain/access_binding.go",
		genre:   "ExpiresAt",
		forbid:  "ConditionID",
		whatFor: "доменная привязка",
	},
	{
		path:    "services/iam/internal/repo/kacho/pg/access_binding_repo.go",
		genre:   "granted_by_user_id",
		forbid:  "condition_id",
		whatFor: "хранилище привязки",
	},
	{
		path:    "services/iam/tests/newman/scripts/run.sh",
		genre:   "run_one",
		forbid:  "iam-condition",
		whatFor: "прогонщик e2e-набора iam",
	},
}

// ── что остаётся живым ───────────────────────────────────────────────────────

// liveConditionDefs / liveMFARestrictions — точные числа живой половины на ревизии
// снятия. Числа, а не «есть хоть что-то»: снос живого дал бы ноль, и одно только
// отрицание этого не заметило бы.
const (
	liveConditionDefs = 6
	// 4, а не 3: одна строка модели несёт ДВА ограничения
	// (`[user with mfa_fresh, service_account with mfa_fresh]`). Число получено
	// подсчётом вхождений, а не строк — первая редакция считала строками и
	// ошиблась ровно на этот случай. Это и есть работа положительной половины.
	liveMFARestrictions = 4
	fgaModelPath        = "proto/kacho/cloud/iam/v1/fga_model.fga"
	accessBindingPth    = "proto/kacho/cloud/iam/v1/access_binding.proto"
)

var (
	reConditionDef   = regexp.MustCompile(`(?m)^condition\s+\w+\(`)
	reMFARestriction = regexp.MustCompile(`with\s+mfa_fresh`)
	reIamCondType    = regexp.MustCompile(`(?m)^type\s+iam_condition\s*$`)
)

// codeOnly отбрасывает комментарии, оставляя исполняемую часть файла.
//
// Без этого гейт считает СЛОВА, а не конструкции, и ошибается в ОБЕ стороны.
// Обе ошибки наблюдались на этом же файле при его написании, а не придуманы:
//
//   - заметка «живое условие трогать нельзя» попала в счёт живых условий, и
//     положительная половина покраснела на собственном комментарии;
//   - историческая заметка «здесь стояли записи ConditionsService/» покраснила
//     отрицательную половину, хотя ни одной записи не осталось.
//
// Второе — хуже: оно делает гейт неудобным ровно там, где следующий читатель
// объясняет, ПОЧЕМУ поверхность снята, и первый же ложный срабат его отключит.
//
// Маркер комментария ищется ВНЕ строкового литерала: путь RPC в Go-карте — это
// строка, и обрезать по `//` внутри неё значило бы спрятать настоящее объявление.
func codeOnly(rel, body string) string {
	marker := ""
	switch {
	case strings.HasSuffix(rel, ".go"):
		marker = "//"
	case strings.HasSuffix(rel, ".fga"), strings.HasSuffix(rel, ".sh"):
		marker = "#"
	default: // .json и прочее — комментариев не бывает, читаем как есть
		return body
	}
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		b.WriteString(cutComment(line, marker))
		b.WriteByte('\n')
	}
	return b.String()
}

// cutComment обрезает строку по первому маркеру комментария, встреченному вне
// кавычек (двойных или обратных).
func cutComment(line, marker string) string {
	inQuote := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inQuote != 0:
			if c == '\\' && inQuote == '"' {
				i++ // экранированный символ не закрывает литерал
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '`' || c == '\'':
			inQuote = c
		default:
			if strings.HasPrefix(line[i:], marker) {
				return line[:i]
			}
		}
	}
	return line
}

// readTreeFile читает файл дерева и отказывается выносить вердикт по пустому.
func readTreeFile(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(root, rel)))
	if err != nil {
		t.Fatalf("не прочитан %s: %v — вердикт по непрочитанному файлу не выносится", rel, err)
	}
	if len(raw) == 0 {
		t.Fatalf("%s пуст — гейт смотрит не туда, его «находок нет» беспредметно", rel)
	}
	return string(raw)
}

// TestTenantConditionSurface_IsGone — отрицательная половина.
func TestTenantConditionSurface_IsGone(t *testing.T) {
	root := repoRoot(t)

	t.Run("файлов снятой поверхности нет", func(t *testing.T) {
		var back []string
		for _, rel := range retiredPaths {
			if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
				back = append(back, rel)
			}
		}
		if len(back) > 0 {
			t.Fatalf("тенантская поверхность условного доступа снята, но %d её путей в дереве:\n  %s",
				len(back), strings.Join(back, "\n  "))
		}
		t.Logf("перепись: проверено %d путей, ни одного не осталось", len(retiredPaths))
	})

	t.Run("объявлений снятой поверхности нет", func(t *testing.T) {
		scanned := 0
		for _, sf := range surfaceFiles {
			body := codeOnly(sf.path, readTreeFile(t, root, sf.path))
			scanned++
			// Предпосылка: файл действительно того жанра, в котором мы ищем.
			if !strings.Contains(body, sf.genre) {
				t.Fatalf("%s (%s) не содержит маркера жанра %q — предпосылка гейта не выполняется: "+
					"«объявления не найдено» здесь означало бы «читаю не тот файл»",
					sf.path, sf.whatFor, sf.genre)
			}
			if strings.Contains(body, sf.forbid) {
				n := strings.Count(body, sf.forbid)
				t.Errorf("%s (%s): %d упоминаний %q — поверхность снята, объявление осталось",
					sf.path, sf.whatFor, n, sf.forbid)
			}
		}
		t.Logf("перепись: прочитано %d файлов объявлений", scanned)
	})

	t.Run("тип условия снят из модели прав", func(t *testing.T) {
		model := codeOnly(fgaModelPath, readTreeFile(t, root, fgaModelPath))
		if reIamCondType.MatchString(model) {
			t.Errorf("%s объявляет `type iam_condition` — тип пережил своего единственного производителя",
				fgaModelPath)
		}
	})

	t.Run("контракт привязки резервирует номер И имя", func(t *testing.T) {
		ab := readTreeFile(t, root, accessBindingPth)
		if !strings.Contains(ab, "message AccessBinding {") {
			t.Fatalf("%s не содержит message AccessBinding — предпосылка не выполняется", accessBindingPth)
		}
		for _, live := range []string{"string condition_id = 9;", "BuiltinCondition builtin_condition = 14;"} {
			if strings.Contains(ab, live) {
				t.Errorf("%s всё ещё объявляет %q — поле снято с контракта, объявление осталось",
					accessBindingPth, live)
			}
		}
		if !strings.Contains(ab, "reserved 9, 14;") {
			t.Errorf("%s: теги 9 и 14 не зарезервированы — снятый номер обязан закрываться, "+
				"иначе следующий дизайн унаследует значение, которого никто не реализовал", accessBindingPth)
		}
		if !strings.Contains(ab, `reserved "condition_id", "builtin_condition";`) {
			t.Errorf("%s: имена condition_id / builtin_condition не зарезервированы — "+
				"резервируется номер И имя", accessBindingPth)
		}
	})

	t.Run("хранилище наложения сносится объявленной миграцией", func(t *testing.T) {
		dir := filepath.Join(root, "services/iam/internal/migrations")
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("не прочитан каталог миграций: %v", err)
		}
		var all strings.Builder
		files := 0
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			raw, err := os.ReadFile(filepath.Clean(filepath.Join(dir, e.Name())))
			if err != nil {
				t.Fatalf("не прочитана миграция %s: %v", e.Name(), err)
			}
			all.Write(raw)
			files++
		}
		if files == 0 {
			t.Fatal("в каталоге миграций нет ни одного .sql — вердикт беспредметен")
		}
		body := all.String()
		for _, tbl := range []string{"kacho_iam.conditions", "kacho_iam.access_binding_conditions"} {
			if !strings.Contains(body, "DROP TABLE IF EXISTS "+tbl) {
				t.Errorf("ни одна миграция не сносит %s — таблица снятой поверхности осталась бы в базе", tbl)
			}
		}
		if !strings.Contains(body, "DROP COLUMN IF EXISTS condition_id") {
			t.Error("ни одна миграция не снимает колонку access_bindings.condition_id")
		}
		// Снос обязан быть ОБЪЯВЛЕН со своим числом строк, иначе он не проверен.
		guard := readTreeFile(t, root, "services/iam/internal/migrations/dropguard.json")
		for _, tbl := range []string{"kacho_iam.conditions", "kacho_iam.access_binding_conditions"} {
			if !strings.Contains(guard, `"`+tbl+`"`) {
				t.Errorf("dropguard.json не объявляет снос %s — число уничтожаемых строк не названо, "+
					"значит не измерено", tbl)
			}
		}
		t.Logf("перепись: прочитано %d файлов миграций", files)
	})
}

// TestTupleConditionMechanism_StaysLive — положительная половина.
//
// Без неё отрицания выше зеленели бы и на дереве, из которого вынесли ВСЁ, что
// называется условием, включая механизм, который никто не просил трогать.
func TestTupleConditionMechanism_StaysLive(t *testing.T) {
	root := repoRoot(t)

	model := codeOnly(fgaModelPath, readTreeFile(t, root, fgaModelPath))
	if !strings.Contains(model, "type project") {
		t.Fatalf("%s не похож на модель прав — предпосылка положительной половины не выполняется", fgaModelPath)
	}
	if got := len(reConditionDef.FindAllString(model, -1)); got != liveConditionDefs {
		t.Errorf("в %s объявлено %d условий, ожидалось %d — живой механизм условий на кортеже задет",
			fgaModelPath, got, liveConditionDefs)
	}
	if got := len(reMFARestriction.FindAllString(model, -1)); got != liveMFARestrictions {
		t.Errorf("в %s %d ограничений `with mfa_fresh`, ожидалось %d — живые условные привязки задеты",
			fgaModelPath, got, liveMFARestrictions)
	}

	// Здесь стояли ещё две проверки живого — `TupleCondition` во внутреннем
	// контракте и `enum BuiltinCondition` рядом с ним. Обе сняты вместе со своим
	// предметом: стадия S6 (kacho#747) убрала службу администрирования внешнего
	// движка прав целым файлом контракта, и `TupleCondition` ушёл с ней.
	//
	// Что из этого СЛЕДУЕТ и здесь не проверяется: `builtin_condition.proto`
	// остался в дереве, но читателей у него больше нет ни одного —
	// `TupleCondition.builtin` был единственным. Восстанавливать на него проверку
	// нельзя: она утверждала бы, что файл кем-то читается, а это уже неправда.
	// Судьба осиротевшего файла — отдельное решение по контракту (снять с
	// объявлением разрыва либо назвать причину, по которой он остаётся), и оно
	// принимается не здесь.
}
