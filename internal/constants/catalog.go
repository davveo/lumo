package constants

// enum:CatalogKind
const (
	CatalogKindSku = "sku"
)

// enum:CatalogStatus
const (
	CatalogStatusActive   = "ACTIVE"
	CatalogStatusDisabled = "DISABLED"
)

// enum:CatalogParent
const (
	CatalogParentLegacy = "__legacy__"
)

// enum:CatalogSort
const (
	CatalogSortDefault   = ""
	CatalogSortPriceAsc  = "price_asc"
	CatalogSortPriceDesc = "price_desc"
	CatalogSortSales     = "sales"
	CatalogSortSalesDesc = "sales_desc"
	CatalogSortNewest    = "newest"
)
