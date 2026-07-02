package webdav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/debridnest/debridnest/internal/storage"
	"github.com/debridnest/debridnest/internal/torrent"
	xwebdav "golang.org/x/net/webdav"
)

var errReadOnly = errors.New("read-only filesystem")

type torrentFS struct {
	manager *torrent.Manager
}

func newTorrentFS(manager *torrent.Manager) *torrentFS {
	return &torrentFS{manager: manager}
}

type torrentIndex struct {
	byFolder   map[string]*storage.TorrentRecord
	byAlias    map[string]*storage.TorrentRecord
	idToFolder map[string]string
}

func cleanPath(name string) string {
	name = strings.TrimPrefix(name, "/")
	name = path.Clean(name)
	if name == "." {
		return ""
	}
	return name
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unnamed"
	}
	var b strings.Builder
	for _, r := range name {
		if r == '/' || r == 0 {
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

func (fs *torrentFS) buildIndex(ctx context.Context) (*torrentIndex, error) {
	items, err := fs.manager.List(ctx, 10000)
	if err != nil {
		return nil, err
	}
	idx := &torrentIndex{
		byFolder:   make(map[string]*storage.TorrentRecord),
		byAlias:    make(map[string]*storage.TorrentRecord),
		idToFolder: make(map[string]string),
	}
	seen := map[string]int{}
	for i := range items {
		rec := &items[i]
		if !torrent.IsCompletedStatus(rec.Status) {
			continue
		}
		base := sanitizeName(rec.Name)
		key := strings.ToLower(base)
		seen[key]++
		folder := base
		if seen[key] > 1 {
			folder = fmt.Sprintf("%s (%d)", base, seen[key])
		}
		idx.byFolder[folder] = rec
		idx.idToFolder[rec.ID] = folder
		idx.byAlias[strings.ToLower(folder)] = rec
		idx.byAlias[strings.ToLower(rec.ID)] = rec
		if rec.InfoHash != "" {
			idx.byAlias[strings.ToLower(rec.InfoHash)] = rec
		}
	}
	return idx, nil
}

func torrentModTime(rec *storage.TorrentRecord) time.Time {
	if rec.EndedAt != nil {
		return *rec.EndedAt
	}
	return rec.AddedAt
}

func listChildren(rec *storage.TorrentRecord, subPath string) []fileInfo {
	subPath = strings.Trim(subPath, "/")
	children := map[string]fileInfo{}
	modTime := torrentModTime(rec)

	for _, f := range rec.Files {
		if !f.Selected {
			continue
		}
		rel := strings.TrimPrefix(strings.ReplaceAll(f.Path, "\\", "/"), "/")
		if subPath != "" {
			if rel == subPath {
				name := path.Base(rel)
				children[name] = fileInfo{
					name:    name,
					size:    f.Bytes,
					mode:    0o444,
					modTime: modTime,
				}
				continue
			}
			prefix := subPath + "/"
			if !strings.HasPrefix(rel, prefix) {
				continue
			}
			rel = strings.TrimPrefix(rel, prefix)
		}
		parts := strings.Split(rel, "/")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		name := parts[0]
		if len(parts) == 1 {
			children[name] = fileInfo{
				name:    name,
				size:    f.Bytes,
				mode:    0o444,
				modTime: modTime,
			}
			continue
		}
		if _, ok := children[name]; !ok {
			children[name] = fileInfo{
				name:    name,
				size:    0,
				mode:    os.ModeDir | 0o555,
				modTime: modTime,
			}
		}
	}

	out := make([]fileInfo, 0, len(children))
	for _, fi := range children {
		out = append(out, fi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func findFile(rec *storage.TorrentRecord, subPath string) (*storage.TorrentFileRecord, bool) {
	clean := strings.Trim(strings.ReplaceAll(subPath, "\\", "/"), "/")
	want := "/" + clean
	var basenameMatch *storage.TorrentFileRecord
	for i := range rec.Files {
		f := &rec.Files[i]
		if !f.Selected {
			continue
		}
		p := strings.ReplaceAll(f.Path, "\\", "/")
		if p == want {
			return f, true
		}
		if path.Base(p) == clean {
			if basenameMatch != nil {
				return nil, false
			}
			basenameMatch = f
		}
	}
	if basenameMatch != nil {
		return basenameMatch, true
	}
	return nil, false
}

func isVirtualDir(rec *storage.TorrentRecord, subPath string) bool {
	if subPath == "" {
		return true
	}
	subPath = strings.Trim(subPath, "/")
	for _, f := range rec.Files {
		if !f.Selected {
			continue
		}
		rel := strings.TrimPrefix(strings.ReplaceAll(f.Path, "\\", "/"), "/")
		if rel == subPath {
			return false
		}
		if strings.HasPrefix(rel, subPath+"/") {
			return true
		}
	}
	return false
}

type fileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (fi fileInfo) Name() string       { return fi.name }
func (fi fileInfo) Size() int64        { return fi.size }
func (fi fileInfo) Mode() os.FileMode  { return fi.mode }
func (fi fileInfo) ModTime() time.Time { return fi.modTime }
func (fi fileInfo) IsDir() bool        { return fi.mode.IsDir() }
func (fi fileInfo) Sys() any           { return nil }

type vfsDir struct {
	info    fileInfo
	entries []fileInfo
	pos     int
}

func (d *vfsDir) Read([]byte) (int, error)       { return 0, io.EOF }
func (d *vfsDir) Write([]byte) (int, error)      { return 0, errReadOnly }
func (d *vfsDir) Seek(int64, int) (int64, error) { return 0, nil }
func (d *vfsDir) Close() error                   { return nil }
func (d *vfsDir) Stat() (os.FileInfo, error)     { return d.info, nil }
func (d *vfsDir) Readdir(n int) ([]os.FileInfo, error) {
	if d.pos >= len(d.entries) {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}
	if n <= 0 {
		out := make([]os.FileInfo, len(d.entries)-d.pos)
		for i := d.pos; i < len(d.entries); i++ {
			out[i-d.pos] = d.entries[i]
		}
		d.pos = len(d.entries)
		return out, nil
	}
	end := d.pos + n
	if end > len(d.entries) {
		end = len(d.entries)
	}
	out := make([]os.FileInfo, end-d.pos)
	for i := d.pos; i < end; i++ {
		out[i-d.pos] = d.entries[i]
	}
	d.pos = end
	if d.pos >= len(d.entries) {
		return out, io.EOF
	}
	return out, nil
}

type servingFile struct {
	reader io.ReadSeekCloser
	info   fileInfo
}

func (f *servingFile) Read(p []byte) (int, error)         { return f.reader.Read(p) }
func (f *servingFile) Write([]byte) (int, error)          { return 0, errReadOnly }
func (f *servingFile) Seek(o int64, w int) (int64, error) { return f.reader.Seek(o, w) }
func (f *servingFile) Close() error                       { return f.reader.Close() }
func (f *servingFile) Stat() (os.FileInfo, error)         { return f.info, nil }
func (f *servingFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, &os.PathError{Op: "readdir", Path: f.info.name, Err: errors.New("not a directory")}
}

func (fs *torrentFS) resolve(ctx context.Context, name string) (rec *storage.TorrentRecord, subPath string, idx *torrentIndex, err error) {
	idx, err = fs.buildIndex(ctx)
	if err != nil {
		return nil, "", nil, err
	}
	p := cleanPath(name)
	if p == "" {
		return nil, "", idx, nil
	}
	parts := strings.SplitN(p, "/", 2)
	rec = idx.byFolder[parts[0]]
	if rec == nil {
		rec = idx.byAlias[strings.ToLower(parts[0])]
	}
	if rec == nil {
		return nil, "", idx, os.ErrNotExist
	}
	full, err := fs.manager.Get(ctx, rec.ID)
	if err == nil {
		rec = full
		if !torrent.IsCompletedStatus(rec.Status) {
			return nil, "", idx, os.ErrNotExist
		}
	}
	if len(parts) == 1 {
		return rec, "", idx, nil
	}
	return rec, parts[1], idx, nil
}

func (fs *torrentFS) Mkdir(context.Context, string, os.FileMode) error {
	return errReadOnly
}

func (fs *torrentFS) RemoveAll(context.Context, string) error {
	return errReadOnly
}

func (fs *torrentFS) Rename(context.Context, string, string) error {
	return errReadOnly
}

func (fs *torrentFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	p := cleanPath(name)
	if p == "" {
		return fileInfo{name: "/", mode: os.ModeDir | 0o555, modTime: time.Now().UTC()}, nil
	}

	rec, subPath, _, err := fs.resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	modTime := torrentModTime(rec)

	if subPath == "" {
		return fileInfo{name: path.Base(p), mode: os.ModeDir | 0o555, modTime: modTime}, nil
	}

	if f, ok := findFile(rec, subPath); ok {
		return fileInfo{name: path.Base(subPath), size: f.Bytes, mode: 0o444, modTime: modTime}, nil
	}
	if isVirtualDir(rec, subPath) {
		return fileInfo{name: path.Base(subPath), mode: os.ModeDir | 0o555, modTime: modTime}, nil
	}
	return nil, os.ErrNotExist
}

func (fs *torrentFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (xwebdav.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return nil, errReadOnly
	}

	p := cleanPath(name)
	if p == "" {
		idx, err := fs.buildIndex(ctx)
		if err != nil {
			return nil, err
		}
		entries := make([]fileInfo, 0, len(idx.byFolder))
		for folder, rec := range idx.byFolder {
			entries = append(entries, fileInfo{
				name:    folder,
				mode:    os.ModeDir | 0o555,
				modTime: torrentModTime(rec),
			})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
		return &vfsDir{
			info:    fileInfo{name: "/", mode: os.ModeDir | 0o555, modTime: time.Now().UTC()},
			entries: entries,
		}, nil
	}

	rec, subPath, _, err := fs.resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	modTime := torrentModTime(rec)

	if subPath == "" {
		children := listChildren(rec, "")
		entries := make([]fileInfo, len(children))
		copy(entries, children)
		return &vfsDir{
			info:    fileInfo{name: path.Base(p), mode: os.ModeDir | 0o555, modTime: modTime},
			entries: entries,
		}, nil
	}

	if f, ok := findFile(rec, subPath); ok {
		reader, modTime, size, err := fs.manager.OpenServingReader(ctx, rec.ID, f.ID, torrent.StreamOptions{})
		if err != nil {
			return nil, err
		}
		if size <= 0 {
			size = f.Bytes
		}
		if modTime.IsZero() {
			modTime = torrentModTime(rec)
		}
		return &servingFile{
			reader: reader,
			info: fileInfo{
				name:    path.Base(subPath),
				size:    size,
				mode:    0o444,
				modTime: modTime,
			},
		}, nil
	}

	if isVirtualDir(rec, subPath) {
		children := listChildren(rec, subPath)
		entries := make([]fileInfo, len(children))
		copy(entries, children)
		return &vfsDir{
			info:    fileInfo{name: path.Base(subPath), mode: os.ModeDir | 0o555, modTime: modTime},
			entries: entries,
		}, nil
	}

	return nil, os.ErrNotExist
}
