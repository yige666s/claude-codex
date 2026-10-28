package sandboxtransfer

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSnapshotRoundTripAndBounds(t *testing.T) {
	base := t.TempDir()
	workBase := filepath.Join(base, "workspaces")
	work := filepath.Join(workBase, "alice")
	skillsBase := filepath.Join(base, "skills")
	skill := filepath.Join(skillsBase, "docx")
	for _, dir := range []string{work, filepath.Join(skill, "scripts")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "input.txt"), []byte("fake user data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "scripts", "render.py"), []byte("print('ok')"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := Builder{WorkspaceRoot: workBase, SkillRoots: []string{skillsBase}, Limits: DevLimits()}
	data, err := builder.Build(context.Background(), work, skill)
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := ExtractSnapshot(data, dest, DevLimits()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "workspace", "input.txt"))
	if err != nil || string(got) != "fake user data" {
		t.Fatalf("workspace=%q %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dest, "skill", "scripts", "render.py"))
	if err != nil || string(got) != "print('ok')" {
		t.Fatalf("skill=%q %v", got, err)
	}
	if err := ExtractSnapshot(data, dest, DevLimits()); err == nil {
		t.Fatal("non-empty destination accepted")
	}
	tiny := Limits{MaxFiles: 10, MaxBytes: 4, MaxFileBytes: 4}
	if _, err := (Builder{WorkspaceRoot: workBase, SkillRoots: []string{skillsBase}, Limits: tiny}).Build(context.Background(), work, skill); err == nil {
		t.Fatal("oversize source accepted")
	}
}

func tarData(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, h := range entries {
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := w.Write(bytes.Repeat([]byte("x"), int(h.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func roots() []*tar.Header {
	return []*tar.Header{{Name: "workspace/", Typeflag: tar.TypeDir}, {Name: "skill/", Typeflag: tar.TypeDir}}
}

func TestExtractRejectsTraversalLinksDuplicatesAndSpecialFiles(t *testing.T) {
	cases := []struct {
		name   string
		header *tar.Header
	}{
		{"parent", &tar.Header{Name: "workspace/../escape", Typeflag: tar.TypeReg, Size: 1}},
		{"absolute", &tar.Header{Name: "/tmp/escape", Typeflag: tar.TypeReg, Size: 1}},
		{"backslash", &tar.Header{Name: `workspace\\escape`, Typeflag: tar.TypeReg, Size: 1}},
		{"symlink", &tar.Header{Name: "workspace/link", Typeflag: tar.TypeSymlink, Linkname: "../../secret"}},
		{"hardlink", &tar.Header{Name: "workspace/link", Typeflag: tar.TypeLink, Linkname: "/etc/shadow"}},
		{"device", &tar.Header{Name: "workspace/device", Typeflag: tar.TypeChar}},
		{"unknown-root", &tar.Header{Name: "secret/data", Typeflag: tar.TypeReg, Size: 1}},
		{"duplicate", &tar.Header{Name: "workspace/", Typeflag: tar.TypeDir}},
		{"root-as-file", &tar.Header{Name: "workspace", Typeflag: tar.TypeReg, Size: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers := append(roots(), tc.header)
			data := tarData(t, headers...)
			parent := t.TempDir()
			dest := filepath.Join(parent, "private")
			if err := os.Mkdir(dest, 0700); err != nil {
				t.Fatal(err)
			}
			if err := ExtractSnapshot(data, dest, DevLimits()); err == nil {
				t.Fatal("malicious tar accepted")
			}
			if _, err := os.Stat(filepath.Join(parent, "escape")); !os.IsNotExist(err) {
				t.Fatalf("escaped destination: %v", err)
			}
		})
	}
}

func TestBuilderRejectsOutsideAndSymlinks(t *testing.T) {
	base := t.TempDir()
	workBase := filepath.Join(base, "workspaces")
	work := filepath.Join(workBase, "alice")
	skillBase := filepath.Join(base, "skills")
	skill := filepath.Join(skillBase, "demo")
	for _, dir := range []string{work, skill} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	builder := Builder{WorkspaceRoot: workBase, SkillRoots: []string{skillBase}, Limits: DevLimits()}
	if _, err := builder.Build(context.Background(), base, skill); err == nil {
		t.Fatal("workspace outside root accepted")
	}
	if _, err := builder.Build(context.Background(), work, base); err == nil {
		t.Fatal("skill outside roots accepted")
	}
	if err := os.Symlink(base, filepath.Join(work, "link")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := builder.Build(context.Background(), work, skill); err == nil || !strings.Contains(err.Error(), "special file") {
		t.Fatalf("symlink source accepted: %v", err)
	}
}

func TestBuilderRejectsHardlinkOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux link-count validation")
	}
	base := t.TempDir()
	root := filepath.Join(base, "workspaces")
	work := filepath.Join(root, "alice")
	skillBase := filepath.Join(base, "skills")
	skill := filepath.Join(skillBase, "demo")
	for _, dir := range []string{work, skill} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(base, "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(work, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := (Builder{WorkspaceRoot: root, SkillRoots: []string{skillBase}, Limits: DevLimits()}).Build(context.Background(), work, skill); err == nil {
		t.Fatal("hardlink source accepted")
	}
}

func TestBuilderRejectsSymlinkInConfiguredRootAncestor(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "workspaces", "alice"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "skills", "demo"), 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(real, alias); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	b := Builder{WorkspaceRoot: filepath.Join(alias, "workspaces"), SkillRoots: []string{filepath.Join(base, "skills")}, Limits: DevLimits()}
	_, err := b.Build(context.Background(), filepath.Join(alias, "workspaces", "alice"), filepath.Join(base, "skills", "demo"))
	if err == nil || !strings.Contains(err.Error(), "ancestor is symlink") {
		t.Fatalf("symlinked ancestor accepted: %v", err)
	}
}

func TestSeparatePublishedSkillAndPerTaskWorkspace(t *testing.T) {
	base := t.TempDir()
	workRoot := filepath.Join(base, "work")
	work := filepath.Join(workRoot, "alice")
	skillRoot := filepath.Join(base, "skills")
	skill := filepath.Join(skillRoot, "docx")
	for _, p := range []string{work, skill} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "input.txt"), []byte("private-input"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("published-skill"), 0600); err != nil {
		t.Fatal(err)
	}
	b := Builder{WorkspaceRoot: workRoot, SkillRoots: []string{skillRoot}, Limits: DevLimits()}
	input, err := b.BuildWorkspace(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	published, err := b.BuildSkill(context.Background(), skill)
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := ExtractWorkspace(input, dest, DevLimits()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "skill")); !os.IsNotExist(err) {
		t.Fatalf("skill included in request: %v", err)
	}
	second := t.TempDir()
	if err := ExtractSkill(published, second, DevLimits()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second, "workspace")); !os.IsNotExist(err) {
		t.Fatalf("user input included in published artifact: %v", err)
	}
	if err := ExtractSkill(input, t.TempDir(), DevLimits()); err == nil {
		t.Fatal("workspace archive accepted as skill")
	}
	if err := ExtractWorkspace(published, t.TempDir(), DevLimits()); err == nil {
		t.Fatal("skill archive accepted as workspace")
	}
}
