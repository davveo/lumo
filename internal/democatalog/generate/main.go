// Command generate derives a product seed plan from the backend navigation.
package main

import (
	"flag"
	"fmt"
	"github.com/davveo/lumo/internal/democatalog"
	"os"
)

func main() {
	source := flag.String(
		"source",
		"../../services/content/internal/usecase/category_navigation.json",
		"backend category navigation JSON",
	)
	output := flag.String("out", "assortment.json", "output fixture")
	flag.Parse()
	raw, err := os.ReadFile(*source)
	if err != nil {
		fail(err)
	}
	encoded, err := democatalog.BuildAssortmentFixture(raw)
	if err != nil {
		fail(err)
	}
	if err = os.WriteFile(*output, append(encoded, '\n'), 0644); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
