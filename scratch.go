package main

// Scratch chats: agent chats that belong to no project, each in a folder of
// its own: one punk0 makes under <state>/scratch, deleted with the chat, or
// one of the user's, never deleted. A chat is a workspace of kind folder in
// the Scratch project. Its folder never changes once chosen, since agents key
// a session's history on the folder it ran in; what the page shows is Title,
// given in the New chat dialog or taken from the first prompt, and changed by
// renaming. Groups and archiving are labels too: neither moves the folder.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ChatGroup is a named group in Scratch that chats can be filed under.
type ChatGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ChatUpdate changes a chat; a nil field is left as it is.
type ChatUpdate struct {
	Title    *string `json:"title"`
	Group    *string `json:"group"`
	Pinned   *bool   `json:"pinned"`
	Archived *bool   `json:"archived"`
}

// chatTitleMax is how many characters of the first prompt become the title.
const chatTitleMax = 60

// ChatOptions is what the New chat dialog asks for. Agent is a tabAgents key
// or "shell". Title "" leaves the chat to be named by its first prompt. Dir
// "" makes a chat-MMDD-HHMM folder under <state>/scratch.
type ChatOptions struct {
	Title string `json:"title"`
	Group string `json:"group"`
	Agent string `json:"agent"`
	Dir   string `json:"dir"`
}

// NewChat starts a chat on one tab running the chosen agent, in Dir:
//   - under <state>/scratch, a new folder punk0 makes there and deletes with
//     the chat. It must not exist yet, so a chat never takes over another
//     chat's folder or the files of one made before;
//   - anywhere else, a folder of the user's, made when missing and never
//     deleted by punk0.
func (r *Records) NewChat(o ChatOptions) (*Workspace, error) {
	if _, ok := tabAgents[o.Agent]; !ok && o.Agent != tabShell {
		return nil, fmt.Errorf("unknown agent %q", o.Agent)
	}
	title := ""
	if strings.TrimSpace(o.Title) != "" {
		var err error
		if title, err = label(o.Title, 80); err != nil {
			return nil, err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.scratchLocked()
	if o.Group != "" && !slices.ContainsFunc(p.Groups, func(g ChatGroup) bool { return g.ID == o.Group }) {
		return nil, fmt.Errorf("group %q: %w", o.Group, errNotFound)
	}
	now := time.Now()
	dir := o.Dir
	switch {
	case dir == "":
		var err error
		if dir, err = newChatFolder(p.Dir, now); err != nil {
			return nil, err
		}
	case inside(dir, p.Dir):
		// Kept in the scratch folder's own spelling, which is what deleting
		// the chat checks.
		name := filepath.Base(dir)
		if resolvePath(filepath.Dir(dir)) != resolvePath(p.Dir) || !dirName.MatchString(name) {
			return nil, fmt.Errorf("a scratch folder is %s/<name>, the name in letters, digits, '.', '_' and '-'", p.Dir)
		}
		dir = filepath.Join(p.Dir, name)
		if err := os.Mkdir(dir, 0o700); errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("%s already exists: pick another name", dir)
		} else if err != nil {
			return nil, err
		}
	default:
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	w := newWorkspace(filepath.Base(dir), kindFolder, dir, "")
	w.Group, w.Active, w.Created, w.Title = o.Group, now.UnixMilli(), now.UnixMilli(), title
	w.Tabs = append(w.Tabs, newTab(o.Agent, dir, ""))
	r.adoptLocked(w)
	p.Workspaces = append(p.Workspaces, w)
	return w, r.saveLocked()
}

// newChatFolder claims a new chat-MMDD-HHMM folder under scratch, with -2,
// -3... when that one exists.
func newChatFolder(scratch string, now time.Time) (string, error) {
	base := "chat-" + now.Format("0102-1504")
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name += "-" + strconv.Itoa(n)
		}
		dir := filepath.Join(scratch, name)
		// Mkdir, not MkdirAll: an existing folder means the name is taken.
		if err := os.Mkdir(dir, 0o700); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return "", err
		}
		return dir, nil
	}
}

// inside reports whether dir is root or under it, symlinks resolved. dir
// need not exist yet.
func inside(dir, root string) bool {
	dir, root = resolvePath(dir), resolvePath(root)
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

// resolvePath resolves symlinks in p, or in its parent when p does not exist.
func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if r, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(r, filepath.Base(p))
	}
	return p
}

// UpdateChat renames a chat, files it under a group, pins or unpins it, or
// archives or restores it. Archiving stops its tabs; restoring leaves them
// stopped until opened, when a Claude tab resumes its conversation.
func (r *Records) UpdateChat(id string, u ChatUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, i := r.workspaceLocked(id)
	if p == nil {
		return fmt.Errorf("chat %q: %w", id, errNotFound)
	}
	if !p.Scratch {
		return errors.New("only scratch chats can be renamed, grouped or archived")
	}
	w := p.Workspaces[i]
	if u.Title != nil {
		title, err := label(*u.Title, 80)
		if err != nil {
			return err
		}
		w.Title = title
	}
	if u.Group != nil {
		if *u.Group != "" && !slices.ContainsFunc(p.Groups, func(g ChatGroup) bool { return g.ID == *u.Group }) {
			return fmt.Errorf("group %q: %w", *u.Group, errNotFound)
		}
		w.Group = *u.Group
	}
	if u.Pinned != nil {
		w.Pinned = *u.Pinned
	}
	if u.Archived != nil {
		w.Archived = *u.Archived
		if w.Archived {
			closeAll(w.Tabs)
		}
	}
	return r.saveLocked()
}

// AddChatGroup makes an empty group at the end of the list.
func (r *Records) AddChatGroup(name string) (ChatGroup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.scratchLocked()
	name, err := groupName(p, "", name)
	if err != nil {
		return ChatGroup{}, err
	}
	g := ChatGroup{ID: newID(), Name: name}
	p.Groups = append(p.Groups, g)
	return g, r.saveLocked()
}

func (r *Records) RenameChatGroup(id, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.scratchLocked()
	i := slices.IndexFunc(p.Groups, func(g ChatGroup) bool { return g.ID == id })
	if i < 0 {
		return fmt.Errorf("group %q: %w", id, errNotFound)
	}
	name, err := groupName(p, id, name)
	if err != nil {
		return err
	}
	p.Groups[i].Name = name
	return r.saveLocked()
}

// RemoveChatGroup deletes a group; its chats, archived ones included, are
// left in no group.
func (r *Records) RemoveChatGroup(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.scratchLocked()
	i := slices.IndexFunc(p.Groups, func(g ChatGroup) bool { return g.ID == id })
	if i < 0 {
		return fmt.Errorf("group %q: %w", id, errNotFound)
	}
	p.Groups = slices.Delete(p.Groups, i, i+1)
	for _, w := range p.Workspaces {
		if w.Group == id {
			w.Group = ""
		}
	}
	return r.saveLocked()
}

// ChatPrompt records a prompt typed in a tab: a chat moves up the list, and
// one without a title is named after it. Tabs outside Scratch are ignored.
func (r *Records) ChatPrompt(tabID, prompt string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, _ := r.tabLocked(tabID)
	if w == nil || w.Kind != kindFolder {
		return nil
	}
	w.Active = time.Now().UnixMilli()
	if w.Title == "" {
		w.Title = chatTitle(prompt)
	}
	return r.saveLocked()
}

// chatTitle is the first line of a prompt, on one line and cut at a word
// before chatTitleMax characters. A slash command names nothing.
func chatTitle(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if strings.HasPrefix(prompt, "/") {
		return ""
	}
	line, _, _ := strings.Cut(prompt, "\n")
	words := strings.Fields(line)
	var b strings.Builder
	n := 0
	for _, word := range words {
		wn := len([]rune(word))
		if n > 0 && n+1+wn > chatTitleMax {
			b.WriteString("…")
			break
		}
		if n == 0 && wn > chatTitleMax {
			b.WriteString(string([]rune(word)[:chatTitleMax]) + "…")
			break
		}
		if n > 0 {
			b.WriteByte(' ')
			n++
		}
		b.WriteString(word)
		n += wn
	}
	return b.String()
}

// scratchLocked returns the Scratch project, which OpenRecords guarantees.
func (r *Records) scratchLocked() *Project {
	for _, p := range r.projects {
		if p.Scratch {
			return p
		}
	}
	panic("records without Scratch")
}

// groupName checks a group name: a label, unique in Scratch ignoring case,
// apart from the group self being renamed.
func groupName(p *Project, self, name string) (string, error) {
	name, err := label(name, 40)
	if err != nil {
		return "", err
	}
	for _, g := range p.Groups {
		if g.ID != self && strings.EqualFold(g.Name, name) {
			return "", fmt.Errorf("group %q already exists", name)
		}
	}
	return name, nil
}

// label trims a name typed by the user and refuses an empty, overlong or
// control-character one.
func label(s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > max || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("invalid name %q", s)
	}
	return s, nil
}
