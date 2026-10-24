package usecase

import (
	"github.com/davveo/lumo/internal/constants"

	"bytes"
	"context"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecommendationCandidates is an internal, bounded recall operation. It does
// not change the public catalog listing or trust caller-provided ranking data.
func (s *Service) RecommendationCandidates(
	ctx context.Context,
	c platform.Request,
) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "catalog.usecase.recommendation_candidates")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal recommendation recall required")
	}
	var input struct {
		Categories []string `json:"categories"`
		Brands     []string `json:"brands"`
		Queries    []string `json:"queries"`
		Limit      int      `json:"limit"`
	}
	b, err := json.Marshal(c.Data)
	if err != nil {
		return nil, bad("invalid recommendation recall")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		return nil, bad("invalid recommendation recall")
	}
	if input.Limit == 0 {
		input.Limit = 100
	}
	if input.Limit < 1 || input.Limit > 200 {
		return nil, bad("recommendation limit must be between 1 and 200")
	}
	if input.Categories, err = recallTerms(input.Categories, 4, 64); err != nil {
		return nil, err
	}
	if input.Brands, err = recallTerms(input.Brands, 4, 64); err != nil {
		return nil, err
	}
	if input.Queries, err = recallTerms(input.Queries, 3, 80); err != nil {
		return nil, err
	}
	rows, degraded, err := s.repo.RecommendationCandidates(ctx, repository.RecommendationRecall{
		Categories: input.Categories, Brands: input.Brands, Queries: input.Queries, Limit: input.Limit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]platform.Result, 0, len(rows))
	for _, row := range rows {
		items = append(items, view(row))
	}
	return platform.Result{"items": items, "degraded": degraded}, nil
}

func recallTerms(values []string, maxItems, maxLength int) ([]string, error) {
	if len(values) > maxItems {
		return nil, bad("too many recommendation interests")
	}
	result, seen := []string{}, map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxLength ||
			strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, bad("invalid recommendation interest")
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result, nil
}
