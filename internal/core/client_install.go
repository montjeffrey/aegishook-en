package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"aegishook/internal/localaddr"
)

// ClientOptions installs files only. It never opens the server DB or starts a listener.
type ClientOptions struct {
	Dir, Binary, Agent, Scope, Project, PiDir, Endpoint, Token string
	EnrollmentCode                                             string
	Progress                                                   io.Writer
	Switch                                                     bool
	Resources                                                  map[string][]byte
}
type clientEntry struct {
	Installation
	CreatedFile bool     `json:"createdFile"`
	CreatedDirs []string `json:"createdDirs,omitempty"`
}
type clientManifest struct {
	Format    int               `json:"format"`
	NodeID    string            `json:"nodeId"`
	Entries   []clientEntry     `json:"entries"`
	Resources map[string]string `json:"resources"`
}
type ClientResult struct {
	Action    string   `json:"action"`
	Agent     string   `json:"agent"`
	Scope     string   `json:"scope"`
	Entry     string   `json:"entry,omitempty"`
	Remaining int      `json:"remaining"`
	Retained  []string `json:"retained,omitempty"`
}
type clientEdit struct {
	path             string
	before, after    []byte
	mode             os.FileMode
	oldLink, newLink string
	existed          bool
}

func ClientOperation(action string, o ClientOptions) (result ClientResult, err error) {
	result = ClientResult{Action: action, Agent: o.Agent, Scope: o.Scope}
	if action != "install" && action != "uninstall" {
		return result, errors.New("the client only supports install / uninstall")
	}
	if AgentName(o.Agent) == "" || (o.Scope != "global" && o.Scope != "project") {
		return result, errors.New("choose a valid agent and a global/project scope")
	}
	if !filepath.IsAbs(o.Dir) {
		return result, errors.New("client directory must be an absolute path")
	}
	o.Dir = filepath.Clean(o.Dir)
	if o.Dir == filepath.VolumeName(o.Dir)+string(filepath.Separator) {
		return result, errors.New("client directory cannot be the filesystem root")
	}
	if _, e := os.Stat(filepath.Join(o.Dir, "aegishook.db")); e == nil {
		return result, errors.New("client directory cannot be the server data directory")
	}
	if o.Scope == "project" {
		if !filepath.IsAbs(o.Project) {
			return result, errors.New("project must use an absolute path")
		}
		original := filepath.Clean(o.Project)
		o.Project, err = filepath.EvalSymlinks(original)
		if err != nil {
			if action != "uninstall" || !os.IsNotExist(err) {
				return result, errors.New("project directory does not exist")
			}
			o.Project = canonicalMissingPath(original)
		}
		st, e := os.Stat(o.Project)
		if !(action == "uninstall" && os.IsNotExist(e)) && (e != nil || !st.IsDir()) {
			return result, errors.New("project must be a directory")
		}
	} else if o.Project != "" {
		return result, errors.New("the global scope cannot specify a project")
	}
	if action == "install" {
		o.Endpoint, err = localaddr.Endpoint(o.Endpoint)
		if err != nil {
			return result, err
		}
		if !filepath.IsAbs(o.Binary) {
			return result, errors.New("hook program must be an absolute path")
		}
	}
	manifestPath := filepath.Join(o.Dir, "client.json")
	if action == "uninstall" {
		if _, e := os.Stat(o.Dir); os.IsNotExist(e) {
			return result, nil
		}
	}
	if err = os.MkdirAll(o.Dir, 0700); err != nil {
		return result, err
	}
	real, e := filepath.EvalSymlinks(o.Dir)
	if e != nil {
		return result, e
	}
	o.Dir = real
	manifestPath = filepath.Join(o.Dir, "client.json")
	lock, err := os.OpenFile(filepath.Join(o.Dir, "client.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if err = lockProcessFile(lock); err != nil {
		return result, errors.New("another client install/uninstall is in progress")
	}
	// Keep the lock inode stable across operations; unlinking a held lock enables races.
	m := clientManifest{Format: 1, NodeID: ID(), Resources: map[string]string{}}
	if b, e := os.ReadFile(manifestPath); e == nil {
		if json.Unmarshal(b, &m) != nil || m.Format != 1 || len(m.NodeID) != 32 {
			return result, errors.New("client install record is invalid; hooks left unchanged")
		}
	} else if !os.IsNotExist(e) {
		return result, e
	}
	if m.Resources == nil {
		m.Resources = map[string]string{}
	}
	if action == "install" {
		if err = prepareClientConnection(&o, &m); err != nil {
			return result, err
		}
		if strings.TrimSpace(o.Token) == "" || strings.ContainsAny(o.Token, "\r\n") {
			return result, errors.New("device credential is empty or malformed")
		}
		if o.Agent == "grok" && !o.Switch {
			for _, v := range m.Entries {
				if v.Agent == o.Agent && (v.Scope != o.Scope || v.Project != o.Project) {
					return result, errors.New("Grok does not support multiple install scopes; use --switch to change")
				}
			}
		}
	}
	current := -1
	for i, v := range m.Entries {
		if v.Agent == o.Agent && v.Scope == o.Scope && v.Project == o.Project {
			current = i
			break
		}
	}
	if action == "uninstall" && current < 0 {
		result.Remaining = len(m.Entries)
		if len(m.Entries) == 0 {
			result.Retained = removeClientPending(o.Dir)
		}
		return result, nil
	}
	target, err := clientTarget(o)
	if err != nil {
		return result, err
	}
	if current >= 0 {
		target = m.Entries[current]
	}
	result.Entry = target.Entry
	if action == "install" {
		// All scopes share one adapter and node identity, so client-side deduplication survives scope switches.
		if b, e := os.ReadFile(filepath.Join(o.Dir, "adapter", "connection.json")); e == nil && len(m.Entries) > 0 {
			var old struct{ Endpoint, Token string }
			if json.Unmarshal(b, &old) != nil || old.Endpoint != o.Endpoint || old.Token != o.Token {
				return result, errors.New("this client is already connected to another address or credential; uninstall its hooks first, then reinstall")
			}
		}
	}
	edits := []clientEdit{}
	next := []clientEntry{}
	removedDirs := []string{}
	for i, v := range m.Entries {
		remove := (action == "uninstall" && i == current) || (action == "install" && o.Switch && v.Agent == o.Agent && (v.Scope != o.Scope || (o.Agent == "grok" && v.Project != o.Project)))
		if remove {
			edit, e := clientHookEdit(v, false)
			if e != nil {
				return result, e
			}
			edits = append(edits, edit)
			removedDirs = append(removedDirs, v.CreatedDirs...)
		} else {
			next = append(next, v)
		}
	}
	if action == "install" {
		edit, e := clientHookEdit(target, true)
		if e != nil {
			return result, e
		}
		if current < 0 {
			target.CreatedFile = !edit.existed
			target.CreatedDirs = missingParents(filepath.Dir(target.Entry))
			next = append(next, target)
		}
		edits = append(edits, edit)
		resourceData := map[string][]byte{}
		for _, name := range []string{"index.ts", "opencode.mjs"} {
			b, ok := o.Resources[name]
			if !ok {
				return result, fmt.Errorf("missing adapter resource %s", name)
			}
			resourceData[name] = b
		}
		resourceData["connection.json"] = mustJSON(map[string]string{"endpoint": o.Endpoint, "token": o.Token, "nodeId": m.NodeID})
		resourceData["runtime.json"] = mustJSON(map[string]string{"binary": o.Binary})
		for name, b := range resourceData {
			p := filepath.Join(o.Dir, "adapter", name)
			edit, e := clientRegularEdit(p, b)
			if e != nil {
				return result, e
			}
			if edit.existed && len(m.Entries) == 0 {
				return result, errors.New("the client directory has an unregistered adapter file; not overwritten")
			}
			if edit.existed && m.Resources[name] != digestBytes(edit.before) {
				return result, fmt.Errorf("adapter file %s was modified; not overwritten", name)
			}
			// Resource changes affect loaded agents; leave existing installations on their current runtime.
			if edit.existed && !bytes.Equal(edit.before, b) {
				return result, errors.New("the existing adapter version or program path differs; uninstall all client hooks first")
			}
			edits = append([]clientEdit{edit}, edits...)
			m.Resources[name] = digestBytes(b)
		}
	}
	m.Entries = next
	manifestData := mustJSON(m)
	if len(next) == 0 {
		manifestData = nil
	}
	manifestEdit, err := clientRegularEdit(manifestPath, manifestData)
	if err != nil {
		return result, err
	}
	edits = append(edits, manifestEdit)
	applied := []clientEdit{}
	createdDirs := []string{}
	for _, edit := range edits {
		createdDirs = append(createdDirs, missingParents(filepath.Dir(edit.path))...)
		if err = applyClientEdit(edit, false); err != nil {
			for j := len(applied) - 1; j >= 0; j-- {
				if e := applyClientEdit(applied[j], true); e != nil {
					err = fmt.Errorf("%w; rolling back %s failed: %v", err, applied[j].path, e)
				}
			}
			removeEmptyDirs(createdDirs)
			return result, err
		}
		applied = append(applied, edit)
	}
	removeEmptyDirs(removedDirs)
	if action == "install" {
		_ = os.Remove(filepath.Join(o.Dir, "enrolled.json"))
	}
	if len(next) == 0 {
		for name, hash := range m.Resources {
			if name != "index.ts" && name != "opencode.mjs" && name != "connection.json" && name != "runtime.json" {
				continue
			}
			p := filepath.Join(o.Dir, "adapter", name)
			b, e := os.ReadFile(p)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil || digestBytes(b) != hash {
				result.Retained = append(result.Retained, p)
				continue
			}
			if e = os.Remove(p); e != nil {
				result.Retained = append(result.Retained, p)
			}
		}
		// Context caches contain local prompts and are owned by the command Hook.
		files, _ := filepath.Glob(filepath.Join(o.Dir, "adapter", "context-*.json"))
		for _, p := range files {
			base := filepath.Base(p)
			id := strings.TrimSuffix(strings.TrimPrefix(base, "context-"), ".json")
			if b, e := hex.DecodeString(id); e == nil && len(b) == 32 {
				if e = os.Remove(p); e != nil {
					result.Retained = append(result.Retained, p)
				}
			}
		}
		_ = os.Remove(filepath.Join(o.Dir, "adapter"))
	}
	result.Remaining = len(next)
	return result, nil
}

func clientTarget(o ClientOptions) (clientEntry, error) {
	base := agentHome(o.Agent)
	if o.Agent == "pi" && o.PiDir != "" {
		base = o.PiDir
	}
	if o.Scope == "project" {
		base = filepath.Join(o.Project, "."+o.Agent)
	}
	if !filepath.IsAbs(base) {
		return clientEntry{}, errors.New("agent config directory must be an absolute path")
	}
	entry := filepath.Join(base, "hooks.json")
	switch o.Agent {
	case "claude":
		entry = filepath.Join(base, "settings.json")
	case "grok":
		entry = filepath.Join(base, "hooks", "aegishook.json")
	case "opencode":
		entry = filepath.Join(base, "plugins", "aegishook.js")
	case "pi":
		entry = filepath.Join(base, "extensions", "aegishook.ts")
	}
	v := clientEntry{Installation: Installation{Agent: o.Agent, Scope: o.Scope, Project: o.Project, Entry: entry, ID: ID(), Installed: true, Version: Version, CreatedAt: time.Now()}}
	v.ManagedCommand = hookCommand(o.Binary, filepath.Join(o.Dir, "adapter", "connection.json"), o.Agent, runtime.GOOS)
	if o.Agent == "opencode" {
		v.ManagedContent = "// AegisHook managed entry\nexport { default } from " + string(mustJSON(fileURL(filepath.Join(o.Dir, "adapter", "opencode.mjs")))) + ";\n"
	}
	if o.Agent == "pi" {
		v.ManagedContent = filepath.Join(o.Dir, "adapter", "index.ts")
	}
	return v, nil
}
func clientHookEdit(v clientEntry, install bool) (clientEdit, error) {
	if v.Agent == "pi" {
		edit := clientEdit{path: v.Entry, mode: 0600}
		link, e := os.Readlink(v.Entry)
		if e == nil {
			if link != v.ManagedContent {
				return edit, errors.New("the Pi entry is not a symlink installed by this client")
			}
			edit.existed = true
			edit.oldLink = link
		} else if _, e = os.Lstat(v.Entry); !os.IsNotExist(e) {
			return edit, errors.New("a Pi entry with the same name exists and does not belong to this client")
		}
		if install {
			edit.newLink = v.ManagedContent
		}
		return edit, nil
	}
	if v.Agent == "opencode" {
		var b []byte
		if install {
			b = []byte(v.ManagedContent)
		}
		edit, e := clientRegularEdit(v.Entry, b)
		if e == nil && edit.existed && string(edit.before) != v.ManagedContent {
			e = errors.New("the OpenCode plugin was modified or does not belong to this client")
		}
		return edit, e
	}
	next, old, mode, e := modifyHookDocument(v.Installation, install)
	if e != nil {
		return clientEdit{}, e
	}
	if !install && v.CreatedFile && strings.TrimSpace(string(next)) == "{}" {
		next = nil
	}
	if !install && old == nil {
		next = nil
	}
	return clientEdit{path: v.Entry, before: old, after: next, mode: mode, existed: old != nil}, nil
}
func clientRegularEdit(p string, b []byte) (clientEdit, error) {
	edit := clientEdit{path: p, after: b, mode: 0600}
	st, e := os.Lstat(p)
	if os.IsNotExist(e) {
		return edit, nil
	}
	if e != nil {
		return edit, e
	}
	if !st.Mode().IsRegular() {
		return edit, fmt.Errorf("%s is not a regular file; not overwritten", p)
	}
	edit.existed = true
	edit.mode = st.Mode().Perm()
	edit.before, e = os.ReadFile(p)
	return edit, e
}
func applyClientEdit(e clientEdit, undo bool) error {
	expected, expectedLink, exists := e.before, e.oldLink, e.existed
	if undo {
		expected, expectedLink, exists = e.after, e.newLink, e.after != nil || e.newLink != ""
	}
	st, statErr := os.Lstat(e.path)
	if !exists {
		if !os.IsNotExist(statErr) {
			return fmt.Errorf("%s changed during the operation; not overwritten", e.path)
		}
	} else {
		if statErr != nil {
			return fmt.Errorf("%s disappeared during the operation; not overwritten", e.path)
		}
		if expectedLink != "" {
			link, err := os.Readlink(e.path)
			if err != nil || link != expectedLink {
				return fmt.Errorf("%s link changed; not overwritten", e.path)
			}
		} else {
			b, err := os.ReadFile(e.path)
			if !st.Mode().IsRegular() || err != nil || !bytes.Equal(b, expected) {
				return fmt.Errorf("%s content changed; not overwritten", e.path)
			}
		}
	}
	data, link := e.after, e.newLink
	if undo {
		data, link = e.before, e.oldLink
	}
	if data == nil && link == "" {
		err := os.Remove(e.path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(e.path), 0700); err != nil {
		return err
	}
	if link != "" {
		if current, err := os.Readlink(e.path); err == nil && current == link {
			return nil
		}
		return os.Symlink(link, e.path)
	}
	return AtomicFile(e.path, data, e.mode)
}
func missingParents(p string) []string {
	out := []string{}
	for {
		if _, e := os.Stat(p); !os.IsNotExist(e) {
			return out
		}
		out = append(out, p)
		parent := filepath.Dir(p)
		if parent == p {
			return out
		}
		p = parent
	}
}
func removeEmptyDirs(dirs []string) {
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, p := range dirs {
		_ = os.Remove(p)
	}
}
func digestBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func canonicalMissingPath(p string) string {
	for parent := filepath.Dir(p); ; parent = filepath.Dir(parent) {
		if real, err := filepath.EvalSymlinks(parent); err == nil {
			relative, _ := filepath.Rel(parent, p)
			return filepath.Join(real, relative)
		}
		if filepath.Dir(parent) == parent {
			return p
		}
	}
}

// An interrupted first installation may have only a pending ticket or enrollment retry record.
func removeClientPending(dir string) []string {
	retained := []string{}
	for _, name := range []string{"request.json", "enrolled.json"} {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		valid := false
		if name == "request.json" {
			var r ClientRequestTicket
			valid = json.Unmarshal(b, &r) == nil && len(r.ID) == 32 && len(r.Secret) == 64
		} else {
			var c ClientConnection
			valid = json.Unmarshal(b, &c) == nil && len(c.NodeID) == 32 && len(c.Token) == 64
		}
		if err != nil || !valid {
			retained = append(retained, p)
			continue
		}
		if err = os.Remove(p); err != nil {
			retained = append(retained, p)
		}
	}
	return retained
}
