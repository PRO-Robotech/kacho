// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registryv1

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// routingRecorderSrv записывает, в КАКОЙ gRPC-метод grpc-gateway фактически
// диспатчит данный REST-путь. Это сильнее, чем «route зарегистрирован» (non-404):
// проверяет precedence между соседними pattern'ами RegistryService.
//
// grpc-gateway `runtime.ServeMux.Handle` PREPEND-ит хендлеры (newest-registered
// пробуется ПЕРВЫМ), а сгенерированный Register…Handler регистрирует маршруты в
// порядке ОБЪЯВЛЕНИЯ RPC в proto. Значит объявленный ПОЗЖЕ RPC пробуется РАНЬШЕ.
// Более специфичные sub-resource-маршруты (…/tags, …/tags/{tag}, …/referrers,
// :rename) обязаны объявляться ПОЗЖЕ голых `{repository=**}` catch-all'ов
// (GetRepository/UpdateRepository/DeleteRepository), иначе catch-all перехватит
// `…/{repo}/tags` (repository="web/tags") и затенит ListTags/DeleteTag.
type routingRecorderSrv struct {
	UnimplementedRegistryServiceServer
	called chan string
}

func (s *routingRecorderSrv) GetRepository(_ context.Context, r *GetRepositoryRequest) (*Repository, error) {
	s.called <- "GetRepository:" + r.GetRepository()
	return &Repository{}, nil
}

func (s *routingRecorderSrv) ListTags(_ context.Context, r *ListTagsRequest) (*ListTagsResponse, error) {
	s.called <- "ListTags:" + r.GetRepository()
	return &ListTagsResponse{}, nil
}

func (s *routingRecorderSrv) DeleteTag(_ context.Context, r *DeleteTagRequest) (*operationpb.Operation, error) {
	s.called <- "DeleteTag:" + r.GetRepository() + ":" + r.GetTag()
	return &operationpb.Operation{}, nil
}

func (s *routingRecorderSrv) ListReferrers(_ context.Context, r *ListReferrersRequest) (*ListReferrersResponse, error) {
	s.called <- "ListReferrers:" + r.GetRepository()
	return &ListReferrersResponse{}, nil
}

func (s *routingRecorderSrv) RenameRepository(_ context.Context, r *RenameRepositoryRequest) (*operationpb.Operation, error) {
	s.called <- "RenameRepository:" + r.GetRepository()
	return &operationpb.Operation{}, nil
}

// Голые {repository=**} catch-all мутации — записываются, чтобы перехват
// sub-resource-пути показывал ИМЯ метода-виновника (а не глухой 501 Unimplemented).
func (s *routingRecorderSrv) CreateRepository(_ context.Context, r *CreateRepositoryRequest) (*operationpb.Operation, error) {
	s.called <- "CreateRepository:" + r.GetRepository()
	return &operationpb.Operation{}, nil
}

func (s *routingRecorderSrv) UpdateRepository(_ context.Context, r *UpdateRepositoryRequest) (*operationpb.Operation, error) {
	s.called <- "UpdateRepository:" + r.GetRepository()
	return &operationpb.Operation{}, nil
}

func (s *routingRecorderSrv) DeleteRepository(_ context.Context, r *DeleteRepositoryRequest) (*operationpb.Operation, error) {
	s.called <- "DeleteRepository:" + r.GetRepository()
	return &operationpb.Operation{}, nil
}

// TestRegistry_RESTRouting_SubResourceNotShadowedByCatchAll доказывает, что
// более специфичные repo-sub-resource маршруты НЕ затеняются голым
// `{repository=**}` catch-all'ом GetRepository/DeleteRepository. Поднимает
// in-process RegistryService (bufconn) + grpc-gateway REST mux и проверяет
// фактически вызванный gRPC-метод для каждого пути.
//
// До реордера RPC (RG-1) `GET …/repositories/web/tags` уходил в GetRepository
// (repository="web/tags"), а `DELETE …/repositories/web/tags/v1` — в
// DeleteRepository: catch-all объявлен ПОСЛЕ tag-маршрутов → prepend → перехват.
func TestRegistry_RESTRouting_SubResourceNotShadowedByCatchAll(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	rec := &routingRecorderSrv{called: make(chan string, 1)}
	RegisterRegistryServiceServer(gs, rec)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	mux := runtime.NewServeMux()
	if err := RegisterRegistryServiceHandler(context.Background(), mux, conn); err != nil {
		t.Fatalf("RegisterRegistryServiceHandler: %v", err)
	}

	cases := []struct {
		name, method, path, want string
	}{
		// sub-resource /tags — single-segment {repository}; НЕ должен уходить в
		// GetRepository{repository=**}.
		{"ListTags_not_shadowed", "GET", "/registry/v1/registries/reg-1/repositories/web/tags", "ListTags:web"},
		// sub-resource /tags/{tag} — НЕ должен уходить в DeleteRepository{repository=**}.
		{"DeleteTag_not_shadowed", "DELETE", "/registry/v1/registries/reg-1/repositories/web/tags/v1", "DeleteTag:web:v1"},
		// sub-resource /referrers — многосегментный {repository=**}/referrers.
		{"ListReferrers", "GET", "/registry/v1/registries/reg-1/repositories/backend/api/referrers", "ListReferrers:backend/api"},
		// :rename verb-action на голом {repository=**}.
		{"RenameRepository", "POST", "/registry/v1/registries/reg-1/repositories/web:rename", "RenameRepository:web"},
		// truly-bare repo path — только он и должен доходить до GetRepository.
		{"GetRepository_bare", "GET", "/registry/v1/registries/reg-1/repositories/backend/api", "GetRepository:backend/api"},
	}
	for _, tc := range cases {

		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			var got string
			select {
			case got = <-rec.called:
			default:
				t.Fatalf("%s %s: no RegistryService method invoked (http %d) — route not dispatched",
					tc.method, tc.path, w.Code)
			}
			if got != tc.want {
				t.Errorf("%s %s dispatched to %q, want %q — sub-resource route shadowed by {repository=**} catch-all",
					tc.method, tc.path, got, tc.want)
			}
		})
	}
}
