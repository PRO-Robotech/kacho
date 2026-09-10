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
	// Двенадцать координат под `services/iam/` сняты ВМЕСТЕ со своим корнем:
	// служба доступа вынесена отдельным продуктом, и путь под этим корнем не
	// может вернуться в ЭТО дерево ни при каком изменении. Утверждение «его
	// нет» стало истинным by construction — то есть перестало быть способным
	// упасть, а значит перестало быть утверждением. Остались координаты
	// контракта и стабов: они живут здесь, и возврат любой из них — возврат
	// поверхности.
	"proto/kaname/cloud/iam/v1/condition.proto",
	"proto/kaname/cloud/iam/v1/conditions_service.proto",
	"proto/kaname/cloud/iam/v1/access_binding_condition.proto",
	"pkg/api/kaname/cloud/iam/v1/condition.pb.go",
	"pkg/api/kaname/cloud/iam/v1/access_binding_condition.pb.go",
	"pkg/api/kaname/cloud/iam/v1/conditions_service.pb.go",
	"pkg/api/kaname/cloud/iam/v1/conditions_service.pb.gw.go",
	"pkg/api/kaname/cloud/iam/v1/conditions_service_grpc.pb.go",
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
	// Восемь координат под `services/iam/` сняты вместе со своим корнем: служба
	// доступа вынесена отдельным продуктом. Их предпосылка (маркер жанра в
	// читаемом файле) отказывала на КАЖДОМ прогоне, и это верное поведение —
	// вердикт по непрочитанному файлу не выносится. Остались объявления КРАЯ:
	// он маршрутизирует службу и обязан не знать снятой поверхности; его
	// таблица маршрутов, список обхода, вшитый каталог прав и регистрация
	// мультиплексора живут в этом дереве и предпосылку выполняют.
	{
		path:    "gateway/internal/middleware/rest_route_table_gen.go",
		genre:   "kaname.cloud.iam.v1.AccessBindingService/",
		forbid:  "ConditionsService/",
		whatFor: "таблица REST-маршрутов края",
	}, {
		path:    "gateway/internal/allowlist/list.go",
		genre:   "/kaname.cloud.iam.v1.AccessBindingService/",
		forbid:  "ConditionsService/",
		whatFor: "список обхода края",
	}, {
		path:    "gateway/internal/middleware/embed/permission_catalog.json",
		genre:   "kaname.cloud.iam.v1.AccessBindingService/",
		forbid:  "ConditionsService/",
		whatFor: "каталог прав, вшитый в край",
	}, {
		path:    "gateway/internal/restmux/mux.go",
		genre:   "RegisterAccessBindingServiceHandlerFromEndpoint",
		forbid:  "RegisterConditionsServiceHandlerFromEndpoint",
		whatFor: "регистрация REST-мультиплексора",
	},
}

// ── что остаётся живым ───────────────────────────────────────────────────────

// liveConditionDefs / liveMFARestrictions — точные числа живой половины на ревизии
// снятия. Числа, а не «есть хоть что-то»: снос живого дал бы ноль, и одно только
// отрицание этого не заметило бы.
const (
	liveConditionDefs = 6
	// Число получено подсчётом ВХОЖДЕНИЙ, а не строк: одна строка модели способна
	// нести два ограничения (`[user with mfa_fresh, service_account with mfa_fresh]`).
	// Первая редакция считала строками и ошиблась ровно на этот случай.
	//
	// Было 4, стало 2 — и это не подгонка под зелёное, а приведение якоря к факту.
	// Отношение `console` на типе `cluster` снято решением #1820 (коммит d1c78ec271):
	// читателя нет ни одного, вычисляемой ветви нет, выдать его нечем — роль
	// отображается в ярусы, правило даёт только глаголы, пути кортежу не существует.
	// Разбор и цена — services/iam/docs/engineering/architecture/anchor-relations-adjudicated.md.
	// Строка несла ДВА вхождения, поэтому 4 − 2 = 2; живыми остаются `ssh` и
	// `console` на `compute_instance`.
	//
	// Почему якорь пережил своё снятие на три недели: тем же коммитом обновлён
	// близнец этого утверждения в модуле службы
	// (services/iam/internal/authzmap/conditioned_relation_has_producer_test.go —
	// запись `cluster#console with mfa_fresh` снята вместе с предметом), а этот
	// якорь живёт в КОРНЕВОМ модуле, куда `go test ./...` из services/iam не
	// доходит by construction. Два места об одном предмете в двух модулях: правя
	// модель прав, ищи утверждения о ней ОБОИМИ обходами, а не тем, который
	// запускается из твоего каталога.
	liveMFARestrictions = 2
	fgaModelPath        = "proto/kaname/cloud/iam/v1/fga_model.fga"
	accessBindingPth    = "proto/kaname/cloud/iam/v1/access_binding.proto"
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
	t.Parallel()
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

	// Здесь стояла подпроба «хранилища наложения нет в схеме»: она читала цепь
	// миграций службы доступа, требовала якорь `kaname.access_bindings` и
	// отсутствия четырёх имён снятой поверхности.
	//
	// СНЯТА ВМЕСТЕ С ПРЕДМЕТОМ. Схема живёт в базе службы, служба вынесена
	// отдельным продуктом, и каталога миграций в этом дереве нет — подпроба
	// отказывала на чтении каталога, то есть вердикта не выносила вовсе.
	// Свойство «в схеме iam нет поверхности наложения» теперь целиком предмет
	// того репозитория: судить чужую схему отсюда нечем, а утверждать о ней по
	// непрочитанному — ровно то, против чего написана эта проба.
	//
	// Остальные четыре подпробы предмет сохранили: контракт (`proto/kaname/`),
	// стабы (`pkg/api/kaname/`) и объявления КРАЯ живут в этом дереве.
}

// TestTupleConditionMechanism_StaysLive — положительная половина.
//
// Без неё отрицания выше зеленели бы и на дереве, из которого вынесли ВСЁ, что
// называется условием, включая механизм, который никто не просил трогать.
func TestTupleConditionMechanism_StaysLive(t *testing.T) {
	t.Parallel()
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
