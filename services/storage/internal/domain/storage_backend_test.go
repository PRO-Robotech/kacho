// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// Зарегистрированный бэкенд — admin-ресурс внутреннего листенера. Приёмка
// STOR-P-10 (регистрация) и STOR-P-12 (секрет не проходит через API).
//
// Несущее здесь одно: учётные данные хранятся ССЫЛКОЙ. Значение секрета,
// попавшее в колонку, переживает ротацию (ключ сменили — строка в БД осталась
// прежней и продолжает выглядеть действующей) и уезжает в каждую резервную копию,
// откуда его уже не отозвать. Поэтому поле принимает координату, а не материал.

func TestStorageBackend_Validate(t *testing.T) {
	t.Parallel()

	// Положительный контроль: законная регистрация проходит. Без него любая
	// проба ниже зеленела бы на реализации, отвергающей вообще всё.
	require.NoError(t, newValidBackend().Validate())

	for name, mutate := range map[string]func(*domain.StorageBackend){
		"без id":                func(b *domain.StorageBackend) { b.ID = "" },
		"без имени":             func(b *domain.StorageBackend) { b.Name = "" },
		"имя длиннее предела":   func(b *domain.StorageBackend) { b.Name = strings.Repeat("a", 254) },
		"вид вне словаря":       func(b *domain.StorageBackend) { b.Kind = "NFS" },
		"состояние вне словаря": func(b *domain.StorageBackend) { b.Status = "PAUSED" },
		"без координаты":        func(b *domain.StorageBackend) { b.Endpoint = "" },
		"без ссылки на учётные": func(b *domain.StorageBackend) { b.CredentialsRef = "" },
		"пустая зона в перечне": func(b *domain.StorageBackend) { b.ZoneIDs = []string{"ru-central1-a", ""} },
		"зона названа дважды":   func(b *domain.StorageBackend) { b.ZoneIDs = []string{"ru-central1-a", "ru-central1-a"} },
		"секрет вместо ссылки":  func(b *domain.StorageBackend) { b.CredentialsRef = "AQBhCgpzZWNyZXQtbWF0ZXJpYWw=" },
	} {
		b := newValidBackend()
		mutate(&b)
		require.Errorf(t, b.Validate(), "обязано отвергаться: %s", name)
	}
}

// Вид бэкенда — ЗАКРЫТЫЙ словарь. Свободная строка здесь означала бы, что
// композиционный корень выбирает адаптер по значению, которого никто не объявлял:
// неизвестный вид тогда либо молча остаётся без адаптера, либо получает чужой.
func TestStorageBackendKind_ClosedDictionary(t *testing.T) {
	t.Parallel()

	b := newValidBackend()
	b.Kind = domain.BackendKindCephRBD
	require.NoError(t, b.Validate(), "единственный объявленный вид обязан приниматься")

	for _, bad := range []domain.BackendKind{"", "ceph_rbd", "CEPH", "CEPH_RBD ", "NFS", "LVM"} {
		b := newValidBackend()
		b.Kind = bad
		require.Errorf(t, b.Validate(), "вид вне словаря обязан отвергаться: %q", bad)
	}
}

func TestStorageBackendStatus_ClosedDictionary(t *testing.T) {
	t.Parallel()

	for _, st := range []domain.BackendStatus{
		domain.BackendStatusActive, domain.BackendStatusDraining, domain.BackendStatusDisabled,
	} {
		b := newValidBackend()
		b.Status = st
		require.NoErrorf(t, b.Validate(), "состояние %q обязано приниматься", st)
	}

	for _, bad := range []domain.BackendStatus{"", "active", "DELETED", "DRAINING "} {
		b := newValidBackend()
		b.Status = bad
		require.Errorf(t, b.Validate(), "состояние вне словаря обязано отвергаться: %q", bad)
	}
}

// AcceptsNewBindings отвечает на единственный вопрос, ради которого заведён
// DRAINING: можно ли завести на бэкенде НОВУЮ ревизию привязки. Уже созданные
// ресурсы при этом продолжают жить — вывод бэкенда из обращения не отзывает данные.
func TestStorageBackend_AcceptsNewBindings(t *testing.T) {
	t.Parallel()

	for st, want := range map[domain.BackendStatus]bool{
		domain.BackendStatusActive:   true,
		domain.BackendStatusDraining: false,
		domain.BackendStatusDisabled: false,
	} {
		b := newValidBackend()
		b.Status = st
		require.Equalf(t, want, b.AcceptsNewBindings(), "состояние %q", st)
	}
}

// Поле принимает ССЫЛКУ объявленной формы — и это БЕЛЫЙ список, а не детектор
// «похоже на секрет». Чёрный список форм секрета неполон by construction: материал,
// который его обошёл бы, лёг бы в колонку навсегда. Белый список отвергает всё, что
// не является координатой двух объявленных видов, и материал секрета — в частности.
func TestCredentialsRef_AcceptsReferenceRejectsSecret(t *testing.T) {
	t.Parallel()

	for _, ok := range []domain.CredentialsRef{
		"vault://kacho/storage/ceph-central-1",
		"cfg://storage/credentials/central-1",
		"vault://k",
		"vault://kacho/storage/ceph_central-1.v2",
	} {
		require.NoErrorf(t, ok.Validate(), "ссылка объявленной формы обязана приниматься: %q", ok)
	}

	for name, bad := range map[string]domain.CredentialsRef{
		"пусто":                      "",
		"голый пароль":               "s3cr3t-p4ssw0rd",
		"ключ ceph в base64":         "AQBhCgpkZXBsb3ltZW50LXNlY3JldA==",
		"пара ключ-значение":         "key=AQBhCgpkZXBsb3ltZW50LXNlY3JldA==",
		"блок keyring":               "[client.kacho]\n\tkey = AQBhCg==\n",
		"json с материалом":          `{"key":"AQBhCgpkZXBsb3ltZW50"}`,
		"схема вне словаря":          "http://vault.internal/v1/secret/ceph",
		"схема, несущая материал":    "data://AQBhCgpkZXBsb3ltZW50LXNlY3JldA",
		"схема в верхнем регистре":   "VAULT://kacho/storage/ceph",
		"пустой путь":                "vault://",
		"пробел в координате":        "vault://kacho/storage/ceph central-1",
		"выход за пределы поддерева": "vault://kacho/storage/../../root-token",
		"координата длиннее предела": domain.CredentialsRef("vault://" + strings.Repeat("a", 512)),
	} {
		require.Errorf(t, bad.Validate(), "обязано отвергаться: %s", name)
	}
}

// Отказ НИКОГДА не воспроизводит отвергнутое значение. Иначе поле, заведённое
// ради того, чтобы секрет не попал в БД, само отправило бы его в журнал и в тело
// ответа API — то есть в те же два места, ради которых всё и затевалось.
func TestCredentialsRef_ErrorNeverEchoesValue(t *testing.T) {
	t.Parallel()

	const secret = "AQBhCgpkZXBsb3ltZW50LXNlY3JldA=="

	err := domain.CredentialsRef(secret).Validate()
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret, "отказ обязан молчать о значении")

	// Положительный контроль к отрицанию выше: отказ обязан быть ПОЛЕЗНЫМ —
	// назвать поле и принимаемую форму. Иначе «не эхает» удовлетворялось бы
	// пустым сообщением, а администратор не узнал бы, что от него хотят.
	require.Contains(t, err.Error(), "credentials_ref", "отказ обязан называть поле")
	require.Contains(t, err.Error(), "vault://", "отказ обязан называть принимаемую форму")

	// Тот же инвариант на уровне ресурса: обёртка не имеет права дописать значение.
	b := newValidBackend()
	b.CredentialsRef = secret
	wrapped := b.Validate()
	require.Error(t, wrapped)
	require.NotContains(t, wrapped.Error(), secret, "обёртка ресурса тоже молчит о значении")
}

func newValidBackend() domain.StorageBackend {
	return domain.StorageBackend{
		ID:             "sb-7k2m9p4r1t8w3y6zb",
		Name:           "ceph-central-1",
		Kind:           domain.BackendKindCephRBD,
		Description:    "Ceph RBD, central region",
		ZoneIDs:        []string{"ru-central1-a", "ru-central1-b"},
		Endpoint:       "cfg://ceph/central-1",
		CredentialsRef: "vault://kacho/storage/ceph-central-1",
		Status:         domain.BackendStatusActive,
	}
}
