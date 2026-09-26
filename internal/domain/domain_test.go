package domain

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
)

func TestGenerateIDFormat(t *testing.T) {
	id := GenerateID(PrefixRun, 1_790_000_000_000, [8]byte{0xde, 0xad, 0xbe, 0xef, 0, 1, 2, 3})
	if !regexp.MustCompile(`^run_[0-9a-z]{9}[0-9a-f]{16}$`).MatchString(id) {
		t.Fatalf("id %q does not match the format", id)
	}
	if got, want := id[len(id)-16:], "deadbeef00010203"; got != want {
		t.Fatalf("random part = %q, want %q", got, want)
	}
}

func TestGenerateIDSortsByTime(t *testing.T) {
	var r [8]byte
	earlier := GenerateID(PrefixProject, 1_000, [8]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	later := GenerateID(PrefixProject, 1_790_000_000_000, r)
	if !(earlier < later) {
		t.Fatalf("expected %q < %q", earlier, later)
	}
}

func TestTargetWithDefaults(t *testing.T) {
	got := TargetStack{DeployTarget: DeployTargetVercel}.WithDefaults()
	want := DefaultTarget()
	want.DeployTarget = DeployTargetVercel
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestTargetCheckSupported(t *testing.T) {
	ok := []TargetStack{
		DefaultTarget(),
		TargetStack{DeployTarget: DeployTargetVercel}.WithDefaults(),
	}
	for _, tgt := range ok {
		if err := tgt.CheckSupported(); err != nil {
			t.Errorf("%+v: unexpected error %v", tgt, err)
		}
	}

	bad := []TargetStack{
		TargetStack{Framework: FrameworkVue}.WithDefaults(),
		TargetStack{Language: LanguageCSharp}.WithDefaults(),
		TargetStack{Database: DatabaseMongoDB}.WithDefaults(),
		TargetStack{GitProvider: GitProviderGitLab}.WithDefaults(),
		TargetStack{DeployTarget: DeployTargetNetlify}.WithDefaults(),
		TargetStack{Framework: "not-a-framework"}.WithDefaults(),
	}
	for _, tgt := range bad {
		code, _ := CodeOf(tgt.CheckSupported())
		if code != CodeUnsupportedTarget {
			t.Errorf("%+v: code = %q, want %q", tgt, code, CodeUnsupportedTarget)
		}
	}
}

func TestCheckPath(t *testing.T) {
	safe := []string{"app/page.tsx", "package.json", "src/[id]/page.tsx", "a/..b/c", "a/b../c", "./x"}
	for _, p := range safe {
		if err := CheckPath(p); err != nil {
			t.Errorf("CheckPath(%q) = %v, want nil", p, err)
		}
	}

	unsafe := []string{"", "/etc/passwd", `app\page.tsx`, "../x", "a/../b", "a/..", "..", "a\x00b"}
	for _, p := range unsafe {
		var upe *UnsafePathError
		if err := CheckPath(p); !errors.As(err, &upe) {
			t.Errorf("CheckPath(%q) = %v, want *UnsafePathError", p, err)
		}
	}
}

func TestCodeOfThroughWrapping(t *testing.T) {
	err := fmt.Errorf("outer: %w", Errorf(CodeNotFound, "run %s", "run_x"))
	if code, ok := CodeOf(err); !ok || code != CodeNotFound {
		t.Fatalf("CodeOf = %q, %v", code, ok)
	}
	if _, ok := CodeOf(errors.New("plain")); ok {
		t.Fatal("plain error must have no code")
	}
}
