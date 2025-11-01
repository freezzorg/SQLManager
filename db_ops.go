package main

import (
	"context" // Добавлен импорт
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv" // Добавлен импорт для strconv.Atoi
	"strings"
	"sync" // Добавлен импорт для sync.Mutex
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

// restoreProgress - Структура для отслеживания прогресса восстановления
type restoreProgress struct {
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
type backupProgress struct {
	Percentage    int       `json:"percentage"`
	Status        string    `json:"status"` // "pending", "in_progress", "completed", "failed", "cancelled"
	StartTime     time.Time `json:"startTime"`
	EndTime       time.Time `json:"endTime"`
	Error         string    `json:"error,omitempty"`
	BackupFilePath string   `json:"backupFilePath,omitempty"` // Путь к создаваемому файлу бэкапа
	SessionID     int       `json:"sessionID,omitempty"`      // Session ID процесса BACKUP
	CancelFunc    context.CancelFunc `json:"-"` // Функция для отмены контекста горутины
}

// Глобальная карта для хранения прогресса восстановления по имени новой БД
var RestoreProgresses = make(map[string]*restoreProgress)
var RestoreProgressesMutex sync.Mutex

// Глобальная карта для хранения прогресса создания бэкапа по имени БД
var BackupProgresses = make(map[string]*backupProgress)
var BackupProgressesMutex sync.Mutex

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
	// Инициализация прогресса восстановления
	// Создаем контекст для отмены операции восстановления
	ctx, cancel := context.WithCancel(context.Background())

	RestoreProgressesMutex.Lock()
	RestoreProgresses[newDBName] = &restoreProgress{
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

		LogWebInfo(fmt.Sprintf("Начато асинхронное восстановление базы '%s' из бэкапа '%s'.", newDBName, backupBaseName))
		if restoreTime != nil {
			LogDebug(fmt.Sprintf("Желаемое время восстановления (PIRT): %s", restoreTime.Format("2006-01-02 15:04:05")))
		}

		// Обновляем статус на "in_progress"
		RestoreProgressesMutex.Lock()
		progress := RestoreProgresses[newDBName]
		if progress != nil {
			progress.Status = "in_progress"
		}
		RestoreProgressesMutex.Unlock()

		// 1. Получение последовательности бэкапов
		filesToRestore, err := getRestoreSequence(backupBaseName, restoreTime)
		if err != nil {
			LogError(fmt.Sprintf("Ошибка получения последовательности бэкапов для %s: %v", backupBaseName, err))
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
		startFile := filesToRestore[0]
		
		// Получаем логические имена файлов из первого файла в цепочке (startFile)
		logicalFiles, err := getBackupLogicalFiles(startFile.Path)
		if err != nil {
			LogError(fmt.Sprintf("Ошибка получения логических имен файлов бэкапа для %s: %v", backupBaseName, err))
			RestoreProgressesMutex.Lock()
			if progress != nil {
				progress.Status = "failed"
				progress.Error = err.Error()
				progress.EndTime = time.Now()
			}
			RestoreProgressesMutex.Unlock()
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
			// Проверяем контекст на отмену перед каждым шагом восстановления
			select {
			case <-ctx.Done():
				LogError(fmt.Sprintf("Восстановление базы '%s' отменено пользователем.", newDBName))
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
				progress.CurrentFile = filepath.Base(file.Path)
				if progress.TotalFiles > 0 {
					progress.Percentage = (i * 100) / progress.TotalFiles
				}
			}
			RestoreProgressesMutex.Unlock()

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
				LogDebug(fmt.Sprintf("Прерывание RESTORE для %s (файл: %s, позиция: %d): %v", newDBName, file.Path, file.Position, err))
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
		
		LogInfo(fmt.Sprintf("Процесс восстановления базы данных '%s' завершен.", newDBName))
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

// startBackup - Запускает асинхронный процесс создания полного бэкапа базы данных
func startBackup(dbName string) error {
	// Создаем контекст для отмены операции бэкапа
	ctx, cancel := context.WithCancel(context.Background())

	BackupProgressesMutex.Lock()
	BackupProgresses[dbName] = &backupProgress{
		Status:    "pending",
		StartTime: time.Now(),
		CancelFunc: cancel, // Сохраняем функцию отмены
	}
	BackupProgressesMutex.Unlock()

	go func(ctx context.Context, cancel context.CancelFunc) {
		defer cancel()

		LogWebInfo(fmt.Sprintf("Начато асинхронное создание полного бэкапа базы '%s'.", dbName))

		// 1. Проверяем и создаем каталог для бэкапов
		backupDir, err := checkAndCreateBackupDir(dbName)
		if err != nil {
			LogError(fmt.Sprintf("Ошибка при подготовке каталога для бэкапа базы '%s': %v", dbName, err))
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

		BackupProgressesMutex.Lock()
		if progress := BackupProgresses[dbName]; progress != nil {
			progress.Status = "in_progress"
			progress.BackupFilePath = backupFilePath
		}
		BackupProgressesMutex.Unlock()

		// 2. Выполняем команду BACKUP DATABASE
		// STATS = 10 будет выводить прогресс каждые 10%
		backupQuery := fmt.Sprintf("BACKUP DATABASE [%s] TO DISK = N'%s' WITH INIT, STATS = 10", dbName, backupFilePath)
		LogDebug(fmt.Sprintf("Выполнение BACKUP DATABASE: %s", backupQuery))

		// Используем QueryRow для получения session_id, если это возможно, или просто Exec
		// Для получения прогресса нам нужен session_id
		// Запускаем команду в фоновом режиме и сразу пытаемся получить session_id
		go func() {
			// Даем SQL Server немного времени, чтобы начать процесс BACKUP
			time.Sleep(1 * time.Second) 
			sessionID, err := getBackupSessionID(dbName)
			if err != nil {
				LogError(fmt.Sprintf("Не удалось получить session_id для BACKUP базы '%s': %v", dbName, err))
				// Продолжаем без session_id, прогресс не будет отображаться
			} else {
				BackupProgressesMutex.Lock()
				if progress := BackupProgresses[dbName]; progress != nil {
					progress.SessionID = sessionID
				}
				BackupProgressesMutex.Unlock()
				LogDebug(fmt.Sprintf("Получен session_id %d для BACKUP базы '%s'.", sessionID, dbName))
			}
		}()

		_, err = dbConn.ExecContext(ctx, backupQuery) // Используем ExecContext для отмены
		
		// Проверяем, была ли отмена через контекст
		if ctx.Err() != nil {
			LogError(fmt.Sprintf("Создание бэкапа базы '%s' отменено пользователем (контекст).", dbName))
			BackupProgressesMutex.Lock()
			if progress := BackupProgresses[dbName]; progress != nil {
				progress.Status = "cancelled"
				progress.Error = "Отменено пользователем"
				progress.EndTime = time.Now()
			}
			BackupProgressesMutex.Unlock()
			// Удаление файла бэкапа будет выполнено в cancelBackupProcess
			return
		}

		if err != nil {
			LogError(fmt.Sprintf("Ошибка при создании бэкапа базы '%s': %v", dbName, err))
			BackupProgressesMutex.Lock()
			if progress := BackupProgresses[dbName]; progress != nil {
				progress.Status = "failed"
				progress.Error = err.Error()
				progress.EndTime = time.Now()
			}
			BackupProgressesMutex.Unlock()
			// Удаление файла бэкапа будет выполнено в cancelBackupProcess
			return
		}

		LogInfo(fmt.Sprintf("Создание бэкапа базы данных '%s' успешно завершено в файл '%s'.", dbName, backupFilePath))
		BackupProgressesMutex.Lock()
		if progress := BackupProgresses[dbName]; progress != nil {
			progress.Percentage = 100
			progress.Status = "completed"
			progress.EndTime = time.Now()
		}
		BackupProgressesMutex.Unlock()

	}(ctx, cancel)

	return nil
}

// Удаление базы данных
func deleteDatabase(dbName string) error {
	// 1. Удаляем базу данных.
	// Перевод в SINGLE_USER не требуется, так как база не используется во время восстановления.
	deleteQuery := fmt.Sprintf("DROP DATABASE [%s]", dbName)
	LogDebug(fmt.Sprintf("Выполнение DROP DATABASE [%s]", dbName))
	if _, err := dbConn.Exec(deleteQuery); err != nil {
		return fmt.Errorf("ошибка DROP DATABASE для БД %s: %w", dbName, err)
	}
	
	LogWebInfo(fmt.Sprintf("База данных '%s' успешно удалена.", dbName))
	return nil
}

// killRestoreSession - Находит и завершает активные сессии восстановления для указанной БД
func killRestoreSession(dbName string) error {
	query := `
		SELECT r.session_id, t.text
		FROM sys.dm_exec_requests r
		CROSS APPLY sys.dm_exec_sql_text(r.sql_handle) t
		WHERE r.command LIKE '%RESTORE%'
		   OR r.status = 'suspended';
	`
	
	rows, err := dbConn.Query(query)
	if err != nil {
		return fmt.Errorf("ошибка при запросе активных сессий восстановления для БД '%s': %w", dbName, err)
	}
	defer rows.Close()

	var sessionIDsToKill []int
	for rows.Next() {
		var sessionID int
		var commandText sql.NullString
		if err := rows.Scan(&sessionID, &commandText); err != nil {
			LogError(fmt.Sprintf("Ошибка сканирования session_id и текста команды: %v", err))
			continue
		}

		// Проверяем, содержит ли текст команды имя целевой базы данных
		if commandText.Valid && strings.Contains(commandText.String, fmt.Sprintf("DATABASE [%s]", dbName)) {
			sessionIDsToKill = append(sessionIDsToKill, sessionID)
		}
	}

	if len(sessionIDsToKill) == 0 {
		LogDebug(fmt.Sprintf("Активных сессий восстановления для БД '%s' не найдено.", dbName))
		return nil
	}

	LogInfo(fmt.Sprintf("Найдено %d активных сессий восстановления для БД '%s'. Попытка завершения...", len(sessionIDsToKill), dbName))
	for _, sid := range sessionIDsToKill {
		killQuery := fmt.Sprintf("KILL %d", sid)
		LogDebug(fmt.Sprintf("Выполнение: %s", killQuery))
		if _, err := dbConn.Exec(killQuery); err != nil {
			LogError(fmt.Sprintf("Ошибка KILL сессии %d для БД '%s': %v", sid, dbName, err))
			// Продолжаем, чтобы попытаться убить другие сессии
		} else {
			LogInfo(fmt.Sprintf("Сессия %d для БД '%s' успешно завершена.", sid, dbName))
		}
	}

	return nil
}

// Отмена восстановления
func cancelRestoreProcess(dbName string) error {
	RestoreProgressesMutex.Lock()
	progress, exists := RestoreProgresses[dbName]
	RestoreProgressesMutex.Unlock()

	if !exists {
		return fmt.Errorf("восстановление базы '%s' не найдено", dbName)
	}

	if progress.Status == "completed" || progress.Status == "failed" || progress.Status == "cancelled" {
		LogInfo(fmt.Sprintf("Восстановление базы '%s' уже в статусе '%s'. Попытка удаления базы.", dbName, progress.Status))
		delete(RestoreProgresses, dbName)
		return deleteDatabase(dbName)
	}

	LogInfo(fmt.Sprintf("Получен запрос на отмену восстановления базы данных '%s'.", dbName))

	if progress.CancelFunc != nil {
		progress.CancelFunc()
		LogInfo(fmt.Sprintf("Сигнал отмены отправлен для базы '%s'.", dbName))
	} else {
		LogError(fmt.Sprintf("CancelFunc для базы '%s' не установлен. Невозможно отправить сигнал отмены.", dbName))
		return fmt.Errorf("невозможно отменить восстановление для базы '%s': CancelFunc не установлен", dbName)
	}

	// Сразу пытаемся убить сессии и удалить базу, без таймаута и ожидания
	LogInfo(fmt.Sprintf("Попытка завершения активных сессий и удаления базы '%s'.", dbName))
	if err := killRestoreSession(dbName); err != nil {
		LogWebError(fmt.Sprintf("Ошибка при завершении сессий восстановления для базы '%s': %v", dbName, err))
	}
	
	delete(RestoreProgresses, dbName)
	return deleteDatabase(dbName)
}

// cancelBackupProcess - Отменяет активный процесс создания бэкапа и удаляет файл бэкапа
func cancelBackupProcess(dbName string) error {
	BackupProgressesMutex.Lock()
	progress, exists := BackupProgresses[dbName]
	BackupProgressesMutex.Unlock()

	if !exists {
		return fmt.Errorf("процесс создания бэкапа для базы '%s' не найден", dbName)
	}

	if progress.Status == "completed" || progress.Status == "failed" || progress.Status == "cancelled" {
		LogInfo(fmt.Sprintf("Создание бэкапа базы '%s' уже в статусе '%s'. Попытка удаления файла бэкапа.", dbName, progress.Status))
		delete(BackupProgresses, dbName)
		if progress.BackupFilePath != "" {
			return os.Remove(progress.BackupFilePath)
		}
		return nil
	}

	LogInfo(fmt.Sprintf("Получен запрос на отмену создания бэкапа базы данных '%s'.", dbName))

	// Отправляем сигнал отмены через контекст
	if progress.CancelFunc != nil {
		progress.CancelFunc()
		LogInfo(fmt.Sprintf("Сигнал отмены отправлен для бэкапа базы '%s'.", dbName))
	} else {
		LogError(fmt.Sprintf("CancelFunc для бэкапа базы '%s' не установлен. Невозможно отправить сигнал отмены.", dbName))
		return fmt.Errorf("невозможно отменить бэкап для базы '%s': CancelFunc не установлен", dbName)
	}

	// Если есть session_id, пытаемся убить процесс
	if progress.SessionID != 0 {
		killQuery := fmt.Sprintf("KILL %d", progress.SessionID)
		LogDebug(fmt.Sprintf("Попытка KILL сессии %d для бэкапа базы '%s'.", progress.SessionID, dbName))
		if _, err := dbConn.Exec(killQuery); err != nil {
			LogError(fmt.Sprintf("Ошибка KILL сессии %d для бэкапа базы '%s': %v", progress.SessionID, dbName, err))
			// Продолжаем, даже если KILL не удался
		} else {
			LogInfo(fmt.Sprintf("Сессия %d для бэкапа базы '%s' успешно завершена.", progress.SessionID, dbName))
		}
	} else {
		LogDebug(fmt.Sprintf("Session ID для бэкапа базы '%s' не найден, KILL не требуется.", dbName))
	}

	// Удаляем файл бэкапа, если он был создан
	if progress.BackupFilePath != "" {
		if err := os.Remove(progress.BackupFilePath); err != nil {
			LogError(fmt.Sprintf("Ошибка удаления файла бэкапа '%s' для базы '%s': %v", progress.BackupFilePath, dbName, err))
			return fmt.Errorf("ошибка удаления файла бэкапа: %w", err)
		}
		LogInfo(fmt.Sprintf("Файл бэкапа '%s' для базы '%s' успешно удален.", progress.BackupFilePath, dbName))
	}

	// Обновляем статус в глобальной карте
	BackupProgressesMutex.Lock()
	if progress := BackupProgresses[dbName]; progress != nil {
		progress.Status = "cancelled"
		progress.EndTime = time.Now()
		progress.Error = "Отменено пользователем"
	}
	BackupProgressesMutex.Unlock()
	
	delete(BackupProgresses, dbName) // Удаляем запись из карты
	return nil
}

// getRestoreProgress - Возвращает текущий прогресс восстановления для указанной БД
func getRestoreProgress(dbName string) *restoreProgress {
	RestoreProgressesMutex.Lock()
	defer RestoreProgressesMutex.Unlock()
	return RestoreProgresses[dbName]
}

// getBackupProgress - Возвращает текущий прогресс создания бэкапа для указанной БД
func getBackupProgress(dbName string) *backupProgress {
	BackupProgressesMutex.Lock()
	defer BackupProgressesMutex.Unlock()
	return BackupProgresses[dbName]
}

// checkAndCreateBackupDir - Проверяет существование каталога для бэкапов и создает его, если нет
func checkAndCreateBackupDir(dbName string) (string, error) {
    backupDir := filepath.Join("/mnt/sql_backups", dbName) // Используем /mnt/sql_backups как корневой каталог
    
    if _, err := os.Stat(backupDir); os.IsNotExist(err) {
        LogInfo(fmt.Sprintf("Каталог бэкапов '%s' не существует. Создаю...", backupDir))
        if err := os.MkdirAll(backupDir, 0755); err != nil {
            return "", fmt.Errorf("ошибка создания каталога бэкапов '%s': %w", backupDir, err)
        }
        LogInfo(fmt.Sprintf("Каталог бэкапов '%s' успешно создан.", backupDir))
    } else if err != nil {
        return "", fmt.Errorf("ошибка проверки каталога бэкапов '%s': %w", backupDir, err)
    } else {
        LogDebug(fmt.Sprintf("Каталог бэкапов '%s' уже существует.", backupDir))
    }
    return backupDir, nil
}

// getBackupSessionID - Получает session_id активного процесса BACKUP для указанной БД
func getBackupSessionID(dbName string) (int, error) {
    query := `
        SELECT r.session_id
        FROM sys.dm_exec_requests r
        CROSS APPLY sys.dm_exec_sql_text(r.sql_handle) t
        WHERE r.command LIKE '%BACKUP%'
          AND t.text LIKE @p1;
    `
    // Параметр для LIKE, чтобы избежать SQL-инъекций и корректно искать имя базы
    param := fmt.Sprintf("%%BACKUP DATABASE [%s]%%", dbName)
    
    var sessionID int
    err := dbConn.QueryRow(query, param).Scan(&sessionID)
    if err == sql.ErrNoRows {
        return 0, fmt.Errorf("активный процесс BACKUP для базы '%s' не найден", dbName)
    }
    if err != nil {
        return 0, fmt.Errorf("ошибка при запросе session_id для BACKUP базы '%s': %w", dbName, err)
    }
    return sessionID, nil
}

// GetDatabases - Получение списка пользовательских баз данных
func GetDatabases() ([]Database, error) {
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

		// Дополнительная проверка: если база находится в процессе восстановления через наше приложение
		RestoreProgressesMutex.Lock()
		restoreProgress, restoreExists := RestoreProgresses[db.Name]
		RestoreProgressesMutex.Unlock()

		if restoreExists && (restoreProgress.Status == "pending" || restoreProgress.Status == "in_progress") {
			db.State = "restoring" // Переопределяем статус, если наше приложение активно восстанавливает
		}

		// Дополнительная проверка: если база находится в процессе создания бэкапа через наше приложение
		BackupProgressesMutex.Lock()
		backupProgress, backupExists := BackupProgresses[db.Name]
		BackupProgressesMutex.Unlock()

		if backupExists && (backupProgress.Status == "pending" || backupProgress.Status == "in_progress") {
			db.State = "backing_up" // Переопределяем статус, если наше приложение активно создает бэкап
		}

		databases = append(databases, db)
	}
	
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка после итерации строк БД: %w", err)
	}

	return databases, nil
}
