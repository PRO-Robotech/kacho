// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Отношение, которое ВЫДАЁТ миграция, обязано СУЩЕСТВОВАТЬ в модели, которую
// применяют на стенде.
//
// # ПРЕДМЕТ
//
// Выдача права живёт в двух артефактах, и они приезжают на стенд РАЗНЫМИ путями:
// отношение объявляет модель прав (её применяет установочное задание чарта), а
// саму выдачу пишет миграция iam — строкой в очередь `fga_outbox`. Если миграция
// называет отношение, которого в применённой модели нет, хранилище прав отвергает
// запись СО ЗНАЧЕНИЕМ «такого отношения нет», дренаж верно считает такой отказ
// ПОСТОЯННЫМ, и строка травится: `attempt_count` выставляется в потолок, и больше
// её не возьмут никогда.
//
// # ЧТО ЭТО СТОИТ
//
// У очереди `fga_outbox` сервиса iam НЕТ переезда отравленных строк обратно в
// работу: `pkg/outbox/reconciler` разбирает партиции по `resource_id`, которого у
// этой очереди нет by construction (она партиционирована по `tuple_key`), и её
// собственный godoc это прямо оговаривает. Следствие названо честно: отравленная
// строка здесь не «подождёт», а ЛЕЖИТ ДО ВМЕШАТЕЛЬСТВА ОПЕРАТОРА. Право, которое
// она несла, не выдаётся; если это право модуля читать пределы — каждая мутация
// его домена отвергается fail-closed.
//
// # ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ
//
// Расхождение не видно НИ ОДНОЙ из двух сторон. Миграция — обычный SQL: она
// применяется без ошибки, потому что про модель прав ничего не знает. Модель —
// обычный DSL: он проходит проверку, потому что про миграции ничего не знает.
// Сборка цела, пробы обеих сторон зелёные, а право не выдано. Единственное место,
// где два факта встречаются, — дерево, и спрашивать его должен предикат.
//
// # ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
// Каждая пара «тип объекта + отношение», которую миграции iam пишут в `fga_outbox`,
// объявлена в модели, УЕЗЖАЮЩЕЙ НА СТЕНД, — в DSL конфигурационной карты чарта.
// Проверяется именно она, а не каноническая копия: на стенд применяют её. То, что
// карта не разошлась с каноническим файлом, держит отдельная проверка
// (`services/iam/internal/authzmap/fga_model_configmap_identity_test.go`), и здесь
// она не повторяется — два места об одном предмете расходятся молча.
//
// # ЧЕГО ЭТОТ ГЕЙТ НЕ ЛОВИТ (названо, чтобы на него не сослались шире предмета)
//
// Он про СОСТАВ, а не про ПОРЯДОК. Если отношение в модели есть, но установочное
// задание ещё не применило новую версию модели, а дренаж уже взял строку, — пара
// сойдётся, гейт промолчит, а строка отравится ровно так же. Порядок двух путей
// доставки деревом не проверяется; его лечит переезд отравленных строк обратно в
// работу, которого у этой очереди нет.
func TestEveryRelationGrantedByMigrationsExistsInTheAppliedModel(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)

	sqlFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services", "iam", "internal", "migrations"), ".sql")
	require.NoError(t, err, "перечень миграций берётся у индекса дерева, а не обходом диска")

	grants, migrationsRead, blocksSeen := grantsFromMigrations(t, sqlFiles)

	// Предпосылка гейта: он что-то ПРОЧИТАЛ. «Ноль находок» обязано быть отличимо
	// от «ноль осмотренного» — иначе сменившееся имя очереди или формы записи
	// выключит проверку, и она останется зелёной.
	require.NotZero(t, migrationsRead,
		"гейт не прочитал НИ ОДНОЙ миграции iam — он объявил бы «ноль находок», ничего не осмотрев")
	require.NotEmpty(t, grants,
		"гейт не нашёл НИ ОДНОЙ выдачи в очередь: либо имя очереди сменилось, либо форма записи "+
			"перестала попадать под предикат. Осмотрено миграций: %d, блоков кортежа: %d",
		migrationsRead, blocksSeen)

	modelPath := filepath.Join(root, appliedModelRelPath)
	modelRaw, err := os.ReadFile(modelPath)
	require.NoError(t, err,
		"модель, по которой принимается решение, обязана быть в дереве по пути %s — её "+
			"отсутствие и есть тот дефект, ради которого этот гейт написан", appliedModelRelPath)

	model := relationsByType(string(modelRaw))
	require.NotEmpty(t, model,
		"в %s не разобрано НИ ОДНОГО типа — предикат разбора модели перестал её видеть; "+
			"без этого «все отношения на месте» означало бы «ничего не прочитано»", appliedModelRelPath)

	var missing []string
	for _, g := range grants {
		rels, typeKnown := model[g.objectType]
		switch {
		case !typeKnown:
			missing = append(missing, fmt.Sprintf(
				"%s: выдаёт %s#%s — в применяемой модели НЕТ ТИПА %q",
				g.where, g.objectType, g.relation, g.objectType))
		case !rels[g.relation]:
			missing = append(missing, fmt.Sprintf(
				"%s: выдаёт %s#%s — тип есть, ОТНОШЕНИЯ %q у него нет (объявлены: %s)",
				g.where, g.objectType, g.relation, g.relation, joinSet(rels)))
		}
	}
	sort.Strings(missing)

	t.Logf("перепись: миграций iam осмотрено %d; блоков кортежа %d; пар «тип+отношение» выдано %d (%s); "+
		"в применяемой модели типов %d, отношений всего %d",
		migrationsRead, blocksSeen, len(grants), joinGrants(grants), len(model), countRelations(model))

	require.Empty(t, missing,
		"миграция выдаёт отношение, которого в применяемой модели нет: строка журнала ляжет "+
			"прямым фактом, но НИ ОДИН вопрос о доступе её не найдёт — план вывода строится из "+
			"модели, и отношения, которого в модели нет, он не спрашивает. Право не выдастся, а "+
			"отказ будет неотличим от честного:\n%s",
		strings.Join(missing, "\n"))
}

// appliedModelRelPath — модель, которую ПРИМЕНЯЕТ решение о доступе.
//
// До стадии S6 эпика #747 это была карта-заготовка чарта: установочное задание
// заливало её во внешний движок отношений, и состав ЭТОЙ копии решал, примет ли
// движок запись, эмитированную миграцией. Ни движка, ни задания, ни карты больше
// нет.
//
// Применяет модель теперь сама служба: `internal/authzmodel` встраивает файл и
// компилирует из него план вывода, которым форма считает вердикт. Предмет гейта
// от переезда не изменился, а стал точнее — раньше он сверялся с копией, доезжающей
// до чужого хранилища, теперь со ВСТРОЕННОЙ, то есть ровно с той, по которой
// принимается решение.
//
// Каноническая копия (`proto/kaname/cloud/iam/v1/fga_model.fga`) сюда по-прежнему не
// подставляется намеренно: две копии держит байт-идентичными своя цель, и гейт
// обязан читать ту, которая ИСПОЛНЯЕТСЯ.
const appliedModelRelPath = "services/iam/internal/authzmodel/fga_model.fga"

// relationGrant — одна выдача: какое отношение на каком типе объекта пишет миграция.
type relationGrant struct {
	objectType string
	relation   string
	where      string // путь относительно корня — координата находки
}

var (
	// Блок кортежа, ФОРМА ПЕРВАЯ — конструктор: `jsonb_build_object('user', …,
	// 'relation', …, 'object', …)`. Блок ограничивается СЛЕДУЮЩИМ таким же
	// вызовом, поэтому пары не перетекают между соседними кортежами одного INSERT.
	tupleBlockRe = regexp.MustCompile(`jsonb_build_object`)
	grantRelRe   = regexp.MustCompile(`'relation'\s*,\s*'([a-z_]+)'`)
	grantObjRe   = regexp.MustCompile(`'object'\s*,\s*'([a-z_]+):`)

	// Блок кортежа, ФОРМА ВТОРАЯ — готовый объект JSON: `{"user": …, "object":
	// "iam_fgaproxy:system", "relation": "fga_writer"}`.
	//
	// Её производит сведённая первичная миграция: `pg_dump` печатает ЗНАЧЕНИЕ
	// столбца, а не выражение, которым его когда-то собрали. Конструктора в
	// таком файле нет ни одного, поэтому распознаватель, знающий только первую
	// форму, не находит НИЧЕГО — и это молчание, а не находка: гейт остаётся на
	// вид рабочим, ничего не осмотрев. Ловит это предпосылка `NotEmpty(grants)`;
	// она и покраснела на первом же сведённом сервисе.
	//
	// Форма ограничивается фигурными скобками одного объекта — по той же
	// причине, по какой первая ограничивается следующим конструктором: иначе
	// пары перетекают между соседними кортежами одной вставки.
	tupleJSONRe    = regexp.MustCompile(`\{[^{}]*"relation"[^{}]*\}`)
	grantRelJSONRe = regexp.MustCompile(`"relation"\s*:\s*"([a-z_]+)"`)
	grantObjJSONRe = regexp.MustCompile(`"object"\s*:\s*"([a-z_]+):`)
)

// grantsFromMigrations — пары «тип объекта + отношение» из миграций, пишущих в
// очередь. Возвращает ещё и объём осмотренного: без него «ноль находок» неотличимо
// от «ноль прочитанного».
func grantsFromMigrations(t testing.TB, files []string) (grants []relationGrant, migrationsRead, blocksSeen int) {
	t.Helper()
	for _, path := range files {
		raw, err := os.ReadFile(path)
		require.NoError(t, err, "чтение %s", path)
		body := string(raw)
		if !strings.Contains(body, "fga_outbox") {
			continue
		}
		migrationsRead++

		rel := path
		if i := strings.Index(path, "/services/"); i >= 0 {
			rel = path[i+1:]
		}

		idx := tupleBlockRe.FindAllStringIndex(body, -1)
		for i, loc := range idx {
			end := len(body)
			if i+1 < len(idx) {
				end = idx[i+1][0]
			}
			chunk := body[loc[0]:end]
			rm := grantRelRe.FindStringSubmatch(chunk)
			if rm == nil {
				continue
			}
			blocksSeen++
			om := grantObjRe.FindStringSubmatch(chunk)
			if om == nil {
				// Тип объекта собран выражением, а не литералом — деревом он не
				// разрешается. Такой блок пропускается ОСОЗНАННО и виден в переписи
				// как разница между блоками и парами.
				continue
			}
			grants = append(grants, relationGrant{objectType: om[1], relation: rm[1], where: rel})
		}

		// Вторая форма — готовый объект JSON. Обходится ОТДЕЛЬНО, а не общим
		// образцом: у форм разные разделители блока, и один образец на обе дал бы
		// блок, перетекающий за границу кортежа.
		for _, chunk := range tupleJSONRe.FindAllString(body, -1) {
			rm := grantRelJSONRe.FindStringSubmatch(chunk)
			if rm == nil {
				continue
			}
			blocksSeen++
			om := grantObjJSONRe.FindStringSubmatch(chunk)
			if om == nil {
				continue
			}
			grants = append(grants, relationGrant{objectType: om[1], relation: rm[1], where: rel})
		}
	}
	return grants, migrationsRead, blocksSeen
}

var (
	modelTypeRe   = regexp.MustCompile(`^\s*type\s+([a-z_]+)\s*$`)
	modelDefineRe = regexp.MustCompile(`^\s*define\s+([a-z_]+)\s*:`)
)

// relationsByType — разбор DSL модели: тип → множество объявленных отношений.
//
// Предикаты построчные и СТРОГИЕ: `type <имя>` занимает строку целиком, а
// `define <имя>:` начинается с ключевого слова. Готовая JSON-форма модели лежит в
// том же файле и под эти строки не подпадает ни одной строкой, поэтому файл
// читается целиком без выделения блока.
func relationsByType(text string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	cur := ""
	for _, line := range strings.Split(text, "\n") {
		if m := modelTypeRe.FindStringSubmatch(line); m != nil {
			cur = m[1]
			if _, ok := out[cur]; !ok {
				out[cur] = map[string]bool{}
			}
			continue
		}
		if m := modelDefineRe.FindStringSubmatch(line); m != nil && cur != "" {
			out[cur][m[1]] = true
		}
	}
	return out
}

func countRelations(m map[string]map[string]bool) int {
	n := 0
	for _, rels := range m {
		n += len(rels)
	}
	return n
}

func joinSet(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func joinGrants(g []relationGrant) string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range g {
		k := x.objectType + "#" + x.relation
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
