// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package cephrbd — адаптер плоскости данных поверх Ceph RBD.
//
// # Почему через инструмент командной строки, а не через библиотеку
//
// Родная библиотека требует cgo и системных заголовков кластера — то есть привязывает
// СБОРКУ облака к версии чужого хранилища. Инструмент `rbd` идёт в том же образе, что
// и его кластер, говорит стабильным набором глаголов и отдаёт машинно читаемый вывод.
// Обновился кластер — обновился инструмент, а наш бинарь не пересобирается.
//
// # Почему это проверяемо целиком
//
// Исполнитель команд — интерфейс. Настоящий запускает процесс, подставной отвечает
// заготовленным выводом и кодом выхода, поэтому КАЖДАЯ ветка адаптера (включая
// классификацию отказов) проверяется без живого кластера. Та же контрактная суита,
// которой проверяется дублёр, гоняется и по этому адаптеру: расхождение между ними
// было бы ровно тем дефектом, ради которого дублёр и заводился.
//
// # Что этот адаптер требует от РАЗВЁРТЫВАНИЯ, и почему это названо
//
// Наличие инструмента `rbd` в образе и файла конфигурации кластера с ключом. Оба —
// факты посадки, а не кода, поэтому они проверяются при старте: отсутствующий
// инструмент обязан ронять старт, а не превращать каждое обращение в «бэкенд
// недоступен» — это настройка, а не сбой, и мягкий проход по ней сделал бы
// неправильную посадку штатным режимом.
package cephrbd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Runner исполняет одну команду инструмента и возвращает её вывод с кодом выхода.
//
// Код выхода возвращается ОТДЕЛЬНО от ошибки: ненулевой код — это ответ инструмента,
// который нам предстоит классифицировать, а ошибка означает, что команду не удалось
// исполнить вовсе. Слив их в одно значение, адаптер не отличил бы «кластер сказал
// нет» от «инструмента нет в образе».
type Runner interface {
	Run(ctx context.Context, args ...string) (stdout []byte, stderr []byte, exitCode int, err error)
}

// ExecRunner запускает настоящий инструмент.
type ExecRunner struct {
	// Binary — путь к инструменту. Пустое значение недопустимо: умолчание,
	// собранное из имени, всегда непусто и потому выглядит настроенным.
	Binary string
	// ConfPath — файл конфигурации кластера.
	ConfPath string
	// KeyringPath — файл с ключом. Разрешается из ссылки ресурса вызывающим:
	// материал не проходит ни через API, ни через БД.
	KeyringPath string
	// ClientName — имя учётной записи в кластере.
	ClientName string
	// Timeout — срок ОДНОЙ команды. Неположительное значение недопустимо:
	// неотвечающий инструмент без срока паркует слот сверщика навсегда.
	Timeout time.Duration
}

// Validate проверяет полноту исполнителя. Вызывается при сборке адаптера, а не при
// первом обращении: неполная посадка обязана быть видна на старте.
func (r ExecRunner) Validate() error {
	switch {
	case r.Binary == "":
		return errors.New("cephrbd: binary path is required")
	case r.ConfPath == "":
		return errors.New("cephrbd: cluster configuration path is required")
	case r.KeyringPath == "":
		return errors.New("cephrbd: keyring path is required")
	case r.ClientName == "":
		return errors.New("cephrbd: client name is required")
	case r.Timeout <= 0:
		return errors.New("cephrbd: per-command timeout must be positive")
	}
	return nil
}

// Run исполняет команду с собственным сроком.
func (r ExecRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, int, error) {
	callCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	full := append([]string{
		"--conf", r.ConfPath,
		"--keyring", r.KeyringPath,
		"--name", r.ClientName,
	}, args...)

	// Бинарь и аргументы собираются ЗДЕСЬ и не приходят из запроса: путь к
	// инструменту задаёт посадка, остальное — имена объектов, выведенные из
	// префикса установки и id ресурса. Пользовательской строки в этом наборе нет.
	cmd := exec.CommandContext(callCtx, r.Binary, full...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()
	if err == nil {
		return out.Bytes(), errOut.Bytes(), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// Ненулевой код — ОТВЕТ инструмента. Его классифицирует адаптер.
		return out.Bytes(), errOut.Bytes(), exitErr.ExitCode(), nil
	}
	// Команду не удалось исполнить вовсе: инструмента нет, прав нет, контекст истёк.
	// Это не ответ кластера, и трактовать его как ответ нельзя.
	return out.Bytes(), errOut.Bytes(), -1, fmt.Errorf("cephrbd: running %s: %w", r.Binary, err)
}
