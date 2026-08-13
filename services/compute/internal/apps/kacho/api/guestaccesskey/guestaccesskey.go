// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package guestaccesskey — жизненный цикл публичных ключей входа в машину.
package guestaccesskey

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/ssh"
	"google.golang.org/protobuf/types/known/anypb"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/lro"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/ownersync"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/peercheck"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
)

const (
	keyResource = "GuestAccessKey"
	keyPrefix   = "gak"

	maxKeyNameLen   = 63
	maxPublicKeyLen = 16384
)

// Repo — порт хранения ключей.
type Repo interface {
	Get(ctx context.Context, id string) (*domain.GuestAccessKey, error)
	List(ctx context.Context, projectID string, p ports.Pagination) ([]*domain.GuestAccessKey, string, error)
	Insert(ctx context.Context, k *domain.GuestAccessKey) (*domain.GuestAccessKey, []ownerregister.Registration, error)
	Update(ctx context.Context, id string, u ports.GuestAccessKeyUpdate) (*domain.GuestAccessKey, error)
	Delete(ctx context.Context, id string) error
}

// Service — use-case ключей.
type Service struct {
	repo      Repo
	opsRepo   operations.Repo
	projects  ports.ProjectClient
	registrar ports.OwnerRegistrar
}

// NewService собирает use-case.
func NewService(repo Repo, ops operations.Repo, projects ports.ProjectClient, reg ports.OwnerRegistrar) *Service {
	return &Service{repo: repo, opsRepo: ops, projects: projects, registrar: reg}
}

// WithOwnerRegistrar подключает синхронную половину регистрации прав.
//
// Она НЕ обязательна: долговечное намерение пишется в той же транзакции, что
// строка, и дренаж доставляет его хоть раз в любом случае. Синхронная половина
// лишь сужает окно, в котором создатель кратко не видит собственный ключ.
func (s *Service) WithOwnerRegistrar(r ports.OwnerRegistrar) *Service {
	s.registrar = r
	return s
}

// Get возвращает ключ.
func (s *Service) Get(ctx context.Context, id string) (*domain.GuestAccessKey, error) {
	if err := corevalidate.ResourceID(keyResource, keyPrefix, id); err != nil {
		return nil, err
	}
	k, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	return k, nil
}

// List возвращает страницу ключей проекта.
//
// Проверка постраничности идёт ПЕРВОЙ — до любого решения о видимости. Иначе
// ответ на один и тот же неверный ввод зависел бы от того, что вызывающему
// выдано: с пустым грантом он получил бы пустую страницу, с непустым — отказ.
func (s *Service) List(ctx context.Context, projectID string, p ports.Pagination) ([]*domain.GuestAccessKey, string, error) {
	if err := ValidateListPagination(p); err != nil {
		return nil, "", err
	}
	if projectID == "" {
		return nil, "", serviceerr.InvalidArg("project_id", "projectId is required")
	}
	out, next, err := s.repo.List(ctx, projectID, p)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	return out, next, nil
}

// ValidateListPagination проверяет курсор и размер страницы.
//
// Вынесена отдельно, чтобы её мог позвать и транспортный слой: свойство
// «формат проверен до решения о видимости» обязано держаться в той же функции,
// которая замыкается на пустом гранте, а не только на пути до хранилища.
func ValidateListPagination(p ports.Pagination) error {
	if _, err := corevalidate.PageSize("page_size", p.PageSize); err != nil {
		return err
	}
	return nil
}

// CreateReq — вход создания.
type CreateReq struct {
	ProjectID string
	Name      string
	PublicKey string
	Labels    map[string]string
}

// Create заводит ключ.
//
// # Почему материал разбирается здесь, а не в машине
//
// Строка, которую нельзя разобрать, доехала бы до гостя и молча не сработала —
// а отладка началась бы там, где причины нет. Разбор при создании превращает
// это в отказ по имени поля в момент, когда вызывающий ещё смотрит на свой ввод.
//
// # Отпечаток вычисляем МЫ
//
// Присланный на входе означал бы, что мы верим чужому счёту: два разных
// материала с одинаковым присланным отпечатком стали бы неразличимы, а вся
// польза отпечатка — в том, что арендатор сверяет его со своим.
func (s *Service) Create(ctx context.Context, req CreateReq) (*operations.Operation, error) {
	if req.ProjectID == "" {
		return nil, serviceerr.InvalidArg("project_id", "projectId is required")
	}
	if l := len(req.Name); l == 0 || l > maxKeyNameLen {
		return nil, serviceerr.InvalidArg("name", "name must be 1..63 characters")
	}
	if len(req.PublicKey) == 0 || len(req.PublicKey) > maxPublicKeyLen {
		return nil, serviceerr.InvalidArg("public_key", "publicKey is required and must be at most 16384 bytes")
	}

	fingerprint, err := fingerprintOf(req.PublicKey)
	if err != nil {
		return nil, serviceerr.InvalidArg("public_key",
			"publicKey is not a readable public key: it would reach the guest and silently fail")
	}

	if err := peercheck.Project(ctx, s.projects, req.ProjectID); err != nil {
		return nil, err
	}

	key := &domain.GuestAccessKey{
		ID:          ids.NewHyphenID(keyPrefix),
		ProjectID:   req.ProjectID,
		Name:        req.Name,
		PublicKey:   req.PublicKey,
		Fingerprint: fingerprint,
		Labels:      req.Labels,
	}

	return lro.RunOp(ctx, s.opsRepo, "Create guest access key "+key.Name,
		&computev1.CreateGuestAccessKeyMetadata{GuestAccessKeyId: key.ID},
		func(ctx context.Context) (*anypb.Any, error) {
			created, regs, err := s.repo.Insert(ctx, key)
			if err != nil {
				return nil, serviceerr.MapRepoErr(err)
			}
			ownersync.Register(ctx, s.registrar, regs)
			return anypb.New(protoconv.GuestAccessKey(created))
		})
}

// ListOperations возвращает операции над ключом.
//
// Существование ключа проверяется ПЕРВЫМ: иначе список операций несуществующего
// ключа отдавал бы пустую страницу — ответ, неотличимый от «операций не было».
func (s *Service) ListOperations(ctx context.Context, id string, p ports.Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID(keyResource, keyPrefix, id); err != nil {
		return nil, "", err
	}
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	return operations.ListForCaller(ctx, s.opsRepo,
		operations.ListFilter{ResourceID: id, PageSize: p.PageSize, PageToken: p.PageToken})
}

// UpdateReq — вход правки.
type UpdateReq struct {
	ID         string
	UpdateMask []string
	Name       string
	Labels     map[string]string
}

// guestKeyMutable — изменяемый набор; он же применяется при пустой маске.
var guestKeyMutable = []string{"name", "labels"}

// guestKeyUpdateKnown — известный набор имён маски.
var guestKeyUpdateKnown = map[string]struct{}{"name": {}, "labels": {}}

// Update правит косметическую часть ключа.
//
// Порядок проверок обязателен и не косметичен: неизменяемое поле отвергается ДО
// проверки маски по известному набору. Известный набор неизменяемых полей не
// содержит, поэтому обратный порядок вернул бы им общий отказ «неизвестное
// поле» — и вызывающий, назвавший материал ключа, узнал бы, что такого поля не
// существует, вместо того что его нельзя менять.
func (s *Service) Update(ctx context.Context, req UpdateReq) (*operations.Operation, error) {
	if err := corevalidate.ResourceID(keyResource, keyPrefix, req.ID); err != nil {
		return nil, err
	}
	for _, f := range req.UpdateMask {
		switch f {
		case "public_key", "fingerprint", "project_id", "id", "created_at":
			return nil, serviceerr.InvalidArg(f, f+" is immutable after GuestAccessKey.Create")
		}
	}
	if err := corevalidate.UpdateMask("update_mask", req.UpdateMask, guestKeyUpdateKnown); err != nil {
		return nil, err
	}

	applied := req.UpdateMask
	if len(applied) == 0 {
		applied = guestKeyMutable
	}
	var upd ports.GuestAccessKeyUpdate
	for _, f := range applied {
		switch f {
		case "name":
			if l := len(req.Name); l == 0 || l > maxKeyNameLen {
				return nil, serviceerr.InvalidArg("name", "name must be 1..63 characters")
			}
			name := req.Name
			upd.Name = &name
		case "labels":
			upd.Labels, upd.LabelsSet = req.Labels, true
		}
	}

	return lro.RunOp(ctx, s.opsRepo, "Update guest access key "+req.ID,
		&computev1.UpdateGuestAccessKeyMetadata{GuestAccessKeyId: req.ID},
		func(ctx context.Context) (*anypb.Any, error) {
			updated, err := s.repo.Update(ctx, req.ID, upd)
			if err != nil {
				return nil, serviceerr.MapRepoErr(err)
			}
			return anypb.New(protoconv.GuestAccessKey(updated))
		})
}

// Delete снимает ключ.
//
// Снятие НЕ выселяет того, кто уже вошёл: соединение, установленное до снятия,
// живёт своей жизнью, и плоскость управления его не видит. Это свойство прямого
// входа, а не пробел, и оно названо в контракте — иначе снятие читалось бы как
// отзыв доступа.
func (s *Service) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID(keyResource, keyPrefix, id); err != nil {
		return nil, err
	}
	return lro.RunOp(ctx, s.opsRepo, "Delete guest access key "+id,
		&computev1.DeleteGuestAccessKeyMetadata{GuestAccessKeyId: id},
		func(ctx context.Context) (*anypb.Any, error) {
			if err := s.repo.Delete(ctx, id); err != nil {
				return nil, serviceerr.MapRepoErr(err)
			}
			return nil, nil
		})
}

// fingerprintOf разбирает материал и считает отпечаток.
//
// Формат отпечатка — тот же, что показывают клиентские средства арендатора:
// иначе сверка, ради которой он существует, потребовала бы от человека
// пересчёта.
func fingerprintOf(material string) (string, error) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(material)))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(pub.Marshal())
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "="), nil
}
