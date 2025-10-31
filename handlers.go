package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Структура для запроса на восстановление, согласованная с фронтендом
type RestoreRequest struct {
	BackupBaseName  string `json:"backupBaseName"`  // Имя директории бэкапа (например, "Edelweis")
	NewDBName       string `json:"newDbName"`       // Имя новой/восстанавливаемой базы
	RestoreDateTime string `json:"restoreDateTime"` // Дата и время для PIRT (DD.MM.YYYY HH:MM:SS)
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

// API для получения списка баз данных
func handleGetDatabases(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    databases, err := GetDatabases()
    if err != nil {
        LogWebError(fmt.Sprintf("Не удалось получить список баз данных: %v", err))
        http.Error(w, "Ошибка сервера при получении списка баз данных", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(databases)
}

// API для удаления базы данных
func handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    dbName := r.URL.Query().Get("name")
    if dbName == "" {
        http.Error(w, "Имя базы данных не указано.", http.StatusBadRequest)
        return
    }
    
    if err := deleteDatabase(dbName); err != nil {
        LogWebError(fmt.Sprintf("Не удалось удалить базу данных %s: %v", dbName, err))
        http.Error(w, fmt.Sprintf("Ошибка удаления базы данных: %v", err), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    LogWebInfo(fmt.Sprintf("База данных '%s' успешно удалена.", dbName))
    json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("База данных '%s' успешно удалена.", dbName)})
}


// API для получения списка бэкапов (директорий)
func handleGetBackups(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    // Проверка точки монтирования SMB-шары
    mountPoint := appConfig.SMBShare.LocalMountPoint
    if _, err := os.Stat(mountPoint); os.IsNotExist(err) {
        // Если точка монтирования не существует, пытаемся ее создать и смонтировать
        LogInfo(fmt.Sprintf("Точка монтирования %s не существует или недоступна. Попытка монтирования...", mountPoint))
        
        // Попытка монтирования (требует прав sudo или настройки /etc/fstab)
        // В реальном приложении это может быть сложной операцией без root-прав. 
        // Здесь имитируем вызов mount с настройками из конфига.
        // Предполагается, что 'mount.cifs' доступен и настроен для работы без интерактивного ввода.
        cmd := exec.Command("sudo", "mount", "-t", "cifs", appConfig.SMBShare.RemotePath, mountPoint, 
                            "-o", fmt.Sprintf("credentials=/etc/smb-creds,domain=%s,uid=%d,gid=%d,rw", appConfig.SMBShare.Domain, os.Getuid(), os.Getgid())) 

        output, mountErr := cmd.CombinedOutput()
        if mountErr != nil {
            errMsg := fmt.Sprintf("Ошибка монтирования SMB-шары (%s): %v, Вывод: %s", mountPoint, mountErr, string(output))
            LogWebError(errMsg)
            // Возвращаем пустой список и ошибку
            http.Error(w, "Ошибка монтирования SMB-шары: " + errMsg, http.StatusInternalServerError)
            return
        }
        LogWebInfo(fmt.Sprintf("SMB-шара успешно смонтирована в %s.", mountPoint))
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
func handleStartRestore(w http.ResponseWriter, r *http.Request) { 
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

    // Парсинг RestoreDateTime
    var restoreTime *time.Time
	if req.RestoreDateTime != "" {
		// ИСПРАВЛЕНИЕ: Новый формат парсинга, который ожидаем с фронтенда: YYYY-MM-DD HH:MM:SS
        // Используем 2006-01-02 15:04:05, чтобы избежать ошибки
		t, err := time.Parse("2006-01-02 15:04:05", req.RestoreDateTime)
		if err != nil {
			// Логируем ошибку парсинга времени
			LogWebError(fmt.Sprintf("Ошибка парсинга времени восстановления %s: %v", req.RestoreDateTime, err))
			http.Error(w, fmt.Sprintf("Неверный формат даты/времени. Ожидается: YYYY-MM-DD HH:MM:SS. Ошибка: %v", err), http.StatusBadRequest)
			return
		}
		restoreTime = &t
	}

    // Вызываем обновленную функцию
    if err := startRestore(req.BackupBaseName, req.NewDBName, restoreTime); err != nil {
        LogWebError(fmt.Sprintf("Не удалось начать восстановление базы данных %s: %v", req.NewDBName, err))
        http.Error(w, fmt.Sprintf("Ошибка запуска восстановления: %v", err), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Восстановление базы данных '%s' из бэкапа '%s' запущено.", req.NewDBName, req.BackupBaseName)})
}


// API для отмены восстановления базы данных (УДАЛЕНИЕ БД)
func handleCancelRestoreProcess(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { // Изменен на POST для безопасности
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    dbName := r.URL.Query().Get("name")
    if dbName == "" {
        http.Error(w, "Имя базы данных не указано.", http.StatusBadRequest)
        return
    }
    
    if err := cancelRestoreProcess(dbName); err != nil {
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
func handleGetRestoreProgress(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    dbName := r.URL.Query().Get("name")
    if dbName == "" {
        http.Error(w, "Имя базы данных не указано.", http.StatusBadRequest)
        return
    }

    progress := getRestoreProgress(dbName)
    if progress == nil {
        // Если прогресс не найден, возможно, восстановление еще не началось или уже завершено/отменено
        // Возвращаем пустой прогресс или статус "not_found"
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(&restoreProgress{Status: "not_found"})
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(progress)
}
