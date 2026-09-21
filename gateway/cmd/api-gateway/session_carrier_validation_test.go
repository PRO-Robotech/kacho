// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_validation_test.go — страж старта: объявленное множество
// читателей носителя обязано БЫТЬ ИСПОЛНИМЫМ на этой посадке.
//
// Множество разведено с посадкой намеренно, но разведены они не до
// независимости: не всякая пара (посадка, множество) осмысленна, и
// бессмысленная обязана отказывать при СТАРТЕ. Отказ при старте виден
// оператору; читатель, провязанный впустую, виден только по жалобе человека,
// который не смог войти.
package main

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/identityposture"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// windowOpenedAtFixture — момент открытия окна в пробах.
var windowOpenedAtFixture = time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)

// guardClockFixture — часы старта в пробах: ПОЗЖЕ момента открытия окна, иначе
// проба судила бы не свой предмет, а ось «момент обязан быть фактом».
var guardClockFixture = windowOpenedAtFixture.Add(time.Hour)

func carrierSet(t *testing.T, declared string) config.SessionCarrierSet {
	t.Helper()
	// Обе ручки задаются ЯВНО: величина, приехавшая из окружения прогона, дала
	// бы вердикт о чужом значении.
	t.Setenv(config.IdentityProviderKnob, "own")
	t.Setenv(config.SessionCarriersKnob, declared)
	t.Setenv(config.SessionCarrierWindowOpenedAtKnob, "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	set, err := cfg.ResolvedSessionCarriers()
	if err != nil {
		t.Fatalf("ResolvedSessionCarriers(%q): %v", declared, err)
	}
	return set
}

// ─────────────────────────────────────────────────────────────────────────────
// ПОЛНОЕ ПРОИЗВЕДЕНИЕ «посадка × множество», а не перечень удобных пар.
//
// Прежняя редакция называла ТРИ пары и объявляла их законными. Пар при двух
// посадках и трёх состояниях носителя ШЕСТЬ, и та, что осталась вне перечня,
// осталась и вне суда: посадка `own` с множеством БЕЗ нашего читателя. На ней
// край поднимает нашу полосу формы входа, наша служба чеканит `kaname_session`,
// а читателя этого печенья в процессе нет — человек проходит форму и остаётся
// анонимом. Перечень удобных пар не способен такое увидеть: он утверждает о
// том, что в нём есть.
//
// Произведение поэтому ВЫВОДИТСЯ: посадки берутся из словаря
// (`identityposture.Values`), состояния носителя — из трёх объявлений профиля.
// Третье значение посадки, если оно когда-нибудь появится, сделает пробу
// красной, а не молчаливой.

// carrierStateDeclarations — ВСЕ состояния, которые профиль умеет объявить,
// ВЫВЕДЕННЫЕ из словаря сторон: непустые подмножества.
//
// Прежде они были ВЫПИСАНЫ тремя строками, а шапка обещала полное
// произведение. При третьей стороне проба продолжала печатать «пар осмотрено
// 6» и проходила, тогда как произведение — 14: число читалось как полнота,
// которой у него не было. Дыру закрывал соседний гейт, и это было сказано
// честно, — но честная оговорка не делает число верным.
//
// Порядок внутри состояния не перебирается намеренно: множество на него не
// реагирует, и это держит своя проба. Перебирались бы перестановки — росло бы
// число, а не осмотренное.
func carrierStateDeclarations() []string {
	sides := identityposture.Names()
	var out []string
	for mask := 1; mask < 1<<len(sides); mask++ {
		var parts []string
		for i, name := range sides {
			if mask&(1<<i) != 0 {
				parts = append(parts, name)
			}
		}
		out = append(out, strings.Join(parts, ","))
	}
	sort.Strings(out)
	return out
}

// lawfulCarrierPair — законна ли пара, и правило ВЫВОДИТСЯ из свойства, а не
// перечисляется: на стенде читается ровно то, что на нём чеканится.
//
// Наша сторона в множестве ⇔ посадка `own`. Обе половины этой равносильности и
// есть два правила стража; третья сторона, появись она, получит вердикт по
// тому же свойству, а не по отсутствию строки в перечне.
func lawfulCarrierPair(posture identityposture.Provider, declared string) bool {
	namesOwn := false
	for _, s := range strings.Split(declared, ",") {
		if s == identityposture.Own.String() {
			namesOwn = true
		}
	}
	return namesOwn == (posture == identityposture.Own)
}

func TestSessionCarrierGuard_EveryPostureCarrierPairIsJudged(t *testing.T) {
	postures := identityposture.Values()
	if len(postures) == 0 {
		t.Fatal("словарь посадок пуст — произведение строить не из чего")
	}
	pairs, lawful, refused := 0, 0, 0
	states := carrierStateDeclarations()
	for _, posture := range postures {
		for _, declared := range states {
			pairs++
			key := posture.String() + "/" + declared
			want := lawfulCarrierPair(posture, declared)
			set := carrierSet(t, declared)
			// Момент открытия объявляется РОВНО там, где есть окно: страж судит
			// пару целиком, и подать момент всюду значило бы не проверить
			// половину правила.
			opened := time.Time{}
			if set.IsTransitionalWindow() {
				opened = windowOpenedAtFixture
			}
			err := validateSessionCarrierConfig(SessionCarrierConfig{
				Posture: posture, Carriers: set, ProviderURL: "http://kratos:80",
				WindowOpenedAt: opened,
				// Часы обязательны там, где объявлен момент: ось, выключаемая
				// отсутствием величины, контролем не является.
				Now: guardClockFixture,
			})
			switch {
			case want && err != nil:
				t.Errorf("законная пара %s отвергнута: %v", key, err)
			case !want && err == nil:
				t.Errorf("пара %s принята, а провязка на ней НЕПОЛНА: край заводит не всех "+
					"читателей, чей носитель на этом стенде кто-то чеканит, и человек, прошедший "+
					"вход, остаётся анонимом", key)
			}
			if want {
				lawful++
			} else {
				refused++
			}
		}
	}
	// Обе оси ВЫВЕДЕНЫ, и перепись называет их источники: число «пар осмотрено»
	// есть полнота, а не столько, сколько удобно перечислить.
	t.Logf("перепись: посадок в словаре %d (выведены из identityposture.Values) · состояний "+
		"носителя %d (выведены как непустые подмножества сторон: %s) · пар осмотрено %d = "+
		"произведение · законных %d · обязанных отказать %d",
		len(postures), len(states), strings.Join(states, " | "), pairs, lawful, refused)
}

// ЧЕТВЁРТАЯ ПАРА, ради которой заведено произведение: посадка `own` без нашего
// читателя. Названа отдельно, потому что это ВХОД БЕЗ ЧИТАТЕЛЯ — зеркало
// правила «читатель без производителя», и отказ обязан называть обе ручки.
func TestSessionCarrierGuard_OurMintWithoutOurReaderIsRefused(t *testing.T) {
	err := validateSessionCarrierConfig(SessionCarrierConfig{
		Posture:     identityposture.Own,
		Carriers:    carrierSet(t, "external"),
		ProviderURL: "http://kratos:80",
	})
	if err == nil {
		t.Fatal("посадка own без нашего читателя принята: край поднимает нашу полосу формы входа, " +
			"служба чеканит kaname_session, и читателя этого печенья в процессе нет — человек " +
			"проходит вход и остаётся анонимом")
	}
	for _, want := range []string{config.SessionCarriersKnob, config.IdentityProviderKnob} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("отказ не называет ручку %s: %v", want, err)
		}
	}
	t.Logf("отказ: %v", err)
}

// НАШ носитель под ЧУЖОЙ посадкой — читатель без производителя: печенье нашей
// сессии чеканит наша полоса входа, а её под `external` край не поднимает.
func TestSessionCarrierGuard_OurReaderUnderTheForeignPostureIsRefused(t *testing.T) {
	err := validateSessionCarrierConfig(SessionCarrierConfig{
		Posture:        identityposture.External,
		Carriers:       carrierSet(t, "own,external"),
		ProviderURL:    "http://kratos:80",
		WindowOpenedAt: windowOpenedAtFixture,
		Now:            guardClockFixture,
	})
	if err == nil {
		t.Fatal("наш читатель под посадкой external принят: край читал бы печенье, которое на этом " +
			"стенде некому выдать — контроль, выглядящий включённым и не имеющий входа")
	}
	for _, want := range []string{config.SessionCarriersKnob, config.IdentityProviderKnob} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("отказ не называет ручку %s: %v", want, err)
		}
	}
	t.Logf("отказ: %v", err)
}

// ЧУЖОЙ носитель ОБЪЯВЛЕН, а адреса чужой стороны нет — объявление принято и не
// действует. Отказ, а не молчание.
func TestSessionCarrierGuard_ADeclaredProviderReaderWithoutItsAddressIsRefused(t *testing.T) {
	for _, url := range []string{"disabled", "", "   "} {
		err := validateSessionCarrierConfig(SessionCarrierConfig{
			Posture:        identityposture.Own,
			Carriers:       carrierSet(t, "own,external"),
			ProviderURL:    url,
			WindowOpenedAt: windowOpenedAtFixture,
			Now:            guardClockFixture,
		})
		if err == nil {
			t.Fatalf("адрес %q принят при объявленном чужом читателе: профиль назвал переходное "+
				"состояние, и оно молча не наступило — ровно тот вход, ради которого оно заводится", url)
		}
		if !strings.Contains(err.Error(), config.SessionCarriersKnob) {
			t.Fatalf("отказ не называет ручку: %v", err)
		}
	}
	t.Logf("перепись: вырожденных адресов проверено 3 · принято 0")
}

// ВЫВЕДЕННОЕ множество ведёт себя как прежде: посадка `external` с выключенным
// адресом не отказывает, потому что это СЕГОДНЯШНЕЕ поведение края, а не чьё-то
// объявление. Различие «не задано» / «задано» — то же, что у приёма токена.
func TestSessionCarrierGuard_ADerivedSetKeepsTodaysBehaviourOnADisabledAddress(t *testing.T) {
	t.Setenv(config.IdentityProviderKnob, "external")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	set, err := cfg.ResolvedSessionCarriers()
	if err != nil {
		t.Fatalf("ResolvedSessionCarriers: %v", err)
	}
	if set.Declared() {
		t.Fatal("необъявленное множество считает себя объявленным — различие, на котором стоит " +
			"весь разбор, стёрто")
	}
	if err := validateSessionCarrierConfig(SessionCarrierConfig{
		Posture: identityposture.External, Carriers: set, ProviderURL: "disabled",
	}); err != nil {
		t.Fatalf("выведенное множество с выключенным адресом отвергнуто: %v — существующий профиль "+
			"перестал бы подниматься от одного появления ручки", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// МОМЕНТ ОТКРЫТИЯ ОКНА ОБЯЗАН БЫТЬ ФАКТОМ, А НЕ НАМЕРЕНИЕМ.
//
// Страж судил момент по двум признакам — «разбирается» и «ненулевой», — и
// момент ВПЕРЕДИ по оси времени проходил оба. А он обращает свойство окна в
// тождество: приём пропускает всякую чужую сессию вплоть до названного момента,
// включая заведённую только что, и отзыв снова снимается входом заново.
//
// Момент называет то, что УЖЕ СЛУЧИЛОСЬ: окно открыто, живые сессии
// дочитываются. Момент в будущем не описывает ни одного факта — он описывает
// намерение, и до его наступления граница не отделяет ничего.

func TestSessionCarrierGuard_AWindowInstantInTheFutureIsRefused(t *testing.T) {
	bootAt := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		openedAt time.Time
		lawful   bool
	}{
		{"в прошлом — факт", bootAt.Add(-time.Hour), true},
		{"ровно момент старта — факт, граница закрыта", bootAt, true},
		{"в будущем на минуту — намерение", bootAt.Add(time.Minute), false},
		{"в будущем на год — намерение", bootAt.AddDate(1, 0, 0), false},
	}
	refused := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSessionCarrierConfig(SessionCarrierConfig{
				Posture:        identityposture.Own,
				Carriers:       carrierSet(t, "own,external"),
				ProviderURL:    "http://kratos:80",
				WindowOpenedAt: tc.openedAt,
				Now:            bootAt,
			})
			switch {
			case tc.lawful && err != nil:
				t.Fatalf("момент %s отвергнут: %v", tc.openedAt.Format(time.RFC3339), err)
			case !tc.lawful && err == nil:
				t.Fatalf("момент %s принят: граница не отделяет ничего, и всякая чужая сессия, "+
					"включая заведённую только что, проходит как «живая»",
					tc.openedAt.Format(time.RFC3339))
			}
			if !tc.lawful {
				refused++
				if !strings.Contains(err.Error(), config.SessionCarrierWindowOpenedAtKnob) {
					t.Fatalf("отказ не называет ручку: %v", err)
				}
			}
		})
	}
	t.Logf("перепись: моментов проверено %d · законных 2 · отвергнутых %d", len(cases), refused)
}

// ─────────────────────────────────────────────────────────────────────────────
// ОСЬ НЕ ВЫКЛЮЧАЕТСЯ ОТСУТСТВИЕМ ВЕЛИЧИНЫ.
//
// Часы приходят стражу полем. Поле, не переданное вызывающим, давало нулевое
// значение — и ось «момент обязан быть фактом» молча не срабатывала. Обхода,
// который бы это стерёг, не было, и следствие точное: снятие ОДНОГО поля из
// композиционного корня возвращало закрытую находку высокой тяжести, а набор
// оставался зелёным.
//
// Сценария отказа у этого нет — пока поле передают. Тем и опасно: свойство
// держалось тем, что никто не трогал одну строку.
func TestSessionCarrierGuard_TheClockIsNotOptional(t *testing.T) {
	err := validateSessionCarrierConfig(SessionCarrierConfig{
		Posture:        identityposture.Own,
		Carriers:       carrierSet(t, "own,external"),
		ProviderURL:    "http://kratos:80",
		WindowOpenedAt: windowOpenedAtFixture,
		// Now не передан — ровно то, что даёт снятие одной строки из корня.
	})
	if err == nil {
		t.Fatal("страж принял объявленное окно БЕЗ часов: ось «момент обязан быть фактом» молча " +
			"не сработала, и момент в будущем снова прошёл бы. Контроль, выключаемый отсутствием " +
			"величины, контролем не является")
	}
	if !strings.Contains(err.Error(), config.SessionCarrierWindowOpenedAtKnob) {
		t.Fatalf("отказ не называет ручку: %v", err)
	}
	t.Logf("отказ: %v", err)

	// Законный близнец: окна НЕТ — часы не нужны, и отсутствие их не отказ.
	// Без этой половины страж требовал бы часов там, где судить нечего.
	if err := validateSessionCarrierConfig(SessionCarrierConfig{
		Posture: identityposture.External, Carriers: carrierSet(t, "external"),
		ProviderURL: "http://kratos:80",
	}); err != nil {
		t.Fatalf("пара без окна отвергнута из-за часов: %v — часы требуются там, где есть что "+
			"судить, а не всегда", err)
	}
	t.Logf("перепись: пар проверено 2 · окно с часами не судится здесь · без часов отказ 1 · " +
		"без окна отказов 0")
}
