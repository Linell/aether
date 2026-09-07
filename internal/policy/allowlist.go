package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
)

type Rule struct {
	Daemon string   `json:"daemon"`
	Tool   string   `json:"tool"`
	Argv   []string `json:"argv"`
	Path   string   `json:"path"`
}

type Request struct {
	Daemon string
	Tool   string
	Args   json.RawMessage
	Cwd    string
}

type shellArgs struct {
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
}

type pathArgs struct {
	Path string `json:"path"`
}

func LoadRules(path string) ([]Rule, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("policy: read allowlist: %w", err)
	}
	var file struct {
		Rules []Rule `json:"rules"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("policy: parse allowlist: %w", err)
	}
	return file.Rules, nil
}

func Match(rules []Rule, req Request) (Rule, bool) {
	for _, r := range rules {
		if r.matches(req) {
			return r, true
		}
	}
	return Rule{}, false
}

func (r Rule) String() string {
	return strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", r.Daemon, r.Tool, strings.Join(r.Argv, " "), r.Path)), " ")
}

func (r Rule) matches(req Request) bool {
	if r.Tool != req.Tool || (r.Daemon != "" && r.Daemon != req.Daemon) {
		return false
	}
	switch req.Tool {
	case "shell":
		return len(r.Argv) > 0 && matchShell(r.Argv, req)
	case "read_file", "edit_file", "write_file":
		return r.Path != "" && matchPath(r.Path, req)
	}
	return false
}

func matchPath(pattern string, req Request) bool {
	var args pathArgs
	if err := json.Unmarshal(req.Args, &args); err != nil || args.Path == "" {
		return false
	}
	rel, err := RelativeWithin(req.Cwd, args.Path)
	if err != nil {
		return false
	}
	return globMatch(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func globMatch(pattern, segments []string) bool {
	if len(pattern) == 0 {
		return len(segments) == 0
	}
	if pattern[0] == "**" {
		return globMatch(pattern[1:], segments) || (len(segments) > 0 && globMatch(pattern, segments[1:]))
	}
	if len(segments) == 0 {
		return false
	}
	ok, err := path.Match(pattern[0], segments[0])
	return err == nil && ok && globMatch(pattern[1:], segments[1:])
}

func matchShell(patterns []string, req Request) bool {
	var args shellArgs
	if err := json.Unmarshal(req.Args, &args); err != nil || len(args.Argv) == 0 {
		return false
	}
	cwd, err := ResolveWithin(req.Cwd, orDot(args.Cwd))
	if err != nil {
		return false
	}
	return matchArgv(patterns, args.Argv) && pathsWithin(req.Cwd, cwd, args.Argv)
}

func orDot(p string) string {
	if p == "" {
		return "."
	}
	return p
}

func matchArgv(patterns, argv []string) bool {
	for i, p := range patterns {
		if p == "..." {
			return true
		}
		if i >= len(argv) || (p != "*" && p != argv[i]) {
			return false
		}
	}
	return len(patterns) == len(argv)
}

func pathsWithin(root, cwd string, argv []string) bool {
	for _, arg := range argv[1:] {
		if p, ok := pathOf(arg); ok {
			if _, err := ResolveWithin(root, absoluteUnder(cwd, p)); err != nil {
				return false
			}
		}
	}
	return true
}

func pathOf(arg string) (string, bool) {
	if _, v, ok := strings.Cut(arg, "="); ok && strings.HasPrefix(arg, "-") {
		arg = v
	}
	looksLikePath := strings.ContainsRune(arg, '/') || strings.HasPrefix(arg, ".") || strings.HasPrefix(arg, "~")
	return arg, looksLikePath
}
