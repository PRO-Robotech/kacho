// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// Область уникальности имени: контракт, документация и база обязаны называть ОДНУ.
//
// ПРЕДМЕТ. Имя ресурса уникально в какой-то области, и клиент принимает по ней
// решение ОДИН РАЗ — выбирая схему именования, которая потом прорастает в
// дашборды, алерты, ранбуки и внешние выгрузки. Переименовать ресурс дёшево,
// переучить всё, что на имя ссылается, — нет. Поэтому два клиентских источника,
// отвечающих на этот вопрос противоположно, стоят дороже, чем любой из них
// поодиночке: оба выглядят авторитетно, и выбрать между ними нельзя.
//
// ЧТО НАБЛЮДАЛОСЬ (задача продукта #1597). Контракт слушателя дважды говорил
// «unique within the project», документация трижды — «в рамках балансировщика».
// Права была документация: база держит `listeners_lb_name_uniq
// (load_balancer_id, name)`. Причём строкой ниже тот же контракт про ПОРТ писал
// «unique within the load balancer» — то есть автор различал эти две области и
// для имени выбрал не ту.
//
// АВТОРИТЕТ ЗДЕСЬ — БАЗА, а не большинство голосов. Уникальность держит уникальный
// индекс; всякий текст о ней есть УТВЕРЖДЕНИЕ о нём, и проверяется он сверкой с
// ним, а не с соседним текстом. Поэтому гейт сначала ВЫВОДИТ область из миграций
// и лишь потом судит утверждения.
//
// ПОЧЕМУ ЧТЕНИЕ ПРОЗЫ ЗДЕСЬ НЕИЗБЕЖНО. Предмет утверждения — комментарий контракта
// и страница документации; кода у него нет by construction. Разбор синтаксиса не
// применим: судить надо именно текст. Отсюда требование к распознавателю —
// знать ВСЕ формы записи (`testing.md` §«Гейт на класс», п.7), и здесь их четыре:
// англоязычная в контракте, две русские в документации (проза и запись
// ограничения) — плюс форма ограничения, которая опознаёт себя сама колонкой.
//
// ДВУЯЗЫЧНОСТЬ КОРПУСА УЧТЕНА НАМЕРЕННО (`testing.md` §«Предикат по ДВУЯЗЫЧНОМУ
// корпусу»): предикат на одном языке недобирал бы молча, и недобор пришёлся бы
// ровно на документацию — то есть на ту сторону, ради которой гейт заведён.

// nlbNameScopeTable — таблица, чьё имя проверяется. Ключ — имя таблицы в схеме
// `kacho_nlb`, значение — как этот ресурс называется в контракте и на странице.
type nlbNameScopeTable struct {
	table       string   // таблица в БД
	protoPrefix []string // префиксы имён сообщений контракта (Create/Update)
	docPage     string   // страница справочника, посвящённая ресурсу
	docWords    []string // как ресурс называется в русской прозе
}

var nlbNameScopeTables = []nlbNameScopeTable{
	{
		table:       "listeners",
		protoPrefix: []string{"CreateListenerRequest", "UpdateListenerRequest"},
		docPage:     "listener.mdx",
		docWords:    []string{"листенер", "слушател"},
	},
	{
		table:       "load_balancers",
		protoPrefix: []string{"CreateNetworkLoadBalancerRequest", "UpdateNetworkLoadBalancerRequest"},
		docPage:     "network-load-balancer.mdx",
		docWords:    []string{"балансировщик"},
	},
	{
		table:       "target_groups",
		protoPrefix: []string{"CreateTargetGroupRequest", "UpdateTargetGroupRequest"},
		docPage:     "target-group.mdx",
		docWords:    []string{"групп"},
	},
}

// nlbScopeColumnByPhrase — словарь ОБЛАСТЕЙ. Ключ — то, как область называют в
// тексте; значение — колонка, которой она соответствует в уникальном индексе.
// Словарь ЗАКРЫТ: область, которой здесь нет, — находка, а не «прочее», иначе
// новая формулировка молча уезжала бы из-под наблюдения.
var nlbScopeColumnByPhrase = map[string]string{
	// англоязычная форма контракта
	"project":              "project_id",
	"load balancer":        "load_balancer_id",
	"parent load balancer": "load_balancer_id",
	// русская форма документации
	"проекта":        "project_id",
	"проекте":        "project_id",
	"балансировщика": "load_balancer_id",
	"группы":         "target_group_id",
}

var (
	// CREATE UNIQUE INDEX [CONCURRENTLY] [IF NOT EXISTS] <idx> ON [<schema>.]<table> (<col>, name)
	nlbUniqIndexRe = regexp.MustCompile(
		`(?is)CREATE\s+UNIQUE\s+INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?` +
			`(\w+)\s+ON\s+(?:\w+\.)?(\w+)\s*\(\s*(\w+)\s*,\s*name\s*\)`)
	// Комментарий контракта: "... is unique within the <область>". ТЕРМИНАТОРОВ
	// НЕСКОЛЬКО, и это не педантизм: первая редакция знала только точку и запятую,
	// поэтому фраза, продолженная тире, уезжала из-под наблюдения ЦЕЛИКОМ — гейт
	// молчал не потому, что нарушения нет, а потому что не прочитал (п.7
	// §«Гейт на класс»). Обнаружено переписью: утверждений контракта было 4 при
	// шести объявлениях имени.
	nlbProtoScopeRe = regexp.MustCompile(`(?i)unique within the ([A-Za-z ]+?)\s*(?:[.,;—]|$)`)
	// сообщение контракта, объявляющее поле name
	nlbProtoNameFieldRe = regexp.MustCompile(`^\s*string\s+name\s*=\s*\d+\s*;`)
	nlbProtoMessageRe   = regexp.MustCompile(`^\s*message\s+(\w+)\s*\{`)
	// документация, проза: "Уникально в рамках балансировщика" / "не уникально в рамках …"
	nlbDocProseRe = regexp.MustCompile(`(?i)уникальн\S*\s+в\s+рамках\s+([а-яё]+)`)
	// документация, запись ограничения: "UNIQUE(load_balancer_id, name)" — опознаёт себя сама
	nlbDocConstraintRe = regexp.MustCompile(`UNIQUE\(\s*(\w+)\s*,\s*name\s*\)`)
)

// nlbScopeClaim — одно утверждение об области уникальности имени.
type nlbScopeClaim struct {
	file   string // rel-путь
	line   int
	table  string // ресурс, о котором утверждение
	column string // область, которую утверждение называет
	kind   string // "контракт" | "документация (проза)" | "документация (ограничение)"
	text   string
}

// nlbNameScopeCensus — объём осмотренного. Печатается ВСЕГДА, чтобы «ноль
// находок» было отличимо от «ноль прочитанного».
type nlbNameScopeCensus struct {
	MigrationFiles int
	ProtoFiles     int
	DocFiles       int
	IndexesDerived map[string]string // таблица → колонка области (авторитет)
	Claims         []nlbScopeClaim
	ClaimsByKind   map[string]int
}

// gooseVersion — номер версии миграции из имени файла. Порядок применения — по
// нему, а не по строке: `715001` идёт ПОСЛЕ `0035`, и последний CREATE побеждает.
func gooseVersion(name string) int64 {
	base := filepath.Base(name)
	cut := strings.IndexByte(base, '_')
	if cut <= 0 {
		return 0
	}
	v, err := strconv.ParseInt(base[:cut], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// collectNlbNameScope — собирает авторитет (из миграций) и все утверждения о нём
// (из контракта и документации).
//
// Состав дерева приходит СОСТАВЛЕННЫМ (`treecorpus.Tree`), а не собирается здесь
// обходом диска. Конструктор выбирает ВЫЗЫВАЮЩИЙ: гейт берёт
// `treecorpus.NewTree` (индекс git), инъекционная проба — `treecorpus.SyntheticTree`
// (её дерево репозиторием не является, спрашивать у него индекс нечего).
func collectNlbNameScope(tree *treecorpus.Tree) (nlbNameScopeCensus, error) {
	c := nlbNameScopeCensus{
		IndexesDerived: map[string]string{},
		ClaimsByKind:   map[string]int{},
	}

	// ── 1. АВТОРИТЕТ: последний уникальный индекс имени по каждой таблице ──────
	migs := clientTruthTreeFiles(tree, "services/nlb/internal/migrations", false, ".sql")
	sort.Slice(migs, func(i, j int) bool { return gooseVersion(migs[i]) < gooseVersion(migs[j]) })
	for _, m := range migs {
		body, err := clientTruthReadTreeFile(tree, m)
		if err != nil {
			return c, err
		}
		c.MigrationFiles++
		// Секция Down откатывает, а не задаёт действительность: читаем только Up.
		text := string(body)
		if cut := strings.Index(text, "-- +goose Down"); cut >= 0 {
			text = text[:cut]
		}
		for _, mt := range nlbUniqIndexRe.FindAllStringSubmatch(text, -1) {
			c.IndexesDerived[mt[2]] = mt[3]
		}
	}

	// ── 2. УТВЕРЖДЕНИЯ КОНТРАКТА ──────────────────────────────────────────────
	protos := clientTruthTreeFiles(tree, "proto/kacho/cloud/loadbalancer/v1", false, ".proto")
	for _, rel := range protos {
		body, err := clientTruthReadTreeFile(tree, rel)
		if err != nil {
			return c, err
		}
		c.ProtoFiles++
		lines := strings.Split(string(body), "\n")
		msg := ""
		var block []string
		var blockLine int
		for i, ln := range lines {
			if mt := nlbProtoMessageRe.FindStringSubmatch(ln); mt != nil {
				msg = mt[1]
				block, blockLine = nil, 0
				continue
			}
			trimmed := strings.TrimSpace(ln)
			if strings.HasPrefix(trimmed, "//") {
				if len(block) == 0 {
					blockLine = i + 1
				}
				// Префикс `//` СНИМАЕТСЯ до склейки. Иначе фраза, перенесённая на
				// следующую строку («… unique within\n// the project»), после
				// склейки получает `//` ПОСЕРЕДИНЕ и распознавателем не берётся —
				// а перенос в комментарии контракта есть норма, а не редкость.
				// Найдено инъекцией: законный близнец не был прочитан, то есть
				// его молчание ничего не доказывало.
				block = append(block, strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
				continue
			}
			// Поле `name` берёт комментарный блок, стоящий НЕПОСРЕДСТВЕННО перед ним.
			if nlbProtoNameFieldRe.MatchString(ln) && len(block) > 0 {
				tbl := nlbTableForProtoMessage(msg)
				if tbl != "" {
					joined := strings.Join(block, " ")
					if mt := nlbProtoScopeRe.FindStringSubmatch(joined); mt != nil {
						phrase := strings.ToLower(strings.TrimSpace(mt[1]))
						col, known := nlbScopeColumnByPhrase[phrase]
						if !known {
							col = "?" + phrase
						}
						c.Claims = append(c.Claims, nlbScopeClaim{
							file: rel, line: blockLine, table: tbl, column: col,
							kind: "контракт", text: "unique within the " + phrase,
						})
						c.ClaimsByKind["контракт"]++
					}
				}
			}
			block, blockLine = nil, 0
		}
	}

	// ── 3. УТВЕРЖДЕНИЯ ДОКУМЕНТАЦИИ ───────────────────────────────────────────
	for _, rel := range clientTruthTreeFiles(tree, "services/nlb/docs/content", true, ".mdx") {
		body, rerr := clientTruthReadTreeFile(tree, rel)
		if rerr != nil {
			return c, rerr
		}
		c.DocFiles++
		base := path.Base(rel)
		for i, ln := range strings.Split(string(body), "\n") {
			// (а) запись ограничения — опознаёт себя КОЛОНКОЙ, ресурс берётся из неё
			for _, mt := range nlbDocConstraintRe.FindAllStringSubmatch(ln, -1) {
				col := mt[1]
				if tbl := nlbTableForColumn(col); tbl != "" {
					c.Claims = append(c.Claims, nlbScopeClaim{
						file: rel, line: i + 1, table: tbl, column: col,
						kind: "документация (ограничение)", text: mt[0],
					})
					c.ClaimsByKind["документация (ограничение)"]++
				}
			}
			// (б) проза — ресурс берётся из слова в ТОЙ ЖЕ строке, иначе из страницы
			for _, mt := range nlbDocProseRe.FindAllStringSubmatch(ln, -1) {
				phrase := strings.ToLower(mt[1])
				col, known := nlbScopeColumnByPhrase[phrase]
				if !known {
					col = "?" + phrase
				}
				tbl := nlbTableForDocLine(ln, mt[0], base)
				if tbl == "" {
					continue
				}
				c.Claims = append(c.Claims, nlbScopeClaim{
					file: rel, line: i + 1, table: tbl, column: col,
					kind: "документация (проза)", text: strings.TrimSpace(mt[0]),
				})
				c.ClaimsByKind["документация (проза)"]++
			}
		}
	}
	return c, nil
}

func nlbTableForProtoMessage(msg string) string {
	for _, t := range nlbNameScopeTables {
		for _, p := range t.protoPrefix {
			if msg == p {
				return t.table
			}
		}
	}
	return ""
}

func nlbTableForColumn(col string) string {
	switch col {
	case "load_balancer_id":
		return "listeners"
	case "project_id":
		// project_id — область И балансировщика, И группы целей: колонка сама по
		// себе ресурс не различает, поэтому такое утверждение не атрибутируется.
		return ""
	}
	return ""
}

// nlbTableForDocLine — какому ресурсу принадлежит прозаическое утверждение.
//
// СЛОВО ОБЛАСТИ — НЕ СЛОВО РЕСУРСА, и различать их обязательно. «Имя уникально в
// рамках БАЛАНСИРОВЩИКА» на странице слушателя говорит о слушателе, а
// «балансировщик» в нём — это ОБЛАСТЬ. Первая редакция читала его как ресурс и
// объявляла верную строку нарушением: гейт, краснеющий на исправном, отключают
// первым. Поэтому фраза области вырезается из строки ДО поиска имени ресурса.
//
// Порядок: слово ресурса в остатке строки (сводные страницы говорят о нескольких
// ресурсах разом), затем — страница, если она посвящена одному.
func nlbTableForDocLine(line, scopePhrase, page string) string {
	rest := strings.ToLower(line)
	if scopePhrase != "" {
		rest = strings.ReplaceAll(rest, strings.ToLower(scopePhrase), " ")
	}
	for _, t := range nlbNameScopeTables {
		for _, w := range t.docWords {
			if strings.Contains(rest, w) {
				return t.table
			}
		}
	}
	for _, t := range nlbNameScopeTables {
		if page == t.docPage {
			return t.table
		}
	}
	return ""
}

// nlbNameScopeFindings — утверждения, расходящиеся с базой.
func nlbNameScopeFindings(c nlbNameScopeCensus) []string {
	var out []string
	for _, cl := range c.Claims {
		want, ok := c.IndexesDerived[cl.table]
		if !ok {
			continue // авторитета по этой таблице нет — судить не о чем
		}
		if cl.column != want {
			out = append(out, fmt.Sprintf(
				"%s:%d [%s] %q называет область %q, база держит %q (таблица %s)",
				cl.file, cl.line, cl.kind, cl.text, cl.column, want, cl.table))
		}
	}
	sort.Strings(out)
	return out
}
