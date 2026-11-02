package vfs

import (
	"errors"
	"testing"
	"testing/fstest"
	"time"
)

func testFS() *FS {
	return New(Config{
		Base: fstest.MapFS{
			"etc/passwd":          {Data: []byte("root:x:0:0\n")},
			"etc/shadow":          {Data: []byte("secret\n")},
			"home/u/notes.txt":    {Data: []byte("hi\n")},
			"home/u/.hidden":      {Data: []byte("x")},
			"home/u/sub/deep.txt": {Data: []byte("deep")},
			"root/.bash_history":  {Data: []byte("x")},
		},
		Static:     map[string]string{"/home/u/app/main.go": "package main\n"},
		ExtraDirs:  []string{"/tmp"},
		Generated:  map[string]func() []byte{"/proc/uptime": func() []byte { return []byte("1.00 2.00\n") }},
		User:       "u",
		Home:       "/home/u",
		Writable:   []string{"/home/u", "/tmp"},
		Private:    []string{"/root", "/etc/shadow"},
		BaseTime:   time.Unix(0, 0),
		MaxEntries: 4,
		MaxBytes:   10,
	})
}

func TestResolveStaysInsideTheTree(t *testing.T) {
	cases := map[[2]string]string{
		{"/home/u", "../../../../etc/passwd"}: "/etc/passwd",
		{"/home/u", "/../../.."}:              "/",
		{"/home/u", "a/./b/../c"}:             "/home/u/a/c",
		{"/", "x\x00y"}:                       "",
	}
	for in, want := range cases {
		if got := Resolve(in[0], in[1]); got != want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestLayers(t *testing.T) {
	f := testFS()
	if data, err := f.ReadFile("/proc/uptime"); err != nil || string(data) != "1.00 2.00\n" {
		t.Errorf("generated: %q %v", data, err)
	}
	if data, err := f.ReadFile("/home/u/app/main.go"); err != nil || string(data) != "package main\n" {
		t.Errorf("static: %q %v", data, err)
	}
	if !f.IsDir("/proc") || !f.IsDir("/home/u/app") || !f.IsDir("/tmp") {
		t.Error("implicit directories missing")
	}
	entries, err := f.ReadDir("/home/u")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if got := len(names); got != 4 { // .hidden, app, notes.txt, sub
		t.Errorf("ReadDir(/home/u) = %v", names)
	}
	root, _ := f.ReadDir("/")
	if len(root) != 5 { // etc, home, proc, root, tmp
		t.Errorf("ReadDir(/) has %d entries", len(root))
	}
}

func TestPermissions(t *testing.T) {
	f := testFS()
	if _, err := f.ReadFile("/etc/shadow"); !errors.Is(err, ErrPermission) {
		t.Errorf("shadow: %v", err)
	}
	if _, err := f.ReadDir("/root"); !errors.Is(err, ErrPermission) {
		t.Errorf("/root: %v", err)
	}
	if err := f.WriteFile("/etc/new", nil); !errors.Is(err, ErrPermission) {
		t.Errorf("write /etc: %v", err)
	}
	if err := f.WriteFile("/home/u/notes.txt", []byte("x")); !errors.Is(err, ErrPermission) {
		t.Errorf("overwrite base file: %v", err)
	}
	if err := f.Remove("/home/u/notes.txt", false); !errors.Is(err, ErrPermission) {
		t.Errorf("remove base file: %v", err)
	}
	if err := f.Mkdir("/home/u/notes.txt/x", false); !errors.Is(err, ErrNotDir) {
		t.Errorf("mkdir under a file: %v", err)
	}
}

func TestOverlayLimitsAndReset(t *testing.T) {
	f := testFS()
	if err := f.WriteFile("/tmp/a", []byte("12345")); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile("/tmp/b", []byte("123456")); !errors.Is(err, ErrNoSpace) {
		t.Errorf("byte limit: %v", err)
	}
	if err := f.WriteFile("/tmp/a", []byte("1234567890")); err != nil {
		t.Errorf("replace within limit: %v", err)
	}
	for _, p := range []string{"/tmp/c", "/tmp/d", "/tmp/e"} {
		if err := f.Touch(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Touch("/tmp/f"); !errors.Is(err, ErrNoSpace) {
		t.Errorf("entry limit: %v", err)
	}
	f.Reset()
	if _, err := f.Stat("/tmp/a"); !errors.Is(err, ErrNotExist) {
		t.Errorf("after reset: %v", err)
	}
	if err := f.Mkdir("/home/u/x/y/z", true); err != nil {
		t.Errorf("mkdir -p: %v", err)
	}
	if err := f.Remove("/home/u/x", false); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("remove non-empty: %v", err)
	}
	if err := f.Remove("/home/u/x", true); err != nil {
		t.Errorf("remove -r: %v", err)
	}
}
