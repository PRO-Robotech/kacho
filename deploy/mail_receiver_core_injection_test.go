// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// mail_receiver_core_injection_test.go — доказательство того, что суждения
// mail_receiver_core_test.go СПОСОБНЫ дать находку и СПОСОБНЫ смолчать.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО ОТДЕЛЬНЫЙ ФАЙЛ, А НЕ СТРОКА В ГЕЙТЕ
//
// Гейт читает дерево и на исправном дереве молчит. Молчание гейта, потерявшего
// способность краснеть, выглядит ТОЧНО ТАК ЖЕ. Отличить их можно только подав
// дефект — и подать его надо ТЕМ ЖЕ функциям, которые судят дерево, иначе
// доказательство доказывало бы себя.
//
// ВХОД БЕРЁТСЯ ИЗ ДЕРЕВА, А НЕ ВЫДУМЫВАЕТСЯ. Дефектные величины ниже — это
// дословно то, что стояло в профилях до этой правки: `mailhog.kacho.svc` (узел,
// которого не поднимал ни один манифест) и `disable_starttls=true` (полоса,
// объявленная незашифрованной). Синтетика, «похожая на дефект», доказала бы
// меньше: она проверяет распознаватель на входе, которого дерево не производит.
//
// ЗАКОННЫЙ БЛИЗНЕЦ ЕСТЬ У КАЖДОЙ ОСИ. Без него отрицание зеленело бы на всём
// сломанном: гейт, отвергающий любую полосу, «находит» и работающую.

import (
	"strings"
	"testing"
)

// receiverSuffixUnderTest — тот же суффикс, что гейт выводит из манифеста.
// Здесь он подаётся ВХОДОМ: предмет доказательства — суждение, а не чтение.
const receiverSuffixUnderTest = "-mailpit"

func TestInjection_UnraisedInClusterHostIsFound(t *testing.T) {
	cases := []struct {
		name, uri string
		wantFind  string // "" ⇒ гейт обязан молчать
	}{
		// ── ДЕФЕКТЫ ИЗ ДЕРЕВА ────────────────────────────────────────────────
		{"узел, которого никто не поднимает (дословно из профилей)",
			"smtp://mailhog.kacho.svc:1025/?disable_starttls=true", "mailhog.kacho.svc"},
		{"он же короткой формой", "smtp://mailhog:1025/", "mailhog"},
		{"он же полным именем службы",
			"smtps://mailhog.kacho.svc.cluster.local:465/", "mailhog.kacho.svc.cluster.local"},
		{"чужой внутрикластерный узел с учётными данными",
			"smtp://user:pass@relay.kacho.svc:1025/", "relay.kacho.svc"},

		// ── ЗАКОННЫЕ БЛИЗНЕЦЫ ───────────────────────────────────────────────
		{"приёмник, который поставка ПОДНИМАЕТ", "smtp://kacho-umbrella-mailpit:1025/", ""},
		{"он же полным именем", "smtp://kacho-umbrella-mailpit.kacho.svc:1025/", ""},
		{"ВНЕШНИЙ ретранслятор боевой площадки — не наш предмет",
			"smtps://smtp.example.com:465/", ""},
		{"внешний ретранслятор с учётными данными",
			"smtps://user:pass@smtp.example.com:465/", ""},
		{"полоса не объявлена вовсе", "", ""},
	}
	for _, c := range cases {
		got := unraisedInClusterHost(c.uri, receiverSuffixUnderTest)
		switch {
		case c.wantFind == "" && got != "":
			t.Errorf("%s: ложная находка %q — гейт, роняющий законную полосу, снимут первым", c.name, got)
		case c.wantFind != "" && got != c.wantFind:
			t.Errorf("%s: находка %q, ожидалась %q — дефект, возвращённый в дерево, обязан "+
				"опознаваться И называться", c.name, got, c.wantFind)
		}
	}
	t.Logf("перепись: осей подано %d", len(cases))
}

func TestInjection_UnprotectedLaneIsFound(t *testing.T) {
	cases := []struct {
		name, uri, wantSubstr string
	}{
		{"шифрование снято (дословно из профилей)",
			"smtp://mailhog.kacho.svc:1025/?disable_starttls=true", "disable_starttls"},
		{"то же цифрой", "smtp://h:1025/?disable_starttls=1", "disable_starttls"},
		{"то же в верхнем регистре", "SMTP://H:1025/?DISABLE_STARTTLS=TRUE", "disable_starttls"},
		{"проверка сертификата снята", "smtps://h:465/?skip_ssl_verify=true", "skip_ssl_verify"},
		{"чужая схема", "http://h:25/", "схема"},
		{"схемы нет вовсе", "h:25", "схема"},

		{"STARTTLS — законная полоса", "smtp://kacho-umbrella-mailpit:1025/", ""},
		{"неявный TLS — законная полоса", "smtps://relay.example:465/", ""},
		{"параметр, похожий по имени, но не снимающий защиту",
			"smtp://h:1025/?disable_starttls=false", ""},
		{"имя узла содержит слово ловушки", "smtp://skip-ssl-verify-lab:1025/", ""},
	}
	for _, c := range cases {
		got := unprotectedLane(c.uri)
		switch {
		case c.wantSubstr == "" && got != "":
			t.Errorf("%s: ложная находка %q", c.name, got)
		case c.wantSubstr != "" && !strings.Contains(got, c.wantSubstr):
			t.Errorf("%s: находка %q не называет %q — красное без имени предмета читателю бесполезно",
				c.name, got, c.wantSubstr)
		}
	}
	t.Logf("перепись: осей подано %d", len(cases))
}

func TestInjection_GateableReceiverIsFound(t *testing.T) {
	base := shardLayout{
		Gates:  []string{"vpc", "pg-vpc", "uif"},
		Shards: []shardEntry{{ID: "iam", Components: []string{"vpc", "pg-vpc"}}},
	}

	// Законный близнец: приёмник не объявлен снимаемым.
	if got := receiverIsGateable(base, "mailpit"); len(got) != 0 {
		t.Errorf("ложная находка на исправной раскладке: %v", got)
	}
	// Положительный контроль отрицания: шлюзуемый компонент, который ВПРАВЕ
	// сниматься, находкой не является.
	if got := receiverIsGateable(base, "uif"); len(got) == 0 {
		t.Error("предикат не опознаёт компонент, объявленный шлюзуемым, — значит он не опознал бы " +
			"и приёмник, попади тот в тот же перечень")
	}

	// Инъекция 1: приёмник объявлен шлюзуемым.
	inj := base
	inj.Gates = append([]string{"mailpit"}, base.Gates...)
	if got := receiverIsGateable(inj, "mailpit"); len(got) == 0 {
		t.Error("приёмник в перечне шлюзуемых НЕ опознан — шард снял бы узел, оставив настройку, " +
			"и молча не доставлял")
	}

	// Инъекция 2: приёмник перечислен компонентом шарда (то есть остальные его снимают).
	inj2 := base
	inj2.Shards = []shardEntry{{ID: "iam", Components: []string{"vpc", "mailpit"}}}
	if got := receiverIsGateable(inj2, "mailpit"); len(got) == 0 {
		t.Error("приёмник среди компонентов шарда НЕ опознан")
	}
	t.Logf("перепись: раскладок подано 4 (близнец, положительный контроль, две инъекции)")
}

// TestInjection_AppetiteArithmeticIsSound — арифметика запаса. Числа берутся
// ИЗ ОБЪЯВЛЕНИЙ (см. гейт); здесь доказывается, что сравнение не тождественно
// истинно: переполнение обязано опознаваться.
func TestInjection_AppetiteArithmeticIsSound(t *testing.T) {
	const allocatable = 4000
	cases := []struct {
		name               string
		heaviest, receiver int
		wantFits           bool
	}{
		{"замер дерева: тяжелейший шард плюс приёмник", 2630, 30, true},
		{"впритык", 3999, 1, true},
		{"ровно предел", 4000, 0, true},
		{"приёмник переполняет на единицу", 3999, 2, false},
		{"аппетит приёмника вырос вдесятеро", 2630, 1400, false},
		{"шард вырос до предела", 3990, 30, false},
	}
	for _, c := range cases {
		fits := c.heaviest+c.receiver <= allocatable
		if fits != c.wantFits {
			t.Errorf("%s: помещается=%v, ожидалось %v — сравнение либо тождественно истинно, "+
				"либо тождественно ложно, и тогда оно ничего не утверждает", c.name, fits, c.wantFits)
		}
	}
	t.Logf("перепись: осей подано %d · знаменатель %dm", len(cases), allocatable)
}

// TestInjection_ProfileWithoutTheDefectStaysSilent — сводный законный близнец:
// сегодняшнее дерево не должно давать находок ни по одной оси. Без него
// доказательства выше остались бы утверждениями о синтетике.
func TestInjection_ProfileWithoutTheDefectStaysSilent(t *testing.T) {
	const legal = "smtp://kacho-umbrella-mailpit:1025/"
	if h := unraisedInClusterHost(legal, receiverSuffixUnderTest); h != "" {
		t.Errorf("законная полоса дерева объявлена находкой: %q", h)
	}
	if w := unprotectedLane(legal); w != "" {
		t.Errorf("законная полоса дерева объявлена незащищённой: %q", w)
	}
}
