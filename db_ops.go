package main

import (
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
	Path     string    // Полный ЛОКАЛЬНЫЙ путь к файлу (например, /mnt/sql_backups/Edelweis/file.bak)
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
		return time.Time{}, fmt.Errorf("неверный формат имени файла: %s", filename)
	}

	// Формат YYYYMMDD_HHMMSS. Используем Local для соответствия вашему логу.
	t, err := time.ParseInLocation("20060102_150405", timeStr, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("не удалось распарсить время %s: %w", timeStr, err)
	}
	return t, nil
}

// Находит оптимальную последовательность бэкапов для PIRT
func getRestoreSequence(backupBaseName string, restoreTime time.Time) ([]BackupFileSequence, error) {
	// fullLocalDir: /mnt/sql_backups/Edelweis
	fullLocalDir := filepath.Join(appConfig.SMBShare.LocalMountPoint, backupBaseName)

	entries, err := os.ReadDir(fullLocalDir)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения каталога бэкапа %s: %w", fullLocalDir, err)
	}

	var files []BackupFileSequence
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()
		ext := strings.ToLower(filepath.Ext(fileName))

		if ext != ".bak" && ext != ".diff" && ext != ".trn" {
			continue
		}

		fileTime, err := extractTimeFromFilename(fileName)
		if err != nil {
			LogWarning(fmt.Sprintf("Пропуск файла %s: %v", fileName, err))
			continue
		}
		
		// Включаем файлы, созданные до или в момент времени восстановления
		if fileTime.After(restoreTime) {
			continue
		}

		fileType := strings.TrimPrefix(ext, ".")
		localPath := filepath.Join(fullLocalDir, fileName)

		files = append(files, BackupFileSequence{
			Path:     localPath, // Используется локальный путь
			Time:     fileTime,
			Type:     fileType,
			FullPath: localPath,
		})
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("в каталоге %s не найдено подходящих файлов бэкапов до %s", backupBaseName, restoreTime.Format("02.01.2006 15:04:05"))
	}

	// Сортировка по времени
	sort.Slice(files, func(i, j int) bool {
		return files[i].Time.Before(files[j].Time)
	})

	// Выбор оптимальной цепочки
	var fullOrDiffIndex = -1
	
	// 1. Ищем самый свежий полный (.bak) или дифференциальный (.diff) бэкап до restoreTime
SearchLoop:
	for i := len(files) - 1; i >= 0; i-- {
        file := files[i]
        // ИСПОЛЬЗУЕМ TAGGED SWITCH ДЛЯ СТИЛИСТИЧЕСКОГО УЛУЧШЕНИЯ
		switch file.Type { 
        case "bak", "diff":
            fullOrDiffIndex = i
            break SearchLoop
        }
	}

	if fullOrDiffIndex == -1 {
		return nil, fmt.Errorf("не найден полный бэкап (.bak) до %s", restoreTime.Format("02.01.2006 15:04:05"))
	}
	
	// Добавляем полный/дифференциальный бэкап и все последующие файлы
	sequence := files[fullOrDiffIndex:]
    
	LogDebug(fmt.Sprintf("Найдена цепочка восстановления из %d файлов, начиная с %s.", len(sequence), sequence[0].FullPath))
	return sequence, nil
}

// --- ОСНОВНЫЕ ФУНКЦИИ ---

// Получение списка пользовательских баз данных
func getDatabases() ([]Database, error) {
    query := `
        SELECT
            name,
            state_desc
        FROM
            sys.databases
        WHERE
            database_id > 4 -- Исключение системных баз: master, model, msdb, tempdb 
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
            db.State = "unknown"
        }
        
        LogDebug(fmt.Sprintf("Получена база данных: Name='%s', State='%s' (оригинальное: '%s')", db.Name, db.State, stateDesc))
        databases = append(databases, db)
    }

    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("ошибка после итерации строк: %w", err)
    }
	
    return databases, nil
}

// Получение логических имен файлов из бэкапа
func getBackupLogicalFiles(backupPath string) ([]BackupLogicalFile, error) {
	// backupPath - это локальный путь (/mnt/sql_backups/...), который SQL Server может прочитать
    query := fmt.Sprintf("RESTORE FILELISTONLY FROM DISK = N'%s'", backupPath)
	
	LogDebug(fmt.Sprintf("Выполнение RESTORE FILELISTONLY: %s", query))
    rows, err := dbConn.Query(query)
    if err != nil {
        return nil, fmt.Errorf("ошибка при запросе RESTORE FILELISTONLY: mssql: %w", err)
    }
    defer rows.Close()

    var logicalFiles []BackupLogicalFile
    for rows.Next() {
        var logicalName, physicalName, fileType, fileGroupName string
        
        // Используем большое количество заглушек (interface{}) для безопасного сканирования
        // широкого набора колонок, который возвращает RESTORE FILELISTONLY.
        var dummy1, dummy2, dummy3, dummy4, dummy5, dummy6, dummy7, dummy8, dummy9, dummy10, 
            dummy11, dummy12, dummy13, dummy14, dummy15, dummy16, dummy17, dummy18, dummy19, dummy20, 
            dummy21, dummy22, dummy23, dummy24, dummy25, dummy26, dummy27, dummy28, dummy29, dummy30 interface{}
			
        if err := rows.Scan(
            &logicalName, &physicalName, &fileType, &fileGroupName,
            &dummy1, &dummy2, &dummy3, &dummy4, &dummy5, &dummy6, 
            &dummy7, &dummy8, &dummy9, &dummy10, &dummy11, &dummy12, 
            &dummy13, &dummy14, &dummy15, &dummy16, &dummy17, &dummy18, 
            &dummy19, &dummy20, &dummy21, &dummy22, &dummy23, &dummy24, 
            &dummy25, &dummy26, &dummy27, &dummy28, &dummy29, &dummy30,
        ); err != nil {
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

// Запуск процесса восстановления
func startRestore(backupBaseName, newDBName string, restoreTime *time.Time) error {
    go func() {
        LogInfo(fmt.Sprintf("Начато восстановление базы '%s' из бэкапа '%s'.", newDBName, backupBaseName))

        var filesToRestore []BackupFileSequence
        var err error

		// Если restoreTime не указан, используем текущее время для поиска последнего .bak
        if restoreTime == nil {
            now := time.Now()
            restoreTime = &now
        }
		
        // 1. Формирование цепочки восстановления (PIRT)
        filesToRestore, err = getRestoreSequence(backupBaseName, *restoreTime)
        if err != nil {
            LogError(fmt.Sprintf("Ошибка составления цепочки бэкапов для PIRT: %v", err))
            return
        }

        // 2. Получение логических имен файлов из ПЕРВОГО файла цепочки 
        firstBackupPath := filesToRestore[0].Path // Используется ЛОКАЛЬНЫЙ путь
        logicalFiles, err := getBackupLogicalFiles(firstBackupPath)
        if err != nil {
            LogError(fmt.Sprintf("ОШИБКА: Ошибка получения логических имен файлов бэкапа для %s: %v", backupBaseName, err))
            return
        }
        
        LogDebug(fmt.Sprintf("Успешно получены логические имена файлов из бэкапа: %v", logicalFiles))

        // 3. Построение команды RESTORE DATABASE (MOVE-часть)
        moveClause := ""
        // RestorePath должен быть путем на СЕРВЕРЕ MSSQL (например, D:\MSSQL\DATA\)
        restorePath := appConfig.MSSQL.RestorePath 
        // Убеждаемся, что RestorePath заканчивается на обратный слеш
        if !strings.HasSuffix(restorePath, "\\") {
            restorePath += "\\"
        }
        
        for i, lf := range logicalFiles {
            if i > 0 {
                moveClause += ", "
            }
            ext := ".mdf"
            if lf.Type == "LOG" {
                ext = ".ldf"
            }
            // Формат: MOVE N'LogicalName' TO N'RestorePath\NewDBName_LogicalName.ext'
            moveClause += fmt.Sprintf("MOVE N'%s' TO N'%s%s_%s%s'", lf.LogicalName, restorePath, newDBName, lf.LogicalName, ext)
        }

        // 4. Выполнение последовательности RESTORE

        // Параметры для первого бэкапа (Full/Diff)
        // firstBackupPath - это локальный путь, который успешно работает.
        firstRestoreQuery := fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH %s, REPLACE, STATS = 1", newDBName, firstBackupPath, moveClause)
        
        // Первый файл всегда с NORECOVERY, чтобы можно было применить лог
        firstRestoreQuery += ", NORECOVERY"
        
        LogDebug(fmt.Sprintf("Выполнение RESTORE DATABASE (1/%d): %s", len(filesToRestore), firstRestoreQuery))
        if _, err := dbConn.Exec(firstRestoreQuery); err != nil {
            LogError(fmt.Sprintf("Ошибка RESTORE DATABASE для %s (файл: %s): %v", newDBName, firstBackupPath, err))
            return
        }
        LogInfo(fmt.Sprintf("База данных '%s' восстановлена из первого бэкапа '%s' с NORECOVERY.", newDBName, firstBackupPath))
        
        
        // 5. Применение журналов транзакций и дифференциальных бэкапов
        for i := 1; i < len(filesToRestore); i++ {
            file := filesToRestore[i]
            
            // Если это ПОСЛЕДНИЙ файл в цепочке
            isLastFile := (i == len(filesToRestore)-1)
            
            var restoreQuery string
            
            // ИСПОЛЬЗУЕМ TAGGED SWITCH ДЛЯ СТИЛИСТИЧЕСКОГО УЛУЧШЕНИЯ
            switch file.Type {
            case "trn":
                restoreQuery = fmt.Sprintf("RESTORE LOG [%s] FROM DISK = N'%s'", newDBName, file.Path)
            case "diff":
                restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s'", newDBName, file.Path)
            default:
                continue
            }
            

            if isLastFile {
                // Последний файл: STOPAT и RECOVERY
                stopAtTimeStr := restoreTime.Format("2006-01-02 15:04:05")
                restoreQuery += fmt.Sprintf(" WITH STOPAT = N'%s', RECOVERY", stopAtTimeStr)
                LogDebug(fmt.Sprintf("Выполнение RESTORE LOG/DIFF (Последний с STOPAT): %s", restoreQuery))
                
                if _, err := dbConn.Exec(restoreQuery); err != nil {
                    LogError(fmt.Sprintf("Ошибка RESTORE LOG/DIFF (STOPAT) для %s (файл: %s): %v", newDBName, file.Path, err))
                    return
                }
                LogInfo(fmt.Sprintf("База данных '%s' успешно восстановлена на момент времени %s.", newDBName, stopAtTimeStr))
            } else {
                // Промежуточные файлы: NORECOVERY
                if file.Type == "diff" {
                    restoreQuery += " WITH NORECOVERY, STATS = 1"
                } else {
                    restoreQuery += " WITH NORECOVERY"
                }
                
                LogDebug(fmt.Sprintf("Выполнение RESTORE LOG/DIFF (промежуточный %d/%d): %s", i+1, len(filesToRestore), restoreQuery))

                if _, err := dbConn.Exec(restoreQuery); err != nil {
                    LogError(fmt.Sprintf("Ошибка RESTORE LOG/DIFF для %s (файл: %s): %v", newDBName, file.Path, err))
                    return
                }
                LogInfo(fmt.Sprintf("Применен бэкап '%s' с NORECOVERY.", file.Path))
            }
        }

        // 6. Финальное восстановление, если не было логов (т.е. восстановлен только полный/дифф бэкап)
        if len(filesToRestore) == 1 && restoreTime != nil {
            // Если запрошен PIRT, но не было логов (только full/diff), нужно явно завершить.
            finalRecoveryQuery := fmt.Sprintf("RESTORE DATABASE [%s] WITH RECOVERY", newDBName)
            LogDebug(fmt.Sprintf("Завершение восстановления с RECOVERY: %s", finalRecoveryQuery))
            if _, err := dbConn.Exec(finalRecoveryQuery); err != nil {
                LogError(fmt.Sprintf("Ошибка завершения восстановления для %s: %v", newDBName, err))
                return
            }
            LogInfo(fmt.Sprintf("База данных '%s' успешно восстановлена.", newDBName))
        }

        LogInfo(fmt.Sprintf("Процесс восстановления базы данных '%s' завершен.", newDBName))

    }()

    return nil
}

// Удаление базы данных
func deleteDatabase(dbName string) error {
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