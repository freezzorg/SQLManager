package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv" // Добавлен импорт для strconv.Atoi
	"strings"
	"time"
)

// Структура для хранения логических имен файлов бэкапа (для команды MOVE)
type BackupLogicalFile struct {
	LogicalName string
	Type        string // DATA, LOG
}

// Структура для хранения полной информации о файле бэкапа, полученной через RESTORE HEADERONLY
type BackupFileSequence struct {
	Path              string    // Полный ЛОКАЛЬНЫЙ путь к файлу
	Type              string    // FULL, DIFF, LOG (конвертировано из D, I, L)
	BackupFinishDate  time.Time // Время завершения бэкапа (более точное, чем из имени)
	DatabaseName      string    // Имя базы данных из метаданных бэкапа
	Position          int       // Позиция в файле бэкапа (если файл содержит несколько бэкапов)
	FirstLSN          string    // First LSN
	LastLSN           string    // Last LSN
	DatabaseBackupLSN string    // Для DIFF бэкапов (Target LSN)
}

// --- Утилиты для PIRT ---

// getBackupLogicalFiles - Получение логических имен файлов из бэкапа (для формирования MOVE)
func getBackupLogicalFiles(backupPath string) ([]BackupLogicalFile, error) {
	// RESTORE FILELISTONLY возвращает много столбцов, нас интересуют первые четыре
	query := fmt.Sprintf("RESTORE FILELISTONLY FROM DISK = N'%s'", backupPath)
	
	LogDebug(fmt.Sprintf("Выполнение RESTORE FILELISTONLY: %s", query))
	rows, err := dbConn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("ошибка при запросе RESTORE FILELISTONLY: %w", err)
	}
	defer rows.Close()

	columnNames, colErr := rows.Columns()
	if colErr != nil {
		return nil, fmt.Errorf("ошибка получения имен столбцов: %w", colErr)
	}
	numColumns := len(columnNames)

	var logicalFiles []BackupLogicalFile
	for rows.Next() {
		var logicalName, physicalName, fileType string
		var fileGroupName sql.NullString 

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
		return nil, fmt.Errorf("ошибка после итерации строк RESTORE FILELISTONLY: %w", err)
	}
	
	if len(logicalFiles) == 0 {
		return nil, fmt.Errorf("RESTORE FILELISTONLY не вернул DATA или LOG файлы")
	}

	return logicalFiles, nil
}


// getBackupHeaderInfo - Новая функция для выполнения RESTORE HEADERONLY и получения метаданных
func getBackupHeaderInfo(backupPath string) ([]BackupFileSequence, error) {
	query := fmt.Sprintf("RESTORE HEADERONLY FROM DISK = N'%s'", backupPath)

	// LogDebug(fmt.Sprintf("Выполнение RESTORE HEADERONLY: %s", query))
	rows, err := dbConn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("ошибка при запросе RESTORE HEADERONLY для %s: %w", backupPath, err)
	}
	defer rows.Close()

	columnNames, colErr := rows.Columns()
	if colErr != nil {
		return nil, fmt.Errorf("ошибка получения имен столбцов для HEADERONLY: %w", colErr)
	}
	// LogDebug(fmt.Sprintf("Столбцы, полученные из RESTORE HEADERONLY для %s: %v", backupPath, columnNames))
	// Создаем карту для поиска индексов нужных нам столбцов (для устойчивости)
	colIndexMap := make(map[string]int)
	for i, name := range columnNames {
		colIndexMap[name] = i
	}

	// Проверяем наличие всех критически важных столбцов
	requiredColumns := []string{"BackupType", "DatabaseName", "BackupFinishDate", "Position", "FirstLSN", "LastLSN", "DatabaseBackupLSN"}
	for _, col := range requiredColumns {
		if _, exists := colIndexMap[col]; !exists {
			return nil, fmt.Errorf("в результатах RESTORE HEADERONLY отсутствует критически важный столбец: %s", col)
		}
	}

	var allHeaders []BackupFileSequence
	for rows.Next() {
		// Создаем слайс для сканирования: все столбцы должны быть покрыты
		scanArgs := make([]interface{}, len(columnNames))
		
		// Переменные для нужных нам значений
		var (
			backupType   string
			databaseName string
			finishDate   time.Time
			position     int
			// Используем sql.NullString для LSN-полей, которые могут быть NULL
			nullFirstLSN sql.NullString
			nullLastLSN  sql.NullString
			nullDBBackupLSN sql.NullString
		)
		
		// Назначаем адреса наших переменных соответствующим столбцам
		scanArgs[colIndexMap["BackupType"]] = &backupType
		scanArgs[colIndexMap["DatabaseName"]] = &databaseName
		scanArgs[colIndexMap["BackupFinishDate"]] = &finishDate
		scanArgs[colIndexMap["Position"]] = &position
		scanArgs[colIndexMap["FirstLSN"]] = &nullFirstLSN
		scanArgs[colIndexMap["LastLSN"]] = &nullLastLSN
		scanArgs[colIndexMap["DatabaseBackupLSN"]] = &nullDBBackupLSN
		
		// Назначаем заглушки для остальных столбцов
		for i := 0; i < len(columnNames); i++ {
			if scanArgs[i] == nil {
				var dummy interface{}
				scanArgs[i] = &dummy
			}
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("ошибка сканирования строки RESTORE HEADERONLY для %s: %w", backupPath, err)
		}

		// Конвертируем типы бэкапов в FULL, DIFF, LOG
		var fileType string
		
		// Преобразуем backupType в int
		backupTypeInt, err := strconv.Atoi(backupType)
		if err != nil {
			LogDebug(fmt.Sprintf("Ошибка преобразования BackupType '%s' в int для файла %s, позиция %d: %v. Пропускаем.", backupType, backupPath, position, err))
			continue
		}

		switch backupTypeInt {
		case 1: // Полный бэкап базы данных
			fileType = "FULL"
		case 5: // Дифференциальный бэкап базы данных
			fileType = "DIFF"
		case 2: // Бэкап журнала транзакций
			fileType = "LOG"
		default:
			LogDebug(fmt.Sprintf("Неизвестный BackupType '%d' для файла %s, позиция %d. Пропускаем.", backupTypeInt, backupPath, position))
			continue
		}

		// Заполняем структуру
		allHeaders = append(allHeaders, BackupFileSequence{
			Path:              backupPath, // Путь одинаков для всех наборов в файле
			Type:              fileType,
			BackupFinishDate:  finishDate,
			DatabaseName:      databaseName,
			Position:          position,
			FirstLSN:          nullFirstLSN.String,
			LastLSN:           nullLastLSN.String,
			DatabaseBackupLSN: nullDBBackupLSN.String,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка после итерации строк RESTORE HEADERONLY: %w", err)
	}

	return allHeaders, nil
}


// buildRestoreChain - Определяет последовательность бэкапов для восстановления на указанный момент времени
func buildRestoreChain(baseName string, allHeaders []BackupFileSequence, restoreTime *time.Time) ([]BackupFileSequence, error) {
	// 1. Фильтруем по имени базы данных
	var filteredBackups []BackupFileSequence
	for _, h := range allHeaders {
		// Имя базы данных должно совпадать с baseName (имя папки)
		if strings.EqualFold(h.DatabaseName, baseName) {
			filteredBackups = append(filteredBackups, h)
		}
	}
	
	if len(filteredBackups) == 0 {
		return nil, fmt.Errorf("не найдено бэкапов в заголовках, соответствующих базе данных: %s", baseName)
	}
	
	// 2. Сортируем по времени завершения (от старых к новым)
	sort.Slice(filteredBackups, func(i, j int) bool {
		return filteredBackups[i].BackupFinishDate.Before(filteredBackups[j].BackupFinishDate)
	})

	// 3. Ищем самый свежий FULL бэкап, созданный ДО (или в) restoreTime
	var fullBackup *BackupFileSequence
	for i := len(filteredBackups) - 1; i >= 0; i-- {
		file := filteredBackups[i]
		if file.Type == "FULL" {
			// Если restoreTime не задано, берем самый свежий FULL. 
			// Если задано, берем самый свежий FULL, который был создан ДО (или в) restoreTime.
			if restoreTime == nil || !file.BackupFinishDate.After(*restoreTime) {
				fullBackup = &file
				break
			}
		}
	}

	if fullBackup == nil {
		return nil, fmt.Errorf("не найден подходящий полный бэкап (FULL) до указанного времени")
	}

	// 4. Ищем самый свежий DIFF бэкап, созданный ПОСЛЕ FULL, совместимый по LSN и ДО (или в) restoreTime
	var diffBackup *BackupFileSequence
	
	for i := len(filteredBackups) - 1; i >= 0; i-- {
		file := filteredBackups[i]
		if file.Type == "DIFF" {
			// DIFF должен быть сделан после FULL
			if file.BackupFinishDate.After(fullBackup.BackupFinishDate) {
				// DIFF должен быть совместим с FULL (по LSN): Diff.DatabaseBackupLSN == Full.FirstLSN
				// Исправлено: DatabaseBackupLSN DIFF бэкапа должен совпадать с FirstLSN полного бэкапа, на котором он основан.
				if file.DatabaseBackupLSN == fullBackup.FirstLSN {
					// И DIFF должен быть до (или в) restoreTime
					if restoreTime == nil || !file.BackupFinishDate.After(*restoreTime) {
						diffBackup = &file
						break // Нашли самый свежий подходящий DIFF
					}
				} else {
					LogDebug(fmt.Sprintf("Пропущен DIFF бэкап: %s (LSN не совпадает с FULL). Diff.DatabaseBackupLSN: %s, Full.FirstLSN: %s", file.Path, file.DatabaseBackupLSN, fullBackup.FirstLSN))
				}
			}
		}
	}

	// 5. Формируем последовательность: FULL -> [DIFF] -> [LOGs]
	filesToRestore := make([]BackupFileSequence, 0)
	
	// Добавляем FULL
	filesToRestore = append(filesToRestore, *fullBackup)
	lastBackupTime := fullBackup.BackupFinishDate
	LogDebug(fmt.Sprintf("Бэкап полный: %s (Время: %s, FirstLSN: %s, LastLSN: %s)", fullBackup.Path, fullBackup.BackupFinishDate.Format("2006-01-02 15:04:05"), fullBackup.FirstLSN, fullBackup.LastLSN))

	// Добавляем DIFF, если он найден
	if diffBackup != nil {
		filesToRestore = append(filesToRestore, *diffBackup)
		lastBackupTime = diffBackup.BackupFinishDate
		LogDebug(fmt.Sprintf("Бэкап дифференциальный: %s (Время: %s, FirstLSN: %s, LastLSN: %s, DatabaseBackupLSN: %s)", diffBackup.Path, diffBackup.BackupFinishDate.Format("2006-01-02 15:04:05"), diffBackup.FirstLSN, diffBackup.LastLSN, diffBackup.DatabaseBackupLSN))
	}

	// 6. Добавляем LOG бэкапы после последнего добавленного (FULL или DIFF)
	for _, file := range filteredBackups {
		if file.Type == "LOG" && file.BackupFinishDate.After(lastBackupTime) {
			// Для PIRT: включаем логи, BackupFinishDate которых не превышает restoreTime.
			if restoreTime != nil {
				// Если текущий лог завершен после желаемого времени восстановления,
				// то этот лог может содержать нужную точку восстановления.
				// Добавляем его и прерываем цикл.
				if file.BackupFinishDate.After(*restoreTime) {
					filesToRestore = append(filesToRestore, file)
					LogDebug(fmt.Sprintf("Бэкап журналов транзакций (PIRT, последний): %s (Время: %s, FirstLSN: %s, LastLSN: %s)", file.Path, file.BackupFinishDate.Format("2006-01-02 15:04:05"), file.FirstLSN, file.LastLSN))
					break
				}
			}
			
			// Добавляем LOG.
			filesToRestore = append(filesToRestore, file)
			LogDebug(fmt.Sprintf("Бэкап журналов транзакций: %s (Время: %s, FirstLSN: %s, LastLSN: %s)", file.Path, file.BackupFinishDate.Format("2006-01-02 15:04:05"), file.FirstLSN, file.LastLSN))
		}
	}
	
	if len(filesToRestore) == 0 {
		return nil, fmt.Errorf("не удалось сформировать цепочку восстановления")
	}

	return filesToRestore, nil
}


// getRestoreSequence - Определяет последовательность бэкапов для восстановления на указанный момент времени
// (обертка для чтения файлов и построения цепочки)
func getRestoreSequence(baseName string, restoreTime *time.Time) ([]BackupFileSequence, error) {
	// 1. Читаем все файлы бэкапов для этой базы
	backupDir := filepath.Join(appConfig.SMBShare.LocalMountPoint, baseName)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения директории бэкапа %s: %w", backupDir, err)
	}

	var allHeaders []BackupFileSequence
	
	for _, entry := range entries {
		filename := entry.Name()
		// Проверяем только потенциально нужные файлы, чтобы не нагружать SQL Server
		if strings.HasSuffix(strings.ToLower(filename), ".bak") || 
			strings.HasSuffix(strings.ToLower(filename), ".diff") || 
			strings.HasSuffix(strings.ToLower(filename), ".trn") {
			
			fullPath := filepath.Join(backupDir, filename)
			
			// Получаем HEADERONLY. В одном файле может быть несколько бэкапов.
			headers, err := getBackupHeaderInfo(fullPath)
			if err != nil {
				// Логируем ошибку, но продолжаем
				LogError(fmt.Sprintf("Ошибка HEADERONLY для файла %s: %v", filename, err))
				continue
			}
			// LogDebug(fmt.Sprintf("Получено %d заголовков из файла %s", len(headers), filename))
			allHeaders = append(allHeaders, headers...)
		}
	}

	if len(allHeaders) == 0 {
		return nil, fmt.Errorf("в директории %s не найдено валидных файлов бэкапов", backupDir)
	}

	// 2. Строим цепочку восстановления
	filesToRestore, err := buildRestoreChain(baseName, allHeaders, restoreTime)
	if err != nil {
		return nil, err
	}
	
	LogDebug(fmt.Sprintf("Найдена цепочка восстановления из %d файлов, начиная с %s.", len(filesToRestore), filesToRestore[0].Path))
	return filesToRestore, nil
}


// Запускает асинхронный процесс восстановления базы данных
func startRestore(backupBaseName, newDBName string, restoreTime *time.Time) error {
	// Используем горутину, чтобы не блокировать обработчик HTTP-запросов
	go func() {
		LogInfo(fmt.Sprintf("Начато асинхронное восстановление базы '%s' из бэкапа '%s'.", newDBName, backupBaseName))
		if restoreTime != nil {
			LogDebug(fmt.Sprintf("Желаемое время восстановления (PIRT): %s", restoreTime.Format("2006-01-02 15:04:05")))
		}

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
			isFirstFile := i == 0
			isLastFile := i == len(filesToRestore)-1
			var restoreQuery string
			
			var recoveryOption string
			var filePositionClause string // Clause для указания позиции в файле
			
			// Если в одном файле несколько бэкапов (Position > 1), нужно указать POSITION
			if file.Position > 1 {
				filePositionClause = fmt.Sprintf(", FILE = %d", file.Position)
			}
			
			if isLastFile {
				// Для последнего файла всегда используем RECOVERY.
				// STOPAT не используется, так как точное время восстановления может не совпадать с границей транзакции.
				recoveryOption = "RECOVERY"
				LogDebug("Последний файл в цепочке, используется RECOVERY (без STOPAT).")
			} else {
				// Не последний бэкап: всегда используем NORECOVERY без STOPAT
				recoveryOption = "NORECOVERY"
				LogDebug(fmt.Sprintf("Промежуточный файл в цепочке, используется NORECOVERY (без STOPAT). Тип: %s", file.Type))
			}
			
			// Формирование команды RESTORE
			if isFirstFile {
				// Первый файл (FULL/DIFF) использует MOVE и REPLACE
				restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH %s, REPLACE%s, %s, STATS = 10", newDBName, file.Path, moveClause, filePositionClause, recoveryOption)
			} else {
				// Последующие файлы (DIFF/TRN). MOVE не нужен.
				switch file.Type {
				case "LOG":
					// LOG бэкап. 
					restoreQuery = fmt.Sprintf("RESTORE LOG [%s] FROM DISK = N'%s' WITH %s%s, STATS = 10", newDBName, file.Path, recoveryOption, filePositionClause)
				case "DIFF", "FULL": // DIFF/FULL, если они не первые (хотя DIFF должен быть вторым, а FULL всегда первым)
					// Используем RESTORE DATABASE
					restoreQuery = fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH %s%s, STATS = 10", newDBName, file.Path, recoveryOption, filePositionClause)
				}
			}
			
			LogDebug(fmt.Sprintf("Выполнение RESTORE (%d/%d): %s", i+1, len(filesToRestore), restoreQuery))
			
			if _, err := dbConn.Exec(restoreQuery); err != nil {
				LogError(fmt.Sprintf("Ошибка RESTORE для %s (файл: %s, позиция: %d): %v", newDBName, file.Path, file.Position, err))
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
	// 1. Попытка перевести базу в SINGLE_USER, чтобы сбросить соединения.
	singleUserQuery := fmt.Sprintf("ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE", dbName)
	LogDebug(fmt.Sprintf("Попытка перевести БД '%s' в SINGLE_USER...", dbName))
	if _, err := dbConn.Exec(singleUserQuery); err != nil {
		// Логируем ошибку, но не возвращаем её, чтобы перейти к DROP DATABASE
		LogError(fmt.Sprintf("Ошибка перевода БД '%s' в SINGLE_USER. Продолжение попытки DROP: %v", dbName, err))
	}

	// 2. Удаляем базу данных.
	deleteQuery := fmt.Sprintf("DROP DATABASE [%s]", dbName)
	LogDebug(fmt.Sprintf("Выполнение DROP DATABASE [%s]", dbName))
	if _, err := dbConn.Exec(deleteQuery); err != nil {
		// Если DROP DATABASE не сработал, возвращаем ошибку.
		return fmt.Errorf("ошибка DROP DATABASE для БД %s: %w", dbName, err)
	}
	
	LogInfo(fmt.Sprintf("База данных '%s' успешно удалена.", dbName))
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
