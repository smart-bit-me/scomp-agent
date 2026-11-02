// Package vfs is the in-memory file tree of the fake shell.
//
// It layers three sources, none of which touches the real disk:
//
//   - a read-only base tree (any fs.FS, normally embedded data),
//   - generated files whose content is computed on every read (/proc/uptime),
//   - a small writable overlay held in memory and capped in size.
//
// Every path is resolved against "/" with path.Clean, so ".." can never climb
// out of the tree. Reset discards the overlay.
package vfs

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// Error is an FS error; its text is the message a shell prints.
type Error string

func (e Error) Error() string { return string(e) }

// Errors returned by FS operations.
const (
	ErrNotExist   Error = "No such file or directory"
	ErrExist      Error = "File exists"
	ErrNotDir     Error = "Not a directory"
	ErrIsDir      Error = "Is a directory"
	ErrPermission Error = "Permission denied"
	ErrNotEmpty   Error = "Directory not empty"
	ErrNoSpace    Error = "No space left on device"
	ErrInvalid    Error = "Invalid argument"
)

// Default overlay limits.
const (
	DefaultMaxEntries = 128
	DefaultMaxBytes   = 256 << 10
)

// Info describes one entry.
type Info struct {
	Name    string
	Path    string
	Dir     bool
	Mode    fs.FileMode // permission bits only
	Owner   string
	Size    int64
	ModTime time.Time
}

// Config configures a new FS.
type Config struct {
	Base       fs.FS                    // read-only tree; paths have no leading slash
	ExtraDirs  []string                 // empty directories the base cannot carry
	Static     map[string]string        // absolute path → content; read-only like Base
	Generated  map[string]func() []byte // absolute path → content, computed per read
	User       string                   // session user; owns Home and overlay entries
	Home       string                   // e.g. /home/review
	Writable   []string                 // directories under which the user may create entries
	Private    []string                 // paths (and subtrees) only root may read
	BaseTime   time.Time                // modification time of base entries
	Now        func() time.Time
	MaxEntries int
	MaxBytes   int
}

// FS is the layered tree. It is not safe for concurrent use; one shell
// session owns one FS.
type FS struct {
	cfg   Config
	files map[string][]byte
	dirs  map[string]bool
	mtime map[string]time.Time
	used  int
}

// New returns an FS with an empty overlay.
func New(cfg Config) *FS {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = DefaultMaxEntries
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if cfg.User == "" {
		cfg.User = "root"
	}
	f := &FS{cfg: cfg}
	f.Reset()
	return f
}

// Reset discards everything written since New.
func (f *FS) Reset() {
	f.files = map[string][]byte{}
	f.dirs = map[string]bool{}
	f.mtime = map[string]time.Time{}
	f.used = 0
}

// Home returns the configured home directory.
func (f *FS) Home() string { return f.cfg.Home }

// Resolve turns p into a clean absolute path relative to cwd. A path that
// contains NUL resolves to "".
func Resolve(cwd, p string) string {
	if strings.IndexByte(p, 0) >= 0 {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = cwd + "/" + p
	}
	return path.Clean("/" + p)
}

func basePath(abs string) string {
	if abs == "/" {
		return "."
	}
	return strings.TrimPrefix(abs, "/")
}

func under(p, dir string) bool {
	return p == dir || dir == "/" || strings.HasPrefix(p, dir+"/")
}

func (f *FS) private(p string) bool {
	if f.cfg.User == "root" {
		return false
	}
	for _, d := range f.cfg.Private {
		if under(p, d) {
			return true
		}
	}
	return false
}

func (f *FS) owner(p string) string {
	_, file := f.files[p]
	if file || f.dirs[p] || (f.cfg.Home != "" && under(p, f.cfg.Home)) {
		return f.cfg.User
	}
	return "root"
}

func (f *FS) mode(p string, dir bool) fs.FileMode {
	switch {
	case f.private(p) && dir:
		return 0o700
	case f.private(p):
		return 0o600
	case dir && (p == "/tmp" || p == "/var/tmp"):
		return 0o777
	case dir:
		return 0o755
	case strings.HasSuffix(p, ".sh"):
		return 0o755
	default:
		return 0o644
	}
}

func (f *FS) isExtraDir(p string) bool {
	for _, d := range f.cfg.ExtraDirs {
		if d == p {
			return true
		}
	}
	return false
}

func (f *FS) generatedParent(p string) bool {
	for g := range f.cfg.Generated {
		if under(g, p) && g != p {
			return true
		}
	}
	for g := range f.cfg.Static {
		if under(g, p) && g != p {
			return true
		}
	}
	return false
}

// Stat describes the entry at the absolute path p.
func (f *FS) Stat(p string) (Info, error) {
	if p == "" {
		return Info{}, ErrNotExist
	}
	info := Info{Name: path.Base(p), Path: p, Owner: f.owner(p), ModTime: f.cfg.BaseTime}
	if t, ok := f.mtime[p]; ok {
		info.ModTime = t
	}
	if data, ok := f.files[p]; ok {
		info.Size = int64(len(data))
		info.Mode = f.mode(p, false)
		return info, nil
	}
	if f.dirs[p] || f.isExtraDir(p) {
		info.Dir, info.Mode, info.Size = true, f.mode(p, true), 4096
		return info, nil
	}
	if gen, ok := f.cfg.Generated[p]; ok {
		info.Size, info.Mode, info.ModTime = int64(len(gen())), 0o444, f.cfg.Now()
		return info, nil
	}
	if content, ok := f.cfg.Static[p]; ok {
		info.Size, info.Mode = int64(len(content)), f.mode(p, false)
		return info, nil
	}
	if f.cfg.Base != nil {
		if st, err := fs.Stat(f.cfg.Base, basePath(p)); err == nil {
			info.Dir = st.IsDir()
			info.Mode = f.mode(p, info.Dir)
			info.Size = st.Size()
			if info.Dir {
				info.Size = 4096
			}
			return info, nil
		}
	}
	if f.generatedParent(p) {
		info.Dir, info.Mode = true, 0o555
		return info, nil
	}
	return Info{}, ErrNotExist
}

// IsDir reports whether p is an existing directory.
func (f *FS) IsDir(p string) bool {
	st, err := f.Stat(p)
	return err == nil && st.Dir
}

// ReadDir lists the directory p, sorted by name.
func (f *FS) ReadDir(p string) ([]Info, error) {
	st, err := f.Stat(p)
	if err != nil {
		return nil, err
	}
	if !st.Dir {
		return nil, ErrNotDir
	}
	if f.private(p) {
		return nil, ErrPermission
	}
	names := map[string]bool{}
	if f.cfg.Base != nil {
		if entries, err := fs.ReadDir(f.cfg.Base, basePath(p)); err == nil {
			for _, e := range entries {
				names[e.Name()] = true
			}
		}
	}
	add := func(child string) {
		for child != "/" {
			if path.Dir(child) == p {
				names[path.Base(child)] = true
				return
			}
			child = path.Dir(child)
		}
	}
	for _, d := range f.cfg.ExtraDirs {
		add(d)
	}
	for g := range f.cfg.Generated {
		add(g)
	}
	for g := range f.cfg.Static {
		add(g)
	}
	for d := range f.dirs {
		add(d)
	}
	for file := range f.files {
		add(file)
	}
	out := make([]Info, 0, len(names))
	for name := range names {
		if info, err := f.Stat(path.Join(p, name)); err == nil {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ReadFile returns the content of the file p.
func (f *FS) ReadFile(p string) ([]byte, error) {
	st, err := f.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.Dir {
		return nil, ErrIsDir
	}
	if f.private(p) {
		return nil, ErrPermission
	}
	if data, ok := f.files[p]; ok {
		return append([]byte(nil), data...), nil
	}
	if gen, ok := f.cfg.Generated[p]; ok {
		return gen(), nil
	}
	if content, ok := f.cfg.Static[p]; ok {
		return []byte(content), nil
	}
	data, err := fs.ReadFile(f.cfg.Base, basePath(p))
	if err != nil {
		return nil, ErrNotExist
	}
	return data, nil
}

func (f *FS) writable(dir string) bool {
	if f.cfg.User == "root" {
		return true
	}
	for _, d := range f.cfg.Writable {
		if under(dir, d) {
			return true
		}
	}
	return false
}

func (f *FS) checkCreate(p string) error {
	if p == "" || p == "/" {
		return ErrInvalid
	}
	parent := path.Dir(p)
	pst, err := f.Stat(parent)
	if err != nil {
		return ErrNotExist
	}
	if !pst.Dir {
		return ErrNotDir
	}
	if !f.writable(parent) || f.private(parent) {
		return ErrPermission
	}
	if len(f.files)+len(f.dirs) >= f.cfg.MaxEntries {
		return ErrNoSpace
	}
	return nil
}

// WriteFile creates or replaces the overlay file p. Base files are read-only.
func (f *FS) WriteFile(p string, data []byte) error {
	old, exists := f.files[p]
	if !exists {
		if st, err := f.Stat(p); err == nil {
			if st.Dir {
				return ErrIsDir
			}
			return ErrPermission
		}
		if err := f.checkCreate(p); err != nil {
			return err
		}
	}
	if f.used-len(old)+len(data) > f.cfg.MaxBytes {
		return ErrNoSpace
	}
	f.used += len(data) - len(old)
	f.files[p] = append([]byte(nil), data...)
	f.mtime[p] = f.cfg.Now()
	return nil
}

// Touch creates an empty file or updates an overlay entry's time. Touching an
// existing base entry succeeds only where the user could write.
func (f *FS) Touch(p string) error {
	if _, err := f.Stat(p); err != nil {
		return f.WriteFile(p, nil)
	}
	if _, ok := f.files[p]; ok || f.dirs[p] {
		f.mtime[p] = f.cfg.Now()
		return nil
	}
	if !f.writable(path.Dir(p)) {
		return ErrPermission
	}
	return nil
}

// Mkdir creates the overlay directory p; parents behaves like mkdir -p.
func (f *FS) Mkdir(p string, parents bool) error {
	if parents {
		if f.IsDir(p) {
			return nil
		}
		if parent := path.Dir(p); parent != p && !f.IsDir(parent) {
			if err := f.Mkdir(parent, true); err != nil {
				return err
			}
		}
	}
	if _, err := f.Stat(p); err == nil {
		return ErrExist
	}
	if err := f.checkCreate(p); err != nil {
		return err
	}
	f.dirs[p] = true
	f.mtime[p] = f.cfg.Now()
	return nil
}

// Remove deletes an overlay entry; directories need recursive. Base and
// generated entries cannot be removed.
func (f *FS) Remove(p string, recursive bool) error {
	if _, err := f.Stat(p); err != nil {
		return err
	}
	if data, ok := f.files[p]; ok {
		f.used -= len(data)
		delete(f.files, p)
		delete(f.mtime, p)
		return nil
	}
	if !f.dirs[p] {
		return ErrPermission
	}
	children, _ := f.ReadDir(p)
	if len(children) > 0 && !recursive {
		return ErrNotEmpty
	}
	for _, c := range children {
		if err := f.Remove(c.Path, true); err != nil {
			return err
		}
	}
	delete(f.dirs, p)
	delete(f.mtime, p)
	return nil
}
