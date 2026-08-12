// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cephrbd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
)

// Kind — имя вида бэкенда в реестре адаптеров и в ресурсе StorageBackend.
const Kind = "CEPH_RBD"

// Adapter реализует blockbackend.Backend поверх инструмента RBD.
type Adapter struct {
	runner Runner
	caps   blockbackend.Capabilities
}

// New собирает адаптер.
//
// Способности объявляются ЗДЕСЬ константами, а не опрашиваются у кластера. Опрос был
// бы протоколом без второй стороны: инструмент не отвечает на вопрос «умеешь ли ты
// клоны», он либо делает, либо отказывает. Объявленное здесь сверяется контрактной
// суитой — той же, что проверяет дублёр.
func New(runner Runner) *Adapter {
	return &Adapter{
		runner: runner,
		caps: blockbackend.Capabilities{
			Snapshots:         true,
			CloneFromSnapshot: true,
			CloneFromImage:    true,
			// Клон остаётся зависимым от родителя, пока его не отвязали. Это
			// свойство хранилища, а не наш выбор: объявив обратное, мы обещали бы
			// удаление источника, которое кластер не исполнит.
			CloneKeepsParent: true,
			OnlineGrow:       true,
			// Множественная привязка требует снятия исключительной блокировки на
			// образе — решение уровня посадки, а не адаптера. Объявляем то, что
			// умеем ПО УМОЛЧАНИЮ; включат — объявит привязка класса.
			MultiAttach:      false,
			EncryptionAtRest: false,
		},
	}
}

// Kind возвращает вид бэкенда.
func (a *Adapter) Kind() string { return Kind }

// Capabilities возвращает объявленные способности.
func (a *Adapter) Capabilities() blockbackend.Capabilities { return a.caps }

// spec собирает адрес объекта в форме, которую понимает инструмент.
func spec(ref blockbackend.ObjectRef) string {
	if ref.Namespace == "" {
		return ref.Pool + "/" + ref.Name
	}
	return ref.Pool + "/" + ref.Namespace + "/" + ref.Name
}

// run исполняет глагол и переводит ненулевой исход в полосу.
func (a *Adapter) run(ctx context.Context, op, object string, args ...string) ([]byte, error) {
	stdout, stderr, code, err := a.runner.Run(ctx, args...)
	if err != nil {
		// Команду не удалось исполнить вовсе — это НАСТРОЙКА либо отсутствие
		// инструмента, а не ответ кластера. Полоса отдельная, и она громкая.
		return nil, blockbackend.Errorf(blockbackend.OutcomeMisconfigured, op, object, err)
	}
	if code != 0 {
		return stdout, blockbackend.Errorf(classify(stderr, code), op, object,
			fmt.Errorf("exit %d: %s", code, string(stderr)))
	}
	return stdout, nil
}

// CreateVolume создаёт образ. Существует с тем же размером → успех; с другим →
// конфликт.
func (a *Adapter) CreateVolume(ctx context.Context, s blockbackend.VolumeSpec) error {
	if err := validateRef("CreateVolume", s.Ref); err != nil {
		return err
	}
	if s.SizeBytes <= 0 {
		return blockbackend.Errorf(blockbackend.OutcomeRejected, "CreateVolume", s.Ref.Name,
			fmt.Errorf("size must be positive"))
	}
	_, err := a.run(ctx, "CreateVolume", s.Ref.Name,
		"create", "--size", strconv.FormatInt(s.SizeBytes>>20, 10)+"M", spec(s.Ref))
	if err == nil {
		return a.applyQoS(ctx, s.Ref, s.QoS)
	}
	if blockbackend.OutcomeOf(err) != blockbackend.OutcomeConflict {
		return err
	}
	// Объект уже есть: повтор идемпотентен ТОЛЬКО при совпадающем размере. Молча
	// принять расхождение значило бы сделать ненаходимым несовпадение намерения с
	// фактом — и повтор переписал бы размер уже созданного объекта.
	observed, oerr := a.Observe(ctx, s.Ref)
	if oerr != nil {
		return oerr
	}
	if observed.SizeBytes != s.SizeBytes {
		return blockbackend.Errorf(blockbackend.OutcomeConflict, "CreateVolume", s.Ref.Name,
			fmt.Errorf("object exists with size %d, requested %d", observed.SizeBytes, s.SizeBytes))
	}
	return a.applyQoS(ctx, s.Ref, s.QoS)
}

// applyQoS проставляет ограничения на объект. Пустой набор означает «класс чисел не
// объявил», а не нулевые ограничения — и тогда мы не трогаем ничего.
func (a *Adapter) applyQoS(ctx context.Context, ref blockbackend.ObjectRef, qos map[string]int64) error {
	for key, value := range qos {
		if value <= 0 {
			continue
		}
		if _, err := a.run(ctx, "CreateVolume", ref.Name,
			"config", "image", "set", spec(ref), key, strconv.FormatInt(value, 10)); err != nil {
			return err
		}
	}
	return nil
}

// DeleteVolume снимает образ. Отсутствует → успех.
func (a *Adapter) DeleteVolume(ctx context.Context, ref blockbackend.ObjectRef) error {
	if err := validateRef("DeleteVolume", ref); err != nil {
		return err
	}
	_, err := a.run(ctx, "DeleteVolume", ref.Name, "rm", spec(ref))
	if err != nil && blockbackend.OutcomeOf(err) == blockbackend.OutcomeNotFound {
		return nil
	}
	return err
}

// ResizeVolume увеличивает образ. Уже не меньше запрошенного → успех.
func (a *Adapter) ResizeVolume(ctx context.Context, ref blockbackend.ObjectRef, sizeBytes int64) error {
	if err := validateRef("ResizeVolume", ref); err != nil {
		return err
	}
	observed, err := a.Observe(ctx, ref)
	if err != nil {
		return err
	}
	if observed.SizeBytes >= sizeBytes {
		return nil
	}
	_, err = a.run(ctx, "ResizeVolume", ref.Name,
		"resize", "--size", strconv.FormatInt(sizeBytes>>20, 10)+"M", spec(ref))
	return err
}

// CreateSnapshot снимает снимок с образа. Существует → успех.
func (a *Adapter) CreateSnapshot(ctx context.Context, volume, snapshot blockbackend.ObjectRef) error {
	if err := validateRef("CreateSnapshot", snapshot); err != nil {
		return err
	}
	_, err := a.run(ctx, "CreateSnapshot", snapshot.Name,
		"snap", "create", spec(volume)+"@"+snapshot.Name)
	if err != nil && blockbackend.OutcomeOf(err) == blockbackend.OutcomeConflict {
		return nil // идемпотентный повтор
	}
	return err
}

// DeleteSnapshot снимает снимок. Отсутствует → успех. Есть зависимые клоны →
// конфликт: тихое удаление здесь означало бы порчу их данных.
func (a *Adapter) DeleteSnapshot(ctx context.Context, ref blockbackend.ObjectRef) error {
	if err := validateRef("DeleteSnapshot", ref); err != nil {
		return err
	}
	_, err := a.run(ctx, "DeleteSnapshot", ref.Name,
		"snap", "rm", spec(ref)+"@"+ref.Name)
	if err != nil && blockbackend.OutcomeOf(err) == blockbackend.OutcomeNotFound {
		return nil
	}
	return err
}

// CloneVolume засевает цель из источника. При Detached связь с родителем
// разрывается: контракт требует независимости, и оставить её значило бы обещать
// удаление источника, которого кластер не исполнит.
func (a *Adapter) CloneVolume(ctx context.Context, s blockbackend.CloneSpec) error {
	if err := validateRef("CloneVolume", s.Target); err != nil {
		return err
	}
	_, err := a.run(ctx, "CloneVolume", s.Target.Name,
		"clone", spec(s.Source)+"@"+s.Source.Name, spec(s.Target))
	if err != nil && blockbackend.OutcomeOf(err) != blockbackend.OutcomeConflict {
		return err
	}
	if s.Detached {
		if _, ferr := a.run(ctx, "CloneVolume", s.Target.Name, "flatten", spec(s.Target)); ferr != nil {
			return ferr
		}
	}
	if s.SizeBytes > 0 {
		if rerr := a.ResizeVolume(ctx, s.Target, s.SizeBytes); rerr != nil {
			return rerr
		}
	}
	return a.applyQoS(ctx, s.Target, s.QoS)
}

// CopySnapshot переносит объект в другой локатор. Цель существует → успех.
func (a *Adapter) CopySnapshot(ctx context.Context, source, target blockbackend.ObjectRef) error {
	if err := validateRef("CopySnapshot", target); err != nil {
		return err
	}
	_, err := a.run(ctx, "CopySnapshot", target.Name, "cp", spec(source), spec(target))
	if err != nil && blockbackend.OutcomeOf(err) == blockbackend.OutcomeConflict {
		return nil
	}
	return err
}

// MigrateVolume переносит объект в другой локатор, сохраняя данные. Три шага
// инструмента идут подряд: подготовка, исполнение, фиксация. Прерывание между ними
// оставляет объект в подготовленном состоянии, и повтор его подхватывает — поэтому
// конфликт на подготовке не ошибка, а продолжение.
func (a *Adapter) MigrateVolume(ctx context.Context, ref blockbackend.ObjectRef, target blockbackend.Locator) error {
	if err := validateRef("MigrateVolume", ref); err != nil {
		return err
	}
	dst := blockbackend.ObjectRef{Locator: target, Name: ref.Name}
	if _, err := a.run(ctx, "MigrateVolume", ref.Name,
		"migration", "prepare", spec(ref), spec(dst)); err != nil {
		if blockbackend.OutcomeOf(err) != blockbackend.OutcomeConflict {
			return err
		}
	}
	if _, err := a.run(ctx, "MigrateVolume", ref.Name, "migration", "execute", spec(dst)); err != nil {
		return err
	}
	_, err := a.run(ctx, "MigrateVolume", ref.Name, "migration", "commit", spec(dst))
	return err
}

// rbdInfo — та часть машинного вывода инструмента, которая нам нужна.
type rbdInfo struct {
	Size   int64  `json:"size"`
	Parent string `json:"parent"`
}

// rbdDU — вывод учёта занятого.
type rbdDU struct {
	Images []struct {
		Name          string `json:"name"`
		UsedSize      int64  `json:"used_size"`
		ProvisionedSz int64  `json:"provisioned_size"`
	} `json:"images"`
}

// Observe рассказывает об объекте.
//
// Недоступность кластера — ОШИБКА, а не «объекта нет»: молчание не является
// утверждением об отсутствии, и спутав их, сверщик объявил бы живой том пропавшим на
// первой же сетевой неполадке.
func (a *Adapter) Observe(ctx context.Context, ref blockbackend.ObjectRef) (blockbackend.Observed, error) {
	if err := validateRef("Observe", ref); err != nil {
		return blockbackend.Observed{}, err
	}
	out, err := a.run(ctx, "Observe", ref.Name, "info", "--format", "json", spec(ref))
	if err != nil {
		if blockbackend.OutcomeOf(err) == blockbackend.OutcomeNotFound {
			return blockbackend.Observed{State: blockbackend.ObservedAbsent}, nil
		}
		return blockbackend.Observed{}, err
	}
	var info rbdInfo
	if jerr := json.Unmarshal(out, &info); jerr != nil {
		// Вывод не того формата означает, что мы говорим не с тем инструментом.
		// Это настройка, а не сбой: тихо принять её значило бы сделать неправильную
		// посадку штатным режимом.
		return blockbackend.Observed{}, blockbackend.Errorf(
			blockbackend.OutcomeMisconfigured, "Observe", ref.Name, jerr)
	}
	observed := blockbackend.Observed{
		State:     blockbackend.ObservedReady,
		SizeBytes: info.Size,
		Parent:    info.Parent,
	}
	// Занятое место спрашивается отдельно и НЕ обязано получиться: тонкое
	// выделение считается дорого, и кластер вправе отказать. Отсутствие ответа
	// оставляет поле незаполненным — ноль на его месте был бы утверждением о
	// пустом томе.
	if du, derr := a.run(ctx, "Observe", ref.Name, "du", "--format", "json", spec(ref)); derr == nil {
		var usage rbdDU
		if json.Unmarshal(du, &usage) == nil && len(usage.Images) > 0 {
			observed.UsedBytes = usage.Images[0].UsedSize
			observed.HasUsedBytes = true
		}
	}
	return observed, nil
}

// ListObjects перечисляет имена объектов локатора.
//
// Курсор инструментом не поддерживается: он отдаёт перечень целиком. Мы честно
// возвращаем его одной страницей и пустое продолжение — выдумывать разбиение,
// которого нет у источника, значило бы обещать обход, который пропускает строки.
func (a *Adapter) ListObjects(ctx context.Context, loc blockbackend.Locator, cursor string, limit int) ([]string, string, error) {
	if cursor != "" {
		return nil, "", nil // перечень уже отдан целиком первой страницей
	}
	ref := blockbackend.ObjectRef{Locator: loc}
	args := []string{"ls", "--format", "json", loc.Pool}
	if loc.Namespace != "" {
		args = []string{"ls", "--format", "json", "--namespace", loc.Namespace, loc.Pool}
	}
	out, err := a.run(ctx, "ListObjects", loc.Pool, args...)
	if err != nil {
		if blockbackend.OutcomeOf(err) == blockbackend.OutcomeNotFound {
			return nil, "", nil
		}
		return nil, "", err
	}
	var names []string
	if jerr := json.Unmarshal(out, &names); jerr != nil {
		return nil, "", blockbackend.Errorf(blockbackend.OutcomeMisconfigured, "ListObjects", ref.Pool, jerr)
	}
	return names, "", nil
}

// validateRef отвергает неполный адрес. Адаптер обязан быть НЕ снисходительнее
// дублёра: расхождение между ними и есть тот дефект, ради которого дублёр заводился.
func validateRef(op string, ref blockbackend.ObjectRef) error {
	switch {
	case ref.Pool == "":
		return blockbackend.Errorf(blockbackend.OutcomeRejected, op, ref.Name, fmt.Errorf("pool is empty"))
	case ref.Namespace == "":
		return blockbackend.Errorf(blockbackend.OutcomeRejected, op, ref.Name, fmt.Errorf("namespace is empty"))
	case ref.Name == "":
		return blockbackend.Errorf(blockbackend.OutcomeRejected, op, ref.Name, fmt.Errorf("object name is empty"))
	}
	return nil
}

// Соответствие порту заперто на этапе компиляции.
var _ blockbackend.Backend = (*Adapter)(nil)
