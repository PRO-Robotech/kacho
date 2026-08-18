// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package storagebackend_test

import (
	"context"
	"errors"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/storagebackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// fakeBackendRepo — подставной репозиторий бэкендов. Он НЕ снисходительнее
// настоящего: правку, дошедшую до него, он запоминает, а проба судит по тому,
// дошла ли она вовсе.
type fakeBackendRepo struct {
	applied *storagebackend.Update
	updated bool
}

func (r *fakeBackendRepo) Get(context.Context, string) (*domain.StorageBackend, error) {
	return &domain.StorageBackend{}, nil
}

func (r *fakeBackendRepo) List(context.Context, int64, string) ([]*domain.StorageBackend, string, error) {
	return nil, "", nil
}

func (r *fakeBackendRepo) Insert(_ context.Context, b *domain.StorageBackend) (*domain.StorageBackend, error) {
	return b, nil
}

func (r *fakeBackendRepo) Update(_ context.Context, _ string, u storagebackend.Update) (*domain.StorageBackend, error) {
	r.applied, r.updated = &u, true
	return &domain.StorageBackend{Name: "kept"}, nil
}

func (r *fakeBackendRepo) Delete(context.Context, string) error { return nil }

// TestUpdateAdminRejectsEmptyName — ДЕФЕКТ асимметрии: создание отвергает пустое
// имя (`storage_backend name is required`), а правка не смотрела на имя вовсе.
// Индекс `storage_backends_name_uniq` ПОЛНЫЙ, поэтому второй бэкенд,
// переименованный в пустое, столкнулся бы с первым — и узнал бы об этом от
// драйвера, а не от контракта.
func TestUpdateAdminRejectsEmptyName(t *testing.T) {
	repo := &fakeBackendRepo{}
	empty := ""
	_, err := storagebackend.New(repo).UpdateAdmin(context.Background(), "sbk-1",
		storagebackend.Update{Name: &empty})
	if !errors.Is(err, storageerr.ErrInvalidArg) {
		t.Fatalf("UpdateAdmin(name=\"\") err = %v, want ErrInvalidArg", err)
	}
	if repo.updated {
		t.Fatal("пустое имя дошло до репозитория — отказ обязан быть ДО записи")
	}
}

// TestUpdateAdminAcceptsNonEmptyName — положительный контроль: без него проба
// выше зеленела бы и на правке, отвергающей ЛЮБОЕ имя.
func TestUpdateAdminAcceptsNonEmptyName(t *testing.T) {
	repo := &fakeBackendRepo{}
	name := "ceph-primary"
	_, err := storagebackend.New(repo).UpdateAdmin(context.Background(), "sbk-1",
		storagebackend.Update{Name: &name})
	if err != nil {
		t.Fatalf("UpdateAdmin(name=%q): %v", name, err)
	}
	if !repo.updated || repo.applied.Name == nil || *repo.applied.Name != name {
		t.Fatalf("до репозитория дошло %v, want %q", repo.applied, name)
	}
}

// TestUpdateAdminLeavesNameAloneWhenNotNamed — правка, не называющая имя,
// именем не судится: nil означает «не менять», и требовать его здесь значило бы
// отвергать смену состояния из-за поля, которого запрос не касался.
func TestUpdateAdminLeavesNameAloneWhenNotNamed(t *testing.T) {
	repo := &fakeBackendRepo{}
	desc := "changed"
	_, err := storagebackend.New(repo).UpdateAdmin(context.Background(), "sbk-1",
		storagebackend.Update{Description: &desc})
	if err != nil {
		t.Fatalf("UpdateAdmin без имени в наборе: %v", err)
	}
	if !repo.updated {
		t.Fatal("правка без имени не дошла до репозитория")
	}
}
