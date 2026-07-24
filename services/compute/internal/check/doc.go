// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package check содержит kacho-compute per-service Check-interceptor wiring.
//
// Состав:
//
//   - permission_map.go   — RPCMap для ВСЕХ выставленных RPC kacho-compute
//     (Disk, Image, Snapshot, Instance, DiskType, MachineType + Operation +
//     Internal* на :9091). Region/Zone serving снят — Geography принадлежит
//     kacho-geo (миграция 0011).
//
//     access-bindings RPC (`{Disk,Image,Instance,Snapshot}Service/
//     {List,Set,Update}AccessBindings`) ЗДЕСЬ ЕСТЬ, хотя handler'ы — AAA-скелет
//     (не переопределены → codes.Unimplemented). Раньше они были опущены «их
//     авторизует сама kacho-iam» — но регистрация ServiceServer поднимает ВЕСЬ
//     ServiceDesc, включая унаследованные из Unimplemented* методы, поэтому они
//     проходят через тот же authz-интерсептор: без записи в карте он fail-close'ил
//     их как DecisionUnmapped → `permission denied (rpc not mapped)`, т.е. дыра в
//     проводке маскировалась под authz-отказ. Записи зеркалят gateway
//     permission_catalog.json 1:1 (relation + unscoped project-scope), так что оба
//     тира выносят одно и то же решение. Гейт против повторения —
//     permission_map_coverage_test.go (обход proto-generated ServiceDesc).
//
//   - check_client.go     — gRPC adapter поверх `iamv1.InternalIAMServiceClient.Check`.
//
//   - factory.go          — фабрика, собирающая `*authz.Interceptor` из
//     (IAMConn, Breakglass). nil-conn + Breakglass=false → ErrIAMConnNotConfigured
//     (graceful start без kacho-iam в dev).
//
// Wiring (composition root — `cmd/compute/main.go`):
//
//	authzIntr, err := check.NewInterceptor(check.Options{
//	    ServiceName: "kacho-compute",
//	    IAMConn:     iamConn,        // *grpc.ClientConn к kacho-iam:9091
//	    Breakglass:  cfg.AuthZBreakglass,
//	    Logger:      logger,
//	})
//	if err != nil { return err }
//	if authzIntr != nil {
//	    publicUnary = append(publicUnary, authzIntr.Unary())
//	    publicStream = append(publicStream, authzIntr.Stream())
//	}
//
// Cache-invalidation (LISTEN/NOTIFY → `kacho_iam_subjects`) — НЕ wired в
// этом MVP. TTL=5s + outbox-drain≤2s = ≤10s revoke propagation.
package check
