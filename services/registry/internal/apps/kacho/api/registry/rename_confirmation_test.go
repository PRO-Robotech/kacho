// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// Полоса подтверждения переименования (#1644).
//
// RenameRepository — единственный глагол платформы, меняющий внешне-адресуемую
// координату: после него `$домен/$registryId/$repository:$тег` отвечает 404, без
// редиректа и переходного окна. До этой полосы платформа в момент вызова не
// показывала ничем, что у репозитория есть потребители, — цену узнавали при
// следующей выкатке и, как правило, в чужой команде.
//
// Полоса устроена на ДОКАЗАННЫХ потребителях (`download_count`), а не на наличии
// тегов: репозиторий, заведённый опечаткой в `docker push` минуту назад, теги
// несёт, а потребителей у него нет, и наказывать частый безобидный случай ради
// редкого опасного — размен не в пользу продукта (тот же довод, которым отвергнуто
// снятие глагола, `known-divergences.md`).

// Доказанные потребители + подтверждения нет → синхронный отказ, и он называет
// величину. Операция при этом НЕ заводится: иначе у отказа остался бы след,
// который клиент прочтёт как начатую работу.
func TestRename_ProvenPullsRefusedWithoutConfirmation(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["live/api"] = &domain.RepositoryConfig{RegistryID: regID, Name: "live/api", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{projByName: map[string]*domain.Repository{
		"live/api": {RegistryID: regID, Name: "live/api", TagCount: 3, DownloadCount: 37},
	}}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	_, err := uc.RenameRepository(aliceCtx(), regID, "live/api", "live/api-v2", "")
	require.Equal(t, codes.FailedPrecondition, status.Code(err),
		"состояние ресурса не позволяет вызов без подтверждения — это FAILED_PRECONDITION, не INVALID_ARGUMENT")

	msg := status.Convert(err).Message()
	require.Contains(t, msg, "37", "отказ обязан назвать ВЕЛИЧИНУ — сколько раз репозиторий скачивали")
	require.Contains(t, msg, "confirm_current_name",
		"отказ обязан назвать следующий шаг клиента, а не только запретить")

	require.Zero(t, opsCount(ops), "операция не заведена: отказ синхронный и следа не оставляет")
}

// Доказанные потребители + верное подтверждение → вызов принят.
func TestRename_ProvenPullsProceedWithConfirmation(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["live/api"] = &domain.RepositoryConfig{RegistryID: regID, Name: "live/api", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{projByName: map[string]*domain.Repository{
		"live/api": {RegistryID: regID, Name: "live/api", TagCount: 3, DownloadCount: 37},
	}}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.RenameRepository(aliceCtx(), regID, "live/api", "live/api-v2", "live/api")
	require.NoError(t, err, "назвавший текущее имя вызывающий подтвердил предмет — отказывать не в чем")
	require.Nil(t, awaitOpDone(t, ops, op.ID).Error)
	require.Contains(t, cfg.byName, "live/api-v2")
}

// Подтверждение, называющее НЕ ТОТ предмет, подтверждением не является — и это
// верно независимо от потребителей. Пара намеренная: без второй половины поле
// читалось бы только на опасной полосе, то есть на безопасной принималось бы
// молча (запрет «принято-и-проигнорировано»).
func TestRename_WrongConfirmationIsAlwaysRejected(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["live/api"] = &domain.RepositoryConfig{RegistryID: regID, Name: "live/api", Visibility: domain.VisibilityPrivate}
	cfg.byName["quiet/api"] = &domain.RepositoryConfig{RegistryID: regID, Name: "quiet/api", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{projByName: map[string]*domain.Repository{
		"live/api": {RegistryID: regID, Name: "live/api", DownloadCount: 37},
	}}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	// «Подтвердил» НОВЫМ именем — частая ошибка, и она обязана отвергаться.
	_, err := uc.RenameRepository(aliceCtx(), regID, "live/api", "live/api-v2", "live/api-v2")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "confirm_current_name must repeat the current repository name",
		status.Convert(err).Message())

	// Тот же отказ у репозитория БЕЗ доказанных потребителей: поле читается всегда.
	_, err = uc.RenameRepository(aliceCtx(), regID, "quiet/api", "quiet/api-v2", "nonsense")
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"поле принято и не прочитано — ровно тот класс, который запрещён")
	require.Zero(t, opsCount(ops), "ни одна операция не заведена")
}

// Потребителей не доказано → подтверждения не требуется. Это положительный
// контроль ко всему разделу: без него отказ выше зеленел бы на запрете любого
// переименования вообще.
func TestRename_NoProvenPullsNeedsNoConfirmation(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["typo/svc"] = &domain.RepositoryConfig{RegistryID: regID, Name: "typo/svc", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{projByName: map[string]*domain.Repository{
		"typo/svc": {RegistryID: regID, Name: "typo/svc", TagCount: 1, DownloadCount: 0},
	}}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.RenameRepository(aliceCtx(), regID, "typo/svc", "team/svc", "")
	require.NoError(t, err, "репозиторий с тегами, но без скачиваний, переименовывается одним вызовом")
	require.Nil(t, awaitOpDone(t, ops, op.ID).Error)
}

// Движок не отвечает → потребители НЕИЗВЕСТНЫ, и это не «их нет»: полоса
// закрывается fail-closed. Пара с проверкой ниже показывает, что закрывается она
// именно на отсутствии подтверждения, а не на недоступности движка.
func TestRename_UnknownPullsAreTreatedAsProven(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["dark/svc"] = &domain.RepositoryConfig{RegistryID: regID, Name: "dark/svc", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{projErr: regerrors.ErrUnavailable}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	_, err := uc.RenameRepository(aliceCtx(), regID, "dark/svc", "dark/svc2", "")
	require.Equal(t, codes.FailedPrecondition, status.Code(err),
		"неполученный ответ о потребителях не есть «потребителей нет»")
	require.Contains(t, status.Convert(err).Message(), "confirm_current_name",
		"отказ обязан оставить клиенту следующий шаг и в этом случае тоже")
}

// Тот же недоступный движок, но подтверждение дано → вызов принят синхронно.
// Полоса не вправе превращать недоступность движка в невозможность переименовать
// для того, кто предмет уже назвал.
func TestRename_ConfirmationSurvivesAnUnreadableEngine(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["dark/svc"] = &domain.RepositoryConfig{RegistryID: regID, Name: "dark/svc", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{projErr: regerrors.ErrUnavailable}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	_, err := uc.RenameRepository(aliceCtx(), regID, "dark/svc", "dark/svc2", "dark/svc")
	require.NoError(t, err, "подтверждение снимает вопрос, ради которого движок и спрашивали")
}

// Формат идёт ПЕРЕД состоянием: у репозитория с доказанными потребителями
// негодное новое имя отвергается своим отказом, а не отказом полосы подтверждения.
// Иначе клиент чинил бы не то, что сломано.
func TestRename_FormatRefusalPrecedesTheConfirmationLane(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["live/api"] = &domain.RepositoryConfig{RegistryID: regID, Name: "live/api", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{projByName: map[string]*domain.Repository{
		"live/api": {RegistryID: regID, Name: "live/api", DownloadCount: 37},
	}}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	_, err := uc.RenameRepository(aliceCtx(), regID, "live/api", "Bad Name!", "")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "invalid repository name 'Bad Name!'")
}

// opsCount — сколько Operation завёл вызов. Отдельной пробой не является: это
// способ отличить синхронный отказ от отказа, оставившего клиенту след начатой
// работы.
func opsCount(m *memOps) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ops)
}
