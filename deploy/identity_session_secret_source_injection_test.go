// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_session_secret_source_injection_test.go — ИНЪЕКЦИЯ: обе стороны
// гоняют ТУ ЖЕ функцию, что и проверка по дереву (`identityLaneOf`), а не её
// копию.
//
// ПРОГОНОВ ТРИ, и это не оформление. У проверки ДВЕ оси — «стенд самодостаточен
// в git» и «источник величин объявлен», — а инъекция, роняющая обе сразу,
// ничего не доказывает про каждую: красное пришло бы от соседа, и любая из осей
// могла бы оказаться вакуумной, не показав этого ничем.
//
//	контроль          — всё объявлено: находок нет;
//	инъекция новой    — снята ТОЛЬКО одна величина: находка называет ЕЁ;
//	инъекция соседней — снята строка соединения: стенд перестаёт быть
//	                    самодостаточным, и требование к нему НЕ предъявляется —
//	                    ровно то, что отличает эту проверку от «требовать секрет
//	                    в git у всех».

package deploy_test

import (
	"reflect"
	"testing"
)

// lane — синтетическая цепочка значений одного стенда.
func lane(dsn string, secrets map[string]any, secret map[string]any) map[string]any {
	cfg := map[string]any{}
	if dsn != "" {
		cfg["dsn"] = dsn
	}
	if secrets != nil {
		cfg["secrets"] = secrets
	}
	k := map[string]any{
		"enabled": true,
		"kratos":  map[string]any{"config": cfg},
	}
	if secret != nil {
		k["secret"] = secret
	}
	return map[string]any{"kratos": k}
}

func allThree() map[string]any {
	return map[string]any{
		"default": []any{"fixture-not-a-secret-D"},
		"cookie":  []any{"fixture-not-a-secret-A"},
		"cipher":  []any{"fixture-not-a-secret-B"},
	}
}

// Контроль: самодостаточный стенд объявил все три — находок нет.
func TestIdentitySecretGateSilentOnAFullyDeclaredStand(t *testing.T) {
	got := identityLaneOf(lane("postgres://x", allThree(), nil))
	if !got.raisesIdentity || !got.selfContained {
		t.Fatalf("предпосылка контроля не собрана: %s", laneSummary(got))
	}
	if m := got.missing(); len(m) > 0 {
		t.Fatalf("проверка нашла дефект в законном стенде: не хватает %v (%s)", m, laneSummary(got))
	}
}

// Инъекция ПРОВЕРЯЕМОГО: снята ровно одна величина — находка называет её и
// только её. Соседняя ось («самодостаточен») при этом не тронута.
func TestIdentitySecretGateNamesTheMissingValueAndOnlyIt(t *testing.T) {
	for _, drop := range identitySecretKeys {
		secrets := allThree()
		delete(secrets, drop)
		got := identityLaneOf(lane("postgres://x", secrets, nil))
		if !got.selfContained {
			t.Fatalf("инъекция уронила соседнюю ось: стенд перестал быть самодостаточным (%s)",
				laneSummary(got))
		}
		if !reflect.DeepEqual(got.missing(), []string{drop}) {
			t.Fatalf("снята величина %q, а находка называет %v — по ней чинили бы не то",
				drop, got.missing())
		}
	}
}

// Инъекция СОСЕДНЕЙ ОСИ: снята строка соединения — стенд перестаёт быть
// самодостаточным, и требование к нему не предъявляется.
//
// Это и есть разница между «читать признак» и «завести ведомость исключений на
// боевой профиль»: признак истекает сам — объявят строку соединения, и
// требование придёт вместе с ней.
func TestIdentitySecretGateDoesNotDemandSecretsFromAProfileFedFromOutsideGit(t *testing.T) {
	got := identityLaneOf(lane("", nil, nil))
	if !got.raisesIdentity {
		t.Fatalf("предпосылка не собрана: служба личности не поднята (%s)", laneSummary(got))
	}
	if got.selfContained {
		t.Fatalf("профиль без строки соединения объявлен самодостаточным (%s) — тогда проверка "+
			"потребовала бы секрет в git у боевой площадки", laneSummary(got))
	}
}

// Вторая законная форма владения: секрет заведён вне helm — величин не требуем.
func TestIdentitySecretGateAcceptsASecretOwnedOutsideHelm(t *testing.T) {
	got := identityLaneOf(lane("postgres://x", nil,
		map[string]any{"enabled": false, "nameOverride": "kacho-identity-session-secrets"}))
	if !got.ownedOutside {
		t.Fatalf("секрет вне helm не распознан (%s)", laneSummary(got))
	}
	if m := got.missing(); len(m) > 0 {
		t.Fatalf("от стенда с секретом вне helm потребованы величины %v — тогда законная форма "+
			"владения объявлялась бы нарушением", m)
	}
}

// Половина второй формы формой НЕ является: `enabled: false` без имени секрета
// означает «секрета нет вовсе», а не «секретом владеет оператор».
func TestIdentitySecretGateRejectsHalfOfTheOutsideOwnershipForm(t *testing.T) {
	for name, secret := range map[string]map[string]any{
		"нет имени":        {"enabled": false},
		"чарт чеканит":     {"enabled": true, "nameOverride": "kacho-identity-session-secrets"},
		"имя пустое":       {"enabled": false, "nameOverride": "   "},
		"поля enabled нет": {"nameOverride": "kacho-identity-session-secrets"},
	} {
		t.Run(name, func(t *testing.T) {
			got := identityLaneOf(lane("postgres://x", nil, secret))
			if got.ownedOutside {
				t.Fatalf("половина формы принята за форму (%s) — послабление шире своего предмета",
					laneSummary(got))
			}
			if len(got.missing()) != len(identitySecretKeys) {
				t.Fatalf("величины не потребованы: %v", got.missing())
			}
		})
	}
}

// Отрицательный контроль РАСПОЗНАВАНИЯ: пустой список, список из пробелов и
// ссылка `${…}` объявлением НЕ являются — на всех трёх чарт подставит ту же
// случайную величину, что и на отсутствующем ключе.
func TestIdentitySecretGateIgnoresValuesThatAreNotValues(t *testing.T) {
	for name, v := range map[string]any{
		"пустой список":   []any{},
		"пустая строка":   []any{""},
		"одни пробелы":    []any{"   "},
		"ссылка":          []any{"${KRATOS_SECRETS_COOKIE}"},
		"не список вовсе": "fixture-not-a-secret",
	} {
		t.Run(name, func(t *testing.T) {
			secrets := allThree()
			secrets["cookie"] = v
			got := identityLaneOf(lane("postgres://x", secrets, nil))
			if !reflect.DeepEqual(got.missing(), []string{"cookie"}) {
				t.Fatalf("%s засчитано объявлением: находки %v — «объявлено» и «не объявлено» "+
					"дают один наблюдаемый исход, значит различие не измеряется", name, got.missing())
			}
		})
	}
}

// Граница: служба личности выключена — требовать нечего.
func TestIdentitySecretGateIsSilentWhenIdentityIsOff(t *testing.T) {
	got := identityLaneOf(map[string]any{"kratos": map[string]any{"enabled": false}})
	if got.raisesIdentity {
		t.Fatalf("выключенная служба личности объявлена поднятой (%s)", laneSummary(got))
	}
}
