// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// retiredenginedbnames.go — разбор «снятый движок прав не получает НОВЫХ имён в
// схеме».
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Внешний движок отношений снят стадией S6 эпика #747: вердикт о доступе
// вычисляет реляционная форма в собственной базе службы. Часть механизмов
// движка пережила его законно и работает по сей день — журнал намерений iam
// складывается триггером в прямой факт, очереди регистрации доменов доезжают до
// службы прав, — но ИМЕНА у них остались от движка.
//
// Имя, пережившее свой предмет, здесь уже стоило работы: по нему заведена
// задача #1667, а объяснять, почему слово в дереве законно, приходится в трёх
// местах сразу (шапка `authzengineretired.go`, шапка миграции
// `20260822160000_journal_without_delivery_columns.sql`, §7 приёмки R7-3).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТОТ ГЕЙТ СТЕРЕЖЁТ, А ЧТО НЕТ
//
// Он НЕ требует переименования того, что уже названо: имена стоят в применённых
// миграциях, править которые нельзя, и живая ведомость ниже описывает их как
// факт. Он стережёт РОСТ: новый объект схемы, взявший имя снятого движка,
// продлевает ложь ещё на одну миграцию — а именно так это и происходило, по
// одному объекту за раз, последний раз 2026-08-29.
//
// Ведомость истекает САМА и в обе стороны: она хранит точный состав, а не
// потолок. Объект прибавился — находка «новое имя»; объект ушёл (переименован
// или снят) — находка «ведомость пережила свой предмет», и правится она тем же
// изменением, которым ушёл объект.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРЕДИКАТ — ОБЪЕКТ СХЕМЫ, А НЕ СЛОВО В ДЕРЕВЕ
//
// Тот же довод, что у соседнего гейта Г6 и по той же причине: слово остаётся в
// дереве законно — файл модели прав `fga_model.fga` разбирает теперь сама
// служба (ЯЗЫК движка пережил движок и является источником истины формы),
// учётка посредника зовётся `iam_fgaproxy`, а разборы в шапках говорят о снятом
// движке в прошедшем времени и обязаны продолжать это делать. Гейт по слову
// краснел бы на исправном дереве и был бы снят первым же обходом.
//
// Объект схемы этой двусмысленности не имеет: он ЗАВОДИТСЯ оператором, его
// заведение видно в разборе, и вопрос «взял ли новый объект имя снятого
// движка» отвечается однозначно.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА НАЗВАНА ВСЛУХ
//
//  1. Гейт судит ОБЪЯВЛЕНИЕ, а не базу. Объект, заведённый мимо каталога
//     миграций (руками на стенде, посевом), ему не виден.
//  2. Гейт судит ИМЯ ОБЪЕКТА, а не значения в нём. Словарь событий журналов
//     (`fga.tuple.write`, `fga.register`) несёт то же слово в ДАННЫХ; это
//     отдельный предмет, и он здесь не решается — сказано, а не умолчано.
//  3. Гейт не знает, законно ли имя по существу. Он знает только, что состав
//     таких имён не менялся с момента, когда его переписали.

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// retiredEngineToken — след снятого движка в идентификаторе.
//
// Одно слово, а не перечень: имена объектов, доставшиеся от движка, все
// построены от него (`fga_outbox`, `fga_register_outbox`, `fga_model_version`).
const retiredEngineToken = "fga"

// RetiredEngineDatabaseObject — объект схемы, чьё имя несёт след снятого движка.
type RetiredEngineDatabaseObject struct {
	// Service — имя сервиса-владельца (каталог под services/).
	Service string
	// Kind — род объекта: TABLE, INDEX, TRIGGER, FUNCTION, SEQUENCE, CONSTRAINT, TYPE, VIEW.
	Kind string
	// Name — имя объекта без схемы.
	Name string
	// Migration — базовое имя миграции, которая объект завела.
	Migration string
}

// Key — устойчивое имя строки ведомости.
func (o RetiredEngineDatabaseObject) Key() string {
	return o.Service + " " + o.Kind + " " + o.Name
}

// RetiredEngineDatabaseCensus — объём осмотренного.
//
// Печатается ВСЕГДА: «ноль находок» обязано быть отличимо от «ноль
// прочитанного».
type RetiredEngineDatabaseCensus struct {
	// Files — прочитано файлов миграций.
	Files int
	// Services — сервисов, чей каталог миграций попал в обход.
	Services int
	// Statements — распознано операторов заведения и снятия объектов (любых, не
	// только помеченных следом движка).
	Statements int
	// Live — объектов со следом движка, живых после применения всей цепочки.
	Live int
}

func (c RetiredEngineDatabaseCensus) String() string {
	return fmt.Sprintf("прочитано файлов миграций %d · сервисов %d · распознано операторов %d · живых объектов со следом движка %d",
		c.Files, c.Services, c.Statements, c.Live)
}

var (
	reCreateObject = regexp.MustCompile(`(?is)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:UNIQUE\s+)?(TABLE|INDEX|TRIGGER|FUNCTION|SEQUENCE|TYPE|VIEW)\b(?:\s+IF\s+NOT\s+EXISTS)?\s+([a-zA-Z_][\w.]*)`)
	reDropObject   = regexp.MustCompile(`(?is)\bDROP\s+(TABLE|INDEX|TRIGGER|FUNCTION|SEQUENCE|TYPE|VIEW)\b(?:\s+IF\s+EXISTS)?\s+([a-zA-Z_][\w.]*)`)
	reAddConstr    = regexp.MustCompile(`(?is)\bCONSTRAINT\s+([a-zA-Z_]\w*)\s+(?:CHECK|PRIMARY|UNIQUE|FOREIGN|EXCLUDE)\b`)
	reDropConstr   = regexp.MustCompile(`(?is)\bDROP\s+CONSTRAINT\b(?:\s+IF\s+EXISTS)?\s+([a-zA-Z_]\w*)`)
	reRenameObject = regexp.MustCompile(`(?is)\bALTER\s+(TABLE|INDEX|SEQUENCE|TYPE|VIEW|TRIGGER)\b(?:\s+IF\s+EXISTS)?\s+(?:ONLY\s+)?([a-zA-Z_][\w.]*)\s+RENAME\s+TO\s+([a-zA-Z_][\w.]*)`)
	reRenameConstr = regexp.MustCompile(`(?is)\bRENAME\s+CONSTRAINT\s+([a-zA-Z_]\w*)\s+TO\s+([a-zA-Z_]\w*)`)
	reLeadingDigit = regexp.MustCompile(`^(\d+)`)
)

// FindRetiredEngineDatabaseObjects — живой состав объектов схемы, чьё имя несёт
// след снятого движка.
//
// sources — содержимое файлов миграций по путям вида
// `services/<svc>/internal/migrations/<файл>.sql`. Карта, а не дерево: инъекция
// подаёт свой вход, не заводя ни файла, ни репозитория.
//
// Цепочка применяется В ПОРЯДКЕ ВЕРСИЙ внутри каждого сервиса, поэтому снятый
// позже объект в живой состав не попадает, а переименованный попадает под НОВЫМ
// именем: разбор понимает RENAME, и потому переименование не делает ведомость
// беспредметной молча.
func FindRetiredEngineDatabaseObjects(sources map[string]string) ([]RetiredEngineDatabaseObject, RetiredEngineDatabaseCensus, error) {
	var census RetiredEngineDatabaseCensus

	bySvc := map[string][]string{}
	for p := range sources {
		svc, ok := serviceOfMigrationPath(p)
		if !ok {
			return nil, census, fmt.Errorf("путь %q не похож на файл миграции сервиса "+
				"(ожидается services/<svc>/internal/migrations/<файл>.sql)", p)
		}
		bySvc[svc] = append(bySvc[svc], p)
	}
	census.Files = len(sources)
	census.Services = len(bySvc)

	// live — состав по ключу; значение хранит саму строку ведомости.
	live := map[string]RetiredEngineDatabaseObject{}

	svcNames := make([]string, 0, len(bySvc))
	for svc := range bySvc {
		svcNames = append(svcNames, svc)
	}
	sort.Strings(svcNames)

	for _, svc := range svcNames {
		files := bySvc[svc]
		sort.Slice(files, func(i, j int) bool {
			vi, vj := migrationVersion(files[i]), migrationVersion(files[j])
			if vi != vj {
				return vi < vj
			}
			return files[i] < files[j]
		})
		for _, p := range files {
			up := stripSQLProse(upSection(sources[p]))
			base := path.Base(p)

			for _, m := range reCreateObject.FindAllStringSubmatch(up, -1) {
				census.Statements++
				name := bareName(m[2])
				if !carriesRetiredEngineToken(name) {
					continue
				}
				o := RetiredEngineDatabaseObject{Service: svc, Kind: strings.ToUpper(m[1]), Name: name, Migration: base}
				live[o.Key()] = o
			}
			for _, m := range reAddConstr.FindAllStringSubmatch(up, -1) {
				census.Statements++
				name := bareName(m[1])
				if !carriesRetiredEngineToken(name) {
					continue
				}
				o := RetiredEngineDatabaseObject{Service: svc, Kind: "CONSTRAINT", Name: name, Migration: base}
				live[o.Key()] = o
			}
			for _, m := range reRenameObject.FindAllStringSubmatch(up, -1) {
				census.Statements++
				kind, from, to := strings.ToUpper(m[1]), bareName(m[2]), bareName(m[3])
				old := RetiredEngineDatabaseObject{Service: svc, Kind: kind, Name: from}
				prev, had := live[old.Key()]
				delete(live, old.Key())
				if !carriesRetiredEngineToken(to) {
					continue
				}
				mig := base
				if had {
					mig = prev.Migration
				}
				o := RetiredEngineDatabaseObject{Service: svc, Kind: kind, Name: to, Migration: mig}
				live[o.Key()] = o
			}
			for _, m := range reRenameConstr.FindAllStringSubmatch(up, -1) {
				census.Statements++
				from, to := bareName(m[1]), bareName(m[2])
				old := RetiredEngineDatabaseObject{Service: svc, Kind: "CONSTRAINT", Name: from}
				prev, had := live[old.Key()]
				delete(live, old.Key())
				if !carriesRetiredEngineToken(to) {
					continue
				}
				mig := base
				if had {
					mig = prev.Migration
				}
				o := RetiredEngineDatabaseObject{Service: svc, Kind: "CONSTRAINT", Name: to, Migration: mig}
				live[o.Key()] = o
			}
			for _, m := range reDropObject.FindAllStringSubmatch(up, -1) {
				census.Statements++
				o := RetiredEngineDatabaseObject{Service: svc, Kind: strings.ToUpper(m[1]), Name: bareName(m[2])}
				delete(live, o.Key())
			}
			for _, m := range reDropConstr.FindAllStringSubmatch(up, -1) {
				census.Statements++
				o := RetiredEngineDatabaseObject{Service: svc, Kind: "CONSTRAINT", Name: bareName(m[1])}
				delete(live, o.Key())
			}
		}
	}

	out := make([]RetiredEngineDatabaseObject, 0, len(live))
	for _, o := range live {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	census.Live = len(out)
	return out, census, nil
}

// carriesRetiredEngineToken — несёт ли идентификатор след снятого движка.
//
// Токен ищется как СЕГМЕНТ имени, разделённого подчёркиванием, а не подстрокой:
// иначе под предикат попало бы всякое имя, внутри которого эти три буквы
// оказались случайно.
func carriesRetiredEngineToken(name string) bool {
	for _, seg := range strings.Split(strings.ToLower(name), "_") {
		if seg == retiredEngineToken {
			return true
		}
	}
	return false
}

// bareName — имя без схемы и без кавычек.
func bareName(ident string) string {
	ident = strings.Trim(ident, `"`)
	if i := strings.LastIndex(ident, "."); i >= 0 {
		ident = ident[i+1:]
	}
	return strings.Trim(ident, `"`)
}

// upSection — часть миграции, которая ПРИМЕНЯЕТСЯ.
//
// Down описывает откат и живого состава не задаёт; читать его значило бы
// считать снятое заведённым и наоборот.
func upSection(src string) string {
	if i := strings.Index(src, "-- +goose Down"); i >= 0 {
		return src[:i]
	}
	return src
}

// stripSQLProse — снимает построчные и блочные комментарии.
//
// Не украшение: миграции этого дерева несут длинные разборы, и `CREATE TABLE` в
// объяснении того, ПОЧЕМУ таблицы нет, дал бы фантомный объект. Одинарные
// кавычки уважаются, чтобы `--` внутри литерала не съел остаток строки.
func stripSQLProse(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	inLine, inBlock, inStr := false, false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				b.WriteByte(c)
			}
		case inBlock:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				inBlock = false
				i++
			}
		case inStr:
			b.WriteByte(c)
			if c == '\'' {
				inStr = false
			}
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			inLine = true
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			inBlock = true
			i++
		case c == '\'':
			inStr = true
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// serviceOfMigrationPath — сервис-владелец по пути файла миграции.
func serviceOfMigrationPath(p string) (string, bool) {
	parts := strings.Split(path.Clean(p), "/")
	for i := 0; i+4 < len(parts); i++ {
		if parts[i] == "services" && parts[i+2] == "internal" && parts[i+3] == "migrations" {
			return parts[i+1], true
		}
	}
	return "", false
}

// migrationVersion — числовая версия из имени файла; goose применяет цепочку по ней.
func migrationVersion(p string) uint64 {
	m := reLeadingDigit.FindStringSubmatch(path.Base(p))
	if m == nil {
		return ^uint64(0)
	}
	v, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return ^uint64(0)
	}
	return v
}
