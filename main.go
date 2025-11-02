package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	// Используем стандартный драйвер для MSSQL
	_ "github.com/denisenkom/go-mssqldb"
	"gopkg.in/yaml.v3"
)

var appConfig Config
var logMutex sync.Mutex // Мьютекс для безопасной записи в лог-файл (используется в logging.go)
var briefLog []LogEntry // Краткий лог для веб-интерфейса (используется в logging.go)

// Главная функция, запускающая приложение
func main() {
    // 1. Загрузка конфигурации 
    if err := loadConfig("config.yaml"); err != nil {
        log.Fatalf("Ошибка загрузки конфигурации: %v", err)
    }

    // 2. Настройка логирования в файл (функция определена в logging.go)
    setupLogger(appConfig.App.LogFile, appConfig.App.LogLevel)
    
    // 3. Установка подключения к MSSQL
    db, err := setupDBConnection(appConfig.MSSQL)
    if err != nil {
        // Мы используем LogError, который пишет и в файл, и в консоль
        LogError(fmt.Sprintf("Ошибка подключения к SQL Server (%s): %v", appConfig.MSSQL.Server, err))
        return
    }
    LogInfo("Успешное подключение к SQL Server.")
    defer db.Close() // Закрываем соединение при завершении работы приложения

    // 4. Запуск веб-сервера
    startWebServer(db, appConfig.App.BindAddress)
}

// Загружает конфигурацию из файла
func loadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &appConfig)
}

// Устанавливает соединение с SQL Server
func setupDBConnection(cfg struct {
	Server   string `yaml:"server"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	RestorePath string `yaml:"restore_path"` 
}) (*sql.DB, error) {
	connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d",
		cfg.Server, cfg.User, cfg.Password, cfg.Port)

    db, err := sql.Open("sqlserver", connString)
    if err != nil {
        return nil, err
    }
    // Проверка соединения
    if err := db.Ping(); err != nil {
        return nil, err
    }
    return db, nil
}

// Запускает веб-сервер
func startWebServer(db *sql.DB, addr string) {
    // Настройка маршрутов
    // Обслуживание статических файлов из директории "static"
    http.Handle("/", http.FileServer(http.Dir("./static")))
    
    // Создаем экземпляр AppHandlers
    handlers := &AppHandlers{DB: db}

    // API маршруты:
    http.HandleFunc("/api/databases", authMiddleware(handlers.handleGetDatabases))
    http.HandleFunc("/api/delete", authMiddleware(handlers.handleDeleteDatabase)) 
    http.HandleFunc("/api/backups", authMiddleware(handlers.handleGetBackups))
    http.HandleFunc("/api/restore", authMiddleware(handlers.handleStartRestore)) 
    http.HandleFunc("/api/log", authMiddleware(handleGetBriefLog)) // handleGetBriefLog не является методом AppHandlers
    http.HandleFunc("/api/cancel-restore", authMiddleware(handlers.handleCancelRestoreProcess)) 
    http.HandleFunc("/api/restore-progress", authMiddleware(handlers.handleGetRestoreProgress))
    http.HandleFunc("/api/backup", authMiddleware(handlers.handleStartBackup))
    http.HandleFunc("/api/backup-progress", authMiddleware(handlers.handleGetBackupProgress))

    LogInfo(fmt.Sprintf("Веб-сервер запущен на %s", addr))
    // Запускаем веб-сервер
    if err := http.ListenAndServe(addr, nil); err != nil {
        LogError(fmt.Sprintf("Ошибка запуска веб-сервера: %v", err))
    }
}
