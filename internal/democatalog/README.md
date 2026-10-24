# Development product catalog

`Products()` and `SKUs()` retain the original twelve storefront products and their selectable variants. Existing orders, promotions and integration fixtures keep those IDs and definitions.

`ExpandedProducts()` supplies an additional 1,838 deterministic demonstration products across all 20 configured categories. Each of the 586 fine categories receives three choices; 80 additional themed bundles cover row-level queries that do not occur naturally in leaf product names. Every product has two separately priced, independently stocked variants, for 3,676 additional SKUs. Catalog and inventory initialize their own records; the initial presentation stock is 50, while the inventory service remains authoritative after initialization.

The compact `assortment.json` plan is generated from the content service's backend navigation defaults. It is product seed data, not a navigation source or frontend fallback. After changing those defaults, rebuild the derived seed plan with:

```sh
go generate ./internal/democatalog
```

The generated fixture stores its source SHA-256. Tests reject stale plans and check all leaf, row, shortcut and configured brand searches, valid variant IDs/prices, existing image assets, and IDs stable across navigation ordering. Product IDs use a scoped hash of group, section, leaf label, query and edition, so reordering navigation never creates duplicate products. Calls return independent maps and slices.

`ProductsFromNavigation([]byte)` can derive the same assortment from a current, already-validated backend configuration for an explicit demonstration-data import. It does not change the persisted navigation. Additional queries receive themed bundle products rather than unrelated keyword lists in ordinary product names.

Products carry `demo=true`, `demo_source=category-assortment-v1` and visible demonstration disclosures in their descriptions/specifications. Prices, sales counts, named editions and stock values are demonstration data. Real local images are selected only for compatible product families; other items use neutral, category-labelled `assets/demo-<group-id>.svg` illustrations.
