package bridge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kaikozlov/toyota-diagnostics/internal/appenv"
)

func sourceRoot() (string, error) {
	if root := appenv.Root(); root != "" {
		if validRoot(root) {
			return filepath.Abs(root)
		}
		return "", fmt.Errorf("TOYO_ROOT is not a checkout: %s", root)
	}
	var starts []string
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		for dir := start; ; dir = filepath.Dir(dir) {
			if validRoot(dir) {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", fmt.Errorf("toyo-cli checkout not found")
}

func validRoot(root string) bool {
	_, py := os.Stat(filepath.Join(root, "toyota_diag", "__main__.py"))
	_, project := os.Stat(filepath.Join(root, "pyproject.toml"))
	return py == nil && project == nil
}

func python() (string, error) {
	if path, err := exec.LookPath("python3"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("python"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("python runtime not found")
}

func command(args []string) (string, []string, []string, error) {
	root, err := sourceRoot()
	if err != nil {
		return "", nil, nil, err
	}
	path, err := python()
	if err != nil {
		return "", nil, nil, err
	}
	env := os.Environ()
	env = setEnv(env, "TOYO_ROOT", root)
	env = setEnv(env, "TOYOTA_DIAGNOSTICS_ROOT", root)
	pythonPath := root
	if existing := os.Getenv("PYTHONPATH"); existing != "" {
		pythonPath += string(os.PathListSeparator) + existing
	}
	env = setEnv(env, "PYTHONPATH", pythonPath)
	argv := append([]string{path, "-m", "toyota_diag"}, args...)
	return path, argv, env, nil
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
