package store

import (
	_ "embed"
	"encoding/json"
)

// IDs come from config type 2, args 139 / jifndkppcjg field 1 (eapbnnpdooo),
// restricted to fishing tackle. Item-definition flags use a different enum.
//
//go:embed assets/discontinued_shop_items.json
var discontinuedShopJSON []byte

var discontinuedShopIDs = func() map[string]bool {
	var ids []string
	if err := json.Unmarshal(discontinuedShopJSON, &ids); err != nil {
		panic(err)
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}()

func IsDiscontinuedShopItem(itemID string) bool {
	return discontinuedShopIDs[itemID]
}
