package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/freezzorg/SQLManager/internal/logging"
	"github.com/freezzorg/SQLManager/internal/utils"
)

// BackupMetadata представляет структуру данных из backup_metadate.json
type BackupMetadata struct {
	FileName          string     `json:"FileName"`
	Start             CustomTime `json:"Start"`
	End               CustomTime `json:"End"`
	Type              string     `json:"Type"` // Database, Database Differential, Log
	FirstLSN          string     `json:"FirstLSN"`
	DatabaseBackupLSN string     `json:"DatabaseBackupLSN"`
	CheckpointLSN     string     `json:"CheckpointLSN"`
	LastLSN           string     `json:"LastLSN"`
	IsCopyOnly        bool       `json:"IsCopyOnly"`
}

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

// compareLSN производит лексикографическое сравнение строк (подходит для формата вида 1454000000767000081)
func compareLSN(a, b string) int {
	if a == b {	return 0 }
	if a > b { return 1	}
	return -1
}

// GetRestoreSequence - Определяет последовательность бэкапов для восстановления на указанный момент времени
func GetRestoreSequence(db *sql.DB, baseName string, restoreTime *time.Time, smbSharePath string) ([]BackupMetadata, error) {
	// 1. Проверяем и монтируем SMB-шару при необходимости
	if err := utils.EnsureSMBMounted(smbSharePath); err != nil {
		return nil, fmt.Errorf("не удалось смонтировать SMB-шару %s: %w", smbSharePath, err)
	}
	
	// Формируем путь к директории бэкапов
	backupDir := filepath.Join(smbSharePath, baseName)
	
	// Проверяем существование файла метаданных
	metadataPath := filepath.Join(backupDir, "backup_metadata.json")
	
	// Проверяем существование файла метаданных
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("файл метаданных %s не найден", metadataPath)
	}
	
	// Читаем файл метаданных
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения файла метаданных %s: %w", metadataPath, err)
	}
	
	// Парсим JSON
	var allHeaders []BackupMetadata
	if err := json.Unmarshal(data, &allHeaders); err != nil {
		return nil, fmt.Errorf("ошибка парсинга файла метаданных %s: %w", metadataPath, err)
	}

	// Строим цепочку восстановления
	// Имя базы данных в BackupMetadata не содержится, но мы фильтруем по baseName вызывающем коде
	backups := make([]BackupMetadata, len(allHeaders))
	copy(backups, allHeaders)
	
	if len(backups) == 0 {
		return nil, fmt.Errorf("не найдено бэкапов для базы данных: %s", baseName)
	}

	// Отфильтруем бэкапы, которые завершились до targetTime
	var validBackups []BackupMetadata
	for _, b := range backups {
		if restoreTime == nil || b.End.Before(*restoreTime) || b.End.Equal(*restoreTime) {
			validBackups = append(validBackups, b)
		}
	}
	if len(validBackups) == 0 {
		return nil, fmt.Errorf("нет бэкапов до указанного времени")
	}

	// В начале ищем последний полный бэкап (Database, IsCopyOnly == false) перед targetTime с максимальным End
	var fullBackups []BackupMetadata
	for _, b := range validBackups {
		if b.Type == "Database" && !b.IsCopyOnly {
			fullBackups = append(fullBackups, b)
		}
	}
	if len(fullBackups) == 0 {
		return nil, fmt.Errorf("нет полного бэкапа для базы")
	}
	sort.Slice(fullBackups, func(i, j int) bool {
		return fullBackups[i].End.AfterCT(fullBackups[j].End)
	})
	fullBackup := fullBackups[0]
	restoreChain := []BackupMetadata{fullBackup}

	// Далее добавляем дифференциальный бэкап, если есть, сделанный после полного и до targetTime
	var diffBackups []BackupMetadata
	for _, b := range validBackups {
		if b.Type == "Database Differential" && !b.IsCopyOnly && compareLSN(b.DatabaseBackupLSN, fullBackup.FirstLSN) == 0 {
			if b.End.AfterCT(fullBackup.End) && (restoreTime == nil || b.End.Before(*restoreTime) || b.End.Equal(*restoreTime)) {
				diffBackups = append(diffBackups, b)
			}
		}
	}
	if len(diffBackups) > 0 {
		sort.Slice(diffBackups, func(i, j int) bool {
			return diffBackups[i].End.AfterCT(diffBackups[j].End)
		})
		diffBackup := diffBackups[0]
		restoreChain = append(restoreChain, diffBackup)
	}

	// Построение цепочки журнальных бэкапов
	lastBackup := restoreChain[len(restoreChain)-1]
	var logBackups []BackupMetadata
	for _, b := range validBackups {
		if b.Type == "Transaction Log" && !b.IsCopyOnly {
			// Проверяем, что транзакционный лог основан на том же полном бэкапе, что и цепочка
			if compareLSN(b.DatabaseBackupLSN, fullBackup.FirstLSN) == 0 {
				// Также проверяем, что транзакционный лог был создан после последнего бэкапа в цепочке
				if b.Start.AfterCT(lastBackup.End) || b.Start.EqualCT(lastBackup.End) {
					logBackups = append(logBackups, b)
				}
			}
		}
	}

	// Сортируем журнальные бэкапы по FirstLSN
	sort.Slice(logBackups, func(i, j int) bool {
		return compareLSN(logBackups[i].FirstLSN, logBackups[j].FirstLSN) < 0
	})

	// Находим первый журнальный бэкап, который может быть использован для продолжения цепочки
	// FirstLSN журнала может быть <= LastLSN предыдущего бэкапа, но при этом лог должен быть создан после предыдущего бэкапа
	var firstLog *BackupMetadata
	for i, log := range logBackups {
		// Проверяем, что FirstLSN лога <= LastLSN предыдущего бэкапа
		if compareLSN(log.FirstLSN, lastBackup.LastLSN) <= 0 {
			firstLog = &logBackups[i]
			restoreChain = append(restoreChain, log)
			break
		}
	}

	// Если первый транзакционный лог не найден, но они есть, проверяем возможность восстановления
	if firstLog == nil && len(logBackups) > 0 {
		// Если все транзакционные логи начинаются после LastLSN предыдущего бэкапа, восстановление невозможно
		// Но если восстанавливаем на момент времени окончания предыдущего бэкапа, то можно обойтись без транзакционных логов
		if restoreTime != nil && lastBackup.End.Before(*restoreTime) {
			return nil, fmt.Errorf("не найден подходящий транзакционный лог для продолжения цепочки восстановления")
	}
	}

	// Добавляем остальные журнальные бэкапы, если они есть, с учетом непрерывности цепочки
	// После добавления первого журнального бэкапа, остальные должны следовать последовательно
	if firstLog != nil {
		currentLSN := firstLog.LastLSN
	for _, log := range logBackups {
			// Пропускаем уже добавленный бэкап
			if log.FileName == firstLog.FileName {
				continue
			}
			// Для остальных транзакционных логов: FirstLSN == LastLSN предыдущего
			if compareLSN(log.FirstLSN, currentLSN) == 0 {
				restoreChain = append(restoreChain, log)
				currentLSN = log.LastLSN
			}
		}
	}

	// Проверяем непрерывность цепочки по LSN
	// Только для транзакционных логов: первый может начинаться до LastLSN предыдущего бэкапа, но остальные должны точно стыковаться
	for i := 1; i < len(restoreChain); i++ {
		prev := restoreChain[i-1]
		curr := restoreChain[i]
		if curr.Type == "Transaction Log" && prev.Type == "Transaction Log" {
			// Только между транзакционными логами: FirstLSN == LastLSN предыдущего
			if compareLSN(curr.FirstLSN, prev.LastLSN) != 0 {
				return nil, fmt.Errorf("порвана цепочка: журнал %s (FirstLSN=%s) не стыкуется с предыдущим %s (LastLSN=%s)", 
					curr.FileName, curr.FirstLSN, prev.FileName, prev.LastLSN)
			}
		}
	}

	// Логируем имена файлов в цепочке
	var chainFileNames []string
	for _, backup := range restoreChain {
		chainFileNames = append(chainFileNames, backup.FileName)
	}
	logging.LogDebug(fmt.Sprintf("Цепочка восстановления для базы %s: %v", baseName, chainFileNames))

	return restoreChain, nil
}

// StartRestore - Запускает асинхронный процесс восстановления базы данных
func StartRestore(db *sql.DB, backupBaseName, newDBName string, restoreTime *time.Time, smbSharePath, restorePath string) error {
	// Проверяем, существует ли база данных на сервере
	dbExists, err := checkDatabaseExists(db, newDBName)
	if err != nil {
		return fmt.Errorf("ошибка проверки существования базы данных '%s': %w", newDBName, err)
	}
	
	// Если база существует, переводим её в однопользовательский режим перед восстановлением
	if dbExists {
		if err := SetSingleUserMode(db, newDBName); err != nil {
			return fmt.Errorf("ошибка перевода базы '%s' в однопользовательский режим перед восстановлением: %w", newDBName, err)
		}
	}
	
	// Инициализация прогресса восстановления
	// Создаем контекст для отмены операции восстановления
	ctx, cancel := context.WithCancel(context.Background())

	RestoreProgressesMutex.Lock()
	RestoreProgresses[newDBName] = &RestoreProgress{
		Status:      "pending",
		StartTime:   time.Now(),
		TotalFiles:  0, // Будет обновлено после получения filesToRestore
		CurrentFile: "Инициализация...",
		CancelFunc:  cancel, // Сохраняем функцию отмены
	}
	RestoreProgressesMutex.Unlock()

	// Используем горутину, чтобы не блокировать обработчик HTTP-запросов
	go func(ctx context.Context, cancel context.CancelFunc) { // Передаем контекст и функцию отмены
		defer cancel() // Гарантируем вызов cancel при завершении горутины

		logging.LogWebInfo(fmt.Sprintf("Начато асинхронное восстановление базы '%s' из бэкапа '%s'.", newDBName, backupBaseName))
		if restoreTime != nil {
			logging.LogDebug(fmt.Sprintf("Желаемое время восстановления (PIRT): %s", restoreTime.Format("2006-01-02 15:04:05")))
		}

		// Обновляем статус на "in_progress"
		RestoreProgressesMutex.Lock()
		progress := RestoreProgresses[newDBName]
		if progress != nil {
			progress.Status = "in_progress"
		}
		RestoreProgressesMutex.Unlock()

		// 1. Получение последовательности бэкапов
		filesToRestore, err := GetRestoreSequence(db, backupBaseName, restoreTime, smbSharePath)
		if err != nil {
			logging.LogError(fmt.Sprintf("Ошибка получения последовательности бэкапов для %s: %v", backupBaseName, err))
			RestoreProgressesMutex.Lock()
			if progress != nil {
				progress.Status = "failed"
				progress.Error = err.Error()
				progress.EndTime = time.Now()
			}
			RestoreProgressesMutex.Unlock()
			return
		}

		// Обновляем общее количество файлов
		RestoreProgressesMutex.Lock()
		if progress != nil {
			progress.TotalFiles = len(filesToRestore)
		}
		RestoreProgressesMutex.Unlock()

		// 2. Определение первого файла и логических имен
		// Используем FileName из BackupMetadata
		startFile := filesToRestore[0]
		
		// Получаем логические имена файлов из первого файла в цепочке (startFile)
		backupFilePath := filepath.Join(smbSharePath, backupBaseName, startFile.FileName)
		logicalFiles, err := GetBackupLogicalFiles(db, backupFilePath)
		if err != nil {
			logging.LogError(fmt.Sprintf("Ошибка получения логических имен файлов бэкапа для %s: %v", backupBaseName, err))
			RestoreProgressesMutex.Lock()
			if progress != nil {
				progress.Status = "failed"
				progress.Error = err.Error()
				progress.EndTime = time.Now()
			}
			RestoreProgressesMutex.Unlock()
			return
		}
		logging.LogDebug(fmt.Sprintf("Успешно получены логические имена файлов из бэкапа: %+v", logicalFiles))

		// 3. Формируем MOVE-часть команды RESTORE
		var moveClause string
		var moveParts []string
		
		for _, logicalFile := range logicalFiles {
			var ext string
			var physicalFileName string 

			switch logicalFile.Type {
			case "DATA":
				ext = ".mdf"
				// Формируем имя файла как NewDBName.mdf
				physicalFileName = fmt.Sprintf("%s%s", newDBName, ext)
			case "LOG":
				ext = ".ldf"
				// Формируем имя файла как NewDBName_log.ldf
				physicalFileName = fmt.Sprintf("%s_log%s", newDBName, ext)
			default:
				// Если тип файла неизвестен, просто пропускаем
				continue
			}

			// Формируем полный путь к физическому файлу
			physicalPath := filepath.Join(restorePath, physicalFileName) 

			moveParts = append(moveParts, fmt.Sprintf("MOVE N'%s' TO N'%s'", logicalFile.LogicalName, physicalPath))
		}

		moveClause = strings.Join(moveParts, ", ")
		
		// 4. Выполнение восстановления
		for i, file := range filesToRestore {
			// Проверяем контекст на отмену перед каждым шагом восстановления
			select {
			case <-ctx.Done():
				logging.LogError(fmt.Sprintf("Восстановление базы '%s' отменено пользователем.", newDBName))
				RestoreProgressesMutex.Lock()
				if progress != nil {
					progress.Status = "cancelled"
					progress.Error = "Отменено пользователем"
					progress.EndTime = time.Now()
				}
				RestoreProgressesMutex.Unlock()
				// Горутина восстановления просто завершается, удаление БД будет выполнено в cancelRestoreProcess
				// Не пытаемся перевести в EMERGENCY или удалять здесь.
				return
			default:
				// Продолжаем, если контекст не отменен
			}

			// Обновляем прогресс перед выполнением каждого RESTORE
			RestoreProgressesMutex.Lock()
			if progress != nil {
				progress.CompletedFiles = i
				progress.CurrentFile = filepath.Base(file.FileName)
				if progress.TotalFiles > 0 {
					progress.Percentage = (i * 100) / progress.TotalFiles
				}
			}
			RestoreProgressesMutex.Unlock()

			isFirstFile := i == 0
			isLastFile := i == len(filesToRestore)-1
			var restoreQuery string
			
			var recoveryOption string
			// file.Position больше не доступен, так как мы используем BackupMetadata
			// Предполагаем, что каждый файл содержит один бэкап на позиции 1
			filePositionClause := ", FILE = 1"
			
			if isLastFile {
				// Для последного файла всегда используем RECOVERY.
				// STOPAT не используется, так как точное время восстановления может не совпадать с границей транзакции.
				recoveryOption = "RECOVERY"
				logging.LogDebug("Последний файл в цепочке, используется RECOVERY.")
			} else {
				// Не последний бэкап: всегда используем NORECOVERY без STOPAT
				recoveryOption = "NORECOVERY"
				logging.LogDebug(fmt.Sprintf("Промежуточный файл в цепочке, используется NORECOVERY. Тип: %s", file.Type))
			}
			
			// Формирование команды RESTORE
			// Создаем полный путь к файлу бэкапа через smbSharePath
			backupFilePath := filepath.Join(smbSharePath, backupBaseName, file.FileName)
			
			if isFirstFile {
				// Первый файл (FULL/DIFF) использует MOVE и REPLACE
				restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH %s, REPLACE%s, %s, STATS = 10", newDBName, backupFilePath, moveClause, filePositionClause, recoveryOption)
			} else {
				// Последующие файлы (DIFF/TRN). MOVE не нужен.
				switch file.Type {
				case "Transaction Log":
					// LOG бэкап. 
					restoreQuery = fmt.Sprintf("RESTORE LOG [%s] FROM DISK = N'%s' WITH %s%s, STATS = 10", newDBName, backupFilePath, recoveryOption, filePositionClause)
				case "Database Differential":
					// Дифференциальный бэкап. Используем RESTORE DATABASE
					restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH %s%s, STATS = 10", newDBName, backupFilePath, recoveryOption, filePositionClause)
				case "Database": // FULL, если он не первый
					// Используем RESTORE DATABASE
					restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH %s%s, STATS = 10", newDBName, backupFilePath, recoveryOption, filePositionClause)
				}
			}
			
			logging.LogDebug(fmt.Sprintf("Выполнение RESTORE (%d/%d): %s", i+1, len(filesToRestore), restoreQuery))
			
			if _, err := db.Exec(restoreQuery); err != nil {
				logging.LogDebug(fmt.Sprintf("Прерывание RESTORE для %s (файл: %s): %v", newDBName, backupFilePath, err))
				// Обновляем статус на "failed", удаление БД будет выполнено в cancelRestoreProcess
				RestoreProgressesMutex.Lock()
				if progress != nil {
					progress.Status = "failed"
					progress.Error = err.Error()
					progress.EndTime = time.Now()
				}
				RestoreProgressesMutex.Unlock()
				return
			}
		}
		
		logging.LogInfo(fmt.Sprintf("Процесс восстановления базы данных '%s' завершен.", newDBName))
		
		// Переводим базу данных на модель простого восстановления
		alterRecoveryModelQuery := fmt.Sprintf("ALTER DATABASE [%s] SET RECOVERY SIMPLE", newDBName)
		if _, err := db.Exec(alterRecoveryModelQuery); err != nil {
			logging.LogError(fmt.Sprintf("Ошибка при изменении модели восстановления для базы '%s': %v", newDBName, err))
			// Обновляем статус на "failed", несмотря на успешное восстановление
			RestoreProgressesMutex.Lock()
			if progress != nil {
				progress.Status = "failed"
				progress.Error = fmt.Sprintf("Ошибка изменения модели восстановления: %v", err)
				progress.EndTime = time.Now()
			}
			RestoreProgressesMutex.Unlock()
			return
		}
		
		logging.LogInfo(fmt.Sprintf("Модель восстановления для базы данных '%s' изменена на SIMPLE.", newDBName))
		
		// Переводим базу в многопользовательский режим после завершения восстановления
		if err := SetMultiUserMode(db, newDBName); err != nil {
			logging.LogError(fmt.Sprintf("Ошибка перевода базы '%s' в многопользовательский режим после восстановления: %v", newDBName, err))
			// Обновляем статус на "failed", несмотря на успешное восстановление
			RestoreProgressesMutex.Lock()
			if progress != nil {
				progress.Status = "failed"
				progress.Error = fmt.Sprintf("Ошибка перевода в многопользовательский режим: %v", err)
				progress.EndTime = time.Now()
			}
			RestoreProgressesMutex.Unlock()
			return
		}
		
		RestoreProgressesMutex.Lock()
		if progress != nil {
			progress.Status = "completed"
			progress.CompletedFiles = progress.TotalFiles // Все файлы завершены
			progress.Percentage = 100
			progress.EndTime = time.Now()
		}
		RestoreProgressesMutex.Unlock()

	}(ctx, cancel) // Передаем контекст и функцию отмены в горутину

	return nil
}

// GetRestoreProgress - Возвращает текущий прогресс восстановления для указанной БД
func GetRestoreProgress(dbName string) *RestoreProgress {
	RestoreProgressesMutex.Lock()
	defer RestoreProgressesMutex.Unlock()
	return RestoreProgresses[dbName]
}

// CancelRestoreProcess - Отмена восстановления
func CancelRestoreProcess(db *sql.DB, dbName string) error {
	RestoreProgressesMutex.Lock()
	progress, exists := RestoreProgresses[dbName]
	RestoreProgressesMutex.Unlock()

	if !exists {
		return fmt.Errorf("восстановление базы '%s' не найдено", dbName)
	}

	switch progress.Status {
	case "failed", "cancelled":
		delete(RestoreProgresses, dbName)
		return DeleteDatabase(db, dbName)
	case "completed":
		// При успешном завершении не удаляем базу, а просто удаляем запись о процессе
		delete(RestoreProgresses, dbName)
	return nil
	}

	if progress.CancelFunc != nil {
		progress.CancelFunc()
	} else {
		return fmt.Errorf("невозможно отменить восстановление для базы '%s': CancelFunc не установлен", dbName)
	}

	// Сразу пытаемся убить сессии и удалить базу, без таймаута и ожидания
	if err := KillRestoreSession(db, dbName); err != nil {
	}
	
	delete(RestoreProgresses, dbName)
	return DeleteDatabase(db, dbName)
}

// KillRestoreSession - Находит и завершает активные сессии восстановления для указанной БД
func KillRestoreSession(db *sql.DB, dbName string) error {
	query := `
		SELECT r.session_id, t.text
		FROM sys.dm_exec_requests r
		CROSS APPLY sys.dm_exec_sql_text(r.sql_handle) t
		WHERE r.command LIKE '%RESTORE%'
		   OR r.status = 'suspended';
	`
	
	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("ошибка при запросе активных сессий восстановления для БД '%s': %w", dbName, err)
	}
	defer rows.Close()

	var sessionIDsToKill []int
	for rows.Next() {
		var sessionID int
		var commandText sql.NullString
		if err := rows.Scan(&sessionID, &commandText); err != nil {
			continue
		}

		// Проверяем, содержит ли текст команды имя целевой базы данных
		if commandText.Valid && strings.Contains(commandText.String, fmt.Sprintf("DATABASE [%s]", dbName)) {
			sessionIDsToKill = append(sessionIDsToKill, sessionID)
	}
	}

	if len(sessionIDsToKill) == 0 {
		return nil
	}

	for _, sid := range sessionIDsToKill {
		killQuery := fmt.Sprintf("KILL %d", sid)
		if _, err := db.Exec(killQuery); err != nil {
			// Продолжаем, чтобы попытаться убить другие сессии
		}
	}

	return nil
}
