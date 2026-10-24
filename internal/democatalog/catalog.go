// Package democatalog contains the development catalog shared by independent
// catalog and inventory seeders. It contains no database access or peer writes.
package democatalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed products.json
var seedJSON []byte

// Products returns a fresh copy of the twelve storefront product definitions.
// The JSON uses yuan for presentation; services convert prices to integer cents.
func Products() []map[string]any {
	var out []map[string]any
	if err := json.Unmarshal(seedJSON, &out); err != nil {
		panic("invalid embedded development catalog: " + err.Error())
	}
	return out
}

// SKUs returns every independently stocked color/option combination. The first
// color and default option use the product ID, all others have stable suffixes.
func SKUs() []string { return productSKUs(Products()) }

func productSKUs(products []map[string]any) []string {
	var out []string
	for _, p := range products {
		id := p["id"].(string)
		colors := p["colors"].([]any)
		options := p["options"].([]any)
		for ci := range colors {
			for oi, option := range options {
				sku := id
				if ci != 0 || option != p["defaultOption"] {
					sku = fmt.Sprintf("%s--%d-%d", id, ci, oi)
				}
				out = append(out, sku)
			}
		}
	}
	return out
}
