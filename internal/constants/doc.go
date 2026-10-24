// Package constants owns the string enums shared by service boundaries and
// their storefront projections. Constants are untyped to preserve the existing
// string values and dynamic map/JSON/RPC behavior.
//
// Keep each domain in its own file. Mark each exported enum declaration with
// //enum:GroupName and prefix every identifier with that group name. The browser
// constants are generated from these declarations; do not edit their values in
// web/dist/constants.js.
package constants

//go:generate go run ./generate -output ../../web/dist/constants.js
