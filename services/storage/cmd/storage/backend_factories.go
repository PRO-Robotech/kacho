// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/clients"
	"github.com/PRO-Robotech/kacho/services/storage/internal/clients/cephrbd"
)

// blockBackendFactories — виды плоскости данных, которые умеет обслуживать этот
// бинарь.
//
// Перечень — ЕДИНСТВЕННЫЙ источник ответа на вопрос «умеем ли мы этот вид». Страж
// старта спрашивает его же, поэтому вид, объявленный в конфигурации и отсутствующий
// здесь, роняет старт. Альтернатива — принять вид и молча ничего не делать — дала бы
// сверщик, который каждый проход берёт работу и не двигает её, при здоровом рапорте
// сервиса и томах, стоящих создаваемыми навсегда.
func blockBackendFactories(callTimeout time.Duration) map[string]clients.BackendFactory {
	return map[string]clients.BackendFactory{
		cephrbd.Kind: newCephRBD(callTimeout),
	}
}

// newCephRBD собирает адаптер к кластеру блочного хранения.
//
// # Что приходит откуда
//
// Координата кластера — из ресурса StorageBackend (её задаёт администратор), учётный
// материал — РАЗРЕШЕНИЕМ ссылки в файл смонтированного каталога. Секрет не проходит
// ни через API, ни через БД: строка таблицы переживает ротацию и уезжает в резервные
// копии.
//
// # Почему проверка полноты стоит ЗДЕСЬ, а не на первом обращении
//
// Неполный исполнитель (нет инструмента, нет конфигурации, нет срока) — это
// НАСТРОЙКА. Обнаружив её на первом обращении, мы получили бы поток отказов, неотличимых
// от недоступности кластера, и постоянная неправильная посадка стала бы штатным
// режимом. Здесь она видна в момент сборки адаптера и называется вслух.
func newCephRBD(callTimeout time.Duration) clients.BackendFactory {
	return func(_ context.Context, endpoint string, credentials []byte) (blockbackend.Backend, error) {
		if endpoint == "" {
			return nil, fmt.Errorf("cephrbd: storage backend endpoint is empty: " +
				"the cluster configuration path is registered on the StorageBackend resource")
		}
		if len(credentials) == 0 {
			return nil, fmt.Errorf("cephrbd: credentials reference resolved to nothing: " +
				"the keyring is mounted, not stored, and an empty one turns every call into " +
				"what looks like cluster unavailability")
		}
		// Материал разрешён в файл: инструмент читает ключ из файла, а не из
		// аргументов — аргументы видны в списке процессов всякому на узле.
		keyring, err := writeKeyring(credentials)
		if err != nil {
			return nil, err
		}
		runner := cephrbd.ExecRunner{
			Binary:      cephBinary(),
			ConfPath:    endpoint,
			KeyringPath: keyring,
			ClientName:  cephClientName(),
			Timeout:     callTimeout,
		}
		if verr := runner.Validate(); verr != nil {
			return nil, verr
		}
		return cephrbd.New(runner), nil
	}
}

// writeKeyring кладёт разрешённый материал во временный файл с правами только для
// владельца процесса.
func writeKeyring(material []byte) (string, error) {
	dir, err := os.MkdirTemp("", "kacho-storage-keyring-")
	if err != nil {
		return "", fmt.Errorf("cephrbd: keyring staging directory: %w", err)
	}
	path := filepath.Join(dir, "keyring")
	if werr := os.WriteFile(path, material, 0o600); werr != nil {
		return "", fmt.Errorf("cephrbd: writing keyring: %w", werr)
	}
	return path, nil
}

// cephBinary — путь к инструменту. Значение по умолчанию названо здесь, а не
// собрано из имени: собранное всегда непусто и потому выглядит настроенным.
func cephBinary() string {
	if v := os.Getenv("KACHO_STORAGE_CEPH_RBD_BINARY"); v != "" {
		return v
	}
	return "/usr/bin/rbd"
}

// cephClientName — учётная запись в кластере.
func cephClientName() string {
	if v := os.Getenv("KACHO_STORAGE_CEPH_CLIENT_NAME"); v != "" {
		return v
	}
	return "client.kacho-storage"
}
