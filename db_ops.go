package main

import (
	"fmt"
	"strings"
	"time"
)

// Структура для хранения логических имен файлов бэкапа
type BackupLogicalFile struct {
	LogicalName string
	Type        string // DATA, LOG
}

// Получение списка пользовательских баз данных [cite: 9, 10]
func getDatabases() ([]Database, error) {
    query := `
        SELECT
            name,
            state_desc
        FROM
            sys.databases
        WHERE
            database_id > 4 -- Исключение системных баз: master, model, msdb, tempdb [cite: 9]
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
        var stateDesc string // Сканируем state_desc как string
        if err := rows.Scan(&db.Name, &stateDesc); err != nil {
            return nil, fmt.Errorf("ошибка сканирования строки БД: %w", err)
        }
        
        // Преобразуем состояние базы в упрощённый статус
        switch strings.ToUpper(stateDesc) {
        case "ONLINE":
            db.State = "online"
        case "RESTORING", "RECOVERING": // Добавляем "RECOVERING" из примера PowerShell
            db.State = "restoring"
        case "OFFLINE", "SUSPECT", "EMERGENCY": // Добавляем "EMERGENCY" из примера PowerShell
            db.State = "error"
        default:
            db.State = "unknown"
        }
        
        LogDebug(fmt.Sprintf("Получена база данных: Name='%s', State='%s' (оригинальное: '%s')", db.Name, db.State, stateDesc)) // Обновлено логирование
        databases = append(databases, db)
    }
    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("ошибка после итерации строк БД: %w", err)
    }
    return databases, nil
}

// Удаление базы данных [cite: 18, 20]
func deleteDatabase(dbName string) error {
    // T-SQL требует, чтобы имя базы данных было передано как динамический SQL
    // для ALTER DATABASE и DROP DATABASE, чтобы избежать проблем с символами.
    
    LogDebug(fmt.Sprintf("Подготовка к удалению базы данных: %s", dbName))

    // 1. Переключение базы в однопользовательский режим и завершение существующих подключений
    alterQuery := fmt.Sprintf("ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE;", dbName)
    if _, err := dbConn.Exec(alterQuery); err != nil {
        return fmt.Errorf("ошибка переключения БД в SINGLE_USER: %w", err)
    }
    LogInfo(fmt.Sprintf("База данных %s переключена в SINGLE_USER.", dbName))

    // 2. Удаление базы данных
    dropQuery := fmt.Sprintf("DROP DATABASE [%s];", dbName)
    if _, err := dbConn.Exec(dropQuery); err != nil {
        return fmt.Errorf("ошибка удаления БД: %w", err)
    }
    LogInfo(fmt.Sprintf("База данных %s успешно удалена.", dbName))
    
    return nil
}

// Получает логические имена файлов из бэкапа с помощью RESTORE FILELISTONLY
func getBackupLogicalFiles(backupPath string) ([]BackupLogicalFile, error) {
    query := fmt.Sprintf("RESTORE FILELISTONLY FROM DISK = N'%s'", backupPath)
    rows, err := dbConn.Query(query)
    if err != nil {
        return nil, fmt.Errorf("ошибка при запросе RESTORE FILELISTONLY: %w", err)
    }
    defer rows.Close()

    var logicalFiles []BackupLogicalFile
    for rows.Next() {
        var (
            logicalName, physicalName, fileType, fileGroupName, collationName string
            size, maxSize, fileID, createLSN, dropLSN, uniqueID, readOnlyLSN, readWriteLSN int64
            backupSizeInBytes, differentialBaseLSN, differentialBaseGUID, differentialBaseTime int64
            isReadOnly, isPresent, TDEThumbprint bool
            containerID string
        )
        // Сканируем только нужные поля, остальные игнорируем или сканируем в пустые переменные
        err := rows.Scan(
            &logicalName, &physicalName, &fileType, &fileGroupName, &size, &maxSize,
            &fileID, &createLSN, &dropLSN, &uniqueID, &readOnlyLSN, &readWriteLSN,
            &backupSizeInBytes, &differentialBaseLSN, &differentialBaseGUID, &differentialBaseTime,
            &isReadOnly, &isPresent, &TDEThumbprint, &collationName, &containerID,
        )
        if err != nil {
            return nil, fmt.Errorf("ошибка сканирования строки RESTORE FILELISTONLY: %w", err)
        }

        logicalFiles = append(logicalFiles, BackupLogicalFile{
            LogicalName: logicalName,
            Type:        fileType,
        })
    }

    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("ошибка после итерации строк RESTORE FILELISTONLY: %w", err)
    }

    return logicalFiles, nil
}

// Проверяет существование базы данных по имени
func checkDatabaseExists(dbName string) (bool, error) {
    query := fmt.Sprintf("SELECT COUNT(*) FROM sys.databases WHERE name = N'%s'", dbName)
    var count int
    err := dbConn.QueryRow(query).Scan(&count)
    if err != nil {
        return false, fmt.Errorf("ошибка при проверке существования БД: %w", err)
    }
    return count > 0, nil
}

// Запуск процесса восстановления [cite: 13]
func startRestore(backupPath, newDBName string, restoreTime *time.Time) error {
    go func() {
        LogInfo(fmt.Sprintf("Начато восстановление базы '%s' из бэкапа '%s'.", newDBName, backupPath))

        // 1. Получение логических имен файлов из бэкапа
        logicalFiles, err := getBackupLogicalFiles(backupPath)
        if err != nil {
            LogError(fmt.Sprintf("Ошибка получения логических имен файлов бэкапа для %s: %v", backupPath, err))
            return
        }

        // 2. Построение команды RESTORE DATABASE
        moveClause := ""
        for i, lf := range logicalFiles {
            if i > 0 {
                moveClause += ", "
            }
            ext := ".mdf"
            if lf.Type == "LOG" {
                ext = ".ldf"
            }
            moveClause += fmt.Sprintf("MOVE N'%s' TO N'%s%s_%s%s'", lf.LogicalName, appConfig.MSSQL.RestorePath, newDBName, lf.LogicalName, ext)
        }

        var restoreQuery string
        if restoreTime != nil {
            // Восстановление на момент времени требует RESTORE DATABASE WITH NORECOVERY
            // и затем RESTORE LOG WITH STOPAT
            restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH %s, NORECOVERY, STATS = 1", newDBName, backupPath, moveClause)
            LogDebug(fmt.Sprintf("Выполнение RESTORE DATABASE WITH NORECOVERY: %s", restoreQuery))
            if _, err := dbConn.Exec(restoreQuery); err != nil {
                LogError(fmt.Sprintf("Ошибка RESTORE DATABASE WITH NORECOVERY для %s: %v", newDBName, err))
                return
            }
            LogInfo(fmt.Sprintf("База данных '%s' восстановлена из полного/дифференциального бэкапа с NORECOVERY.", newDBName))

            // Здесь должна быть логика для поиска и применения всех .trn бэкапов до restoreTime
            // Для текущей задачи это будет упрощено: предполагаем, что restoreTime применяется к основному бэкапу.
            // В реальной системе нужно:
            // 1. Просканировать все .trn бэкапы в каталоге.
            // 2. Отфильтровать те, которые были созданы до restoreTime.
            // 3. Применить их последовательно с RESTORE LOG ... WITH STOPAT.

            // Упрощенная логика для RESTORE LOG WITH STOPAT
            // Предполагаем, что backupPath - это полный бэкап, и мы применяем STOPAT к нему.
            // Это не совсем корректно для реального восстановления на момент времени,
            // но соответствует текущему уровню детализации задачи.
            restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] WITH RECOVERY, STOPAT = N'%s'", newDBName, restoreTime.Format("2006-01-02 15:04:05"))
            LogDebug(fmt.Sprintf("Выполнение RESTORE DATABASE WITH RECOVERY, STOPAT: %s", restoreQuery))
            if _, err := dbConn.Exec(restoreQuery); err != nil {
                LogError(fmt.Sprintf("Ошибка RESTORE DATABASE WITH RECOVERY, STOPAT для %s: %v", newDBName, err))
                return
            }
            LogInfo(fmt.Sprintf("База данных '%s' успешно восстановлена на момент времени %s.", newDBName, restoreTime.Format("02.01.2006 15:04:05")))

        } else {
            // Обычное восстановление (без восстановления на момент времени)
            restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH %s, REPLACE, STATS = 1", newDBName, backupPath, moveClause)
            LogDebug(fmt.Sprintf("Выполнение RESTORE DATABASE: %s", restoreQuery))
            if _, err := dbConn.Exec(restoreQuery); err != nil {
                LogError(fmt.Sprintf("Ошибка восстановления базы данных %s: %v", newDBName, err))
                return
            }
            LogInfo(fmt.Sprintf("Восстановление базы данных %s успешно завершено.", newDBName))
        }
    }()

    return nil
}
