// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// clusterrelationproducer_test.go — запись каталога прав, гейтящая RPC на
// объекте `cluster`, обязана называть отношение, которое кто-то ПРОИЗВОДИТ.
//
// ПРЕДМЕТ. Кластер — единственный объект своего типа, и арендатор его не
// создаёт: все права на нём заводит посев (миграции iam и код первичной
// настройки). Если запись каталога требует отношения, которого посев не
// заводит НИКОМУ, проверка не выполнится ни для кого — ни для арендатора, ни
// для администратора. Возможность объявлена, покрыта типами, описана в
// контракте и не работает ни при каком входе: класс «неисполнимая
// возможность» (`api-conventions.md`).
//
// ЧЕМ ЭТО ОТЛИЧАЕТСЯ ОТ ОБЫЧНОГО ОТКАЗА. Отказ по правам говорит «этому
// субъекту не выдано» и лечится выдачей. Здесь выдать нечего: отношения нет в
// словаре ни у одной роли и ни у одного посева, поэтому «выдайте мне доступ»
// не имеет исполнителя. Со стороны оба состояния выглядят одинаково — 403 с
// тем же текстом, — и различает их только этот гейт.
//
// ЗАМЕР, ради которого гейт заведён (внешний стенд, 2026-08-21): каталоги
// типов машин и типов дисков — четыре RPC — требовали отношения `viewer` на
// кластере. Отношение не производилось ничем: в фактах на кластере жили
// `system_admin`, `system_viewer`, `quota_reader`, и только они. Следствие для
// арендатора: нельзя выбрать ни тип машины, ни тип диска, то есть нельзя
// создать ни машину, ни том. Найдено владельцем на живом стенде.
//
// ПОЧЕМУ СТАТИЧЕСКИ, А НЕ ПРОБОЙ НА БАЗЕ. Часть посева выполняется миграциями,
// часть — кодом первичной настройки при старте службы. Проба, читающая базу
// сразу после миграций, назвала бы непроизводимым и `system_admin`, который
// заводится кодом. Гейт читает ОБА производителя в дереве, поэтому не зависит
// от того, кто именно сеет.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// catalogEntry — запись каталога прав в том виде, в каком она лежит в дереве.
type catalogEntry struct {
	FQN              string `json:"fqn"`
	Permission       string `json:"permission"`
	RequiredRelation string `json:"required_relation"`
	ScopeExtractor   struct {
		ObjectType string `json:"object_type"`
	} `json:"scope_extractor"`
}

// clusterScopedCatalogRelations — отношение → RPC, которые его требуют на
// объекте `cluster`. Записи `<exempt>` и записи с пустым отношением сюда не
// попадают: у них проверки нет by construction, поэтому и производителя им не
// нужно.
func clusterScopedCatalogRelations(catalogJSON []byte) (map[string][]string, int, error) {
	var entries []catalogEntry
	if err := json.Unmarshal(catalogJSON, &entries); err != nil {
		return nil, 0, err
	}
	out := map[string][]string{}
	for _, e := range entries {
		if e.ScopeExtractor.ObjectType != "cluster" || e.RequiredRelation == "" {
			continue
		}
		out[e.RequiredRelation] = append(out[e.RequiredRelation], e.FQN)
	}
	return out, len(entries), nil
}

// relationBeforeObject — отношение, названное непосредственно перед объектом.
// Покрывает обе формы посева: SQL (`'relation', '<rel>'` в jsonb_build_object)
// и JSON в коде (`"relation": "<rel>"`).
var relationBeforeObject = regexp.MustCompile(`['"]relation['"]\s*[:,]\s*['"]([a-z_]+)['"]`)

// clusterObjectMention — упоминание кластерного объекта в полезной нагрузке.
var clusterObjectMention = regexp.MustCompile(`['"]object['"]\s*[:,]\s*['"]cluster:`)

// clusterRelationRow — выдача на кластере, записанная СТОЛБЦАМИ: тип объекта,
// его идентификатор, отношение. Так её печатает сведённая первичная миграция.
var clusterRelationRow = regexp.MustCompile(`VALUES \('cluster', '[^']+', '([a-z_]+)'`)

// relationsProducedOnCluster — отношения, которые дерево заводит на кластерном
// объекте, и сколько раз каждое встречено. Отношение считается произведённым,
// если оно названо в пределах 400 байт ПЕРЕД упоминанием кластерного объекта:
// в обеих формах посева отношение идёт раньше объекта в одной и той же
// полезной нагрузке.
func relationsProducedOnCluster(files []string) (map[string]int, int, error) {
	produced := map[string]int{}
	read := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, read, err
		}
		read++
		text := string(b)
		for _, loc := range clusterObjectMention.FindAllStringIndex(text, -1) {
			from := loc[0] - 400
			if from < 0 {
				from = 0
			}
			if m := relationBeforeObject.FindAllStringSubmatch(text[from:loc[0]], -1); len(m) > 0 {
				produced[m[len(m)-1][1]]++
				continue
			}
			// ВПЕРЁД — вторая законная форма, и она не редкость, а умолчание
			// сведённой миграции. `jsonb` хранит ключи в порядке «сначала
			// короткие»: `user`(4) · `object`(6) · `relation`(8), — поэтому в
			// напечатанном дампом объекте отношение стоит ПОСЛЕ объекта всегда,
			// а не иногда. Разбор, смотрящий только назад, на таком файле не
			// находит НИЧЕГО и объявляет отношение непроизводимым при живой
			// выдаче.
			//
			// Окно ограничено концом объекта JSON, а не длиной: иначе отношение
			// перетекло бы из СЛЕДУЮЩЕГО кортежа той же вставки.
			to := loc[1] + 400
			if to > len(text) {
				to = len(text)
			}
			ahead := text[loc[1]:to]
			if end := strings.IndexByte(ahead, '}'); end >= 0 {
				ahead = ahead[:end]
			}
			if m := relationBeforeObject.FindStringSubmatch(ahead); m != nil {
				produced[m[1]]++
			}
		}

		// ФОРМА ТРЕТЬЯ — столбцовая строка журнала отношений.
		//
		// У неё нет ни конструктора, ни объекта JSON: тип объекта, его
		// идентификатор и отношение стоят соседними значениями одной вставки.
		// Дамп печатает выдачу именно так, и по обеим прежним формам она
		// невидима целиком.
		for _, m := range clusterRelationRow.FindAllStringSubmatch(text, -1) {
			produced[m[1]]++
		}
	}
	return produced, read, nil
}

func TestClusterScopedCatalogEntryNamesARelationSomeoneProduces(t *testing.T) {
	catalogPath := filepath.Join("..", "..",
		"services", "iam", "internal", "apps", "kaname", "seed", "embedded", "permission_catalog.json")
	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("каталог прав не прочитан (%s): %v", catalogPath, err)
	}
	required, total, err := clusterScopedCatalogRelations(raw)
	if err != nil {
		t.Fatalf("каталог прав не разобран: %v", err)
	}

	files, err := treecorpus.UnderWithSuffix(filepath.Join("..", "..", "services", "iam"), ".sql", ".go")
	if err != nil {
		t.Fatalf("состав дерева iam не получен: %v", err)
	}
	produced, read, err := relationsProducedOnCluster(files)
	if err != nil {
		t.Fatalf("производители отношений не прочитаны: %v", err)
	}

	t.Logf("перепись: записей каталога %d; из них с областью cluster %d (отношений %d); "+
		"файлов iam осмотрено %d; отношений, производимых на кластере, %d",
		total, countFQNs(required), len(required), read, len(produced))

	// Предпосылка гейта. Ноль прочитанных файлов или ноль найденных
	// производителей означают, что молчание гейта ничего не значит.
	if read == 0 {
		t.Fatal("осмотрено ноль файлов iam — гейт не читал дерева")
	}
	if len(produced) == 0 {
		t.Fatal("на кластерном объекте не найдено НИ ОДНОГО производимого отношения — " +
			"разбор не дошёл до посева, и любой вердикт этого гейта недействителен")
	}
	if total == 0 {
		t.Fatal("каталог прав пуст — проверять нечего")
	}

	for relation, fqns := range required {
		if produced[relation] > 0 {
			continue
		}
		t.Errorf("отношение %q требуется на объекте cluster, но его не производит НИКТО "+
			"(в дереве iam ноль посевов, заводящих это отношение на кластере) — "+
			"проверка не выполнится ни для арендатора, ни для администратора.\n"+
			"  затронутые RPC (%d): %s\n"+
			"  производимые на кластере отношения: %s\n"+
			"  исходов три: (1) объявить RPC `<exempt>`, если это глобальный справочник, "+
			"который каждый аутентифицированный обязан читать (эталон — каталог geo); "+
			"(2) требовать отношение, которое посев заводит; "+
			"(3) завести посев этого отношения.",
			relation, len(fqns), strings.Join(fqns, ", "), namesOf(produced))
	}
}

func countFQNs(m map[string][]string) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}

func namesOf(m map[string]int) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return strings.Join(out, ", ")
}
