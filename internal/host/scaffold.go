package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Runner func(ctx context.Context, dir string, args ...string) error

func Scaffold(ctx context.Context, dir, name, sdk string, run Runner) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("host: scaffold %s: %w", name, err)
	}
	for file, body := range scaffoldFiles(name, sdk) {
		if err := writeIfMissing(filepath.Join(dir, file), body); err != nil {
			return err
		}
	}
	if err := run(ctx, dir, "bun", "install"); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	return run(ctx, dir, "git", "init", "-q")
}

func scaffoldFiles(name, sdk string) map[string]string {
	return map[string]string{
		"aether.json":  jsonDoc(Manifest{Name: name, Run: "bun run start"}),
		"package.json": jsonDoc(packageJSON(name, sdk)),
		"index.ts":     indexTS(name),
		".gitignore":   "node_modules\n",
	}
}

func packageJSON(name, sdk string) map[string]any {
	return map[string]any{
		"name":         name,
		"private":      true,
		"type":         "module",
		"scripts":      map[string]string{"start": "bun run index.ts"},
		"dependencies": map[string]string{"@aether/daemon": sdk},
	}
}

func indexTS(name string) string {
	return fmt.Sprintf("import { defineDaemon, start } from \"@aether/daemon\";\nconst daemon = defineDaemon({ name: %q });\nawait start(daemon);\n", name)
}

func jsonDoc(v any) string {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(body) + "\n"
}

func writeIfMissing(path, body string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("host: write %s: %w", path, err)
	}
	defer f.Close()
	_, err = f.WriteString(body)
	return err
}

func Exec(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = BaseEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("host: %v in %s: %w: %s", args, dir, err, out)
	}
	return nil
}
