package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Middleware для проверки IP-адреса клиента [cite: 8]
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
            LogError(fmt.Sprintf("Доступ запрещен для клиента: %s", ip))
            http.Error(w, "Доступ запрещен. Ваш IP/хост не в белом списке.", http.StatusForbidden)
            return
        }

        // Продолжаем выполнение, если разрешено
        next.ServeHTTP(w, r)
    }
}

// API для получения списка баз данных [cite: 9, 10]
func handleGetDatabases(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    databases, err := getDatabases()
    if err != nil {
        LogError(fmt.Sprintf("Ошибка получения списка баз: %v", err))
        http.Error(w, "Ошибка сервера при получении списка баз", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    if debugOutput, err := json.Marshal(databases); err == nil {
        LogDebug(fmt.Sprintf("Отправка списка баз данных: %s", string(debugOutput)))
    } else {
        LogError(fmt.Sprintf("Ошибка маршалинга списка баз данных для отладки: %v", err))
    }
    json.NewEncoder(w).Encode(databases)
    LogDebug("Список баз данных отправлен.")
}

// API для удаления базы данных [cite: 18, 20]
func handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    // Пример получения имени БД из URL-параметра или тела запроса
    dbName := r.URL.Query().Get("name")
    if dbName == "" {
        http.Error(w, "Имя базы данных не указано.", http.StatusBadRequest)
        return
    }

    if err := deleteDatabase(dbName); err != nil {
        LogError(fmt.Sprintf("Не удалось удалить базу данных %s: %v", dbName, err))
        http.Error(w, fmt.Sprintf("Ошибка удаления: %v", err), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    LogInfo(fmt.Sprintf("Удаление базы данных '%s' инициировано/завершено.", dbName))
}

// API для получения краткого лога [cite: 16, 31]
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

// API для запуска восстановления [cite: 24]
func handleRestoreDatabase(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    // 1. Парсинг тела запроса (backupPath, newDBName, restoreTime) [cite: 22, 23]
    var req struct {
        BackupPath  string `json:"backupPath"`
        NewDBName   string `json:"newDbName"`
        RestoreTime string `json:"restoreTime"` // Строка для парсинга даты/времени [cite: 23]
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Неверный формат запроса", http.StatusBadRequest)
        return
    }

    // 2. Проверка существования базы данных для вывода предупреждения [cite: 25]
    exists, err := checkDatabaseExists(req.NewDBName)
    if err != nil {
        LogError(fmt.Sprintf("Ошибка при проверке существования БД %s: %v", req.NewDBName, err))
        http.Error(w, "Ошибка сервера при проверке существования БД", http.StatusInternalServerError)
        return
    }
    if exists {
        LogWarning(fmt.Sprintf("База данных '%s' уже существует. Будет выполнена перезапись.", req.NewDBName))
        // В реальном приложении здесь можно было бы запросить подтверждение у пользователя
    }
    
    // 3. Запуск восстановления
    var rt *time.Time
    if req.RestoreTime != "" {
        // Парсинг даты и времени
        parsedTime, err := time.Parse("02.01.2006 15:04:05", req.RestoreTime)
        if err == nil {
            rt = &parsedTime
        } else {
             LogWarning(fmt.Sprintf("Не удалось распарсить дату/время: %s", req.RestoreTime))
        }
    }

    if err := startRestore(req.BackupPath, req.NewDBName, rt); err != nil {
         LogError(fmt.Sprintf("Не удалось запустить восстановление: %v", err))
         http.Error(w, fmt.Sprintf("Ошибка запуска восстановления: %v", err), http.StatusInternalServerError)
         return
    }

    w.WriteHeader(http.StatusAccepted) // Принято, но выполняется асинхронно
    LogInfo(fmt.Sprintf("Запрос на восстановление базы '%s' принят.", req.NewDBName))
}

// API для получения списка бэкапов [cite: 21, 12]
func handleGetBackups(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    // 1. Монтирование SMB-шары
    mountPoint := appConfig.SMBShare.LocalMountPoint
    remotePath := appConfig.SMBShare.RemotePath // remotePath все еще нужен для логирования

    // Создаем точку монтирования, если ее нет
    if _, err := os.Stat(mountPoint); os.IsNotExist(err) {
        if err := os.MkdirAll(mountPoint, 0755); err != nil {
            LogError(fmt.Sprintf("Не удалось создать точку монтирования %s: %v", mountPoint, err))
            http.Error(w, "Ошибка сервера при подготовке к чтению бэкапов", http.StatusInternalServerError)
            return
        }
    }

    // Проверяем, смонтирована ли уже шара с помощью systemd
    if !checkMountStatus(mountPoint) {
        LogInfo(fmt.Sprintf("SMB-шара %s не смонтирована на %s. Попытка перемонтирования через systemctl...", remotePath, mountPoint))
        // Имя unit-файла systemd для монтирования
        mountUnitName := strings.ReplaceAll(mountPoint[1:], "/", "-") + ".mount" // /mnt/sql_backups -> mnt-sql_backups.mount
        cmd := exec.Command("sudo", "systemctl", "start", mountUnitName)
        output, err := cmd.CombinedOutput()
        if err != nil {
            LogError(fmt.Sprintf("Ошибка systemctl start %s: %v, Output: %s", mountUnitName, err, string(output)))
            http.Error(w, "Ошибка сервера при перемонтировании SMB-шары", http.StatusInternalServerError)
            return
        }
        // Даем системе время на монтирование (может занять несколько секунд)
        time.Sleep(3 * time.Second) 

        if !checkMountStatus(mountPoint) {
            LogError(fmt.Sprintf("Повторное монтирование %s не удалось после systemctl start.", mountPoint))
            http.Error(w, "Ошибка сервера: перемонтирование SMB-шары не удалось.", http.StatusInternalServerError)
            return
        }
        LogInfo(fmt.Sprintf("SMB-шара %s успешно перемонтирована на %s через systemctl.", remotePath, mountPoint))
    } else {
        LogDebug(fmt.Sprintf("SMB-шара %s уже смонтирована на %s.", remotePath, mountPoint))
    }

    // 2. Чтение имен директорий бэкапов из локальной точки монтирования
    backupFiles, err := getBackupBaseNames(mountPoint, appConfig.App.BackupBlacklist)
    if err != nil {
        LogError(fmt.Sprintf("Ошибка при получении списка названий баз бэкапов: %v", err))
        http.Error(w, "Ошибка сервера при чтении бэкапов", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    if debugOutput, err := json.Marshal(backupFiles); err == nil {
        LogDebug(fmt.Sprintf("Отправка списка бэкапов: %s", string(debugOutput)))
    } else {
        LogError(fmt.Sprintf("Ошибка маршалинга списка бэкапов для отладки: %v", err))
    }
    json.NewEncoder(w).Encode(backupFiles)
    LogDebug("Список бэкапов отправлен.")
}

// Проверяет, примонтирована ли указанная точка монтирования.
func checkMountStatus(mountPoint string) bool {
    // Используем команду mountpoint -q (quiet) для проверки
    // Команда возвращает 0, если примонтировано, 1, если нет.
    cmd := exec.Command("mountpoint", "-q", mountPoint)
    
    // Если cmd.Run() возвращает nil, это значит, что код выхода 0 (успех)
    if cmd.Run() == nil {
        return true
    }
    return false
}

// API для отмены восстановления (удаление базы в состоянии RESTORING) [cite: 29]
func handleCancelRestore(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }

    dbName := r.URL.Query().Get("name")
    if dbName == "" {
        http.Error(w, "Имя базы данных не указано.", http.StatusBadRequest)
        return
    }

    // Проверка, находится ли база данных в состоянии RESTORING
    queryState := fmt.Sprintf("SELECT state_desc FROM sys.databases WHERE name = N'%s'", dbName)
    var state string
    err := dbConn.QueryRow(queryState).Scan(&state)
    if err != nil {
        if err == sql.ErrNoRows {
            http.Error(w, fmt.Sprintf("База данных '%s' не найдена.", dbName), http.StatusNotFound)
        } else {
            LogError(fmt.Sprintf("Ошибка при проверке состояния БД %s: %v", dbName, err))
            http.Error(w, "Ошибка сервера при проверке состояния БД", http.StatusInternalServerError)
        }
        return
    }

    if state != "RESTORING" {
        http.Error(w, fmt.Sprintf("База данных '%s' не находится в состоянии RESTORING.", dbName), http.StatusBadRequest)
        return
    }

    // Удаление базы данных
    if err := deleteDatabase(dbName); err != nil {
        LogError(fmt.Sprintf("Не удалось отменить восстановление (удалить БД %s): %v", dbName, err))
        http.Error(w, fmt.Sprintf("Ошибка отмены восстановления: %v", err), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    LogInfo(fmt.Sprintf("Восстановление базы данных '%s' отменено (БД удалена).", dbName))
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
            LogDebug(fmt.Sprintf("Добавлена директория бэкапа: '%s'", dirName)) // Добавлено логирование
            baseNames = append(baseNames, BackupFile{FileName: dirName})
        }
    }
    return baseNames, nil
}
