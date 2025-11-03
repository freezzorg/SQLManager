package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/freezzorg/SQLManager/internal/config"
	"github.com/freezzorg/SQLManager/internal/logging"
	"github.com/freezzorg/SQLManager/internal/utils"
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

// --- Утилиты для PIRT ---

// GetBackupLogicalFiles - Получение логических имен файлов из бэкапа (для формирования MOVE)
func GetBackupLogicalFiles(db *sql.DB, backupPath string) ([]BackupLogicalFile, error) {
	query := fmt.Sprintf("RESTORE FILELISTONLY FROM DISK = N'%s'", backupPath)

	rows, err := db.Query(query)
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
		// Используем sql.RawBytes для всех столбцов
		columns := make([]sql.RawBytes, numColumns)
		scanArgs := make([]interface{}, numColumns)
	for i := range columns {
			scanArgs[i] = &columns[i]
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("ошибка сканирования строки RESTORE FILELISTONLY: %w", err)
		}

		// Извлекаем нужные значения из sql.RawBytes
		var logicalName, fileType string
		// Предполагаем, что LogicalName - это первый столбец, а Type - третий.
		// Это соответствует выводу RESTORE FILELISTONLY.
		if len(columns[0]) > 0 { // LogicalName
			logicalName = string(columns[0])
		}
		if len(columns[2]) > 0 { // Type (D, L)
			fileType = string(columns[2])
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
		return nil, fmt.Errorf("ошибка после итерации строк RESTORE FILELISTONLY: %w", err)
	}

	if len(logicalFiles) == 0 {
		return nil, fmt.Errorf("RESTORE FILELISTONLY не вернул DATA или LOG файлы")
	}

	return logicalFiles, nil
}

// GetBackupHeaderInfo - Новая функция для выполнения RESTORE HEADERONLY и получения метаданных
func GetBackupHeaderInfo(db *sql.DB, backupPath string) ([]BackupFileSequence, error) {
	query := fmt.Sprintf("RESTORE HEADERONLY FROM DISK = N'%s'", backupPath)

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("ошибка при запросе RESTORE HEADERONLY для %s: %w", backupPath, err)
	}
	defer rows.Close()

	columnNames, colErr := rows.Columns()
	if colErr != nil {
		return nil, fmt.Errorf("ошибка получения имен столбцов для HEADERONLY: %w", colErr)
	}

	colIndexMap := make(map[string]int)
	for i, name := range columnNames {
		colIndexMap[name] = i
	}

	requiredColumns := []string{"BackupType", "DatabaseName", "BackupFinishDate", "Position", "FirstLSN", "LastLSN", "DatabaseBackupLSN"}
	for _, col := range requiredColumns {
		if _, exists := colIndexMap[col]; !exists {
			return nil, fmt.Errorf("в результатах RESTORE HEADERONLY отсутствует критически важный столбец: %s", col)
		}
	}

	var allHeaders []BackupFileSequence
	for rows.Next() {
		var (
			backupTypeRaw sql.RawBytes
			databaseNameRaw sql.RawBytes
			backupFinishDateRaw sql.RawBytes
			positionRaw sql.RawBytes
			firstLSNRaw sql.RawBytes
			lastLSNRaw sql.RawBytes
			databaseBackupLSNRaw sql.RawBytes
		)

		// Создаем слайс для сканирования всех столбцов в sql.RawBytes
		rawColumns := make([]sql.RawBytes, len(columnNames))
		scanArgs := make([]interface{}, len(columnNames))
		for i := range rawColumns {
			scanArgs[i] = &rawColumns[i]
	}

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("ошибка сканирования строки RESTORE HEADERONLY для %s: %w", backupPath, err)
		}

		// Присваиваем RawBytes соответствующим переменным, используя colIndexMap
		if idx, ok := colIndexMap["BackupType"]; ok {
			backupTypeRaw = rawColumns[idx]
		}
		if idx, ok := colIndexMap["DatabaseName"]; ok {
			databaseNameRaw = rawColumns[idx]
		}
		if idx, ok := colIndexMap["BackupFinishDate"]; ok {
			backupFinishDateRaw = rawColumns[idx]
		}
		if idx, ok := colIndexMap["Position"]; ok {
			positionRaw = rawColumns[idx]
		}
		if idx, ok := colIndexMap["FirstLSN"]; ok {
			firstLSNRaw = rawColumns[idx]
		}
		if idx, ok := colIndexMap["LastLSN"]; ok {
			lastLSNRaw = rawColumns[idx]
		}
		if idx, ok := colIndexMap["DatabaseBackupLSN"]; ok {
			databaseBackupLSNRaw = rawColumns[idx]
		}

		// Извлечение и преобразование данных из sql.RawBytes
		var (
			backupTypeStr string
			databaseName  string
			finishDate    time.Time
			position      int
			firstLSN      string
			lastLSN       string
			dbBackupLSN   string
		)

		if len(backupTypeRaw) > 0 {
			backupTypeStr = string(backupTypeRaw)
		}
		if len(databaseNameRaw) > 0 {
			databaseName = string(databaseNameRaw)
	}
		if len(backupFinishDateRaw) > 0 {
			// Попытка парсинга с миллисекундами
			finishDate, err = time.Parse("2006-01-02T15:04:05.000Z", string(backupFinishDateRaw))
			if err != nil {
				// Если не удалось, попытка парсинга без миллисекунд
				finishDate, err = time.Parse("2006-01-02T15:04:05Z", string(backupFinishDateRaw))
				if err != nil {
					finishDate = time.Time{}
				}
			}
		}
		if len(positionRaw) > 0 {
			position, err = strconv.Atoi(string(positionRaw))
			if err != nil {
				position = 0
			}
		}
		if len(firstLSNRaw) > 0 {
			firstLSN = string(firstLSNRaw)
	}
		if len(lastLSNRaw) > 0 {
			lastLSN = string(lastLSNRaw)
		}
	if len(databaseBackupLSNRaw) > 0 {
			dbBackupLSN = string(databaseBackupLSNRaw)
		}

		var fileType string
		backupTypeInt, err := strconv.Atoi(backupTypeStr)
		if err != nil {
			continue
		}

		switch backupTypeInt {
		case 1:
			fileType = "FULL"
		case 5:
			fileType = "DIFF"
		case 2:
			fileType = "LOG"
		default:
			continue
		}

		allHeaders = append(allHeaders, BackupFileSequence{
			Path:              backupPath,
			Type:              fileType,
			BackupFinishDate:  finishDate,
			DatabaseName:      databaseName,
			Position:          position,
			FirstLSN:          firstLSN,
			LastLSN:           lastLSN,
			DatabaseBackupLSN: dbBackupLSN,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка после итерации строк RESTORE HEADERONLY: %w", err)
	}

	return allHeaders, nil
}

// BuildRestoreChain - Определяет последовательность бэкапов для восстановления на указанный момент времени
func BuildRestoreChain(baseName string, allHeaders []BackupFileSequence, restoreTime *time.Time) ([]BackupFileSequence, error) {
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
				}
			}
		}
	}

	// 5. Формируем последовательность: FULL -> [DIFF] -> [LOGs]
	filesToRestore := make([]BackupFileSequence, 0)
	
	// Добавляем FULL
	filesToRestore = append(filesToRestore, *fullBackup)

	// Добавляем DIFF, если он найден
	if diffBackup != nil {
		filesToRestore = append(filesToRestore, *diffBackup)
	}

	// Определяем время, после которого должны идти LOG файлы (после FULL или DIFF)
	lastBackupTime := fullBackup.BackupFinishDate
	if diffBackup != nil {
		lastBackupTime = diffBackup.BackupFinishDate
	}

	// 6. Добавляем LOG бэкапы после последнего добавленного (FULL или DIFF), в хронологическом порядке
	for _, file := range filteredBackups {
		if file.Type == "LOG" && file.BackupFinishDate.After(lastBackupTime) {
			// Если задано время восстановления
			if restoreTime != nil {
				// Если текущий лог завершен после желаемого времени восстановления,
				// то этот лог может содержать нужную точку восстановления.
				// Добавляем его и прерываем цикл.
				if file.BackupFinishDate.After(*restoreTime) {
					filesToRestore = append(filesToRestore, file)
					break
				}
				// В противном случае добавляем LOG, если он до времени восстановления
				if file.BackupFinishDate.Before(*restoreTime) || file.BackupFinishDate.Equal(*restoreTime) {
					filesToRestore = append(filesToRestore, file)
				}
			} else {
				// Если время восстановления не задано, добавляем все LOG файлы
				filesToRestore = append(filesToRestore, file)
			}
	}
	}
	
	if len(filesToRestore) == 0 {
		return nil, fmt.Errorf("не удалось сформировать цепочку восстановления")
	}

	return filesToRestore, nil
}

// GetRestoreSequence - Определяет последовательность бэкапов для восстановления на указанный момент времени
// (обертка для чтения файлов и построения цепочки)
func GetRestoreSequence(db *sql.DB, baseName string, restoreTime *time.Time, smbSharePath string) ([]BackupFileSequence, error) {
	// 1. Проверяем и монтируем SMB-шару при необходимости
	if err := utils.EnsureSMBMounted(smbSharePath); err != nil {
		return nil, fmt.Errorf("не удалось смонтировать SMB-шару %s: %w", smbSharePath, err)
	}
	
	// Формируем путь к директории бэкапов
	backupDir := filepath.Join(smbSharePath, baseName)
	
	// Читаем все файлы бэкапов для этой базы
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
			headers, err := GetBackupHeaderInfo(db, fullPath)
			if err != nil {
				// Пропускаем ошибку, но продолжаем
				continue
			}
			allHeaders = append(allHeaders, headers...)
		}
	}

	if len(allHeaders) == 0 {
		return nil, fmt.Errorf("в директории %s не найдено валидных файлов бэкапов", backupDir)
	}

	// 2. Строим цепочку восстановления
	filesToRestore, err := BuildRestoreChain(baseName, allHeaders, restoreTime)
	if err != nil {
		return nil, err
	}
	
	return filesToRestore, nil
}

// StartRestore - Запускает асинхронный процесс восстановления базы данных
func StartRestore(db *sql.DB, backupBaseName, newDBName string, restoreTime *time.Time, smbSharePath, restorePath string) error {
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
		startFile := filesToRestore[0]
		
		// Получаем логические имена файлов из первого файла в цепочке (startFile)
		logicalFiles, err := GetBackupLogicalFiles(db, startFile.Path)
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
				logging.LogDebug("Последний файл в цепочке, используется RECOVERY (без STOPAT).")
			} else {
				// Не последний бэкап: всегда используем NORECOVERY без STOPAT
				recoveryOption = "NORECOVERY"
				logging.LogDebug(fmt.Sprintf("Промежуточный файл в цепочке, используется NORECOVERY (без STOPAT). Тип: %s", file.Type))
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
			
			logging.LogDebug(fmt.Sprintf("Выполнение RESTORE (%d/%d): %s", i+1, len(filesToRestore), restoreQuery))
			
			if _, err := db.Exec(restoreQuery); err != nil {
				logging.LogDebug(fmt.Sprintf("Прерывание RESTORE для %s (файл: %s, позиция: %d): %v", newDBName, file.Path, file.Position, err))
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

// StartBackup - Запускает асинхронный процесс создания полного бэкапа базы данных
func StartBackup(db *sql.DB, dbName string, smbSharePath string) error {
	BackupProgressesMutex.Lock()
	BackupProgresses[dbName] = &BackupProgress{
		Status:    "pending",
		StartTime: time.Now(),
	}
	BackupProgressesMutex.Unlock()

	logging.LogWebInfo(fmt.Sprintf("Начато создание бэкапа базы '%s'...", dbName))

	go func() {
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

	}()

	return nil
}

// DeleteDatabase - Удаление базы данных
func DeleteDatabase(db *sql.DB, dbName string) error {
	// 1. Удаляем базу данных.
	// Перевод в SINGLE_USER не требуется, так как база не используется во время восстановления.
	deleteQuery := fmt.Sprintf("DROP DATABASE [%s]", dbName)
	if _, err := db.Exec(deleteQuery); err != nil {
		return fmt.Errorf("ошибка DROP DATABASE для БД %s: %w", dbName, err)
	}
	
	return nil
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

// GetRestoreProgress - Возвращает текущий прогресс восстановления для указанной БД
func GetRestoreProgress(dbName string) *RestoreProgress {
	RestoreProgressesMutex.Lock()
	defer RestoreProgressesMutex.Unlock()
	return RestoreProgresses[dbName]
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
