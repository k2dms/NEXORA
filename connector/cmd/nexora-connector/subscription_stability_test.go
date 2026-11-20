package main

import "testing"

// VER-67's checker. Doc 29 §6's id-reuse row promises that a refresh never orphans the rules naming a
// server: `id`, its `tag` (the id's own first eight hex digits) and `userIdRef` are stable across
// refreshes, because the public id is derived from the normalized connection fields alone — the feed's
// display name and the credential UUID are deliberately absent from it — and `userIdRef` is spelled from
// the id. What no test showed before this one is that the real parser and the real refresh path keep the
// promise; the merge decision itself (a user rename outranking the feed name) belongs to the extension's
// registry, and the Connector's half is delivering the same id so the merge has something to match on.

func TestSubscriptionStableIdentityAcrossFeedRenameAndRotation(t *testing.T) {
	id1 := "11111111-1111-4111-8111-111111111111"
	body1 := "vless://" + id1 + "@srv1.example.test:443?type=tcp&security=none#Feed%20calls%20it%20this"
	servers1, _, _, dropped1, err := parseSubscription([]byte(body1), "auto", subscriptionTestID, 1024)
	if err != nil || dropped1 != 0 || len(servers1) != 1 {
		t.Fatalf("first parse: err=%v dropped=%d", err, dropped1)
	}
	// Same connection fields; a different credential UUID and a feed that renamed the server.
	id2 := "22222222-2222-4222-8222-222222222222"
	body2 := "vless://" + id2 + "@srv1.example.test:443?type=tcp&security=none#The%20user%20renamed%20it"
	servers2, _, _, dropped2, err := parseSubscription([]byte(body2), "auto", subscriptionTestID, 1024)
	if err != nil || dropped2 != 0 || len(servers2) != 1 {
		t.Fatalf("second parse: err=%v dropped=%d", err, dropped2)
	}
	a, b := servers1[0], servers2[0]
	for _, field := range []string{"id", "userIdRef"} {
		if a[field] != b[field] {
			t.Fatalf("%s moved across a rename and a rotation: %v -> %v", field, a[field], b[field])
		}
	}
	// The compiler derives the outbound tag from the id's own first eight hex digits, so a stable id is a
	// stable tag by construction; this pins the derivation the compiler uses.
	tagA, tagB := "srv-"+a["id"].(string)[:8], "srv-"+b["id"].(string)[:8]
	if tagA != tagB {
		t.Fatalf("tag moved: %v -> %v", tagA, tagB)
	}
	ref := "keystore://server/" + a["id"].(string) + "/user-id"
	if a["userIdRef"] != ref {
		t.Fatalf("userIdRef is not spelled from the id: %v", a["userIdRef"])
	}
	if a["displayName"] == b["displayName"] {
		t.Fatal("a rename in the feed must reach the refresh result as a different display name; the merge decides which one wins")
	}
	// A genuinely different server must not collide with the first one.
	body3 := "vless://" + id1 + "@srv2.example.test:443?type=tcp&security=none#Other"
	servers3, _, _, _, err := parseSubscription([]byte(body3), "auto", subscriptionTestID, 1024)
	if err != nil || len(servers3) != 1 {
		t.Fatalf("third parse: %v", err)
	}
	if servers3[0]["id"] == a["id"] {
		t.Fatal("a different address derived the same public id")
	}
}

func TestSubscriptionRefreshKeepsIdentityAcrossTwoFetches(t *testing.T) {
	h, _ := subscriptionTestHandler(t)
	frame := subscriptionTestFrame(subscriptionTestSource())
	v1 := subscriptionTestBody("stable.example.test") // credential importUser, fragment "Test server"
	subscriptionTestFeed(h, v1)
	first := subscriptionTestAnswer(t, h, frame)
	if len(first.Refreshed) != 1 || len(first.Refreshed[0].Servers) != 1 {
		t.Fatalf("first refresh: %+v", first)
	}
	one := first.Refreshed[0].Servers[0]
	// Same server, different credential UUID and a changed feed name: the body's hash moves, so the
	// source is refreshed rather than answered Unchanged.
	v2 := "vless://99999999-9999-4999-8999-999999999999@stable.example.test:443?type=tcp&security=none#Renamed%20by%20feed"
	subscriptionTestFeed(h, v2)
	second := subscriptionTestAnswer(t, h, frame)
	if len(second.Refreshed) != 1 || len(second.Refreshed[0].Servers) != 1 {
		t.Fatalf("second refresh: %+v", second)
	}
	two := second.Refreshed[0].Servers[0]
	for _, field := range []string{"id", "tag", "userIdRef", "subscriptionId", "source"} {
		if one[field] != two[field] {
			t.Fatalf("%s moved across a refresh: %v -> %v", field, one[field], two[field])
		}
	}
	if one["displayName"] == two["displayName"] {
		t.Fatal("the feed's rename never reached the second result; the comparison would not notice a feed that renamed the server")
	}
}
