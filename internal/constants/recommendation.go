package constants

// enum:RecommendationKind
const (
	RecommendationKindPolicy = "policy"
)

// enum:RecommendationStatus
const (
	RecommendationStatusActive = "ACTIVE"
)

// enum:RecommendationStrategy
const (
	RecommendationStrategyHybridContentV2 = "hybrid_content_v2"
)

// enum:RecommendationScene
const (
	RecommendationSceneHome         = "home"
	RecommendationSceneSimilar      = "similar"
	RecommendationScenePostPurchase = "post_purchase"
)

// enum:RecommendationSignal
const (
	RecommendationSignalFavorite = "favorite"
	RecommendationSignalCompare  = "compare"
	RecommendationSignalHistory  = "history"
	RecommendationSignalSearch   = "search"
	RecommendationSignalPurchase = "purchase"
	RecommendationSignalOrders   = "orders"
)

// enum:RecommendationReason
const (
	RecommendationReasonCatalogDiscovery   = "catalog_discovery"
	RecommendationReasonPreferenceAffinity = "preference_affinity"
	RecommendationReasonRecentSearch       = "recent_search"
	RecommendationReasonSimilarProduct     = "similar_product"
	RecommendationReasonPurchaseRelated    = "purchase_related"
)

// enum:RecommendationSource
const (
	RecommendationSourceHistory        = "history"
	RecommendationSourceFavorite       = "favorite"
	RecommendationSourceRecommendation = "recommendation"
	RecommendationSourceFeatured       = "featured"
)
