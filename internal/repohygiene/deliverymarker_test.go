// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// deliverymarker_test.go — СЕМЬЯ, ОБЪЯВЛЕННАЯ РЕЕСТРОМ РОСТА, СХОДИТСЯ СО СХЕМОЙ.
//
// # Предмет — объявление, пережившее свой предмет
//
// Реестр `tableGrowthRegistry` объявляет по каждой живой таблице темп, вердикт и
// причину. Темп и вердикт — суждения человека, и соседний гейт проверяет лишь,
// что они вынесены и взяты из закрытого словаря: «верно ли» там непроверяемо.
//
// СЕМЬЯ — третья ось, и она другая: у неё есть машинный признак. Очередь дренажа
// помечает доставленную строку колонкой [DeliveryMarkerColumn]; журнал подписки
// читают курсором по номеру позиции, и колонки этой у него нет. Значит
// объявление семьи проверяемо по применённым миграциям — и обязано проверяться,
// потому что цена ошибки уже заплачена.
//
// # Что наблюдалось (замер на ветке, 2026-08-31)
//
// Тринадцать записей реестра несли ДОСЛОВНО ОДНУ причину — «очередь дренажа:
// строка заводится в writer-транзакции мутации, дренаж помечает доставленную
// sent_at и не удаляет её никогда». Для ШЕСТИ из тринадцати это неверно:
//
//   - у четырёх (`compute_outbox`, `geo_outbox`, `nlb_outbox`, `vpc_outbox`)
//     колонки-признака нет и НИКОГДА не было: форма у них другая — `sequence_no`
//     и `processed_at`, — а читают их курсором либо не читают вовсе;
//   - у двух (`fga_outbox`, `subject_change_outbox` в iam) колонка БЫЛА и снята
//     ПРИМЕНЁННЫМИ миграциями вместе с дренажом, которому принадлежала.
//
// Реестр читают как перечень предметов работы: по нему делят полосы и по нему
// пишут предикат уборки. Один предикат на все тринадцать неразбираем у шести —
// `42703` на каждом проходе, — то есть ошибка объявления стоила бы полосы.
//
// # Проверка ДВУСТОРОННЯЯ, и это несущее
//
// Односторонняя («у очереди признак обязан быть») зеленела бы на реестре,
// объявившем журналом всё подряд. Поэтому вторая половина утверждает обратное: у
// журнала признака обязано НЕ быть. Обе половины падают на своей инъекции.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// deliveryMarkerVerdict — ЧИСТОЕ суждение по уже прочитанному дереву.
//
// Отделено от обхода намеренно: инъекция подаёт сюда синтетическое состояние и
// проверяет, что суждение способно упасть И способно смолчать. На настоящем
// дереве ни того ни другого не показать, не сломав его.
func deliveryMarkerVerdict(
	marker map[TableRef]bool,
	registry []TableGrowthDecl,
) (findings []string, classified, queues, journals int) {
	for _, e := range registry {
		if e.Family == familyUnclassified {
			continue
		}
		classified++
		ref := TableRef{Owner: e.Owner, Name: e.Table}
		has := marker[ref]
		switch e.Family {
		case familyDrainerQueue:
			queues++
			if !has {
				findings = append(findings, ref.String()+
					" — реестр объявляет её ОЧЕРЕДЬЮ ДРЕНАЖА, но колонки `"+DeliveryMarkerColumn+
					"` у неё нет: применённые миграции её не заводят либо уже сняли. "+
					"Уборка по признаку доставки на такой таблице не разберётся вовсе "+
					"(`42703` на каждом проходе), а причина записи обещает механизм, "+
					"которого нет. Исход — назвать настоящую семью, а не поправить причину")
			}
		case familyJournal:
			journals++
			if has {
				findings = append(findings, ref.String()+
					" — реестр объявляет её ЖУРНАЛОМ, но колонка `"+DeliveryMarkerColumn+
					"` у неё ЕСТЬ. Журнал читают курсором по номеру позиции; признак доставки "+
					"означает, что у строки есть адресат, который её применяет, — то есть это "+
					"очередь, и предикат её уборки другой")
			}
		default:
			findings = append(findings, ref.String()+
				" — семья объявлена значением вне закрытого словаря ("+string(e.Family)+")")
		}
	}
	return findings, classified, queues, journals
}

// readDeliveryMarkerTree читает миграции В ПОРЯДКЕ ПРИМЕНЕНИЯ и отвечает, какие
// таблицы несут признак доставки СЕГОДНЯ.
func readDeliveryMarkerTree(t *testing.T, root string) (map[TableRef]bool, DeliveryMarkerCensus) {
	t.Helper()

	var migrations []string
	for _, sub := range tableGrowthRoots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		files, err := treecorpus.UnderWithSuffix(base, ".sql")
		if err != nil {
			t.Fatalf("состав дерева под %s: %v", base, err)
		}
		for _, path := range files {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				t.Fatalf("относительный путь %s: %v", path, rerr)
			}
			rel = filepath.ToSlash(rel)
			if !strings.Contains(rel, "/migrations/") {
				continue
			}
			migrations = append(migrations, rel)
		}
	}
	// Порядок применения: внутри каталога — по имени файла, каталоги между собой
	// независимы (у каждого владельца своя база).
	sort.Slice(migrations, func(i, j int) bool {
		di, dj := filepath.Dir(migrations[i]), filepath.Dir(migrations[j])
		if di != dj {
			return di < dj
		}
		return filepath.Base(migrations[i]) < filepath.Base(migrations[j])
	})

	var (
		census DeliveryMarkerCensus
		events []ColumnEvent
	)
	for _, rel := range migrations {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		ev, c := ScanDeliveryMarker(MigrationOwnerOf(filepath.Dir(rel)), rel, body)
		census.Add(c)
		events = append(events, ev...)
	}
	return FoldDeliveryMarker(events), census
}

// TestGrowthRegistryFamilyMatchesTheSchema — сам гейт.
func TestGrowthRegistryFamilyMatchesTheSchema(t *testing.T) {
	root := repoRoot(t)
	marker, census := readDeliveryMarkerTree(t, root)
	findings, classified, queues, journals := deliveryMarkerVerdict(marker, tableGrowthRegistry)

	present := 0
	for _, has := range marker {
		if has {
			present++
		}
	}

	// Перепись печатает ОБЕ величины — сколько осмотрено и сколько с признаком, —
	// потому что одно число не отличает «ноль находок» от «ноль прочитанного».
	t.Logf("перепись реестра: записей %d, из них классифицировано %d "+
		"(очередей дренажа %d · журналов %d); находок %d",
		len(tableGrowthRegistry), classified, queues, journals, len(findings))
	t.Logf("перепись обхода: миграций прочитано %d, тел CREATE TABLE разобрано %d; "+
		"объявлений признака `%s` в теле создания %d, добавлений ALTER %d, снятий ALTER %d; "+
		"таблиц несут признак СЕГОДНЯ %d",
		census.MigrationFiles, census.CreateBodies, DeliveryMarkerColumn,
		census.Declared, census.Added, census.Dropped, present)

	// ПРЕДПОСЫЛКИ РАЗБОРА. Каждая — факт о дереве; факт меняется, и тогда запрет
	// становится ложью. Пусть гейт заявляет их сам.
	if census.MigrationFiles == 0 {
		t.Fatal("прочитано ноль миграций — гейт не читал дерева, и его молчание ничего не значит")
	}
	if census.CreateBodies == 0 {
		t.Fatal("не разобрано ни одного тела `CREATE TABLE` — предпосылка 1 (колонка объявляется " +
			"телом создания либо ALTER) перестала быть верной, и всякая очередь была бы " +
			"объявлена лишённой признака")
	}
	if census.Declared == 0 {
		t.Fatalf("ни одна таблица дерева не объявляет колонку `%s` в теле создания — разбор "+
			"разъехался со схемой. Очереди дренажа в этом дереве есть (их дренажит "+
			"`pkg/outbox/drainer`, единственный писатель этой колонки), значит ноль здесь "+
			"означает слепоту, а не отсутствие предмета", DeliveryMarkerColumn)
	}
	if census.Dropped == 0 {
		t.Fatalf("снятий колонки `%s` не найдено ни одного — а разбор именно ими и отличает "+
			"очередь от журнала, ПЕРЕСТАВШЕГО быть очередью. В этом дереве снятие произошло "+
			"дважды (iam `fga_outbox` и `subject_change_outbox`); ноль означает, что ветвь "+
			"снятия не читается", DeliveryMarkerColumn)
	}
	if classified == 0 {
		t.Fatal("ни одна запись реестра не объявляет семью — гейт судит пустое множество, " +
			"и его молчание неотличимо от исправности")
	}
	if queues == 0 || journals == 0 {
		t.Fatalf("реестр объявляет очередей %d и журналов %d — обе стороны обязаны быть "+
			"непусты, иначе проверяется лишь одна половина, а вторая зеленеет вакуумно",
			queues, journals)
	}

	for _, f := range findings {
		t.Errorf("семья реестра разошлась со схемой: %s", f)
	}
}
