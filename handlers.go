package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Структура для запроса на восстановление, согласованная с фронтендом
type RestoreRequest struct {
	BackupBaseName  string `json:"backupBaseName"`  // Имя директории бэкапа (например, "Edelweis")
	NewDBName       string `json:"newDbName"`       // Имя новой/восстанавливаемой базы
	RestoreDateTime string `json:"restoreDateTime"` // Дата и время для PIRT (DD.MM.YYYY HH:MM:SS)
}

// Структура для запроса на бэкап
type BackupRequest struct {
    DBName string `json:"dbName"` // Имя базы данных для бэкапа
}

// --- Вспомогательные функции ---

// Middleware для проверки IP-адреса клиента
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        remoteAddr := r.RemoteAddr
        ip, _, err := net.SplitHostPort(remoteAddr)
        if err != nil {
            // Если не удалось распарсить адрес, берем его как есть (например, из прокси)
            ip = remoteAddr
        }

        // Проверка IP в белом списке
        isAllowed := false
        for _, allowed := range appConfig.Whitelist {
            if allowed == ip {
                isAllowed = true
                break
            }
        }

        if !isAllowed {
            LogWebError(fmt.Sprintf("Доступ запрещен для клиента: %s", ip))
            http.Error(w, "Доступ запрещен. Ваш IP/хост не в белом списке.", http.StatusForbidden)
            return
        }

        // Продолжаем выполнение, если разрешено
        next.ServeHTTP(w, r)
    }
}

// Вспомогательная функция для получения списка имен директорий (названий баз)
func getBackupBaseNames(root string, blacklist []string) ([]BackupFile, error) {
    var baseNames []BackupFile
    entries, err := os.ReadDir(root)
    if err != nil {
        return nil, fmt.Errorf("ошибка чтения каталога бэкапов %s: %w", root, err)
    }

    for _, entry := range entries {
        if entry.IsDir() {
            dirName := entry.Name()
            // Проверка на черный список
            isBlacklisted := false
            for _, blName := range blacklist {
                if strings.Contains(dirName, blName) {
                    isBlacklisted = true
                    LogDebug(fmt.Sprintf("Директория бэкапа '%s' находится в черном списке и будет пропущена.", dirName))
                    break
                }
            }
            if isBlacklisted {
                continue
            }
            LogDebug(fmt.Sprintf("Добавлена директория бэкапа: '%s'", dirName)) 
            baseNames = append(baseNames, BackupFile{BaseName: dirName})
        }
    }
    return baseNames, nil
}

// --- API-обработчики ---

// AppHandlers - Структура для хранения зависимостей обработчиков, таких как *sql.DB
type AppHandlers struct {
	DB *sql.DB
}

// API для получения списка баз данных
func (h *AppHandlers) handleGetDatabases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	databases, err := GetDatabases(h.DB)
	if err != nil {
		LogWebError(fmt.Sprintf("Не удалось получить список баз данных: %v", err))
		http.Error(w, "Ошибка сервера при получении списка баз данных", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(databases)
}

// API для удаления базы данных
func (h *AppHandlers) handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	dbName := r.URL.Query().Get("name")
	if dbName == "" {
		http.Error(w, "Имя базы данных не указано.", http.StatusBadRequest)
		return
	}
	// Валидация dbName
	if !isValidDBName(dbName) {
		LogWebError(fmt.Sprintf("Недопустимое имя базы данных: %s", dbName))
		http.Error(w, "Недопустимое имя базы данных.", http.StatusBadRequest)
		return
	}

	if err := deleteDatabase(h.DB, dbName); err != nil {
		LogWebError(fmt.Sprintf("Не удалось удалить базу данных %s: %v", dbName, err))
		http.Error(w, fmt.Sprintf("Ошибка удаления базы данных: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	LogWebInfo(fmt.Sprintf("База данных '%s' успешно удалена.", dbName))
	json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("База данных '%s' успешно удалена.", dbName)})
}

// API для получения списка бэкапов (директорий)
func (h *AppHandlers) handleGetBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	mountPoint := appConfig.SMBShare.LocalMountPoint
	// Проверка существования точки монтирования без попытки монтирования
	if _, err := os.Stat(mountPoint); os.IsNotExist(err) {
		LogWebError(fmt.Sprintf("Точка монтирования SMB-шары %s не существует или недоступна. Убедитесь, что она смонтирована через systemd.", mountPoint))
		http.Error(w, "Точка монтирования SMB-шары недоступна. Убедитесь, что она смонтирована.", http.StatusInternalServerError)
		return
	}

	// Получаем список директорий (базовых имен бэкапов)
	baseNames, err := getBackupBaseNames(mountPoint, appConfig.App.BackupBlacklist)
	if err != nil {
		LogWebError(fmt.Sprintf("Не удалось получить список бэкапов: %v", err))
		http.Error(w, "Ошибка сервера при получении списка бэкапов", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(baseNames)
}

// API для запуска восстановления базы данных (handleStartRestore)
func (h *AppHandlers) handleStartRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req RestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат запроса: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.BackupBaseName == "" || req.NewDBName == "" {
		http.Error(w, "Не указано имя бэкапа или имя восстанавливаемой базы.", http.StatusBadRequest)
		return
	}
	// Валидация имен
	if !isValidDBName(req.NewDBName) {
		LogWebError(fmt.Sprintf("Недопустимое имя новой базы данных: %s", req.NewDBName))
		http.Error(w, "Недопустимое имя новой базы данных.", http.StatusBadRequest)
		return
	}
	if !isValidBackupBaseName(req.BackupBaseName) {
		LogWebError(fmt.Sprintf("Недопустимое имя базового бэкапа: %s", req.BackupBaseName))
		http.Error(w, "Недопустимое имя базового бэкапа.", http.StatusBadRequest)
		return
	}

	var restoreTime *time.Time
	if req.RestoreDateTime != "" {
		t, err := time.Parse("2006-01-02 15:04:05", req.RestoreDateTime)
		if err != nil {
			LogWebError(fmt.Sprintf("Ошибка парсинга времени восстановления %s: %v", req.RestoreDateTime, err))
			http.Error(w, fmt.Sprintf("Неверный формат даты/времени. Ожидается: YYYY-MM-DD HH:MM:SS. Ошибка: %v", err), http.StatusBadRequest)
			return
		}
		restoreTime = &t
	}

	if err := startRestore(h.DB, req.BackupBaseName, req.NewDBName, restoreTime); err != nil {
		LogWebError(fmt.Sprintf("Не удалось начать восстановление базы данных %s: %v", req.NewDBName, err))
		http.Error(w, fmt.Sprintf("Ошибка запуска восстановления: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Восстановление базы данных '%s' из бэкапа '%s' запущено.", req.NewDBName, req.BackupBaseName)})
}

// API для запуска создания бэкапа базы данных
func (h *AppHandlers) handleStartBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req BackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный формат запроса: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.DBName == "" {
		http.Error(w, "Не указано имя базы данных для бэкапа.", http.StatusBadRequest)
		return
	}
	// Валидация dbName
	if !isValidDBName(req.DBName) {
		LogWebError(fmt.Sprintf("Недопустимое имя базы данных для бэкапа: %s", req.DBName))
		http.Error(w, "Недопустимое имя базы данных для бэкапа.", http.StatusBadRequest)
		return
	}

	if err := startBackup(h.DB, req.DBName); err != nil {
		LogWebError(fmt.Sprintf("Не удалось начать создание бэкапа базы данных %s: %v", req.DBName, err))
		http.Error(w, fmt.Sprintf("Ошибка запуска создания бэкапа: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Создание бэкапа базы данных '%s' запущено.", req.DBName)})
}

// API для отмены восстановления базы данных (УДАЛЕНИЕ БД)
func (h *AppHandlers) handleCancelRestoreProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	dbName := r.URL.Query().Get("name")
	if dbName == "" {
		http.Error(w, "Имя базы данных не указано.", http.StatusBadRequest)
		return
	}
	// Валидация dbName
	if !isValidDBName(dbName) {
		LogWebError(fmt.Sprintf("Недопустимое имя базы данных для отмены восстановления: %s", dbName))
		http.Error(w, "Недопустимое имя базы данных.", http.StatusBadRequest)
		return
	}

	if err := cancelRestoreProcess(h.DB, dbName); err != nil {
		LogWebError(fmt.Sprintf("Не удалось отменить восстановление (удалить БД %s): %v", dbName, err))
		http.Error(w, fmt.Sprintf("Ошибка отмены восстановления: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	LogWebInfo(fmt.Sprintf("Восстановление базы данных '%s' отменено (БД удалена).", dbName))
	json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Восстановление базы данных '%s' отменено (БД удалена).", dbName)})
}

// API для получения краткого лога
func handleGetBriefLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	logMutex.Lock()
	defer logMutex.Unlock()
	json.NewEncoder(w).Encode(briefLog)
}

// API для получения прогресса восстановления конкретной базы данных
func (h *AppHandlers) handleGetRestoreProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	dbName := r.URL.Query().Get("name")
	if dbName == "" {
		http.Error(w, "Имя базы данных не указано.", http.StatusBadRequest)
		return
	}
	// Валидация dbName
	if !isValidDBName(dbName) {
		LogWebError(fmt.Sprintf("Недопустимое имя базы данных для получения прогресса восстановления: %s", dbName))
		http.Error(w, "Недопустимое имя базы данных.", http.StatusBadRequest)
		return
	}

	progress := getRestoreProgress(dbName)
	if progress == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&restoreProgress{Status: "not_found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(progress)
}

// API для получения прогресса создания бэкапа конкретной базы данных
func (h *AppHandlers) handleGetBackupProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	dbName := r.URL.Query().Get("name")
	if dbName == "" {
		http.Error(w, "Имя базы данных не указано.", http.StatusBadRequest)
		return
	}
	// Валидация dbName
	if !isValidDBName(dbName) {
		LogWebError(fmt.Sprintf("Недопустимое имя базы данных для получения прогресса бэкапа: %s", dbName))
		http.Error(w, "Недопустимое имя базы данных.", http.StatusBadRequest)
		return
	}

	progress := getBackupProgress(h.DB, dbName)
	if progress == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&backupProgress{Status: "not_found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(progress)
}

// isValidDBName - Простая валидация имени базы данных
func isValidDBName(name string) bool {
	// Имя базы данных должно состоять из букв, цифр, подчеркиваний и дефисов.
	// Длина от 1 до 128 символов (стандартное ограничение SQL Server).
	// Не должно начинаться с цифры или дефиса.
	// Более строгая валидация может включать проверку на зарезервированные слова SQL.
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	// Проверка на первый символ
	firstChar := rune(name[0])
	if !((firstChar >= 'a' && firstChar <= 'z') || (firstChar >= 'A' && firstChar <= 'Z') || firstChar == '_') {
		return false
	}
	// Проверка остальных символов
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// isValidBackupBaseName - Простая валидация имени базового бэкапа (имени директории)
func isValidBackupBaseName(name string) bool {
	// Имя директории бэкапа должно быть безопасным для файловой системы и не содержать спецсимволов.
	// Для простоты, ограничимся буквенно-цифровыми символами, подчеркиваниями и дефисами.
	// Длина от 1 до 255 символов (стандартное ограничение для имен файлов/директорий).
	if len(name) == 0 || len(name) > 255 {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
