// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_session_secret_source_test.go — САМОДОСТАТОЧНЫЙ СТЕНД, поднимающий
// службу личности, ОБЯЗАН объявить величины её сессии (задача #1751).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Необъявленные `kratos.kratos.config.secrets.{default,cookie,cipher}` чарт
// поставщика чеканит сам — `randAlphaNum 32` НА КАЖДОМ РЕНДЕРЕ, — а свой секрет
// пересоздаёт каждым обновлением (`helm.sh/hook: pre-install, pre-upgrade` плюс
// `helm.sh/hook-delete-policy: before-hook-creation`; соседняя
// `helm.sh/resource-policy: keep` у hook-ресурсов helm не читает). Значит всякое
// `helm upgrade` меняет ключ подписи печенья и аннулирует ВСЕ действующие
// сессии арендаторов.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТА ПРОВЕРКА, ЕСЛИ ЕСТЬ СТРАЖ РЕНДЕРА
//
// Их два, и они закрывают РАЗНОЕ. Страж
// `helm/umbrella/templates/identity-session-secret-guard.yaml` видит ВСЕ слои,
// включая слой учётных данных площадки вне git, и потому единственный способен
// судить боевую посадку — но исполняется он только там, где есть helm и
// собранные зависимости. Эта проверка читает ОБЪЯВЛЕНИЯ профилей и исполняется
// везде, где исполняется `go test`.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ТРЕБОВАНИЕ АДРЕСОВАНО НЕ ВСЯКОМУ ПРОФИЛЮ
//
// Величина СЕКРЕТНАЯ, и в git ей не место. Боевая площадка объявляет её слоем
// учётных данных, которого в цепочке `stacks.txt` нет и быть не может (сказано
// там же дословно). Требовать объявления от такого профиля значило бы требовать
// секрет в репозиторий — то есть чинить один класс, заводя худший.
//
// Разделяет их ИЗМЕРИМОЕ свойство, а не ведомость исключений: строку соединения
// службы личности (`kratos.kratos.config.dsn`) стенд, самодостаточный в git,
// объявляет тут же — а профиль, чьи учётные данные приходят извне, не объявляет
// её тоже. То есть «объявил DSN» и есть признак «этот профиль поднимается ИЗ
// GIT», и он обязан объявить и величины сессии.
//
// Ведомость исключений здесь была бы хуже вдвойне: её пришлось бы писать самому
// себе на самый важный профиль, и она не истекала бы никогда. Признак истекает
// сам: объявят DSN боевому профилю — требование придёт вместе с ним.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
//   - ничего о стойкости величины — она судит НАЛИЧИЕ источника;
//   - ничего о профилях, чьи учётные данные приходят извне: их судит страж
//     рендера, и только он способен это сделать;
//   - ничего о первой установке — там сессий ещё нет.

package deploy_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// identitySecretKeys — величины, чья перечеканка аннулирует сессии. Три, а не
// две: `default` объявлен обязательным самим чартом и служит документированным
// запасным ключом для остальных, поэтому оставить его необъявленным значило бы
// держать перечеканиваемую величину, перечень читателей которой не установлен.
var identitySecretKeys = []string{"default", "cookie", "cipher"}

// identityLane — что объявила цепочка профилей одного стенда.
type identityLane struct {
	// raisesIdentity — служба личности включена.
	raisesIdentity bool
	// selfContained — строка соединения объявлена В GIT: стенд поднимается из
	// дерева, а не из слоя учётных данных площадки.
	selfContained bool
	// declared — величины сессии, объявленные непустыми (отсортированы).
	declared []string
	// ownedOutside — секрет заведён вне helm: `secret.enabled: false` плюс
	// непустой `secret.nameOverride`. Вторая законная форма владения.
	ownedOutside bool
}

// missing — величины, которых не хватает, если источник обязан быть объявлен.
func (l identityLane) missing() []string {
	if l.ownedOutside {
		return nil
	}
	var out []string
	for _, k := range identitySecretKeys {
		found := false
		for _, d := range l.declared {
			if d == k {
				found = true
			}
		}
		if !found {
			out = append(out, k)
		}
	}
	return out
}

// TestSelfContainedStandDeclaresItsIdentitySessionSecrets — полосы одного
// механизма сверяются МЕЖДУ СОБОЙ: стенд, поднимающийся из git, обязан объявить
// то же, что объявляет слой учётных данных боевой площадке.
func TestSelfContainedStandDeclaresItsIdentitySessionSecrets(t *testing.T) {
	stacks := deployStacks(t)

	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)

	lanes := map[string]identityLane{}
	raising, self, declaring := 0, 0, 0
	for _, name := range names {
		merged := map[string]any{}
		for _, profile := range stacks[name] {
			merged = mergeValues(merged, readYAML(t, filepath.Join(umbrellaDir, profile)))
		}
		lane := identityLaneOf(merged)
		lanes[name] = lane
		if lane.raisesIdentity {
			raising++
		}
		if lane.raisesIdentity && lane.selfContained {
			self++
			if len(lane.missing()) == 0 {
				declaring++
			}
		}
	}

	// Перепись печатается ВСЕГДА и ПОРОЗНЬ: одно число скрывает ровно тот
	// случай, ради которого ось заведена, — «полос N · несут свойство M».
	t.Logf("осмотрено: стендов прочитано=%d (%s), поднимают службу личности=%d, "+
		"из них самодостаточны в git=%d, из них объявляют источник величин сессии=%d",
		len(names), strings.Join(names, ", "), raising, self, declaring)

	// Предпосылка: обход что-то нашёл. Ноль поднимающих означает, что ключ
	// переехал либо служба выключена всюду, — и тогда проверка судит пустоту.
	if raising == 0 {
		t.Fatalf("предпосылка проверки нарушена: ни один стенд не поднимает службу личности "+
			"(стендов прочитано %d) — ключ `kratos.enabled` переехал либо служба выключена "+
			"всюду; «ноль находок» здесь означало бы «ноль прочитанного»", len(names))
	}

	for _, name := range names {
		lane := lanes[name]
		if !lane.raisesIdentity || !lane.selfContained {
			continue
		}
		if miss := lane.missing(); len(miss) > 0 {
			t.Errorf("стенд %q поднимает службу личности и самодостаточен в git (объявляет её "+
				"строку соединения), но НЕ объявляет величины сессии: %s. Необъявленную величину "+
				"чарт чеканит заново на каждом рендере, а свой секрет пересоздаёт каждым "+
				"обновлением — то есть всякое `helm upgrade` этого стенда аннулирует ВСЕ "+
				"действующие сессии. Исходов два: объявить "+
				"kratos.kratos.config.secrets.{%s} либо завести секрет вне helm "+
				"(kratos.secret.enabled=false плюс kratos.secret.nameOverride)",
				name, strings.Join(miss, ", "), strings.Join(identitySecretKeys, ","))
		}
	}
}

// identityLaneOf — ЧИСТЫЙ предикат над слитыми значениями. Отдельная функция, а
// не тело проверки: инъекция обязана звать ЕЁ ЖЕ, иначе доказывает свойство
// своей копии.
func identityLaneOf(values map[string]any) identityLane {
	var lane identityLane
	kratos, _ := values["kratos"].(map[string]any)
	if kratos == nil {
		return lane
	}
	if enabled, ok := kratos["enabled"].(bool); !ok || !enabled {
		return lane
	}
	lane.raisesIdentity = true

	if secret, ok := kratos["secret"].(map[string]any); ok {
		enabled, hasEnabled := secret["enabled"].(bool)
		name, _ := secret["nameOverride"].(string)
		lane.ownedOutside = hasEnabled && !enabled && strings.TrimSpace(name) != ""
	}

	inner, _ := kratos["kratos"].(map[string]any)
	cfg, _ := inner["config"].(map[string]any)
	if dsn, ok := cfg["dsn"].(string); ok && strings.TrimSpace(dsn) != "" {
		lane.selfContained = true
	}
	secrets, _ := cfg["secrets"].(map[string]any)
	for _, key := range identitySecretKeys {
		if declaredNonEmpty(secrets[key]) {
			lane.declared = append(lane.declared, key)
		}
	}
	sort.Strings(lane.declared)
	return lane
}

// declaredNonEmpty — величина объявлена непустым списком непустых строк.
//
// Пустой список и список из пустых строк объявлением НЕ являются: чарт на них
// подставляет ту же случайную величину, что и на отсутствующем ключе, — то есть
// «объявлено» и «не объявлено» дали бы один наблюдаемый исход.
func declaredNonEmpty(v any) bool {
	items, ok := v.([]any)
	if !ok {
		return false
	}
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		// Ссылка `${…}` величиной не является: служба личности подстановки в
		// значениях настроек не делает, и строка уехала бы в ключ подписи
		// дословно (тот же довод, что у стража обратного вызова).
		if s != "" && !strings.Contains(s, "${") {
			return true
		}
	}
	return false
}

// laneSummary — короткая форма для сообщений инъекции.
func laneSummary(l identityLane) string {
	return fmt.Sprintf("raises=%v selfContained=%v ownedOutside=%v declared=%v missing=%v",
		l.raisesIdentity, l.selfContained, l.ownedOutside, l.declared, l.missing())
}
