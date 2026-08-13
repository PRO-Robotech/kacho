// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"context"

	"google.golang.org/grpc"

	operationv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// AddressServiceClient — alias на сгенерированный gRPC-клиент.
type AddressServiceClient = vpcv1.AddressServiceClient

// GetAddress — sync read.
func (c *Client) GetAddress(ctx context.Context, id string, opts ...grpc.CallOption) (*vpcv1.Address, error) {
	return c.Addresses.Get(ctx, &vpcv1.GetAddressRequest{AddressId: id}, opts...)
}

// ListAddresses — sync list по project/page.
func (c *Client) ListAddresses(ctx context.Context, req *vpcv1.ListAddressesRequest, opts ...grpc.CallOption) (*vpcv1.ListAddressesResponse, error) {
	return c.Addresses.List(ctx, req, opts...)
}

// Обёртки поиска по значению и списка по подсети СНЯТЫ вместе с методами.
//
// Оба вопроса закрывает список: сужение по значению — поле `ip_address`, сужение по
// подсети — поле `subnet_id`. Область запроса при этом берётся из проекта, а не из
// подсети: у снятого поиска по значению внешняя ветвь была неавторизуема по
// построению, потому что у внешнего адреса подсети нет.

// CreateAddress — async; IPAM allocate происходит inline в worker.
func (c *Client) CreateAddress(ctx context.Context, req *vpcv1.CreateAddressRequest, opts ...grpc.CallOption) (*operationv1.Operation, error) {
	return c.Addresses.Create(ctx, req, opts...)
}

// UpdateAddress — async. external_ipv4_spec / internal_ipv4_spec / project_id immutable.
func (c *Client) UpdateAddress(ctx context.Context, req *vpcv1.UpdateAddressRequest, opts ...grpc.CallOption) (*operationv1.Operation, error) {
	return c.Addresses.Update(ctx, req, opts...)
}

// DeleteAddress — async hard-delete. Address в использовании у NIC нельзя удалить
// (FailedPrecondition) — сначала detach NIC.
func (c *Client) DeleteAddress(ctx context.Context, id string, opts ...grpc.CallOption) (*operationv1.Operation, error) {
	return c.Addresses.Delete(ctx, &vpcv1.DeleteAddressRequest{AddressId: id}, opts...)
}
