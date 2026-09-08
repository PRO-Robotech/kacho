// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retiredblockstorageedgecatalog_test.go — снятые типы блочного хранения не
// возвращаются в каталог прав У КРАЯ (задача продукта #2361).
//
// # Почему этот гейт заведён ЗДЕСЬ, а не оставлен там, где он был
//
// Утверждение о копии каталога у края жило внутри службы доступа
// (`services/iam/internal/check/retired_block_storage_test.go`, полоса
// `TestRetiredBlockStorageIsNotInPermissionCatalog`). Копий каталога ДВЕ, и
// живут они в РАЗНЫХ деревьях: вшитая копия службы уезжает вместе с ней,
// копия края остаётся у платформы. После разреза прежний носитель честно
// объявит третий исход («условие не создано»), потому что дерева платформы
// рядом с ним не будет, — и копия края останется БЕЗ СТОРОЖА. Молчание
// пропущенной пробы неотличимо от исправной работы, поэтому половина
// утверждения переезжает сюда ДО разреза, а не после.
//
// Это РАЗДЕЛЕНИЕ, а не перенос: прежний носитель продолжает судить СВОЮ копию
// (и словари, и модель — они уезжают с ним), здесь судится копия платформы.
//
// # Предмет
//
// Блочное хранение принадлежит `services/storage`. У `services/compute` была
// вторая, независимая копия тех же ресурсов; она снята, а вот АВТОРИЗАЦИОННЫЕ
// типы её пережили — снимать их было изменением на стороне службы доступа.
// Тип, названный каталогом края, остаётся АДРЕСУЕМЫМ: край гейтит по нему RPC,
// извлекает по нему область и принимает решение о доступе. Возвращение такого
// типа — не устаревшая упаковка, а живое объявление.
//
// # Почему КАЖДОЕ отрицание парное
//
// «Снятого имени нет» само по себе неотличимо от «каталог пуст», «каталог
// переехал» и «разбор перестал видеть поле». Поэтому рядом с каждым отрицанием
// стоит положительный контроль: ЖИВОЙ владелец тех же ресурсов обязан
// найтись В ТОЙ ЖЕ копии ТЕМ ЖЕ разбором. Правка, выпотрошившая таблицу, роняет
// положительную половину; регрессия, вернувшая снятый тип, — отрицательную.
//
// # Почему таблица здесь СВОЯ, и чем это удержано
//
// Прежний носитель сверяет свою таблицу с прод-словарём службы
// (`domain.RetiredTypes()`), и держать её ОДНИМ объявлением на оба дерева
// нельзя by construction: служба резолвит фундамент ОПУБЛИКОВАННОЙ версией
// (`services/iam/go.mod`, `require github.com/PRO-Robotech/kacho v0.0.0-…`), а
// подмена пути на внутренний модуль запрещена. Значит новое объявление в `pkg/`
// доехало бы до службы только вместе с бампом пина — то есть в другом изменении
// и в другом репозитории.
//
// Расхождение двух таблиц здесь НЕ прощается молча, а ловится с другой стороны:
// [TestRetiredBlockStorageTableStillHasASubject] требует, чтобы у каждой записи
// таблицы был ЖИВОЙ предмет в дереве платформы — снятый ресурс отсутствует у
// прежнего владельца, а живой присутствует у нынешнего. Запись, потерявшая
// предмет, — находка, а не «стало лучше»: она унаследовала бы следующую слепую
// зону.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// edgeCatalogRel — копия каталога прав У КРАЯ, от корня дерева платформы.
const edgeCatalogRel = "gateway/internal/middleware/embed/permission_catalog.json"

// retiredBlockStorageType — один снятый тип блочного хранения в двух написаниях:
// точечный ключ права (`<модуль>.<ресурс>`) и тип объекта модели, которым край
// извлекает область.
type retiredBlockStorageType struct {
	// Dotted — стебель ключа права: `<модуль>.<ресурс>`. Ключ каталога несёт
	// ресурс во МНОЖЕСТВЕННОМ числе (`compute.disks.get`), поэтому сверка идёт
	// приставкой стебля, а не равенством.
	Dotted string
	// ObjectType — тип объекта в `scope_extractor.object_type`.
	ObjectType string
	// ProtoOwnerDir — каталог контрактов ПРЕЖНЕГО владельца. Предмет записи
	// жив, пока этот каталог ресурса НЕ объявляет.
	ProtoOwnerDir string
	// ProtoResource — имя сообщения ресурса в контракте, по которому судится
	// предмет записи.
	ProtoResource string
}

// retiredEdgeBlockStorage — типы, которых у края больше не бывает.
var retiredEdgeBlockStorage = []retiredBlockStorageType{
	{Dotted: "compute.disk", ObjectType: "compute_disk", ProtoOwnerDir: "proto/kacho/cloud/compute/v1", ProtoResource: "Disk"},
	{Dotted: "compute.image", ObjectType: "compute_image", ProtoOwnerDir: "proto/kacho/cloud/compute/v1", ProtoResource: "Image"},
	{Dotted: "compute.snapshot", ObjectType: "compute_snapshot", ProtoOwnerDir: "proto/kacho/cloud/compute/v1", ProtoResource: "Snapshot"},
}

// liveEdgeBlockStorage — те же ресурсы у НЫНЕШНЕГО владельца. Это положительный
// контроль каждого отрицания выше: они обязаны находиться ТЕМ ЖЕ разбором.
var liveEdgeBlockStorage = []retiredBlockStorageType{
	{Dotted: "storage.volume", ObjectType: "storage_volume", ProtoOwnerDir: "proto/kacho/cloud/storage/v1", ProtoResource: "Volume"},
	{Dotted: "storage.snapshot", ObjectType: "storage_snapshot", ProtoOwnerDir: "proto/kacho/cloud/storage/v1", ProtoResource: "Snapshot"},
	{Dotted: "storage.image", ObjectType: "storage_image", ProtoOwnerDir: "proto/kacho/cloud/storage/v1", ProtoResource: "Image"},
}

// edgeCatalogRow — ровно те поля каталога, о которых этот гейт утверждает.
type edgeCatalogRow struct {
	FQN            string `json:"fqn"`
	Permission     string `json:"permission"`
	ScopeExtractor *struct {
		ObjectType string `json:"object_type"`
	} `json:"scope_extractor"`
}

// edgeCatalogCensus — объём осмотренного. Печатается на ВСЯКОМ исходе: без него
// «ноль находок» неотличимо от «ноль прочитанного».
type edgeCatalogCensus struct {
	Rows      int
	Scoped    int
	LiveSeen  map[string]int
	Retired   int
	LiveTypes int
}

// auditEdgeCatalogBlockStorage — предикат ОБЕИХ половин.
//
// Чистая функция от строк каталога: её же зовёт инъекция, подавая
// синтетические строки. Гейт, чью способность падать доказывают правкой живого
// дерева, доказательства не имеет — испортить каталог ради опыта нельзя.
func auditEdgeCatalogBlockStorage(rows []edgeCatalogRow) ([]string, edgeCatalogCensus) {
	c := edgeCatalogCensus{
		Rows:      len(rows),
		LiveSeen:  map[string]int{},
		Retired:   len(retiredEdgeBlockStorage),
		LiveTypes: len(liveEdgeBlockStorage),
	}
	var found []string

	for _, row := range rows {
		if row.ScopeExtractor != nil && row.ScopeExtractor.ObjectType != "" {
			c.Scoped++
		}
		for _, r := range retiredEdgeBlockStorage {
			module, resource, ok := strings.Cut(r.Dotted, ".")
			if !ok {
				found = append(found, fmt.Sprintf(
					"собственная таблица гейта несёт негодный точечный ключ %q", r.Dotted))
				continue
			}
			if strings.HasPrefix(row.Permission, module+"."+resource) {
				found = append(found, fmt.Sprintf(
					"каталог края несёт право %q на %s — снятый ресурс снова на контракте",
					row.Permission, row.FQN))
			}
			if row.ScopeExtractor != nil && row.ScopeExtractor.ObjectType == r.ObjectType {
				found = append(found, fmt.Sprintf(
					"каталог края извлекает область %s по СНЯТОМУ типу %q — тип снова адресуем",
					row.FQN, r.ObjectType))
			}
		}
		if row.ScopeExtractor == nil {
			continue
		}
		for _, l := range liveEdgeBlockStorage {
			if row.ScopeExtractor.ObjectType == l.ObjectType {
				c.LiveSeen[l.ObjectType]++
			}
		}
	}

	// ПОЛОЖИТЕЛЬНАЯ половина. Без неё всякое отрицание выше зеленело бы на
	// пустом каталоге, на переехавшем каталоге и на разборе, разучившемся
	// читать поле области.
	for _, l := range liveEdgeBlockStorage {
		if c.LiveSeen[l.ObjectType] == 0 {
			found = append(found, fmt.Sprintf(
				"каталог края не гейтит НИ ОДНОГО RPC живым типом %q — положительный "+
					"контроль пуст, и отрицания выше не утверждают ничего", l.ObjectType))
		}
	}
	sort.Strings(found)
	return found, c
}

// TestRetiredBlockStorageIsNotInTheEdgePermissionCatalog — сам гейт.
func TestRetiredBlockStorageIsNotInTheEdgePermissionCatalog(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	// #nosec G304 -- путь собран из корня дерева и константы.
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(edgeCatalogRel)))
	if err != nil {
		t.Fatalf("каталог прав края %s не прочитан: %v — непрочитанное есть НАХОДКА, "+
			"а не «снятых типов там нет»", edgeCatalogRel, err)
	}
	var rows []edgeCatalogRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("разбор %s: %v", edgeCatalogRel, err)
	}

	found, c := auditEdgeCatalogBlockStorage(rows)
	t.Logf("перепись: строк каталога %d, из них с извлечением области %d; "+
		"снятых типов в таблице %d, живых %d; найдено живых: %v",
		c.Rows, c.Scoped, c.Retired, c.LiveTypes, c.LiveSeen)

	if c.Rows == 0 {
		t.Fatalf("%s разобран в НОЛЬ строк — гейт не утверждал бы ничего", edgeCatalogRel)
	}
	if c.Retired == 0 {
		t.Fatal("таблица снятых типов пуста — отрицать нечего, и гейт беспредметен")
	}
	if len(found) > 0 {
		t.Fatalf("каталог прав У КРАЯ разошёлся с картой владельцев — %d находка(и):\n  %s\n\n"+
			"Блочное хранение принадлежит storage; тип, названный этим каталогом, край "+
			"считает адресуемым и принимает по нему решение о доступе.",
			len(found), strings.Join(found, "\n  "))
	}
}

// TestRetiredBlockStorageTableStillHasASubject — у КАЖДОЙ записи таблицы есть
// живой предмет в дереве платформы.
//
// Без этой половины таблица пережила бы то, что ею обозначалось: запись, чей
// ресурс прежний владелец снова объявил, перестаёт быть «снятой», а запись,
// чей нынешний владелец ресурс потерял, перестаёт быть контролем. И то и другое
// — находка, а не «стало лучше»: ведомость обязана ИСТЕКАТЬ САМА.
func TestRetiredBlockStorageTableStillHasASubject(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	declares := func(dir, message string) (bool, int) {
		seen := 0
		hit := false
		want := "message " + message + " {"
		for rel := range tt.files {
			if !strings.HasPrefix(rel, dir+"/") || !strings.HasSuffix(rel, ".proto") {
				continue
			}
			seen++
			// #nosec G304 -- путь из индекса git.
			body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				continue
			}
			if strings.Contains(string(body), want) {
				hit = true
			}
		}
		return hit, seen
	}

	var findings []string
	scanned := 0
	for _, r := range retiredEdgeBlockStorage {
		hit, seen := declares(r.ProtoOwnerDir, r.ProtoResource)
		scanned += seen
		if seen == 0 {
			findings = append(findings, fmt.Sprintf(
				"%s: контрактов у прежнего владельца прочитано НОЛЬ — предпосылка записи не проверена",
				r.Dotted))
			continue
		}
		if hit {
			findings = append(findings, fmt.Sprintf(
				"%s: %s снова объявляет `message %s` — запись «снят» пережила свой предмет",
				r.Dotted, r.ProtoOwnerDir, r.ProtoResource))
		}
	}
	for _, l := range liveEdgeBlockStorage {
		hit, seen := declares(l.ProtoOwnerDir, l.ProtoResource)
		scanned += seen
		if seen == 0 {
			findings = append(findings, fmt.Sprintf(
				"%s: контрактов у нынешнего владельца прочитано НОЛЬ — контроль беспредметен",
				l.Dotted))
			continue
		}
		if !hit {
			findings = append(findings, fmt.Sprintf(
				"%s: %s НЕ объявляет `message %s` — положительный контроль потерял предмет",
				l.Dotted, l.ProtoOwnerDir, l.ProtoResource))
		}
	}

	t.Logf("перепись: контрактов осмотрено %d, записей снятых %d, записей живых %d",
		scanned, len(retiredEdgeBlockStorage), len(liveEdgeBlockStorage))
	if scanned == 0 {
		t.Fatal("осмотрено ноль контрактов — обход пуст, вердикт беспредметен")
	}
	if len(findings) > 0 {
		t.Fatalf("таблица снятых типов разошлась с деревом — %d находка(и):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}
