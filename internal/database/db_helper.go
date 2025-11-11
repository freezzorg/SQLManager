package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/freezzorg/SQLManager/internal/config"
	"github.com/freezzorg/SQLManager/internal/logging"
)

// Структура для хранения логических имен файлов бэкапа (для команды MOVE)
type BackupLogicalFile struct {
	LogicalName string
	Type        string // DATA, LOG
}

// restoreProgress - Структура для отслеживания прогресса восстановления
type RestoreProgress struct {
	TotalFiles    int       `json:"totalFiles"`
	CompletedFiles int       `json:"completedFiles"`
	CurrentFile   string    `json:"currentFile"`
	Percentage    int       `json:"percentage"`
	Status        string    `json:"status"` // "pending", "in_progress", "completed", "failed", "cancelled"
	StartTime     time.Time `json:"startTime"`
	EndTime       time.Time `json:"endTime"`
	Error         string    `json:"error,omitempty"`
	CancelFunc    context.CancelFunc `json:"-"` // Функция для отмены контекста горутины
}

// backupProgress - Структура для отслеживания прогресса создания бэкапа
type BackupProgress struct {
	Percentage    int       `json:"percentage"`
	Status        string    `json:"status"` // "pending", "in_progress", "completed", "failed", "cancelled"
	StartTime     time.Time `json:"startTime"`
	EndTime       time.Time `json:"endTime"`
	Error         string    `json:"error,omitempty"`
	BackupFilePath string   `json:"backupFilePath,omitempty"` // Путь к создаваемому файлу бэкапа
	SessionID     int       `json:"sessionID,omitempty"`      // Session ID процесса BACKUP
}

// Глобальная карта для хранения прогресса восстановления по имени новой БД
var RestoreProgresses = make(map[string]*RestoreProgress)
var RestoreProgressesMutex sync.Mutex

// Глобальная карта для хранения прогресса создания бэкапа по имени БД
var BackupProgresses = make(map[string]*BackupProgress)
var BackupProgressesMutex sync.Mutex

// SetSingleUserMode - Перевод базы данных в однопользовательский режим
func SetSingleUserMode(db *sql.DB, dbName string) error {
	logging.LogDebug(fmt.Sprintf("Попытка перевода базы данных '%s' в однопользовательский режим", dbName))
	query := fmt.Sprintf("ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE", dbName)
	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("ошибка перевода базы данных '%s' в однопользовательский режим: %w", dbName, err)
	}
	logging.LogDebug(fmt.Sprintf("База данных '%s' переведена в однопользовательский режим", dbName))
	return nil
}

// SetMultiUserMode - Перевод базы данных в многопользовательский режим
func SetMultiUserMode(db *sql.DB, dbName string) error {
	query := fmt.Sprintf("ALTER DATABASE [%s] SET MULTI_USER", dbName)
	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("ошибка перевода базы данных '%s' в многопользовательский режим: %w", dbName, err)
	}
	logging.LogDebug(fmt.Sprintf("База данных '%s' переведена в многопользовательский режим", dbName))
	return nil
}

// checkDatabaseExists - Проверяет существование базы данных на сервере
func checkDatabaseExists(db *sql.DB, dbName string) (bool, error) {
	query := fmt.Sprintf("SELECT 1 FROM sys.databases WHERE name = N'%s'", dbName)
	rows, err := db.Query(query)
	if err != nil {
		return false, fmt.Errorf("ошибка при проверке существования базы данных '%s': %w", dbName, err)
	}
	defer rows.Close()

	if rows.Next() {
		return true, nil
	}
	
	return false, nil
}

// GetDatabases - Получение списка пользовательских баз данных
func GetDatabases(db *sql.DB) ([]config.Database, error) {
	query := `
		SELECT
			name,
			state_desc
		FROM
			sys.databases
		WHERE
			database_id > 4 -- Исключение системных баз
			AND name NOT IN ('master', 'model', 'msdb', 'tempdb')
		ORDER BY
			name;
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("ошибка при запросе списка баз: %w", err)
	}
	defer rows.Close()

	var databases []config.Database
	for rows.Next() {
		var dbItem config.Database
		var stateDesc string 
		if err := rows.Scan(&dbItem.Name, &stateDesc); err != nil {
			return nil, fmt.Errorf("ошибка сканирования строки БД: %w", err)
		}
		
		// Преобразуем состояние базы в упрощённый статус
		switch strings.ToUpper(stateDesc) {
		case "ONLINE":
			dbItem.State = "online"
		case "RESTORING", "RECOVERING": 
			dbItem.State = "restoring"
		case "OFFLINE":
			dbItem.State = "offline"
		default:
			dbItem.State = "error" 
		}

		// Дополнительная проверка: если база находится в процессе восстановления через наше приложение
		RestoreProgressesMutex.Lock()
		restoreProgress, restoreExists := RestoreProgresses[dbItem.Name]
		RestoreProgressesMutex.Unlock()

		if restoreExists && (restoreProgress.Status == "pending" || restoreProgress.Status == "in_progress") {
			dbItem.State = "restoring" // Переопределяем статус, если наше приложение активно восстанавливает
		}

		// Дополнительная проверка: если база находится в процессе создания бэкапа через наше приложение
		BackupProgressesMutex.Lock()
		backupProgress, backupExists := BackupProgresses[dbItem.Name]
		BackupProgressesMutex.Unlock()

		if backupExists && (backupProgress.Status == "pending" || backupProgress.Status == "in_progress") {
			dbItem.State = "backing_up" // Переопределяем статус, если наше приложение активно создает бэкап
		}

		databases = append(databases, dbItem)
	}
	
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка после итерации строк БД: %w", err)
	}

	return databases, nil
}

// CheckAndCreateBackupDir - Проверяет существование каталога для бэкапов и создает его, если нет
func checkAndCreateBackupDir(dbName string, smbSharePath string) (string, error) {
    backupDir := filepath.Join(smbSharePath, dbName) // Используем smbSharePath как корневой каталог
    
    if _, err := os.Stat(backupDir); os.IsNotExist(err) {
        if err := os.MkdirAll(backupDir, 0755); err != nil {
            return "", fmt.Errorf("ошибка создания каталога бэкапов '%s': %w", backupDir, err)
        }
    } else if err != nil {
        return "", fmt.Errorf("ошибка проверки каталога бэкапов '%s': %w", backupDir, err)
    }
    
    return backupDir, nil
}
