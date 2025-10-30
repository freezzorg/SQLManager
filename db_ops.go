package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Структура для хранения логических имен файлов бэкапа
type BackupLogicalFile struct {
	LogicalName string
	Type        string // DATA, LOG
}

// Структура для хранения информации о файле бэкапа для цепочки восстановления
type BackupFileSequence struct {
	Path     string    // Полный ЛОКАЛЬНЫЙ путь к файлу
	Time     time.Time // Время из имени файла
	Type     string    // bak, diff, trn
	FullPath string    // Полный локальный путь (для отладки)
}

// --- Утилиты для PIRT ---

// Извлекает дату и время из имени файла (DBName_YYYYMMDD_HHMMSS.ext)
func extractTimeFromFilename(filename string) (time.Time, error) {
	// Ожидаем формат типа: 'DBName_20251019_210001.bak'
	parts := strings.Split(filename, "_")
	
	var timeStr string
	if len(parts) >= 3 {
		// Обычно дата - предпоследний элемент, время - последний (без расширения)
		timeStr = parts[len(parts)-2] + "_" + strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
	} else {
		return time.Time{}, fmt.Errorf("неверный формат имени файла бэкапа: %s", filename)
	}

	// Формат времени: YYYYMMDD_HHMMSS
	t, err := time.Parse("20060102_150405", timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("ошибка парсинга даты/времени из имени файла %s: %w", filename, err)
	}
	return t, nil
}

// Получение логических имен файлов из бэкапа (для формирования MOVE)
func getBackupLogicalFiles(backupPath string) ([]BackupLogicalFile, error) {
    // Внимание: RESTORE FILELISTONLY возвращает более 40 столбцов, многие из которых могут быть NULL.
    query := fmt.Sprintf("RESTORE FILELISTONLY FROM DISK = N'%s'", backupPath)
	
	LogDebug(fmt.Sprintf("Выполнение RESTORE FILELISTONLY: %s", query))
    rows, err := dbConn.Query(query)
    if err != nil {
        return nil, fmt.Errorf("ошибка при запросе RESTORE FILELISTONLY: mssql: %w", err)
    }
    defer rows.Close()

    columnNames, colErr := rows.Columns()
    if colErr != nil {
         return nil, fmt.Errorf("ошибка получения имен столбцов: %w", colErr)
    }
    numColumns := len(columnNames)

    var logicalFiles []BackupLogicalFile
    for rows.Next() {
        // Переменные для нужных нам столбцов
        var logicalName, physicalName, fileType string
        var fileGroupName sql.NullString // ИСПРАВЛЕНО: FileGroupName может быть NULL

        // Инициализация аргументов сканирования. Нам нужны первые 4, остальные - заглушки.
        scanArgs := make([]interface{}, numColumns)
        
        // Назначаем адреса наших переменных первым четырем
        scanArgs[0] = &logicalName 
        scanArgs[1] = &physicalName 
        scanArgs[2] = &fileType 
        scanArgs[3] = &fileGroupName 
        
        // Назначаем заглушки для остальных столбцов
        for i := 4; i < numColumns; i++ {
            var dummy interface{}
            scanArgs[i] = &dummy
        }

        if err := rows.Scan(scanArgs...); err != nil { 
            return nil, fmt.Errorf("ошибка сканирования строки RESTORE FILELISTONLY: %w", err)
        }
		
        // Нас интересуют DATA ("D") и LOG ("L") файлы
        if fileType == "D" || fileType == "L" {
            fileEntry := BackupLogicalFile{
                LogicalName: logicalName,
                Type:        strings.Replace(fileType, "D", "DATA", 1), 
            }
            fileEntry.Type = strings.Replace(fileEntry.Type, "L", "LOG", 1) 
            logicalFiles = append(logicalFiles, fileEntry)
        }
    }

    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("ошибка после итерации строк RESTORE FILELISTONLY: %w", err)
    }
	
	if len(logicalFiles) == 0 {
		return nil, fmt.Errorf("RESTORE FILELISTONLY не вернул DATA или LOG файлы")
	}

    return logicalFiles, nil
}


// Определяет последовательность бэкапов для восстановления на указанный момент времени
func getRestoreSequence(baseName string, restoreTime *time.Time) ([]BackupFileSequence, error) {
	// 1. Читаем все файлы бэкапов для этой базы
	backupDir := filepath.Join(appConfig.SMBShare.LocalMountPoint, baseName)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения директории бэкапа %s: %w", backupDir, err)
	}

	var allFiles []BackupFileSequence
	for _, entry := range entries {
		filename := entry.Name()
		ext := strings.ToLower(filepath.Ext(filename))
		fileType := strings.TrimPrefix(ext, ".")
		
		if fileType == "bak" || fileType == "diff" || fileType == "trn" {
			t, err := extractTimeFromFilename(filename)
			if err != nil {
				LogDebug(fmt.Sprintf("Пропущен файл из-за ошибки времени: %s: %v", filename, err))
				continue
			}
			allFiles = append(allFiles, BackupFileSequence{
				Path:     filepath.Join(backupDir, filename),
				Time:     t,
				Type:     fileType,
				FullPath: filepath.Join(backupDir, filename), 
			})
		}
	}

	if len(allFiles) == 0 {
		return nil, fmt.Errorf("в директории %s не найдены файлы бэкапов", backupDir)
	}

	// 2. Сортируем все файлы по времени (от старых к новым)
	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].Time.Before(allFiles[j].Time)
	})

	// 3. Корректный поиск цепочки для PIRT
	var fullIndex = -1
	var diffIndex = -1
	
	// A. Ищем самый свежий полный (.bak) бэкап, который был создан ДО (или в) restoreTime
	for i := len(allFiles) - 1; i >= 0; i-- { 
		file := allFiles[i]
		if restoreTime != nil && file.Time.After(*restoreTime) {
			continue 
		}
		if file.Type == "bak" {
			fullIndex = i
			break 
		}
	}

	if fullIndex == -1 {
		return nil, fmt.Errorf("не найден полный бэкап (.bak) до указанного времени")
	}

	// B. Ищем самый свежий дифференциальный (.diff) бэкап, 
    // который был создан ПОСЛЕ полного (fullIndex) и ДО (или в) restoreTime
	for i := len(allFiles) - 1; i > fullIndex; i-- { 
		file := allFiles[i]
		if file.Type == "diff" && file.Time.After(allFiles[fullIndex].Time) {
			if restoreTime != nil && file.Time.After(*restoreTime) {
				continue // Пропускаем, если DIFF новее, чем время PIRT
			}
			diffIndex = i
			break // Нашли самый свежий подходящий DIFF
		}
	}

	// 4. Формируем последовательность восстановления
	filesToRestore := make([]BackupFileSequence, 0)
	
	// Всегда добавляем полный бэкап
	filesToRestore = append(filesToRestore, allFiles[fullIndex]) 

	// Если найден дифференциальный бэкап, добавляем его сразу после полного
	lastIndex := fullIndex
	if diffIndex != -1 {
		filesToRestore = append(filesToRestore, allFiles[diffIndex])
		lastIndex = diffIndex
	}

	// 5. Добавляем бэкапы журналов транзакций (.trn) после последнего добавленного файла (full или diff)
	for i := lastIndex + 1; i < len(allFiles); i++ {
		file := allFiles[i]
		if file.Type == "trn" {
			// Проверяем, что TRN не новее времени восстановления (для PIRT)
			if restoreTime != nil && file.Time.After(*restoreTime) {
				continue
			}
			filesToRestore = append(filesToRestore, file)
		}
	}
    
    if len(filesToRestore) == 0 {
		return nil, fmt.Errorf("не удалось сформировать цепочку восстановления")
	}

	LogDebug(fmt.Sprintf("Найдена цепочка восстановления из %d файлов, начиная с %s.", len(filesToRestore), filesToRestore[0].Path))
	return filesToRestore, nil
}

// Запускает асинхронный процесс восстановления базы данных
func startRestore(backupBaseName, newDBName string, restoreTime *time.Time) error {
    // Используем горутину, чтобы не блокировать обработчик HTTP-запросов
    go func() {
        LogInfo(fmt.Sprintf("Начато восстановление базы '%s' из бэкапа '%s'.", newDBName, backupBaseName))

        // 1. Получение последовательности бэкапов
        filesToRestore, err := getRestoreSequence(backupBaseName, restoreTime)
        if err != nil {
            LogError(fmt.Sprintf("Ошибка получения последовательности бэкапов для %s: %v", backupBaseName, err))
            return
        }

        // 2. Определение первого файла и логических имен
        startFile := filesToRestore[0]
        
        // Получаем логические имена файлов из первого файла в цепочке (startFile)
		logicalFiles, err := getBackupLogicalFiles(startFile.Path)
		if err != nil {
			LogError(fmt.Sprintf("Ошибка получения логических имен файлов бэкапа для %s: %v", backupBaseName, err))
			return
		}
        LogDebug(fmt.Sprintf("Успешно получены логические имена файлов из бэкапа: %+v", logicalFiles))

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
			physicalPath := filepath.Join(appConfig.MSSQL.RestorePath, physicalFileName) 

			moveParts = append(moveParts, fmt.Sprintf("MOVE N'%s' TO N'%s'", logicalFile.LogicalName, physicalPath))
		}

		moveClause = strings.Join(moveParts, ", ")
        
        // 4. Выполнение восстановления
        for i, file := range filesToRestore {
            isLastFile := i == len(filesToRestore)-1
            var restoreQuery string
            
            var recoveryOption string
            if isLastFile {
                if restoreTime != nil {
                    // Последний бэкап в PIRT-цепочке: используем RECOVERY и STOPAT
                    recoveryOption = fmt.Sprintf("RECOVERY, STOPAT = N'%s'", restoreTime.Format("2006-01-02 15:04:05"))
                } else {
                    // Последний бэкап в обычном восстановлении: используем RECOVERY
                    recoveryOption = "RECOVERY"
                }
            } else {
                // Не последний бэкап: используем NORECOVERY
                recoveryOption = "NORECOVERY"
            }
            
            // Формирование команды RESTORE
            if i == 0 {
                // Первый файл (FULL/DIFF) использует MOVE и REPLACE
                restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH %s, REPLACE, %s, STATS = 10", newDBName, file.Path, moveClause, recoveryOption)
            } else {
                // Последующие файлы (DIFF/TRN). MOVE не нужен.
                switch file.Type {
				case "trn":
					// LOG бэкап. 
					restoreQuery = fmt.Sprintf("RESTORE LOG [%s] FROM DISK = N'%s' WITH %s, STATS = 10", newDBName, file.Path, recoveryOption)
				case "diff":
					// DIFF бэкап. 
					restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH %s, STATS = 10", newDBName, file.Path, recoveryOption)
				}
            }
            
            LogDebug(fmt.Sprintf("Выполнение RESTORE (%d/%d): %s", i+1, len(filesToRestore), restoreQuery))
            
            if _, err := dbConn.Exec(restoreQuery); err != nil {
                LogError(fmt.Sprintf("Ошибка RESTORE для %s (файл: %s): %v", newDBName, file.Path, err))
                // Удаляем нерабочую БД при ошибке восстановления
                deleteDatabase(newDBName) 
                return
            }
        }
        
        LogInfo(fmt.Sprintf("Процесс восстановления базы данных '%s' завершен.", newDBName))

    }()

    return nil
}

// Удаление базы данных
func deleteDatabase(dbName string) error {
    LogDebug(fmt.Sprintf("Удаление базы данных %s...", dbName))
    // Устанавливаем базу в SINGLE_USER, чтобы сбросить все соединения
    singleUserQuery := fmt.Sprintf("ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE", dbName)
    if _, err := dbConn.Exec(singleUserQuery); err != nil {
        return fmt.Errorf("ошибка перевода БД в SINGLE_USER: %w", err)
    }

    // Удаляем базу данных
    deleteQuery := fmt.Sprintf("DROP DATABASE [%s]", dbName)
    if _, err := dbConn.Exec(deleteQuery); err != nil {
        return fmt.Errorf("ошибка DROP DATABASE: %w", err)
    }

    return nil
}


// Отмена восстановления (УДАЛЯЕТ БД, так как она неработоспособна)
func cancelRestoreProcess(dbName string) error {
	LogInfo(fmt.Sprintf("Получен запрос на отмену восстановления. Удаление нерабочей базы данных '%s'.", dbName))
	return deleteDatabase(dbName)
}

// Получение списка пользовательских баз данных
func getDatabases() ([]Database, error) {
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
    rows, err := dbConn.Query(query)
    if err != nil {
        return nil, fmt.Errorf("ошибка при запросе списка баз: %w", err)
    }
    defer rows.Close()

    var databases []Database
    for rows.Next() {
        var db Database
        var stateDesc string 
        if err := rows.Scan(&db.Name, &stateDesc); err != nil {
            return nil, fmt.Errorf("ошибка сканирования строки БД: %w", err)
        }
        
        // Преобразуем состояние базы в упрощённый статус
        switch strings.ToUpper(stateDesc) {
        case "ONLINE":
            db.State = "online"
        case "RESTORING", "RECOVERING": 
            db.State = "restoring"
        case "OFFLINE":
            db.State = "offline"
        default:
            db.State = "error" 
        }
        databases = append(databases, db)
    }
    
    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("ошибка после итерации строк БД: %w", err)
    }

    return databases, nil
}