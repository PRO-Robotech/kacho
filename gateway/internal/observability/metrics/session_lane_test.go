// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics_test

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
)

// TestSessionLane_F3_48_EveryCellExistsWithZeroBeforeTheFirstEvent — клетки
// счётчиков края (Ф3-48): отказ по отсечке · «сессии нет» · недоступность ·
// окно раската · ретрансляция по глаголу · служба недостижима — все существуют
// с нулём в только что поднятом процессе; после одного события ровно одна
// клетка выросла на единицу.
func TestSessionLane_F3_48_EveryCellExistsWithZeroBeforeTheFirstEvent(t *testing.T) {
	lane := middleware.SessionLaneSnapshot{}
	relays := zeroRelaySnapshots()
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterSessionLane(func() gwmetrics.SessionLaneSnapshot {
		return gwmetrics.SessionLaneSnapshot{Lane: lane, Relays: relays}
	})
	body := expose(t, m)

	zeroes := []string{
		`kacho_api_gateway_session_lane_refusals_total{outcome="cutoff_denied"} 0`,
		`kacho_api_gateway_session_lane_refusals_total{outcome="no_session"} 0`,
		`kacho_api_gateway_session_lane_refusals_total{outcome="unavailable"} 0`,
		`kacho_api_gateway_session_lane_rollout_window_total 0`,
	}
	// Клетка ретрансляции — на КАЖДУЮ запись объявления, включая второй фактор
	// (Ф12) и координаты церемонии (LINE-A-1 §5.1): словарь меток обходится
	// целиком, а не выписанной частью.
	for _, verb := range gwmetrics.LoginLaneVerbLabels() {
		zeroes = append(zeroes, `kacho_api_gateway_login_lane_relayed_total{verb="`+verb+`"} 0`)
	}
	for _, tg := range middleware.RelayTargets() {
		zeroes = append(zeroes, `kacho_api_gateway_login_lane_unreachable_total{target="`+string(tg)+`"} 0`)
	}
	for _, line := range zeroes {
		assert.Contains(t, body, line, "клетка обязана существовать с нулём до первого события")
	}
	t.Logf("перепись: клеток объявлено %d · на поверхности с нулём %d", len(zeroes), countPresent(body, zeroes))

	// Положительный контроль: ровно одна клетка выросла на единицу.
	lane.CutoffDenied = 1
	body = expose(t, m)
	require.Contains(t, body, `kacho_api_gateway_session_lane_refusals_total{outcome="cutoff_denied"} 1`)
	for _, line := range zeroes[1:] {
		assert.Contains(t, body, line, "соседняя клетка не должна была вырасти")
	}
	relays[0].Relayed["logout"] = 1
	body = expose(t, m)
	require.Contains(t, body, `kacho_api_gateway_login_lane_relayed_total{verb="logout"} 1`)
	require.Contains(t, body, `kacho_api_gateway_login_lane_relayed_total{verb="login"} 0`)
}

// Недостижимость различается по ЦЕЛИ: слушатель формы и слушатель выдачи —
// разные слушатели и падают порознь; одна клетка на двоих не сказала бы, какой
// лежит. Клетка ретрансляции координаты церемонии растёт у своей записи.
func TestSessionLane_L13_UnreachableIsCountedPerTargetAndCeremonyVerbsHaveCells(t *testing.T) {
	relays := zeroRelaySnapshots()
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterSessionLane(func() gwmetrics.SessionLaneSnapshot {
		return gwmetrics.SessionLaneSnapshot{Relays: relays}
	})
	for i := range relays {
		if relays[i].Target == middleware.RelayTargetIssuance {
			relays[i].Unreachable = 7
			relays[i].Relayed["authorize"] = 3
		}
	}
	body := expose(t, m)
	require.Contains(t, body, `kacho_api_gateway_login_lane_unreachable_total{target="issuance"} 7`)
	require.Contains(t, body, `kacho_api_gateway_login_lane_unreachable_total{target="form"} 0`)
	require.Contains(t, body, `kacho_api_gateway_login_lane_relayed_total{verb="authorize"} 3`)
	require.Contains(t, body, `kacho_api_gateway_login_lane_relayed_total{verb="token"} 0`)
	require.Contains(t, body, `kacho_api_gateway_login_lane_relayed_total{verb="discovery"} 0`,
		"клетка метаданных обнаружения обязана стоять с нулём с первой секунды процесса")
}

// zeroRelaySnapshots — снимки ретрансляторов только что поднятого процесса: по
// одному на цель, клетки своих записей с нулём.
func zeroRelaySnapshots() []handler.LoginLaneRelaySnapshot {
	var out []handler.LoginLaneRelaySnapshot
	for _, tg := range middleware.RelayTargets() {
		snap := handler.LoginLaneRelaySnapshot{Target: tg, Relayed: map[string]uint64{}}
		for _, rt := range middleware.LoginLaneRoutes() {
			if rt.Target == tg {
				snap.Relayed[rt.Verb] = 0
			}
		}
		out = append(out, snap)
	}
	return out
}

func countPresent(body string, lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(body, l) {
			n++
		}
	}
	return n
}

// Словарь меток `verb` сходится с объявлением путей формы — в обе стороны.
// Второе объявление глаголов здесь есть словарь ПОВЕРХНОСТИ (тот же порядок,
// что у полос решений), и расхождение с объявлением путей обязано краснеть.
func TestSessionLane_F3_48_VerbLabelsMatchTheDeclaredRoutes(t *testing.T) {
	labels := gwmetrics.LoginLaneVerbLabels()
	routes := middleware.LoginLaneRoutes()
	require.Equal(t, len(routes), len(labels), "число меток и число глаголов формы")
	for i, rt := range routes {
		require.Equal(t, rt.Verb, labels[i], "метка %d расходится с объявлением пути %s", i, rt.Path)
	}
	t.Logf("перепись: глаголов объявлено %d · меток %d · сошлись %d", len(routes), len(labels), len(routes))
}

// ЗДЕСЬ СТОЯЛА проба клетки `password_change_required` в семействе решений
// (Ф3-23, Ф3-48) — снята вместе с предметом (kaname#201, kacho#2707): у отказа
// не осталось производителя, и клетка с вечным нулём утверждала бы о полосе,
// которой нет. Отсутствие клетки — предмет утверждения ниже.
func TestSessionLane_PasswordChangeRequiredCellIsGoneWithItsSubject(t *testing.T) {
	authz := middleware.NewAuthzMetrics()
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAuthz(func() gwmetrics.AuthzSnapshot { return gwmetrics.AuthzSnapshot{Counts: authz.Counts()} })
	out := expose(t, m)
	require.NotContains(t, out, `decision="password_change_required"`)
	// Положительный контроль: семейство решений живо — соседняя клетка на месте.
	require.Contains(t, out, `kacho_api_gateway_authz_check_decisions_total{decision="scope_filtered"} 0`)
}

// Клетка «уровень вне оси сессии» (Ф11-19): существует с нулём до первого
// ответа службы без уровня и растёт на единицу с каждым таким ответом.
// Отдельная клетка, а не исход отказа: на глаголе без пола такой ответ
// ПРОХОДИТ, и отказом он не является — состояние докладывается само по себе.
func TestSessionLane_F11_19_OffAxisAssuranceCellExistsWithZeroAndGrows(t *testing.T) {
	lane := middleware.SessionLaneSnapshot{}
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterSessionLane(func() gwmetrics.SessionLaneSnapshot {
		return gwmetrics.SessionLaneSnapshot{Lane: lane, Relays: zeroRelaySnapshots()}
	})
	require.Contains(t, expose(t, m), `kacho_api_gateway_session_lane_assurance_off_axis_total 0`)
	lane.AssuranceOffAxis = 1
	body := expose(t, m)
	require.Contains(t, body, `kacho_api_gateway_session_lane_assurance_off_axis_total 1`)
	require.Contains(t, body, `kacho_api_gateway_session_lane_refusals_total{outcome="no_session"} 0`,
		"соседняя клетка не должна была вырасти")
}

// ─────────────────────────────────────────────────────────────────────────────
// У КАЖДОЙ КЛЕТКИ СНИМКА ЕСТЬ ЧИТАТЕЛЬ, И ЭТО СУДИТСЯ ОБХОДОМ ПОЛЕЙ.
//
// Величина, объявленная в снимке и не собранная коллектором, невидима на
// стенде — а вместе с ней невидим и вопрос, ради которого её завели: клетку
// предлагают как свидетельство, а судить ею нельзя, потому что ни в
// `Describe`, ни в `Collect` её нет.
//
// Гейт судит ПОВЕДЕНИЕМ, а не текстом: каждому полю снимка даётся СВОЁ число,
// и после сбора каждое обязано найтись в выдаче. Поле, которого коллектор не
// читает, своего числа не даст — и назовётся по имени.
//
// Так же этот гейт ИСТЕКАЕТ САМ: новое поле снимка без читателя краснеет в тот
// же прогон, а не ждёт, пока кто-нибудь заметит.
func TestSessionLane_EveryLaneSnapshotFieldIsRead(t *testing.T) {
	lane := middleware.SessionLaneSnapshot{}
	rv := reflect.ValueOf(&lane).Elem()
	rt := rv.Type()

	// Числа заметные и различные: значение, совпавшее с чужим, скрыло бы
	// непрочитанное поле за чужой клеткой.
	want := map[string]uint64{}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Type.Kind() != reflect.Uint64 || !rv.Field(i).CanSet() {
			continue
		}
		v := uint64(9000 + i*111)
		rv.Field(i).SetUint(v)
		want[f.Name] = v
	}
	require.NotEmpty(t, want, "полей-счётчиков в снимке не найдено — гейт судил бы о непрочитанном")

	relays := zeroRelaySnapshots()
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterSessionLane(func() gwmetrics.SessionLaneSnapshot {
		return gwmetrics.SessionLaneSnapshot{Lane: lane, Relays: relays}
	})
	body := expose(t, m)

	unread := []string{}
	for name, v := range want {
		if !strings.Contains(body, " "+strconv.FormatUint(v, 10)+"\n") {
			unread = append(unread, name)
		}
	}
	sort.Strings(unread)
	if len(unread) > 0 {
		t.Errorf("поля снимка без читателя в коллекторе: %s. Величина, которую никто не собирает, "+
			"невидима на стенде — а вместе с ней невидим и вопрос, ради которого её завели",
			strings.Join(unread, ", "))
	}
	t.Logf("перепись: полей-счётчиков в снимке %d · прочитанных коллектором %d",
		len(want), len(want)-len(unread))
}

// Словарь меток `target` сходится с закрытым перечнем целей ретрансляции — в
// обе стороны.
func TestSessionLane_L13_TargetLabelsMatchTheDeclaredTargets(t *testing.T) {
	labels := gwmetrics.LoginLaneTargetLabels()
	targets := middleware.RelayTargets()
	require.Equal(t, len(targets), len(labels), "число меток и число целей")
	for i, tg := range targets {
		require.Equal(t, string(tg), labels[i], "метка %d расходится с целью", i)
	}
	t.Logf("перепись: целей объявлено %d · меток %d", len(targets), len(labels))
}
