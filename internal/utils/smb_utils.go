package utils

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// SMBMountedChecker - Проверяет, что точка монтирования содержит смонтированную SMB-шару
func SMBMountedChecker(path string) (bool, error) {
	// Магические числа для различных типов SMB/CIFS файловых систем
	const (
		SMB_SUPER_MAGIC   = 0x517B    // SMB
		CIFS_SUPER_MAGIC  = 0xFF534D42 // CIFS
		SMB2_SUPER_MAGIC  = 0xFE534D42 // SMB2
	)

	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return false, fmt.Errorf("ошибка syscall.Statfs для %s: %w", path, err)
	}

	// Проверяем тип файловой системы
	fsType := stat.Type
	if fsType == SMB_SUPER_MAGIC || fsType == CIFS_SUPER_MAGIC || fsType == SMB2_SUPER_MAGIC {
		return true, nil
	}
	
	return false, nil
}

// MountSMBShare - Монтирует SMB-шару через systemctl
func MountSMBShare(mountPoint string) error {
	cmd := exec.Command("sudo", "systemctl", "start", "mnt-sql_backups.mount")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ошибка монтирования шары: %v, вывод: %s", err, string(output))
	}
	
	// Проверяем, действительно ли шара смонтирована после монтирования
	isMounted, err := SMBMountedChecker(mountPoint)
	if err != nil || !isMounted {
		return fmt.Errorf("SMB-шара %s по-прежнему не смонтирована после попытки монтирования: %w", mountPoint, err)
	}
	
	return nil
}

// EnsureSMBMounted - Проверяет и монтирует SMB-шару при необходимости
func EnsureSMBMounted(mountPoint string) error {
	// Проверяем, существует ли точка монтирования
	if _, err := os.Stat(mountPoint); os.IsNotExist(err) {
		return fmt.Errorf("точка монтирования %s не существует: %w", mountPoint, err)
	}
	
	// Проверяем, действительно ли шара смонтирована
	isMounted, err := SMBMountedChecker(mountPoint)
	if err != nil {
		// Если не удалось проверить состояние монтирования, все равно пытаемся смонтировать
	} else if isMounted {
		// Шара уже смонтирована, продолжаем
		return nil
	}
	
	// Если шара не смонтирована, пытаемся смонтировать
	return MountSMBShare(mountPoint)
}
