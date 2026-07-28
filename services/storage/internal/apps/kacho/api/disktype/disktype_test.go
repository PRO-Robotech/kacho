// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package disktype_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// TestGetDelegates — public read проходит в repo-порт сквозняком.
func TestGetDelegates(t *testing.T) {
	const wantID = "block-balanced"
	want := &domain.DiskType{ID: wantID, Name: "balanced"}
	repo := &repomock.DiskTypeRepo{
		GetFunc: func(_ context.Context, id string) (*domain.DiskType, error) {
			if id != wantID {
				t.Fatalf("repo got id %q", id)
			}
			return want, nil
		},
	}
	got, err := disktype.New(repo).Get(context.Background(), wantID)
	if err != nil || got != want {
		t.Fatalf("Get = (%+v, %v)", got, err)
	}
}

// TestCreateAdminValidatesDomain — admin Create отвергает пустой id ДО repo
// (self-validating domain): InvalidArgument-sentinel, Insert не вызывается.
func TestCreateAdminValidatesDomain(t *testing.T) {
	repo := &repomock.DiskTypeRepo{
		InsertFunc: func(context.Context, *domain.DiskType) (*domain.DiskType, error) {
			t.Fatal("repo.Insert must not be called on invalid domain")
			return nil, nil
		},
	}
	_, err := disktype.New(repo).CreateAdmin(context.Background(), &domain.DiskType{Name: "no-id"})
	if !errors.Is(err, storageerr.ErrInvalidArg) {
		t.Fatalf("CreateAdmin invalid err = %v, want ErrInvalidArg", err)
	}
}

// TestCreateAdminDelegates — валидный DiskType проходит в repo.Insert.
func TestCreateAdminDelegates(t *testing.T) {
	want := &domain.DiskType{ID: "block-x", Name: "x"}
	var got *domain.DiskType
	repo := &repomock.DiskTypeRepo{
		InsertFunc: func(_ context.Context, d *domain.DiskType) (*domain.DiskType, error) {
			got = d
			return d, nil
		},
	}
	res, err := disktype.New(repo).CreateAdmin(context.Background(), want)
	if err != nil || res != want || got != want {
		t.Fatalf("CreateAdmin = (%+v, %v)", res, err)
	}
}

// TestUpdateAdminValidatesDomain — admin Update отвергает over-long name ДО repo
// (self-validating domain, парити с CreateAdmin): InvalidArgument-sentinel,
// repo.Update не вызывается (домен ловит, не только DB-CHECK).
func TestUpdateAdminValidatesDomain(t *testing.T) {
	repo := &repomock.DiskTypeRepo{
		UpdateFunc: func(context.Context, string, string, string, []string, string) (*domain.DiskType, error) {
			t.Fatal("repo.Update must not be called on invalid domain")
			return nil, nil
		},
	}
	overLong := strings.Repeat("a", 254) // > maxNameLen (253)
	_, err := disktype.New(repo).UpdateAdmin(context.Background(), "block-x", overLong, "", nil, "")
	if !errors.Is(err, storageerr.ErrInvalidArg) {
		t.Fatalf("UpdateAdmin over-long name err = %v, want ErrInvalidArg", err)
	}
}

// TestUpdateAdminDelegates — валидный вход проходит в repo.Update (full-replace).
func TestUpdateAdminDelegates(t *testing.T) {
	want := &domain.DiskType{ID: "block-x", Name: "renamed"}
	var gotID, gotName string
	repo := &repomock.DiskTypeRepo{
		UpdateFunc: func(_ context.Context, id, name, _ string, _ []string, _ string) (*domain.DiskType, error) {
			gotID, gotName = id, name
			return want, nil
		},
	}
	res, err := disktype.New(repo).UpdateAdmin(context.Background(), "block-x", "renamed", "", nil, "")
	if err != nil || res != want {
		t.Fatalf("UpdateAdmin = (%+v, %v)", res, err)
	}
	if gotID != "block-x" || gotName != "renamed" {
		t.Fatalf("repo.Update got (%q, %q)", gotID, gotName)
	}
}

// TestDeleteAdminDelegates — admin Delete проходит в repo (FK RESTRICT-текст мапит
// serviceerr в handler; use-case пробрасывает sentinel).
func TestDeleteAdminDelegates(t *testing.T) {
	sentinel := errors.New("repo delete boom")
	repo := &repomock.DiskTypeRepo{
		DeleteFunc: func(_ context.Context, id string) error {
			if id != "block-x" {
				t.Fatalf("repo got id %q", id)
			}
			return sentinel
		},
	}
	err := disktype.New(repo).DeleteAdmin(context.Background(), "block-x")
	if !errors.Is(err, sentinel) {
		t.Fatalf("DeleteAdmin err = %v, want sentinel", err)
	}
}
