package loom

import "golang.org/x/sys/unix"

// dup2 через dup3: на части linux-архитектур (arm64, riscv64) syscall dup2
// отсутствует.
func dup2(oldfd, newfd int) error {
	return unix.Dup3(oldfd, newfd, 0)
}
