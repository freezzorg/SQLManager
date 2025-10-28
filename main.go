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
var dbConn *sql.DB
var logMutex sync.Mutex // Мьютекс для безопасной записи в лог-файл
var briefLog []LogEntry // Краткий лог для веб-интерфейса [cite: 16]

// Главная функция, запускающая приложение
func main() {
    // 1. Загрузка конфигурации 
    if err := loadConfig("config.yaml"); err != nil {
        log.Fatalf("Ошибка загрузки конфигурации: %v", err)
    }

    // 2. Настройка логирования в файл [cite: 15, 17]
    setupLogger(appConfig.App.LogFile, appConfig.App.LogLevel)
    
    // 3. Установка подключения к MSSQL
    var err error
    dbConn, err = setupDBConnection(appConfig.MSSQL)
    if err != nil {
        // Мы используем LogError, который пишет и в файл, и в консоль
        LogError(fmt.Sprintf("Ошибка подключения к SQL Server (%s): %v", appConfig.MSSQL.Server, err))
        return
    }
    LogInfo("Успешное подключение к SQL Server.")

    // 4. Запуск веб-сервера (для systemd и ручного запуска) [cite: 6]
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
	RestorePath string `yaml:"restore_path"` // Добавляем RestorePath
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
    http.HandleFunc("/", handleIndex)
    http.HandleFunc("/api/databases", authMiddleware(handleGetDatabases)) // Получение списка БД [cite: 9]
    http.HandleFunc("/api/delete", authMiddleware(handleDeleteDatabase)) // Удаление БД [cite: 18, 7]
    http.HandleFunc("/api/backups", authMiddleware(handleGetBackups)) // Получение списка бэкапов [cite: 21, 12]
    http.HandleFunc("/api/restore", authMiddleware(handleRestoreDatabase)) // Восстановление [cite: 24]
    http.HandleFunc("/api/log", authMiddleware(handleGetBriefLog)) // Получение краткого лога [cite: 16, 31]
    http.HandleFunc("/api/cancel-restore", authMiddleware(handleCancelRestore)) // Отмена восстановления

    LogInfo(fmt.Sprintf("Веб-сервер запущен на %s", addr))
    
    // На самом деле, systemd будет отвечать за автоматический запуск [cite: 6]
    if err := http.ListenAndServe(addr, nil); err != nil {
        LogError(fmt.Sprintf("Ошибка запуска веб-сервера: %v", err))
    }
}
