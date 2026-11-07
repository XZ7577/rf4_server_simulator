package gameserver

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

func TestStoreChangeQueryAcrossTimedWindows(t *testing.T) {
	srv, _ := newEconomyTxTestServer(t)
	for _, tt := range []struct {
		since, now int
		changed    bool
	}{
		{8*60 + 59, 9 * 60, true},
		{9 * 60, 9*60 + 1, false},
		{11*60 + 59, 12 * 60, true},
		{19*60 + 59, 20 * 60, true},
		{21*60 + 59, 22 * 60, true},
		{22 * 60, 23 * 60, false},
		{8 * 60, 13 * 60, true},
		{20 * 60, 24*60 + 20*60, true},
	} {
		for _, shop := range []string{"fishing_store_19", "fishingstore_volhov", "atm_006", "housestore_019"} {
			c := economyTestCtx(srv, 1, proto.Args524(proto.StrVal(shop),
				proto.U64(uint64(proto.UnixToFiletime(gameClockAtMinute(tt.since).Unix())))))
			body, ok := hStoreQueryAt(c, gameClockAtMinute(tt.now))
			want := byte(0x0f)
			if shop == "housestore_019" || isFishingShop(shop) && tt.changed {
				want = 0x0e
			}
			if !ok || !bytes.Equal(body, []byte{want}) {
				t.Fatalf("%s %d -> %d: %x want %x", shop, tt.since, tt.now, body, want)
			}
		}
	}
}

func TestReportedReelPurchasesAndClosedWindowResult(t *testing.T) {
	srv, _ := newEconomyTxTestServer(t)
	db, err := store.OpenWithStatic(filepath.Join(t.TempDir(), "player.db"), filepath.Join("..", "..", "server_data", "static.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	srv.SetDB(db)
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=1000000000 WHERE uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"spin_5042", "spin_5025", "spin_5019", "spin_5018", "bolo_10043_SB23"} {
		makeCtx := func() *Ctx {
			c := economyTestCtx(srv, uid, proto.Args524(proto.StrVal("fishing_store_19"), proto.StrVal(id), proto.U64(1)))
			c.Main, c.Sub, c.ExpectResponse, c.CallID = 22, 2, true, uint32(i+1)
			c.replyCapture = &ctxReplyCapture{}
			return c
		}
		c := makeCtx()
		body, ok := hStoreBuyAt(false, gameClockAtMinute(22*60+9))(c)
		failure, present, _, failed := c.replyCapture.snapshot()
		if !ok || body != nil || !present || !failed || !bytes.Equal(failure, proto.BuildFailureEnvelope(c.CallID, "RF4.API.APIError", "129")) {
			t.Fatalf("closed %s did not return native out-of-stock result: %x", id, failure)
		}
		var balance, purchases int64
		if err := db.SQL.QueryRow(`SELECT silver FROM profile WHERE uid=?`, uid).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='buy'`, uid).Scan(&purchases); err != nil {
			t.Fatal(err)
		}
		if purchases != int64(i) {
			t.Fatal("closed purchase wrote a debit")
		}
		price, err := srv.storePriceAt(context.Background(), "fishing_store_19", id, 1, false, gameClockAtMinute(20*60+30))
		if err != nil {
			t.Fatal(err)
		}
		body, ok = hStoreBuyAt(false, gameClockAtMinute(20*60+30))(makeCtx())
		if !ok || len(body) == 0 {
			t.Fatalf("open %s purchase failed", id)
		}
		v, end, err := proto.DecodeValue(body, 0, srv.Table())
		if err != nil || end != len(body) || v.TypeID != 804 {
			t.Fatalf("invalid purchase response for %s: %v", id, err)
		}
		var after, granted int64
		if err := db.SQL.QueryRow(`SELECT silver FROM profile WHERE uid=?`, uid).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, id).Scan(&granted); err != nil {
			t.Fatal(err)
		}
		if after != balance-int64(price) || granted != 1 {
			t.Fatalf("%s debit/delivery mismatch: %d %d %d %d", id, balance, price, after, granted)
		}
	}
}

func gameClockAtMinute(minute int) time.Time {
	return time.Unix(worldDayEpochUnix+int64(float64(minute)/gameMinutesPerRealSecond), 0)
}

func TestDiscontinuedShopOpenWindowsUseGameTime(t *testing.T) {
	tests := []struct {
		minute int
		open   bool
	}{
		{8*60 + 59, false},
		{9 * 60, true},
		{11*60 + 59, true},
		{12 * 60, false},
		{19*60 + 59, false},
		{20 * 60, true},
		{21*60 + 59, true},
		{22 * 60, false},
	}
	for _, tt := range tests {
		if got := discontinuedShopOpenAt(gameClockAtMinute(tt.minute)); got != tt.open {
			t.Fatalf("游戏时间 %02d:%02d open=%v want=%v", tt.minute/60, tt.minute%60, got, tt.open)
		}
	}
}

func TestDiscontinuedShopCatalogStatus(t *testing.T) {
	for _, id := range []string{"bolo_10043_SB13", "bolo_10043_SB16", "bolo_10043_SB23", "bclp_5005", "crankbaits_738"} {
		if !store.IsDiscontinuedShopItem(id) {
			t.Fatalf("missing discontinued tackle %s", id)
		}
	}
	for _, id := range []string{"bolo_10043n_SB13", "AV_shamp_1l", "unknown"} {
		if store.IsDiscontinuedShopItem(id) {
			t.Fatalf("incorrect timed item %s", id)
		}
	}
}

func TestDiscontinuedShopConfigVersionMatchesBlob(t *testing.T) {
	blob, err := configBlob(2)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := configVersions()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha1.Sum(blob)
	if string(versions[1]) != hex.EncodeToString(hash[:]) {
		t.Fatal("client would retain the obsolete discontinued catalog")
	}
	raw, err := configWireFS.ReadFile("assets/config_wire/index.json")
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Types map[string]struct {
			Bytes int `json:"bytes"`
		} `json:"types"`
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatal(err)
	}
	if index.Types["2"].Bytes != len(blob) {
		t.Fatal("config length mismatch")
	}
}

func TestDiscontinuedShopWithReadOnlyStaticDatabase(t *testing.T) {
	srv, _ := newEconomyTxTestServer(t)
	db, err := store.OpenWithStatic(filepath.Join(t.TempDir(), "player.db"), filepath.Join("..", "..", "server_data", "static.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	srv.SetDB(db)
	for _, minute := range []int{20*60 + 47, 22 * 60} {
		rows, err := srv.shopItemsAt(context.Background(), "fishingstore_volhov", gameClockAtMinute(minute))
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"bolo_10043_SB13", "bolo_10043_SB16", "bolo_10043_SB23"} {
			found := false
			for _, p := range rows {
				if p.ItemID == id {
					found = p.Silver > 0 && p.Stock > 0
				}
			}
			if found != (minute < 22*60) {
				t.Fatalf("read-only static shelf %s at %d: %v", id, minute, found)
			}
		}
	}
}

func TestReportedDiscontinuedRodsShelfAndPurchasePrice(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()
	if _, err := db.SQL.Exec(`ATTACH DATABASE '../../server_data/static.sqlite3' AS src;
 INSERT OR REPLACE INTO main.shop_item SELECT * FROM src.shop_item;
 INSERT OR REPLACE INTO main.item_price SELECT * FROM src.item_price;
 INSERT OR REPLACE INTO main.item_catalog SELECT * FROM src.item_catalog;
 DETACH DATABASE src;`); err != nil {
		t.Fatal(err)
	}
	ids := []string{"bolo_10043_SB13", "bolo_10043_SB16", "bolo_10043_SB23"}
	for _, minute := range []int{9 * 60, 11*60 + 59, 12 * 60, 20*60 + 47, 22 * 60} {
		now := gameClockAtMinute(minute)
		rows, err := srv.shopItemsAt(ctx, "fishingstore_volhov", now)
		if err != nil {
			t.Fatal(err)
		}
		shelf := map[string]*store.ShopPrice{}
		for _, p := range rows {
			shelf[p.ItemID] = p
		}
		open := minute < 12*60 || minute >= 20*60 && minute < 22*60
		for _, id := range ids {
			p := shelf[id]
			price, err := srv.storePriceAt(ctx, "fishingstore_volhov", id, 1, false, now)
			if open {
				if p == nil || p.Silver <= 0 || p.Stock <= 0 || err != nil || price != uint64(p.Silver) {
					t.Fatalf("%d %s shelf=%+v price=%d err=%v", minute, id, p, price, err)
				}
				if _, err := srv.storePriceAt(ctx, "fishingstore_volhov", id, 1, true, now); err == nil {
					t.Fatal("invented a gold price")
				}
			} else if p != nil || err == nil {
				t.Fatalf("closed shelf or price accepted %s at %d", id, minute)
			}
		}
		const normal = "bolo_10043n_SB13"
		if shelf[normal] == nil {
			t.Fatal("normal rod disappeared")
		}
		if _, err := srv.storePriceAt(ctx, "fishingstore_volhov", normal, 1, false, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, shop := range []string{"fishingstore_unknown", "admin_012"} {
		if _, err := srv.storePriceAt(ctx, shop, ids[0], 1, false, gameClockAtMinute(20*60+47)); err == nil {
			t.Fatalf("wrong shop accepted %s", shop)
		}
	}
}

func TestFishingShopScope(t *testing.T) {
	for _, tt := range []struct {
		store string
		want  bool
	}{
		{"fishingstore_009", true},
		{"fishing_store_01", true},
		{"admin_012", false},
		{"", false},
	} {
		if got := isFishingShop(tt.store); got != tt.want {
			t.Fatalf("isFishingShop(%q)=%v want=%v", tt.store, got, tt.want)
		}
	}
}
