package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/freezzorg/SQLManager/internal/logging"
	"github.com/freezzorg/SQLManager/internal/utils"
)

// StartBackup - Запускает асинхронный процесс создания полного бэкапа базы данных
func StartBackup(db *sql.DB, dbName string, smbSharePath string) error {
	// Переводим базу в однопользовательский режим перед созданием бэкапа
	if err := SetSingleUserMode(db, dbName); err != nil {
		return fmt.Errorf("ошибка перевода базы '%s' в однопользовательский режим перед бэкапом: %w", dbName, err)
	}
	
	BackupProgressesMutex.Lock()
	BackupProgresses[dbName] = &BackupProgress{
		Status:    "pending",
		StartTime: time.Now(),
	}
	BackupProgressesMutex.Unlock()

	logging.LogWebInfo(fmt.Sprintf("Начато создание бэкапа базы '%s'...", dbName))

	go func() {
		defer func() {
			// Всегда пытаемся перевести базу в многопользовательский режим после завершения бэкапа
			if err := SetMultiUserMode(db, dbName); err != nil {
				logging.LogError(fmt.Sprintf("Ошибка перевода базы '%s' в многопользовательский режим после бэкапа: %v", dbName, err))
			}
		}()
		
		// 1. Проверяем и монтируем SMB-шару при необходимости
		if err := utils.EnsureSMBMounted(smbSharePath); err != nil {
			logging.LogError(fmt.Sprintf("Не удалось смонтировать SMB-шару %s: %v", smbSharePath, err))
			BackupProgressesMutex.Lock()
			if progress := BackupProgresses[dbName]; progress != nil {
				progress.Status = "failed"
				progress.Error = err.Error()
				progress.EndTime = time.Now()
			}
			BackupProgressesMutex.Unlock()
			return
		}

		// 2. Проверяем и создаем каталог для бэкапов
		backupDir, err := checkAndCreateBackupDir(dbName, smbSharePath)
		if err != nil {
			logging.LogError(fmt.Sprintf("Ошибка проверки/создания каталога бэкапов для базы '%s': %v", dbName, err))
			BackupProgressesMutex.Lock()
			if progress := BackupProgresses[dbName]; progress != nil {
				progress.Status = "failed"
				progress.Error = err.Error()
				progress.EndTime = time.Now()
			}
			BackupProgressesMutex.Unlock()
			return
		}

		// Формируем имя файла бэкапа: имя_базы_ГГГГММДД_ЧЧММСС.bak
		backupFileName := fmt.Sprintf("%s_%s.bak", dbName, time.Now().Format("20060102_150405"))
		backupFilePath := filepath.Join(backupDir, backupFileName)

		logging.LogDebug(fmt.Sprintf("Путь к файлу бэкапа для базы '%s': %s", dbName, backupFilePath))

		BackupProgressesMutex.Lock()
		if progress := BackupProgresses[dbName]; progress != nil {
			progress.Status = "in_progress"
			progress.BackupFilePath = backupFilePath
		}
		BackupProgressesMutex.Unlock()

		// 2. Выполняем команду BACKUP DATABASE
		backupQuery := fmt.Sprintf("BACKUP DATABASE [%s] TO DISK = N'%s' WITH INIT", dbName, backupFilePath)

		logging.LogDebug(fmt.Sprintf("Выполнение BACKUP DATABASE: %s", backupQuery))

		_, err = db.Exec(backupQuery)
		
		if err != nil {
			logging.LogError(fmt.Sprintf("Ошибка создания бэкапа базы '%s': %v", dbName, err))
			BackupProgressesMutex.Lock()
			if progress := BackupProgresses[dbName]; progress != nil {
				progress.Status = "failed"
				progress.Error = err.Error()
				progress.EndTime = time.Now()
			}
			BackupProgressesMutex.Unlock()
			return
		}

		logging.LogWebInfo(fmt.Sprintf("Создание бэкапа базы '%s' успешно завершено", dbName))
		BackupProgressesMutex.Lock()
		if progress := BackupProgresses[dbName]; progress != nil {
			progress.Percentage = 100
			progress.Status = "completed"
			progress.EndTime = time.Now()
		}
		BackupProgressesMutex.Unlock()

	}()

	return nil
}

// GetBackupProgress - Возвращает текущий прогресс создания бэкапа для указанной БД
func GetBackupProgress(db *sql.DB, dbName string) *BackupProgress {
	BackupProgressesMutex.Lock()
	defer BackupProgressesMutex.Unlock()

	progress, exists := BackupProgresses[dbName]
	if !exists {
		return nil
	}

	// Если бэкап еще не завершен, пытаемся получить процент выполнения из sys.dm_exec_requests
	if progress.Status == "in_progress" {
		query := `
			SELECT r.percent_complete, r.session_id, t.text
			FROM sys.dm_exec_requests r
			CROSS APPLY sys.dm_exec_sql_text(r.sql_handle) t
			WHERE r.command LIKE '%BACKUP%';
		`
		rows, err := db.Query(query)
		if err != nil {
			return progress
		}
		defer rows.Close()

		for rows.Next() {
			var percentComplete float64
			var sessionID int
			var commandText sql.NullString
			if err := rows.Scan(&percentComplete, &sessionID, &commandText); err != nil {
				continue
			}

			// Проверяем, содержит ли текст команды имя целевой базы данных
			if commandText.Valid && strings.Contains(commandText.String, fmt.Sprintf("DATABASE [%s]", dbName)) {
				progress.Percentage = int(percentComplete)
				progress.SessionID = sessionID
				break
			}
		}
	}

	return progress
}
