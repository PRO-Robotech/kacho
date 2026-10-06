// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// Служба с документацией, у чьего контракта нет ни одного метода с телом HTTP
// (служба без внешнего API: notify), — законный член реестра. Отметка
// `NoBodyMethods` объявляет это, и объявление судится по дескрипторам, а не
// принимается на слово: у отмеченного пакета методов с телом ноль, и пакет
// вообще зарегистрирован. Метод с телом, появившийся у отмеченного пакета, —
// находка: отметка пережила свой предмет.

import (
	"strings"
	"testing"
)

// nobodyStandOptions — вход стенда с третьим доменом d (страниц у него нет:
// его роль — дескрипторы).
func nobodyStandOptions(t *testing.T, root string, d ClientTruthRequestBodyDomain) ClientTruthRequestBodyOptions {
	t.Helper()
	opts := bodyStandOptions(t, root)
	opts.Domains = append(opts.Domains, d)
	return opts
}

// TestBodyGate_SilentOnDeclaredDomainWithoutBodyMethods — контроль: пакет
// notify без методов с телом, отметка стоит — анализатор отработал, домен в
// переписи с нулём методов и отметкой.
func TestBodyGate_SilentOnDeclaredDomainWithoutBodyMethods(t *testing.T) {
	t.Parallel()
	s := newBodyStand(t)
	var log strings.Builder
	findings, census, err := AuditClientTruthRequestBody(nobodyStandOptions(t, s.root,
		ClientTruthRequestBodyDomain{Name: "notify", ProtoPackage: "kacho.cloud.notify.v1", NoBodyMethods: true}), &log)
	if err != nil {
		t.Fatalf("анализатор отказал на законно отмеченном домене: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings=%v, ожидался 0", findings)
	}
	var seen bool
	for _, d := range census.Domains {
		if d.Name == "notify" {
			seen = true
			if d.Methods != 0 || !d.NoBodyMethods {
				t.Fatalf("перепись notify: методов %d, отметка %v — ожидались 0 и true", d.Methods, d.NoBodyMethods)
			}
		}
	}
	if !seen {
		t.Fatal("домен notify не попал в перепись")
	}
}

// TestBodyGate_RedOnUndeclaredDomainWithoutBodyMethods — близнец: тот же пакет
// без отметки — отказ «не выведено ни одного метода с телом».
func TestBodyGate_RedOnUndeclaredDomainWithoutBodyMethods(t *testing.T) {
	t.Parallel()
	s := newBodyStand(t)
	_, _, err := AuditClientTruthRequestBody(nobodyStandOptions(t, s.root,
		ClientTruthRequestBodyDomain{Name: "notify", ProtoPackage: "kacho.cloud.notify.v1"}), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "notify") || !strings.Contains(err.Error(), "ни одного метода с телом") {
		t.Fatalf("неотмеченный домен без методов с телом: ошибка %v, ожидался отказ с именем домена", err)
	}
}

// TestBodyGate_RedOnDeclaredDomainThatHasBodyMethods — отметка у пакета, где
// методы с телом есть (vpc), — находка: отметка пережила свой предмет.
func TestBodyGate_RedOnDeclaredDomainThatHasBodyMethods(t *testing.T) {
	t.Parallel()
	s := newBodyStand(t)
	_, _, err := AuditClientTruthRequestBody(nobodyStandOptions(t, s.root,
		ClientTruthRequestBodyDomain{Name: "registry", ProtoPackage: "kacho.cloud.registry.v1", NoBodyMethods: true}), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "registry") || !strings.Contains(err.Error(), "отметка") {
		t.Fatalf("отметка у пакета с методами с телом: ошибка %v, ожидался отказ с именем домена и словом «отметка»", err)
	}
}

// TestBodyGate_RedOnDeclaredDomainWhosePackageIsNotRegistered — отметка у
// пакета, чьих дескрипторов нет, — отказ: «методов ноль» о незарегистрированном
// пакете — «не прочитано», а не «нет».
func TestBodyGate_RedOnDeclaredDomainWhosePackageIsNotRegistered(t *testing.T) {
	t.Parallel()
	s := newBodyStand(t)
	_, _, err := AuditClientTruthRequestBody(nobodyStandOptions(t, s.root,
		ClientTruthRequestBodyDomain{Name: "absent", ProtoPackage: "kacho.cloud.absent.v1", NoBodyMethods: true}), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "kacho.cloud.absent.v1") || !strings.Contains(err.Error(), "не зарегистрирован") {
		t.Fatalf("отметка у незарегистрированного пакета: ошибка %v, ожидался отказ «не зарегистрирован»", err)
	}
}
