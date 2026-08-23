// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// settledwatermarksingularity_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, и того, что он молчит на законных близнецах.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`settledwatermarksingularity_test.go`) ничего не говорит о способности
// проверки падать — зелёный получает и та, что не смотрит никуда.
//
// Каждое утверждение стоит ПАРОЙ: внесённый дефект обязан краснеть И НАЗЫВАТЬ
// координату, а законный близнец той же формы — молчать. Без второй половины
// гейт ловил бы форму, а не существо, и первый же ложный срабат его отключил бы.
//
// Близнецов здесь ДВА, и оба взяты из настоящих промахов, а не выдуманы:
//
//   - признаки в КОММЕНТАРИИ (так устроена шапка самого анализатора — он
//     объясняет, что ищет, теми же словами);
//   - признаки тремя ОТДЕЛЬНЫМИ литералами (так устроено объявление признаков
//     данными — на нём анализатор находил сам себя, пока предикат считал их по
//     файлу, а не по одному тексту запроса).
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// observerSQL — текст наблюдения: все три несущих признака в ОДНОМ литерале.
// Ровно эта форма и есть предмет гейта.
const observerSQL = "`\n" + `
	SELECT COALESCE((SELECT max(position) FROM journal), 0),
	       COALESCE((SELECT array_agg(DISTINCT l.virtualtransaction)
	                   FROM pg_locks l
	                  WHERE l.mode = 'RowExclusiveLock'
	                    AND l.granted
	                    AND l.pid <> pg_backend_pid()), '{}'::text[])` + "`"

type watermarkStand struct {
	root string
}

func newWatermarkStand(t *testing.T) *watermarkStand {
	t.Helper()
	s := &watermarkStand{root: t.TempDir()}
	// Законное состояние: единственный наблюдатель, и он в фундаменте.
	s.write(t, "pkg/subscription/watermark.go",
		"package subscription\n\nconst watermarkSQL = "+observerSQL+"\n")
	// Наполнитель: обход обязан быть непустым и без него.
	s.write(t, "services/nlb/internal/repo/repo.go",
		"package repo\n\nconst listSQL = `SELECT id FROM load_balancers ORDER BY id`\n")
	return s
}

func (s *watermarkStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *watermarkStand) audit(t *testing.T, allow ...SettledWatermarkAllowance) ([]SettledWatermarkFinding, SettledWatermarkCensus) {
	t.Helper()
	var log strings.Builder
	findings, census, err := AuditSettledWatermarkSingularity(SettledWatermarkOptions{
		Root:    s.root,
		GoRoots: []string{"pkg", "services", "internal"},
		Allow:   allow,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return findings, census
}

func kindsOf(findings []SettledWatermarkFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Kind)
	}
	return out
}

func hasKind(findings []SettledWatermarkFinding, kind string) bool {
	for _, f := range findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func findingNaming(findings []SettledWatermarkFinding, kind, where string) bool {
	for _, f := range findings {
		if f.Kind == kind && strings.Contains(f.Where, where) {
			return true
		}
	}
	return false
}

// TestWatermarkGateIsSilentOnTheLawfulTree — положительный контроль.
//
// Без него всякое отрицание ниже зеленело бы и на анализаторе, который считает
// находкой всё подряд.
func TestWatermarkGateIsSilentOnTheLawfulTree(t *testing.T) {
	s := newWatermarkStand(t)
	findings, census := s.audit(t)
	if len(findings) != 0 {
		t.Fatalf("законное дерево дало находки %v", kindsOf(findings))
	}
	if census.Observers != 1 || census.InHome != 1 {
		t.Fatalf("наблюдателей %d (в фундаменте %d), ожидался ровно один в фундаменте",
			census.Observers, census.InHome)
	}
}

// TestWatermarkGateRedsWhenTheTechniqueIsGone — ПЕРВАЯ половина: утрата.
//
// Направление, ради которого гейт заведён: снятие мёртвого читателя не должно
// уносить единственный написанный ответ на класс.
func TestWatermarkGateRedsWhenTheTechniqueIsGone(t *testing.T) {
	s := newWatermarkStand(t)
	if err := os.Remove(filepath.Join(s.root, "pkg/subscription/watermark.go")); err != nil {
		t.Fatalf("снять наблюдателя: %v", err)
	}
	findings, census := s.audit(t)
	if !hasKind(findings, "ТЕХНИКА-ИСЧЕЗЛА") {
		t.Fatalf("техника снята, а гейт молчит: находки %v, наблюдателей %d",
			kindsOf(findings), census.Observers)
	}
}

// TestWatermarkGateRedsOnASecondObserverOutsideTheFoundation — ВТОРАЯ половина:
// форк. Это ровно состояние дерева до снятия мёртвого читателя nlb (kacho#1043).
func TestWatermarkGateRedsOnASecondObserverOutsideTheFoundation(t *testing.T) {
	s := newWatermarkStand(t)
	s.write(t, "services/nlb/internal/repo/kacho/pg/lifecycle_feed.go",
		"package pg\n\nconst feedSQL = "+observerSQL+"\n")

	findings, _ := s.audit(t)
	if !findingNaming(findings, "НАБЛЮДАТЕЛЬ-ВНЕ-ФУНДАМЕНТА", "lifecycle_feed.go") {
		t.Fatalf("второй наблюдатель вне фундамента не назван координатой: %v", findings)
	}
	if !hasKind(findings, "ВТОРОЙ-НАБЛЮДАТЕЛЬ") {
		t.Fatalf("второй наблюдатель не посчитан: %v", kindsOf(findings))
	}
}

// TestWatermarkGateRedsOnASecondObserverInsideTheFoundation — форк БЛИЖЕ.
//
// Два наблюдателя в самом фундаменте — то же расхождение; гейт, судящий только
// «лежит ли вне pkg/», его пропустил бы.
func TestWatermarkGateRedsOnASecondObserverInsideTheFoundation(t *testing.T) {
	s := newWatermarkStand(t)
	s.write(t, "pkg/feed/watermark.go",
		"package feed\n\nconst feedSQL = "+observerSQL+"\n")

	findings, census := s.audit(t)
	if !hasKind(findings, "ВТОРОЙ-НАБЛЮДАТЕЛЬ") {
		t.Fatalf("два наблюдателя в фундаменте — гейт молчит: находки %v, наблюдателей %d",
			kindsOf(findings), census.Observers)
	}
}

// TestWatermarkGateIsSilentWhenTheMarkersLiveInAComment — ЗАКОННЫЙ БЛИЗНЕЦ 1.
//
// Так устроена шапка самого анализатора. Гейт, читающий текст, покраснел бы на
// собственном объяснении.
func TestWatermarkGateIsSilentWhenTheMarkersLiveInAComment(t *testing.T) {
	s := newWatermarkStand(t)
	s.write(t, "services/nlb/internal/repo/kacho/pg/doc.go", `package pg

// Здесь про наблюдение границы РАССКАЗАНО: писатель держит RowExclusiveLock с
// момента планирования вставки, virtualtransaction опознаёт транзакцию, а
// pg_backend_pid исключает себя. Ни одного запроса тут нет.
`)
	findings, census := s.audit(t)
	if len(findings) != 0 {
		t.Fatalf("признаки в комментарии приняты за наблюдателя: находки %v, наблюдателей %d",
			kindsOf(findings), census.Observers)
	}
}

// TestWatermarkGateIsSilentWhenTheMarkersAreSeparateLiterals — ЗАКОННЫЙ БЛИЗНЕЦ 2.
//
// Так устроено объявление признаков данными в самом анализаторе. Пока предикат
// считал признаки ПО ФАЙЛУ, гейт находил сам себя; после уточнения до ОДНОГО
// текста запроса — молчит. Эта проба и есть охрана уточнения: верни счёт по
// файлу — она покраснеет.
func TestWatermarkGateIsSilentWhenTheMarkersAreSeparateLiterals(t *testing.T) {
	s := newWatermarkStand(t)
	s.write(t, "internal/repohygiene/markers.go", `package repohygiene

var markers = []string{
	"RowExclusiveLock",
	"virtualtransaction",
	"pg_backend_pid",
}
`)
	findings, census := s.audit(t)
	if len(findings) != 0 {
		t.Fatalf("три отдельных литерала приняты за текст наблюдения: находки %v, наблюдателей %d",
			kindsOf(findings), census.Observers)
	}
}

// TestWatermarkGateIsSilentOnAPartialMarkerSet — близнец 3: не всякий, кто
// трогает pg_locks, наблюдает границу.
//
// Файл с ОДНИМ признаком техникой не является: сними любой из трёх — наблюдение
// перестаёт быть верным, оставаясь похожим.
func TestWatermarkGateIsSilentOnAPartialMarkerSet(t *testing.T) {
	s := newWatermarkStand(t)
	s.write(t, "services/nlb/internal/jobs/locks.go",
		"package jobs\n\nconst q = `SELECT pid FROM pg_locks WHERE mode = 'RowExclusiveLock'`\n")

	findings, census := s.audit(t)
	if len(findings) != 0 {
		t.Fatalf("частичный набор признаков принят за наблюдателя: находки %v, наблюдателей %d",
			kindsOf(findings), census.Observers)
	}
}

// TestWatermarkGateRefusesAnEmptyWalk — «ноль находок» обязано быть отличимо от
// «ноль прочитанного».
func TestWatermarkGateRefusesAnEmptyWalk(t *testing.T) {
	var log strings.Builder
	_, census, err := AuditSettledWatermarkSingularity(SettledWatermarkOptions{
		Root:    t.TempDir(),
		GoRoots: []string{"pkg", "services"},
	}, &log)
	if err == nil {
		t.Fatalf("пустой обход принят за зелёный вердикт: файлов %d", census.GoFiles)
	}
}

// TestWatermarkAllowanceWithoutAReasonIsItselfAFinding — послабление обязано
// нести причину и предикат истечения.
func TestWatermarkAllowanceWithoutAReasonIsItselfAFinding(t *testing.T) {
	s := newWatermarkStand(t)
	s.write(t, "services/nlb/internal/repo/kacho/pg/lifecycle_feed.go",
		"package pg\n\nconst feedSQL = "+observerSQL+"\n")

	findings, _ := s.audit(t, SettledWatermarkAllowance{
		File: "services/nlb/internal/repo/kacho/pg/lifecycle_feed.go",
	})
	if !hasKind(findings, "ПОСЛАБЛЕНИЕ-БЕЗ-ПРИЧИНЫ") {
		t.Fatalf("послабление без причины принято: %v", kindsOf(findings))
	}
}

// TestWatermarkAllowanceSilencesItsOwnSubject — послабление С причиной работает.
//
// Без этой половины предыдущая проба зеленела бы и на ведомости, которая не
// действует вовсе.
func TestWatermarkAllowanceSilencesItsOwnSubject(t *testing.T) {
	s := newWatermarkStand(t)
	s.write(t, "services/nlb/internal/repo/kacho/pg/lifecycle_feed.go",
		"package pg\n\nconst feedSQL = "+observerSQL+"\n")

	findings, _ := s.audit(t, SettledWatermarkAllowance{
		File:    "services/nlb/internal/repo/kacho/pg/lifecycle_feed.go",
		Because: "перенос техники в фундамент идёт фазой X; истекает её вливанием",
	})
	if findingNaming(findings, "НАБЛЮДАТЕЛЬ-ВНЕ-ФУНДАМЕНТА", "lifecycle_feed.go") {
		t.Fatalf("послабление с причиной не подействовало: %v", findings)
	}
}

// TestWatermarkAllowanceWithNothingToExcuseIsAFinding — САМОИСТЕЧЕНИЕ.
//
// Запись, которой больше нечего исключать, переживёт своё снятие и разрешит
// следующему завести свой наблюдатель под тем же оправданием.
func TestWatermarkAllowanceWithNothingToExcuseIsAFinding(t *testing.T) {
	s := newWatermarkStand(t)
	findings, _ := s.audit(t, SettledWatermarkAllowance{
		File:    "services/nlb/internal/repo/kacho/pg/lifecycle_feed.go",
		Because: "перенос техники в фундамент идёт фазой X; истекает её вливанием",
	})
	if !findingNaming(findings, "ПОСЛАБЛЕНИЕ-БЕЗ-ПРЕДМЕТА", "lifecycle_feed.go") {
		t.Fatalf("послабление без предмета не найдено: %v", findings)
	}
}
