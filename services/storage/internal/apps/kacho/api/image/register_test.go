// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package image_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// testInstallPrefix — префикс установки для проб образа.
//
// Задаётся явно, а не умолчанием: отсутствие префикса обязано оставаться
// наблюдаемым отказом (TestImageCreateWithoutInstallPrefixIsRefused), а не тихо
// подставленной пустой строкой в имени объекта.
const testInstallPrefix = "kctest"

// okPeers — соседи, отвечающие успехом. Отдельный конструктор нужен, чтобы пробы,
// проверяющие ПОРЯДОК (сервис отказывает ДО обращения к соседям), могли подменить
// его на панические моки и увидеть разницу.
func okPeers() (*repomock.PeerClient, *repomock.PeerClient) {
	geo := &repomock.PeerClient{EnsureRegionFunc: func(context.Context, string) error { return nil }}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	return geo, iam
}

// TestImageCreateWithoutInstallPrefixIsRefused — посадка без префикса установки не
// создаёт образов, отказ говорит о СЕРВИСЕ, и он приходит ДО обращений к соседям.
//
// Порядок несущий: посадка без префикса не создаст образ ни при каком вводе, поэтому
// платить за вызовы владельцам региона и проекта незачем. Пробы соседей падают,
// если их всё-таки позвали.
func TestImageCreateWithoutInstallPrefixIsRefused(t *testing.T) {
	geo := &repomock.PeerClient{EnsureRegionFunc: func(context.Context, string) error {
		t.Fatal("geo не должен вызываться: сервис уже неспособен исполнить запрос")
		return nil
	}}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error {
		t.Fatal("iam не должен вызываться: сервис уже неспособен исполнить запрос")
		return nil
	}}
	uc := image.New(&repomock.ImageReader{}, &repomock.ImageWriter{}, geo, iam, nil, serviceerr.ToStatus).
		WithDataPlane(true) // отказ ждут ТАМ, ГДЕ плоскость данных объявлена: без неё имя объекта выводить не для чего

	_, err := uc.Create(context.Background(), &domain.Image{
		ProjectID: "prj-1", RegionID: "ru-central1", Name: "img-a",
		SourceSnapshot: "snp00000000000000000",
	})
	if err == nil {
		t.Fatal("посадка без префикса установки обязана отказывать в создании образа")
	}
	if got := status.Code(err); got != codes.Unavailable {
		t.Errorf("код %s: отказ обязан говорить о неспособности сервиса, а не о неверном вводе", got)
	}
}

// TestImageCreateDerivesBackendObject — имя объекта у бэкенда ВЫВОДИТСЯ из префикса
// установки и неизменяемого идентификатора образа, а не приходит из запроса.
//
// Положительный полюс к пробе выше: с префиксом создание идёт, и имя объекта
// детерминировано. Проба подаёт СВОЁ имя во входе и убеждается, что оно
// проигнорировано — принимая имя из запроса, сервис позволил бы вызывающему
// адресовать чужой объект.
func TestImageCreateDerivesBackendObject(t *testing.T) {
	var seen domain.Image
	writer := &repomock.ImageWriter{
		InsertFunc: func(_ context.Context, i *domain.Image, _ []string) (*domain.Image, error) {
			seen = *i
			out := *i
			return &out, nil
		},
	}
	geo, iam := okPeers()
	ops := repomock.NewOpsRepo()
	uc := image.New(&repomock.ImageReader{}, writer, geo, iam, ops, serviceerr.ToStatus).
		WithInstallPrefix(testInstallPrefix)

	op, err := uc.Create(context.Background(), &domain.Image{
		ProjectID: "prj-1", RegionID: "ru-central1", Name: "img-a",
		SourceSnapshot:  "snp00000000000000000",
		Backend:         domain.Placement{BackendObject: "attacker-supplied"},
		SeededVolumeIDs: []string{"vol00000000000000000"},
	})
	if err != nil {
		t.Fatalf("Create sync err = %v", err)
	}
	repomock.AwaitOpDone(t, ops, op.ID)

	want := testInstallPrefix + "-" + seen.ID
	if seen.Backend.BackendObject != want {
		t.Errorf("имя объекта = %q, ожидалось выведенное %q (имя из запроса обязано игнорироваться)",
			seen.Backend.BackendObject, want)
	}
}

// TestImageRegisterRefusesIncompleteInput — регистрация проверяет форму СИНХРОННО,
// до обращений к соседям и до записи.
//
// Каждое отрицание — в паре с положительным контролем ниже (тот же вход, поле
// исправлено), иначе отказ зеленел бы на реализации, отвергающей вообще всё.
func TestImageRegisterRefusesIncompleteInput(t *testing.T) {
	valid := image.RegisterInput{
		ProjectID: "prj-1", RegionID: "ru-central1", Name: "ubuntu-24-04",
		BackendObject: "kc7f-img-ubuntu-2404-20260812",
		SizeBytes:     21474836480, MinDiskBytes: 21474836480,
	}

	cases := []struct {
		name  string
		spoil func(in *image.RegisterInput)
	}{
		{"без проекта", func(in *image.RegisterInput) { in.ProjectID = "" }},
		{"без региона", func(in *image.RegisterInput) { in.RegionID = "" }},
		{"имя не по форме", func(in *image.RegisterInput) { in.Name = "Ubuntu_24" }},
		{"без имени объекта", func(in *image.RegisterInput) { in.BackendObject = "" }},
		{"размер не назван", func(in *image.RegisterInput) { in.SizeBytes = 0 }},
		{"приёмная ёмкость не названа", func(in *image.RegisterInput) { in.MinDiskBytes = 0 }},
		{"описание длиннее границы", func(in *image.RegisterInput) { in.Description = strings.Repeat("a", 257) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			geo := &repomock.PeerClient{EnsureRegionFunc: func(context.Context, string) error {
				t.Fatal("форма проверяется ДО обращения к соседям")
				return nil
			}}
			iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error {
				t.Fatal("форма проверяется ДО обращения к соседям")
				return nil
			}}
			writer := &repomock.ImageWriter{
				RegisterFunc: func(context.Context, *domain.Image) (*domain.Image, error) {
					t.Fatal("запись не должна произойти на невалидном вводе")
					return nil, nil
				},
			}
			uc := image.New(&repomock.ImageReader{}, writer, geo, iam, nil, serviceerr.ToStatus)

			in := valid
			c.spoil(&in)
			_, err := uc.Register(context.Background(), in)
			if got := status.Code(err); got != codes.InvalidArgument {
				t.Fatalf("код %s, ожидался InvalidArgument", got)
			}
		})
	}

	// Положительный контроль: тот же вход целиком — регистрация проходит.
	t.Run("полный вход принимается", func(t *testing.T) {
		geo, iam := okPeers()
		writer := &repomock.ImageWriter{
			RegisterFunc: func(_ context.Context, i *domain.Image) (*domain.Image, error) {
				out := *i
				return &out, nil
			},
		}
		uc := image.New(&repomock.ImageReader{}, writer, geo, iam, nil, serviceerr.ToStatus)
		if _, err := uc.Register(context.Background(), valid); err != nil {
			t.Fatalf("полный вход отвергнут: %v", err)
		}
	})
}

// TestImageRegisterFailsClosedOnPeers — недоступность владельца региона либо проекта
// НЕ создаёт строку: непроверенное предусловие не считается выполненным.
func TestImageRegisterFailsClosedOnPeers(t *testing.T) {
	valid := image.RegisterInput{
		ProjectID: "prj-1", RegionID: "ru-central1", Name: "ubuntu-24-04",
		BackendObject: "kc7f-img-ubuntu", SizeBytes: 1 << 30, MinDiskBytes: 1 << 30,
	}
	noWrite := &repomock.ImageWriter{
		RegisterFunc: func(context.Context, *domain.Image) (*domain.Image, error) {
			t.Fatal("запись не должна произойти при недоступном соседе")
			return nil, nil
		},
	}

	t.Run("владелец региона недоступен", func(t *testing.T) {
		geo := &repomock.PeerClient{EnsureRegionFunc: func(context.Context, string) error {
			return status.Error(codes.Unavailable, "geo region validation unavailable")
		}}
		iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
		uc := image.New(&repomock.ImageReader{}, noWrite, geo, iam, nil, serviceerr.ToStatus)
		_, err := uc.Register(context.Background(), valid)
		if got := status.Code(err); got != codes.Unavailable {
			t.Fatalf("код %s, ожидался Unavailable", got)
		}
	})

	t.Run("владелец проекта недоступен", func(t *testing.T) {
		geo := &repomock.PeerClient{EnsureRegionFunc: func(context.Context, string) error { return nil }}
		iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error {
			return status.Error(codes.Unavailable, "iam project validation unavailable")
		}}
		uc := image.New(&repomock.ImageReader{}, noWrite, geo, iam, nil, serviceerr.ToStatus)
		_, err := uc.Register(context.Background(), valid)
		if got := status.Code(err); got != codes.Unavailable {
			t.Fatalf("код %s, ожидался Unavailable", got)
		}
	})
}

// TestImageRegisterKeepsSuppliedObjectName — имя объекта ПРИХОДИТ ИЗВНЕ и остаётся
// собой даже там, где префикс установки задан.
//
// Это единственное место контракта, где имя не выводится: объект внесён в хранилище
// ДО того, как у облака появилась строка, и его имя — факт провайдера, а не наше
// решение. Проба подаёт префикс установки вторым полюсом: если бы регистрация
// выводила имя, как публичный Create, оно бы здесь подменилось — и облако указывало
// бы на несуществующий объект.
//
// Заодно: у зарегистрированного образа нет источника ВНУТРИ облака — выводить
// размеры не из чего, поэтому их называет регистрирующий.
func TestImageRegisterKeepsSuppliedObjectName(t *testing.T) {
	const object = "provider-chosen-object-name"
	var seen domain.Image
	writer := &repomock.ImageWriter{
		RegisterFunc: func(_ context.Context, i *domain.Image) (*domain.Image, error) {
			seen = *i
			out := *i
			out.Status = domain.ImageStatusReady
			out.Observation = domain.Observation{State: domain.ObservedReady}
			return &out, nil
		},
	}
	geo, iam := okPeers()
	uc := image.New(&repomock.ImageReader{}, writer, geo, iam, nil, serviceerr.ToStatus).
		WithInstallPrefix(testInstallPrefix)

	got, err := uc.Register(context.Background(), image.RegisterInput{
		ProjectID: "prj-1", RegionID: "ru-central1", Name: "ubuntu-24-04",
		BackendObject: object, SizeBytes: 21474836480, MinDiskBytes: 21474836480,
	})
	if err != nil {
		t.Fatalf("Register err = %v", err)
	}
	if seen.Backend.BackendObject != object {
		t.Errorf("имя объекта = %q, ожидалось названное регистрирующим %q", seen.Backend.BackendObject, object)
	}
	if seen.ID == "" || seen.ID[:3] != domain.PrefixImage {
		t.Errorf("идентификатор = %q, ожидался выданный облаком img-идентификатор", seen.ID)
	}
	if seen.SourceSnapshot != "" || seen.SourceVolume != "" {
		t.Errorf("у зарегистрированного образа нет источника внутри облака, получено snapshot=%q volume=%q",
			seen.SourceSnapshot, seen.SourceVolume)
	}
	if got.Status != domain.ImageStatusReady {
		t.Errorf("статус = %v, зарегистрированный образ рождается готовым", got.Status)
	}
	if got.Observation.State != domain.ObservedReady {
		t.Errorf("наблюдённое состояние = %q, ожидалось READY: объект у бэкенда уже существует",
			got.Observation.State)
	}
}
