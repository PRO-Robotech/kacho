// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dropguardproducer_test.go — у стража сноса колонок обязан быть производитель.
//
// # Предмет
//
// Снос колонки необратим, и решается он ЧИСЛОМ: страж проигрывает цепочку
// миграций в пустой Postgres, останавливается на версии перед каждым сносом и
// считает строки, сверяя счёт с объявленным в dropguard.json. Утверждение
// сильное — и ровно поэтому его молчание дорого стоит.
//
// Цепочка от манифеста до вердикта состоит из ТРЁХ звеньев, и обрыв любого
// выглядит одинаково зелёным:
//
//	манифест dropguard.json  →  проба, которая его читает  →  прогон, который эту пробу зовёт
//
// Третье звено и было оборвано (задача #1649). Пробы этих пакетов гейтятся
// кратким режимом (им нужен настоящий Postgres), а отбор интеграционной джобы
// идёт ПО ПУТИ — `services/<svc>/internal/(repo|clients|reconciler|
// subscriptionjournal)` — и до `internal/migrations` не достаёт вовсе. То есть
// пробы не исполняла ни одна джоба: под кратким страж честно печатает «измерено
// 0 из N», помечает себя пропущенным и выходит нулём, а пакет с нулём
// исполненных проб печатает `ok`.
//
// # Почему этот гейт, а не один соседний
//
// pgoutsideselection_test.go сверяет ДРУГУЮ пару: освобождение от переписи
// долга ↔ запись в PG_OUTSIDE_SELECTION_PKGS. Он отвечает на вопрос «согласны
// ли между собой два объявления», и на дереве, где стража сноса не гоняет никто,
// он ЗЕЛЁН — потому что оба объявления согласно молчат.
//
// Здесь предмет другой и приходит он со стороны ДЕРЕВА, а не объявлений: в
// дереве лежит манифест, то есть служба заявила, что её сносы решаются счётом.
// Гейт требует, чтобы у этого заявления был исполнитель. Манифест — вход,
// которого ни одно объявление не производит, поэтому подделать согласие нельзя:
// запись убирают из обоих списков, и находка остаётся.
//
// # Самоистечение
//
// Гейт выводит перечень ИЗ ДЕРЕВА (обход `services/*/internal/migrations`), а не
// выписывает. Новая служба, заведшая манифест сноса и не подключившая
// производителя, краснеет сама; служба, снявшая манифест, уходит из-под гейта
// сама. Выписанный перечень пережил бы свой предмет молча — тот самый класс,
// ради которого гейт стоит.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает,
// сколько манифестов нашёл и сколько из них дошло до производителя. Пустой обход
// — провал: манифестов в этом дереве пять, и ноль означал бы, что сломан обход,
// а не что стражей не стало.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// dropGuardManifest — имя манифеста, которым служба объявляет свои сносы.
const dropGuardManifest = "dropguard.json"

// dropGuardRunner — пакет общего стража. Проба, которая его не зовёт, манифест
// не читает, сколько бы файлов ни лежало рядом.
const dropGuardRunner = "pkg/dropguard/dropguardtest"

// dropGuardOwner — служба, объявившая сносы манифестом, и то, что при ней есть.
type dropGuardOwner struct {
	Pkg      string // services/<svc>/internal/migrations
	HasProbe bool   // рядом лежит проба, зовущая общего стража
}

// TestEveryDropGuardManifestHasAProducer — вердикт на настоящем дереве.
func TestEveryDropGuardManifestHasAProducer(t *testing.T) {
	root := repoRoot(t)
	owners := dropGuardOwners(t, root)
	if len(owners) == 0 {
		t.Fatal("обход не нашёл НИ ОДНОГО манифеста " + dropGuardManifest + " — " +
			"перепись беспредметна, а молчание такого гейта неотличимо от согласия")
	}

	findings := judgeDropGuardProducers(owners, shortGatedRunByOwnCIStep, shortGatedOutsideSelection)
	for _, f := range findings {
		t.Errorf("%s", f)
	}

	produced := 0
	for _, o := range owners {
		if o.HasProbe && shortGatedRunByOwnCIStep[o.Pkg] == pgOutsideMakeTarget {
			produced++
		}
	}
	t.Logf("перепись: манифестов сноса — %d, из них с пробой и производителем — %d",
		len(owners), produced)
}

// judgeDropGuardProducers — решающая часть, вынесенная из вердикта, чтобы её
// можно было проверить подставными входами, а не только зелёным деревом.
func judgeDropGuardProducers(owners []dropGuardOwner, exemptions map[string]string, debt []string) []string {
	inDebt := map[string]bool{}
	for _, p := range debt {
		inDebt[p] = true
	}

	var findings []string
	for _, o := range owners {
		if !o.HasProbe {
			findings = append(findings,
				o.Pkg+" объявляет сносы манифестом "+dropGuardManifest+", но пробы, зовущей "+
					dropGuardRunner+", рядом нет — манифест не читает никто, и его числа "+
					"не сверяет ничто")
			continue
		}
		if inDebt[o.Pkg] {
			findings = append(findings,
				o.Pkg+" объявляет сносы манифестом "+dropGuardManifest+", а числится ДОЛГОМ "+
					"в shortGatedOutsideSelection — то есть его страж не исполняется нигде: "+
					"под кратким он печатает «измерено 0 из N» и выходит нулём, а пакет с "+
					"нулём исполненных проб печатает ok. Снос колонки необратим, и решать "+
					"его нечему")
			continue
		}
		if exemptions[o.Pkg] != pgOutsideMakeTarget {
			findings = append(findings,
				o.Pkg+" объявляет сносы манифестом "+dropGuardManifest+", но производителя у "+
					"его стража нет: shortGatedRunByOwnCIStep не ссылается на "+
					pgOutsideMakeTarget+", а отбор интеграционной джобы идёт по пути и до "+
					"internal/migrations не достаёт")
		}
	}
	sort.Strings(findings)
	return findings
}

// dropGuardOwners — обход дерева. Перечень ВЫВОДИТСЯ, а не выписывается.
func dropGuardOwners(t *testing.T, root string) []dropGuardOwner {
	t.Helper()
	services, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatalf("не прочитан каталог служб: %v", err)
	}

	var out []dropGuardOwner
	for _, svc := range services {
		if !svc.IsDir() {
			continue
		}
		dir := filepath.Join(root, "services", svc.Name(), "internal", "migrations")
		if _, statErr := os.Stat(filepath.Join(dir, dropGuardManifest)); statErr != nil {
			continue
		}
		out = append(out, dropGuardOwner{
			Pkg:      "services/" + svc.Name() + "/internal/migrations",
			HasProbe: dirCallsDropGuardRunner(t, dir),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pkg < out[j].Pkg })
	return out
}

// dirCallsDropGuardRunner — есть ли в каталоге проба, ИМПОРТИРУЮЩАЯ общего
// стража. Сверяется путь импорта, а не имя файла: файл можно назвать как угодно,
// а импорта без чтения манифеста не бывает.
func dirCallsDropGuardRunner(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("не прочитан каталог цепочки %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		// #nosec G304 -- читается файл проб этого же репозитория.
		raw, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			t.Fatalf("не прочитан %s: %v", e.Name(), readErr)
		}
		if strings.Contains(string(raw), `"github.com/PRO-Robotech/kacho/`+dropGuardRunner+`"`) {
			return true
		}
	}
	return false
}

// TestDropGuardProducerJudgeFiresAndStaysSilent — инъекция в обе стороны
// подставными входами.
//
// Гейт выше на дереве зелёный, и зелёный сам по себе не значит ничего: ровно так
// же он выглядел бы, не умей вердикт краснеть. Каждая ось обрыва цепочки
// проверяется ОТДЕЛЬНО — инъекция, роняющая всё разом, не отличила бы гейт,
// умеющий одно, от гейта, умеющего три.
func TestDropGuardProducerJudgeFiresAndStaysSilent(t *testing.T) {
	const pkg = "services/storage/internal/migrations"
	const other = "services/vpc/internal/migrations"
	wired := []dropGuardOwner{{Pkg: pkg, HasProbe: true}, {Pkg: other, HasProbe: true}}
	exempt := map[string]string{pkg: pgOutsideMakeTarget, other: pgOutsideMakeTarget}

	t.Run("манифест, проба и производитель на месте — молчит", func(t *testing.T) {
		if got := judgeDropGuardProducers(wired, exempt, nil); len(got) != 0 {
			t.Fatalf("законный вход помечен: %v", got)
		}
	})

	t.Run("манифест без пробы — находка", func(t *testing.T) {
		got := judgeDropGuardProducers(
			[]dropGuardOwner{{Pkg: pkg, HasProbe: false}}, exempt, nil)
		if len(got) != 1 {
			t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(got), got)
		}
		if !strings.Contains(got[0], pkg) || !strings.Contains(got[0], dropGuardRunner) {
			t.Fatalf("находка не называет ни виновника, ни того, чего не хватает: %v", got[0])
		}
	})

	t.Run("манифест, числящийся долгом, — находка", func(t *testing.T) {
		got := judgeDropGuardProducers(
			[]dropGuardOwner{{Pkg: pkg, HasProbe: true}}, map[string]string{}, []string{pkg})
		if len(got) != 1 {
			t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(got), got)
		}
		if !strings.Contains(got[0], "ДОЛГОМ") {
			t.Fatalf("находка не называет, ЧЕМ именно пакет объявлен: %v", got[0])
		}
	})

	t.Run("манифест без производителя — находка", func(t *testing.T) {
		got := judgeDropGuardProducers(
			[]dropGuardOwner{{Pkg: pkg, HasProbe: true}}, map[string]string{}, nil)
		if len(got) != 1 {
			t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(got), got)
		}
		if !strings.Contains(got[0], pgOutsideMakeTarget) {
			t.Fatalf("находка не называет недостающего производителя: %v", got[0])
		}
	})

	t.Run("чужая цель в освобождении производителем не считается", func(t *testing.T) {
		got := judgeDropGuardProducers(
			[]dropGuardOwner{{Pkg: pkg, HasProbe: true}},
			map[string]string{pkg: "make test-authz-fga"}, nil)
		if len(got) != 1 {
			t.Fatalf("освобождение чужой целью принято за своё: %v", got)
		}
	})

	t.Run("служба без манифеста под гейт не подпадает", func(t *testing.T) {
		// Предпосылка гейта: он судит ТОЛЬКО тех, кто заявил счёт манифестом.
		// Служба, сносов не объявлявшая, обвинений не получает.
		//
		// Сегодня это не послабление, а совпадение с фактом: манифестов пять, и
		// столько же служб дерева сносят таблицы. У geo и registry сносов В
		// НАКАТЫВАЕМОМ НАПРАВЛЕНИИ ноль — предикат считает `DROP TABLE` внутри
		// секции `-- +goose Up`, а не по всему файлу: снос в секции отката это
		// сам откат, и стражу он не предмет. Широкий поиск по обоим направлениям
		// даёт у этих двух ненулевое число и создаёт находку на пустом месте —
		// проверено и отвергнуто при заведении гейта.
		//
		// Если служба начнёт сносить таблицы, не заведя манифеста, ЭТОТ гейт
		// промолчит: его вход — манифест. Такую полосу закрывает страж дерева
		// (pkg/dropguard), а не он.
		if got := judgeDropGuardProducers(nil, map[string]string{}, nil); len(got) != 0 {
			t.Fatalf("пустой обход дал находки: %v", got)
		}
	})
}
