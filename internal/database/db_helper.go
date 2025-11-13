package database

import (
	"context"
	"database/sql"
	"encoding/json"
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

// CustomTime представляет собой кастомный тип времени для формата "2006-01-02T15:04:05"
type CustomTime struct {
	time.Time
}

// UnmarshalJSON реализует интерфейс json.Unmarshaler для CustomTime
func (ct *CustomTime) UnmarshalJSON(b []byte) error {
	// Убираем кавычки из строки JSON
	s := strings.Trim(string(b), "\"")
	
	// Парсим время в формате "206-01-02T15:04:05"
	t, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
	return fmt.Errorf("не удалось распарсить время '%s': %w", s, err)
	}
	
	ct.Time = t
	return nil
}

// MarshalJSON реализует интерфейс json.Marshaler для CustomTime
func (ct CustomTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(ct.Time.Format("2006-01-02T15:04:05"))
}

// Методы для сравнения времени
func (ct CustomTime) After(t time.Time) bool {
	return ct.Time.After(t)
}

func (ct CustomTime) Before(t time.Time) bool {
	return ct.Time.Before(t)
}

func (ct CustomTime) Equal(t time.Time) bool {
	return ct.Time.Equal(t)
}

// Методы для сравнения CustomTime с CustomTime
func (ct CustomTime) AfterCT(other CustomTime) bool {
	return ct.Time.After(other.Time)
}

func (ct CustomTime) BeforeCT(other CustomTime) bool {
	return ct.Time.Before(other.Time)
}

func (ct CustomTime) EqualCT(other CustomTime) bool {
	return ct.Time.Equal(other.Time)
}

// BackupMetadata представляет структуру метаданных бэкапа
type BackupMetadata struct {
	FileName          string     `json:"FileName"`
	Start             CustomTime `json:"Start"`
	End               CustomTime `json:"End"`
	Type              string     `json:"Type"`
	FirstLSN          string     `json:"FirstLSN"`
	DatabaseBackupLSN string     `json:"DatabaseBackupLSN"`
	CheckpointLSN     string     `json:"CheckpointLSN"`
	LastLSN           string     `json:"LastLSN"`
	IsCopyOnly        bool       `json:"IsCopyOnly"`
}

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

// compareLSN производит лексикографическое сравнение строк (подходит для формата вида 1454000000767000081)
func compareLSN(a, b string) int {
	if a == b {	return 0 }
	if a > b { return 1	}
	return -1
}

// GetBackupLogicalFiles - Получение логических имен файлов из бэкапа (для формирования MOVE)
func GetBackupLogicalFiles(db *sql.DB, backupPath string) ([]BackupLogicalFile, error) {
	query := fmt.Sprintf("RESTORE FILELISTONLY FROM DISK = N'%s'", backupPath)

    rows, err := db.Query(query)
    if err != nil {
        return nil, fmt.Errorf("ошибка при запросе RESTORE FILELISTONLY для файла %s: %w", backupPath, err)
    }
    defer rows.Close()

    columnNames, err := rows.Columns()
    if err != nil {
        return nil, fmt.Errorf("ошибка получения имен столбцов для файла %s: %w", backupPath, err)
    }

    // Ищем индексы нужных столбцов
    var logicalNameIdx, typeIdx int = -1, -1
    for i, name := range columnNames {
        switch name {
        case "LogicalName":
            logicalNameIdx = i
        case "Type":
            typeIdx = i
        }
    }
    if logicalNameIdx == -1 || typeIdx == -1 {
        return nil, fmt.Errorf("не найдены столбцы LogicalName или Type для файла %s", backupPath)
    }

    var logicalFiles []BackupLogicalFile
    for rows.Next() {
        columns := make([]sql.RawBytes, len(columnNames))
        scanArgs := make([]interface{}, len(columnNames))
        for i := range columns {
            scanArgs[i] = &columns[i]
        }

        if err := rows.Scan(scanArgs...); err != nil {
            return nil, fmt.Errorf("ошибка сканирования строки RESTORE FILELISTONLY для файла %s: %w", backupPath, err)
        }

        var logicalName, fileType string
        if logicalNameIdx < len(columns) && len(columns[logicalNameIdx]) > 0 {
            logicalName = string(columns[logicalNameIdx])
        }
        if typeIdx < len(columns) && len(columns[typeIdx]) > 0 {
            fileType = string(columns[typeIdx])
        }

        if logicalName == "" || fileType == "" {
            continue // Пропускаем строки с пустыми значениями
        }

        switch strings.ToUpper(fileType) {
        case "D":
            logicalFiles = append(logicalFiles, BackupLogicalFile{
                LogicalName: logicalName,
                Type:        "DATA",
            })
        case "L":
            logicalFiles = append(logicalFiles, BackupLogicalFile{
                LogicalName: logicalName,
                Type:        "LOG",
            })
        }
    }

    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("ошибка после итерации строк RESTORE FILELISTONLY для файла %s: %w", backupPath, err)
    }

    if len(logicalFiles) == 0 {
        return nil, fmt.Errorf("RESTORE FILELISTONLY не вернул DATA или LOG файлы для файла %s", backupPath)
    }

    return logicalFiles, nil
}

// getBackupHeaderInfo - Получение метаданных бэкапа из файла с помощью RESTORE HEADERONLY
func getBackupHeaderInfo(db *sql.DB, backupFilePath string) (*BackupMetadata, error) {
	logging.LogDebug(fmt.Sprintf("Получение метаданных для файла бэкапа: %s", backupFilePath))
	
	query := fmt.Sprintf("RESTORE HEADERONLY FROM DISK = N'%s'", backupFilePath)
	
	rows, err := db.Query(query)
	if err != nil {
		logging.LogError(fmt.Sprintf("Ошибка при выполнении RESTORE HEADERONLY для файла %s: %v", backupFilePath, err))
		return nil, fmt.Errorf("ошибка при выполнении RESTORE HEADERONLY для файла %s: %w", backupFilePath, err)
	}
	defer rows.Close()

	// Получаем количество столбцов
	columns, err := rows.Columns()
	if err != nil {
		logging.LogError(fmt.Sprintf("Ошибка получения имен столбцов для файла %s: %v", backupFilePath, err))
		return nil, err
	}
	
	logging.LogDebug(fmt.Sprintf("Количество столбцов в RESTORE HEADERONLY: %d", len(columns)))

	// Ищем индексы нужных столбцов
	var backupTypeIdx, backupStartDateIdx, backupFinishDateIdx, firstLSNIdx, 
		lastLSNIdx, checkpointLSNIdx, databaseBackupLSNIdx, isCopyOnlyIdx int = -1, -1, -1, -1, -1, -1, -1, -1
	
	for i, name := range columns {
		switch name {
		case "BackupType":
			backupTypeIdx = i
		case "BackupStartDate":
			backupStartDateIdx = i
		case "BackupFinishDate":
			backupFinishDateIdx = i
		case "FirstLSN":
			firstLSNIdx = i
		case "LastLSN":
			lastLSNIdx = i
		case "CheckpointLSN":
			checkpointLSNIdx = i
		case "DatabaseBackupLSN":
			databaseBackupLSNIdx = i
		case "IsCopyOnly":
			isCopyOnlyIdx = i
		}
	}
	
	if backupTypeIdx == -1 || backupStartDateIdx == -1 || backupFinishDateIdx == -1 || 
		firstLSNIdx == -1 || lastLSNIdx == -1 || checkpointLSNIdx == -1 || 
		databaseBackupLSNIdx == -1 || isCopyOnlyIdx == -1 {
		logging.LogError("Не найдены необходимые столбцы в результатах RESTORE HEADERONLY")
		return nil, fmt.Errorf("не найдены необходимые столбцы в результатах RESTORE HEADERONLY")
	}

	if rows.Next() {
		// Создаем срез для хранения всех значений
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		
		// Инициализируем все элементы с указателями на переменные
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		
		// Сканируем все столбцы
		if err := rows.Scan(valuePtrs...); err != nil {
			logging.LogError(fmt.Sprintf("Ошибка сканирования строк для %s: %v", backupFilePath, err))
			return nil, err
		}
		
		// Извлекаем нужные значения из соответствующих индексов
		var backupType int
		var backupStartDate, backupFinishDate time.Time
		var firstLSN, lastLSN, checkpointLSN, databaseBackupLSN string
		var isCopyOnly bool

		// Обработка backupType
		if values[backupTypeIdx] != nil {
			switch v := values[backupTypeIdx].(type) {
			case int32:
				backupType = int(v)
			case int64:
				backupType = int(v)
			case int:
				backupType = v
			case *int32:
				if v != nil {
					backupType = int(*v)
				}
			case *int64:
				if v != nil {
					backupType = int(*v)
				}
			case *int:
				if v != nil {
					backupType = *v
				}
			case []uint8:
				fmt.Sscanf(string(v), "%d", &backupType)
			case string:
				fmt.Sscanf(v, "%d", &backupType)
			default:
				logging.LogError(fmt.Sprintf("Неожиданный тип для BackupType: %T, значение: %v", values[backupTypeIdx], values[backupTypeIdx]))
			}
		}

		// Обработка backupStartDate
		if values[backupStartDateIdx] != nil {
			switch v := values[backupStartDateIdx].(type) {
			case time.Time:
				backupStartDate = v
			case *time.Time:
				if v != nil {
					backupStartDate = *v
				}
			case string:
				backupStartDate, _ = time.Parse("2006-01-02 15:04:05", v)
			case []uint8:
				timeStr := string(v)
				backupStartDate, _ = time.Parse("2006-01-02 15:04:05", timeStr)
			default:
				logging.LogError(fmt.Sprintf("Неожиданный тип для BackupStartDate: %T, значение: %v", values[backupStartDateIdx], values[backupStartDateIdx]))
			}
		}

		// Обработка backupFinishDate
		if values[backupFinishDateIdx] != nil {
			switch v := values[backupFinishDateIdx].(type) {
			case time.Time:
				backupFinishDate = v
			case *time.Time:
				if v != nil {
					backupFinishDate = *v
				}
			case string:
				backupFinishDate, _ = time.Parse("2006-01-02 15:04:05", v)
			case []uint8:
				timeStr := string(v)
				backupFinishDate, _ = time.Parse("2006-01-02 15:04:05", timeStr)
			default:
				logging.LogError(fmt.Sprintf("Неожиданный тип для BackupFinishDate: %T, значение: %v", values[backupFinishDateIdx], values[backupFinishDateIdx]))
			}
		}

		// Обработка firstLSN
		if values[firstLSNIdx] != nil {
			switch v := values[firstLSNIdx].(type) {
			case string:
				firstLSN = v
			case *string:
				if v != nil {
					firstLSN = *v
				}
			case []uint8:
				firstLSN = string(v)
			default:
				logging.LogError(fmt.Sprintf("Неожиданный тип для FirstLSN: %T, значение: %v", values[firstLSNIdx], values[firstLSNIdx]))
			}
		}

		// Обработка lastLSN
		if values[lastLSNIdx] != nil {
			switch v := values[lastLSNIdx].(type) {
			case string:
				lastLSN = v
			case *string:
				if v != nil {
					lastLSN = *v
				}
			case []uint8:
				lastLSN = string(v)
			default:
				logging.LogError(fmt.Sprintf("Неожиданный тип для LastLSN: %T, значение: %v", values[lastLSNIdx], values[lastLSNIdx]))
			}
		}

		// Обработка checkpointLSN
		if values[checkpointLSNIdx] != nil {
			switch v := values[checkpointLSNIdx].(type) {
			case string:
				checkpointLSN = v
			case *string:
				if v != nil {
					checkpointLSN = *v
				}
			case []uint8:
				checkpointLSN = string(v)
			default:
				logging.LogError(fmt.Sprintf("Неожиданный тип для CheckpointLSN: %T, значение: %v", values[checkpointLSNIdx], values[checkpointLSNIdx]))
			}
		}

		// Обработка databaseBackupLSN
		if values[databaseBackupLSNIdx] != nil {
			switch v := values[databaseBackupLSNIdx].(type) {
			case string:
				databaseBackupLSN = v
			case *string:
				if v != nil {
					databaseBackupLSN = *v
				}
			case []uint8:
				databaseBackupLSN = string(v)
			default:
				logging.LogError(fmt.Sprintf("Неожиданный тип для DatabaseBackupLSN: %T, значение: %v", values[databaseBackupLSNIdx], values[databaseBackupLSNIdx]))
			}
		}

		// Обработка isCopyOnly
		if values[isCopyOnlyIdx] != nil {
			switch v := values[isCopyOnlyIdx].(type) {
			case bool:
				isCopyOnly = v
			case *bool:
				if v != nil {
					isCopyOnly = *v
				}
			case int32:
				isCopyOnly = v != 0
			case int64:
				isCopyOnly = v != 0
			case int:
				isCopyOnly = v != 0
			case []uint8:
				var val int
				fmt.Sscanf(string(v), "%d", &val)
				isCopyOnly = val != 0
			default:
				logging.LogError(fmt.Sprintf("Неожиданный тип для IsCopyOnly: %T, значение: %v", values[isCopyOnlyIdx], values[isCopyOnlyIdx]))
			}
		}
		
		logging.LogDebug(fmt.Sprintf("Тип бэкапа: %d, Start: %v, End: %v", backupType, backupStartDate, backupFinishDate))

		// Определяем тип бэкапа
		var backupTypeStr string
		switch backupType {
		case 1:
			backupTypeStr = "Database"
		case 5:
			backupTypeStr = "Database Differential"
		case 2:
			backupTypeStr = "Transaction Log"
		default:
			backupTypeStr = fmt.Sprintf("Unknown (%d)", backupType)
		}

		// Создаем и возвращаем структуру BackupMetadata
		metadata := &BackupMetadata{
			FileName:          filepath.Base(backupFilePath),
			Start:             CustomTime{backupStartDate},
			End:               CustomTime{backupFinishDate},
			Type:              backupTypeStr,
			FirstLSN:          firstLSN,
			DatabaseBackupLSN: databaseBackupLSN,
			CheckpointLSN:     checkpointLSN,
			LastLSN:           lastLSN,
			IsCopyOnly:        isCopyOnly,
		}
		
		logging.LogDebug(fmt.Sprintf("Метаданные успешно получены для файла: %s", metadata.FileName))
		return metadata, nil
	} else {
		logging.LogError(fmt.Sprintf("Запрос RESTORE HEADERONLY для %s не вернул строк.", backupFilePath))
		return nil, fmt.Errorf("не найдено метаданных для файла бэкапа %s", backupFilePath)
	}
}

// getAllBackupFiles - Получает список всех файлов бэкапов в каталоге
func getAllBackupFiles(backupDir string) ([]string, error) {
	var backupFiles []string

	err := filepath.Walk(backupDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			// Проверяем расширение файла
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".bak" || ext == ".trn" || ext == ".diff" {
				// Добавляем только имя файла, не полный путь
				backupFiles = append(backupFiles, info.Name())
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("ошибка обхода каталога %s: %w", backupDir, err)
	}

	return backupFiles, nil
}
