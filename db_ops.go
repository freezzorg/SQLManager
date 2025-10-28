package main

import (
	"fmt"
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
        if err := rows.Scan(&db.Name, &db.State); err != nil {
            return nil, fmt.Errorf("ошибка сканирования строки БД: %w", err)
        }
        databases = append(databases, db)
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

// Запуск процесса восстановления (в упрощенном виде) [cite: 13]
func startRestore(backupPath, newDBName string, restoreTime *time.Time) error {
    // В реальном приложении это должна быть горутина.
    // Команда RESTORE DATABASE должна быть построена на основе типа бэкапа (.bak, .diff, .trn) [cite: 4] 
    // и логических имен файлов, полученных через RESTORE FILELISTONLY.

    // 1. Получение логических имен файлов из бэкапа
    logicalFiles, err := getBackupLogicalFiles(backupPath)
    if err != nil {
        return fmt.Errorf("ошибка получения логических имен файлов бэкапа: %w", err)
    }

    // 2. Построение команды RESTORE DATABASE
    restoreQuery := fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH ", newDBName, backupPath)
    
    // Добавляем MOVE для каждого логического файла
    for i, lf := range logicalFiles {
        if i > 0 {
            restoreQuery += ", "
        }
        // Предполагаем, что для DATA файлов расширение .mdf, для LOG - .ldf
        ext := ".mdf"
        if lf.Type == "LOG" {
            ext = ".ldf"
        }
        restoreQuery += fmt.Sprintf("MOVE N'%s' TO N'%s%s_%s%s'", lf.LogicalName, appConfig.MSSQL.RestorePath, newDBName, lf.LogicalName, ext)
    }

    restoreQuery += ", REPLACE, STATS = 1" // REPLACE для перезаписи существующей БД, STATS для прогресса

    if restoreTime != nil {
        // Если указана дата и время, нужно использовать STOPAT
        // Это требует полной цепочки бэкапов (.bak, .diff, .trn) и логики RESTORE LOG.
        // Для упрощения, здесь только добавляем STOPAT к RESTORE DATABASE,
        // но в реальном приложении потребуется RESTORE DATABASE WITH NORECOVERY,
        // затем RESTORE LOG WITH STOPAT.
        restoreQuery += fmt.Sprintf(", STOPAT = N'%s'", restoreTime.Format("2006-01-02 15:04:05"))
        LogDebug(fmt.Sprintf("Восстановление на момент времени: %s", restoreTime.Format("2006-01-02 15:04:05")))
    }
    
    // Запуск восстановления в горутине для асинхронного выполнения
    go func() {
        LogInfo(fmt.Sprintf("Начато восстановление базы '%s' из бэкапа '%s'.", newDBName, backupPath))
        
        // Взаимодействие с MSSQL в горутине для отслеживания прогресса STATS=1
        // ... dbConn.ExecContext(ctx, restoreQuery) ... 
        
        // Обработка ошибок и завершения
        if _, err := dbConn.Exec(restoreQuery); err != nil {
            LogError(fmt.Sprintf("Ошибка восстановления базы данных %s: %v", newDBName, err))
        } else {
            LogInfo(fmt.Sprintf("Восстановление базы данных %s успешно завершено.", newDBName))
        }
    }()

    return nil
}
