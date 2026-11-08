//go:build !windows

package bridge

import "syscall"

func Exec(args []string) error {
	path, argv, env, err := command(args)
	if err != nil {
		return err
	}
	return syscall.Exec(path, argv, env)
}
