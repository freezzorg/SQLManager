package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	_ "github.com/denisenkom/go-mssqldb"
	"gopkg.in/yaml.v3"
)

var appConfig Config
var dbConn *sql.DB
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
    var err error
    dbConn, err = setupDBConnection(appConfig.MSSQL)
    if err != nil {
        LogError(fmt.Sprintf("Ошибка подключения к SQL Server (%s): %v", appConfig.MSSQL.Server, err))
        return
    }
    LogInfo("Успешное подключение к SQL Server.")

    // 4. Запуск веб-сервера
    startWebServer(appConfig.App.BindAddress)
}

// Загружает конфигурацию из файла
func loadConfig(path string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return err
    }
    return yaml.Unmarshal(data, &appConfig)
}

// Устанавливает соединение с MSSQL
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
func startWebServer(addr string) {
    // Настройка маршрутов
    http.Handle("/", http.FileServer(http.Dir("./static")))
    
    // API маршруты:
    // handleStartRestore (бывший handleRestoreDatabase) и handleCancelRestoreProcess (бывший handleCancelRestore)
    http.HandleFunc("/api/databases", authMiddleware(handleGetDatabases))
    http.HandleFunc("/api/delete", authMiddleware(handleDeleteDatabase)) 
    http.HandleFunc("/api/backups", authMiddleware(handleGetBackups))
    http.HandleFunc("/api/restore", authMiddleware(handleStartRestore)) 
    http.HandleFunc("/api/log", authMiddleware(handleGetBriefLog))
    http.HandleFunc("/api/cancel-restore", authMiddleware(handleCancelRestoreProcess)) 

    LogInfo(fmt.Sprintf("Веб-сервер запущен на %s", addr))
    
    if err := http.ListenAndServe(addr, nil); err != nil {
        LogError(fmt.Sprintf("Ошибка запуска веб-сервера: %v", err))
    }
}