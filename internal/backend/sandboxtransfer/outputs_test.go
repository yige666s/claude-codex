package sandboxtransfer

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

type outputEntry struct {
	name string
	data string
	kind byte
}

func outputArchive(t *testing.T, items ...outputEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, item := range items {
		h := &tar.Header{Name: item.name, Typeflag: item.kind, Size: int64(len(item.data)), Mode: 0644}
		if item.kind == tar.TypeDir {
			h.Size = 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if item.kind == tar.TypeReg {
			if _, err := w.Write([]byte(item.data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func TestCollectOutputsLimitsAndDigest(t *testing.T) {
	archive := outputArchive(t, outputEntry{"outputs/", "", tar.TypeDir}, outputEntry{"outputs/report.txt", "hello", tar.TypeReg})
	results, err := CollectOutputs(context.Background(), bytes.NewReader(archive), 32, 3)
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%v err=%v", results, err)
	}
	sum := sha256.Sum256([]byte("hello"))
	if results[0].Path != "report.txt" || string(results[0].Content) != "hello" || results[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("bad artifact: %+v", results[0])
	}
	if _, err := CollectOutputs(context.Background(), bytes.NewReader(archive), 4, 3); err == nil {
		t.Fatal("content budget ignored")
	}
	if _, err := CollectOutputs(context.Background(), bytes.NewReader(archive), 32, 1); err == nil {
		t.Fatal("entry budget ignored")
	}
	if _, err := CollectOutputs(context.Background(), bytes.NewReader(archive), 32, 3); err != nil {
		t.Fatalf("repeat collection: %v", err)
	}
}
func TestCollectOutputsRejectsLinksTraversalDuplicatesAndOverflow(t *testing.T) {
	root := outputEntry{"outputs/", "", tar.TypeDir}
	cases := [][]outputEntry{
		{root, {"outputs/../escape", "bad", tar.TypeReg}},
		{root, {"outputs/a", "bad", tar.TypeSymlink}},
		{root, {"workspace/a", "bad", tar.TypeReg}},
		{root, {"outputs/a", "a", tar.TypeReg}, {"outputs/a", "b", tar.TypeReg}},
		{root, {"outputs/a", "a", tar.TypeReg}, {"outputs/a/", "", tar.TypeDir}},
		{{"outputs", "bad", tar.TypeReg}},
		{{"not-outputs/", "", tar.TypeDir}},
		{root, {"outputs/a", "overflow", tar.TypeReg}},
	}
	for i, items := range cases {
		// A symlink header must not have file payload.
		if i == 1 {
			items[1].data = ""
		}
		archive := outputArchive(t, items...)
		budget := int64(64)
		if i == 7 {
			budget = 4
		}
		if _, err := CollectOutputs(context.Background(), bytes.NewReader(archive), budget, 8); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	appended := append(outputArchive(t, root), []byte("unexpected")...)
	if _, err := CollectOutputs(context.Background(), bytes.NewReader(appended), 64, 4); err == nil {
		t.Fatal("appended payload accepted")
	}
	if _, err := CollectOutputs(context.Background(), strings.NewReader(strings.Repeat("x", 20000)), 4, 2); err == nil {
		t.Fatal("archive budget ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CollectOutputs(ctx, bytes.NewReader(outputArchive(t, root)), 64, 4); err == nil {
		t.Fatal("cancellation ignored")
	}
}
