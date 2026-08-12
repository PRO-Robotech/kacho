// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cephrbd_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/clients/cephrbd"
)

// Адаптер проверяется ЦЕЛИКОМ без живого кластера: исполнитель команд подставлен, и
// каждая ветка — включая классификацию отказов — исполняется здесь.
//
// Это не «проба на моке вместо настоящей». Предмет адаптера ровно один: превратить
// глагол порта в команду инструмента и ответ инструмента — в полосу исхода. Обе
// стороны этого превращения наблюдаемы отсюда полностью; за кластер адаптер не
// отвечает и отвечать не может.
//
// Приёмка STOR-P-58, 59, 60, 61, 63.

// scriptedRunner отвечает заготовленным выводом на каждую команду и ЗАПОМИНАЕТ
// аргументы: проба обязана утверждать не только исход, но и то, что именно было
// сказано инструменту.
type scriptedRunner struct {
	mu    sync.Mutex
	calls [][]string
	reply func(args []string) (stdout, stderr []byte, code int, err error)
}

func (r *scriptedRunner) Run(_ context.Context, args ...string) ([]byte, []byte, int, error) {
	r.mu.Lock()
	r.calls = append(r.calls, args)
	r.mu.Unlock()
	if r.reply == nil {
		return nil, nil, 0, nil
	}
	return r.reply(args)
}

func (r *scriptedRunner) said(sub string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if strings.Contains(strings.Join(c, " "), sub) {
			return true
		}
	}
	return false
}

func ref() blockbackend.ObjectRef {
	return blockbackend.ObjectRef{
		Locator: blockbackend.Locator{Pool: "kacho-block", Namespace: "prj-1"},
		Name:    "kc7f-vol0a7b3c9d2e5f8g1hj",
	}
}

func TestCreateVolume_SpeaksSizeAndFullSpec(t *testing.T) {
	t.Parallel()
	r := &scriptedRunner{}
	a := cephrbd.New(r)

	if err := a.CreateVolume(context.Background(), blockbackend.VolumeSpec{
		Ref: ref(), SizeBytes: 4 << 30,
	}); err != nil {
		t.Fatalf("создание не должно было отказать: %v", err)
	}
	if !r.said("create") || !r.said("4096M") {
		t.Errorf("инструменту не назван размер: %v", r.calls)
	}
	// Пространство арендатора обязано попадать в адрес: без него объект лёг бы в
	// общее пространство пула, и изоляция арендаторов исчезла бы молча.
	if !r.said("kacho-block/prj-1/kc7f-vol0a7b3c9d2e5f8g1hj") {
		t.Errorf("адрес объекта не несёт пространства арендатора: %v", r.calls)
	}
}

func TestCreateVolume_ExistingWithDifferentSizeIsConflict(t *testing.T) {
	t.Parallel()
	r := &scriptedRunner{reply: func(args []string) ([]byte, []byte, int, error) {
		switch {
		case args[0] == "create":
			return nil, []byte("rbd: error: (17) File exists"), 17, nil
		case args[0] == "info":
			return []byte(`{"size": 1073741824}`), nil, 0, nil
		}
		return nil, nil, 0, nil
	}}
	a := cephrbd.New(r)

	err := a.CreateVolume(context.Background(), blockbackend.VolumeSpec{Ref: ref(), SizeBytes: 4 << 30})
	if got := blockbackend.OutcomeOf(err); got != blockbackend.OutcomeConflict {
		t.Fatalf("повтор с ДРУГИМ размером обязан давать конфликт, получено %s (%v)", got, err)
	}
}

func TestCreateVolume_ExistingWithSameSizeIsSuccess(t *testing.T) {
	t.Parallel()
	r := &scriptedRunner{reply: func(args []string) ([]byte, []byte, int, error) {
		switch {
		case args[0] == "create":
			return nil, []byte("rbd: error: (17) File exists"), 17, nil
		case args[0] == "info":
			return []byte(`{"size": 4294967296}`), nil, 0, nil
		}
		return nil, nil, 0, nil
	}}
	a := cephrbd.New(r)

	if err := a.CreateVolume(context.Background(), blockbackend.VolumeSpec{Ref: ref(), SizeBytes: 4 << 30}); err != nil {
		t.Fatalf("повтор с ТЕМ ЖЕ размером обязан быть успехом: %v", err)
	}
}

func TestDeleteVolume_AbsentIsSuccess(t *testing.T) {
	t.Parallel()
	r := &scriptedRunner{reply: func([]string) ([]byte, []byte, int, error) {
		return nil, []byte("rbd: error opening image: (2) No such file or directory"), 2, nil
	}}
	if err := cephrbd.New(r).DeleteVolume(context.Background(), ref()); err != nil {
		t.Fatalf("удаление отсутствующего обязано быть успехом: %v", err)
	}
}

func TestObserve_AbsentIsStateNotError(t *testing.T) {
	t.Parallel()
	r := &scriptedRunner{reply: func([]string) ([]byte, []byte, int, error) {
		return nil, []byte("rbd: error opening image: (2) No such file or directory"), 2, nil
	}}
	obs, err := cephrbd.New(r).Observe(context.Background(), ref())
	if err != nil {
		t.Fatalf("отсутствие объекта — это СОСТОЯНИЕ, а не ошибка чтения: %v", err)
	}
	if obs.State != blockbackend.ObservedAbsent {
		t.Errorf("ожидалось отсутствие, получено %v", obs.State)
	}
}

func TestObserve_UnavailableIsErrorNotAbsent(t *testing.T) {
	t.Parallel()
	r := &scriptedRunner{reply: func([]string) ([]byte, []byte, int, error) {
		return nil, []byte("rbd: couldn't connect to the cluster: (110) Connection timed out"), 1, nil
	}}
	_, err := cephrbd.New(r).Observe(context.Background(), ref())
	if err == nil {
		t.Fatal("недоступность кластера обязана быть ОШИБКОЙ: молчание не есть утверждение об отсутствии")
	}
	if got := blockbackend.OutcomeOf(err); got != blockbackend.OutcomeUnavailable {
		t.Errorf("полоса %s, ожидалась повторяемая недоступность", got)
	}
}

func TestObserve_UsedBytesAbsentWhenClusterDidNotSay(t *testing.T) {
	t.Parallel()
	r := &scriptedRunner{reply: func(args []string) ([]byte, []byte, int, error) {
		if args[0] == "info" {
			return []byte(`{"size": 1073741824}`), nil, 0, nil
		}
		// учёт занятого отказал — кластер вправе
		return nil, []byte("rbd: du failed: (110) Connection timed out"), 1, nil
	}}
	obs, err := cephrbd.New(r).Observe(context.Background(), ref())
	if err != nil {
		t.Fatalf("отказ учёта занятого не обязан ронять наблюдение: %v", err)
	}
	if obs.HasUsedBytes {
		t.Error("потребление не сообщено — поле обязано остаться НЕзаполненным")
	}
	if obs.UsedBytes != 0 {
		t.Error("ноль на месте несообщённого потребления был бы утверждением о пустом томе")
	}
}

func TestObserve_WrongOutputFormatIsMisconfiguration(t *testing.T) {
	t.Parallel()
	r := &scriptedRunner{reply: func(args []string) ([]byte, []byte, int, error) {
		if args[0] == "info" {
			return []byte("<html>not this tool at all</html>"), nil, 0, nil
		}
		return nil, nil, 0, nil
	}}
	_, err := cephrbd.New(r).Observe(context.Background(), ref())
	if got := blockbackend.OutcomeOf(err); got != blockbackend.OutcomeMisconfigured {
		t.Errorf("ответ не того формата — это НАСТРОЙКА, а не сбой; полоса %s", got)
	}
}

func TestRun_ToolMissingIsMisconfigurationNotUnavailable(t *testing.T) {
	t.Parallel()
	r := &scriptedRunner{reply: func([]string) ([]byte, []byte, int, error) {
		return nil, nil, -1, errors.New("exec: \"rbd\": executable file not found in $PATH")
	}}
	err := cephrbd.New(r).DeleteVolume(context.Background(), ref())
	if got := blockbackend.OutcomeOf(err); got != blockbackend.OutcomeMisconfigured {
		t.Errorf("отсутствие инструмента — настройка, а не недоступность кластера; полоса %s", got)
	}
}

func TestClassify_ClosedSetWithPairedControls(t *testing.T) {
	t.Parallel()

	cases := map[string]blockbackend.Outcome{
		"rbd: error opening image: (2) No such file or directory": blockbackend.OutcomeNotFound,
		"rbd: error: (17) File exists":                            blockbackend.OutcomeConflict,
		"rbd: write failed: (28) No space left on device":         blockbackend.OutcomeCapacityExhausted,
		"rbd: couldn't connect: (110) Connection timed out":       blockbackend.OutcomeUnavailable,
		"rbd: error: (1) Operation not permitted":                 blockbackend.OutcomeDenied,
		"rbd: unrecognized command":                               blockbackend.OutcomeMisconfigured,
		"rbd: image still has watchers":                           blockbackend.OutcomeRejected,
	}
	for stderr, want := range cases {
		got := classifyThrough(t, stderr)
		if got != want {
			t.Errorf("сообщение %q классифицировано как %s, ожидалось %s", stderr, got, want)
		}
	}

	// Неопознанный ответ НЕ становится догадкой и НЕ считается временным: повтор
	// того, чего мы не поняли, либо не пройдёт никогда и вечно держит голову
	// очереди, либо пройдёт по причине, которой мы не знаем.
	got := classifyThrough(t, "rbd: something nobody has ever seen")
	if got != blockbackend.OutcomeUnclassified {
		t.Errorf("незнакомый ответ обязан оставаться неклассифицированным, получено %s", got)
	}
	if got.Retryable() {
		t.Error("неклассифицированный исход не может быть повторяемым")
	}
}

// classifyThrough прогоняет сообщение через настоящий путь адаптера: классификатор
// не экспортирован намеренно — проверять его в обход адаптера значило бы проверять
// не то, что исполняется.
func classifyThrough(t *testing.T, stderr string) blockbackend.Outcome {
	t.Helper()
	r := &scriptedRunner{reply: func([]string) ([]byte, []byte, int, error) {
		return nil, []byte(stderr), 1, nil
	}}
	err := cephrbd.New(r).ResizeVolume(context.Background(), ref(), 1<<30)
	return blockbackend.OutcomeOf(err)
}

func TestValidateRef_NoMoreLenientThanTheDouble(t *testing.T) {
	t.Parallel()
	a := cephrbd.New(&scriptedRunner{})
	base := ref()

	for name, bad := range map[string]blockbackend.ObjectRef{
		"без пула":         {Locator: blockbackend.Locator{Namespace: base.Namespace}, Name: base.Name},
		"без пространства": {Locator: blockbackend.Locator{Pool: base.Pool}, Name: base.Name},
		"без имени":        {Locator: base.Locator},
	} {
		err := a.CreateVolume(context.Background(), blockbackend.VolumeSpec{Ref: bad, SizeBytes: 1 << 30})
		if got := blockbackend.OutcomeOf(err); got != blockbackend.OutcomeRejected {
			t.Errorf("%s: адаптер обязан отвергать неполный адрес тем же исходом, что дублёр; получено %s", name, got)
		}
	}
	// Положительный контроль: полный адрес принимается — иначе проба зеленела бы
	// на реализации, отвергающей вообще всё.
	if err := a.CreateVolume(context.Background(), blockbackend.VolumeSpec{Ref: base, SizeBytes: 1 << 30}); err != nil {
		t.Errorf("полный адрес обязан приниматься: %v", err)
	}
}

func TestExecRunner_ValidateNamesEveryMissingPiece(t *testing.T) {
	t.Parallel()
	full := cephrbd.ExecRunner{
		Binary: "/usr/bin/rbd", ConfPath: "/etc/ceph/ceph.conf",
		KeyringPath: "/etc/ceph/keyring", ClientName: "client.kacho", Timeout: 1,
	}
	if err := full.Validate(); err != nil {
		t.Fatalf("полный исполнитель обязан проходить: %v", err)
	}
	for name, mut := range map[string]func(*cephrbd.ExecRunner){
		"нет бинаря":   func(r *cephrbd.ExecRunner) { r.Binary = "" },
		"нет конфига":  func(r *cephrbd.ExecRunner) { r.ConfPath = "" },
		"нет ключа":    func(r *cephrbd.ExecRunner) { r.KeyringPath = "" },
		"нет имени":    func(r *cephrbd.ExecRunner) { r.ClientName = "" },
		"нулевой срок": func(r *cephrbd.ExecRunner) { r.Timeout = 0 },
		"срок < 0":     func(r *cephrbd.ExecRunner) { r.Timeout = -1 },
	} {
		r := full
		mut(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("%s: неполный исполнитель обязан отвергаться на СБОРКЕ, а не на первом обращении", name)
		}
	}
}
