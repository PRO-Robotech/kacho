// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// grpcmountparity_test.go — гейт на класс «сервис объявлен контрактом, но не
// смонтирован», плюс доказательство того, что сам анализатор способен упасть.
//
// РАДИУС. Полоса маршрутизируемости закрыла край: и разрешённый список gRPC, и
// таблицы REST-биндингов сводятся с настоящей картой соединений. Ниже края тот
// же класс живёт на СЕМИ парах листенеров сервисов (:9090/:9091), где монтирование
// — рукописный вызов в композиционном корне. Отказ там производит сам grpc-go
// (`Unimplemented`) и одинаков для «сервиса нет» и «сервис намеренно не поднят на
// этом порту», поэтому недоделка не производит никакого симптома, кроме «RPC не
// работает».
//
// ЧТО БЫЛО ВМЕСТО ЭТОГО (ревизия 89242e6b). Классификация «поднято / не поднято»
// существует у ДВУХ сервисов из семи (compute, vpc) и в обоих случаях объявлена
// РУКОПИСНЫМ списком `servedPublicServiceDescs`/`servedInternalServiceDescs`. Список
// — утверждение о композиционном корне, а не измерение его: снеси регистрацию из
// `main.go`, и список продолжит утверждать прежнее. У остальных пяти нет и этого.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mountAllow — сервисы, намеренно не поднимаемые по gRPC. Каждая запись обязана
// иметь предмет: анализатор сам краснеет на записи, которой больше нечего
// исключать (сервис смонтирован либо исчез из контракта).
// СЕЙЧАС ПУСТ, и это исход, а не упущение. Здесь стояли четыре сервиса, ни один
// из которых не был смонтирован ни в одном композиционном корне: хуки Hydra
// (обслуживаются по HTTP СВОИМИ структурами тела запроса — типы этого proto не
// читала ни одна строка неgenerated-кода), фид жизненного цикла в compute.v1 и
// vpc.v1 (живой — только в loadbalancer.v1) и поток событий в vpc.v1 (живой —
// только в compute.v1). Ни у одного не было ни сервера, ни клиента, ни
// неgenerated-ссылки — включая типы сообщений. Это были объявления без единой
// реализации, и они сняты с контракта целиком; надгробие — retiredRPCSurface в
// retiredrpcsurface_test.go.
//
// Пустой список означает: каждый объявленный контрактом gRPC-сервис смонтирован.
// Способность анализатора видеть несмонтированный сервис от этой пустоты не
// зависит — она доказана инъекцией, см.
// TestGRPCMountParity_SeesAnUnmountedServiceAsUnmounted.
var mountAllow = []string{}

func mountOptions(t *testing.T) MountOptions {
	t.Helper()
	return MountOptions{
		Root:       repoRoot(t),
		APIRoot:    "pkg/api",
		ModulePath: "github.com/PRO-Robotech/kacho",
		Roots:      []string{"services", "gateway"},
		Allow:      mountAllow,
	}
}

// TestGRPCMountParity_EveryDeclaredServiceIsMounted — положительная сторона на
// НАСТОЯЩЕМ дереве.
func TestGRPCMountParity_EveryDeclaredServiceIsMounted(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditGRPCMountParity(mountOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: анализатор действительно что-то прочитал и что-то нашёл
	// смонтированным. Ноль находок обязано быть отличимо от нуля прочитанного.
	if census.OwningBinaries < 5 {
		t.Fatalf("монтирующих композиционных корней найдено %d (< 5) — разбор не нашёл того, "+
			"что заведомо есть, и «расхождений нет» получено даром", census.OwningBinaries)
	}
	if census.MountedSvcs == 0 || census.DeclaredSvcs == 0 {
		t.Fatalf("объявлено %d, смонтировано %d — предмета нет", census.DeclaredSvcs, census.MountedSvcs)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("%d расхождений между контрактом и композиционными корнями:\n%s",
		len(findings), strings.Join(lines, "\n"))
}

// ── доказательство того, что анализатор способен упасть ─────────────────────

// tinyTree материализует минимальное дерево: стабы двух сервисов одного
// proto-пакета и композиционный корень, монтирующий их.
func tinyTree(t *testing.T, mountBoth bool, extraImportAlias string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write("pkg/api/kacho/cloud/demo/v1/demo_grpc.pb.go", `package demov1

var AlphaService_ServiceDesc = struct{ ServiceName string }{ServiceName: "kacho.cloud.demo.v1.AlphaService"}
var BetaService_ServiceDesc = struct{ ServiceName string }{ServiceName: "kacho.cloud.demo.v1.BetaService"}

func RegisterAlphaServiceServer(s any, i any) {}
func RegisterBetaServiceServer(s any, i any) {}
`)
	alias := extraImportAlias
	if alias != "" {
		alias += " "
	}
	body := "\tdemov1.RegisterAlphaServiceServer(srv, nil)\n"
	if mountBoth {
		body += "\tdemov1.RegisterBetaServiceServer(srv, nil)\n"
	} else {
		body += "\t// demov1.RegisterBetaServiceServer(srv, nil) — снесено, фраза осталась\n"
	}
	write("services/demo/cmd/demo/main.go", `package main

import (
	`+alias+`"github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/demo/v1"
)

var srv any

func main() {
`+body+`}
`)
	return root
}

func tinyOptions(root string, allow ...string) MountOptions {
	return MountOptions{
		Root:       root,
		APIRoot:    "pkg/api",
		ModulePath: "github.com/PRO-Robotech/kacho",
		Roots:      []string{"services"},
		Allow:      allow,
	}
}

// TestGRPCMountParity_SeesAnUnmountedServiceAsUnmounted — отрицание в паре с
// положительным: тот же анализатор на дереве, где один сервис не смонтирован, а
// объясняющая его фраза осталась (обычная форма удаления).
func TestGRPCMountParity_SeesAnUnmountedServiceAsUnmounted(t *testing.T) {
	// Законный близнец: то же дерево, оба сервиса подняты — анализатор молчит.
	if f, _, err := AuditGRPCMountParity(tinyOptions(tinyTree(t, true, "")), nil); err != nil || len(f) != 0 {
		t.Fatalf("на исправном дереве анализатор нашёл %v (err=%v) — он реагирует не на предмет", f, err)
	}

	f, _, err := AuditGRPCMountParity(tinyOptions(tinyTree(t, false, "")), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(f) != 1 || f[0].Kind != "unmounted" || f[0].FQN != "kacho.cloud.demo.v1.BetaService" {
		t.Fatalf("ожидалась ровно одна находка про BetaService, получено: %v", f)
	}
}

// TestGRPCMountParity_ReadsTheImportAliasAndNotThePathTail — у сгенерированных
// пакетов последний сегмент пути «v1», поэтому имя импорта обязано браться из
// алиаса либо из объявления пакета. Проба разом на обе формы.
func TestGRPCMountParity_ReadsTheImportAliasAndNotThePathTail(t *testing.T) {
	for _, alias := range []string{"", "demopb"} {
		root := tinyTree(t, true, alias)
		src := filepath.Join(root, "services/demo/cmd/demo/main.go")
		if alias != "" {
			b, err := os.ReadFile(src)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if err := os.WriteFile(src, []byte(strings.ReplaceAll(string(b), "demov1.", alias+".")), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
		f, c, err := AuditGRPCMountParity(tinyOptions(root), nil)
		if err != nil {
			t.Fatalf("alias=%q: %v", alias, err)
		}
		if c.MountedSvcs != 2 || len(f) != 0 {
			t.Fatalf("alias=%q: смонтировано %d (ожидалось 2), находок %v — имя импорта разобрано неверно",
				alias, c.MountedSvcs, f)
		}
	}
}

// TestGRPCMountParity_AStaleAllowIsItselfAFinding — исключение живёт, пока у него
// есть предмет. Обе формы протухания: сервис смонтирован и сервиса нет в контракте.
func TestGRPCMountParity_AStaleAllowIsItselfAFinding(t *testing.T) {
	root := tinyTree(t, true, "")
	for _, tc := range []struct{ name, allow string }{
		{"сервис смонтирован", "kacho.cloud.demo.v1.BetaService"},
		{"сервиса нет в контракте", "kacho.cloud.demo.v1.GammaService"},
	} {

		t.Run(tc.name, func(t *testing.T) {
			f, _, err := AuditGRPCMountParity(tinyOptions(root, tc.allow), nil)
			if err != nil {
				t.Fatalf("анализатор не отработал: %v", err)
			}
			if len(f) != 1 || f[0].Kind != "stale-allow" || f[0].FQN != tc.allow {
				t.Fatalf("ожидалась находка про протухшее исключение %q, получено: %v", tc.allow, f)
			}
		})
	}

	// Законный близнец: исключение, у которого предмет ЕСТЬ, молчит.
	f, _, err := AuditGRPCMountParity(tinyOptions(tinyTree(t, false, ""), "kacho.cloud.demo.v1.BetaService"), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(f) != 0 {
		t.Fatalf("исключение с живым предметом дало находки %v — анализатор краснеет на чём угодно", f)
	}
}

// TestGRPCMountParity_EmptySubjectIsAnError — «ничего не прочитано» не должно
// быть неотличимо от «расхождений нет».
func TestGRPCMountParity_EmptySubjectIsAnError(t *testing.T) {
	if _, _, err := AuditGRPCMountParity(tinyOptions(t.TempDir()), nil); err == nil {
		t.Fatal("на пустом дереве анализатор вернул успех — ноль находок стал неотличим от нуля прочитанного")
	}
}
