package magicdocs

import "testing"

func TestDetectAndRegisterMagicDoc(t *testing.T) {
	content := "# MAGIC DOC: Build Guide\n*Keep this updated*\n\nBody"
	info, ok := DetectHeader(content)
	if !ok || info.Title != "Build Guide" || info.Instructions != "Keep this updated" {
		t.Fatalf("unexpected detection: %+v %v", info, ok)
	}
	service := NewService()
	if !service.Register("/tmp/doc.md", content) {
		t.Fatal("expected register success")
	}
	if len(service.Tracked()) != 1 {
		t.Fatal("expected tracked magic doc")
	}
}
