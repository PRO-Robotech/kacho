// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package storagebackend — use-case ресурса StorageBackend.
//
// Зарегистрированный бэкенд — админ-справочник, живущий ТОЛЬКО на внутреннем
// листенере. Он не project-scoped: бэкенд один на кластер и не принадлежит ни
// одному арендатору, поэтому пообъектного сужения страницы у него нет и быть не
// может — сужать не к чему.
//
// Отсюда прямое следствие для аудита списков: список объявлен кластерным. Это НЕ
// послабление — на публичной поверхности ресурса нет вовсе, а внутренний листенер
// гейтится системным админским отношением, которое подстановочной выдачей не
// выполнимо (в отличие от справочного `viewer` на том же типе объекта).
//
// Мутации СИНХРОННЫ и возвращают ресурс, а не операцию: за правкой справочника нет
// длящейся работы, и оборачивать её в операцию значило бы заставить администратора
// поллить готовое.
package storagebackend

import (
	"context"
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// Update — набор изменяемых полей бэкенда: nil означает «не менять», а не
// «обнулить». Форма отвечает маске правки один в один.
type Update struct {
	Name           *string
	Description    *string
	ZoneIDs        *[]string
	Endpoint       *string
	CredentialsRef *domain.CredentialsRef
	Status         *domain.BackendStatus
}

// Repo — порт хранилища зарегистрированных бэкендов.
type Repo interface {
	Get(ctx context.Context, id string) (*domain.StorageBackend, error)
	List(ctx context.Context, pageSize int64, pageToken string) ([]*domain.StorageBackend, string, error)
	Insert(ctx context.Context, b *domain.StorageBackend) (*domain.StorageBackend, error)
	Update(ctx context.Context, id string, u Update) (*domain.StorageBackend, error)
	Delete(ctx context.Context, id string) error
}

// UseCase — бизнес-логика зарегистрированного бэкенда.
type UseCase struct {
	repo Repo
}

// New собирает UseCase.
func New(repo Repo) *UseCase { return &UseCase{repo: repo} }

// Get возвращает бэкенд по id.
func (u *UseCase) Get(ctx context.Context, id string) (*domain.StorageBackend, error) {
	return u.repo.Get(ctx, id)
}

// List возвращает страницу бэкендов.
//
// Страница НЕ сужается пообъектно намеренно: сужать не к чему — у бэкенда нет
// владельца-арендатора. Проверку размера страницы это не отменяет, и она стоит
// ПЕРВОЙ: мусорный размер обязан давать отказ формы независимо от того, что
// вызывающему выдано.
func (u *UseCase) List(ctx context.Context, pageSize int64, pageToken string) ([]*domain.StorageBackend, string, error) {
	size, err := validate.PageSize("page_size", pageSize)
	if err != nil {
		return nil, "", err
	}
	return u.repo.List(ctx, size, pageToken)
}

// Create регистрирует бэкенд.
//
// Умолчание состояния проставляется ЗДЕСЬ, в одном названном месте: домен пустое
// состояние отвергает, и угадывать за администратора он не вправе — опечатка в этом
// поле иначе завела бы бэкенд принимающим новые привязки.
func (u *UseCase) Create(ctx context.Context, b *domain.StorageBackend) (*domain.StorageBackend, error) {
	if b.Status == "" {
		b.Status = domain.BackendStatusActive
	}
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error())
	}
	return u.repo.Insert(ctx, b)
}

// UpdateAdmin меняет изменяемые поля бэкенда.
//
// Вид бэкенда неизменяем: он определяет, каким адаптером говорить с уже созданными
// объектами, и смена вида на живых данных означала бы, что мы обращаемся к чужому
// хранилищу под своими именами.
func (u *UseCase) UpdateAdmin(ctx context.Context, id string, upd Update) (*domain.StorageBackend, error) {
	if upd.CredentialsRef != nil {
		if err := upd.CredentialsRef.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error())
		}
	}
	if upd.Status != nil && !upd.Status.Valid() {
		return nil, fmt.Errorf("%w: storage backend status %q is not a known status",
			storageerr.ErrInvalidArg, *upd.Status)
	}
	return u.repo.Update(ctx, id, upd)
}

// Delete снимает регистрацию бэкенда.
//
// Ограничительная связь держит бэкенд, пока на него ссылается хоть одна ревизия
// привязки: снять его раньше значило бы оставить живые ресурсы без адресата.
func (u *UseCase) Delete(ctx context.Context, id string) error {
	return u.repo.Delete(ctx, id)
}
