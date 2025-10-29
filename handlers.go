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

// Вспомогательная функция для проверки существования базы данных на сервере.
func checkDatabaseExists(dbName string) (bool, error) {
    // Используем функцию getDatabases, определенную в db_ops.go
    databases, err := getDatabases() 
    if err != nil {
        return false, fmt.Errorf("ошибка при получении списка баз данных: %w", err)
    }

    for _, db := range databases {
        if strings.EqualFold(db.Name, dbName) {
            return true, nil
        }
    }
    return false, nil
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

// Проверяет, примонтирована ли указанная точка монтирования.
func checkMountStatus(mountPoint string) bool {
    cmd := exec.Command("mountpoint", "-q", mountPoint)
    // Возвращаем результат сравнения напрямую: true, если команда выполнилась успешно (код выхода 0).
    return cmd.Run() == nil 
}

// --- Middleware и API Обработчики ---

// Middleware для проверки IP-адреса клиента
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        remoteAddr := r.RemoteAddr
        ip, _, err := net.SplitHostPort(remoteAddr)
        if err != nil {
            ip = remoteAddr
        }

        isAllowed := false
        for _, allowed := range appConfig.Whitelist {
            if allowed == ip {
                isAllowed = true
                break
            }
        }

        if !isAllowed {
            LogError(fmt.Sprintf("Доступ запрещен для клиента: %s", ip))
            http.Error(w, "Доступ запрещен. Ваш IP/хост не в белом списке.", http.StatusForbidden)
            return
        }

        next.ServeHTTP(w, r)
    }
}

// API для получения списка баз данных
func handleGetDatabases(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    databases, err := getDatabases() // Определена в db_ops.go
    if err != nil {
        LogError(fmt.Sprintf("Ошибка получения списка баз: %v", err))
        http.Error(w, "Ошибка сервера при получении списка баз", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(databases)
}

// API для получения списка бэкапов
func handleGetBackups(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    mountPoint := appConfig.SMBShare.LocalMountPoint
    
    // Проверка и монтирование SMB-шары (логика из предыдущих шагов)
    if !checkMountStatus(mountPoint) {
        // ... (логика монтирования через systemctl) ...
        mountUnitName := strings.ReplaceAll(mountPoint[1:], "/", "-") + ".mount" 
        cmd := exec.Command("sudo", "systemctl", "start", mountUnitName)
        if _, err := cmd.CombinedOutput(); err != nil {
            LogError(fmt.Sprintf("Ошибка systemctl start %s: %v", mountUnitName, err))
            http.Error(w, "Ошибка сервера при перемонтировании SMB-шары", http.StatusInternalServerError)
            return
        }
        time.Sleep(3 * time.Second) 
        if !checkMountStatus(mountPoint) {
            LogError(fmt.Sprintf("Повторное монтирование %s не удалось после systemctl start.", mountPoint))
            http.Error(w, "Ошибка сервера: перемонтирование SMB-шары не удалось.", http.StatusInternalServerError)
            return
        }
        LogInfo(fmt.Sprintf("SMB-шара успешно перемонтирована на %s.", mountPoint))
    }

    backupFiles, err := getBackupBaseNames(mountPoint, appConfig.App.BackupBlacklist)
    if err != nil {
        LogError(fmt.Sprintf("Ошибка при получении списка названий баз бэкапов: %v", err))
        http.Error(w, "Ошибка сервера при чтении бэкапов", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(backupFiles)
}

// API для запуска восстановления
func handleStartRestore(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    var req RestoreRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Неверный формат запроса", http.StatusBadRequest)
        return
    }
    
    // Проверка существования БД (используется checkDatabaseExists)
    exists, err := checkDatabaseExists(req.NewDBName)
    if err != nil {
        LogError(fmt.Sprintf("Ошибка при проверке существования базы данных %s: %v", req.NewDBName, err))
        http.Error(w, fmt.Sprintf("Ошибка проверки существования базы данных: %v", err), http.StatusInternalServerError)
        return
    }
    if exists {
         LogInfo(fmt.Sprintf("База данных '%s' уже существует. Восстановление будет выполнено с REPLACE.", req.NewDBName))
    }
    
    // Обработка времени восстановления (PIRT)
    var restoreTime *time.Time = nil
    if req.RestoreDateTime != "" {
        // Формат, который использует фронтенд: DD.MM.YYYY HH:MM:SS
        t, err := time.Parse("02.01.2006 15:04:05", req.RestoreDateTime)
        if err != nil {
            LogWarning(fmt.Sprintf("Не удалось распарсить дату/время '%s': %v. Восстановление будет полным.", req.RestoreDateTime, err))
        } else {
            restoreTime = &t
        }
    }
    
    // Запуск асинхронного восстановления (startRestore определена в db_ops.go)
    if err := startRestore(req.BackupBaseName, req.NewDBName, restoreTime); err != nil { 
        LogError(fmt.Sprintf("Не удалось запустить восстановление: %v", err))
        http.Error(w, fmt.Sprintf("Не удалось запустить восстановление: %v", err), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusAccepted)
    fmt.Fprintf(w, "Процесс восстановления базы данных '%s' запущен.", req.NewDBName)
}

// API для отмены восстановления
func handleCancelRestoreProcess(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    dbName := r.URL.Query().Get("name")
    if dbName == "" {
        http.Error(w, "Имя базы данных не указано.", http.StatusBadRequest)
        return
    }
    
    // Удаление базы данных (deleteDatabase определена в db_ops.go)
    if err := deleteDatabase(dbName); err != nil { 
        LogError(fmt.Sprintf("Не удалось отменить восстановление (удалить БД %s): %v", dbName, err))
        http.Error(w, fmt.Sprintf("Ошибка отмены восстановления: %v", err), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    LogInfo(fmt.Sprintf("Восстановление базы данных '%s' отменено (БД удалена).", dbName))
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
        LogError(fmt.Sprintf("Не удалось удалить базу данных %s: %v", dbName, err))
        http.Error(w, fmt.Sprintf("Ошибка удаления базы данных: %v", err), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    LogInfo(fmt.Sprintf("База данных '%s' успешно удалена.", dbName))
}

// API для получения краткого лога
func handleGetBriefLog(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    logMutex.Lock() 
    defer logMutex.Unlock()

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(briefLog) 
}