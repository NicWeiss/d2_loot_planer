// Package catalog holds the currencies the site trades and the fixed game rules around them:
// groups, cube recipes, crafting ties.
package catalog

import (
	"fmt"
	"strings"

	"loot-planner/internal/domain/bundle"
)

// Unit is Ist: every value is expressed in Ist-equivalents.
const Unit = "r24"

// Craft prices crafting chains as fixed fractions of the top item: code -> (crafts into, how many make one).
var Craft = map[string]struct {
	Base string
	Per  float64
}{
	"cr1": {"cr3", 144}, // Blacksmith Ingot: 12 -> Horadric Sphere, 12 spheres -> Resplendent Spark
	"cr2": {"cr3", 12},  // Horadric Sphere
}

type Recipe struct {
	Out  string
	Give bundle.Bundle
}

// Cube lists every Horadric Cube recipe (standard D2R): 3 runes -> next up to Pul, 2 runes -> next from Pul on;
// from Thul on each step also eats a gem. The four Essences make a Token of Absolution.
var Cube []Recipe

// CubeByOut indexes Cube by its product.
var CubeByOut = map[string]bundle.Bundle{}

func init() {
	gems := []string{"gcy", "gcv", "gcb", "gcr", "gcg", "gcw", "gfy", "gfv", "gfb", "gfr", "gfg", "gfw",
		"gsy", "gsv", "gsb", "gsr", "gsg", "gsw", "gly", "gzv", "glb", "glr", "glg"}
	for n := 1; n <= 32; n++ {
		qty := 2.0
		if n < 21 {
			qty = 3
		}
		give := bundle.Bundle{{Code: fmt.Sprintf("r%02d", n), Qty: qty}}
		if n >= 10 {
			give = append(give, bundle.Part{Code: gems[n-10], Qty: 1})
		}
		Cube = append(Cube, Recipe{fmt.Sprintf("r%02d", n+1), give})
	}
	Cube = append(Cube, Recipe{"toa", bundle.Bundle{{Code: "tes", Qty: 1}, {Code: "ceh", Qty: 1}, {Code: "bet", Qty: 1}, {Code: "fed", Qty: 1}}})
	// server crafting: 12 Blacksmith Ingots make a Horadric Sphere, 12 spheres make a Resplendent Spark
	Cube = append(Cube, Recipe{"cr2", bundle.Bundle{{Code: "cr1", Qty: 12}}}, Recipe{"cr3", bundle.Bundle{{Code: "cr2", Qty: 12}}})
	for _, r := range Cube {
		CubeByOut[r.Out] = r.Give
	}
}

// IsCore tells money-like currencies: Lem and up plus Deep shards pay for high runes.
func IsCore(code string) bool {
	if code == "xa4" {
		return true
	}
	var n int
	if _, err := fmt.Sscanf(code, "r%d", &n); err == nil && len(code) == 3 {
		return n >= 20 && n <= 33
	}
	return false
}

// Farmable says whether a player gathers this by playing rather than trading: keys, Uber statues, crafting materials.
func (c Currency) Farmable() bool {
	g := c.Group()
	return g == "keys" || g == "statues" || g == "craft"
}

var Groups = []string{"runes", "keys", "uber", "essences", "shards", "statues", "craft", "gems", "other"}

type Currency struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	TypeCode  string `json:"typeCode"`
	ItemClass string `json:"itemClass"`
}

func (c Currency) Display() string { return strings.ReplaceAll(c.Name, " Rune", "") }

func (c Currency) Group() string {
	code := c.Code
	switch {
	case c.TypeCode == "rune":
		return "runes"
	case strings.HasPrefix(code, "pk"):
		return "keys"
	case code == "dhn" || code == "bey" || code == "mbr" || code == "toa" || code == "std" || code == "sth" || code == "jwd":
		return "uber"
	case code == "tes" || code == "ceh" || code == "bet" || code == "fed":
		return "essences"
	case strings.HasPrefix(code, "xa"):
		return "shards"
	case strings.HasPrefix(code, "ua"):
		return "statues"
	case strings.HasPrefix(code, "cr"):
		return "craft"
	case c.ItemClass == "gem":
		return "gems"
	}
	return "other"
}

// SiteCurrency is the site's /items/currency answer.
type SiteCurrency struct {
	CurrencyItems []Currency `json:"currencyItems"`
}

// Catalog is every stackable currency the site knows, in the site's order.
type Catalog struct {
	Order  []string
	ByCode map[string]Currency
}

func FromSite(raw SiteCurrency) *Catalog {
	c := &Catalog{ByCode: map[string]Currency{}}
	for _, cur := range raw.CurrencyItems {
		if _, dup := c.ByCode[cur.Code]; !dup {
			c.Order = append(c.Order, cur.Code)
		}
		c.ByCode[cur.Code] = cur
	}
	return c
}

func (c *Catalog) Has(code string) bool { _, ok := c.ByCode[code]; return ok }

// ItemKey names non-currency goods: they are traded by name and quality, copies differ in rolls.
func ItemKey(name, quality string) string { return "i:" + name + "|" + quality }

// Own is the player's own copy of an item: only that can fill someone's WTB for it.
func Own(key string) string { return "own:" + key }
