package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// IDE is one editor this machine might have. The catalogue is deliberately about
// how to jump to a line, which is the only thing the walkthrough ever asks for.
type IDE struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	Available bool   `json:"available"`
	Hinted    bool   `json:"hinted,omitempty"`
	NoLine    bool   `json:"noLine,omitempty"`
}

type ideDef struct {
	id     string
	name   string
	cmds   []string
	paths  []string
	args   func(file string, line, col int) []string
	scheme func(file string, line, col int) string
	noLine bool
}

func home(rest ...string) string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(append([]string{h}, rest...)...)
}

func env(name string, rest ...string) string {
	v := os.Getenv(name)
	if v == "" {
		return ""
	}
	return filepath.Join(append([]string{v}, rest...)...)
}

// gotoArg is the "file:line:column" form the VS Code family, Zed and Sublime share.
func gotoArg(file string, line, col int) string {
	if line <= 0 {
		return file
	}
	if col <= 0 {
		col = 1
	}
	return file + ":" + strconv.Itoa(line) + ":" + strconv.Itoa(col)
}

func codeArgs(file string, line, col int) []string {
	return []string{"--goto", gotoArg(file, line, col)}
}

func plainArgs(file string, line, col int) []string {
	return []string{gotoArg(file, line, col)}
}

func jetbrainsArgs(file string, line, col int) []string {
	if line <= 0 {
		return []string{file}
	}
	return []string{"--line", strconv.Itoa(line), file}
}

func codeScheme(app string) func(string, int, int) string {
	return func(file string, line, col int) string {
		p := filepath.ToSlash(file)
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if line > 0 {
			if col <= 0 {
				col = 1
			}
			return fmt.Sprintf("%s://file%s:%d:%d", app, p, line, col)
		}
		return app + "://file" + p
	}
}

func ideCatalogue() []ideDef {
	win := runtime.GOOS == "windows"
	mac := runtime.GOOS == "darwin"

	defs := []ideDef{
		{id: "vscode", name: "Visual Studio Code", cmds: []string{"code"}, args: codeArgs, scheme: codeScheme("vscode")},
		{id: "cursor", name: "Cursor", cmds: []string{"cursor"}, args: codeArgs, scheme: codeScheme("cursor")},
		{id: "zed", name: "Zed", cmds: []string{"zed"}, args: plainArgs, scheme: codeScheme("zed")},
		{id: "windsurf", name: "Windsurf", cmds: []string{"windsurf"}, args: codeArgs, scheme: codeScheme("windsurf")},
		{id: "vscodium", name: "VSCodium", cmds: []string{"codium"}, args: codeArgs, scheme: codeScheme("vscodium")},
		{id: "vscode-insiders", name: "VS Code Insiders", cmds: []string{"code-insiders"}, args: codeArgs, scheme: codeScheme("vscode-insiders")},
		{id: "rider", name: "JetBrains Rider", cmds: []string{"rider", "rider64"}, args: jetbrainsArgs},
		{id: "idea", name: "IntelliJ IDEA", cmds: []string{"idea", "idea64"}, args: jetbrainsArgs},
		{id: "webstorm", name: "WebStorm", cmds: []string{"webstorm", "webstorm64"}, args: jetbrainsArgs},
		{id: "goland", name: "GoLand", cmds: []string{"goland", "goland64"}, args: jetbrainsArgs},
		{id: "pycharm", name: "PyCharm", cmds: []string{"pycharm", "charm", "pycharm64"}, args: jetbrainsArgs},
		{id: "phpstorm", name: "PhpStorm", cmds: []string{"phpstorm"}, args: jetbrainsArgs},
		{id: "clion", name: "CLion", cmds: []string{"clion"}, args: jetbrainsArgs},
		{id: "fleet", name: "JetBrains Fleet", cmds: []string{"fleet"}, args: plainArgs},
		{id: "sublime", name: "Sublime Text", cmds: []string{"subl"}, args: plainArgs},
		{id: "helix", name: "Helix", cmds: []string{"hx"}, args: plainArgs},
	}

	if win {
		byID := map[string][]string{
			"vscode": {
				env("LOCALAPPDATA", "Programs", "Microsoft VS Code", "bin", "code.cmd"),
				env("ProgramFiles", "Microsoft VS Code", "bin", "code.cmd"),
			},
			"cursor": {
				env("LOCALAPPDATA", "Programs", "cursor", "resources", "app", "bin", "cursor.cmd"),
			},
			"zed": {
				env("LOCALAPPDATA", "Programs", "Zed", "bin", "zed.exe"),
				env("LOCALAPPDATA", "Programs", "Zed", "Zed.exe"),
			},
			"windsurf": {
				env("LOCALAPPDATA", "Programs", "Windsurf", "bin", "windsurf.cmd"),
			},
			"vscode-insiders": {
				env("LOCALAPPDATA", "Programs", "Microsoft VS Code Insiders", "bin", "code-insiders.cmd"),
			},
			"sublime": {env("ProgramFiles", "Sublime Text", "subl.exe")},
		}
		tb := env("LOCALAPPDATA", "JetBrains", "Toolbox", "scripts")
		for i := range defs {
			if p, ok := byID[defs[i].id]; ok {
				defs[i].paths = append(defs[i].paths, p...)
			}
			if tb != "" && len(defs[i].cmds) > 0 && isJetBrains(defs[i].id) {
				for _, c := range defs[i].cmds {
					defs[i].paths = append(defs[i].paths, filepath.Join(tb, c+".cmd"))
				}
			}
		}
		defs = append(defs, ideDef{
			id: "visualstudio", name: "Visual Studio", cmds: []string{"devenv"}, noLine: true,
			args: func(file string, line, col int) []string { return []string{"/edit", file} },
			paths: []string{
				env("ProgramFiles", "Microsoft Visual Studio", "2022", "Enterprise", "Common7", "IDE", "devenv.exe"),
				env("ProgramFiles", "Microsoft Visual Studio", "2022", "Professional", "Common7", "IDE", "devenv.exe"),
				env("ProgramFiles", "Microsoft Visual Studio", "2022", "Community", "Common7", "IDE", "devenv.exe"),
			},
		})
	}

	if mac {
		byID := map[string][]string{
			"vscode":          {"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"},
			"cursor":          {"/Applications/Cursor.app/Contents/Resources/app/bin/cursor"},
			"zed":             {"/Applications/Zed.app/Contents/MacOS/cli", home("Applications", "Zed.app", "Contents", "MacOS", "cli")},
			"windsurf":        {"/Applications/Windsurf.app/Contents/Resources/app/bin/windsurf"},
			"vscode-insiders": {"/Applications/Visual Studio Code - Insiders.app/Contents/Resources/app/bin/code-insiders"},
			"sublime":         {"/Applications/Sublime Text.app/Contents/SharedSupport/bin/subl"},
		}
		for i := range defs {
			if p, ok := byID[defs[i].id]; ok {
				defs[i].paths = append(defs[i].paths, p...)
			}
			if isJetBrains(defs[i].id) {
				defs[i].paths = append(defs[i].paths, home("Library", "Application Support", "JetBrains", "Toolbox", "scripts", defs[i].cmds[0]))
			}
		}
	}

	if !win && !mac {
		for i := range defs {
			defs[i].paths = append(defs[i].paths,
				filepath.Join("/snap/bin", defs[i].cmds[0]),
				filepath.Join("/usr/local/bin", defs[i].cmds[0]),
				home(".local", "bin", defs[i].cmds[0]),
			)
		}
	}
	return defs
}

func isJetBrains(id string) bool {
	switch id {
	case "rider", "idea", "webstorm", "goland", "pycharm", "phpstorm", "clion", "fleet":
		return true
	}
	return false
}

// resolveIDE finds the executable for one definition, PATH first, then the places
// the installers put things.
func resolveIDE(d ideDef) string {
	for _, c := range d.cmds {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	for _, p := range d.paths {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// envHint reads the terminal this server was started from. Someone running the
// walkthrough from inside their editor almost always wants that same editor.
func envHint() string {
	askpass := os.Getenv("VSCODE_GIT_ASKPASS_NODE") + os.Getenv("VSCODE_GIT_ASKPASS_MAIN")
	low := strings.ToLower(askpass)
	switch {
	case strings.Contains(low, "cursor"):
		return "cursor"
	case strings.Contains(low, "windsurf"):
		return "windsurf"
	case strings.Contains(low, "insiders"):
		return "vscode-insiders"
	case strings.Contains(low, "vscodium"):
		return "vscodium"
	}
	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "vscode":
		return "vscode"
	case "zed":
		return "zed"
	}
	if os.Getenv("ZED_TERM") != "" {
		return "zed"
	}
	return ""
}

func DetectIDEs() []IDE {
	hint := envHint()
	var out []IDE
	for _, d := range ideCatalogue() {
		p := resolveIDE(d)
		out = append(out, IDE{
			ID: d.id, Name: d.name, Path: p,
			Available: p != "", Hinted: d.id == hint && p != "", NoLine: d.noLine,
		})
	}
	out = append(out, IDE{ID: "custom", Name: "Custom command", Available: true})
	return out
}

var autoOrder = []string{
	"vscode", "cursor", "zed", "windsurf", "vscodium", "vscode-insiders",
	"rider", "idea", "webstorm", "goland", "pycharm", "phpstorm", "clion", "fleet",
	"sublime", "helix", "visualstudio",
}

func firstAvailableIDE() string {
	if h := envHint(); h != "" {
		for _, d := range ideCatalogue() {
			if d.id == h && resolveIDE(d) != "" {
				return h
			}
		}
	}
	found := map[string]bool{}
	for _, d := range ideCatalogue() {
		if resolveIDE(d) != "" {
			found[d.id] = true
		}
	}
	for _, id := range autoOrder {
		if found[id] {
			return id
		}
	}
	return "auto"
}

// ---------------------------------------------------------------- opening

type OpenResult struct {
	OK      bool   `json:"ok"`
	IDE     string `json:"ide"`
	Command string `json:"command"`
	Message string `json:"message,omitempty"`
	Fell    bool   `json:"fellBackToUrl,omitempty"`
}

// OpenInIDE jumps the configured editor to a file and line. When nothing is
// configured it hands the job to the operating system through a URL scheme,
// which is worse but still lands the reader in the right place.
func OpenInIDE(s Settings, absFile string, line, col int) OpenResult {
	id := s.IDE
	if id == "" || id == "auto" {
		id = firstAvailableIDE()
	}

	if id == "custom" {
		if strings.TrimSpace(s.IDECommand) == "" {
			return OpenResult{IDE: id, Message: "the IDE is set to custom but ideCommand is empty. Fill it in under Settings"}
		}
		argv := expandTemplate(s.IDECommand, absFile, line, col)
		if len(argv) == 0 {
			return OpenResult{IDE: id, Message: "ideCommand did not parse into a command"}
		}
		if err := launch(argv[0], argv[1:]); err != nil {
			return OpenResult{IDE: id, Command: strings.Join(argv, " "), Message: err.Error()}
		}
		return OpenResult{OK: true, IDE: id, Command: strings.Join(argv, " ")}
	}

	var def *ideDef
	for _, d := range ideCatalogue() {
		if d.id == id {
			dd := d
			def = &dd
			break
		}
	}
	if def == nil {
		return OpenResult{IDE: id, Message: fmt.Sprintf("%q is not an editor this build knows. Pick another under Settings", id)}
	}

	exe := s.IDEPath
	if exe == "" {
		exe = resolveIDE(*def)
	}
	if exe == "" {
		if def.scheme != nil {
			url := def.scheme(absFile, line, col)
			if err := openURL(url); err != nil {
				return OpenResult{IDE: id, Command: url, Message: fmt.Sprintf("%s is not on this machine and the %s:// handler failed: %v", def.name, id, err)}
			}
			return OpenResult{OK: true, IDE: id, Command: url, Fell: true,
				Message: fmt.Sprintf("%s has no command line on PATH, so this went through the %s:// handler", def.name, id)}
		}
		return OpenResult{IDE: id, Message: fmt.Sprintf("%s is not on PATH. Install its shell command, or set idePath under Settings", def.name)}
	}

	args := def.args(absFile, line, col)
	if err := launch(exe, args); err != nil {
		return OpenResult{IDE: id, Command: quoted(exe, args), Message: err.Error()}
	}
	res := OpenResult{OK: true, IDE: id, Command: quoted(exe, args)}
	if def.noLine && line > 0 {
		res.Message = def.name + " has no command line switch for a line number, so it opened the file at the top"
	}
	return res
}

func quoted(exe string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	for _, p := range append([]string{exe}, args...) {
		if strings.ContainsAny(p, " \t") {
			p = `"` + p + `"`
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}

// expandTemplate turns a custom command into argv, splitting on spaces outside
// quotes so a path with a space survives.
func expandTemplate(tpl, file string, line, col int) []string {
	if col <= 0 {
		col = 1
	}
	uri := filepath.ToSlash(file)
	if !strings.HasPrefix(uri, "/") {
		uri = "/" + uri
	}
	rep := strings.NewReplacer(
		"{file}", file,
		"{fileUri}", "file://"+uri,
		"{slashed}", filepath.ToSlash(file),
		"{line}", strconv.Itoa(line),
		"{col}", strconv.Itoa(col),
	)
	var argv []string
	var cur bytes.Buffer
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			argv = append(argv, rep.Replace(cur.String()))
			cur.Reset()
		}
	}
	for _, ch := range tpl {
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				cur.WriteRune(ch)
			}
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == ' ' || ch == '\t':
			flush()
		default:
			cur.WriteRune(ch)
		}
	}
	flush()
	return argv
}

func isBatch(p string) bool {
	l := strings.ToLower(p)
	return strings.HasSuffix(l, ".cmd") || strings.HasSuffix(l, ".bat")
}

// launch starts the editor and gives it a moment to fail. Editors hand off to an
// already running window and exit straight away, so a non-zero exit inside that
// window is a real error worth showing.
func launch(exe string, args []string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" && isBatch(exe) {
		cmd = exec.Command("cmd", append([]string{"/c", exe}, args...)...)
	} else {
		cmd = exec.Command(exe, args...)
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	cmd.Stdout = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			msg := strings.TrimSpace(errBuf.String())
			if msg == "" {
				return err
			}
			return fmt.Errorf("%v: %s", err, firstLine(msg))
		}
		return nil
	case <-time.After(1200 * time.Millisecond):
		return nil
	}
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func openURL(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
