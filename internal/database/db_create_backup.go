package database

import (
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

		// Обновляем метаданные бэкапа в отдельной горутине
		go func() {
			// Добавим небольшую задержку, чтобы убедиться, что файл бэкапа полностью записан
			// и соединение с базой данных освободилось от выполнения BACKUP
			time.Sleep(5 * time.Second)
			
			// Попробуем получить метаданные с повторными попытками
			maxRetries := 3
			for i := 0; i < maxRetries; i++ {
				err := updateBackupMetadata(db, dbName, backupFilePath, backupDir)
				if err == nil {
					logging.LogInfo(fmt.Sprintf("Метаданные бэкапа успешно обновлены для базы '%s'", dbName))
					return // Успешно завершаем горутину
				}
				
				logging.LogError(fmt.Sprintf("Ошибка обновления метаданных бэкапа для базы '%s' (попытка %d): %v", dbName, i+1, err))
				
				if i < maxRetries-1 {
					// Перед следующей попыткой подождем
					time.Sleep(3 * time.Second)
				}
			}
			
			logging.LogError(fmt.Sprintf("Не удалось обновить метаданные бэкапа для базы '%s' после %d попыток", dbName, maxRetries))
		}()

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

// updateBackupMetadata - Обновляет файл метаданных для базы данных
func updateBackupMetadata(db *sql.DB, dbName, backupFilePath, backupDir string) error {
	logging.LogDebug(fmt.Sprintf("Начало обновления метаданных для бэкапа: %s", backupFilePath))
	
	// Получаем метаданные из файла бэкапа
	newMetadata, err := getBackupHeaderInfo(db, backupFilePath)
	if err != nil {
		logging.LogError(fmt.Sprintf("Ошибка получения метаданных из файла бэкапа %s: %v", backupFilePath, err))
		return fmt.Errorf("ошибка получения метаданных из файла бэкапа: %w", err)
	}
	
	logging.LogDebug(fmt.Sprintf("Получены метаданные для файла: %s", newMetadata.FileName))

	// Путь к файлу метаданных
	metadataPath := filepath.Join(backupDir, "backup_metadata.json")
	logging.LogDebug(fmt.Sprintf("Путь к файлу метаданных: %s", metadataPath))

	// Читаем существующие метаданные
	var allMetadata []BackupMetadata
	if _, err := os.Stat(metadataPath); err == nil {
		// Файл существует, читаем его
		logging.LogDebug("Файл метаданных существует, читаем его")
		data, err := os.ReadFile(metadataPath)
		if err != nil {
			logging.LogError(fmt.Sprintf("Ошибка чтения файла метаданных %s: %v", metadataPath, err))
			return fmt.Errorf("ошибка чтения файла метаданных: %w", err)
		}

		if len(data) > 0 {
			if err := json.Unmarshal(data, &allMetadata); err != nil {
				logging.LogError(fmt.Sprintf("Ошибка разбора JSON файла метаданных %s: %v", metadataPath, err))
				return fmt.Errorf("ошибка разбора JSON файла метаданных: %w", err)
			}
			logging.LogDebug(fmt.Sprintf("Прочитано %d записей из файла метаданных", len(allMetadata)))
		}
	} else if !os.IsNotExist(err) {
		logging.LogError(fmt.Sprintf("Ошибка проверки файла метаданных %s: %v", metadataPath, err))
		return fmt.Errorf("ошибка проверки файла метаданных: %w", err)
	} else {
		logging.LogDebug("Файл метаданных не существует, будет создан новый")
	}

	// Проверяем, есть ли уже запись с таким именем файла
	existingIndex := -1
	for i, metadata := range allMetadata {
		if metadata.FileName == newMetadata.FileName {
			existingIndex = i
			break
		}
	}

	if existingIndex != -1 {
		// Обновляем существующую запись
		logging.LogDebug(fmt.Sprintf("Обновляем существующую запись для файла: %s", newMetadata.FileName))
		allMetadata[existingIndex] = *newMetadata
	} else {
		// Добавляем новую запись
		logging.LogDebug(fmt.Sprintf("Добавляем новую запись для файла: %s", newMetadata.FileName))
		allMetadata = append(allMetadata, *newMetadata)
	}

	// Сортируем все метаданные по времени начала бэкапа
	sort.Slice(allMetadata, func(i, j int) bool {
		return allMetadata[i].Start.Before(allMetadata[j].Start.Time)
	})

	// Записываем обновленные метаданные обратно в файл
	data, err := json.MarshalIndent(allMetadata, "", "  ")
	if err != nil {
		logging.LogError(fmt.Sprintf("Ошибка сериализации метаданных в JSON: %v", err))
		return fmt.Errorf("ошибка сериализации метаданных в JSON: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		logging.LogError(fmt.Sprintf("Ошибка записи файла метаданных %s: %v", metadataPath, err))
		return fmt.Errorf("ошибка записи файла метаданных: %w", err)
	}

	logging.LogInfo(fmt.Sprintf("Файл метаданных успешно обновлен для базы '%s', файл: %s, всего записей: %d", dbName, backupFilePath, len(allMetadata)))
	return nil
}

// UpdateAllBackupMetadata - Обновляет файл метаданных для всех файлов бэкапов в каталоге
func UpdateAllBackupMetadata(db *sql.DB, dbName, backupDir string) error {
	// Получаем список всех файлов бэкапов в каталоге
	backupFiles, err := getAllBackupFiles(backupDir)
	if err != nil {
		return fmt.Errorf("ошибка получения списка файлов бэкапов: %w", err)
	}

	// Путь к файлу метаданных
	metadataPath := filepath.Join(backupDir, "backup_metadata.json")

	// Читаем существующие метаданные
	var allMetadata []BackupMetadata
	if _, err := os.Stat(metadataPath); err == nil {
		// Файл существует, читаем его
		data, err := os.ReadFile(metadataPath)
		if err != nil {
			return fmt.Errorf("ошибка чтения файла метаданных: %w", err)
	}

		if len(data) > 0 {
			if err := json.Unmarshal(data, &allMetadata); err != nil {
				return fmt.Errorf("ошибка разбора JSON файла метаданных: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("ошибка проверки файла метаданных: %w", err)
	}

	// Создаем мапу для быстрого поиска существующих метаданных по имени файла
	metadataMap := make(map[string]*BackupMetadata)
	for i := range allMetadata {
		metadataMap[allMetadata[i].FileName] = &allMetadata[i]
	}

	// Обновляем или добавляем метаданные для каждого файла бэкапа
	for _, backupFile := range backupFiles {
		backupFilePath := filepath.Join(backupDir, backupFile)

		// Проверяем, есть ли уже метаданные для этого файла
		if existingMetadata, exists := metadataMap[backupFile]; exists {
			// Проверяем, изменилось ли время модификации файла
			fileInfo, err := os.Stat(backupFilePath)
			if err != nil {
				logging.LogError(fmt.Sprintf("Ошибка получения информации о файле %s: %v", backupFilePath, err))
				continue
			}

			// Если файл был изменен позже, чем время окончания бэкапа в метаданных, обновляем метаданные
			if fileInfo.ModTime().After(existingMetadata.End.Time) {
				newMetadata, err := getBackupHeaderInfo(db, backupFilePath)
				if err != nil {
					logging.LogError(fmt.Sprintf("Ошибка получения метаданных из файла %s: %v", backupFilePath, err))
					continue
				}
				// Обновляем существующую запись
				*existingMetadata = *newMetadata
			}
		} else {
			// Добавляем новую запись
			newMetadata, err := getBackupHeaderInfo(db, backupFilePath)
			if err != nil {
				logging.LogError(fmt.Sprintf("Ошибка получения метаданных из файла %s: %v", backupFilePath, err))
				continue
			}
			allMetadata = append(allMetadata, *newMetadata)
		}
	}

	// Удаляем записи для файлов, которые больше не существуют
	var updatedMetadata []BackupMetadata
	for _, metadata := range allMetadata {
		backupFilePath := filepath.Join(backupDir, metadata.FileName)
		if _, err := os.Stat(backupFilePath); err == nil {
			// Файл существует, оставляем запись
			updatedMetadata = append(updatedMetadata, metadata)
		} else if os.IsNotExist(err) {
			// Файл не существует, пропускаем запись
			logging.LogDebug(fmt.Sprintf("Удалена устаревшая запись метаданных для файла: %s", metadata.FileName))
		}
	}

	// Сортируем все метаданные по времени начала бэкапа
	sort.Slice(updatedMetadata, func(i, j int) bool {
		return updatedMetadata[i].Start.Before(updatedMetadata[j].Start.Time)
	})

	// Записываем обновленные метаданные обратно в файл
	data, err := json.MarshalIndent(updatedMetadata, "", " ")
	if err != nil {
		return fmt.Errorf("ошибка сериализации метаданных в JSON: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("ошибка записи файла метаданных: %w", err)
	}

	logging.LogInfo(fmt.Sprintf("Файл метаданных обновлен для базы '%s', всего записей: %d", dbName, len(updatedMetadata)))
	return nil
}
