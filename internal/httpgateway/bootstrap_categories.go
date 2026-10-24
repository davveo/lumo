package httpgateway

import (
	"context"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

const storefrontCategoriesID = "storefront-categories"

// Prime the exact public get key consumed by the category renderer, while
// reusing the storefront content list already loaded for the page editor. The
// fixed lookup only runs if that bounded list omitted the navigation document.
func (g *gateway) bootstrapCategoryNavigation(
	ctx context.Context,
	results []platform.Result,
	principal platform.Command,
) []platform.Result {
	request := readRequest{
		Key:     "bootstrap-category-navigation",
		Service: constants.ServiceContent,
		Action:  constants.ActionGet,
		Command: readCommand{ID: storefrontCategoriesID, Data: map[string]any{}},
	}
	for _, result := range results {
		if result["service"] != constants.ServiceContent || result["action"] != constants.ActionList ||
			result["error"] != nil {
			continue
		}
		command, ok := result["command"].(readCommand)
		if !ok || platform.Text(command.Data, "kind") != constants.ContentKindStorefront {
			continue
		}
		data, ok := result["data"].(platform.Result)
		if !ok {
			continue
		}
		for _, row := range resultRows(data) {
			if platform.Text(row, "id") == storefrontCategoriesID &&
				platform.Text(row, "kind") == constants.ContentKindStorefront {
				return append(results, readResult(request, row, nil))
			}
		}
	}
	return append(results, g.runReads(ctx, []readRequest{request}, principal)...)
}
