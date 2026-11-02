package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

func (s *Service) Query(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "search.usecase.query")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	return s.search(ctx, c)
}
func (s *Service) Suggest(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "search.usecase.suggest")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	return s.search(ctx, c)
}
func (s *Service) seed(ctx context.Context) error {
	r := record(
		"search-policy",
		"",
		constants.SearchKindPolicy,
		constants.SearchStatusActive,
		map[string]any{
			"engine": constants.SearchEngineCatalogRPC,
			"note":   "Elasticsearch adapter is not enabled; authoritative RPC reads",
		},
	)
	_, e := s.repo.InsertOnce(ctx, &r, nil)
	return e
}
func (s *Service) event(context.Context, platform.Event) error { return nil }
