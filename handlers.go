package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
    remotePath := appConfig.SMBShare.RemotePath
    domain := appConfig.SMBShare.Domain
    user := appConfig.SMBShare.User
    password := appConfig.SMBShare.Password

    // Создаем точку монтирования, если ее нет
    if _, err := os.Stat(mountPoint); os.IsNotExist(err) {
        if err := os.MkdirAll(mountPoint, 0755); err != nil {
            LogError(fmt.Sprintf("Не удалось создать точку монтирования %s: %v", mountPoint, err))
            http.Error(w, "Ошибка сервера при подготовке к чтению бэкапов", http.StatusInternalServerError)
            return
        }
    }

    // Проверяем, смонтирована ли уже шара
    mountCheckCmd := exec.Command("findmnt", "-M", mountPoint)
    if err := mountCheckCmd.Run(); err != nil { // Если не смонтирована, монтируем
        LogInfo(fmt.Sprintf("Монтирование SMB-шары %s на %s...", remotePath, mountPoint))
        mountCmd := exec.Command("sudo", "mount", "-t", "cifs",
            fmt.Sprintf("//%s", strings.ReplaceAll(remotePath, "\\", "/")),
            mountPoint,
            "-o", fmt.Sprintf("username=%s\\%s,password=%s,domain=%s,vers=3.0,uid=%d,gid=%d", domain, user, password, domain, os.Getuid(), os.Getgid())) // uid/gid для доступа текущего пользователя
        
        output, err := mountCmd.CombinedOutput()
        if err != nil {
            LogError(fmt.Sprintf("Ошибка монтирования SMB-шары: %v\n%s", err, string(output)))
            http.Error(w, "Ошибка сервера при монтировании SMB-шары", http.StatusInternalServerError)
            return
        }
        LogInfo(fmt.Sprintf("SMB-шара %s успешно смонтирована на %s.", remotePath, mountPoint))
    } else {
        LogDebug(fmt.Sprintf("SMB-шара %s уже смонтирована на %s.", remotePath, mountPoint))
    }

    // 2. Чтение файлов бэкапов из локальной точки монтирования
    files, err := os.ReadDir(mountPoint)
    if err != nil {
        LogError(fmt.Sprintf("Ошибка чтения каталога бэкапов %s: %v", mountPoint, err))
        http.Error(w, "Ошибка сервера при чтении бэкапов", http.StatusInternalServerError)
        return
    }

    var backupFiles []BackupFile
    for _, file := range files {
        if file.IsDir() {
            continue
        }
        fileName := file.Name()

        // Проверка на черный список
        isBlacklisted := false
        for _, blName := range appConfig.App.BackupBlacklist {
            if strings.Contains(fileName, blName) {
                isBlacklisted = true
                LogDebug(fmt.Sprintf("Файл бэкапа '%s' находится в черном списке и будет пропущен.", fileName))
                break
            }
        }
        if isBlacklisted {
            continue
        }

        if strings.HasSuffix(fileName, ".bak") || strings.HasSuffix(fileName, ".diff") || strings.HasSuffix(fileName, ".trn") {
            fullPath := filepath.Join(mountPoint, fileName)
            
            // Получение метаданных бэкапа с помощью RESTORE HEADERONLY
            headerQuery := fmt.Sprintf("RESTORE HEADERONLY FROM DISK = N'%s'", fullPath)
            rows, err := dbConn.Query(headerQuery)
            if err != nil {
                LogWarning(fmt.Sprintf("Не удалось получить заголовок бэкапа %s: %v", fileName, err))
                continue
            }
            defer rows.Close()

            var backupDate time.Time
            var backupType string
            
            // Используем структуру для более надежного сканирования заголовка бэкапа
            var header struct {
                BackupType       int       `sql:"BackupType"`
                BackupFinishDate time.Time `sql:"BackupFinishDate"`
            }

            // Для сканирования полей по имени, нужно использовать обертку или кастомный сканер.
            // В данном случае, для простоты, будем сканировать все поля в интерфейсы, а затем выбирать нужные.
            // Это менее эффективно, но более надежно, чем позиционное сканирование.
            columns, err := rows.Columns()
            if err != nil {
                LogWarning(fmt.Sprintf("Ошибка получения имен столбцов для заголовка бэкапа %s: %v", fileName, err))
                continue
            }

            values := make([]interface{}, len(columns))
            valuePtrs := make([]interface{}, len(columns))
            for i := range columns {
                valuePtrs[i] = &values[i]
            }

            if rows.Next() {
                err := rows.Scan(valuePtrs...)
                if err != nil {
                    LogWarning(fmt.Sprintf("Ошибка сканирования заголовка бэкапа %s: %v", fileName, err))
                    continue
                }

                // Поиск нужных полей по имени
                for i, colName := range columns {
                    switch colName {
                    case "BackupType":
                        if val, ok := values[i].(int64); ok {
                            header.BackupType = int(val)
                        }
                    case "BackupFinishDate":
                        if val, ok := values[i].(time.Time); ok {
                            header.BackupFinishDate = val
                        }
                    }
                }
                
                backupDate = header.BackupFinishDate
                switch header.BackupType {
                case 1:
                    backupType = ".bak" // Full
                case 2:
                    backupType = ".diff" // Differential
                case 5:
                    backupType = ".trn" // Transaction Log
                default:
                    backupType = ".unknown"
                }
            } else {
                LogWarning(fmt.Sprintf("Не удалось прочитать заголовок бэкапа %s: нет строк", fileName))
                continue
            }

            backupFiles = append(backupFiles, BackupFile{
                FileName:  fileName,
                Type:      backupType,
                BackupDate: backupDate,
            })
        }
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(backupFiles)
    LogDebug("Список бэкапов отправлен.")
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
