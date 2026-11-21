// Package market turns site listings into trades, fits fair values and answers market questions for one realm.
package market

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	"loot-planner/internal/domain/bundle"
	"loot-planner/internal/domain/catalog"
)

// excluded: gold quantities are too noisy to price against runes.
var excluded = map[string]bool{"gld": true}

type Realm struct{ Mode, Season string }

func ParseRealm(key string) Realm {
	f := strings.Fields(strings.ReplaceAll(key, ":", " "))
	if len(f) != 2 {
		return Realm{}
	}
	return Realm{f[0], f[1]}
}

func (r Realm) Key() string { return r.Mode + " " + r.Season }

func (r Realm) Holds(a *Ad) bool {
	return a.GameMode == r.Mode && a.Ladder == r.Season && a.AdType != "auction"
}

// Ad is a site advertisement, reduced to the fields this app reads. Unknown fields are dropped on decode,
// which keeps a snapshot of tens of thousands of listings small in memory.
type Ad struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Status          string          `json:"status"`
	GameMode        string          `json:"gameMode"`
	Ladder          string          `json:"ladder"`
	AdType          string          `json:"adType"`
	PayloadKind     string          `json:"payloadKind"`
	UnitSale        bool            `json:"unitSale,omitempty"`
	EscrowMaterials []Material      `json:"escrowMaterials,omitempty"`
	KitItems        []KitItem       `json:"kitItems,omitempty"`
	ItemNameKey     string          `json:"itemNameKey,omitempty"`
	ItemName        string          `json:"itemName,omitempty"`
	Quality         string          `json:"quality,omitempty"`
	Params          *Params         `json:"params,omitempty"`
	Payments        []Payment       `json:"payments"`
	WtbRequirements json.RawMessage `json:"wtbRequirements,omitempty"`
	UpdatedAt       string          `json:"updatedAt,omitempty"`
	ClosedAt        string          `json:"closedAt,omitempty"`
	BumpedAt        string          `json:"bumpedAt,omitempty"`
	CreatedAt       string          `json:"createdAt,omitempty"`
}

type Material struct {
	BaseCode string  `json:"baseCode"`
	Quantity float64 `json:"quantity"`
}

type KitItem struct {
	BaseCode string  `json:"baseCode"`
	Params   *Params `json:"params,omitempty"`
}

type Params struct {
	Quantity float64 `json:"quantity,omitempty"`
}

type Payment struct {
	Kind             string  `json:"kind"`
	OptionGroup      int     `json:"optionGroup"`
	Amount           float64 `json:"amount,omitempty"`
	Code             string  `json:"code,omitempty"`
	CurrencyBaseCode string  `json:"currencyBaseCode,omitempty"`
}

func (p Payment) code() string {
	if p.CurrencyBaseCode != "" {
		return p.CurrencyBaseCode
	}
	return p.Code
}

// Date is when the listing last told its price: closed, bumped or posted.
func (a *Ad) Date() string {
	if a.ClosedAt != "" {
		return a.ClosedAt
	}
	if a.BumpedAt != "" {
		return a.BumpedAt
	}
	return a.CreatedAt
}

func (a *Ad) quantity() float64 {
	if a.Params != nil && a.Params.Quantity != 0 {
		return a.Params.Quantity
	}
	return 1
}

// HasRequirements: the buyer of an item WTB asks for particular stats.
func (a *Ad) HasRequirements() bool {
	s := string(bytes.TrimSpace(a.WtbRequirements))
	return s != "" && s != "null" && s != "[]" && s != "{}" && s != "false" && s != `""` && s != "0"
}

// Trade is a currency-for-currency listing with one payment option, from the poster's side.
type Trade struct {
	ID, Type, Status string
	Goods            bundle.Bundle // what the listing is about (WTS: sold, WTB: wanted)
	Price            bundle.Bundle // one payment option
	Unit             bool          // priced per piece, Stock pieces left
	Stock            float64
	Date             string
	Weight           float64 // relevance of the listing's date, see Relevance
}

// Action is a live listing seen from the player's side: give -> get, at most Uses times.
type Action struct {
	ID    string
	Type  string // wts | wtb
	Give  bundle.Bundle
	Get   bundle.Bundle
	Uses  float64
	Check bool // the buyer has stat requirements
}

// Goods are what the listing is about. Unit sales are priced per piece and their quantity is the remaining
// stock (0 once sold out), so they are normalised to one piece.
func Goods(a *Ad) bundle.Bundle {
	if a.UnitSale && len(a.EscrowMaterials) > 0 {
		return bundle.Bundle{{Code: a.EscrowMaterials[0].BaseCode, Qty: 1}}
	}
	if len(a.EscrowMaterials) > 0 {
		out := make(bundle.Bundle, 0, len(a.EscrowMaterials))
		for _, m := range a.EscrowMaterials {
			out = append(out, bundle.Part{Code: m.BaseCode, Qty: m.Quantity})
		}
		return out
	}
	if (a.PayloadKind == "kit" || a.PayloadKind == "kit_wtb") && len(a.KitItems) > 0 {
		var out bundle.Bundle
		for _, k := range a.KitItems {
			q := 1.0
			if k.Params != nil && k.Params.Quantity != 0 {
				q = k.Params.Quantity
			}
			out.Add(k.BaseCode, q)
		}
		return out
	}
	return bundle.Bundle{{Code: a.ItemNameKey, Qty: a.quantity()}}
}

// PriceOptions are the payment alternatives, one bundle per option group in group order.
// Groups containing vague "any runes" entries are dropped.
func PriceOptions(a *Ad) []bundle.Bundle {
	groups := map[int]*bundle.Bundle{}
	vague := map[int]bool{}
	for _, p := range a.Payments {
		if p.Kind != "item" || p.Amount == 0 {
			vague[p.OptionGroup] = true
			continue
		}
		g := groups[p.OptionGroup]
		if g == nil {
			g = &bundle.Bundle{}
			groups[p.OptionGroup] = g
		}
		g.Add(p.code(), p.Amount)
	}
	keys := make([]int, 0, len(groups))
	for k := range groups {
		if !vague[k] {
			keys = append(keys, k)
		}
	}
	sort.Ints(keys)
	out := make([]bundle.Bundle, len(keys))
	for i, k := range keys {
		out[i] = *groups[k]
	}
	return out
}

// CurrencyTrades are the realm's currency-for-currency listings, one Trade per payment option.
// Listings from before the season are left out.
func CurrencyTrades(ads []Ad, cat *catalog.Catalog, realm Realm, rel Relevance) []Trade {
	ok := func(b bundle.Bundle) bool {
		if len(b) == 0 {
			return false
		}
		for _, p := range b {
			if !cat.Has(p.Code) || excluded[p.Code] || p.Qty <= 0 {
				return false
			}
		}
		return true
	}
	var rows []Trade
	for i := range ads {
		a := &ads[i]
		if !realm.Holds(a) {
			continue
		}
		g := Goods(a)
		if !ok(g) {
			continue
		}
		date := a.Date()
		w := rel.Weight(date)
		if w == 0 {
			continue
		}
		stock := 1.0
		if a.UnitSale && len(a.EscrowMaterials) > 0 {
			stock = a.EscrowMaterials[0].Quantity
		}
		for _, opt := range PriceOptions(a) {
			if ok(opt) {
				rows = append(rows, Trade{a.ID, a.Type, a.Status, g, opt, a.UnitSale, stock, date, w})
			}
		}
	}
	return rows
}

// PlayerSide is (what the player gives, what the player gets) when answering this listing.
func PlayerSide(t *Trade) (bundle.Bundle, bundle.Bundle) {
	if t.Type == "wts" {
		return t.Price, t.Goods
	}
	return t.Goods, t.Price
}
