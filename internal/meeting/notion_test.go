package meeting

import "testing"

func TestNotionBlockURL(t *testing.T) {
	got, err := NotionBlockURL(
		"3d12d5f2-8d7d-8067-ad48-c686bec6fb0a",
		"3d12d5f2-8d7d-803f-a8ec-e1c7b133b4f5",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://app.notion.com/p/3d12d5f28d7d8067ad48c686bec6fb0a#3d12d5f28d7d803fa8ece1c7b133b4f5"
	if got != want {
		t.Fatalf("NotionBlockURL() = %q, want %q", got, want)
	}
}

func TestNotionBlockURLRejectsInvalidIDs(t *testing.T) {
	if _, err := NotionBlockURL("not-a-page", "not-a-block"); err == nil {
		t.Fatal("expected invalid IDs to fail")
	}
}
