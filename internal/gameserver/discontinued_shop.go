package gameserver

import (
	"context"
	"errors"
	"rf4server/internal/store"
	"strings"
	"time"
)

var errDiscontinuedShopClosed = errors.New("钓鱼站停产商品当前不在限时开放时段")

// discontinuedShopChangedSince reports whether any opening/closing boundary was
// crossed since the client's last shelf load, even across an entire game day.
func discontinuedShopChangedSince(since, now time.Time) bool {
	if since.After(now) {
		return true
	}
	day := (now.Unix() - worldDayEpochUnix) / 3600
	start := time.Unix(worldDayEpochUnix+day*3600, 0)
	for _, minute := range []int{9 * 60, 12 * 60, 20 * 60, 22 * 60} {
		boundary := start.Add(gameMinutesToRealDuration(int64(minute)))
		if boundary.After(now) {
			boundary = boundary.Add(-time.Hour)
		}
		if boundary.After(since) {
			return true
		}
	}
	return false
}

func isFishingShop(storeID string) bool {
	id := strings.ToLower(strings.TrimSpace(storeID))
	return strings.HasPrefix(id, "fishingstore_") || strings.HasPrefix(id, "fishing_store_")
}

func discontinuedShopOpenAt(now time.Time) bool {
	minute := int(gameMinuteOfDayAt(now))
	return minute >= 9*60 && minute < 12*60 || minute >= 20*60 && minute < 22*60
}

func (s *Server) shopItemsAt(ctx context.Context, storeID string, now time.Time) ([]*store.ShopPrice, error) {
	items, err := s.DB.ShopItems(ctx, storeID)
	if err != nil || !isFishingShop(storeID) || discontinuedShopOpenAt(now) {
		return items, err
	}
	out := make([]*store.ShopPrice, 0, len(items))
	for _, p := range items {
		if p != nil && !store.IsDiscontinuedShopItem(p.ItemID) {
			out = append(out, p)
		}
	}
	return out, nil
}
