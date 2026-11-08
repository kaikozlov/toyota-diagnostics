//go:build windows

package bridge

import (
	"os"
	"os/exec"
)

func Exec(args []string) error {
	path, argv, env, err := command(args)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
