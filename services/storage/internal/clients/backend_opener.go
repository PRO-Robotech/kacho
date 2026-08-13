// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
)

// BackendFactory собирает адаптер к одному зарегистрированному бэкенду.
//
// Учётный материал приходит РАЗРЕШЕНИЕМ ссылки, а не значением: ресурс несёт ссылку,
// процесс — способ её разрешить. Секрет не проходит через API и не лежит в БД.
type BackendFactory func(ctx context.Context, endpoint string, credentials []byte) (blockbackend.Backend, error)

// CredentialsResolver разрешает ссылку на учётный материал в сам материал.
type CredentialsResolver interface {
	Resolve(ctx context.Context, ref string) ([]byte, error)
}

// BackendOpener открывает адаптеры по виду бэкенда.
//
// # Почему неизвестный вид — ОТКАЗ, а не тишина
//
// Вид, для которого адаптера нет, означает ровно одно: этот процесс не умеет
// обслуживать зарегистрированные на него ресурсы. Молча пропустить такую строку
// значило бы завести сверщик, который каждый проход берёт работу и ничего не делает,
// — контроль, присутствующий и не отказавший ни разу за всю свою жизнь.
type BackendOpener struct {
	factories   map[string]BackendFactory
	credentials CredentialsResolver

	mu    sync.Mutex
	cache map[string]blockbackend.Backend
}

// NewBackendOpener собирает открыватель.
func NewBackendOpener(factories map[string]BackendFactory, creds CredentialsResolver) *BackendOpener {
	return &BackendOpener{
		factories:   factories,
		credentials: creds,
		cache:       map[string]blockbackend.Backend{},
	}
}

// Kinds перечисляет виды, которые этот процесс умеет обслуживать. Нужен стражу
// старта: посадка, объявившая вид без адаптера, не поднимается.
func (o *BackendOpener) Kinds() []string {
	out := make([]string, 0, len(o.factories))
	for k := range o.factories {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Supports сообщает, умеет ли процесс обслуживать названный вид.
func (o *BackendOpener) Supports(kind string) bool {
	_, ok := o.factories[kind]
	return ok
}

// Open открывает адаптер к бэкенду ревизии привязки.
func (o *BackendOpener) Open(ctx context.Context, b reconciler.Binding) (blockbackend.Backend, error) {
	o.mu.Lock()
	if be, ok := o.cache[b.BackendID]; ok {
		o.mu.Unlock()
		return be, nil
	}
	o.mu.Unlock()

	factory, ok := o.factories[b.Kind]
	if !ok {
		return nil, fmt.Errorf("storage backend kind %q has no adapter in this build: "+
			"resources registered on it cannot be provisioned, and treating that as a "+
			"transient failure would hide it forever (adapters present: %v)", b.Kind, o.Kinds())
	}

	var material []byte
	if o.credentials != nil {
		m, err := o.credentials.Resolve(ctx, b.CredentialsRef)
		if err != nil {
			return nil, fmt.Errorf("storage backend %s: credentials reference %q is not resolvable: %w",
				b.BackendID, b.CredentialsRef, err)
		}
		material = m
	}

	be, err := factory(ctx, b.Endpoint, material)
	if err != nil {
		return nil, err
	}

	o.mu.Lock()
	o.cache[b.BackendID] = be
	o.mu.Unlock()
	return be, nil
}
